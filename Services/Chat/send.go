package Chat

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/anyscapelabs/argus/Keyring"
	"github.com/anyscapelabs/argus/Models"
	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Router"
	"github.com/anyscapelabs/argus/Services/Location"
	"github.com/anyscapelabs/argus/Services/Memory"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
	"github.com/google/uuid"
)

const defaultMaxOutputTokens = int64(4096)

// maxToolRounds bounds the agent loop: model turn + tool execution pairs.
const maxToolRounds = 12

// maxModelRetries bounds retries per model turn when the provider failed in
// a retryable way (429, 5xx, timeout). Long unattended runs die on a single
// blip otherwise.
const maxModelRetries = 2

// wrapUpDirective is injected as a final user turn when the round budget is
// exhausted, so the run ends with an honest summary instead of a canned lie.
const wrapUpDirective = "You have reached the tool-round limit for this run. Do not call any more tools. " +
	"Tell the user plainly what you already did, what you learned, and what could not be finished."

// toolCalls collects the completed tool_use blocks of a collected turn.
func (s *Service) Send(agentID, text string) Shared.SendResult {
	return s.send(agentID, text, false, "")
}

// SendScheduledWake starts the turn a scheduled task's fire produces. It is
// deliberately unlike Send: no user spoke. The row is written with kind
// 'scheduled' — invisible in the transcript, replayed in history like a
// steering row — and the run is unattended, so a routine at 7am on a desk with
// no one watching still works. The wake text the scheduler passes is
// phrased as the job description it is, not as something a person said.
func (s *Service) SendScheduledWake(agentID, text string) Shared.SendResult {
	return s.send(agentID, text, true, Storage.MessageKindScheduled)
}

// Steer starts a run on a row the person did not type. The connector uses it
// when an OAuth settle lands after the run that asked for the connection is
// already gone: without a turn of its own the agent has no way to learn the
// outcome, and the user has to say "I connected" to get the work moving again.
// The row is written with kind 'steering' so the model reads it as new
// information while the transcript keeps it out of the user's mouth.
//
// It is deliberately not exposed to the frontend: nothing but the app's own
// services should be able to speak as the user.
func (s *Service) Steer(agentID, text string) Shared.SendResult {
	return s.send(agentID, text, false, Storage.MessageKindSteering)
}

// unattended drives the permission gate; kind drives the row's visibility.
// Only "", Storage.MessageKindSteering and Storage.MessageKindScheduled are
// written today.
func (s *Service) send(agentID, text string, unattended bool, kind string) Shared.SendResult {
	log.Printf("Send called agent=%s unattended=%v kind=%s", agentID, unattended, kind)
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.SendResult{Error: aerr}
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		agentID = "default"
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return Shared.SendResult{Error: Shared.NewError(Shared.CodeBadInput, "Message text is required")}
	}

	profile, err := s.ensureAgent(agentID)
	if err != nil {
		return Shared.SendResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	if strings.TrimSpace(profile.ModelRef) == "" {
		return Shared.SendResult{Error: Shared.NewError(Shared.CodeBadInput, "Select a model before sending")}
	}

	// Reserve the agent's run slot up front so a double-click cannot start
	// two runs. The empty value marks the reservation; it is filled with
	// the real run id below or released on any failure.
	s.mu.Lock()
	if _, ok := s.agentRun[agentID]; ok {
		s.mu.Unlock()
		return Shared.SendResult{Error: Shared.NewError(Shared.CodeBusy, "The agent is busy")}
	}
	if s.agentRun == nil {
		s.agentRun = map[string]string{}
	}
	s.agentRun[agentID] = ""
	s.mu.Unlock()
	release := func() {
		s.mu.Lock()
		if s.agentRun[agentID] == "" {
			delete(s.agentRun, agentID)
		}
		s.mu.Unlock()
	}

	// Validate the provider, key and routing target before any message or
	// request row is written, so a rejected send never leaves artifacts
	// behind in the chat.
	providerID, modelID := Models.SplitRef(profile.ModelRef)
	provider, ok := s.Core.Store.Get().Provider(providerID)
	if !ok {
		release()
		return Shared.SendResult{Error: Shared.NewError(Shared.CodeUnknownProvider, "Unknown provider")}
	}
	if base := s.baseURLOverride(providerID); base != "" {
		provider.BaseURL = base
	}
	// Defence in depth: the selected model must be in the paired plan's
	// allowlist. The router enforces it server-side too, but failing here
	// keeps the agent's profile honest.
	if providerID == Shared.RouterProviderID && !s.allowedModel(modelID) {
		release()
		return Shared.SendResult{Error: Shared.NewError(Shared.CodeBadInput, "This model is not in your plan")}
	}
	overlayProvider, _ := s.Core.Overlay.Lookup(providerID)
	key := ""
	if !provider.NoAuth {
		stored, kerr := getKey(providerID)
		if kerr != nil {
			release()
			if errors.Is(kerr, Keyring.ErrNotFound) {
				return Shared.SendResult{Error: Shared.NewError(Shared.CodeNotConfigured, "Connect "+providerID+" before sending")}
			}
			return Shared.SendResult{Error: Shared.NewError(Shared.CodeStorage, "The secrets store is unreachable. Re-enter the key")}
		}
		if strings.TrimSpace(stored) == "" {
			release()
			return Shared.SendResult{Error: Shared.NewError(Shared.CodeNotConfigured, "Connect "+providerID+" before sending")}
		}
		key = stored
	}
	target, terr := Router.TargetFor(provider, modelID, key, Router.TargetOptions{
		ExtraHeaders: overlayProvider.ExtraHeaders,
		Quirks:       overlayProvider.Quirks,
	})
	if terr != nil {
		release()
		return Shared.SendResult{Error: Shared.NewError(Shared.CodeBadInput, terr.Error())}
	}

	// Everything above is now validated: write the message rows.
	userSeq, serr := s.Core.DB.NextSeq(agentID)
	if serr != nil {
		release()
		return Shared.SendResult{Error: Shared.NewError(Shared.CodeStorage, serr.Error())}
	}
	assistantSeq := userSeq + 1

	uid, _ := uuid.NewV7()
	userID := uid.String()
	aid, _ := uuid.NewV7()
	assistantID := aid.String()
	rid, _ := uuid.NewV7()
	runID := rid.String()

	userMsg := Storage.Message{
		ID:          userID,
		AgentID:     agentID,
		Seq:         userSeq,
		Role:        "user",
		Kind:        kind,
		BlocksJSON:  mustMarshal([]Protocol.Block{{Type: Protocol.BlockText, Text: text}}),
		Text:        text,
		Status:      "complete",
		CreatedAt:   nowUnixMs(),
		CompletedAt: nowUnixMs(),
	}
	if err := s.Core.DB.InsertMessage(userMsg); err != nil {
		release()
		return Shared.SendResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}

	assistantMsg := Storage.Message{
		ID:         assistantID,
		AgentID:    agentID,
		Seq:        assistantSeq,
		Role:       "assistant",
		BlocksJSON: "[]",
		Text:       "",
		Status:     "streaming",
		ModelRef:   profile.ModelRef,
		CreatedAt:  nowUnixMs(),
	}
	if err := s.Core.DB.InsertMessage(assistantMsg); err != nil {
		release()
		return Shared.SendResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}

	// contexto through userSeq (the user message was inserted above)
	// Only a short recent window is replayed; anything older is reached through
	// conversation_read. Every model the router allows can call tools, so there
	// is no long-replay fallback.
	history := s.buildHistory(agentID, userSeq, s.historyBudgetTokens(profile.ModelRef, providerID, modelID), recentHistoryMessages)

	req := Protocol.Request{
		Model:           modelID,
		Messages:        history,
		MaxOutputTokens: defaultMaxOutputTokens,
	}
	// Terminal tool calling, gated by the capability check inside
	// attachTools; the system prompt tells the model about it only when the
	// tool is actually attached. Integration tools ride along only when the
	// replayed history already touched them; every other connector waits
	// behind the loader, so a request costs schemas for one integration at
	// most instead of all of them.
	withTools := s.attachTools(&req, &target, mcpServersReferenced(history, s.connectedServerIDs()))
	sysPrompt := systemPromptFor(profile, withTools)
	// Durable facts the agent saved earlier, re-read every run.
	if mem := Memory.PromptBlock(s.Core.DB, withTools); mem != "" {
		sysPrompt += mem
	}
	// Timezone and the approximate location — resolved fresh per send so
	// dates and local reasoning stay correct.
	if loc := Location.Current(sCtx(), s.Core.DB); loc.Timezone != "" {
		sysPrompt += loc.Prompt()
	}
	// Which connectors are live, counted from the tools actually attached right
	// now. Re-read every run so a sign-in that finished after the agent last
	// looked needs no one telling: the run before it, the model had only its own
	// "stop and wait" tool result to go on.
	if s.MCPConnect != nil {
		if live := s.MCPConnect.PromptBlock(withTools); live != "" {
			sysPrompt += live
		}
	}
	req.System = sysPrompt
	log.Printf("chat: history messages=%d system_tokens≈%d tools=%d", len(history), s.historySystemTokens(&req), len(req.Tools))

	// Keep the default output budget unless the model's own limit is tighter,
	// so reasoning models aren't handed a field they reject.
	clampToModel(s, profile.ModelRef, providerID, modelID, &target, &req.MaxOutputTokens)

	mr := Storage.ModelRequest{
		ID:                  runID,
		AgentID:             agentID,
		MessageID:           assistantID,
		Attempt:             1,
		ProviderID:          providerID,
		ModelID:             modelID,
		ModelRef:            profile.ModelRef,
		AdapterFamily:       string(target.Family),
		BaseHost:            hostOf(provider.BaseURL),
		Slot:                "",
		ReasoningLevel:      "",
		MaxOutputTokens:     req.MaxOutputTokens,
		SystemPrompt:        sysPrompt,
		ContextThroughSeq:   userSeq,
		ContextMessageCount: len(history),
		Status:              "started",
		StartedAt:           nowUnixMs(),
	}
	if err := s.Core.DB.InsertModelRequest(mr); err != nil {
		release()
		return Shared.SendResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}

	s.mu.Lock()
	if s.cancels == nil {
		s.cancels = map[string]context.CancelFunc{}
	}
	s.agentRun[agentID] = runID
	ctx, cancel := context.WithCancel(context.Background())
	if unattended {
		ctx = Shared.WithUnattended(ctx)
	}
	s.cancels[runID] = cancel
	s.mu.Unlock()

	go s.runTurn(runID, ctx, cancel, agentID, assistantID, profile.ModelRef, req, &target, profile, mr)

	s.Core.EmitEvent(Shared.EventChatMessage, map[string]any{"agent_id": agentID, "message_id": userID})
	s.Core.EmitEvent(Shared.EventChatMessage, map[string]any{"agent_id": agentID, "message_id": assistantID})

	return Shared.SendResult{Ok: true, RunID: runID, UserMessageID: userID, AssistantMessageID: assistantID}
}

func messageIsOpaqueProviderError(e *Protocol.Error) bool {
	if e == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(e.Message))
	if msg == "" {
		return true
	}
	if msg == "provider returned error" {
		return true
	}
	// OpenRouter's catch-all: "Provider returned error" with a raw of
	// "ERROR" means the hidden backend failed without explanation.
	if msg == "error" || msg == "internal error" {
		return true
	}
	return false
}

// friendlyError replaces an opaque provider message with one a person can
// act on. The raw body and code are kept in the database (error_raw), so
// debugging is unaffected — only the visible string changes.
func friendlyError(e *Protocol.Error) {
	if e == nil || !messageIsOpaqueProviderError(e) {
		return
	}
	e.Message = "Sorry, something went wrong connecting to the model. Please try again."
}

// retryDelay honors the provider's Retry-After when present, otherwise backs
// off exponentially (2s, 4s).
func retryDelay(err *Protocol.Error, tries int) time.Duration {
	if err.RetryAfter > 0 {
		d := time.Duration(err.RetryAfter) * time.Second
		if d > 90*time.Second {
			d = 90 * time.Second
		}
		return d
	}
	return time.Duration(2<<tries) * time.Second
}

// stripToolUses removes tool_use blocks from a message being persisted. An
// assistant row whose tool_results never got written (cancelled or failed
// turn) would otherwise sit in history as an orphaned call the provider
// rejects, poisoning every later send.
func stripToolUses(blocks []Protocol.Block) []Protocol.Block {
	out := make([]Protocol.Block, 0, len(blocks))
	for _, b := range blocks {
		if b.Type == Protocol.BlockToolUse {
			continue
		}
		out = append(out, b)
	}
	return out
}

func clampToModel(s *Service, modelRef, providerID, modelID string, target *Protocol.Target, maxOut *int64) {
	if maxOut == nil {
		return
	}
	ad, aerr := Router.AdapterFor(target)
	if aerr != nil {
		return
	}
	info := Protocol.ModelInfo{ID: modelID}
	if m, ok := s.Core.Store.Get().Get(modelRef); ok && m.Limits != nil {
		info.ContextLen = m.Limits.Context
		info.MaxOutput = m.Limits.Output
	} else if rows, lerr := s.Core.DB.ListCustomModels(providerID); lerr == nil {
		for _, cm := range rows {
			if cm.ModelID == modelID {
				info.ContextLen = cm.ContextWindow
				info.MaxOutput = cm.MaxOutput
				break
			}
		}
	}
	if info.MaxOutput <= 0 {
		return
	}
	scratch := &Protocol.Request{MaxOutputTokens: *maxOut}
	Protocol.ClampRequest(scratch, ad.Capabilities(info))
	*maxOut = scratch.MaxOutputTokens
}

func (s *Service) ensureAgent(agentID string) (Storage.AgentProfile, error) {
	p, found, err := s.Core.DB.GetAgentProfile(agentID)
	if err != nil {
		return p, err
	}
	if found {
		return p, nil
	}
	p = Storage.AgentProfile{ID: agentID, AgentName: "Argus", Avatar: "/logo.png", UpdatedAt: ""}
	if err := s.Core.DB.SaveAgentProfile(p); err != nil {
		return p, err
	}
	return p, nil
}

func (s *Service) computeCost(usage Protocol.Usage, modelRef string) *float64 {
	// try catalog
	cat := s.Core.Store.Get()
	if m, ok := cat.Get(modelRef); ok && m.Cost != nil {
		return computeCostFromPrice(usage, m.Cost)
	}
	// custom models
	rows, err := s.Core.DB.ListAllCustomModels()
	if err == nil {
		for _, cm := range rows {
			if cm.Ref() == modelRef {
				// build cost
				cost := &Models.Cost{}
				if cm.PriceKnown {
					cost.Input = &cm.PriceInput
					cost.Output = &cm.PriceOutput
				}
				return computeCostFromPrice(usage, cost)
			}
		}
	}
	return nil
}

func computeCostFromPrice(usage Protocol.Usage, cost *Models.Cost) *float64 {
	if cost == nil {
		return nil
	}
	total := 0.0
	if usage.InputTokens > 0 {
		if cost.Input == nil {
			return nil
		}
		total += float64(usage.InputTokens) / 1e6 * *cost.Input
	}
	if usage.CacheReadTokens > 0 {
		price := cost.CacheRead
		if price == nil {
			price = cost.Input
		}
		if price == nil {
			return nil
		}
		total += float64(usage.CacheReadTokens) / 1e6 * *price
	}
	if usage.CacheWriteTokens > 0 {
		price := cost.CacheWrite
		if price == nil {
			price = cost.Input
		}
		if price == nil {
			return nil
		}
		total += float64(usage.CacheWriteTokens) / 1e6 * *price
	}
	if usage.OutputTokens > 0 {
		if cost.Output == nil {
			return nil
		}
		total += float64(usage.OutputTokens) / 1e6 * *cost.Output
	}
	if usage.ReasoningTokens > 0 {
		price := cost.Reasoning
		if price == nil {
			price = cost.Output
		}
		if price == nil {
			return nil
		}
		total += float64(usage.ReasoningTokens) / 1e6 * *price
	}
	return &total
}

func flattenText(blocks []Protocol.Block) string {
	var b strings.Builder
	for _, blk := range blocks {
		if blk.Type == Protocol.BlockText {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(blk.Text)
		}
	}
	return b.String()
}

func mustMarshal(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func hostOf(raw string) string {
	raw = strings.TrimPrefix(raw, "https://")
	raw = strings.TrimPrefix(raw, "http://")
	if i := strings.Index(raw, "/"); i >= 0 {
		raw = raw[:i]
	}
	return raw
}

func nowUnixMs() int64 {
	return time.Now().UnixMilli()
}

func (s *Service) baseURLOverride(providerID string) string {
	cfg, found, err := s.Core.DB.GetProviderConfig(providerID)
	if err != nil || !found {
		return ""
	}
	return strings.TrimSpace(cfg.BaseURLOverride)
}

func getKey(providerID string) (string, error) {
	return Keyring.GetFor(providerID)
}

func (s *Service) allowedModel(modelID string) bool {
	if s.Core == nil || s.Core.DB == nil {
		return true
	}
	payload, _, found, err := s.Core.DB.GetAccountEntitlements()
	if err != nil || !found || payload == "" {
		return true
	}
	var ent struct {
		Allowlist []any `json:"allowlist"`
	}
	if err := json.Unmarshal([]byte(payload), &ent); err != nil || len(ent.Allowlist) == 0 {
		return true
	}
	for _, item := range ent.Allowlist {
		switch v := item.(type) {
		case string:
			if strings.EqualFold(strings.TrimSpace(v), modelID) {
				return true
			}
		case map[string]any:
			if id, _ := v["id"].(string); strings.EqualFold(strings.TrimSpace(id), modelID) {
				return true
			}
		}
	}
	return false
}

// sCtx gives the location resolver its own bounded network budget without
// touching the run lifecycle.
func sCtx() context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	_ = cancel // lifetime bounded by the inner request timeout
	return ctx
}

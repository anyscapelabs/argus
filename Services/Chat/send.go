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
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
	"github.com/google/uuid"
)

const defaultMaxOutputTokens = int64(4096)

func (s *Service) Send(agentID, text string) Shared.SendResult {
	log.Printf("Send called agent=%s", agentID)
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

	// context through userSeq (the user message was inserted above)
	history, sysPrompt := s.buildHistory(agentID, userSeq, profile.SystemPrompt)

	req := Protocol.Request{
		Model:           modelID,
		System:          sysPrompt,
		Messages:        history,
		MaxOutputTokens: defaultMaxOutputTokens,
	}
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
	s.cancels[runID] = cancel
	s.mu.Unlock()

	go s.runTurn(runID, ctx, cancel, agentID, assistantID, profile.ModelRef, req, &target, profile, mr)

	s.Core.EmitEvent(Shared.EventChatMessage, map[string]any{"agent_id": agentID, "message_id": userID})
	s.Core.EmitEvent(Shared.EventChatMessage, map[string]any{"agent_id": agentID, "message_id": assistantID})

	return Shared.SendResult{Ok: true, RunID: runID, UserMessageID: userID, AssistantMessageID: assistantID}
}

func (s *Service) runTurn(runID string, ctx context.Context, cancel context.CancelFunc, agentID, assistantID, modelRef string, req Protocol.Request, target *Protocol.Target, profile Storage.AgentProfile, mr Storage.ModelRequest) {
	defer cancel()
	trace := &Router.Trace{}
	events := []Protocol.Event{}
	for ev := range Router.New().Run(ctx, &req, target, trace) {
		s.Core.EmitEvent(Shared.EventModelStream, Shared.StreamEvent{RunID: runID, AgentID: agentID, MessageID: assistantID, Event: ev})
		events = append(events, ev)
	}

	seq := func(yield func(Protocol.Event) bool) {
		for _, ev := range events {
			if !yield(ev) {
				return
			}
		}
	}
	result := Protocol.Collect(seq)

	// The message table and the model_requests table have different
	// constraint sets: messages use 'complete' for a finished reply while
	// model_requests use 'success'.
	msgStatus := "complete"
	reqStatus := "success"
	if ctx.Err() != nil {
		msgStatus = "cancelled"
		reqStatus = "cancelled"
	} else if result.Error != nil {
		msgStatus = "error"
		reqStatus = "error"
	}

	blocks := result.Message.Blocks
	text := flattenText(blocks)
	stopReason := ""
	if result.Stop.Raw != "" {
		stopReason = result.Stop.Raw
	}
	errorMessage := ""
	if result.Error != nil {
		errorMessage = result.Error.Message
	}
	if err := s.Core.DB.FinalizeMessage(assistantID, blocks, text, msgStatus, stopReason, modelRef, errorMessage, nowUnixMs()); err != nil {
		log.Printf("chat: could not finalize message %s: %v", assistantID, err)
	}

	// finalize request
	finalMR := mr
	finalMR.Status = reqStatus
	if result.Error != nil {
		finalMR.ErrorCode = result.Error.Code
		finalMR.ErrorMessage = result.Error.Message
		finalMR.ErrorRaw = result.Error.Raw
		if result.Error.Retryable {
			finalMR.ErrorRetryable = true
		}
	}
	finalMR.StopReason = stopReason
	finalMR.HTTPStatus = trace.HTTPStatus
	finalMR.ProviderRequestID = trace.ProviderRequestID
	if trace.FirstByteAt > 0 {
		finalMR.FirstByteAt = trace.FirstByteAt
	}
	if trace.FirstTokenAt > 0 {
		finalMR.FirstTokenAt = trace.FirstTokenAt
	}
	finalMR.CompletedAt = nowUnixMs()

	// usage
	usage := result.Usage
	hasUsage := usage.InputTokens != 0 || usage.OutputTokens != 0 || usage.CacheReadTokens != 0 || usage.CacheWriteTokens != 0 || usage.ReasoningTokens != 0
	if hasUsage {
		finalMR.UsageSource = "provider"
		in := usage.InputTokens
		cr := usage.CacheReadTokens
		cw := usage.CacheWriteTokens
		out := usage.OutputTokens
		rs := usage.ReasoningTokens
		finalMR.InputTokens = &in
		finalMR.CacheReadTokens = &cr
		finalMR.CacheWriteTokens = &cw
		finalMR.OutputTokens = &out
		finalMR.ReasoningTokens = &rs
		// cost
		cost := s.computeCost(usage, modelRef)
		finalMR.CostUSD = cost
	} else {
		finalMR.UsageSource = "missing"
	}
	raw, _ := json.Marshal(usage)
	finalMR.UsageRawJSON = string(raw)

	if err := s.Core.DB.FinalizeModelRequest(finalMR); err != nil {
		log.Printf("chat: could not finalize model request %s: %v", finalMR.ID, err)
	}

	s.mu.Lock()
	delete(s.agentRun, agentID)
	delete(s.cancels, runID)
	s.mu.Unlock()

	s.Core.EmitEvent(Shared.EventChatMessage, map[string]any{"agent_id": agentID, "message_id": assistantID})
	s.Core.EmitEvent(Shared.EventChatRunFinished, map[string]any{"agent_id": agentID, "run_id": runID, "status": reqStatus})
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

func (s *Service) buildHistory(agentID string, throughSeq int, systemPrompt string) ([]Protocol.Message, string) {
	msgs, err := s.Core.DB.ListMessages(agentID, 0, 1000)
	if err != nil {
		return nil, systemPrompt
	}
	var out []Protocol.Message
	for _, m := range msgs {
		if m.Seq > throughSeq {
			continue
		}
		if m.Status != "complete" {
			continue
		}
		blocks := m.Blocks()
		var textBlocks []Protocol.Block
		for _, b := range blocks {
			if b.Type == Protocol.BlockText {
				textBlocks = append(textBlocks, Protocol.Block{Type: Protocol.BlockText, Text: b.Text})
			}
		}
		if len(textBlocks) == 0 && m.Text != "" {
			textBlocks = []Protocol.Block{{Type: Protocol.BlockText, Text: m.Text}}
		}
		if len(textBlocks) == 0 {
			continue
		}
		out = append(out, Protocol.Message{Role: Protocol.Role(m.Role), Blocks: textBlocks})
	}
	// trim to last 40
	if len(out) > 40 {
		out = out[len(out)-40:]
	}
	return out, systemPrompt
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
	return Keyring.Get(providerID)
}

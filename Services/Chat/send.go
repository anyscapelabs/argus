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
func toolCalls(msg Protocol.Message) []Protocol.ToolCall {
	var calls []Protocol.ToolCall
	for _, b := range msg.Blocks {
		if b.Type == Protocol.BlockToolUse && b.ToolCall != nil && b.ToolCall.Name != "" {
			calls = append(calls, *b.ToolCall)
		}
	}
	return calls
}

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
	history := s.buildHistory(agentID, userSeq, s.historyBudgetTokens(profile.ModelRef, providerID, modelID))

	req := Protocol.Request{
		Model:           modelID,
		Messages:        history,
		MaxOutputTokens: defaultMaxOutputTokens,
	}
	// Terminal tool calling, gated by the capability check inside
	// attachTools; the system prompt tells the model about it only when the
	// tool is actually attached.
	withTools := s.attachTools(&req, &target)
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

func (s *Service) runTurn(runID string, ctx context.Context, cancel context.CancelFunc, agentID, assistantID, modelRef string, req Protocol.Request, target *Protocol.Target, profile Storage.AgentProfile, mr Storage.ModelRequest) {
	defer cancel()
	trace := &Router.Trace{}
	total := Protocol.Usage{}
	var result Protocol.Result
	reachedEnd := false
	rounds := 0
	extraTries := 0

	// runModelTurn asks the model for one turn and re-asks on retryable
	// provider failures. The request is stateless, so a retry simply re-runs
	// the turn; tools already executed earlier are not re-executed.
	runModelTurn := func() Protocol.Result {
		res := s.collectWithTyping(ctx, runID, agentID, &req, target, trace)
		for tries := 0; res.Error != nil && res.Error.Retryable && ctx.Err() == nil && tries < maxModelRetries; tries++ {
			extraTries++
			select {
			case <-ctx.Done():
			case <-time.After(retryDelay(res.Error, tries)):
			}
			if ctx.Err() != nil {
				break
			}
			res = s.collectWithTyping(ctx, runID, agentID, &req, target, trace)
		}
		total.Merge(&res.Usage)
		return res
	}

	// Agent loop: run model turns and execute tools until the model stops
	// requesting tools. Every completed round-trip is persisted as its own
	// message pair, so the next send replays what was actually done and an
	// unattended follow-up turn cannot repeat or forget earlier commands.
	for round := 0; round < maxToolRounds && !reachedEnd; round++ {
		result = runModelTurn()

		if ctx.Err() != nil || result.Error != nil {
			reachedEnd = true
			break
		}

		calls := toolCalls(result.Message)
		if len(calls) == 0 {
			reachedEnd = true
			break
		}

		// Replaying the assistant turn, then appending one user message
		// carrying every tool_result block, is the shape all three adapter
		// families translate back into their own wire format.
		req.Messages = append(req.Messages, result.Message)
		roundResults := make([]Protocol.Block, 0, len(calls))
		for _, call := range calls {
			roundResults = append(roundResults, s.runTool(ctx, runID, agentID, profile.AgentName, call))
		}
		if len(roundResults) > 0 {
			req.Messages = append(req.Messages, Protocol.Message{Role: Protocol.RoleUser, Blocks: roundResults})
			s.persistRound(agentID, modelRef, result.Message, roundResults)
		}
		rounds++
	}

	if !reachedEnd && ctx.Err() == nil && result.Error == nil {
		// Out of rounds with the model still mid-plan. Don't end on the
		// limit itself: give it one tools-off turn to summarize honestly
		// what ran and what did not.
		req.Tools = nil
		req.Messages = append(req.Messages, Protocol.Message{
			Role:   Protocol.RoleUser,
			Blocks: []Protocol.Block{{Type: Protocol.BlockText, Text: wrapUpDirective}},
		})
		if wrap := runModelTurn(); wrap.Error == nil && len(toolCalls(wrap.Message)) == 0 {
			result = wrap
		} else {
			result.Message.Blocks = append(stripToolUses(result.Message.Blocks), Protocol.Block{
				Type: Protocol.BlockText,
				Text: "Stopped at the tool-round limit. The actions listed above ran; nothing after them did.",
			})
		}
	}

	// Opaque provider failures are transient more often than they are our
	// fault, so a single retry gets a second chance before the user sees
	// the error. A bad-input refusal that names a problem we can fix would
	// repeat identically, so only the generic "provider returned error"
	// class is considered retryable here — and only once, so a real bug
	// still surfaces instead of spinning.
	if result.Error != nil && ctx.Err() == nil && messageIsOpaqueProviderError(result.Error) && extraTries == 0 {
		extraTries++
		res := s.collectWithTyping(ctx, runID, agentID, &req, target, trace)
		if res.Error == nil {
			result = res
			total.Merge(&res.Usage)
		} else {
			friendlyError(res.Error)
			result = res
		}
	} else if result.Error != nil {
		friendlyError(result.Error)
	}

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

	blocks := stripToolUses(result.Message.Blocks)
	text := flattenText(blocks)
	// The reply row was reserved before any round existed; move it behind
	// the round pairs so history replays in execution order.
	if rounds > 0 {
		if finalSeq, seqErr := s.Core.DB.NextSeq(agentID); seqErr != nil {
			log.Printf("chat: could not reserve final seq for run %s: %v", runID, seqErr)
		} else if err := s.Core.DB.ReseqMessage(assistantID, finalSeq); err != nil {
			log.Printf("chat: could not reseq reply %s: %v", assistantID, err)
		}
	}
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
	finalMR.Attempt = 1 + extraTries
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
	usage := total
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
	s.runFinished(RunOutcome{
		RunID:              runID,
		AgentID:            agentID,
		AssistantMessageID: assistantID,
		Status:             reqStatus,
	})
}

// persistRound writes one completed tool round-trip to the chat: the
// assistant row that requested the tools, then a user carrier row holding the
// results. Carriers have no visible text and are hidden by GetChat, but
// buildHistory replays them, which is exactly what makes past actions visible
// to future turns.
func (s *Service) persistRound(agentID, modelRef string, assistantMsg Protocol.Message, results []Protocol.Block) {
	aid, _ := uuid.NewV7()
	uid, _ := uuid.NewV7()
	rows := []Storage.Message{
		{ID: aid.String(), AgentID: agentID, Role: "assistant",
			BlocksJSON: mustMarshal(assistantMsg.Blocks), Text: flattenText(assistantMsg.Blocks),
			Status: "complete", ModelRef: modelRef, CreatedAt: nowUnixMs(), CompletedAt: nowUnixMs()},
		{ID: uid.String(), AgentID: agentID, Role: "user",
			BlocksJSON: mustMarshal(results), Text: "",
			Status: "complete", CreatedAt: nowUnixMs(), CompletedAt: nowUnixMs()},
	}
	for _, row := range rows {
		seq, err := s.Core.DB.NextSeq(agentID)
		if err != nil {
			log.Printf("chat: round seq failed for agent %s: %v", agentID, err)
			return
		}
		row.Seq = seq
		if err := s.Core.DB.InsertMessage(row); err != nil {
			log.Printf("chat: round persist failed for agent %s: %v", agentID, err)
			return
		}
		s.Core.EmitEvent(Shared.EventChatMessage, map[string]any{"agent_id": agentID, "message_id": row.ID})
	}
}

// messageIsOpaqueProviderError reports whether an error carries no usable
// detail: the provider said something generic ("Provider returned error" or
// empty) and gave no code a user could act on. These are upstream flakiness,
// not our bug, so the run retries once and, if it still fails, the friendly
// message above replaces the opaque one.
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

func (s *Service) buildHistory(agentID string, throughSeq int, budgetTokens int64) []Protocol.Message {
	msgs, err := s.Core.DB.ListMessages(agentID, 0, 1000)
	if err != nil {
		return nil
	}
	agentName := agentDisplayName(s, agentID)
	var out []Protocol.Message
	// Steer rows are invisible turns the app raises itself (takeover
	// handover, sign-in settled). The model must still see them, so they
	// become user turns — but a steer row never has a genuine preceding
	// tool round-trip, so a tool_result carrier beside one would read as a
	// response to a call the model never made.
	for _, m := range msgs {
		if m.Seq > throughSeq {
			continue
		}
		if m.Status != "complete" {
			continue
		}
		if m.Kind == "steering" {
			// A steer row is a user turn wearing a hidden costume; it can
			// never be the answer to a tool call, so it is replayed as text
			// and any tool_result sitting beside it is dropped.
			if m.Text != "" {
				appendMerged(&out, Protocol.RoleUser, []Protocol.Block{{Type: Protocol.BlockText, Text: m.Text}})
			}
			continue
		}
		if m.Role != "assistant" {
			// User rows: replay text blocks plus any tool_result blocks
			// from prior agentic turns so the model sees complete tool
			// round-trips. A result whose call is not the immediately
			// preceding kept message is orphaned and dropped.
			var userBlocks []Protocol.Block
			for _, b := range m.Blocks() {
				switch b.Type {
				case Protocol.BlockText:
					userBlocks = append(userBlocks, Protocol.Block{Type: Protocol.BlockText, Text: b.Text})
				case Protocol.BlockToolResult:
					if n := len(out); n > 0 && carriesToolUse(out[n-1]) {
						userBlocks = append(userBlocks, b)
					}
				}
			}
			if len(userBlocks) == 0 && m.Text != "" {
				userBlocks = []Protocol.Block{{Type: Protocol.BlockText, Text: m.Text}}
			}
			if len(userBlocks) == 0 {
				continue
			}
			appendMerged(&out, Protocol.Role(m.Role), userBlocks)
			continue
		}
		// Assistant rows: replay text blocks and tool_use blocks so the model
		// remembers which tools it called and can pair them with results.
		// A tool_use survives only when its matching tool_result is the
		// immediately following persisted message; buildHistory is the one
		// place that can see both halves, so unpairing is fixed here rather
		// than in each adapter.
		blocks := m.Blocks()
		var assistantBlocks []Protocol.Block
		firstText := true
		hasToolUse := false
		for _, b := range blocks {
			if b.Type == Protocol.BlockToolUse {
				hasToolUse = true
				break
			}
		}
		if hasToolUse {
			hasToolUse = nextUserCarriesResult(msgs, m.Seq, throughSeq)
		}
		for _, b := range blocks {
			switch b.Type {
			case Protocol.BlockText:
				text := b.Text
				if firstText {
					// Cleaned history stops old titled replies from priming
					// the model into repeating the pattern.
					text = StripLeadingTitle(text, agentName, "history")
					firstText = false
				}
				if text == "" && m.Text != "" {
					text = m.Text
				}
				assistantBlocks = append(assistantBlocks, Protocol.Block{Type: Protocol.BlockText, Text: text})
			case Protocol.BlockToolUse:
				if hasToolUse {
					assistantBlocks = append(assistantBlocks, b)
				}
			}
		}
		if len(assistantBlocks) == 0 && m.Text != "" {
			assistantBlocks = []Protocol.Block{{Type: Protocol.BlockText, Text: StripLeadingTitle(m.Text, agentName, "history")}}
		}
		if len(assistantBlocks) == 0 {
			continue
		}
		appendMerged(&out, Protocol.Role(m.Role), assistantBlocks)
	}
	// Token-budget trim: keep the newest exchanges that fit, never starting
	// mid round-trip — a window that opens on a tool_result carrier without
	// its tool_use is a wire-format error, not just lost context. The 40-row
	// ceiling from before stays as a hard secondary bound.
	start := 0
	used := int64(0)
	overBudget := false
	for i := len(out) - 1; i >= 0; i-- {
		t := msgTokens(out[i])
		if used+t > budgetTokens {
			start = i + 1
			overBudget = true
			break
		}
		used += t
	}
	if overBudget && start >= len(out) && len(out) > 0 {
		start = len(out) - 1 // the newest message always survives
	}
	if len(out)-start > 40 {
		start = len(out) - 40
	}
	for start < len(out) && startsOrphaned(out, start) {
		start++
	}
	if start > 0 {
		out = out[start:]
	}
	// Pair every function call with an immediate function response, and
	// vice versa. Gemini rejects a content whose functionCall is not
	// immediately followed by its functionResponse ("Please ensure that
	// function call turn comes immediately after a user turn or after a
	// function response turn"), and a window trimmed mid-round can open or
	// close on either half of a pair. Pairs are kept or dropped whole:
	// cancelling only the response of a call, or only the call of a
	// response, is what produces the error, so never do that.
	return repairHistoryTurns(out)
}

// repairHistoryTurns pairs tool calls with their responses and then makes the
// transcript open on a user turn. Gemini needs a user turn ahead of any
// function call, so a window that opens on an assistant turn is cut back to
// its first user turn; cutting the head can orphan the next half of a pair, so
// the repair runs again until the head is stable.
func repairHistoryTurns(out []Protocol.Message) []Protocol.Message {
	out = repairToolPairs(out)
	for pass := 0; pass < 8; pass++ {
		trimmed := trimLeadingModelTurns(out)
		if len(trimmed) == len(out) {
			break
		}
		out = repairToolPairs(trimmed)
	}
	return out
}

// nextUserCarriesResult reports whether the persisted user message right
// after seq — the carrier a tool round-trip persists as the next row —
// exists within the window and carries at least one tool_result. An
// assistant tool_use whose response is missing from storage (cancelled
// mid-round, carrier lost) must not enter the request: strip the call
// instead.
func nextUserCarriesResult(msgs []Storage.Message, seq, throughSeq int) bool {
	for _, m := range msgs {
		if m.Seq <= seq {
			continue
		}
		if m.Seq > throughSeq {
			return false
		}
		if m.Role != "user" {
			// The next row is not a user carrier; the round-trip did not
			// persist its result.
			return false
		}
		for _, b := range m.Blocks() {
			if b.Type == Protocol.BlockToolResult {
				return true
			}
		}
		return false
	}
	return false
}

// appendMerged coalesces consecutive same-role messages into one protocol
// message. Persisted tool round-trips put a carrier row right before the
// next user message; strict providers reject two user turns in a row, and
// tool_results ordered before the text reads correctly for every family.
func appendMerged(out *[]Protocol.Message, role Protocol.Role, blocks []Protocol.Block) {
	if n := len(*out); n > 0 && (*out)[n-1].Role == role {
		(*out)[n-1].Blocks = append((*out)[n-1].Blocks, blocks...)
		return
	}
	*out = append(*out, Protocol.Message{Role: role, Blocks: blocks})
}

// msgTokens is a deliberate rough estimate — 4 chars per token, floor of 100
// for structural overhead. It steers a budget, it doesn't bill.
func msgTokens(m Protocol.Message) int64 {
	n := 0
	for _, b := range m.Blocks {
		n += len(b.Text)
		if b.ToolCall != nil {
			n += len(b.ToolCall.Input) + len(b.ToolCall.Name)
		}
	}
	if n == 0 {
		n = 400
	}
	return int64(n/4) + 100
}

// startsOrphaned reports whether out[start] is the trailing half of a tool
// round-trip: a user message carrying only tool results whose assistant
// tool_use sat before the window. Starting there would ask the provider
// for a function response that never had a call. The newest message is
// exempt — repairToolPairs converts it rather than dropping the turn.
func startsOrphaned(out []Protocol.Message, start int) bool {
	if start >= len(out) {
		return false
	}
	if start == len(out)-1 {
		return false
	}
	return isToolCarrier(out[start])
}

// repairToolPairs keeps every function call immediately followed by its
// response and vice versa, and never merges a call and its response into the
// same content: Gemini rejects a content whose functionCall shares a turn
// with its functionResponse ("Please ensure that function call turn comes
// immediately after a user turn or after a function response turn"). A merged
// message that carries both halves is therefore split — the call stays in
// its assistant content, the response starts a new user content, and any text
// that was glued after the response becomes its own turn. Orphaned halves
// (a call with no following response, or a response with no preceding call)
// are dropped whole; a message carrying text plus an orphaned half keeps
// the text.
func repairToolPairs(out []Protocol.Message) []Protocol.Message {
	return coalesceModelTurns(repairPairsOnce(resultsFirst(out)))
}

func repairPairsOnce(out []Protocol.Message) []Protocol.Message {
	kept := make([]Protocol.Message, 0, len(out))
	for i, m := range out {
		if carriesToolUse(m) {
			if i+1 >= len(out) || !carriesToolResult(out[i+1]) {
				// A call whose response never follows (or sits in the
				// same content) is orphaned: drop it, keep any text that
				// rode along.
				if !carriesToolResult(m) {
					stripKind(m, Protocol.BlockToolUse, &kept)
				} else {
					// Call and response share one content: split them.
					kept = appendSplit(kept, m)
				}
				continue
			}
		}
		if carriesToolResult(m) && !carriesToolUse(m) {
			if len(kept) == 0 || !carriesToolUse(kept[len(kept)-1]) {
				// Orphaned response: drop it, keep any text that rode along.
				stripKind(m, Protocol.BlockToolResult, &kept)
				continue
			}
		}
		// No splits needed: keep the message when the pairing is intact.
		// If it carries both halves, split at the tool_result boundary.
		kept = appendSplit(kept, m)
	}
	// A trailing-text split can leave a second user content right after a
	// tool_result; that is the intent. Apply the same split once more to
	// contents that still mix a response with later text.
	for i := 0; i < len(kept); i++ {
		if !carriesToolResult(kept[i]) || len(kept[i].Blocks) <= 1 {
			continue
		}
		var out []Protocol.Message
		head := kept[i]
		// find the first non-tool_result block
		split := -1
		for j, b := range head.Blocks {
			if b.Type != Protocol.BlockToolResult {
				split = j
				break
			}
		}
		if split <= 0 {
			continue
		}
		out = append(out, kept[:i]...)
		out = append(out, Protocol.Message{Role: head.Role, Blocks: head.Blocks[:split]})
		out = append(out, Protocol.Message{Role: Protocol.RoleUser, Blocks: head.Blocks[split:]})
		out = append(out, kept[i+1:]...)
		kept = out
	}
	return kept
}

// coalesceModelTurns merges adjacent assistant messages. Dropping an orphaned
// tool_result carrier leaves the assistant turns on either side of it
// touching, and Gemini rejects a function call that follows a model turn
// instead of a user turn or a function response ("Please ensure that function
// call turn comes immediately after a user turn or after a function response
// turn"). Merged, the text and the call share one model turn, which is valid.
func coalesceModelTurns(in []Protocol.Message) []Protocol.Message {
	out := make([]Protocol.Message, 0, len(in))
	for _, m := range in {
		if n := len(out); n > 0 && m.Role == Protocol.RoleAssistant && out[n-1].Role == Protocol.RoleAssistant {
			blocks := make([]Protocol.Block, 0, len(out[n-1].Blocks)+len(m.Blocks))
			blocks = append(blocks, out[n-1].Blocks...)
			blocks = append(blocks, m.Blocks...)
			out[n-1].Blocks = blocks
			continue
		}
		out = append(out, m)
	}
	return out
}

// resultsFirst moves the tool_result blocks of every user message ahead of
// its text. A function response must sit directly after its function call;
// text glued in front of it would push the response into a later content and
// leave the call unanswered. The input is not mutated.
func resultsFirst(in []Protocol.Message) []Protocol.Message {
	out := make([]Protocol.Message, len(in))
	for i, m := range in {
		out[i] = m
		if m.Role != Protocol.RoleUser || !carriesToolResult(m) {
			continue
		}
		blocks := make([]Protocol.Block, 0, len(m.Blocks))
		for _, b := range m.Blocks {
			if b.Type == Protocol.BlockToolResult {
				blocks = append(blocks, b)
			}
		}
		for _, b := range m.Blocks {
			if b.Type != Protocol.BlockToolResult {
				blocks = append(blocks, b)
			}
		}
		out[i].Blocks = blocks
	}
	return out
}

// trimLeadingModelTurns drops any assistant turns at the head of the
// transcript. Gemini requires a function call turn to come immediately after
// a user turn or a function response turn, so a window that the token budget
// or the 40-row ceiling trimmed to open on an assistant message (with or
// without a tool call in it) is rejected outright. Dropping the turn costs a
// little context; sending it costs the whole request.
func trimLeadingModelTurns(in []Protocol.Message) []Protocol.Message {
	i := 0
	for i < len(in) && in[i].Role != Protocol.RoleUser {
		i++
	}
	// A leading user message that is only tool results has lost its call.
	for i < len(in) && isToolCarrier(in[i]) {
		i++
	}
	return in[i:]
}

// stripKind drops the blocks of one kind from m and appends what remains to
// kept: the text-only part of a message whose tool half was orphaned.
func stripKind(m Protocol.Message, kind Protocol.BlockType, kept *[]Protocol.Message) {
	rest := make([]Protocol.Block, 0, len(m.Blocks))
	for _, b := range m.Blocks {
		if b.Type != kind {
			rest = append(rest, b)
		}
	}
	if len(rest) == 0 {
		return
	}
	*kept = append(*kept, Protocol.Message{Role: m.Role, Blocks: rest})
}

// appendSplit appends m after splitting it at a tool_result boundary: any
// tool_result (and everything after it) becomes a new user content. A
// message with no tool_result — or with text only before it — is appended
// untouched.
func appendSplit(kept []Protocol.Message, m Protocol.Message) []Protocol.Message {
	split := -1
	for j, b := range m.Blocks {
		if b.Type == Protocol.BlockToolResult {
			split = j
			break
		}
	}
	if split <= 0 {
		// Nothing to split: the message has no tool_result, or the
		// tool_result is already the first block.
		return append(kept, m)
	}
	head := Protocol.Message{Role: m.Role, Blocks: m.Blocks[:split]}
	tail := Protocol.Message{Role: Protocol.RoleUser, Blocks: m.Blocks[split:]}
	kept = append(kept, head)
	// Everything after a tool_result in the same merged row was the user's
	// next sentence; it must be its own turn, not part of the response.
	// Split off trailing text into its own content (the text block may be
	// the first block of the tail when the tool_result was first).
	trailing := -1
	for j, b := range tail.Blocks {
		if b.Type == Protocol.BlockText {
			trailing = j
			break
		}
	}
	if trailing == 0 {
		// No tool_result before the text — the tail is pure text.
		return append(kept, tail)
	}
	if trailing > 0 {
		text := tail.Blocks[trailing:]
		tail = Protocol.Message{Role: Protocol.RoleUser, Blocks: tail.Blocks[:trailing]}
		kept = append(kept, tail)
		kept = append(kept, Protocol.Message{Role: Protocol.RoleUser, Blocks: text})
		return kept
	}
	return append(kept, tail)
}

func carriesToolUse(m Protocol.Message) bool {
	for _, b := range m.Blocks {
		if b.Type == Protocol.BlockToolUse {
			return true
		}
	}
	return false
}

func carriesToolResult(m Protocol.Message) bool {
	for _, b := range m.Blocks {
		if b.Type == Protocol.BlockToolResult {
			return true
		}
	}
	return false
}

func isToolCarrier(m Protocol.Message) bool {
	if m.Role != Protocol.RoleUser || len(m.Blocks) == 0 {
		return false
	}
	for _, b := range m.Blocks {
		if b.Type != Protocol.BlockToolResult {
			return false
		}
	}
	return true
}

// historyBudgetTokens replays at most half the model's context, leaving room
// for the system prompt, tool payloads and the reply. Unknown models fall
// back to a conservative 24k window.
func (s *Service) historyBudgetTokens(modelRef, providerID, modelID string) int64 {
	ctxLen := int64(0)
	if m, ok := s.Core.Store.Get().Get(modelRef); ok && m.Limits != nil {
		ctxLen = m.Limits.Context
	} else if rows, err := s.Core.DB.ListCustomModels(providerID); err == nil {
		for _, cm := range rows {
			if cm.ModelID == modelID {
				ctxLen = cm.ContextWindow
				break
			}
		}
	}
	if ctxLen <= 0 {
		ctxLen = 24000
	}
	b := ctxLen / 2
	if b > 60000 {
		b = 60000
	}
	return b
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

// collectWithTyping runs a model turn identically to Protocol.Collect, but
// fires a single chat:typing event when the first text token arrives so the
// frontend indicator can transition from "Thinking…" to "Typing…".
func (s *Service) collectWithTyping(ctx context.Context, runID, agentID string, req *Protocol.Request, target *Protocol.Target, trace *Router.Trace) Protocol.Result {
	src := Router.New().Run(ctx, req, target, trace)
	typingFired := false
	return Protocol.Collect(func(yield func(Protocol.Event) bool) {
		for ev := range src {
			if !typingFired && ev.Type == Protocol.EventBlockDelta && ev.Delta != nil && ev.Delta.Kind == Protocol.DeltaText {
				typingFired = true
				s.Core.EmitEvent(Shared.EventChatTyping, map[string]any{"agent_id": agentID, "run_id": runID})
			}
			if !yield(ev) {
				return
			}
		}
	})
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

// sCtx gives the location resolver its own bounded network budget without
// touching the run lifecycle.
func sCtx() context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	_ = cancel // lifetime bounded by the inner request timeout
	return ctx
}

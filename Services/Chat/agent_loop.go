package Chat

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Router"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
	"github.com/google/uuid"
)

func (s *Service) runTurn(runID string, ctx context.Context, cancel context.CancelFunc, agentID, assistantID, modelRef string, req Protocol.Request, target *Protocol.Target, profile Storage.AgentProfile, mr Storage.ModelRequest) {
	defer cancel()
	trace := &Router.Trace{}
	total := Protocol.Usage{}
	var result Protocol.Result
	reachedEnd := false
	rounds := 0
	extraTries := 0
	// wroteArtifact turns on once any round called write_artifact. From then on
	// the turn's visible text is checked against the stored documents so a model
	// that pastes the document into the chat as well cannot leak it.
	wroteArtifact := false

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
		result.Message = dropUnnamedCalls(result.Message)
		req.Messages = append(req.Messages, result.Message)
		loadedMCP := map[string]bool{}
		roundResults := make([]Protocol.Block, 0, len(calls))
		for _, call := range calls {
			if call.Name == writeArtifactToolDef.Name {
				wroteArtifact = true
			}
			if call.Name == mcpLoadToolName {
				serverID, defs, block := s.runMCPLoad(call, loadedMCP)
				roundResults = append(roundResults, block)
				// The schemas belong to the next model round, not this
				// one; a repeat load must not stack a second copy.
				if len(defs) > 0 && !loadedMCP[serverID] {
					loadedMCP[serverID] = true
					req.Tools = append(req.Tools, defs...)
				}
				continue
			}
			roundResults = append(roundResults, s.runTool(ctx, runID, agentID, profile.AgentName, call))
		}
		if len(roundResults) > 0 {
			req.Messages = append(req.Messages, Protocol.Message{Role: Protocol.RoleUser, Blocks: roundResults})
			persisted := result.Message
			if wroteArtifact {
				persisted.Blocks, _ = s.withoutArtifactEcho(agentID, mr.StartedAt, persisted.Blocks, "round")
			}
			s.persistRound(agentID, modelRef, persisted, roundResults)
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
	if wroteArtifact && msgStatus == "complete" {
		var dropped int
		blocks, dropped = s.withoutArtifactEcho(agentID, mr.StartedAt, blocks, "reply")
		// The reply was only the document. Leave one line so the turn does not end
		// on a card with no words, which GetChat would otherwise hide as empty.
		if dropped > 0 && strings.TrimSpace(flattenText(blocks)) == "" {
			blocks = append(blocks, Protocol.Block{Type: Protocol.BlockText, Text: artifactEchoFallback})
		}
	}
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

func toolCalls(msg Protocol.Message) []Protocol.ToolCall {
	var calls []Protocol.ToolCall
	for _, b := range msg.Blocks {
		if b.Type == Protocol.BlockToolUse && b.ToolCall != nil && b.ToolCall.Name != "" {
			calls = append(calls, *b.ToolCall)
		}
	}
	return calls
}

// dropUnnamedCalls removes tool_use blocks that carry no tool name. toolCalls
// never runs them, so they get no result; left in the assistant message they
// would be replayed (and persisted) as calls that nothing answered, which
// providers reject — Gemini because the call and response counts differ, and
// a nameless call is invalid on its own.
func dropUnnamedCalls(msg Protocol.Message) Protocol.Message {
	kept := make([]Protocol.Block, 0, len(msg.Blocks))
	for _, b := range msg.Blocks {
		if b.Type == Protocol.BlockToolUse && (b.ToolCall == nil || b.ToolCall.Name == "") {
			continue
		}
		kept = append(kept, b)
	}
	msg.Blocks = kept
	return msg
}

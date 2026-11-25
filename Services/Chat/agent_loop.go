package Chat

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/anyscapelabs/argus/Models"
	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Router"
	"github.com/anyscapelabs/argus/Services/MCP"
	"github.com/anyscapelabs/argus/Services/Memory"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
	"github.com/google/uuid"
)

func (s *Service) runTurn(runID string, ctx context.Context, cancel context.CancelFunc, agentID, assistantID, modelRef string, req Protocol.Request, target *Protocol.Target, profile Storage.AgentProfile, mr Storage.ModelRequest, envelope *runEnvelope) {
	defer cancel()
	trace := &Router.Trace{}
	total := Protocol.Usage{}
	var result Protocol.Result
	reachedEnd := false
	rounds := 0
	extraTries := 0
	opaqueTries := 0
	toolCallRecoveryTries := 0
	// wroteArtifact turns on once any round called write_artifact. From then on
	// the turn's visible text is checked against the stored documents so a model
	// that pastes the document into the chat as well cannot leak it.
	wroteArtifact := false
	discoveredTools := map[string]bool{}
	// failedToolCalls tracks inner tool errors in this run so a subsequent
	// successful retry can automatically record a compact recovery lesson.
	failedToolCalls := map[string]string{}

	// A long unattended run keeps every round in req.Messages. The budget lets
	// the loop elide old tool output instead of dying on a context error, which
	// is what made a small round cap necessary in the first place.
	providerID, modelID := Models.SplitRef(modelRef)
	runBudget := s.runContextBudget(modelRef, providerID, modelID, &req)
	var lastSig uint64
	repeats := 0
	stuck := false
	stopReason := ""
	// Tool-call, no-progress and quota budgets close the gap the signature
	// detector leaves open: a turn that keeps varying its failing calls makes
	// no forward motion but never repeats a signature.
	toolCallCount := 0
	noProgressRounds := 0
	quotaBlocked := false
	quotaHits := map[string]int{}

	// The request is stateless, so a retry simply re-runs
	// the turn; tools already executed earlier are not re-executed.
	runModelTurn := func() Protocol.Result {
		// Tell the frontend a model turn is starting. Between tool rounds nothing
		// else announces it, so without this the indicator went blank while the
		// model worked out its next step.
		s.Core.EmitEvent(Shared.EventChatPhase, map[string]any{"agent_id": agentID, "run_id": runID, "phase": "thinking"})
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
	for round := 0; round < maxToolRounds && !reachedEnd && !stuck; round++ {
		runContext := Shared.AgentRun(ctx)
		if inbox, err := DrainRoundInbox(s.Core.DB.SQL(), agentID, runContext.ThreadID); err == nil && len(inbox) > 0 {
			req.Messages = append(req.Messages, Protocol.Message{
				Role:   Protocol.RoleUser,
				Blocks: []Protocol.Block{{Type: Protocol.BlockText, Text: strings.Join(inbox, "\n\n")}},
			})
		}
		if n := compactRunMessages(req.Messages, runBudget); n > 0 {
			log.Printf("chat: run %s compacted %d earlier messages to stay inside the context window", runID, n)
		}
		result = runModelTurn()

		if ctx.Err() != nil || result.Error != nil {
			reachedEnd = true
			break
		}

		calls := toolCalls(result.Message)
		if len(calls) == 0 && needsToolCallRecovery(result) && ctx.Err() == nil && toolCallRecoveryTries < maxToolCallRecovery {
			// The model asked for a tool but the stream was cut before a usable
			// call arrived (nameless/empty tool_use block). The stream-end guard
			// cannot catch this because a finish reason was present, so without
			// a resend the turn is saved as an empty "complete" reply.
			toolCallRecoveryTries++
			if clean := dropUnnamedCalls(result.Message); len(clean.Blocks) > 0 {
				req.Messages = append(req.Messages, clean)
			}
			req.Messages = append(req.Messages, Protocol.Message{
				Role:   Protocol.RoleUser,
				Blocks: []Protocol.Block{{Type: Protocol.BlockText, Text: toolCallRecoveryDirective}},
			})
			result = runModelTurn()
			if ctx.Err() != nil || result.Error != nil {
				reachedEnd = true
				break
			}
			calls = toolCalls(result.Message)
		}
		if len(calls) == 0 {
			reachedEnd = true
			break
		}

		// Replaying the assistant turn, then appending one user message
		// carrying every tool_result block, is the shape all three adapter
		// families translate back into their own wire format.
		result.Message = dropUnnamedCalls(result.Message)
		req.Messages = append(req.Messages, result.Message)
		roundResults := make([]Protocol.Block, 0, len(calls))
		for _, call := range calls {
			innerName := call.Name
			var block Protocol.Block
			if isGatewayMetaTool(call.Name) {
				block, innerName = s.runTypedGatewayTool(ctx, runID, agentID, profile.AgentName, discoveredTools, call)
			} else {
				block = s.runTool(ctx, runID, agentID, profile.AgentName, call)
			}
			if !block.IsError && (innerName == writeArtifactToolDef.Name || innerName == createSlidesToolDef.Name) {
				wroteArtifact = true
			}
			if innerName != "" && strings.Contains(innerName, "__") {
				if block.IsError {
					failedToolCalls[innerName] = block.Text
				} else if prevErr, hadErr := failedToolCalls[innerName]; hadErr {
					delete(failedToolCalls, innerName)
					if s.Core != nil && s.Core.DB != nil {
						serverID := ""
						if target, err := ResolveAgentMCPTool(s.Core.DB.SQL(), agentID, innerName); err == nil {
							serverID = target.ServerID
						}
						Memory.RecordToolLesson(s.Core.DB, agentID, serverID, innerName, prevErr, call.Input)
					}
				}
			}
			if block.IsError && isQuotaRateLimitError(block.Text) {
				// One quota failure may be transient; a second from the same
				// tool means the cap will not lift this turn. One retry, then
				// stop — 27 calls against a spent daily quota is the incident
				// this guard exists to prevent.
				quotaHits[innerName]++
				if quotaHits[innerName] >= 2 {
					quotaBlocked = true
				}
			}
			roundResults = append(roundResults, block)
		}
		if len(roundResults) > 0 {
			req.Messages = append(req.Messages, Protocol.Message{Role: Protocol.RoleUser, Blocks: roundResults})
			persisted := redactPersistedToolInputs(result.Message)
			if wroteArtifact {
				persisted.Blocks, _ = s.withoutArtifactEcho(agentID, mr.StartedAt, persisted.Blocks, "round")
			}
			s.persistRound(agentID, modelRef, persisted, roundResults)
		}
		if req.ToolChoice.Mode == Protocol.ToolChoiceTool {
			if s.firstRunPending(agentID) && s.firstRunNameAnswer(agentID) != "" {
				req.ToolChoice = Protocol.ToolChoice{Mode: Protocol.ToolChoiceTool, Name: setAgentNameToolDef.Name}
			} else if !s.firstRunPending(agentID) {
				req.Tools = gatewayToolDefs()
				req.ToolChoice = Protocol.ToolChoice{Mode: Protocol.ToolChoiceAuto}
			}
		}
		rounds++
		if sig := callSignature(calls); sig == lastSig {
			repeats++
			if repeats+1 >= stuckRoundLimit {
				stuck, stopReason = true, "repeating"
			}
		} else {
			lastSig, repeats = sig, 0
		}
		toolCallCount += len(calls)
		allFailed := len(roundResults) > 0
		for _, b := range roundResults {
			if !b.IsError {
				allFailed = false
				break
			}
		}
		if allFailed {
			noProgressRounds++
		} else {
			noProgressRounds = 0
		}
		if !stuck {
			switch {
			case quotaBlocked:
				stuck, stopReason = true, "rate_limited"
			case toolCallCount >= maxToolCallsPerTurn:
				stuck, stopReason = true, "tool_call_budget"
			case noProgressRounds >= noProgressRoundLimit:
				stuck, stopReason = true, "no_progress"
			}
		}
	}

	if !reachedEnd && ctx.Err() == nil && result.Error == nil {
		// Stopped early with the model still mid-plan: the runaway backstop,
		// a repeating loop, the tool-call budget, aimless rounds, or a
		// connector that hit its quota. Don't end on the limit itself: give
		// it one tools-off turn to summarize honestly what ran and what did
		// not, naming why the run stopped.
		directive := wrapUpDirective
		switch stopReason {
		case "repeating":
			directive += " The same tool calls were repeating without progress."
		case "tool_call_budget":
			directive += " The turn's tool-call budget ran out."
		case "no_progress":
			directive += " Repeated tool attempts made no progress."
		case "rate_limited":
			directive += " A connector reported a rate limit or exhausted quota; further calls would keep failing."
		}
		req.Tools = nil
		req.Messages = append(req.Messages, Protocol.Message{
			Role:   Protocol.RoleUser,
			Blocks: []Protocol.Block{{Type: Protocol.BlockText, Text: directive}},
		})
		if wrap := runModelTurn(); wrap.Error == nil && len(toolCalls(wrap.Message)) == 0 {
			result = wrap
		} else {
			result.Message.Blocks = append(stripToolUses(result.Message.Blocks), Protocol.Block{
				Type: Protocol.BlockText,
				Text: "Stopped before finishing. The actions listed above ran; nothing after them did. Reply \"continue\" to pick up from here.",
			})
		}
	}

	// Opaque provider failures are transient more often than they are our
	// fault, so a single retry gets a second chance before the user sees
	// the error. A bad-input refusal that names a problem we can fix would
	// repeat identically, so only the generic "provider returned error"
	// class is considered retryable here — and only once, so a real bug
	// still surfaces instead of spinning. This stacks on top of the normal
	// retryable attempts because the gateway masks upstream 5xx as an opaque
	// bad_input error over HTTP 200, which the status-based loop cannot see.
	if result.Error != nil && ctx.Err() == nil && opaqueTries < maxOpaqueRetries && messageIsOpaqueProviderError(result.Error) {
		opaqueTries++
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
	if wroteArtifact {
		// The artifact card is the document's delivery surface. A generated line
		// is the only reliable boundary: fuzzy matching cannot catch every
		// reformatted or earlier-round copy of model-authored content.
		blocks = []Protocol.Block{{Type: Protocol.BlockText, Text: artifactEchoFallback}}
	}
	text := flattenText(blocks)
	// If the run ended in error but produced no visible text (e.g. a 502 that
	// was masked by the gateway and exhausted all retries), the assistant row
	// would be finalised with empty blocks — the frontend gets the event but
	// has nothing to render, so the user sees a silent blank. Surface the
	// friendly error string so the row is never completely invisible.
	if result.Error != nil && result.Error.Code != Protocol.CodeCancelled && strings.TrimSpace(text) == "" {
		errText := publicModelError
		if result.Error.Code == Protocol.CodeRateLimited || Protocol.IsUsageLimit(result.Error.Code) {
			errText = result.Error.Message
		}
		blocks = []Protocol.Block{{Type: Protocol.BlockText, Text: errText}}
		text = errText
	}
	// The reply row was reserved before any round existed; move it behind
	// the round pairs so history replays in execution order.
	if rounds > 0 {
		if finalSeq, seqErr := s.Core.DB.NextSeq(agentID); seqErr != nil {
			log.Printf("chat: could not reserve final seq for run %s: %v", runID, seqErr)
		} else if err := s.Core.DB.ReseqMessage(assistantID, finalSeq); err != nil {
			log.Printf("chat: could not reseq reply %s: %v", assistantID, err)
		}
	}
	providerStopReason := result.Stop.Raw
	errorMessage := ""
	errorCode := ""
	if result.Error != nil {
		errorMessage = result.Error.Message
		errorCode = result.Error.Code
	}
	if err := s.Core.DB.FinalizeMessage(assistantID, blocks, text, msgStatus, providerStopReason, modelRef, errorMessage, errorCode, nowUnixMs()); err != nil {
		log.Printf("chat: could not finalize message %s: %v", assistantID, err)
	}

	finalMR := mr
	finalMR.Attempt = 1 + extraTries + opaqueTries
	finalMR.Status = reqStatus
	if result.Error != nil {
		finalMR.ErrorCode = result.Error.Code
		finalMR.ErrorMessage = result.Error.Message
		finalMR.ErrorRaw = result.Error.Raw
		if result.Error.Retryable {
			finalMR.ErrorRetryable = true
		}
	}
	finalMR.StopReason = providerStopReason
	finalMR.HTTPStatus = trace.HTTPStatus
	finalMR.ProviderRequestID = trace.ProviderRequestID
	if trace.FirstByteAt > 0 {
		finalMR.FirstByteAt = trace.FirstByteAt
	}
	if trace.FirstTokenAt > 0 {
		finalMR.FirstTokenAt = trace.FirstTokenAt
	}
	finalMR.CompletedAt = nowUnixMs()

	usage := total
	hasUsage := usage.InputTokens != 0 || usage.OutputTokens != 0 || usage.CacheReadTokens != 0 || usage.CacheWriteTokens != 0 || usage.ReasoningTokens != 0
	if hasUsage {
		finalMR.UsageSource = "provider"
		finalMR.InputTokens = &usage.InputTokens
		finalMR.CacheReadTokens = &usage.CacheReadTokens
		finalMR.CacheWriteTokens = &usage.CacheWriteTokens
		finalMR.OutputTokens = &usage.OutputTokens
		finalMR.ReasoningTokens = &usage.ReasoningTokens
		finalMR.CostUSD = s.computeCost(usage, modelRef)
	} else {
		finalMR.UsageSource = "missing"
	}
	raw, _ := json.Marshal(usage)
	finalMR.UsageRawJSON = string(raw)

	if err := s.Core.DB.FinalizeModelRequest(finalMR); err != nil {
		log.Printf("chat: could not finalize model request %s: %v", finalMR.ID, err)
	}

	if envelope != nil && envelope.Delegated != nil {
		delegationStatus := "completed"
		report := text
		if reqStatus != "success" {
			delegationStatus = "failed"
			if errorMessage != "" {
				report = "Delegated run failed: " + errorMessage
			} else if report == "" {
				report = "Delegated run did not complete."
			}
		}
		if err := FinalizeDelegatedRun(s.Core.DB.SQL(), *envelope.Delegated, report, delegationStatus); err != nil {
			log.Printf("chat: could not finalize delegated request %s: %v", envelope.Delegated.ID, err)
		} else if envelope.Delegated.ReportBack {
			s.WakeAgent(envelope.Delegated.FromAgentID)
		}
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
	s.WakeAgent(agentID)
}

// persistRound writes one completed tool round-trip to the chat: the
// assistant row that requested the tools, then a user carrier row holding the
// results. Carriers have no visible text and are hidden by GetChat, but
// buildHistory replays them, which is exactly what makes past actions visible
// to future turns.
//
// Round rows are always tool_round, whatever launched the run. Inheriting the
// run's kind (steering, scheduled, delegated) misfiled the model's own call:
// buildHistory reads a steering row as user text, so it dropped the call and
// the result that pairs with it, and the round vanished from every later turn.
func (s *Service) persistRound(agentID, modelRef string, assistantMsg Protocol.Message, results []Protocol.Block) {
	roundKind := Storage.MessageKindToolRound
	carrierKind := roundKind
	for _, block := range results {
		if !block.IsError && block.FileWrite != nil {
			carrierKind = Storage.MessageKindFileWrite
			break
		}
	}
	rows := []Storage.Message{
		{ID: newID(), AgentID: agentID, Role: "assistant", Kind: roundKind,
			BlocksJSON: mustMarshal(assistantMsg.Blocks), Text: flattenText(assistantMsg.Blocks),
			Status: "complete", ModelRef: modelRef, CreatedAt: nowUnixMs(), CompletedAt: nowUnixMs()},
		{ID: newID(), AgentID: agentID, Role: "user", Kind: carrierKind,
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
	}
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

// needsToolCallRecovery reports a stream that stopped on a tool_calls finish
// reason without leaving a single usable call — the signature of a response
// cut off before the tool name arrived.
func needsToolCallRecovery(res Protocol.Result) bool {
	return res.Error == nil && res.Stop.Reason == Protocol.StopToolUse && len(toolCalls(res.Message)) == 0
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

type ToolSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"input_schema"`
}

// RunGuard carries a run's safety context across rounds: where the run sits
// in a delegation chain and which call shapes it has already repeated. Every
// round observes the guard first, so both ways an unattended loop escalates
// out of control — an endless delegation relay and a stuck toolcall loop —
// are bounded in one place.
type RunGuard struct {
	AgentID  string
	ThreadID string
	BaseHops int

	maxHops int
	calls   map[string]int
	repeats int
	lastSig string
}

// BaseHops is inherited from the delegating request (zero for a user-initiated
// run); every delegation this
// run performs spends one hop.
func NewRunGuard(agentID, threadID string, baseHops int) *RunGuard {
	return &RunGuard{
		AgentID:  agentID,
		ThreadID: threadID,
		BaseHops: baseHops,
		maxHops:  maxDelegationHops,
		calls:    map[string]int{},
	}
}

// HopsLeft reports how many delegations this run may still perform. At zero
// the loop's delegation tools refuse, pushing the model to finish or
// escalate to the user instead of bouncing the task onward forever.
func (g *RunGuard) HopsLeft() int {
	return g.maxHops - g.BaseHops
}

// ObserveStuck tracks consecutive rounds issuing the identical call set, the
// signature the Wails loop already watches. The Scheduler-driven loop uses
// Stuck to stop early rather than burning rounds to the limit.
func (g *RunGuard) ObserveStuck(callNames []string) {
	sig := callSignatureFromNames(callNames)
	if sig == g.lastSig {
		g.repeats++
	} else {
		g.lastSig, g.repeats = sig, 0
	}
}

func (g *RunGuard) Stuck() bool {
	return g.repeats+1 >= stuckRoundLimit
}

func callSignatureFromNames(names []string) string {
	h := sha256.Sum256([]byte(strings.Join(names, "|")))
	return hex.EncodeToString(h[:4])
}

// DrainRoundInbox returns the queued messages addressed to this agent,
// formatted for injection at a round boundary, and marks them delivered. The
// loop calls it before every model turn: mid-run steering and teammate
// reports must be able to change the next round's plan without racing the
// loop's history writes.
func DrainRoundInbox(db *sql.DB, agentID, threadID string) ([]string, error) {
	msgs, err := Storage.DrainQueuedAgentMessagesForThread(db, agentID, threadID)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, m := range msgs {
		var line string
		switch m.Kind {
		case "steer":
			line = "[Mid-run instruction from the user] " + m.Body
		case "report":
			line = fmt.Sprintf("[Report from teammate %s] %s", m.FromAgentID, m.Body)
		case "request":
			line = fmt.Sprintf("[Task request from teammate %s] %s", m.FromAgentID, m.Body)
		default:
			line = fmt.Sprintf("[%s from %s] %s", m.Kind, m.FromAgentID, m.Body)
		}
		out = append(out, line)
		if err := Storage.MarkAgentMessageStatus(db, m.ID, "completed"); err != nil {
			log.Printf("chat: could not complete delivered message %s: %v", m.ID, err)
		}
	}
	return out, nil
}

// FinalizeDelegatedRun closes the loop on one delegation: the request that
// started this run is marked handled, and — when the delegator asked for a
// report — the run's final text is enqueued back to them so the next round
// boundary on their side picks it up.
func FinalizeDelegatedRun(db *sql.DB, request Storage.AgentMessage, report, status string) error {
	if request.ID != "" {
		if err := Storage.MarkAgentMessageStatus(db, request.ID, status); err != nil {
			return err
		}
	}
	if !request.ReportBack || strings.TrimSpace(report) == "" {
		return nil
	}
	return Storage.EnqueueAgentMessage(db, Storage.AgentMessage{
		ID:              "rep_" + uuid.NewString(),
		ThreadID:        request.ThreadID,
		ParentMessageID: request.ID,
		FromAgentID:     request.ToAgentID,
		ToAgentID:       request.FromAgentID,
		Kind:            "report",
		Body:            strings.TrimSpace(report),
		Status:          "queued",
		Hops:            request.Hops,
	})
}

func TeamToolSpecs() []ToolSpec {
	defs := []Protocol.ToolDef{listAgentsToolDef, messageAgentToolDef, saveAgentNoteToolDef, setMyRoleToolDef}
	out := make([]ToolSpec, 0, len(defs))
	for _, def := range defs {
		out = append(out, ToolSpec{Name: def.Name, Description: def.Description, InputSchema: def.Schema})
	}
	return out
}

func isTeamTool(name string) bool {
	switch name {
	case "list_agents", "message_agent", "save_agent_note", "set_my_role":
		return true
	}
	return false
}

// Errors come back as readable tool text rather than a thrown failure: the
// model is expected to read the reason and course-correct on the next round,
// exactly as it does for MCP tier refusals.
func DispatchBuiltInTeamTool(
	ctx context.Context,
	db *sql.DB,
	callerID, threadID string,
	hops int,
	name, rawArgs string,
	wakeFn func(agentID string),
) (string, error) {
	switch name {
	case "list_agents":
		return HandleListAgents(db, callerID)
	case "message_agent":
		return HandleMessageAgent(ctx, db, callerID, threadID, hops, rawArgs, wakeFn)
	case "save_agent_note":
		var in struct {
			Key   string `json:"key"`
			Value string `json:"value"`
			Scope string `json:"scope"`
		}
		if err := json.Unmarshal([]byte(rawArgs), &in); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		return HandleSaveAgentNote(db, callerID, threadID, in.Key, in.Value, in.Scope)
	case "set_my_role":
		var in struct {
			Role         string `json:"role"`
			Instructions string `json:"instructions"`
		}
		if err := json.Unmarshal([]byte(rawArgs), &in); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		return HandleSetAgentRole(db, callerID, in.Role, in.Instructions)
	}
	return "", fmt.Errorf("unknown team tool %q", name)
}

// ExecuteGuardedTool runs one tool call for the delegation loop with the
// access tier checked before anything executes. Built-in team tools come
// first, then projected MCP connector tools through the gateway, then a
// refusal the model can read. Everything else is out of scope for an
// unattended run: the scheduler loop only promises what these surfaces cover.
func ExecuteGuardedTool(
	ctx context.Context,
	db *sql.DB,
	mgr *MCP.Manager,
	agentID, threadID string,
	hops int,
	name, rawArgs string,
	wakeFn func(agentID string),
) (string, error) {
	if isTeamTool(name) {
		return DispatchBuiltInTeamTool(ctx, db, agentID, threadID, hops, name, rawArgs, wakeFn)
	}
	if strings.Contains(name, "__") {
		return ExecuteAgentMCPTool(ctx, db, mgr, agentID, name, rawArgs)
	}
	return "", fmt.Errorf("tool %q is not available to unattended agent runs", name)
}

type LoopMessage struct {
	Role    string // "user" | "assistant" | "tool"
	Content string
}

type ToolCall struct {
	Name string
	Args string
}

type ModelResponse struct {
	Text         string
	ToolCalls    []ToolCall
	InputTokens  int
	OutputTokens int
}

type ModelTurn func(ctx context.Context, cfg LoopConfig, messages []LoopMessage) (ModelResponse, error)

// Returned errors become readable tool results, so a provider hiccup or
// a tier refusal becomes context the model can react to.
type ToolExecutor func(ctx context.Context, cfg LoopConfig, call ToolCall) (string, error)

type LoopConfig struct {
	AgentID   string
	ThreadID  string
	System    string
	Opening   string
	BaseHops  int
	MaxRounds int
	Tools     []ToolSpec

	Turn ModelTurn
	Exec ToolExecutor
}

func RunAgentLoop(ctx context.Context, db *sql.DB, cfg LoopConfig) (ModelResponse, error) {
	started := time.Now()
	if cfg.MaxRounds <= 0 {
		cfg.MaxRounds = maxToolRounds
	}

	guard := NewRunGuard(cfg.AgentID, cfg.ThreadID, cfg.BaseHops)
	messages := make([]LoopMessage, 0, 16)
	if opening := strings.TrimSpace(cfg.Opening); opening != "" {
		messages = append(messages, LoopMessage{Role: "user", Content: opening})
	}
	var usageIn, usageOut, toolCalls int
	var rounds int
	var noProgressRounds int
	var quotaBlocked bool
	quotaHits := map[string]int{}
	stopStatus := ""

	for round := 0; round < cfg.MaxRounds; round++ {
		rounds = round + 1
		if ctx.Err() != nil {
			break
		}

		if db != nil {
			if inbox, err := DrainRoundInbox(db, cfg.AgentID, cfg.ThreadID); err == nil && len(inbox) > 0 {
				messages = append(messages, LoopMessage{Role: "user", Content: strings.Join(inbox, "\n\n")})
			}
		}

		resp, err := cfg.Turn(ctx, cfg, messages)
		usageIn += resp.InputTokens
		usageOut += resp.OutputTokens
		if err != nil {
			recordLoopMetric(db, cfg, started, rounds, usageIn, usageOut, toolCalls, "failed")
			return resp, err
		}

		if len(resp.ToolCalls) == 0 {
			recordLoopMetric(db, cfg, started, rounds, usageIn, usageOut, toolCalls, "completed")
			return resp, nil
		}

		messages = append(messages, LoopMessage{Role: "assistant", Content: resp.Text})
		var names []string
		for _, call := range resp.ToolCalls {
			names = append(names, call.Name)
		}
		guard.ObserveStuck(names)

		roundFailed := true
		for _, call := range resp.ToolCalls {
			toolCalls++
			text, err := cfg.Exec(ctx, cfg, call)
			if err != nil {
				text = "Tool error: " + err.Error()
				if isQuotaRateLimitError(text) {
					// Same rule as the interactive loop: one retry, then
					// stop — a daily quota will not lift mid-run.
					quotaHits[call.Name]++
					if quotaHits[call.Name] >= 2 {
						quotaBlocked = true
					}
				}
			} else {
				roundFailed = false
			}
			messages = append(messages, LoopMessage{Role: "tool", Content: text})
		}
		if roundFailed {
			noProgressRounds++
		} else {
			noProgressRounds = 0
		}

		if toolCalls >= maxToolCallsPerTurn {
			stopStatus = "stopped_call_budget"
			break
		}
		if quotaBlocked {
			stopStatus = "stopped_rate_limited"
			break
		}
		if guard.Stuck() {
			stopStatus = "stopped_repeating"
			break
		}
		if noProgressRounds >= noProgressRoundLimit && stopStatus == "" {
			stopStatus = "stopped_no_progress"
			break
		}
	}

	status := "completed"
	if ctx.Err() != nil {
		status = "cancelled"
	} else if stopStatus != "" {
		status = stopStatus
	} else if guard.Stuck() {
		status = "stopped_repeating"
	}
	recordLoopMetric(db, cfg, started, rounds, usageIn, usageOut, toolCalls, status)
	return ModelResponse{}, nil
}

// recordLoopMetric persists one unattended run's metrics, ignoring failure:
// metrics are observability, not correctness.
func recordLoopMetric(db *sql.DB, cfg LoopConfig, started time.Time, rounds, in, out, calls int, status string) {
	if db == nil {
		return
	}
	_ = Storage.RecordAgentRunMetric(db, Storage.AgentRunMetric{
		RunID:        "run_" + uuid.NewString(),
		AgentID:      cfg.AgentID,
		ThreadID:     cfg.ThreadID,
		Rounds:       rounds,
		InputTokens:  in,
		OutputTokens: out,
		ToolCalls:    calls,
		DurationMs:   time.Since(started).Milliseconds(),
		Status:       status,
	})
}

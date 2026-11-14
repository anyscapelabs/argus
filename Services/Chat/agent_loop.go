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

func (s *Service) runTurn(runID string, ctx context.Context, cancel context.CancelFunc, agentID, assistantID, modelRef, internalKind string, req Protocol.Request, target *Protocol.Target, profile Storage.AgentProfile, mr Storage.ModelRequest) {
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
		if n := compactRunMessages(req.Messages, runBudget); n > 0 {
			log.Printf("chat: run %s compacted %d earlier messages to stay inside the context window", runID, n)
		}
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
						Memory.RecordToolLesson(s.Core.DB, agentID, innerName, prevErr, call.Input)
					}
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
			s.persistRound(agentID, modelRef, internalKind, persisted, roundResults)
		}
		rounds++
		if sig := callSignature(calls); sig == lastSig {
			repeats++
			if repeats+1 >= stuckRoundLimit {
				stuck = true
			}
		} else {
			lastSig, repeats = sig, 0
		}
	}

	if !reachedEnd && ctx.Err() == nil && result.Error == nil {
		// Stopped early with the model still mid-plan: either the runaway
		// backstop or a loop repeating the same calls. Don't end on the
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
				Text: "Stopped before finishing. The actions listed above ran; nothing after them did. Reply \"continue\" to pick up from here.",
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
	if wroteArtifact {
		// The artifact card is the document's delivery surface. A generated line
		// is the only reliable boundary: fuzzy matching cannot catch every
		// reformatted or earlier-round copy of model-authored content.
		blocks = []Protocol.Block{{Type: Protocol.BlockText, Text: artifactEchoFallback}}
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
	errorCode := ""
	if result.Error != nil {
		errorMessage = result.Error.Message
		errorCode = result.Error.Code
	}
	if err := s.Core.DB.FinalizeMessage(assistantID, blocks, text, msgStatus, stopReason, modelRef, errorMessage, errorCode, nowUnixMs()); err != nil {
		log.Printf("chat: could not finalize message %s: %v", assistantID, err)
	}

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
func (s *Service) persistRound(agentID, modelRef, internalKind string, assistantMsg Protocol.Message, results []Protocol.Block) {
	aid, _ := uuid.NewV7()
	uid, _ := uuid.NewV7()
	roundKind := internalKind
	if roundKind == "" {
		roundKind = Storage.MessageKindToolRound
	}
	carrierKind := roundKind
	for _, block := range results {
		if !block.IsError && block.FileWrite != nil {
			carrierKind = Storage.MessageKindFileWrite
			break
		}
	}
	rows := []Storage.Message{
		{ID: aid.String(), AgentID: agentID, Role: "assistant", Kind: roundKind,
			BlocksJSON: mustMarshal(assistantMsg.Blocks), Text: flattenText(assistantMsg.Blocks),
			Status: "complete", ModelRef: modelRef, CreatedAt: nowUnixMs(), CompletedAt: nowUnixMs()},
		{ID: uid.String(), AgentID: agentID, Role: "user", Kind: carrierKind,
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
	msgs, err := Storage.DrainQueuedAgentMessages(db, agentID)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, m := range msgs {
		if threadID != "" && m.ThreadID != threadID && m.Kind != "steer" {
			// A message bound to another thread stays queued for that
			// thread's own run.
			if err := Storage.MarkAgentMessageStatus(db, m.ID, "queued"); err != nil {
				log.Printf("chat: could not requeue cross-thread message %s: %v", m.ID, err)
			}
			continue
		}
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
	}
	return out, nil
}

// FinalizeDelegatedRun closes the loop on one delegation: the request that
// started this run is marked handled, and — when the delegator asked for a
// report — the run's final text is enqueued back to them so the next round
// boundary on their side picks it up.
func FinalizeDelegatedRun(db *sql.DB, requestID, fromAgentID, toAgentID, report, status string) error {
	if requestID != "" {
		if err := Storage.MarkAgentMessageStatus(db, requestID, status); err != nil {
			return err
		}
	}
	if status != "completed" || strings.TrimSpace(report) == "" {
		return nil
	}
	return Storage.EnqueueAgentMessage(db, Storage.AgentMessage{
		ID:          "rep_" + uuid.NewString(),
		FromAgentID: toAgentID,
		ToAgentID:   fromAgentID,
		Kind:        "report",
		Body:        strings.TrimSpace(report),
		Status:      "queued",
	})
}

func TeamToolSpecs() []ToolSpec {
	return []ToolSpec{
		{
			Name:        "list_agents",
			Description: "List the teammate agents you may delegate tasks to, with their handles and specialties.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{}}`),
		},
		{
			Name:        "message_agent",
			Description: "Delegate a task to a teammate agent by handle. Set report_back true to receive their report at a later round boundary.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"to":{"type":"string","description":"Teammate @handle"},"task":{"type":"string"},"report_back":{"type":"boolean"},"urgent":{"type":"boolean"}},"required":["to","task"]}`),
		},
		{
			Name:        "save_agent_note",
			Description: "Persist a durable working-memory note for yourself across runs. An empty value deletes the note.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"key":{"type":"string"},"value":{"type":"string"}},"required":["key"]}`),
		},
	}
}

func isTeamTool(name string) bool {
	switch name {
	case "list_agents", "message_agent", "save_agent_note":
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
		}
		if err := json.Unmarshal([]byte(rawArgs), &in); err != nil {
			return "", fmt.Errorf("invalid arguments: %w", err)
		}
		return HandleSaveAgentNote(db, callerID, in.Key, in.Value)
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
	BaseHops  int
	MaxRounds int

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
	var usageIn, usageOut, toolCalls int
	var rounds int

	for round := 0; round < cfg.MaxRounds; round++ {
		rounds = round + 1
		if ctx.Err() != nil {
			break
		}

		if inbox, err := DrainRoundInbox(db, cfg.AgentID, cfg.ThreadID); err == nil && len(inbox) > 0 {
			messages = append(messages, LoopMessage{Role: "user", Content: strings.Join(inbox, "\n\n")})
		}

		resp, err := cfg.Turn(ctx, cfg, messages)
		if err != nil {
			usageIn += resp.InputTokens
			usageOut += resp.OutputTokens
			recordLoopMetric(db, cfg, started, rounds, usageIn, usageOut, toolCalls, "failed")
			return resp, err
		}
		usageIn += resp.InputTokens
		usageOut += resp.OutputTokens

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

		for _, call := range resp.ToolCalls {
			toolCalls++
			text, err := cfg.Exec(ctx, cfg, call)
			if err != nil {
				text = "Tool error: " + err.Error()
			}
			messages = append(messages, LoopMessage{Role: "tool", Content: text})
		}

		if guard.Stuck() {
			break
		}
	}

	status := "completed"
	if ctx.Err() != nil {
		status = "cancelled"
	} else if guard.Stuck() {
		status = "stopped_repeating"
	}
	recordLoopMetric(db, cfg, started, rounds, usageIn, usageOut, toolCalls, status)
	return ModelResponse{}, nil
}

// recordLoopMetric persists one unattended run's metrics, ignoring failure:
// metrics are observability, not correctness.
func recordLoopMetric(db *sql.DB, cfg LoopConfig, started time.Time, rounds, in, out, calls int, status string) ModelResponse {
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
	return ModelResponse{}
}

package Chat

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"

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

// maxToolRounds is a runaway backstop for the agent loop (model turn + tool
// execution pairs), not a work budget. It used to be 12, which ended any
// multi-step coding or ops task mid-plan. The real work budgets are
// maxToolCallsPerTurn and noProgressRoundLimit in run_budget.go, which bound
// total calls and aimless rounds no matter how the inputs vary; context is
// kept in check by compactRunMessages, and a model repeating the same calls
// is caught by stuckRoundLimit. 250 stays as the last-resort backstop.
const maxToolRounds = 250

// maxModelRetries bounds retries per model turn when the provider failed in
// a retryable way (429, 5xx, timeout). Long unattended runs die on a single
// blip otherwise.
const maxModelRetries = 2

// maxOpaqueRetries is the single extra attempt for an upstream failure the
// gateway masked as an opaque bad_input error over HTTP 200. It stacks on top
// of maxModelRetries so a genuine 502 that got masked still gets a second
// look, while a real bad request still surfaces instead of spinning.
const maxOpaqueRetries = 1

// maxToolCallRecovery bounds the retry for a stream that ended on a tool_calls
// stop without a usable tool call (the call was cut off before its name or
// arguments arrived). One resend covers a mid-stream cutoff; a model that
// keeps emitting unusable calls should end the turn instead of looping.
const maxToolCallRecovery = 1

const toolCallRecoveryDirective = "Your last response ended with a tool call that did not arrive intact — " +
	"no complete tool call was received. Resend the tool call in full with its name and arguments, " +
	"or, if you cannot, reply with plain text instead."

// wrapUpDirective is injected as a final user turn when a run has to stop
// early (round backstop or a repeating loop), so the run ends with an honest
// summary instead of a canned lie.
const wrapUpDirective = "This run has to stop here. Do not call any more tools. " +
	"Tell the user plainly what you already did, what is still unfinished, and the single next step, " +
	"and say they can reply \"continue\" to pick up from where you stopped."

func (s *Service) Send(agentID, text string) Shared.SendResult {
	return s.send(agentID, text, nil, false, "", nil, nil)
}

// SendWithAttachments starts a turn the way Send does, but with uploaded files
// folded into the user message: images become vision blocks, documents become
// extracted text. The attachment ids were returned by the uploads service; a
// missing or unknown id fails the send rather than being silently dropped, so
// the frontend cannot think a file was attached when it was not.
func (s *Service) SendWithAttachments(agentID, text string, attachmentIDs []string) Shared.SendResult {
	return s.send(agentID, text, attachmentIDs, false, "", nil, nil)
}

// SendScheduledWake starts the turn a scheduled task's fire produces. It is
// deliberately unlike Send: no user spoke. The row is written with kind
// 'scheduled' — invisible in the transcript, replayed in history like a
// steering row — and the run is unattended, so a routine at 7am on a desk with
// no one watching still works. The wake text the scheduler passes is
// phrased as the job description it is, not as something a person said.
func (s *Service) SendScheduledWake(agentID, text string, prepared func(Shared.SendResult)) Shared.SendResult {
	return s.send(agentID, text, nil, true, Storage.MessageKindScheduled, prepared, nil)
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
	return s.send(agentID, text, nil, false, Storage.MessageKindSteering, nil, nil)
}

type runEnvelope struct {
	ThreadID  string
	BaseHops  int
	Delegated *Storage.AgentMessage
}

// unattended drives the permission gate; kind drives the row's visibility.
// Only "", Storage.MessageKindSteering and Storage.MessageKindScheduled are
// written today. attachmentIDs are uploaded files to fold into the user
// message; images become vision blocks, documents become extracted text.
func (s *Service) send(agentID, text string, attachmentIDs []string, unattended bool, kind string, prepared func(Shared.SendResult), envelope *runEnvelope) Shared.SendResult {
	log.Printf("Send called agent=%s unattended=%v kind=%s", agentID, unattended, kind)
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.SendResult{Error: aerr}
	}
	agentID = normalizeAgentID(agentID)
	text = strings.TrimSpace(text)
	if text == "" {
		return Shared.SendResult{Error: Shared.NewError(Shared.CodeBadInput, "Message text is required")}
	}

	profile, err := s.ensureAgent(agentID)
	if err != nil {
		return Shared.SendResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	modelRef := Shared.RouterAutoModelRef

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
	// Every early return below gives the reservation back. Once the real run id
	// owns the slot (launched), the run releases it itself when it finishes.
	launched := false
	defer func() {
		if launched {
			return
		}
		s.mu.Lock()
		if s.agentRun[agentID] == "" {
			delete(s.agentRun, agentID)
		}
		s.mu.Unlock()
	}()

	// Validate the provider, key and routing target before any message or
	// request row is written, so a rejected send never leaves artifacts
	// behind in the chat.
	providerID, modelID := Models.SplitRef(modelRef)
	provider, ok := s.Core.Store.Get().Provider(providerID)
	if !ok {
		return Shared.SendResult{Error: Shared.NewError(Shared.CodeUnknownProvider, "Unknown provider")}
	}
	if base := s.baseURLOverride(providerID); base != "" {
		provider.BaseURL = base
	}
	// Defence in depth: the auto alias must be in the paired plan's allowlist.
	// The router enforces the same boundary server-side.
	if providerID == Shared.RouterProviderID && !s.allowedModel(modelID) {
		return Shared.SendResult{Error: Shared.NewError(Shared.CodeBadInput, "This model is not in your plan")}
	}
	overlayProvider, _ := s.Core.Overlay.Lookup(providerID)
	key := ""
	if !provider.NoAuth {
		stored, kerr := getKey(providerID)
		if kerr != nil {
			if errors.Is(kerr, Keyring.ErrNotFound) {
				return Shared.SendResult{Error: Shared.NewError(Shared.CodeNotConfigured, "Connect "+providerID+" before sending")}
			}
			return Shared.SendResult{Error: Shared.NewError(Shared.CodeStorage, "The secrets store is unreachable. Re-enter the key")}
		}
		if strings.TrimSpace(stored) == "" {
			return Shared.SendResult{Error: Shared.NewError(Shared.CodeNotConfigured, "Connect "+providerID+" before sending")}
		}
		key = stored
	}
	target, terr := Router.TargetFor(provider, modelID, key, Router.TargetOptions{
		ExtraHeaders: overlayProvider.ExtraHeaders,
		Quirks:       overlayProvider.Quirks,
	})
	if terr != nil {
		return Shared.SendResult{Error: Shared.NewError(Shared.CodeBadInput, terr.Error())}
	}

	// Everything above is now validated: write the message rows. Both rows
	// allocate from the counter — deriving the assistant seq as userSeq+1
	// writes a seq the counter still believes is free, and the next
	// allocation collides on the (agent_id, seq) unique index.
	userSeq, serr := s.Core.DB.NextSeq(agentID)
	if serr != nil {
		return Shared.SendResult{Error: Shared.NewError(Shared.CodeStorage, serr.Error())}
	}
	assistantSeq, aserr := s.Core.DB.NextSeq(agentID)
	if aserr != nil {
		return Shared.SendResult{Error: Shared.NewError(Shared.CodeStorage, aserr.Error())}
	}

	userID, assistantID, runID := newID(), newID(), newID()

	userBlocks, aerr := s.attachmentBlocks(agentID, attachmentIDs)
	if aerr != nil {
		return Shared.SendResult{Error: aerr}
	}
	userBlocks = append([]Protocol.Block{{Type: Protocol.BlockText, Text: text}}, userBlocks...)
	// The transcript text is what the user typed — attachment contents ride in
	// the blocks for the model and are drawn as file chips, never dumped into
	// the visible chat.
	userText := text

	userMsg := Storage.Message{
		ID:          userID,
		AgentID:     agentID,
		Seq:         userSeq,
		Role:        "user",
		Kind:        kind,
		BlocksJSON:  mustMarshal(userBlocks),
		Text:        userText,
		Status:      "complete",
		CreatedAt:   nowUnixMs(),
		CompletedAt: nowUnixMs(),
	}
	if err := s.Core.DB.InsertMessage(userMsg); err != nil {
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
		ModelRef:   modelRef,
		CreatedAt:  nowUnixMs(),
	}
	if err := s.Core.DB.InsertMessage(assistantMsg); err != nil {
		return Shared.SendResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}

	// Only a short recent window is replayed; anything older is reached through
	// conversation_read. Every model the router allows can call tools, so there
	// is no long-replay fallback.
	memorySelection, merr := Memory.Select(s.Core.DB, agentID, text, 500, 8, false)
	if merr != nil {
		log.Printf("chat: memory selection failed: %v", merr)
	}
	historyBudget := s.historyBudgetTokens(modelRef, providerID, modelID) - int64(memorySelection.EstimatedTokens)
	if historyBudget < 1000 {
		historyBudget = 1000
	}
	history := s.buildHistory(agentID, userSeq, historyBudget, recentHistoryMessages)
	history = prependMemoryContext(history, memorySelection)
	// Drop image blocks for a model that cannot see them, so a vision-only
	// attachment never becomes a wire-format error on a text-only model.
	history = scrubImagesForNonVision(history, &target)

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
	firstRunPending := s.firstRunPending(agentID)
	firstRunName := ""
	if firstRunPending {
		firstRunName = s.firstRunNameAnswer(agentID)
	}
	if firstRunPending && withTools {
		forceFirstRunTool(&req, firstRunName)
		log.Printf("chat: first-run forcing %s tool choice", req.ToolChoice.Name)
	}
	scheduledFire := kind == Storage.MessageKindScheduled
	if scheduledFire {
		req.Tools = withoutScheduleCreationTools(req.Tools)
	}
	delegated := envelope != nil && envelope.Delegated != nil
	sysPrompt := BuildSystemPrompt(s.Core.DB.SQL(), agentID, systemPromptFor(profile, withTools), delegated)
	if firstRunPending {
		sysPrompt += s.firstRunPrompt(agentID, withTools)
		if firstRunName != "" {
			sysPrompt += fmt.Sprintf("\n## Chosen name\nUse this exact name: %q\n", firstRunName)
		}
	}
	if envelope != nil {
		sysPrompt += Memory.ThreadPrompt(s.Core.DB, envelope.ThreadID)
	}
	if scheduledFire {
		sysPrompt += "\n## Active background task\nThis turn was started internally by a reminder or routine that is due now. " +
			"Execute its stored instruction once and return only the user-facing result. " +
			"Do not create, change, or repeat any schedule; schedule creation tools are disabled for this turn.\n"
	}
	if learned := Memory.BehaviorPrompt(s.Core.DB, agentID); learned != "" {
		sysPrompt += learned
	}
	if s.MCP != nil {
		var serverIDs []string
		for serverID := range s.connectedServerSet(agentID) {
			serverIDs = append(serverIDs, serverID)
		}
		if lessons := Memory.ToolLessonsPrompt(s.Core.DB, agentID, serverIDs); lessons != "" {
			sysPrompt += lessons
		}
	}
	if loc := Location.Current(sCtx(), s.Core.DB); loc.Timezone != "" {
		sysPrompt += loc.Prompt()
	}
	// Which connectors are live, counted from the tools actually attached right
	// now. Re-read every run so a sign-in that finished after the agent last
	// looked needs no one telling: the run before it, the model had only its own
	// "stop and wait" tool result to go on.
	if s.MCPConnect != nil {
		if live := s.MCPConnect.AgentPromptBlock(agentID, withTools); live != "" {
			sysPrompt += live
		}
	}
	if skillsBlock := s.skillsPromptBlock(agentID, withTools); skillsBlock != "" {
		sysPrompt += skillsBlock
	}
	req.System = sysPrompt
	log.Printf("chat: history messages=%d system_tokens≈%d tools=%d", len(history), s.historySystemTokens(&req), len(req.Tools))

	// Keep the default output budget unless the model's own limit is tighter,
	// so reasoning models aren't handed a field they reject.
	clampToModel(s, modelRef, providerID, modelID, &target, &req.MaxOutputTokens)

	mr := Storage.ModelRequest{
		ID:                  runID,
		AgentID:             agentID,
		MessageID:           assistantID,
		Attempt:             1,
		ProviderID:          providerID,
		ModelID:             modelID,
		ModelRef:            modelRef,
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
		return Shared.SendResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	if len(memorySelection.Items) > 0 {
		exposures := make([]Storage.MemoryExposure, 0, len(memorySelection.Items))
		for i, selected := range memorySelection.Items {
			exposures = append(exposures, Storage.MemoryExposure{
				MemoryID: selected.Memory.ID, MemoryRevision: selected.Memory.Revision,
				Position: i, Score: selected.Score, EstimatedTokens: selected.EstimatedTokens,
			})
		}
		if err := s.Core.DB.RecordMemoryExposures(runID, exposures); err != nil {
			log.Printf("chat: memory exposure recording failed: %v", err)
		}
	}

	s.mu.Lock()
	if s.cancels == nil {
		s.cancels = map[string]context.CancelFunc{}
	}
	s.agentRun[agentID] = runID
	launched = true
	ctx, cancel := context.WithCancel(context.Background())
	threadID := runID
	baseHops := 0
	if envelope != nil {
		if envelope.ThreadID != "" {
			threadID = envelope.ThreadID
		}
		baseHops = envelope.BaseHops
	}
	ctx = Shared.WithAgentRun(ctx, Shared.AgentRunContext{
		ThreadID: threadID, Hops: baseHops, Delegated: delegated,
	})
	if unattended {
		ctx = Shared.WithUnattended(ctx)
	}
	if scheduledFire {
		ctx = Shared.WithScheduledFire(ctx)
	}
	s.cancels[runID] = cancel
	s.mu.Unlock()

	started := Shared.SendResult{Ok: true, RunID: runID, UserMessageID: userID, AssistantMessageID: assistantID}
	if prepared != nil {
		prepared(started)
	}
	s.Core.EmitEvent(Shared.EventChatRunStarted, map[string]any{"agent_id": agentID, "run_id": runID})
	go s.runTurn(runID, ctx, cancel, agentID, assistantID, modelRef, req, &target, profile, mr, envelope)

	s.Core.EmitEvent(Shared.EventChatMessage, map[string]any{"agent_id": agentID, "message_id": userID})
	s.Core.EmitEvent(Shared.EventChatMessage, map[string]any{"agent_id": agentID, "message_id": assistantID})

	return started
}

func (s *Service) IsBusy(agentID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, busy := s.agentRun[agentID]
	return busy
}

// WakeAgent materializes one queued mailbox item through the normal send path.
// It never grants inherited permissions: the target run discovers only the
// target agent's own bound tools.
func (s *Service) WakeAgent(agentID string) {
	go s.wakeAgent(agentID)
}

func (s *Service) wakeAgent(agentID string) {
	if s.Core == nil || s.Core.DB == nil || s.IsBusy(agentID) {
		return
	}
	message, found, err := Storage.ClaimNextAgentMessage(s.Core.DB.SQL(), agentID)
	if err != nil || !found {
		return
	}
	text := message.Body
	kind := Storage.MessageKindSteering
	unattended := true
	envelope := &runEnvelope{ThreadID: message.ThreadID, BaseHops: message.Hops}
	sender := message.FromAgentID
	if teammate, err := Storage.GetAgentByID(s.Core.DB.SQL(), message.FromAgentID); err == nil {
		sender = "@" + teammate.Handle
	}
	switch message.Kind {
	case "request":
		kind = Storage.MessageKindDelegated
		text = fmt.Sprintf("[Delegated task from %s] %s", sender, message.Body)
		envelope.Delegated = &message
	case "report":
		text = fmt.Sprintf("[Report from %s] %s", sender, message.Body)
	case "user":
		kind = ""
		unattended = false
	}
	started := s.send(agentID, text, nil, unattended, kind, nil, envelope)
	if started.Error != nil {
		status := "failed"
		if started.Error.Code == string(Shared.CodeBusy) {
			status = "queued"
		}
		if message.Kind == "request" && status == "failed" {
			report := "Delegated run could not start: " + started.Error.Message
			_ = FinalizeDelegatedRun(s.Core.DB.SQL(), message, report, status)
			if message.ReportBack {
				s.WakeAgent(message.FromAgentID)
			}
		} else {
			_ = Storage.MarkAgentMessageStatus(s.Core.DB.SQL(), message.ID, status)
		}
		return
	}
	if message.Kind != "request" {
		_ = Storage.MarkAgentMessageStatus(s.Core.DB.SQL(), message.ID, "completed")
	}
}

func withoutScheduleCreationTools(tools []Protocol.ToolDef) []Protocol.ToolDef {
	out := make([]Protocol.ToolDef, 0, len(tools))
	for _, tool := range tools {
		if tool.Name == scheduleReminderToolDef.Name || tool.Name == createRoutineToolDef.Name {
			continue
		}
		out = append(out, tool)
	}
	return out
}

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

func (s *Service) ensureAgent(agentID string) (Storage.AgentProfile, error) {
	p, found, err := s.Core.DB.GetAgentProfile(agentID)
	if err != nil {
		return p, err
	}
	if found {
		return p, nil
	}
	p = Storage.AgentProfile{ID: agentID, AgentName: "Argus", Avatar: Storage.DefaultAgentAvatar, UpdatedAt: ""}
	if err := s.Core.DB.SaveAgentProfile(p); err != nil {
		return p, err
	}
	return p, nil
}

// ResolveMentionTarget routes a leading "@handle prompt" straight to that
// agent instead of the default one, so talking to a teammate needs no tool
// call. A leading @ that names nobody stays with the default agent and keeps
// its text: the model, not the router, decides what an unknown @ means.
func ResolveMentionTarget(db *sql.DB, defaultAgentID, prompt string) (string, string) {
	trimmed := strings.TrimSpace(prompt)
	if !strings.HasPrefix(trimmed, "@") {
		return defaultAgentID, trimmed
	}

	parts := strings.SplitN(trimmed, " ", 2)
	ag, err := Storage.GetAgentByHandle(db, parts[0])
	if err != nil {
		return defaultAgentID, trimmed
	}
	if len(parts) == 1 {
		return ag.ID, ""
	}
	return ag.ID, strings.TrimSpace(parts[1])
}

// SteerActiveRun injects a mid-run user instruction that the run loop drains
// at the next round boundary. It is queued rather than pushed into memory so
// the loop — the only writer of the model's history — stays in charge of
// where the note lands.
func SteerActiveRun(db *sql.DB, agentID, threadID, instruction string) error {
	body := strings.TrimSpace(instruction)
	if body == "" {
		return nil
	}
	return Storage.EnqueueAgentMessage(db, Storage.AgentMessage{
		ID:          "steer_" + uuid.NewString(),
		ThreadID:    threadID,
		FromAgentID: "user",
		ToAgentID:   agentID,
		Kind:        "steer",
		Body:        body,
		Status:      "queued",
		Urgent:      true,
	})
}

// HandleUserChatSend routes one user prompt (mention syntax included) to the
// right agent. An agent that is mid-run gets its prompt as steering; an idle
// agent gets it as queued mail and a wake, so nothing the user says is ever
// dropped between runs.
func HandleUserChatSend(
	db *sql.DB,
	defaultAgentID string,
	threadID string,
	prompt string,
	isActive bool,
	wakeFn func(agentID string),
) (string, error) {
	targetID, cleanPrompt := ResolveMentionTarget(db, defaultAgentID, prompt)
	if cleanPrompt == "" {
		return targetID, fmt.Errorf("prompt cannot be empty")
	}
	if threadID == "" {
		threadID = "thr_" + uuid.NewString()
	}
	if isActive {
		return targetID, SteerActiveRun(db, targetID, threadID, cleanPrompt)
	}
	err := Storage.EnqueueAgentMessage(db, Storage.AgentMessage{
		ID:          "umsg_" + uuid.NewString(),
		ThreadID:    threadID,
		FromAgentID: "user",
		ToAgentID:   targetID,
		Kind:        "user",
		Body:        cleanPrompt,
		Status:      "queued",
	})
	if err != nil {
		return targetID, err
	}
	if wakeFn != nil {
		wakeFn(targetID)
	}
	return targetID, nil
}

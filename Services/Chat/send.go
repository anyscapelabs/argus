package Chat

import (
	"context"
	"errors"
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

func (s *Service) Send(agentID, text string) Shared.SendResult {
	return s.send(agentID, text, nil, false, "", nil)
}

// SendWithAttachments starts a turn the way Send does, but with uploaded files
// folded into the user message: images become vision blocks, documents become
// extracted text. The attachment ids were returned by the uploads service; a
// missing or unknown id fails the send rather than being silently dropped, so
// the frontend cannot think a file was attached when it was not.
func (s *Service) SendWithAttachments(agentID, text string, attachmentIDs []string) Shared.SendResult {
	return s.send(agentID, text, attachmentIDs, false, "", nil)
}

// SendScheduledWake starts the turn a scheduled task's fire produces. It is
// deliberately unlike Send: no user spoke. The row is written with kind
// 'scheduled' — invisible in the transcript, replayed in history like a
// steering row — and the run is unattended, so a routine at 7am on a desk with
// no one watching still works. The wake text the scheduler passes is
// phrased as the job description it is, not as something a person said.
func (s *Service) SendScheduledWake(agentID, text string, prepared func(Shared.SendResult)) Shared.SendResult {
	return s.send(agentID, text, nil, true, Storage.MessageKindScheduled, prepared)
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
	return s.send(agentID, text, nil, false, Storage.MessageKindSteering, nil)
}

// unattended drives the permission gate; kind drives the row's visibility.
// Only "", Storage.MessageKindSteering and Storage.MessageKindScheduled are
// written today. attachmentIDs are uploaded files to fold into the user
// message; images become vision blocks, documents become extracted text.
func (s *Service) send(agentID, text string, attachmentIDs []string, unattended bool, kind string, prepared func(Shared.SendResult)) Shared.SendResult {
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

	// Everything above is now validated: write the message rows. Both rows
	// allocate from the counter — deriving the assistant seq as userSeq+1
	// writes a seq the counter still believes is free, and the next
	// allocation collides on the (agent_id, seq) unique index.
	userSeq, serr := s.Core.DB.NextSeq(agentID)
	if serr != nil {
		release()
		return Shared.SendResult{Error: Shared.NewError(Shared.CodeStorage, serr.Error())}
	}
	assistantSeq, aserr := s.Core.DB.NextSeq(agentID)
	if aserr != nil {
		release()
		return Shared.SendResult{Error: Shared.NewError(Shared.CodeStorage, aserr.Error())}
	}

	uid, _ := uuid.NewV7()
	userID := uid.String()
	aid, _ := uuid.NewV7()
	assistantID := aid.String()
	rid, _ := uuid.NewV7()
	runID := rid.String()

	userBlocks, aerr := s.attachmentBlocks(agentID, attachmentIDs)
	if aerr != nil {
		release()
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
	memorySelection, merr := Memory.Select(s.Core.DB, agentID, text, 500, 8, false)
	if merr != nil {
		log.Printf("chat: memory selection failed: %v", merr)
	}
	historyBudget := s.historyBudgetTokens(profile.ModelRef, providerID, modelID) - int64(memorySelection.EstimatedTokens)
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
	scheduledFire := kind == Storage.MessageKindScheduled
	if scheduledFire {
		req.Tools = withoutScheduleCreationTools(req.Tools)
	}
	sysPrompt := systemPromptFor(profile, withTools)
	if scheduledFire {
		sysPrompt += "\n## Active background task\nThis turn was started internally by a reminder or routine that is due now. " +
			"Execute its stored instruction once and return only the user-facing result. " +
			"Do not create, change, or repeat any schedule; schedule creation tools are disabled for this turn.\n"
	}
	if learned := Memory.BehaviorPrompt(s.Core.DB, agentID); learned != "" {
		sysPrompt += learned
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
	ctx, cancel := context.WithCancel(context.Background())
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
	go s.runTurn(runID, ctx, cancel, agentID, assistantID, profile.ModelRef, kind, req, &target, profile, mr)

	s.Core.EmitEvent(Shared.EventChatMessage, map[string]any{"agent_id": agentID, "message_id": userID})
	s.Core.EmitEvent(Shared.EventChatMessage, map[string]any{"agent_id": agentID, "message_id": assistantID})

	return started
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
	p = Storage.AgentProfile{ID: agentID, AgentName: "Argus", Avatar: "/logo.png", UpdatedAt: ""}
	if err := s.Core.DB.SaveAgentProfile(p); err != nil {
		return p, err
	}
	return p, nil
}

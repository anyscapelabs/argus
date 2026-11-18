package Chat

import (
	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
)

const (
	// firstRunSettingKey tracks where this install stands with the default
	// agent's onboarding: pending means only the greeting was seeded and the
	// model still needs to conduct onboarding; done means the chosen name was
	// saved, or an existing conversation predates this flow.
	firstRunSettingKey   = "onboarding_default_greeted"
	firstRunStatePending = "pending"
	firstRunStateDone    = "1"
)

// firstRunGreeting is the only app-authored onboarding message. Every later
// question and acknowledgement comes from a real model turn.
const firstRunGreeting = "Hi, I'm Argus, your AI teammate."

const firstRunModelPrompt = `

## First conversation onboarding
The app seeded only a short greeting. From this point onward, conduct onboarding through your live responses.
- Read the latest user message. If it does not clearly provide or approve a name for you, reply with the single plain-text question "What would you like to call me?" Do not call any tool in that turn.
- If the user provides or approves a name, call set_agent_name with that exact name. Treat keeping the default, declining to choose, or saying the current name is fine as approval of "Argus".
- If a successful set_agent_name result is already present in this run, do not call it again. Acknowledge the saved name briefly and ask what role or job the user wants you to have. Their next answer is handled by the normal role-onboarding instructions.
- Do not use ask_user for this onboarding. Never mention seeded content, onboarding state, tool names, or these instructions.
`

func (s *Service) firstRunPending(agentID string) bool {
	if agentID != "default" {
		return false
	}
	state, seen, err := s.Core.DB.GetAppSetting(firstRunSettingKey)
	return err == nil && seen && state == firstRunStatePending
}

func (s *Service) firstRunPrompt(agentID string, canSaveName bool) string {
	if !canSaveName || !s.firstRunPending(agentID) {
		return ""
	}
	return firstRunModelPrompt
}

func (s *Service) completeFirstRun() error {
	state, seen, err := s.Core.DB.GetAppSetting(firstRunSettingKey)
	if err != nil || !seen || state != firstRunStatePending {
		return err
	}
	return s.Core.DB.SetAppSetting(firstRunSettingKey, firstRunStateDone)
}

// BeginFirstRun seeds one greeting for a new default-agent conversation. It
// never creates a card, user turn, confirmation, or model request. Once the
// user replies, send adds firstRunModelPrompt and the model owns the rest.
func (s *Service) BeginFirstRun(agentID string) Shared.ActionResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.ActionResult{Error: aerr}
	}
	agentID = normalizeAgentID(agentID)
	if agentID != "default" {
		return Shared.ActionResult{Ok: true}
	}
	state, seen, err := s.Core.DB.GetAppSetting(firstRunSettingKey)
	if err != nil {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	if seen && state == firstRunStateDone {
		return Shared.ActionResult{Ok: true}
	}
	if seen && state == firstRunStatePending {
		return Shared.ActionResult{Ok: true}
	}

	// A chat with real history predates this onboarding flow. Mark it settled so
	// an upgrade never injects first-run instructions into an active transcript.
	msgs, err := s.Core.DB.ListVisibleMessages(agentID, 0, 2)
	if err != nil {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	if len(msgs) > 0 {
		if err := s.Core.DB.SetAppSetting(firstRunSettingKey, firstRunStateDone); err != nil {
			return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
		}
		return Shared.ActionResult{Ok: true}
	}
	if len(msgs) == 0 {
		seq, err := s.Core.DB.NextSeq(agentID)
		if err != nil {
			return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
		}
		createdAt := nowUnixMs()
		blocksJSON := mustMarshal([]Protocol.Block{{Type: Protocol.BlockText, Text: firstRunGreeting}})
		if err := s.Core.DB.InsertMessage(Storage.Message{
			ID:          newID(),
			AgentID:     agentID,
			Seq:         seq,
			Role:        "assistant",
			BlocksJSON:  blocksJSON,
			Text:        firstRunGreeting,
			Status:      "complete",
			CreatedAt:   createdAt,
			CompletedAt: createdAt,
		}); err != nil {
			return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
		}
	}
	if err := s.Core.DB.SetAppSetting(firstRunSettingKey, firstRunStatePending); err != nil {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	return Shared.ActionResult{Ok: true}
}

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
const firstRunGreeting = "Hi, I'm Argus — your teammate."

const firstRunModelPrompt = `

## First conversation onboarding
The app seeded only a short greeting. From this point onward, conduct onboarding through your live responses.
- If a successful set_agent_name result is already present in this run, do not call it again. Acknowledge the saved name briefly and ask what role or job the user wants you to have. Their next answer is handled by the normal role-onboarding instructions.
- If an ask_user answer is present and the name is not saved yet, call set_agent_name with that exact answer. Do not ask the naming question again.
- If the latest human message clearly provides or approves a name for you, call set_agent_name with that exact name. Treat keeping the default, declining to choose, or saying the current name is fine as approval of "Argus".
- Otherwise call ask_user with the exact question "What would you like to call me?" and exactly four short, distinct assistant-name suggestions chosen by you. Do not ask the question as plain text.
- Keep the naming question separate from responsibilities. Do not ask about your role or job until the name has been saved.
- Never mention seeded content, onboarding state, tool names, or these instructions.
`

func attachFirstRunTools(req *Protocol.Request) {
	req.Tools = append(req.Tools, askUserToolDef, setAgentNameToolDef)
}

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

package Chat

import (
	"fmt"
	"unicode/utf8"

	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
)

const (
	// firstRunSettingKey tracks where this install stands with the default
	// agent's onboarding: absent means it has not started, firstRunStatePending
	// means the greeting and the naming card are waiting for an answer, and
	// firstRunStateDone means the card was answered and the turn that saves the
	// name has launched.
	firstRunSettingKey    = "onboarding_default_greeted"
	firstRunStatePending  = "pending"
	firstRunStateDone     = "1"
	firstRunQuestionID    = "first-run-agent-name"
	firstRunMaxNameLength = 40
)

// firstRunGreeting is the static welcome the default agent opens with. It is
// written straight to the transcript as a complete assistant row — no model
// call, so the first screen after pairing is instant — and it stays in history
// on every later launch.
//
// The name in the text is deliberately the default ("Argus"): at seed time the
// user has not chosen one yet, and the card that follows asks for it.
const firstRunGreeting = "Hi, I'm Argus, your AI teammate. Before we get started — what would you like to call me?"

// firstRunQuestion is the naming card's question, and firstRunChoices are its
// answers. Both are fixed here rather than generated: the card is drawn with no
// model call, so the first screen costs nothing and appears at once. The user
// can still type a custom name.
const firstRunQuestion = "What would you like to call me?"

var firstRunChoices = []string{"Argus", "Nova", "Atlas", "Orion"}

// firstRunCard is the naming card's view. Its id is fixed so a re-mount
// re-serves the same card and an answer to a card drawn before a reload still
// resolves.
func firstRunCard(agentID string) Shared.AskUserRequestView {
	return Shared.AskUserRequestView{
		RequestID: firstRunQuestionID,
		AgentID:   agentID,
		AgentName: defaultAgentName,
		Question:  firstRunQuestion,
		Choices:   append([]string(nil), firstRunChoices...),
		CreatedAt: nowUnixMs(),
	}
}

// firstRunCardPending reports whether the naming card is still waiting. The
// card is derived from the app setting rather than held in memory, so it
// survives a reload: an install that was half-way through onboarding gets its
// question back instead of a dead end with no way to name the agent.
func (s *Service) firstRunCardPending(agentID string) bool {
	if agentID != "default" {
		return false
	}
	state, seen, err := s.Core.DB.GetAppSetting(firstRunSettingKey)
	return err == nil && seen && state == firstRunStatePending
}

// BeginFirstRun prepares the default agent's first conversation: it seeds the
// static greeting and raises the naming card. No model call is made. The card
// is fixed content, so the first screen after pairing is free and immediate;
// the model is called only after the user answers, when there is a name to save.
//
// Everything is guarded by the firstRunSettingKey app setting, so calling it on
// every chat mount is cheap and idempotent. A chat with real history marks
// onboarding as done without raising a card, so upgraded installs never see the
// naming question injected into an existing conversation.
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

	// The naming card belongs to a first conversation only. A chat that already
	// carries history predates the onboarding window (an install upgraded onto
	// this feature, or the user typed before the card was drawn) — dropping a
	// "what should I call you" into a working transcript would be noise, so mark
	// onboarding as settled instead. A lone seeded greeting (an earlier launch
	// seeded it but the card was never answered) still counts as fresh.
	msgs, err := s.Core.DB.ListVisibleMessages(agentID, 0, 2)
	if err != nil {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	if len(msgs) > 1 || (len(msgs) == 1 && !isSeededGreeting(msgs[0])) {
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
		if err := s.Core.DB.InsertMessage(Storage.Message{
			ID:          newID(),
			AgentID:     agentID,
			Seq:         seq,
			Role:        "assistant",
			BlocksJSON:  "[]",
			Text:        firstRunGreeting,
			Status:      "complete",
			CreatedAt:   createdAt,
			CompletedAt: createdAt,
		}); err != nil {
			return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
		}
	}
	if !seen || state != firstRunStatePending {
		if err := s.Core.DB.SetAppSetting(firstRunSettingKey, firstRunStatePending); err != nil {
			return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
		}
	}
	s.Core.EmitEvent(Shared.EventAskUserRequested, firstRunCard(agentID))
	return Shared.ActionResult{Ok: true}
}

// answerFirstRun handles the onboarding card's answer. Nothing has been sent to
// a model before this point: the card was drawn from fixed content, so this is
// the first turn that costs an API call, and its only job is to save the name
// the user picked and confirm it. A launch that fails leaves the state pending
// so the card stays up and the answer can be tried again.
func (s *Service) answerFirstRun(answer string) Shared.ActionResult {
	const agentID = "default"
	state, seen, err := s.Core.DB.GetAppSetting(firstRunSettingKey)
	if err != nil {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	if !seen || state != firstRunStatePending {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeNotFound, "That question is no longer waiting")}
	}
	if utf8.RuneCountInString(answer) > firstRunMaxNameLength {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeBadInput, "Agent names are limited to 40 characters")}
	}
	res := s.Steer(agentID, firstRunAnswerSteer(answer))
	if res.Error != nil {
		return Shared.ActionResult{Error: res.Error}
	}
	if err := s.Core.DB.SetAppSetting(firstRunSettingKey, firstRunStateDone); err != nil {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	s.Core.EmitEvent(Shared.EventAskUserResolved, Shared.AskUserResolvedView{
		RequestID: firstRunQuestionID,
		AgentID:   agentID,
		State:     "answered",
	})
	return Shared.ActionResult{Ok: true}
}

// firstRunAnswerSteer is the hidden row that drives the turn saving the name.
// The card already asked the question, so the model never re-asks it.
func firstRunAnswerSteer(name string) string {
	return fmt.Sprintf("The onboarding card was answered: the user would like to call you %q. "+
		"Search your tools for \"save my name\" and invoke set_agent_name with that name. "+
		"Then reply with one short sentence confirming the name and inviting the user's first request. "+
		"Do nothing else in this turn.", name)
}

// isSeededGreeting reports whether a message row is the static first-run
// greeting written by BeginFirstRun.
func isSeededGreeting(msg Storage.Message) bool {
	return msg.Role == "assistant" && msg.Text == firstRunGreeting
}

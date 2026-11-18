package Chat

import (
	"fmt"
	"log"
	"strings"
	"unicode/utf8"

	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
)

const (
	// firstRunSettingKey tracks where this install stands with the default
	// agent's onboarding: absent means it has not started, firstRunStatePending
	// means the greeting and the naming card are waiting for an answer, and
	// firstRunStateDone means the card was answered and the name is saved.
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
// the answer is applied locally too, so onboarding never spends a model turn.
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
	if !seen || state != firstRunStatePending {
		if err := s.Core.DB.SetAppSetting(firstRunSettingKey, firstRunStatePending); err != nil {
			return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
		}
	}
	s.Core.EmitEvent(Shared.EventAskUserRequested, firstRunCard(agentID))
	return Shared.ActionResult{Ok: true}
}

// answerFirstRun handles the onboarding card's answer. Naming the agent is a
// local write, not a model turn: the answer is already known, so spending an
// API call to store it bought nothing and made onboarding fail whenever the
// provider hiccuped on that turn. The confirmation is seeded like the greeting,
// so the whole first screen stays free and works offline.
func (s *Service) answerFirstRun(answer string) Shared.ActionResult {
	const agentID = "default"
	state, seen, err := s.Core.DB.GetAppSetting(firstRunSettingKey)
	if err != nil {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	if !seen || state != firstRunStatePending {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeNotFound, "That question is no longer waiting")}
	}
	name := strings.TrimSpace(answer)
	if name == "" {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeBadInput, "Type or select a name")}
	}
	if utf8.RuneCountInString(name) > firstRunMaxNameLength {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeBadInput, "Agent names are limited to 40 characters")}
	}
	if _, err := saveAgentName(s.Core.DB, agentID, name); err != nil {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	// Settle before seeding so a double-submit cannot write two confirmations;
	// the name is already saved at this point, so the answer is final either way.
	if err := s.Core.DB.SetAppSetting(firstRunSettingKey, firstRunStateDone); err != nil {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	// The card itself stays in the transcript: the question asked and the name
	// picked, so the chat reads as a conversation instead of a vanished card.
	// Neither seed announces chat:message: that event is the run indicator's
	// "a turn is live" signal, and with no run behind it the chat would sit on
	// "Thinking…" forever. SlotsChanged below is what refreshes the transcript.
	if _, err := s.persistAskUserAnswer(agentID, firstRunQuestion, name, false); err != nil {
		log.Printf("chat: could not persist the onboarding card: %v", err)
	}
	if err := s.seedFirstRunUserChoice(agentID, name); err != nil {
		log.Printf("chat: could not seed the onboarding user turn: %v", err)
	}
	if err := s.seedFirstRunConfirmation(agentID, name); err != nil {
		// The rename landed; a missing confirmation line is cosmetic, so the
		// card still closes instead of trapping the user on a saved answer.
		log.Printf("chat: could not seed the onboarding confirmation: %v", err)
	}
	s.Core.EmitEvent(Shared.EventSlotsChanged, nil)
	s.Core.EmitEvent(Shared.EventAskUserResolved, Shared.AskUserResolvedView{
		RequestID: firstRunQuestionID,
		AgentID:   agentID,
		State:     "answered",
	})
	return Shared.ActionResult{Ok: true}
}

// seedFirstRunUserChoice writes an invisible steering user row for the chosen
// name so history has a valid user turn between the greeting and confirmation
// without showing a duplicate bubble in the transcript.
func (s *Service) seedFirstRunUserChoice(agentID, name string) error {
	seq, err := s.Core.DB.NextSeq(agentID)
	if err != nil {
		return err
	}
	createdAt := nowUnixMs()
	choiceText := fmt.Sprintf("Call yourself %s.", name)
	return s.Core.DB.InsertMessage(Storage.Message{
		ID:          newID(),
		AgentID:     agentID,
		Seq:         seq,
		Role:        "user",
		Kind:        Storage.MessageKindSteering,
		BlocksJSON:  mustMarshal([]Protocol.Block{{Type: Protocol.BlockText, Text: choiceText}}),
		Text:        choiceText,
		Status:      "complete",
		CreatedAt:   createdAt,
		CompletedAt: createdAt,
	})
}

// seedFirstRunConfirmation writes the assistant row that names the agent, the
// same way BeginFirstRun writes the greeting: fixed content, no model call.
func (s *Service) seedFirstRunConfirmation(agentID, name string) error {
	seq, err := s.Core.DB.NextSeq(agentID)
	if err != nil {
		return err
	}
	createdAt := nowUnixMs()
	confirmText := fmt.Sprintf("Nice to meet you — I'm %s from now on. What should we work on first?", name)
	message := Storage.Message{
		ID:          newID(),
		AgentID:     agentID,
		Seq:         seq,
		Role:        "assistant",
		BlocksJSON:  mustMarshal([]Protocol.Block{{Type: Protocol.BlockText, Text: confirmText}}),
		Text:        confirmText,
		Status:      "complete",
		CreatedAt:   createdAt,
		CompletedAt: createdAt,
	}
	if err := s.Core.DB.InsertMessage(message); err != nil {
		return err
	}
	return nil
}

// isSeededGreeting reports whether a message row is the static first-run
// greeting written by BeginFirstRun.
func isSeededGreeting(msg Storage.Message) bool {
	return msg.Role == "assistant" && msg.Text == firstRunGreeting
}

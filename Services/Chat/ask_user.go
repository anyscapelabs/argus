package Chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
)

type pendingUserQuestion struct {
	view Shared.AskUserRequestView
	done chan struct{}

	mu      sync.Mutex
	settled bool
	answer  string
	state   string
}

func (q *pendingUserQuestion) settle(answer, state string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.settled {
		return false
	}
	q.settled = true
	q.answer = answer
	q.state = state
	close(q.done)
	return true
}

func (q *pendingUserQuestion) outcome() (string, string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.answer, q.state
}

func (s *Service) PendingAskUser(agentID string) Shared.AskUserListResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.AskUserListResult{Error: aerr}
	}
	agentID = normalizeAgentID(agentID)
	pending := s.askUserStore.List(agentID)
	out := make([]Shared.AskUserRequestView, 0, len(pending)+1)
	// Onboarding's naming card has no blocked tool call behind it — it is drawn
	// from the app setting — so it is served from there and still appears after
	// a reload. It leads because it gates the conversation: the chat input is
	// disabled while it waits.
	if s.firstRunCardPending(agentID) {
		out = append(out, firstRunCard(agentID))
	}
	for _, request := range pending {
		out = append(out, request.view)
	}
	return Shared.AskUserListResult{Requests: out}
}

func (s *Service) RespondAskUser(requestID, answer string) Shared.ActionResult {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeBadInput, "Type or select an answer")}
	}
	if utf8.RuneCountInString(answer) > 2000 {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeBadInput, "The answer is too long")}
	}
	requestID = strings.TrimSpace(requestID)
	// The onboarding card is answered without a run behind it: its answer is
	// what starts the first model turn. Everything else is the answer to a
	// tool call waiting in a live run.
	if requestID == firstRunQuestionID {
		return s.answerFirstRun(answer)
	}
	request, ok := s.askUserStore.Get(requestID)
	if !ok || !request.settle(answer, "answered") {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeNotFound, "That question is no longer waiting")}
	}
	s.finishAskUser(request, "answered")
	// The card is the user's own record of the exchange, so it stays in the
	// transcript after it is answered — question and chosen answer only.
	if _, err := s.persistAskUserAnswer(request.view.AgentID, request.view.Question, answer, true); err != nil {
		log.Printf("chat: could not persist the answered card: %v", err)
	}
	return Shared.ActionResult{Ok: true}
}

// askUserRecord is the persisted card. It lives in the row's text column the
// way a voice transcript does, so storage holds exactly what the transcript
// draws and buildHistory can skip it without inventing a wire shape.
type askUserRecord struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

// persistAskUserAnswer writes the settled question card into the transcript.
// announce is false for app-authored seeding (onboarding), because
// chat:message is the run indicator's "a turn is live" signal: emitting it with
// no run behind it leaves the chat stuck on "Thinking…" forever. It is best-
// effort for the generic path (a failed write must not undo the answer the run
// already received), so callers log the failure rather than ignoring it.
func (s *Service) persistAskUserAnswer(agentID, question, answer string, announce bool) (string, error) {
	seq, err := s.Core.DB.NextSeq(agentID)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(askUserRecord{Question: question, Answer: answer})
	if err != nil {
		return "", err
	}
	createdAt := nowUnixMs()
	message := Storage.Message{
		ID:          newID(),
		AgentID:     agentID,
		Seq:         seq,
		Role:        "assistant",
		Kind:        Storage.MessageKindAskUser,
		BlocksJSON:  "[]",
		Text:        string(raw),
		Status:      "complete",
		CreatedAt:   createdAt,
		CompletedAt: createdAt,
	}
	if err := s.Core.DB.InsertMessage(message); err != nil {
		return "", err
	}
	if announce {
		s.Core.EmitEvent(Shared.EventChatMessage, map[string]any{"agent_id": agentID, "message_id": message.ID})
	}
	return message.ID, nil
}

func (s *Service) finishAskUser(request *pendingUserQuestion, state string) {
	s.askUserStore.End(request.view.RequestID, request.view.AgentID)
	s.Core.EmitEvent(Shared.EventAskUserResolved, Shared.AskUserResolvedView{
		RequestID: request.view.RequestID,
		AgentID:   request.view.AgentID,
		RunID:     request.view.RunID,
		State:     state,
	})
}

func (s *Service) waitForUserAnswer(ctx context.Context, view Shared.AskUserRequestView) (string, error) {
	if Shared.IsUnattended(ctx) {
		return "", fmt.Errorf("cannot ask the user during an unattended background task")
	}
	request := &pendingUserQuestion{view: view, done: make(chan struct{})}
	if !s.askUserStore.Begin(view.AgentID, request, view.RequestID) {
		return "", fmt.Errorf("another question is already waiting for this agent")
	}
	s.Core.EmitEvent(Shared.EventAskUserRequested, view)
	select {
	case <-request.done:
		answer, state := request.outcome()
		if state != "answered" {
			return "", context.Canceled
		}
		return answer, nil
	case <-ctx.Done():
		if request.settle("", "cancelled") {
			s.finishAskUser(request, "cancelled")
			return "", ctx.Err()
		}
		<-request.done
		answer, state := request.outcome()
		if state == "answered" {
			return answer, nil
		}
		return "", ctx.Err()
	}
}

func runAskUserTool(
	s *Service,
	ctx context.Context,
	runID, agentID, agentName string,
	call Protocol.ToolCall,
	result resultFn,
	activity activityFn,
) Protocol.Block {
	var input struct {
		Question string   `json:"question"`
		Choices  []string `json:"choices"`
	}
	if err := json.Unmarshal([]byte(call.Input), &input); err != nil {
		return result("invalid tool input: could not parse ask_user arguments", true)
	}
	input.Question = strings.TrimSpace(input.Question)
	if input.Question == "" || utf8.RuneCountInString(input.Question) > 500 {
		return result("ask_user needs one concise question of at most 500 characters", true)
	}
	if len(input.Choices) < 1 || len(input.Choices) > 6 {
		return result("ask_user needs between one and six choices", true)
	}
	seen := map[string]bool{}
	choices := make([]string, 0, len(input.Choices))
	for _, choice := range input.Choices {
		choice = strings.TrimSpace(choice)
		if choice == "" || utf8.RuneCountInString(choice) > 120 || seen[strings.ToLower(choice)] {
			return result("ask_user choices must be distinct, non-empty, and at most 120 characters", true)
		}
		seen[strings.ToLower(choice)] = true
		choices = append(choices, choice)
	}
	id, _ := uuid.NewV7()
	activity("tool_started", input.Question)
	answer, err := s.waitForUserAnswer(ctx, Shared.AskUserRequestView{
		RequestID: id.String(), AgentID: agentID, AgentName: agentName,
		RunID: runID, Question: input.Question, Choices: choices, CreatedAt: nowUnixMs(),
	})
	activity("tool_finished", input.Question)
	if err != nil {
		return result("could not ask the user: "+err.Error(), true)
	}
	raw, _ := json.Marshal(map[string]string{"answer": answer})
	return result(string(raw), false)
}

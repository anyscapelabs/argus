package Chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Services/Shared"
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
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		agentID = "default"
	}
	pending := s.askUserStore.List(agentID)
	out := make([]Shared.AskUserRequestView, 0, len(pending))
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
	request, ok := s.askUserStore.Get(strings.TrimSpace(requestID))
	if !ok || !request.settle(answer, "answered") {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeNotFound, "That question is no longer waiting")}
	}
	s.finishAskUser(request, "answered")
	return Shared.ActionResult{Ok: true}
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

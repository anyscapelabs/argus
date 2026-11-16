package Chat

import (
	"context"
	"testing"
	"time"

	"github.com/anyscapelabs/argus/Services/Shared"
)

func TestAskUserWaitsAndReturnsCustomAnswer(t *testing.T) {
	s, _ := newVoiceTestService(t)
	view := Shared.AskUserRequestView{
		RequestID: "ask-1", AgentID: "default", AgentName: "Argus", RunID: "run-1",
		Question: "Which environment?", Choices: []string{"Development", "Production"}, CreatedAt: 1,
	}
	answer := make(chan string, 1)
	go func() {
		got, _ := s.waitForUserAnswer(context.Background(), view)
		answer <- got
	}()
	deadline := time.Now().Add(time.Second)
	for len(s.PendingAskUser("default").Requests) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if pending := s.PendingAskUser("default").Requests; len(pending) != 1 || pending[0].Question != view.Question {
		t.Fatalf("pending question was not restorable: %+v", pending)
	}
	if res := s.RespondAskUser("ask-1", "Staging"); res.Error != nil {
		t.Fatal(res.Error)
	}
	select {
	case got := <-answer:
		if got != "Staging" {
			t.Fatalf("answer = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("ask_user did not resume")
	}
}

func TestAskUserRefusesUnattendedRun(t *testing.T) {
	s, _ := newVoiceTestService(t)
	ctx := Shared.WithUnattended(context.Background())
	if _, err := s.waitForUserAnswer(ctx, Shared.AskUserRequestView{RequestID: "ask-2", AgentID: "default"}); err == nil {
		t.Fatal("unattended run was allowed to block on a question")
	}
	if pending := s.PendingAskUser("default").Requests; len(pending) != 0 {
		t.Fatalf("unattended question leaked a card: %+v", pending)
	}
}

package Chat

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
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

	// The answered card stays in the transcript with the question and the
	// chosen answer, and nothing else.
	msgs, err := s.Core.DB.ListVisibleMessages("default", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Kind != Storage.MessageKindAskUser {
		t.Fatalf("the answered card was not persisted: %+v", msgs)
	}
	var record struct {
		Question string `json:"question"`
		Answer   string `json:"answer"`
	}
	if err := json.Unmarshal([]byte(msgs[0].Text), &record); err != nil {
		t.Fatalf("the card payload is not readable: %v (%s)", err, msgs[0].Text)
	}
	if record.Question != view.Question || record.Answer != "Staging" {
		t.Fatalf("unexpected card payload: %+v", record)
	}
	chat := s.GetChat("default", 0, 10)
	if chat.Error != nil {
		t.Fatal(chat.Error)
	}
	if len(chat.Messages) != 1 || chat.Messages[0].Kind != Storage.MessageKindAskUser {
		t.Fatalf("the card did not reach the chat view: %+v", chat.Messages)
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

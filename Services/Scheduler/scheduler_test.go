package Scheduler

import (
	"path/filepath"
	"testing"

	servicechat "github.com/anyscapelabs/argus/Services/Chat"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
)

type recordingNotifier struct {
	id    string
	title string
	body  string
}

func (n *recordingNotifier) Authorize() error { return nil }
func (n *recordingNotifier) Notify(id, title, body string) {
	n.id, n.title, n.body = id, title, body
}

func TestScheduledCompletionNotifiesWithRunIDAndReply(t *testing.T) {
	db, err := Storage.Open(filepath.Join(t.TempDir(), "scheduler.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := db.SaveAgentProfile(Storage.AgentProfile{ID: "default", AgentName: "Argus"}); err != nil {
		t.Fatal(err)
	}
	if err := db.InsertScheduledTask(Storage.ScheduledTask{
		ID: "task-1", AgentID: "default", Kind: Storage.KindRoutine, Title: "Morning report",
		Prompt: "Summarize the inbox", ScheduleJSON: `{}`, NextRunAt: 100, CreatedAt: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.InsertScheduledRun(Storage.ScheduledRun{
		ID: "scheduled-run-1", TaskID: "task-1", AgentID: "default", MessageID: "message-1", Status: Storage.RunStarted, StartedAt: 100,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.InsertMessage(Storage.Message{
		ID: "message-1", AgentID: "default", Seq: 1, Role: "assistant", BlocksJSON: "[]",
		Text: "Your morning report is ready.\nThree messages need attention.", Status: "complete", CreatedAt: 100, CompletedAt: 101,
	}); err != nil {
		t.Fatal(err)
	}
	notifier := &recordingNotifier{}
	s := &Service{
		Core:     &Shared.Core{DB: db},
		Notifier: notifier,
		pending: map[string]pendingRun{
			"chat-run-1": {taskID: "task-1", runID: "scheduled-run-1", title: "Morning report"},
		},
	}
	s.runFinished(servicechat.RunOutcome{
		RunID: "chat-run-1", AgentID: "default", AssistantMessageID: "message-1", Status: "success",
	})
	if notifier.id != "scheduled-run-1" {
		t.Fatalf("notification id = %q", notifier.id)
	}
	if notifier.title != "Morning report" || notifier.body != "Your morning report is ready." {
		t.Fatalf("unexpected notification: %+v", notifier)
	}
}

package Scheduler

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	servicechat "github.com/anyscapelabs/argus/Services/Chat"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
)

// fakeRunner answers the first wake with busy and every later one with a
// started run, which is exactly how a fire lands while its agent is mid-chat.
type fakeRunner struct {
	mu    sync.Mutex
	calls int
}

func (f *fakeRunner) SendScheduledWake(agentID, text string, prepared func(Shared.SendResult)) Shared.SendResult {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.calls == 1 {
		return Shared.SendResult{Error: Shared.NewError(Shared.CodeBusy, "The agent is busy")}
	}
	prepared(Shared.SendResult{Ok: true, RunID: "chat-run-1", UserMessageID: "u", AssistantMessageID: "a"})
	return Shared.SendResult{Ok: true, RunID: "chat-run-1", UserMessageID: "u", AssistantMessageID: "a"}
}

func (f *fakeRunner) OnRunFinished(func(servicechat.RunOutcome)) {}

// TestPokeFiresBusyDeferredTaskImmediately pins the contract that freed the
// scheduler from its timer: a fire the agent was too busy to start must run
// the moment the engine is nudged (a run finished, a schedule changed), not
// on the next tick of the sleep it computed while the agent was still busy.
func TestPokeFiresBusyDeferredTaskImmediately(t *testing.T) {
	db, err := Storage.Open(filepath.Join(t.TempDir(), "scheduler.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := db.SaveAgentProfile(Storage.AgentProfile{ID: "default", AgentName: "Argus"}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	if err := db.InsertScheduledTask(Storage.ScheduledTask{
		ID: "task-1", AgentID: "default", Kind: Storage.KindReminder, Title: "Standup nudge",
		Prompt: "Post the standup note", ScheduleJSON: `{}`, NextRunAt: now - 1000, CreatedAt: now - 2000,
	}); err != nil {
		t.Fatal(err)
	}

	runner := &fakeRunner{}
	s := &Service{Core: &Shared.Core{DB: db}, Chat: runner}
	s.ctx, s.cancel = context.WithCancel(context.Background())
	defer s.cancel()
	// Pretend the probe already passed: the sweep must not sit on the
	// offline retry path for a test about wake-ups.
	s.onlineAt = time.Now()
	s.onlineOK = true
	s.wake = make(chan struct{}, 1)

	go s.loop()

	// The first sweep hits the busy agent; the task stays due.
	deadline := time.Now().Add(5 * time.Second)
	for {
		runner.mu.Lock()
		calls := runner.calls
		runner.mu.Unlock()
		if calls >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("scheduler never attempted the due task")
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Without the poke the loop would sit out its 15s busy wait. The nudge —
	// what a finished run raises — must fire the task right away.
	s.poke()
	deadline = time.Now().Add(2 * time.Second)
	for {
		runner.mu.Lock()
		calls := runner.calls
		runner.mu.Unlock()
		if calls >= 2 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("poke did not trigger an immediate re-sweep (calls=%d)", calls)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

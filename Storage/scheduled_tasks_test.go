package Storage

import (
	"path/filepath"
	"testing"
	"time"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func task(id, kind string, next int64) ScheduledTask {
	return ScheduledTask{
		ID: id, AgentID: "default", Kind: kind, Title: "t-" + id, Prompt: "do the thing",
		ScheduleJSON: `{"freq":"daily","time":"08:00"}`, NextRunAt: next,
	}
}

func TestScheduledTaskDueLifecycle(t *testing.T) {
	db := openTestDB(t)
	now := time.Now().UnixMilli()

	if err := db.InsertScheduledTask(task("r1", KindRoutine, now-1000)); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := db.InsertScheduledTask(task("r2", KindRoutine, now+3600_000)); err != nil {
		t.Fatalf("insert future: %v", err)
	}

	due, err := db.DueScheduledTasks(now)
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	if len(due) != 1 || due[0].ID != "r1" {
		t.Fatalf("expected only r1 due, got %+v", due)
	}
	next, err := db.NextDueMs(now)
	if err != nil {
		t.Fatalf("next due: %v", err)
	}
	if next != now+3600_000 {
		t.Fatalf("next due = %d, want %d", next, now+3600_000)
	}

	// advance r1; it must leave the due set
	if err := db.SetTaskSchedule("r1", `{"freq":"daily","time":"09:00"}`, now+7200_000); err != nil {
		t.Fatalf("advance: %v", err)
	}
	due, _ = db.DueScheduledTasks(now)
	if len(due) != 0 {
		t.Fatalf("advanced task still due: %+v", due)
	}
	// and the schedule rewrite persisted
	got, found, err := db.GetScheduledTask("r1")
	if err != nil || !found {
		t.Fatalf("get: found=%v err=%v", found, err)
	}
	if got.NextRunAt != now+7200_000 || got.ScheduleJSON != `{"freq":"daily","time":"09:00"}` {
		t.Fatalf("persisted wrong: %+v", got)
	}
	if got.Paused {
		t.Fatal("new task should not read back as paused")
	}
}

func TestMarkTaskFiredAndRetire(t *testing.T) {
	db := openTestDB(t)
	now := time.Now().UnixMilli()
	if err := db.InsertScheduledTask(task("m1", KindReminder, now-10)); err != nil {
		t.Fatalf("insert: %v", err)
	}
	// a non-retiring mark keeps the task due (a failed attempt still owes work)
	if err := db.MarkTaskFired("m1", now, "connect openai first", false); err != nil {
		t.Fatalf("mark fired: %v", err)
	}
	got, _, _ := db.GetScheduledTask("m1")
	if got.NextRunAt != now-10 {
		t.Fatalf("non-retiring mark changed next_run_at: %d", got.NextRunAt)
	}
	if got.LastRunAt != now || got.LastStatus != "connect openai first" {
		t.Fatalf("last run not recorded: %+v", got)
	}
	due, _ := db.DueScheduledTasks(now)
	if len(due) != 1 {
		t.Fatalf("task should still be due: %+v", due)
	}
	// a retiring mark retires it for good
	if err := db.MarkTaskFired("m1", now, RunSuccess, true); err != nil {
		t.Fatalf("mark retired: %v", err)
	}
	got, _, _ = db.GetScheduledTask("m1")
	if got.NextRunAt != 0 {
		t.Fatalf("retired task keeps next_run_at %d", got.NextRunAt)
	}
	due, _ = db.DueScheduledTasks(now + 1000)
	if len(due) != 0 {
		t.Fatalf("retired task came back as due: %+v", due)
	}
	// a retired reminder is listed last, never as "next"
	all, _ := db.ListScheduledTasks()
	if len(all) != 1 || all[0].NextRunAt != 0 {
		t.Fatalf("list: %+v", all)
	}
}

func TestPausedTaskLeavesDueSet(t *testing.T) {
	db := openTestDB(t)
	now := time.Now().UnixMilli()
	_ = db.InsertScheduledTask(task("p1", KindRoutine, now-10))
	if err := db.SetTaskPaused("p1", true); err != nil {
		t.Fatalf("pause: %v", err)
	}
	due, _ := db.DueScheduledTasks(now)
	if len(due) != 0 {
		t.Fatalf("paused task still due: %+v", due)
	}
	if n, _ := db.NextDueMs(now); n != 0 {
		t.Fatalf("paused task counted in next due: %d", n)
	}
	if err := db.SetTaskPaused("p1", false); err != nil {
		t.Fatalf("resume: %v", err)
	}
	due, _ = db.DueScheduledTasks(now)
	if len(due) != 1 {
		t.Fatalf("resumed task not due again: %+v", due)
	}
}

func TestScheduledRunRowsAndSweep(t *testing.T) {
	db := openTestDB(t)
	now := time.Now().UnixMilli()
	_ = db.InsertScheduledTask(task("run1", KindRoutine, now))
	if err := db.InsertScheduledRun(ScheduledRun{
		ID: "x1", TaskID: "run1", AgentID: "default", MessageID: "msg1",
		Status: RunStarted, StartedAt: now,
	}); err != nil {
		t.Fatalf("insert run: %v", err)
	}
	// an abandoned started row closes as an error at boot
	if err := db.SweepAbandonedRuns(); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	// and finalizing after the sweep still works (the crash-recovery order)
	if err := db.FinishScheduledRun("x1", RunSuccess, "msg1", "", now+5000); err != nil {
		t.Fatalf("finish run: %v", err)
	}
	var status, messageID string
	var completed int64
	if err := db.conn.QueryRow(`SELECT status, message_id, completed_at FROM scheduled_runs WHERE id = 'x1'`).
		Scan(&status, &messageID, &completed); err != nil {
		t.Fatalf("read run: %v", err)
	}
	if status != RunSuccess || messageID != "msg1" || completed != now+5000 {
		t.Fatalf("run row wrong: %s %s %d", status, messageID, completed)
	}
}

func TestDeleteByPrefix(t *testing.T) {
	db := openTestDB(t)
	now := time.Now().UnixMilli()
	_ = db.InsertScheduledTask(task("abcdef123456", KindReminder, now))
	// the tool layer prints short ids and the model quotes them back
	found, err := db.DeleteScheduledTask("abcdef12")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !found {
		t.Fatal("prefix delete matched nothing")
	}
	if _, ok, _ := db.GetScheduledTask("abcdef123456"); ok {
		t.Fatal("task survived its prefix deletion")
	}
	found, _ = db.DeleteScheduledTask("abcdef12")
	if found {
		t.Fatal("second delete reported a row it did not have")
	}
	// a LIKE wildcard in the id must not match everything
	_ = db.InsertScheduledTask(task("zz", KindReminder, now))
	found, _ = db.DeleteScheduledTask("%")
	if found {
		t.Fatal("unescaped % deleted every task")
	}
}

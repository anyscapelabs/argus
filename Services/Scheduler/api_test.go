package Scheduler

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/anyscapelabs/argus/Services/Scheduler/spec"
	"github.com/anyscapelabs/argus/Storage"
)

// openTempDB gives the read path a real database to walk, so the panel's rows
// come from the same SQL the bound Tasks() call runs.
func openTempDB(t *testing.T) *Storage.DB {
	t.Helper()
	db, err := Storage.Open(filepath.Join(t.TempDir(), "tasks.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func insertTask(t *testing.T, db *Storage.DB, id, kind, title string, sp spec.Schedule, next int64, paused bool) {
	t.Helper()
	raw, err := spec.Encode(sp)
	if err != nil {
		t.Fatalf("encode %s: %v", id, err)
	}
	now := time.Now().UnixMilli()
	if err := db.InsertScheduledTask(Storage.ScheduledTask{
		ID: id, AgentID: "default", Kind: kind, Title: title, Prompt: "p-" + title,
		ScheduleJSON: raw, NextRunAt: next, Paused: paused, CreatedAt: now,
	}); err != nil {
		t.Fatalf("insert %s: %v", id, err)
	}
}

func TestScheduledRowsFeedThePanelInFireOrder(t *testing.T) {
	db := openTempDB(t)
	day := 24 * time.Hour
	future := time.Now().Add(day).UnixMilli()
	sooner := time.Now().Add(time.Hour).UnixMilli()
	insertTask(t, db, "row-routine-later", Storage.KindRoutine, "Weekly review",
		spec.Schedule{Freq: spec.FreqWeekly, Weekdays: []int{4}, Time: "17:00"}, future, false)
	insertTask(t, db, "row-routine-soon", Storage.KindRoutine, "Morning brief",
		spec.Schedule{Freq: spec.FreqDaily, Time: "08:00"}, sooner, false)
	insertTask(t, db, "row-routine-paused", Storage.KindRoutine, "Paused thing",
		spec.Schedule{Freq: spec.FreqDaily, Time: "09:00"}, future, true)
	insertTask(t, db, "row-reminder-done", Storage.KindReminder, "Fired reminder",
		spec.Schedule{At: sooner}, 0, false)

	rows, err := db.ListScheduledTasks()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// Exactly what the panel's query builds: every row mapped, retired dropped.
	live := make([]string, 0, len(rows))
	var firstSummary, pausedFlag string
	for _, r := range rows {
		v := taskView(r)
		if v.Retired {
			continue
		}
		if v.Paused {
			pausedFlag = v.Title
		}
		if len(live) == 0 {
			firstSummary = v.Summary
		}
		live = append(live, v.ID)
	}
	if want := []string{"row-routine-soon", "row-routine-later", "row-routine-paused"}; !equalIDs(live, want) {
		t.Errorf("live order = %v, want soonest fire first with the paused row still listed", live)
	}
	if firstSummary != "every day at 08:00" {
		t.Errorf("first summary = %q, want the rhythm the panel prints", firstSummary)
	}
	if pausedFlag != "Paused thing" {
		t.Errorf("paused row lost its flag: %q", pausedFlag)
	}
}

func equalIDs(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestTaskViewShowsRoutineRhythm(t *testing.T) {
	sp := spec.Schedule{Freq: spec.FreqWeekdays, Time: "08:30"}
	raw, err := spec.Encode(sp)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	v := taskView(Storage.ScheduledTask{
		ID: "id-1", Kind: Storage.KindRoutine, Title: "Morning brief", Prompt: "brief me",
		ScheduleJSON: raw, NextRunAt: 1_800_000_000_000,
	})
	if v.Summary != "every weekday at 08:30" {
		t.Errorf("summary = %q, want the weekday rhythm", v.Summary)
	}
	if v.Retired {
		t.Error("a routine with a next fire is not retired")
	}
	if v.NextRunAt != 1_800_000_000_000 {
		t.Errorf("next_run_at = %d, want the stored fire time", v.NextRunAt)
	}
}

func TestTaskViewRetiresAFiredReminder(t *testing.T) {
	raw, err := spec.Encode(spec.Schedule{At: 1_700_000_000_000})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	v := taskView(Storage.ScheduledTask{
		Kind: Storage.KindReminder, Title: "Call", Prompt: "call grandma",
		ScheduleJSON: raw, NextRunAt: 0, LastRunAt: 1_700_000_000_000, LastStatus: "success",
	})
	if !v.Retired {
		t.Fatal("a reminder with no next fire must be reported retired")
	}
	if v.LastStatus != "success" {
		t.Errorf("last_status = %q, want it forwarded so the panel can trust it", v.LastStatus)
	}
}

// A row the panel cannot read is still a row the user must be able to see and
// cancel, so an unreadable schedule must not drop it or fail the call.
func TestTaskViewSurvivesUnreadableSchedule(t *testing.T) {
	v := taskView(Storage.ScheduledTask{
		Kind: Storage.KindRoutine, Title: "Broken", Prompt: "p",
		ScheduleJSON: "{not json", NextRunAt: 5,
	})
	if v.Summary != "" {
		t.Errorf("summary = %q, want empty for an unreadable spec", v.Summary)
	}
	if v.Title != "Broken" || v.Retired {
		t.Errorf("row lost its identity: %+v", v)
	}
}

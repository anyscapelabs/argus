package Chat

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/anyscapelabs/argus/Protocol"
	schedulespec "github.com/anyscapelabs/argus/Services/Scheduler/spec"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
)

// runScheduleReminderTool creates a one-time reminder.
func runScheduleReminderTool(
	s *Service,
	ctx context.Context,
	_ string,
	agentID string,
	_ string,
	call Protocol.ToolCall,
	result resultFn,
	activity activityFn,
) Protocol.Block {
	if Shared.IsScheduledFire(ctx) {
		return result("This is a scheduled task firing now, not a request to create another reminder. Execute the stored instruction and report its result.", true)
	}
	var input struct {
		Title  string `json:"title"`
		Prompt string `json:"prompt"`
		At     string `json:"at"`
	}
	if err := json.Unmarshal([]byte(call.Input), &input); err != nil {
		return result("invalid tool input: could not parse the schedule_reminder arguments", true)
	}
	title := strings.TrimSpace(input.Title)
	prompt := strings.TrimSpace(input.Prompt)
	if title == "" || prompt == "" {
		return result("invalid tool input: title and prompt are both required", true)
	}
	at, perr := parseWhen(input.At)
	if perr != nil {
		return result("invalid 'at': "+perr.Error()+" — call get_time_and_location and pass an absolute local future time.", true)
	}
	if at.Before(time.Now()) {
		return result("that time is already in the past: "+at.Format("Mon 2 Jan 15:04")+". Call get_time_and_location and pick a future moment.", true)
	}
	sp := schedulespec.Schedule{At: at.UnixMilli()}
	if err := sp.Validate(); err != nil {
		return result("invalid schedule: "+err.Error(), true)
	}
	encoded, err := schedulespec.Encode(sp)
	if err != nil {
		return result("could not encode the schedule: "+err.Error(), true)
	}
	existing, dup, derr := s.Core.DB.FindDuplicateTask(agentID, Storage.KindReminder, title, prompt, encoded)
	if derr != nil {
		return result("could not check the existing reminders: "+derr.Error(), true)
	}
	if dup {
		activity("tool_finished", title)
		return result(fmt.Sprintf("Reminder %q for %s already exists (id %s); nothing was added.", title, whenLabel(sp.At), shortID(existing.ID)), false)
	}
	id, _ := uuid.NewV7()
	task := Storage.ScheduledTask{
		ID: id.String(), AgentID: agentID, Kind: Storage.KindReminder,
		Title: title, Prompt: prompt, ScheduleJSON: encoded,
		NextRunAt: sp.At, CreatedAt: nowUnixMs(),
	}
	activity("tool_started", title)
	if err := s.Core.DB.InsertScheduledTask(task); err != nil {
		activity("tool_finished", title)
		return result("could not save the reminder: "+err.Error(), true)
	}
	s.Core.EmitEvent(Shared.EventScheduleUpdated, nil)
	activity("tool_finished", title)
	return result(fmt.Sprintf("Reminder %q set for %s (id %s). It fires even if this chat is closed; if the machine is asleep or offline it fires as soon as it is back.",
		title, whenLabel(sp.At), shortID(id.String())), false)
}

// runCreateRoutineTool creates a recurring routine.
func runCreateRoutineTool(
	s *Service,
	ctx context.Context,
	_ string,
	agentID string,
	_ string,
	call Protocol.ToolCall,
	result resultFn,
	activity activityFn,
) Protocol.Block {
	if Shared.IsScheduledFire(ctx) {
		return result("This is a scheduled task firing now, not a request to create another routine. Execute the stored instruction and report its result.", true)
	}
	var input struct {
		Title        string `json:"title"`
		Prompt       string `json:"prompt"`
		Freq         string `json:"freq"`
		Time         string `json:"time"`
		Weekdays     []int  `json:"weekdays"`
		Day          int    `json:"day"`
		EveryMinutes int    `json:"every_minutes"`
	}
	if err := json.Unmarshal([]byte(call.Input), &input); err != nil {
		return result("invalid tool input: could not parse the create_routine arguments", true)
	}
	title := strings.TrimSpace(input.Title)
	prompt := strings.TrimSpace(input.Prompt)
	if title == "" || prompt == "" {
		return result("invalid tool input: title and prompt are both required", true)
	}
	sp := schedulespec.Schedule{
		Freq: strings.TrimSpace(strings.ToLower(input.Freq)), Time: strings.TrimSpace(input.Time),
		Weekdays: input.Weekdays, Day: input.Day, EveryMinutes: input.EveryMinutes,
	}
	// Everything but interval fires at a wall-clock time, so HH:MM is
	// mandatory; the spec's own validation only covers what it can share
	// with one-off reminders.
	if sp.Freq != schedulespec.FreqInterval && strings.TrimSpace(sp.Time) == "" {
		return result("invalid routine: freq "+sp.Freq+" needs a time as HH:MM", true)
	}
	if sp.Freq == schedulespec.FreqMonthly && sp.Day == 0 {
		sp.Day = time.Now().Day()
	}
	if err := sp.Validate(); err != nil {
		return result("invalid routine: "+err.Error(), true)
	}
	nowMs := time.Now().UnixMilli()
	next, err := sp.NextAfter(nowMs, nowMs, time.Local)
	if err != nil {
		return result("could not compute the next fire: "+err.Error(), true)
	}
	encoded, err := schedulespec.Encode(sp)
	if err != nil {
		return result("could not encode the schedule: "+err.Error(), true)
	}
	existing, dup, derr := s.Core.DB.FindDuplicateTask(agentID, Storage.KindRoutine, title, prompt, encoded)
	if derr != nil {
		return result("could not check the existing routines: "+derr.Error(), true)
	}
	if dup {
		activity("tool_finished", title)
		return result(fmt.Sprintf("Routine %q (%s) already exists (id %s); nothing was added.", title, sp.Summary(), shortID(existing.ID)), false)
	}
	// Twice — the exact-text duplicate above — the model's own words are
	// never a clean match for themselves. Titles drift a little every time
	// ("Morning news for Zimbabwe" vs "Zimbabwe morning news"); the times
	// a job runs do not. The guard below treats the same normalized title
	// with an identically firing schedule as one job and refuses to stack
	// a third copy, which is what stopped a 9am fire from being answered
	// with "I've set up a routine" instead of the work.
	if clash, found := s.RoutineAlreadySpoken(agentID, title, sp); found {
		summary := sp.Summary()
		if old, aerr := schedulespec.Decode(clash.ScheduleJSON); aerr == nil {
			summary = old.Summary()
		}
		activity("tool_finished", title)
		return result(fmt.Sprintf(
			"A routine named %q (id %s) already fires %s: restating it as a new create_routine does nothing the user asked for — the task, not a schedule, is the thing to act on. If the user genuinely wants that schedule's instruction changed, say in plain language what would change and wait for them to confirm; for now nothing was added.",
			clash.Title, shortID(clash.ID), summary), false)
	}
	id, _ := uuid.NewV7()
	task := Storage.ScheduledTask{
		ID: id.String(), AgentID: agentID, Kind: Storage.KindRoutine,
		Title: title, Prompt: prompt, ScheduleJSON: encoded,
		NextRunAt: next, CreatedAt: nowMs,
	}
	activity("tool_started", title)
	if err := s.Core.DB.InsertScheduledTask(task); err != nil {
		activity("tool_finished", title)
		return result("could not save the routine: "+err.Error(), true)
	}
	s.Core.EmitEvent(Shared.EventScheduleUpdated, nil)
	activity("tool_finished", title)
	return result(fmt.Sprintf("Routine %q created (%s), first fire %s (id %s). Missed fires catch up when the machine is back; a fire older than its own period is skipped, not stacked.",
		title, sp.Summary(), whenLabel(next), shortID(id.String())), false)
}

// runListScheduledTool lists every reminder and routine.
func runListScheduledTool(
	s *Service,
	_ context.Context,
	_ string,
	_ string,
	_ string,
	_ Protocol.ToolCall,
	result resultFn,
	_ activityFn,
) Protocol.Block {
	tasks, err := s.Core.DB.ListScheduledTasks()
	if err != nil {
		return result("could not read the schedule: "+err.Error(), true)
	}
	if len(tasks) == 0 {
		return result("Nothing is scheduled.", false)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d scheduled task(s):\n", len(tasks))
	for _, t := range tasks {
		next := "retired (already fired)"
		if t.Paused {
			next = "paused"
		} else if t.NextRunAt > 0 {
			next = whenLabel(t.NextRunAt)
		}
		last := ""
		if t.LastRunAt > 0 {
			last = ", last run " + whenLabel(t.LastRunAt)
			if t.LastStatus != "" {
				last += " (" + t.LastStatus + ")"
			}
		}
		fmt.Fprintf(&b, "- [%s] %s (id %s) — %s — next: %s%s\n", t.Kind, t.Title, shortID(t.ID), t.Prompt, next, last)
	}
	return result(b.String(), false)
}

// runCancelScheduledTool deletes a scheduled task by id.
func runCancelScheduledTool(
	s *Service,
	_ context.Context,
	_ string,
	_ string,
	_ string,
	call Protocol.ToolCall,
	result resultFn,
	activity activityFn,
) Protocol.Block {
	var input struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal([]byte(call.Input), &input); err != nil {
		return result("invalid tool input: could not parse the cancel_scheduled arguments", true)
	}
	id := strings.TrimSpace(input.TaskID)
	if id == "" {
		return result("invalid tool input: task_id is required", true)
	}
	activity("tool_started", id)
	found, err := s.Core.DB.DeleteScheduledTask(id)
	if err != nil {
		return result("could not delete the schedule: "+err.Error(), true)
	}
	if !found {
		return result("No scheduled task with id \""+id+"\". Use list_scheduled to see the real ids.", true)
	}
	s.Core.EmitEvent(Shared.EventScheduleUpdated, nil)
	return result("Cancelled.", false)
}

// parseWhen reads the times the schedule tools accept. A bare
// "2026-11-03T09:00" is the user's wall clock; an offset in RFC3339 wins if
// one is given.
func parseWhen(raw string) (time.Time, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty time")
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	for _, layout := range []string{"2006-01-02T15:04", "2006-01-02 15:04"} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("%q is not RFC3339 or 2006-01-02T15:04", raw)
}

// whenLabel is how fire times read back to the model and the user.
func whenLabel(ms int64) string {
	t := time.UnixMilli(ms)
	layout := "Mon 2 Jan 15:04"
	if t.Year() != time.Now().Year() {
		layout += " 2006"
	}
	return t.Format(layout)
}

// shortID keeps confirmation strings readable while leaving the tool result
// actionable: the list tool prints the same prefix and cancel tolerates it.
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// RoutineAlreadySpoken looks for a live routine owned by agentID whose title
// reads the same ignoring case and punctuation and whose schedule would fire
// identically. Schedules, unlike instructions, cannot drift into ambiguity,
// so equality there is the honest signal the job already exists.
func (s *Service) RoutineAlreadySpoken(agentID, title string, sp schedulespec.Schedule) (Storage.ScheduledTask, bool) {
	tasks, err := s.Core.DB.ListScheduledTasks()
	if err != nil {
		return Storage.ScheduledTask{}, false
	}
	want := normRoutineTitle(title)
	for _, task := range tasks {
		if task.Kind != Storage.KindRoutine || task.AgentID != agentID {
			continue
		}
		existing, derr := schedulespec.Decode(task.ScheduleJSON)
		if derr != nil || !existing.SameAs(sp) {
			continue
		}
		if normRoutineTitle(task.Title) == want {
			return task, true
		}
	}
	return Storage.ScheduledTask{}, false
}

// normRoutineTitle strips everything a model's phrasing differs in — case,
// punctuation, extra whitespace — leaving bare words to compare.
func normRoutineTitle(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

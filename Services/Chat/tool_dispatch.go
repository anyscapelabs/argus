package Chat

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Services/Location"
	"github.com/anyscapelabs/argus/Services/Memory"
	"github.com/anyscapelabs/argus/Services/Permission"
	schedulespec "github.com/anyscapelabs/argus/Services/Scheduler/spec"
	"github.com/anyscapelabs/argus/Services/Search"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Services/Terminal"
	"github.com/anyscapelabs/argus/Storage"
	"github.com/google/uuid"
	"strings"
	"time"
	"unicode"
)

// runTool executes one tool_use block and returns the tool_result block the
// model should see. Permission gating happens before anything executes.
func (s *Service) runTool(ctx context.Context, runID, agentID, agentName string, call Protocol.ToolCall) Protocol.Block {
	result := func(text string, isError bool) Protocol.Block {
		limit := maxResultChars(call.Name)
		if n := len(text); n > limit {
			head := strings.ToValidUTF8(text[:limit], "")
			text = head + fmt.Sprintf("\n[…tool output truncated: showing %d of %d characters]", len(head), n)
		}
		return Protocol.Block{
			Type:      Protocol.BlockToolResult,
			ToolRefID: call.ID,
			ToolName:  call.Name,
			Text:      text,
			IsError:   isError,
		}
	}
	// activity emits a live status the chat shows while nothing else streams.
	activity := func(kind, command string) {
		s.Core.EmitEvent(Shared.EventActivity, Shared.ActivityView{
			AgentID: agentID,
			RunID:   runID,
			Kind:    kind,
			Tool:    call.Name,
			Command: command,
		})
	}

	if !call.Valid {
		return result(invalidCallMessage(call.Name), true)
	}

	if call.Name == "get_time_and_location" {
		activity("tool_started", "")
		info := Location.Current(ctx, s.Core.DB)
		activity("tool_finished", "")
		return result(info.ToolResult(), false)
	}

	if call.Name == "conversation_read" {
		return s.runConversationRead(agentID, call, result, activity)
	}

	if call.Name == "remember" {
		var input struct {
			Key   string `json:"key"`
			Value string `json:"value"`
		}
		if err := json.Unmarshal([]byte(call.Input), &input); err != nil {
			return result("invalid tool input: could not parse the remember arguments", true)
		}
		k, kerr := Memory.NormalizeKey(input.Key)
		if kerr != nil {
			return result("invalid memory key: "+kerr.Error(), true)
		}
		activity("tool_started", k)
		if err := Memory.Save(s.Core.DB, input.Key, input.Value); err != nil {
			activity("tool_finished", k)
			return result("could not save the memory: "+err.Error(), true)
		}
		activity("tool_finished", k)
		return result("Saved under key \""+k+"\". It will appear in your context from now on.", false)
	}

	if call.Name == "forget" {
		var input struct {
			Key string `json:"key"`
		}
		if err := json.Unmarshal([]byte(call.Input), &input); err != nil {
			return result("invalid tool input: could not parse the forget arguments", true)
		}
		k, err := Memory.NormalizeKey(input.Key)
		if err != nil {
			return result("invalid memory key: "+err.Error(), true)
		}
		activity("tool_started", k)
		found, err := Memory.Forget(s.Core.DB, input.Key)
		if err != nil {
			activity("tool_finished", k)
			return result("could not delete the memory: "+err.Error(), true)
		}
		if !found {
			activity("tool_finished", k)
			return result("No memory is stored under \""+k+"\". Nothing was deleted.", true)
		}
		activity("tool_finished", k)
		return result("Forgot \""+k+"\". It is gone from future context.", false)
	}

	if call.Name == "schedule_reminder" {
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

	if call.Name == "create_routine" {
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

	if call.Name == "list_scheduled" {
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

	if call.Name == "cancel_scheduled" {
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

	if call.Name == "write_artifact" {
		return s.runArtifactTool(call, agentID, result, activity)
	}

	if call.Name == "read_file" || call.Name == "write_file" || call.Name == "grep_files" {
		return s.runFileTool(ctx, call, result, activity, agentID, agentName)
	}

	if call.Name == "web_search" {
		var input struct {
			Query      string `json:"query"`
			MaxResults int    `json:"max_results"`
		}
		if err := json.Unmarshal([]byte(call.Input), &input); err != nil {
			return result("invalid tool input: could not parse the web_search arguments", true)
		}
		query := strings.TrimSpace(input.Query)
		if query == "" {
			return result("invalid tool input: query is required", true)
		}

		activity("tool_started", query)
		answer := Search.Search(ctx, query, input.MaxResults)
		// The indicator consumes the found sites right away (favicon plus
		// result titles) instead of echoing the query back to the user.
		s.Core.EmitEvent(Shared.EventActivity, Shared.ActivityView{
			AgentID: agentID,
			RunID:   runID,
			Kind:    "tool_results",
			Tool:    call.Name,
			Sources: activitySources(answer.Results),
		})
		activity("tool_finished", query)
		return result(answer.Render(), answer.Err != "")
	}

	if call.Name == "browser" {
		return s.runBrowser(ctx, runID, agentID, agentName, call, result, activity)
	}

	if call.Name == "connect_mcp" {
		var input struct {
			ServerID string `json:"server_id"`
		}
		if err := json.Unmarshal([]byte(call.Input), &input); err != nil {
			return result("invalid tool input: could not parse the connect_mcp arguments", true)
		}
		switch s.MCPConnect.Register(agentID, agentName, strings.TrimSpace(input.ServerID)) {
		case "shown":
			return result("A connection card is on screen. The user must press Connect and finish sign-in in their browser. Do not say it is connected; stop and wait — the app gives you a new turn when it settles.", false)
		case "pending":
			return result("A connection card is already waiting for the user. Do not request another.", false)
		case "connected":
			return result("That integration is already connected. Use its tools.", false)
		default:
			return result("Unknown integration id. Valid ids: "+strings.Join(s.integrationIDs(), ", ")+".", true)
		}
	}

	if s.MCP != nil {
		for _, t := range s.MCP.ListTools() {
			if mcpToolName(t.ServerID, t.Name) == call.Name {
				var args map[string]interface{}
				if strings.TrimSpace(call.Input) != "" {
					_ = json.Unmarshal([]byte(call.Input), &args)
				}
				argsJSON, _ := json.Marshal(args)
				out, err := s.MCP.CallTool(t.ServerID, t.Name, string(argsJSON))
				if err != nil {
					return result("integration call failed: "+err.Error(), true)
				}
				if out == "" {
					out = "{}"
				}
				return result(out, false)
			}
		}
	}

	if call.Name != Permission.TerminalTool {
		return result("unknown tool: "+call.Name, true)
	}

	var input struct {
		Command        string `json:"command"`
		TimeoutSeconds int    `json:"timeout_seconds"`
	}
	if err := json.Unmarshal([]byte(call.Input), &input); err != nil {
		return result("invalid tool input: could not parse the command arguments", true)
	}
	command := strings.TrimSpace(input.Command)
	if command == "" {
		return result("invalid tool input: command is required", true)
	}

	decision, err := Permission.Request(ctx, s.Permissions, agentID, agentName, Permission.TerminalTool, command)
	if err != nil {
		return result("the permission request was cancelled", true)
	}
	if decision == Permission.Deny {
		return result("The user declined terminal access for this command. Do not retry it; continue without running commands.", true)
	}

	activity("tool_started", command)
	res := Terminal.Run(ctx, command, input.TimeoutSeconds)
	activity("tool_finished", command)
	var b strings.Builder
	b.WriteString(fmt.Sprintf("exit code: %d\n", res.ExitCode))
	if res.DurationMs > 0 {
		b.WriteString(fmt.Sprintf("duration: %d ms\n", res.DurationMs))
	}
	b.WriteString("\n")
	b.WriteString(res.Output)
	text := b.String()
	if res.Err != "" {
		return result("command failed: "+res.Err+"\n"+res.Output, true)
	}
	if res.TimedOut {
		return result(text, true)
	}
	return result(text, false)
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

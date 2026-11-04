// Bound surface of the scheduler: what the frontend may call.
//
// Read and pause only. Creating a routine means writing the prompt the agent
// will later run, and that is the model's job — it is the agent's tools
// (create_routine / schedule_reminder) that set schedules, while the panel
// lists them and decides whether one is allowed to fire.
package Scheduler

import (
	"strings"

	"github.com/anyscapelabs/argus/Services/Scheduler/spec"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
)

// Tasks lists the schedules the agent set. Live ones come first, soonest fire
// first; reminders that already ran are returned as retired so the panel can
// drop them without the backend deciding what the user should not see.
func (s *Service) Tasks() Shared.ScheduledTasksResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.ScheduledTasksResult{Error: aerr}
	}
	rows, err := s.Core.DB.ListScheduledTasks()
	if err != nil {
		return Shared.ScheduledTasksResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	out := make([]Shared.ScheduledTaskView, 0, len(rows))
	for _, t := range rows {
		out = append(out, taskView(t))
	}
	return Shared.ScheduledTasksResult{Tasks: out}
}

// SetPaused holds a schedule back without cancelling it. A paused task leaves
// the engine's due scan entirely, and on resume a routine that slept through
// its own period is coalesced forward rather than firing all its missed runs.
func (s *Service) SetPaused(taskID string, paused bool) Shared.ActionResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.ActionResult{Error: aerr}
	}
	id := strings.TrimSpace(taskID)
	if id == "" {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeBadInput, "no scheduled task was given")}
	}
	t, found, err := s.Core.DB.GetScheduledTask(id)
	if err != nil {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	if !found {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeNotFound, "no scheduled task with that id")}
	}
	// A retired reminder has no next fire to hold back; pausing it would put
	// the panel in a state the engine can never leave.
	if t.NextRunAt == 0 {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeBadInput, "that reminder has already fired")}
	}
	if err := s.Core.DB.SetTaskPaused(id, paused); err != nil {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	s.notifyChanged()
	return Shared.ActionResult{Ok: true}
}

// taskView turns a stored task into the panel's row. A schedule whose JSON the
// spec cannot read keeps an empty summary: the row must still show so the user
// can see and cancel a broken schedule.
func taskView(t Storage.ScheduledTask) Shared.ScheduledTaskView {
	summary := ""
	if sp, err := spec.Decode(t.ScheduleJSON); err == nil {
		summary = sp.Summary()
	}
	return Shared.ScheduledTaskView{
		ID:         t.ID,
		AgentID:    t.AgentID,
		Kind:       t.Kind,
		Title:      t.Title,
		Prompt:     t.Prompt,
		Summary:    summary,
		NextRunAt:  t.NextRunAt,
		LastRunAt:  t.LastRunAt,
		LastStatus: t.LastStatus,
		Paused:     t.Paused,
		Retired:    t.NextRunAt == 0,
		CreatedAt:  t.CreatedAt,
	}
}

// notifyChanged tells the frontend the schedule table moved, so the routines
// panel refetches instead of showing a fire time the engine already spent.
func (s *Service) notifyChanged() {
	s.Core.EmitEvent(Shared.EventScheduleUpdated, nil)
}

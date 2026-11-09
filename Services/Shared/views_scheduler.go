package Shared

// ScheduledTaskView is one row of the routines panel. Times are unix
// milliseconds so the panel can render them in the user's own calendar
// ("Today at 8:00am") instead of a string the backend chose for them.
type ScheduledTaskView struct {
	ID         string `json:"id"`
	AgentID    string `json:"agent_id"`
	Kind       string `json:"kind"` // routine | reminder
	Title      string `json:"title"`
	Prompt     string `json:"prompt"`
	Summary    string `json:"summary"`
	NextRunAt  int64  `json:"next_run_at"`
	LastRunAt  int64  `json:"last_run_at"`
	LastStatus string `json:"last_status,omitempty"`
	Paused     bool   `json:"paused"`
	// Retired means a one-off reminder already ran: it has no next fire and is
	// history rather than a live schedule.
	Retired   bool  `json:"retired"`
	CreatedAt int64 `json:"created_at"`
}

type ScheduledTasksResult struct {
	Tasks []ScheduledTaskView `json:"tasks"`
	Error *ActionError        `json:"error,omitempty"`
}

// AgentStatusView is the small dashboard the right panel's Details tab renders.
// StartedAtMs is the app process start (uptime reads off it), LastActiveMs is
// the moment the agent last finished a turn (0 = never), Busy says a run is
// on right now so the row can read Working instead of Online.

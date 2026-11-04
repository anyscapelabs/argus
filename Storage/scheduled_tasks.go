package Storage

import (
	"database/sql"
	"fmt"
	"strings"
)

// Task kinds. A routine repeats; a reminder fires once and goes inactive.
const (
	KindRoutine  = "routine"
	KindReminder = "reminder"
)

// Run statuses recorded in scheduled_runs and last_status.
const (
	RunStarted = "started"
	RunSuccess = "success"
	RunError   = "error"
)

// ScheduledTask is one routine or reminder. ScheduleJSON is the spec-package
// encoding of the recurrence; NextRunAt is the wall-clock ms of the next
// fire, or 0 once a reminder has fired and retired.
type ScheduledTask struct {
	ID           string
	AgentID      string
	Kind         string
	Title        string
	Prompt       string
	ScheduleJSON string
	NextRunAt    int64
	LastRunAt    int64
	LastStatus   string
	Paused       bool
	CreatedAt    int64
	UpdatedAt    int64
}

// ScheduledRun is one execution of a task, tracked so the engine can tie the
// assistant message the agent loop produced back to the schedule that asked
// for it.
type ScheduledRun struct {
	ID          string
	TaskID      string
	AgentID     string
	MessageID   string
	Status      string
	Error       string
	StartedAt   int64
	CompletedAt int64
}

func (db *DB) InsertScheduledTask(t ScheduledTask) error {
	if t.ID == "" {
		return fmt.Errorf("storage: scheduled task id is required")
	}
	if t.CreatedAt == 0 {
		t.CreatedAt = nowUnixMs()
	}
	t.UpdatedAt = t.CreatedAt
	_, err := db.conn.Exec(
		`INSERT INTO scheduled_tasks (id, agent_id, kind, title, prompt, schedule_json, next_run_at, last_run_at, last_status, paused, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		t.ID, t.AgentID, t.Kind, t.Title, t.Prompt, t.ScheduleJSON, t.NextRunAt, t.LastRunAt, t.LastStatus, boolInt(t.Paused), t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("storage: scheduled task insert failed: %w", err)
	}
	return nil
}

const scheduledTaskColumns = `id, agent_id, kind, title, prompt, schedule_json, next_run_at, last_run_at, last_status, paused, created_at, updated_at`

func scanScheduledTask(row *sql.Row) (ScheduledTask, error) {
	var t ScheduledTask
	var paused int
	err := row.Scan(&t.ID, &t.AgentID, &t.Kind, &t.Title, &t.Prompt, &t.ScheduleJSON,
		&t.NextRunAt, &t.LastRunAt, &t.LastStatus, &paused, &t.CreatedAt, &t.UpdatedAt)
	t.Paused = paused != 0
	return t, err
}

func (db *DB) GetScheduledTask(id string) (ScheduledTask, bool, error) {
	row := db.conn.QueryRow(
		`SELECT `+scheduledTaskColumns+` FROM scheduled_tasks WHERE id = ?`, id)
	t, err := scanScheduledTask(row)
	if err == sql.ErrNoRows {
		return ScheduledTask{}, false, nil
	}
	if err != nil {
		return ScheduledTask{}, false, fmt.Errorf("storage: scheduled task get failed: %w", err)
	}
	return t, true, nil
}

// FindDuplicateTask returns an identical live task for the agent, if any —
// same agent, kind, title, prompt, and recurrence. The model occasionally
// retries a create call, and an exact repeat must resolve to the row that
// already exists, not a second one.
func (db *DB) FindDuplicateTask(agentID, kind, title, prompt, scheduleJSON string) (ScheduledTask, bool, error) {
	row := db.conn.QueryRow(
		`SELECT `+scheduledTaskColumns+` FROM scheduled_tasks
		 WHERE agent_id = ? AND kind = ? AND title = ? AND prompt = ? AND schedule_json = ?
		 ORDER BY CASE WHEN next_run_at = 0 THEN 9223372036854775807 ELSE next_run_at END, created_at
		 LIMIT 1`, agentID, kind, title, prompt, scheduleJSON)
	t, err := scanScheduledTask(row)
	if err == sql.ErrNoRows {
		return ScheduledTask{}, false, nil
	}
	if err != nil {
		return ScheduledTask{}, false, fmt.Errorf("storage: scheduled task duplicate lookup failed: %w", err)
	}
	return t, true, nil
}

// ListScheduledTasks returns every task, soonest fire first. The agent only
// ever sees its own schedules through the tool layer, so it lists them all.
func (db *DB) ListScheduledTasks() ([]ScheduledTask, error) {
	rows, err := db.conn.Query(
		`SELECT ` + scheduledTaskColumns + ` FROM scheduled_tasks
		 ORDER BY CASE WHEN next_run_at = 0 THEN 9223372036854775807 ELSE next_run_at END, created_at`)
	if err != nil {
		return nil, fmt.Errorf("storage: scheduled task list failed: %w", err)
	}
	defer rows.Close()
	var out []ScheduledTask
	for rows.Next() {
		var t ScheduledTask
		var paused int
		if err := rows.Scan(&t.ID, &t.AgentID, &t.Kind, &t.Title, &t.Prompt, &t.ScheduleJSON,
			&t.NextRunAt, &t.LastRunAt, &t.LastStatus, &paused, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("storage: scheduled task scan failed: %w", err)
		}
		t.Paused = paused != 0
		out = append(out, t)
	}
	return out, rows.Err()
}

// DueScheduledTasks returns active tasks whose fire time has passed, oldest
// due first. next_run_at = 0 marks a retired reminder and never comes back.
func (db *DB) DueScheduledTasks(nowMs int64) ([]ScheduledTask, error) {
	rows, err := db.conn.Query(
		`SELECT `+scheduledTaskColumns+` FROM scheduled_tasks
		 WHERE paused = 0 AND next_run_at > 0 AND next_run_at <= ?
		 ORDER BY next_run_at LIMIT 20`, nowMs)
	if err != nil {
		return nil, fmt.Errorf("storage: due scan failed: %w", err)
	}
	defer rows.Close()
	var out []ScheduledTask
	for rows.Next() {
		var t ScheduledTask
		var paused int
		if err := rows.Scan(&t.ID, &t.AgentID, &t.Kind, &t.Title, &t.Prompt, &t.ScheduleJSON,
			&t.NextRunAt, &t.LastRunAt, &t.LastStatus, &paused, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, fmt.Errorf("storage: due scan failed: %w", err)
		}
		t.Paused = paused != 0
		out = append(out, t)
	}
	return out, rows.Err()
}

// NextDueMs is the soonest future fire across active tasks, 0 when none. The
// engine uses it to arm its timer instead of busy-polling.
func (db *DB) NextDueMs(nowMs int64) (int64, error) {
	var next sql.NullInt64
	err := db.conn.QueryRow(
		`SELECT MIN(next_run_at) FROM scheduled_tasks WHERE paused = 0 AND next_run_at > ?`, nowMs).Scan(&next)
	if err != nil {
		return 0, fmt.Errorf("storage: next due lookup failed: %w", err)
	}
	if !next.Valid {
		return 0, nil
	}
	return next.Int64, nil
}

// SetTaskSchedule rewrites the recurrence and the next fire time in one go —
// the engine advances a routine before firing so it can never double-run, and
// the schedule is never left pointing at a moment already handled.
func (db *DB) SetTaskSchedule(id, scheduleJSON string, nextRunAt int64) error {
	_, err := db.conn.Exec(
		`UPDATE scheduled_tasks SET schedule_json = ?, next_run_at = ?, updated_at = ? WHERE id = ?`,
		scheduleJSON, nextRunAt, nowUnixMs(), id)
	if err != nil {
		return fmt.Errorf("storage: schedule update failed: %w", err)
	}
	return nil
}

// MarkTaskFired records that a task just ran. Retired is true for reminders,
// which then leave the due set permanently (next_run_at = 0).
func (db *DB) MarkTaskFired(id string, firedAtMs int64, status string, retired bool) error {
	set := `last_run_at = ?, last_status = ?, updated_at = ?`
	args := []any{firedAtMs, status, nowUnixMs()}
	if retired {
		set += `, next_run_at = 0`
	}
	_, err := db.conn.Exec(`UPDATE scheduled_tasks SET `+set+` WHERE id = ?`, append(args, id)...)
	if err != nil {
		return fmt.Errorf("storage: fired mark failed: %w", err)
	}
	return nil
}

func (db *DB) SetTaskPaused(id string, paused bool) error {
	_, err := db.conn.Exec(`UPDATE scheduled_tasks SET paused = ?, updated_at = ? WHERE id = ?`,
		boolInt(paused), nowUnixMs(), id)
	if err != nil {
		return fmt.Errorf("storage: pause update failed: %w", err)
	}
	return nil
}

// DeleteScheduledTask accepts a full id or an unambiguous prefix — the tool
// layer prints short ids, and the model quotes back what it saw.
func (db *DB) DeleteScheduledTask(id string) (bool, error) {
	res, err := db.conn.Exec(`DELETE FROM scheduled_tasks WHERE id = ? OR id LIKE ? ESCAPE '\'`,
		id, escapeLike(id)+"%")
	if err != nil {
		return false, fmt.Errorf("storage: scheduled task delete failed: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("storage: scheduled task delete failed: %w", err)
	}
	return n > 0, nil
}

func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// InsertScheduledRun opens a run row and returns its id, so the engine can
// finalize it when the agent loop reports the run finished.
func (db *DB) InsertScheduledRun(r ScheduledRun) error {
	if r.ID == "" {
		return fmt.Errorf("storage: scheduled run id is required")
	}
	if r.StartedAt == 0 {
		r.StartedAt = nowUnixMs()
	}
	_, err := db.conn.Exec(
		`INSERT INTO scheduled_runs (id, task_id, agent_id, message_id, status, error, started_at, completed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.TaskID, r.AgentID, r.MessageID, r.Status, r.Error, r.StartedAt, nullInt64(r.CompletedAt))
	if err != nil {
		return fmt.Errorf("storage: scheduled run insert failed: %w", err)
	}
	return nil
}

func (db *DB) FinishScheduledRun(id, status, messageID, errorMessage string, completedAtMs int64) error {
	if completedAtMs == 0 {
		completedAtMs = nowUnixMs()
	}
	_, err := db.conn.Exec(
		`UPDATE scheduled_runs SET status = ?, message_id = ?, error = ?, completed_at = ? WHERE id = ?`,
		status, messageID, errorMessage, completedAtMs, id)
	if err != nil {
		return fmt.Errorf("storage: scheduled run finish failed: %w", err)
	}
	return nil
}

// SweepAbandonedRuns closes run rows left "started" by a crash or quit; the
// scheduler calls it at startup, same as the chat loop sweeps its messages.
func (db *DB) SweepAbandonedRuns() error {
	_, err := db.conn.Exec(
		`UPDATE scheduled_runs SET status = 'error', error = 'the app stopped before this run finished', completed_at = ?
		 WHERE status = 'started'`, nowUnixMs())
	if err != nil {
		return fmt.Errorf("storage: scheduled run sweep failed: %w", err)
	}
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

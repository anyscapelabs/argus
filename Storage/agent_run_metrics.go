package Storage

import (
	"database/sql"
	"time"
)

// AgentRunMetric is the per-run telemetry row: rounds, token counts, tool
// calls and wall time, written from the run loop's deferred finalizer so a
// crashed run still leaves what it got through.
type AgentRunMetric struct {
	RunID        string `json:"run_id"`
	AgentID      string `json:"agent_id"`
	ThreadID     string `json:"thread_id"`
	Rounds       int    `json:"rounds"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	ToolCalls    int    `json:"tool_calls"`
	DurationMs   int64  `json:"duration_ms"`
	Status       string `json:"status"`
}

func RecordAgentRunMetric(db *sql.DB, m AgentRunMetric) error {
	if m.Status == "" {
		m.Status = "completed"
	}
	_, err := db.Exec(`
		INSERT INTO agent_run_metrics (
			run_id, agent_id, thread_id, rounds, input_tokens,
			output_tokens, tool_calls, duration_ms, status, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.RunID, m.AgentID, m.ThreadID, m.Rounds, m.InputTokens,
		m.OutputTokens, m.ToolCalls, m.DurationMs, m.Status, time.Now().UnixMilli(),
	)
	return err
}

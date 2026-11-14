package Storage

import (
	"database/sql"
	"time"
)

// AgentMemoryNote is one persistent domain rule or SOP an agent saved for
// itself. It is scoped per agent — a legal agent's citation rule must not leak
// into a support agent's prompt — and is upserted by key, so a corrected rule
// replaces the stale one rather than contradicting it.
type AgentMemoryNote struct {
	Key       string `json:"key"`
	Value     string `json:"value"`
	UpdatedAt int64  `json:"updated_at"`
}

func SetAgentMemory(db *sql.DB, agentID, key, value string) error {
	_, err := db.Exec(`
		INSERT INTO agent_memory (agent_id, mem_key, mem_value, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(agent_id, mem_key) DO UPDATE SET
			mem_value = excluded.mem_value,
			updated_at = excluded.updated_at`,
		agentID, key, value, time.Now().UnixMilli(),
	)
	return err
}

func DeleteAgentMemory(db *sql.DB, agentID, key string) error {
	_, err := db.Exec(`DELETE FROM agent_memory WHERE agent_id = ? AND mem_key = ?`, agentID, key)
	return err
}

func ListAgentMemory(db *sql.DB, agentID string) ([]AgentMemoryNote, error) {
	rows, err := db.Query(
		`SELECT mem_key, mem_value, updated_at FROM agent_memory WHERE agent_id = ? ORDER BY mem_key ASC`,
		agentID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AgentMemoryNote
	for rows.Next() {
		var n AgentMemoryNote
		if err := rows.Scan(&n.Key, &n.Value, &n.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

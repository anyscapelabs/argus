package Storage

import "fmt"

// PermissionNotice is one persisted permission decision, so the UI can show
// what the agent got granted even after the app is restarted.
type PermissionNotice struct {
	ID        string
	AgentID   string
	AgentName string
	Tool      string
	Command   string
	Decision  string
	CreatedAt int64
}

func (db *DB) InsertPermissionNotice(n PermissionNotice) error {
	if n.ID == "" {
		return fmt.Errorf("storage: notice id is required")
	}
	_, err := db.conn.Exec(
		`INSERT INTO permission_notices (id, agent_id, agent_name, tool, command, decision, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		n.ID, n.AgentID, n.AgentName, n.Tool, n.Command, n.Decision, n.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("storage: notice insert failed: %w", err)
	}
	return nil
}

func (db *DB) ListPermissionNotices(agentID string, limit int) ([]PermissionNotice, error) {
	if limit <= 0 {
		limit = 1
	}
	rows, err := db.conn.Query(
		`SELECT id, agent_id, agent_name, tool, command, decision, created_at
		 FROM permission_notices WHERE agent_id = ? ORDER BY created_at DESC LIMIT ?`,
		agentID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("storage: notice listing failed: %w", err)
	}
	defer rows.Close()
	var out []PermissionNotice
	for rows.Next() {
		var n PermissionNotice
		if err := rows.Scan(&n.ID, &n.AgentID, &n.AgentName, &n.Tool, &n.Command, &n.Decision, &n.CreatedAt); err != nil {
			return nil, fmt.Errorf("storage: notice scan failed: %w", err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

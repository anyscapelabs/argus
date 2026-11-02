package Storage

import "fmt"

// PermissionNotice is one persisted notice: a permission decision, or anything
// else the agent loop wants the user to be able to see again after a restart.
// Kind says which, and Body carries the copy when the row cannot be described
// by a decision alone.
type PermissionNotice struct {
	ID        string
	AgentID   string
	AgentName string
	Tool      string
	Command   string
	Decision  string
	Kind      string
	Body      string
	CreatedAt int64
}

func (db *DB) InsertPermissionNotice(n PermissionNotice) error {
	if n.ID == "" {
		return fmt.Errorf("storage: notice id is required")
	}
	if n.Kind == "" {
		n.Kind = "permission"
	}
	_, err := db.conn.Exec(
		`INSERT INTO permission_notices (id, agent_id, agent_name, tool, command, decision, kind, notice_text, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		n.ID, n.AgentID, n.AgentName, n.Tool, n.Command, n.Decision, n.Kind, n.Body, n.CreatedAt,
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
		`SELECT id, agent_id, agent_name, tool, command, decision, kind, notice_text, created_at
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
		if err := rows.Scan(&n.ID, &n.AgentID, &n.AgentName, &n.Tool, &n.Command, &n.Decision, &n.Kind, &n.Body, &n.CreatedAt); err != nil {
			return nil, fmt.Errorf("storage: notice scan failed: %w", err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// LatestNoticeOf returns the newest row of a kind whose decision matches, which
// is how a card that settled in an earlier run is found again after a restart.
func (db *DB) LatestNoticeOf(agentID, kind, decision string) (PermissionNotice, bool, error) {
	rows, err := db.ListPermissionNotices(agentID, 50)
	if err != nil {
		return PermissionNotice{}, false, err
	}
	for _, n := range rows {
		if n.Kind == kind && (decision == "" || n.Decision == decision) {
			return n, true, nil
		}
	}
	return PermissionNotice{}, false, nil
}

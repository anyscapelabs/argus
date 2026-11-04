package Storage

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// GrantMode is the persisted decision for a grant row.
type GrantMode string

const (
	GrantAllow GrantMode = "allow"
	GrantDeny  GrantMode = "deny"
)

// Grant is a persisted, per-agent permission grant for a tool. Agents only
// get tool access once a grant row with mode "allow" exists.
type Grant struct {
	AgentID   string
	Tool      string
	Mode      string
	CreatedAt int64
}

func (db *DB) GetGrant(agentID, tool string) (Grant, bool, error) {
	row := db.conn.QueryRow(
		`SELECT agent_id, tool, mode, created_at FROM permission_grants WHERE agent_id = ? AND tool = ?`,
		agentID, tool,
	)
	var g Grant
	if err := row.Scan(&g.AgentID, &g.Tool, &g.Mode, &g.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Grant{}, false, nil
		}
		return Grant{}, false, fmt.Errorf("storage: grant lookup failed: %w", err)
	}
	return g, true, nil
}

func (db *DB) SetGrant(agentID, tool string, mode GrantMode) error {
	_, err := db.conn.Exec(
		`INSERT INTO permission_grants (agent_id, tool, mode, created_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT (agent_id, tool) DO UPDATE SET mode = excluded.mode, created_at = excluded.created_at`,
		agentID, tool, string(mode), nowUnixMs(),
	)
	if err != nil {
		return fmt.Errorf("storage: grant save failed: %w", err)
	}
	return nil
}

func (db *DB) ClearGrant(agentID, tool string) error {
	if _, err := db.conn.Exec(
		`DELETE FROM permission_grants WHERE agent_id = ? AND tool = ?`,
		agentID, tool,
	); err != nil {
		return fmt.Errorf("storage: grant delete failed: %w", err)
	}
	return nil
}

// ClearGrantPrefix removes every grant whose tool starts with prefix. Grants are
// keyed by an opaque tool string, and the browser family is scoped per site
// ("browser_action:github.com"), so revoking browser access has to sweep the
// whole family rather than one row.
func (db *DB) ClearGrantPrefix(agentID, prefix string) error {
	// `_` and `%` are LIKE metacharacters and the family names contain both;
	// escape them so the prefix means itself and not a pattern.
	escaped := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(prefix)
	_, err := db.conn.Exec(
		`DELETE FROM permission_grants WHERE agent_id = ? AND tool LIKE ? ESCAPE '\'`,
		agentID, escaped+`%`,
	)
	if err != nil {
		return fmt.Errorf("storage: grant delete failed: %w", err)
	}
	return nil
}

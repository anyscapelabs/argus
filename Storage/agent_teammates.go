package Storage

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	handlePattern   = regexp.MustCompile(`^[a-z0-9_-]{2,32}$`)
	reservedHandles = map[string]bool{
		"user": true, "system": true, "all": true, "admin": true, "root": true,
	}
)

// AgentTeammate is another agent profile as the delegation tools see it: a
// routable @handle plus the specialist role that goes into the prompt block.
type AgentTeammate struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Handle string `json:"handle"`
	Role   string `json:"role"`
}

// ValidateAgentHandle normalises and checks a user-supplied handle. Handles
// are what teammate addressing is built on, so they are lowercase, short, and
// drawn from a fixed alphabet; a handful of names are reserved because they
// would be ambiguous in a prompt or an @mention.
func ValidateAgentHandle(raw string) (string, error) {
	h := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(raw), "@"))
	if !handlePattern.MatchString(h) {
		return "", errors.New("handle must be 2-32 lowercase letters, digits, underscores, or hyphens")
	}
	if reservedHandles[h] {
		return "", fmt.Errorf("handle @%s is reserved", h)
	}
	return h, nil
}

// slugifyHandle derives a handle candidate from an agent's display name, with
// the id as fallback when the name reduces to nothing usable or lands on a
// reserved word.
func slugifyHandle(name, fallbackID string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len(out) < 2 || reservedHandles[out] {
		out = "agent_" + strings.ToLower(fallbackID)
	}
	if len(out) > 28 {
		out = out[:28]
	}
	return out
}

// backfillAgentHandles gives every pre-existing profile a handle. It runs once
// at the migration that adds the column. Candidates are derived from the
// display name and disambiguated with a numeric suffix, so no profile is left
// unaddressable and no two profiles share a handle.
func backfillAgentHandles(db *sql.DB) error {
	rows, err := db.Query(`SELECT id, agent_name FROM agent_profiles WHERE handle = '' AND id != ?`, GlobalMemoryAgentID)
	if err != nil {
		return err
	}
	defer rows.Close()

	type pending struct{ id, name string }
	var list []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.name); err != nil {
			return err
		}
		list = append(list, p)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for i, p := range list {
		base := slugifyHandle(p.name, p.id)
		cand := base
		for n := 2; ; n++ {
			var exists int
			_ = db.QueryRow(`SELECT COUNT(1) FROM agent_profiles WHERE handle = ?`, cand).Scan(&exists)
			if exists == 0 {
				break
			}
			cand = fmt.Sprintf("%s_%d", base, n+i)
		}
		if _, err := db.Exec(`UPDATE agent_profiles SET handle = ? WHERE id = ?`, cand, p.id); err != nil {
			return err
		}
	}
	return nil
}

// GetAgentByHandle resolves "@handle" (with or without the sigil) to a
// teammate. sql.ErrNoRows is the caller's signal that the handle is unknown.
func GetAgentByHandle(db *sql.DB, handle string) (AgentTeammate, error) {
	h := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(handle), "@"))
	var ag AgentTeammate
	err := db.QueryRow(
		`SELECT id, agent_name, handle, role FROM agent_profiles WHERE handle = ?`, h,
	).Scan(&ag.ID, &ag.Name, &ag.Handle, &ag.Role)
	return ag, err
}

// GetAgentByID resolves an agent id to the teammate view.
func GetAgentByID(db *sql.DB, id string) (AgentTeammate, error) {
	var ag AgentTeammate
	err := db.QueryRow(
		`SELECT id, agent_name, handle, role FROM agent_profiles WHERE id = ?`, id,
	).Scan(&ag.ID, &ag.Name, &ag.Handle, &ag.Role)
	return ag, err
}

// ListAgentTeammates lists every other agent, ordered by name, as the
// delegation prompt block and list_agents tool present them.
func ListAgentTeammates(db *sql.DB, excludeAgentID string) ([]AgentTeammate, error) {
	rows, err := db.Query(
		`SELECT id, agent_name, handle, role FROM agent_profiles WHERE id != ? AND id != ? ORDER BY agent_name ASC`,
		excludeAgentID, GlobalMemoryAgentID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AgentTeammate
	for rows.Next() {
		var ag AgentTeammate
		if err := rows.Scan(&ag.ID, &ag.Name, &ag.Handle, &ag.Role); err != nil {
			return nil, err
		}
		out = append(out, ag)
	}
	return out, rows.Err()
}

// ListAllAgentIDs lists every agent id, used by one-time migrations that must
// touch each profile (legacy connection seeding, for example).
func ListAllAgentIDs(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`SELECT id FROM agent_profiles WHERE id != ? ORDER BY agent_name ASC`, GlobalMemoryAgentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// SetAgentHandle gives a freshly created profile its routable handle,
// derived from the display name and disambiguated with a numeric suffix
// until unique. New agents are the callers; backfillAgentHandles covers the
// pre-existing rows once at migration time.
func SetAgentHandle(db *sql.DB, agentID, name string) (string, error) {
	base := slugifyHandle(name, agentID)
	cand := base
	for n := 2; ; n++ {
		var exists int
		if err := db.QueryRow(
			`SELECT COUNT(1) FROM agent_profiles WHERE handle = ? AND id != ?`, cand, agentID,
		).Scan(&exists); err != nil {
			return "", err
		}
		if exists == 0 {
			break
		}
		cand = fmt.Sprintf("%s_%d", base, n)
	}
	if _, err := db.Exec(`UPDATE agent_profiles SET handle = ? WHERE id = ?`, cand, agentID); err != nil {
		return "", err
	}
	return cand, nil
}

// AgentSummary is one agent as the sidebar lists it: identity plus its
// avatar value (a hex color or an image path) and its routable handle.
type AgentSummary struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Avatar          string `json:"avatar"`
	Handle          string `json:"handle"`
	Role            string `json:"role"`
	LastMessage     string `json:"last_message"`
	LastMessageKind string `json:"last_message_kind"`
	LastMessageAt   int64  `json:"last_message_at"`
}

// ListAgentSummaries lists every agent profile, default first and the rest
// by name, for the sidebar's agent list.
func ListAgentSummaries(db *sql.DB) ([]AgentSummary, error) {
	rows, err := db.Query(`
		SELECT p.id, p.agent_name, p.avatar, p.handle, p.role,
		       CASE
		         WHEN m.kind = 'file_write' THEN 'Wrote a file'
		         ELSE COALESCE(NULLIF(TRIM(m.text), ''), NULLIF(TRIM(m.error_message), ''), '')
		       END,
		       COALESCE(m.kind, ''), COALESCE(m.created_at, 0)
		FROM agent_profiles p
		LEFT JOIN messages m ON m.agent_id = p.id AND m.seq = (
			SELECT MAX(latest.seq)
			FROM messages latest
			WHERE latest.agent_id = p.id
			  AND (latest.kind = '' OR latest.kind = 'voice' OR latest.kind = 'file_write')
			  AND NOT (latest.role = 'user' AND latest.kind = '' AND TRIM(latest.text) = '')
			  AND NOT (latest.role = 'assistant' AND latest.kind = '' AND latest.status <> 'streaming'
			           AND TRIM(COALESCE(latest.text, '')) = ''
			           AND TRIM(COALESCE(latest.error_message, '')) = '')
		)
		WHERE p.id != ?
		ORDER BY CASE WHEN p.id = 'default' THEN 0 ELSE 1 END, p.agent_name ASC`, GlobalMemoryAgentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AgentSummary
	for rows.Next() {
		var a AgentSummary
		if err := rows.Scan(
			&a.ID, &a.Name, &a.Avatar, &a.Handle, &a.Role,
			&a.LastMessage, &a.LastMessageKind, &a.LastMessageAt,
		); err != nil {
			return nil, err
		}
		if runes := []rune(a.LastMessage); len(runes) > 160 {
			a.LastMessage = string(runes[:157]) + "..."
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

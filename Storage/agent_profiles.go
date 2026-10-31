package Storage

import (
	"database/sql"
	"fmt"
	"strings"
)

type AgentProfile struct {
	ID           string
	Avatar       string
	AgentName    string
	ModelRef     string
	SystemPrompt string
	UpdatedAt    string
}

func (db *DB) SaveAgentProfile(p AgentProfile) error {
	if p.ID == "" {
		return fmt.Errorf("storage: agent profile id is required")
	}
	if p.UpdatedAt == "" {
		p.UpdatedAt = nowRFC3339()
	}
	_, err := db.conn.Exec(
		`INSERT INTO agent_profiles (id, avatar, agent_name, model_ref, system_prompt, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   avatar = excluded.avatar,
		   agent_name = excluded.agent_name,
		   model_ref = excluded.model_ref,
		   system_prompt = excluded.system_prompt,
		   updated_at = excluded.updated_at`,
		p.ID, p.Avatar, p.AgentName, nullString(p.ModelRef), p.SystemPrompt, p.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("storage: agent profile save failed: %w", err)
	}
	return nil
}

func (db *DB) GetAgentProfile(id string) (AgentProfile, bool, error) {
	row := db.conn.QueryRow(
		`SELECT id, avatar, agent_name, model_ref, system_prompt, updated_at
		 FROM agent_profiles WHERE id = ?`, id)
	var p AgentProfile
	var modelRef sql.NullString
	var systemPrompt sql.NullString
	err := row.Scan(&p.ID, &p.Avatar, &p.AgentName, &modelRef, &systemPrompt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return AgentProfile{}, false, nil
	}
	if err != nil {
		return AgentProfile{}, false, fmt.Errorf("storage: agent profile read failed: %w", err)
	}
	p.ModelRef = modelRef.String
	p.SystemPrompt = systemPrompt.String
	return p, true, nil
}

func (db *DB) ListAgentProfiles() ([]AgentProfile, error) {
	rows, err := db.conn.Query(
		`SELECT id, avatar, agent_name, model_ref, system_prompt, updated_at FROM agent_profiles`)
	if err != nil {
		return nil, fmt.Errorf("storage: agent profile listing failed: %w", err)
	}
	defer rows.Close()
	var out []AgentProfile
	for rows.Next() {
		var p AgentProfile
		var modelRef sql.NullString
		var systemPrompt sql.NullString
		if err := rows.Scan(&p.ID, &p.Avatar, &p.AgentName, &modelRef, &systemPrompt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("storage: agent profile scan failed: %w", err)
		}
		p.ModelRef = modelRef.String
		p.SystemPrompt = systemPrompt.String
		out = append(out, p)
	}
	return out, rows.Err()
}

func (db *DB) DeleteAgentProfile(id string) error {
	if _, err := db.conn.Exec(`DELETE FROM agent_profiles WHERE id = ?`, id); err != nil {
		return fmt.Errorf("storage: agent profile delete failed: %w", err)
	}
	return nil
}

// ClearAgentModelsNotIn removes agent-profile model references that point into
// providerID but are no longer part of the provider's selection. keep maps
// model id -> true for references that survive; pass nil to drop every
// reference into the provider. Returns the ids of the profiles it cleared.
func (db *DB) ClearAgentModelsNotIn(providerID string, keep map[string]bool) ([]string, error) {
	profiles, err := db.ListAgentProfiles()
	if err != nil {
		return nil, err
	}
	prefix := providerID + "/"
	var cleared []string
	for _, p := range profiles {
		if !strings.HasPrefix(strings.ToLower(p.ModelRef), strings.ToLower(prefix)) {
			continue
		}
		_, modelID := splitRef(p.ModelRef)
		if keep[modelID] {
			continue
		}
		p.ModelRef = ""
		p.UpdatedAt = ""
		if err := db.SaveAgentProfile(p); err != nil {
			return cleared, err
		}
		cleared = append(cleared, p.ID)
	}
	return cleared, nil
}

func splitRef(ref string) (string, string) {
	if i := strings.Index(ref, "/"); i >= 0 {
		return ref[:i], ref[i+1:]
	}
	return ref, ""
}

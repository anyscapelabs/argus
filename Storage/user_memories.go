package Storage

import (
	"database/sql"
	"fmt"

	"github.com/google/uuid"
)

type Memory struct {
	ID              string
	AgentID         string
	Key             string
	Value           string
	Scope           string
	Confidence      float64
	SourceType      string
	SourceMessageID string
	SourceRunID     string
	Revision        int
	CreatedAt       int64
	UpdatedAt       int64
}

type MemoryExposure struct {
	MemoryID        string
	MemoryRevision  int
	Position        int
	Score           int
	EstimatedTokens int
}

type RequestSource struct {
	MessageID string
	AgentID   string
	Role      string
	Kind      string
	Text      string
}

const memoryColumns = `id, agent_id, key, value, scope, confidence, source_type, source_message_id, source_run_id, revision, created_at, updated_at`

func scanMemory(scanner interface{ Scan(...any) error }) (Memory, error) {
	var m Memory
	err := scanner.Scan(&m.ID, &m.AgentID, &m.Key, &m.Value, &m.Scope, &m.Confidence,
		&m.SourceType, &m.SourceMessageID, &m.SourceRunID, &m.Revision, &m.CreatedAt, &m.UpdatedAt)
	return m, err
}

func (db *DB) UpsertMemory(memory Memory) (Memory, error) {
	if memory.Scope == "avoid" {
		now := nowUnixMs()
		id, _ := uuid.NewV7()
		_, err := db.conn.Exec(
			`INSERT INTO memory_avoid_rules (id, agent_id, key, value, updated_at) VALUES (?, ?, ?, ?, ?)
			 ON CONFLICT(agent_id, key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
			id.String(), memory.AgentID, memory.Key, memory.Value, now,
		)
		return memory, err
	}
	now := nowUnixMs()
	current, found, err := db.GetMemory(memory.AgentID, memory.Key)
	if err != nil {
		return Memory{}, err
	}
	if found {
		memory.ID = current.ID
		memory.CreatedAt = current.CreatedAt
		memory.Revision = current.Revision
		if current.Value != memory.Value || current.Scope != memory.Scope {
			memory.Revision++
		}
	} else {
		id, _ := uuid.NewV7()
		memory.ID = id.String()
		memory.CreatedAt = now
		memory.Revision = 1
	}
	memory.UpdatedAt = now
	_, err = db.conn.Exec(
		`INSERT INTO memory_items (`+memoryColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(agent_id, key) DO UPDATE SET
		 value=excluded.value, scope=excluded.scope, confidence=excluded.confidence,
		 source_type=excluded.source_type, source_message_id=excluded.source_message_id,
		 source_run_id=excluded.source_run_id, revision=excluded.revision, updated_at=excluded.updated_at`,
		memory.ID, memory.AgentID, memory.Key, memory.Value, memory.Scope, memory.Confidence,
		memory.SourceType, memory.SourceMessageID, memory.SourceRunID, memory.Revision,
		memory.CreatedAt, memory.UpdatedAt,
	)
	if err != nil {
		return Memory{}, fmt.Errorf("storage: memory save failed: %w", err)
	}
	return memory, nil
}

func (db *DB) GetMemory(agentID, key string) (Memory, bool, error) {
	m, err := scanMemory(db.conn.QueryRow(`SELECT `+memoryColumns+` FROM memory_items WHERE agent_id = ? AND key = ?`, agentID, key))
	if err == sql.ErrNoRows {
		return Memory{}, false, nil
	}
	if err != nil {
		return Memory{}, false, fmt.Errorf("storage: memory get failed: %w", err)
	}
	return m, true, nil
}

func (db *DB) ListMemoryCandidates(agentID string) ([]Memory, error) {
	// Seal the candidate window: memory ranking is top-8 by score over
	// potentially thousands of rows; the cap keeps a bloated memory store
	// from turning every send into a full scan.
	rows, err := db.conn.Query(`SELECT `+memoryColumns+` FROM memory_items WHERE agent_id = ? ORDER BY updated_at DESC LIMIT 200`, agentID)
	if err != nil {
		return nil, fmt.Errorf("storage: memory list failed: %w", err)
	}
	defer rows.Close()
	var out []Memory
	for rows.Next() {
		m, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (db *DB) ForgetMemoryForAgent(agentID, key string) (bool, error) {
	_, _ = db.conn.Exec(`DELETE FROM memory_avoid_rules WHERE agent_id = ? AND key = ?`, agentID, key)
	res, err := db.conn.Exec(`DELETE FROM memory_items WHERE agent_id = ? AND key = ?`, agentID, key)
	if err != nil {
		return false, fmt.Errorf("storage: memory delete failed: %w", err)
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (db *DB) ListAvoidMemories(agentID string, limit int) ([]Memory, error) {
	if limit <= 0 {
		limit = 12
	}
	rows, err := db.conn.Query(
		`SELECT id, agent_id, key, value, updated_at FROM memory_avoid_rules WHERE agent_id = ? ORDER BY updated_at DESC LIMIT ?`,
		agentID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Memory
	for rows.Next() {
		var m Memory
		m.Scope = "avoid"
		if err := rows.Scan(&m.ID, &m.AgentID, &m.Key, &m.Value, &m.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (db *DB) GetAgentSkillState(agentID, skillID string) (bool, bool, error) {
	var enabled int
	err := db.conn.QueryRow(`SELECT enabled FROM agent_skills WHERE agent_id = ? AND skill_id = ?`, agentID, skillID).Scan(&enabled)
	if err == sql.ErrNoRows {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return enabled == 1, true, nil
}

func (db *DB) SetAgentSkillState(agentID, skillID string, enabled bool) error {
	_, err := db.conn.Exec(
		`INSERT INTO agent_skills (agent_id, skill_id, enabled, updated_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT(agent_id, skill_id) DO UPDATE SET enabled=excluded.enabled, updated_at=excluded.updated_at`,
		agentID, skillID, b2i(enabled), nowUnixMs(),
	)
	return err
}

func (db *DB) UpsertToolLesson(agentID, serverID, toolName, lesson string) error {
	_, err := db.conn.Exec(
		`INSERT INTO learned_tool_lessons (agent_id, server_id, tool_name, lesson, hit_count, updated_at)
		 VALUES (?, ?, ?, ?, 1, ?)
		 ON CONFLICT(agent_id, tool_name) DO UPDATE SET
		 lesson=excluded.lesson, hit_count=learned_tool_lessons.hit_count+1, updated_at=excluded.updated_at`,
		agentID, serverID, toolName, lesson, nowUnixMs(),
	)
	return err
}

func (db *DB) ListToolLessons(agentID string, serverIDs []string, limit int) ([]string, error) {
	if len(serverIDs) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 8
	}
	marks := ""
	args := make([]any, 0, len(serverIDs)+2)
	args = append(args, agentID)
	for i, id := range serverIDs {
		if i > 0 {
			marks += ","
		}
		marks += "?"
		args = append(args, id)
	}
	args = append(args, limit)
	rows, err := db.conn.Query(
		`SELECT lesson FROM learned_tool_lessons WHERE agent_id = ? AND server_id IN (`+marks+`) ORDER BY updated_at DESC LIMIT ?`,
		args...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (db *DB) GetRequestSource(runID string) (RequestSource, bool, error) {
	var source RequestSource
	err := db.conn.QueryRow(
		`SELECT m.id, m.agent_id, m.role, m.kind, m.text
		 FROM model_requests r JOIN messages m
		 ON m.agent_id = r.agent_id AND m.seq = r.context_through_seq
		 WHERE r.id = ?`, runID,
	).Scan(&source.MessageID, &source.AgentID, &source.Role, &source.Kind, &source.Text)
	if err == sql.ErrNoRows {
		return RequestSource{}, false, nil
	}
	if err != nil {
		return RequestSource{}, false, fmt.Errorf("storage: request source failed: %w", err)
	}
	return source, true, nil
}

func (db *DB) RecordMemoryExposures(requestID string, items []MemoryExposure) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := nowUnixMs()
	for _, item := range items {
		if _, err := tx.Exec(
			`INSERT OR REPLACE INTO memory_exposures
			 (request_id, memory_id, memory_revision, position, score, estimated_tokens, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			requestID, item.MemoryID, item.MemoryRevision, item.Position, item.Score, item.EstimatedTokens, now,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (db *DB) MemoryFeedback(memoryID string, revision int) (positive, negative int, err error) {
	err = db.conn.QueryRow(
		`SELECT
		 COALESCE(SUM(CASE WHEN r.signal = 1 THEN 1 ELSE 0 END), 0),
		 COALESCE(SUM(CASE WHEN r.signal = -1 THEN 1 ELSE 0 END), 0)
		 FROM memory_exposures e
		 JOIN model_requests mr ON mr.id = e.request_id AND mr.status = 'success'
		 JOIN message_reactions r ON r.message_id = mr.message_id
		 WHERE e.memory_id = ? AND e.memory_revision = ?`, memoryID, revision,
	).Scan(&positive, &negative)
	return
}

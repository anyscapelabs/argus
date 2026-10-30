package Storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/anyscapelabs/argus/Protocol"
)

type Message struct {
	ID          string
	AgentID     string
	Seq         int
	Role        string
	BlocksJSON  string
	Text        string
	Status      string
	StopReason  string
	ModelRef    string
	CreatedAt   int64
	CompletedAt int64
}

func (m Message) Blocks() []Protocol.Block {
	var blocks []Protocol.Block
	if m.BlocksJSON == "" {
		return blocks
	}
	_ = json.Unmarshal([]byte(m.BlocksJSON), &blocks)
	return blocks
}

func (db *DB) NextSeq(agentID string) (int, error) {
	row := db.conn.QueryRow(`SELECT COALESCE(MAX(seq), 0) + 1 FROM messages WHERE agent_id = ?`, agentID)
	var seq int
	if err := row.Scan(&seq); err != nil {
		return 0, fmt.Errorf("storage: next seq failed: %w", err)
	}
	return seq, nil
}

func (db *DB) InsertMessage(m Message) error {
	if m.ID == "" {
		return fmt.Errorf("storage: message id is required")
	}
	if m.CreatedAt == 0 {
		m.CreatedAt = nowUnixMs()
	}
	_, err := db.conn.Exec(
		`INSERT INTO messages (id, agent_id, seq, role, blocks_json, text, status, stop_reason, model_ref, created_at, completed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.AgentID, m.Seq, m.Role, m.BlocksJSON, m.Text, m.Status, nullString(m.StopReason), nullString(m.ModelRef), m.CreatedAt, nullInt64(m.CompletedAt),
	)
	if err != nil {
		return fmt.Errorf("storage: message insert failed: %w", err)
	}
	return nil
}

func (db *DB) FinalizeMessage(id string, blocks []Protocol.Block, text, status, stopReason, modelRef string, completedAt int64) error {
	blocksJSON, err := json.Marshal(blocks)
	if err != nil {
		return fmt.Errorf("storage: message marshal failed: %w", err)
	}
	if completedAt == 0 {
		completedAt = nowUnixMs()
	}
	_, err = db.conn.Exec(
		`UPDATE messages SET blocks_json = ?, text = ?, status = ?, stop_reason = ?, model_ref = ?, completed_at = ?
		 WHERE id = ?`,
		string(blocksJSON), text, status, nullString(stopReason), nullString(modelRef), completedAt, id,
	)
	if err != nil {
		return fmt.Errorf("storage: message finalize failed: %w", err)
	}
	return nil
}

func (db *DB) UpdateMessageStatus(id, status string) error {
	_, err := db.conn.Exec(`UPDATE messages SET status = ? WHERE id = ?`, status, id)
	if err != nil {
		return fmt.Errorf("storage: message status update failed: %w", err)
	}
	return nil
}

func (db *DB) ListMessages(agentID string, beforeSeq int, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 100
	}
	var rows *sql.Rows
	var err error
	if beforeSeq > 0 {
		rows, err = db.conn.Query(
			`SELECT id, agent_id, seq, role, blocks_json, text, status, stop_reason, model_ref, created_at, completed_at
			 FROM messages WHERE agent_id = ? AND seq < ? ORDER BY seq DESC LIMIT ?`, agentID, beforeSeq, limit)
	} else {
		rows, err = db.conn.Query(
			`SELECT id, agent_id, seq, role, blocks_json, text, status, stop_reason, model_ref, created_at, completed_at
			 FROM messages WHERE agent_id = ? ORDER BY seq DESC LIMIT ?`, agentID, limit)
	}
	if err != nil {
		return nil, fmt.Errorf("storage: message listing failed: %w", err)
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		var stopReason, modelRef sql.NullString
		var completedAt sql.NullInt64
		if err := rows.Scan(&m.ID, &m.AgentID, &m.Seq, &m.Role, &m.BlocksJSON, &m.Text, &m.Status, &stopReason, &modelRef, &m.CreatedAt, &completedAt); err != nil {
			return nil, fmt.Errorf("storage: message scan failed: %w", err)
		}
		m.StopReason = stopReason.String
		m.ModelRef = modelRef.String
		if completedAt.Valid {
			m.CompletedAt = completedAt.Int64
		}
		out = append(out, m)
	}
	// reverse to ascending
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, rows.Err()
}

func (db *DB) SweepInterrupted() error {
	if _, err := db.conn.Exec(`UPDATE messages SET status = 'interrupted' WHERE status IN ('streaming')`); err != nil {
		return fmt.Errorf("storage: message sweep failed: %w", err)
	}
	if _, err := db.conn.Exec(`UPDATE model_requests SET status = 'interrupted' WHERE status IN ('started')`); err != nil {
		return fmt.Errorf("storage: request sweep failed: %w", err)
	}
	return nil
}

func nullInt64(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}

func nowUnixMs() int64 {
	return time.Now().UnixMilli()
}
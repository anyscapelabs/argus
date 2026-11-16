package Storage

import (
	"database/sql"
	"time"
)

// AgentMessage is one hop of agent-to-agent (or user-to-agent) mail. Requests
// carry a task brief, reports carry the delegated run's outcome back to the
// delegator, and steer notes carry mid-run user instructions. Rows move
// queued -> delivered (claimed by the target's run) -> completed/failed, so a
// restart never loses a hand-off that was accepted but not yet read.
type AgentMessage struct {
	ID              string `json:"id"`
	ThreadID        string `json:"thread_id"`
	ParentMessageID string `json:"parent_message_id"`
	FromAgentID     string `json:"from_agent_id"`
	ToAgentID       string `json:"to_agent_id"`
	Kind            string `json:"kind"` // request | report | notice | steer | user
	Body            string `json:"body"`
	Status          string `json:"status"` // queued | delivered | completed | blocked | failed
	ReportBack      bool   `json:"report_back"`
	Urgent          bool   `json:"urgent"`
	Hops            int    `json:"hops"`
	CreatedAt       int64  `json:"created_at"`
	DeliveredAt     int64  `json:"delivered_at"`
	CompletedAt     int64  `json:"completed_at"`
}

func EnqueueAgentMessage(db *sql.DB, msg AgentMessage) error {
	if msg.CreatedAt == 0 {
		msg.CreatedAt = time.Now().UnixMilli()
	}
	if msg.Status == "" {
		msg.Status = "queued"
	}
	rb := 0
	if msg.ReportBack {
		rb = 1
	}
	urg := 0
	if msg.Urgent {
		urg = 1
	}
	_, err := db.Exec(`
		INSERT INTO agent_messages (
			id, thread_id, parent_message_id, from_agent_id, to_agent_id,
			kind, body, status, report_back, urgent, hops, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		msg.ID, msg.ThreadID, msg.ParentMessageID, msg.FromAgentID, msg.ToAgentID,
		msg.Kind, msg.Body, msg.Status, rb, urg, msg.Hops, msg.CreatedAt,
	)
	return err
}

// DrainQueuedAgentMessages atomically claims queued inbox messages for an
// agent: the read and the delivered flip happen in one transaction, so two
// runs racing for the same inbox cannot both execute the same request.
func DrainQueuedAgentMessages(db *sql.DB, agentID string) ([]AgentMessage, error) {
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rows, err := tx.Query(`
		SELECT id, thread_id, parent_message_id, from_agent_id, to_agent_id,
		       kind, body, status, report_back, urgent, hops, created_at
		FROM agent_messages
		WHERE to_agent_id = ? AND status = 'queued'
		ORDER BY urgent DESC, created_at ASC`, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []AgentMessage
	for rows.Next() {
		var m AgentMessage
		var rb, urg int
		if err := rows.Scan(
			&m.ID, &m.ThreadID, &m.ParentMessageID, &m.FromAgentID, &m.ToAgentID,
			&m.Kind, &m.Body, &m.Status, &rb, &urg, &m.Hops, &m.CreatedAt,
		); err != nil {
			return nil, err
		}
		m.ReportBack = rb == 1
		m.Urgent = urg == 1
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(msgs) == 0 {
		return nil, tx.Commit()
	}

	now := time.Now().UnixMilli()
	for _, m := range msgs {
		if _, err := tx.Exec(
			`UPDATE agent_messages SET status = 'delivered', delivered_at = ? WHERE id = ?`,
			now, m.ID,
		); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return msgs, nil
}

// ClaimNextAgentMessage reserves one queued item without touching the rest of
// the inbox. Desktop wakes use this to materialize exactly one normal chat run;
// messages for other threads remain queued for their own run.
func ClaimNextAgentMessage(db *sql.DB, agentID string) (AgentMessage, bool, error) {
	tx, err := db.Begin()
	if err != nil {
		return AgentMessage{}, false, err
	}
	defer tx.Rollback()
	var m AgentMessage
	var rb, urgent int
	err = tx.QueryRow(`
		SELECT id, thread_id, parent_message_id, from_agent_id, to_agent_id,
		       kind, body, status, report_back, urgent, hops, created_at
		FROM agent_messages
		WHERE to_agent_id = ? AND status = 'queued'
		ORDER BY urgent DESC, created_at ASC LIMIT 1`, agentID).Scan(
		&m.ID, &m.ThreadID, &m.ParentMessageID, &m.FromAgentID, &m.ToAgentID,
		&m.Kind, &m.Body, &m.Status, &rb, &urgent, &m.Hops, &m.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return AgentMessage{}, false, tx.Commit()
	}
	if err != nil {
		return AgentMessage{}, false, err
	}
	m.ReportBack = rb == 1
	m.Urgent = urgent == 1
	now := time.Now().UnixMilli()
	res, err := tx.Exec(`UPDATE agent_messages SET status = 'delivered', delivered_at = ?
		WHERE id = ? AND status = 'queued'`, now, m.ID)
	if err != nil {
		return AgentMessage{}, false, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return AgentMessage{}, false, tx.Commit()
	}
	if err := tx.Commit(); err != nil {
		return AgentMessage{}, false, err
	}
	m.Status = "delivered"
	m.DeliveredAt = now
	return m, true, nil
}

// DrainQueuedAgentMessagesForThread claims only mail the active run can
// consume. This avoids the old drain-and-requeue race where a fresh random
// thread repeatedly bounced delegated work back to the inbox.
func DrainQueuedAgentMessagesForThread(db *sql.DB, agentID, threadID string) ([]AgentMessage, error) {
	if threadID == "" {
		return nil, nil
	}
	tx, err := db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	rows, err := tx.Query(`
		SELECT id, thread_id, parent_message_id, from_agent_id, to_agent_id,
		       kind, body, status, report_back, urgent, hops, created_at
		FROM agent_messages
		WHERE to_agent_id = ? AND status = 'queued'
		  AND kind NOT IN ('request', 'user')
		  AND (thread_id = ? OR kind = 'steer')
		ORDER BY urgent DESC, created_at ASC`, agentID, threadID)
	if err != nil {
		return nil, err
	}
	var messages []AgentMessage
	for rows.Next() {
		var m AgentMessage
		var rb, urgent int
		if err := rows.Scan(
			&m.ID, &m.ThreadID, &m.ParentMessageID, &m.FromAgentID, &m.ToAgentID,
			&m.Kind, &m.Body, &m.Status, &rb, &urgent, &m.Hops, &m.CreatedAt,
		); err != nil {
			rows.Close()
			return nil, err
		}
		m.ReportBack = rb == 1
		m.Urgent = urgent == 1
		messages = append(messages, m)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	now := time.Now().UnixMilli()
	for i := range messages {
		if _, err := tx.Exec(`UPDATE agent_messages SET status = 'delivered', delivered_at = ?
			WHERE id = ? AND status = 'queued'`, now, messages[i].ID); err != nil {
			return nil, err
		}
		messages[i].Status = "delivered"
		messages[i].DeliveredAt = now
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return messages, nil
}

func MarkAgentMessageStatus(db *sql.DB, id, status string) error {
	now := time.Now().UnixMilli()
	if status == "queued" || status == "delivered" {
		_, err := db.Exec(`UPDATE agent_messages SET status = ?, completed_at = 0 WHERE id = ?`, status, id)
		return err
	}
	_, err := db.Exec(
		`UPDATE agent_messages SET status = ?, completed_at = ? WHERE id = ?`,
		status, now, id,
	)
	return err
}

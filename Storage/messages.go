package Storage

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/anyscapelabs/argus/Protocol"
)

type Message struct {
	ID           string
	AgentID      string
	Seq          int
	Role         string
	Kind         string
	BlocksJSON   string
	Text         string
	Status       string
	ErrorMessage string
	StopReason   string
	ModelRef     string
	CreatedAt    int64
	CompletedAt  int64
}

// MessageKindSteering marks a user-role row that the app wrote, not the
// person. It exists so an out-of-band event — an MCP connection settling while
// no run was alive — can enter the model's history the way a follow-up message
// would. The chat UI hides these rows; buildHistory replays them.
const MessageKindSteering = "steering"

// MessageKindScheduled marks the app-authored row a routine or reminder fire
// starts with. Like steering rows it is invisible in the chat and replayed in
// model history, but it is semantically different: it is the scheduler waking
// the agent for a job it already agreed to, not anyone saying anything new —
// which is why the send path that writes it marks the run unattended.
const MessageKindScheduled = "scheduled"

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
		`INSERT INTO messages (id, agent_id, seq, role, kind, blocks_json, text, status, stop_reason, model_ref, created_at, completed_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.AgentID, m.Seq, m.Role, m.Kind, m.BlocksJSON, m.Text, m.Status, nullString(m.StopReason), nullString(m.ModelRef), m.CreatedAt, nullInt64(m.CompletedAt),
	)
	if err != nil {
		return fmt.Errorf("storage: message insert failed: %w", err)
	}
	return nil
}

func (db *DB) FinalizeMessage(id string, blocks []Protocol.Block, text, status, stopReason, modelRef, errorMessage string, completedAt int64) error {
	blocksJSON, err := json.Marshal(blocks)
	if err != nil {
		return fmt.Errorf("storage: message marshal failed: %w", err)
	}
	if completedAt == 0 {
		completedAt = nowUnixMs()
	}
	_, err = db.conn.Exec(
		`UPDATE messages SET blocks_json = ?, text = ?, status = ?, stop_reason = ?, model_ref = ?, error_message = ?, completed_at = ?
		 WHERE id = ?`,
		string(blocksJSON), text, status, nullString(stopReason), nullString(modelRef),
		shortError(errorMessage), completedAt, id,
	)
	if err != nil {
		return fmt.Errorf("storage: message finalize failed: %w", err)
	}
	return nil
}

// maxStoredErrorChars bounds what a provider's complaint may cost the transcript.
// A validation rejection repeats itself per field, and one Gemini turn stored
// 91,548 characters of "Unknown name \"$defs\" …" for a single message: the bubble
// rendered the whole blob and the conversation became unreadable. The first line is
// the sentence that actually identifies the failure.
const maxStoredErrorChars = 400

func shortError(msg string) string {
	if len(msg) <= maxStoredErrorChars {
		return msg
	}
	line, _, _ := strings.Cut(strings.TrimSpace(msg), "\n")
	if line == "" {
		line = strings.ReplaceAll(strings.TrimSpace(msg), "\n", " ")
	}
	line = cutBytes(line, maxStoredErrorChars)
	if line == "" {
		return ""
	}
	return line + " …"
}

// cutBytes keeps the first n bytes of a string without splitting a rune, so a
// truncated provider error never ends in a broken character.
func cutBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func (db *DB) UpdateMessageStatus(id, status string) error {
	_, err := db.conn.Exec(`UPDATE messages SET status = ? WHERE id = ?`, status, id)
	if err != nil {
		return fmt.Errorf("storage: message status update failed: %w", err)
	}
	return nil
}

// ReseqMessage moves a row to a new seq. The agent loop uses it to park the
// reserved reply row behind the tool-round rows written during the run, so
// history replays in real execution order.
func (db *DB) ReseqMessage(id string, seq int) error {
	_, err := db.conn.Exec(`UPDATE messages SET seq = ? WHERE id = ?`, seq, id)
	if err != nil {
		return fmt.Errorf("storage: message reseq failed: %w", err)
	}
	return nil
}

// GetMessage fetches one row by id; used by the scheduler to pull the reply
// text for a notification body after a scheduled run finishes.
func (db *DB) GetMessage(id string) (Message, bool, error) {
	row := db.conn.QueryRow(
		`SELECT id, agent_id, seq, role, kind, blocks_json, text, status, error_message, stop_reason, model_ref, created_at, completed_at
		 FROM messages WHERE id = ?`, id)
	var m Message
	var errorMessage, stopReason, modelRef sql.NullString
	var completedAt sql.NullInt64
	err := row.Scan(&m.ID, &m.AgentID, &m.Seq, &m.Role, &m.Kind, &m.BlocksJSON, &m.Text, &m.Status, &errorMessage, &stopReason, &modelRef, &m.CreatedAt, &completedAt)
	if err == sql.ErrNoRows {
		return Message{}, false, nil
	}
	if err != nil {
		return Message{}, false, fmt.Errorf("storage: message get failed: %w", err)
	}
	m.ErrorMessage = errorMessage.String
	m.StopReason = stopReason.String
	m.ModelRef = modelRef.String
	if completedAt.Valid {
		m.CompletedAt = completedAt.Int64
	}
	return m, true, nil
}

// LastActiveMs is when an assistant turn last finished, 0 when it never did.
// The right panel's "Last active" row reads exactly this: what counts as
// active is the agent finishing work, not the user typing.
func (db *DB) LastActiveMs(agentID string) (int64, error) {
	var last sql.NullInt64
	err := db.conn.QueryRow(
		`SELECT MAX(completed_at) FROM messages WHERE agent_id = ? AND role = 'assistant'`,
		agentID).Scan(&last)
	if err != nil {
		return 0, fmt.Errorf("storage: last active lookup failed: %w", err)
	}
	if !last.Valid {
		return 0, nil
	}
	return last.Int64, nil
}

func (db *DB) ListMessages(agentID string, beforeSeq int, limit int) ([]Message, error) {
	selector, err := db.listStmt(agentID, beforeSeq, limit, false)
	if err != nil {
		return nil, err
	}
	rows, err := selector()
	return scanMessages(rows, err)
}

// ListVisibleMessages pages what the chat UI may draw, with the visibility
// rules applied inside the SQL LIMIT rather than the Go filter: paging through
// rows the UI will drop anyway would make 100-row pages deliver a handful of
// real messages while tool-carrier rows burned the rest of the quota.
//
// The rules are the read path's (Chat/read.go), the ones buildHistory does not
// share: app-authored kinds are hidden from the display, an empty user row is a
// tool carrier, and an assistant row that carries neither text nor an error and
// has stopped streaming is a tool round or a mid-flight kill.
func (db *DB) ListVisibleMessages(agentID string, beforeSeq int, limit int) ([]Message, error) {
	selector, err := db.listStmt(agentID, beforeSeq, limit, true)
	if err != nil {
		return nil, err
	}
	rows, err := selector()
	return scanMessages(rows, err)
}

// listStmt builds the page query. The returned closure runs it — Query cannot
// take pre-baked SQL for both the cursor and the no-cursor shape without one
// branch being built string-side.
func (db *DB) listStmt(agentID string, beforeSeq, limit int, visibleOnly bool) (func() (*sql.Rows, error), error) {
	if limit <= 0 {
		limit = 100
	}
	visible := `
	  AND m.kind = ''
	  AND NOT (m.role = 'user' AND TRIM(m.text) = '')
	  AND NOT (m.role = 'assistant' AND m.status <> 'streaming'
	           AND TRIM(COALESCE(m.text, '')) = '' AND TRIM(COALESCE(m.error_message, '')) = '')`
	if !visibleOnly {
		visible = ""
	}
	cursor := ""
	args := []any{agentID}
	if beforeSeq > 0 {
		cursor = " AND m.seq < ?"
		args = append(args, beforeSeq)
	}
	args = append(args, limit)
	stmt := `SELECT m.id, m.agent_id, m.seq, m.role, m.kind, m.blocks_json, m.text, m.status, m.error_message, m.stop_reason, m.model_ref, m.created_at, m.completed_at
		 FROM messages m
		 WHERE m.agent_id = ?` + cursor + visible + `
		 ORDER BY m.seq DESC LIMIT ?`
	return func() (*sql.Rows, error) { return db.conn.Query(stmt, args...) }, nil
}

// scanMessages drains a page and hands it back oldest-first.
func scanMessages(rows *sql.Rows, err error) ([]Message, error) {
	if err != nil {
		return nil, fmt.Errorf("storage: message listing failed: %w", err)
	}
	defer rows.Close()
	var out []Message
	for rows.Next() {
		var m Message
		var errorMessage, stopReason, modelRef sql.NullString
		var completedAt sql.NullInt64
		if err := rows.Scan(&m.ID, &m.AgentID, &m.Seq, &m.Role, &m.Kind, &m.BlocksJSON, &m.Text, &m.Status, &errorMessage, &stopReason, &modelRef, &m.CreatedAt, &completedAt); err != nil {
			return nil, fmt.Errorf("storage: message scan failed: %w", err)
		}
		m.ErrorMessage = errorMessage.String
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

// SweepInterrupted closes the rows a shutdown left mid-flight. It marks, and it
// never deletes: a run that died with the app is a fact about what happened, and
// the message that started it belongs to the user. What the chat shows for a
// marked row is the read path's decision, not storage's.
func (db *DB) SweepInterrupted() error {
	if _, err := db.conn.Exec(`UPDATE messages SET status = 'interrupted' WHERE status IN ('streaming')`); err != nil {
		return fmt.Errorf("storage: message sweep failed: %w", err)
	}
	if _, err := db.conn.Exec(`UPDATE model_requests SET status = 'interrupted' WHERE status IN ('started')`); err != nil {
		return fmt.Errorf("storage: request sweep failed: %w", err)
	}
	return nil
}

// maxSeq is the sentinel that stands for "nothing follows this turn", used as
// the open end of a turn's row range.
const maxSeq = int64(1<<62 - 1)

// UnansweredTurns counts the turns that never produced a reply. An empty
// agentID counts every agent's chat.
//
// This began life as a DELETE, and it decided "unanswered" by looking at
// seq+1: with no row sitting directly after a user message, the message was
// removed. That test was false in the most common case. The send path reserves
// the reply row at userSeq+1 (Chat/send.go) and, once the run has used any
// tools, parks that row behind the tool rounds it wrote in the meantime —
// which abandons seq+1 as a permanent hole, because NextSeq only ever hands out
// MAX(seq)+1. So on the next startup the cleaner saw a hole where the reply
// should have been, deleted the question, and left the answer and every tool
// round above it as an orphan nobody could trace back to anything. Restart per
// restart, it ate the human half of every tool-using turn.
//
// A turn is now identified by what it actually is: the run's own rows, between
// this user message and the next visible one. Holes are irrelevant to that
// question, so they can no longer destroy anything. And the answer is only ever
// reported — what the user typed is not this function's to throw away.
func (db *DB) UnansweredTurns(agentID string) (int, error) {
	var n int
	err := db.conn.QueryRow(`
		SELECT COUNT(*) FROM messages u
		WHERE u.role = 'user' AND TRIM(u.text) <> ''
		  AND (? = '' OR u.agent_id = ?)
		  AND NOT EXISTS (
			SELECT 1 FROM messages a
			WHERE a.agent_id = u.agent_id AND a.role = 'assistant' AND a.seq > u.seq
			  AND a.seq < COALESCE(
				(SELECT MIN(nxt.seq) FROM messages nxt
				 WHERE nxt.agent_id = u.agent_id AND nxt.role = 'user'
				   AND TRIM(nxt.text) <> '' AND nxt.seq > u.seq),
				?)
			  AND (TRIM(a.text) <> '' OR a.error_message <> '')
		  )`, agentID, agentID, maxSeq).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("storage: unanswered turn count failed: %w", err)
	}
	return n, nil
}

// UnansweredTurnsAll is the whole-database count, for the startup log line that
// replaced the startup deletion.
func (db *DB) UnansweredTurnsAll() (int, error) {
	return db.UnansweredTurns("")
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

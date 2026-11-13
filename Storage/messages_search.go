package Storage

import (
	"strings"
)

// messageCols is the column list scanMessages expects, in scan order.
const messageCols = `SELECT m.id, m.agent_id, m.seq, m.role, m.kind, m.blocks_json, m.text, m.status, m.error_message, m.error_code, m.stop_reason, m.model_ref, m.created_at, m.completed_at`

// likeEscape neutralises the LIKE wildcards in a search word so "100%" or
// "snake_case" match literally.
func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// SearchMessages finds finished, person-visible messages of one agent whose
// text contains every word of query (case-insensitive). It backs the
// conversation_read tool, which lets the model reach turns that are no longer
// replayed in its context. Steering and scheduled rows are app-authored and
// never searched. The newest `limit` matches come back oldest-first, the same
// order every other listing here uses.
func (db *DB) SearchMessages(agentID, query string, limit int) ([]Message, error) {
	terms := strings.Fields(query)
	if len(terms) == 0 {
		return nil, nil
	}
	if len(terms) > 6 {
		terms = terms[:6]
	}
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	var b strings.Builder
	b.WriteString(messageCols)
	b.WriteString(` FROM messages m WHERE m.agent_id = ? AND m.kind = '' AND m.status = 'complete' AND TRIM(m.text) <> ''`)
	args := []any{agentID}
	for _, t := range terms {
		b.WriteString(` AND m.text LIKE ? ESCAPE '\'`)
		args = append(args, "%"+likeEscape(t)+"%")
	}
	b.WriteString(` ORDER BY m.seq DESC LIMIT ?`)
	args = append(args, limit)
	rows, err := db.conn.Query(b.String(), args...)
	return scanMessages(rows, err)
}

// ListMessageRange returns the finished, person-visible messages of one agent
// whose seq lies in [fromSeq, toSeq], oldest-first. When the span holds more
// than limit rows the newest limit survive; callers bound the span instead of
// relying on that.
func (db *DB) ListMessageRange(agentID string, fromSeq, toSeq, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 40
	}
	rows, err := db.conn.Query(
		messageCols+` FROM messages m WHERE m.agent_id = ? AND m.kind = '' AND m.status = 'complete' AND m.seq BETWEEN ? AND ? ORDER BY m.seq DESC LIMIT ?`,
		agentID, fromSeq, toSeq, limit,
	)
	return scanMessages(rows, err)
}

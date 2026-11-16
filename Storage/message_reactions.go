package Storage

import (
	"fmt"
	"strings"
)

type ReactionSample struct {
	Emoji  string
	Signal int
	Text   string
	Status string
}

func (db *DB) SetMessageReaction(messageID, emoji string, signal int) error {
	now := nowUnixMs()
	if emoji == "" {
		_, err := db.conn.Exec(`DELETE FROM message_reactions WHERE message_id = ?`, messageID)
		return err
	}
	_, err := db.conn.Exec(
		`INSERT INTO message_reactions (message_id, emoji, signal, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(message_id) DO UPDATE SET emoji = excluded.emoji, signal = excluded.signal, updated_at = excluded.updated_at`,
		messageID, emoji, signal, now, now,
	)
	if err != nil {
		return fmt.Errorf("storage: reaction save failed: %w", err)
	}
	return nil
}

func (db *DB) ListMessageReactions(messageIDs []string) (map[string]string, error) {
	out := map[string]string{}
	if len(messageIDs) == 0 {
		return out, nil
	}
	marks := strings.TrimRight(strings.Repeat("?,", len(messageIDs)), ",")
	args := make([]any, len(messageIDs))
	for i, id := range messageIDs {
		args[i] = id
	}
	rows, err := db.conn.Query(`SELECT message_id, emoji FROM message_reactions WHERE message_id IN (`+marks+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: reaction list failed: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, emoji string
		if err := rows.Scan(&id, &emoji); err != nil {
			return nil, err
		}
		out[id] = emoji
	}
	return out, rows.Err()
}

func (db *DB) ListReactionSamples(agentID string, limit int) ([]ReactionSample, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	rows, err := db.conn.Query(
		`SELECT r.emoji, r.signal, m.text, m.status
		 FROM message_reactions r
		 JOIN messages m ON m.id = r.message_id
		 WHERE m.agent_id = ? AND m.role = 'assistant' AND m.kind = ''
		 ORDER BY r.updated_at DESC LIMIT ?`, agentID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("storage: reaction samples failed: %w", err)
	}
	defer rows.Close()
	var out []ReactionSample
	for rows.Next() {
		var sample ReactionSample
		if err := rows.Scan(&sample.Emoji, &sample.Signal, &sample.Text, &sample.Status); err != nil {
			return nil, err
		}
		out = append(out, sample)
	}
	return out, rows.Err()
}

package Storage

import "fmt"

// Memory is one durable fact about the user, keyed so an update replaces the
// old value instead of adding a contradiction.
type Memory struct {
	Key       string
	Value     string
	CreatedAt int64
	UpdatedAt int64
}

// Remember upserts by key. created_at survives updates so a fact shows its
// true age; updated_at is what the prompt sorts and refreshes by.
func (db *DB) Remember(key, value string) error {
	_, err := db.conn.Exec(
		`INSERT INTO user_memories (key, value, created_at, updated_at)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, nowUnixMs(), nowUnixMs(),
	)
	if err != nil {
		return fmt.Errorf("storage: memory save failed: %w", err)
	}
	return nil
}

// ForgetMemory deletes one key and reports whether it existed.
func (db *DB) ForgetMemory(key string) (bool, error) {
	res, err := db.conn.Exec(`DELETE FROM user_memories WHERE key = ?`, key)
	if err != nil {
		return false, fmt.Errorf("storage: memory delete failed: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("storage: memory delete failed: %w", err)
	}
	return n > 0, nil
}

// ListMemories returns the freshest memories first; limit <= 0 means all.
func (db *DB) ListMemories(limit int) ([]Memory, error) {
	q := `SELECT key, value, created_at, updated_at FROM user_memories ORDER BY updated_at DESC`
	args := []any{}
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := db.conn.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("storage: memory list failed: %w", err)
	}
	defer rows.Close()
	var out []Memory
	for rows.Next() {
		var m Memory
		if err := rows.Scan(&m.Key, &m.Value, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, fmt.Errorf("storage: memory list failed: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: memory list failed: %w", err)
	}
	return out, nil
}

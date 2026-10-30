package Storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

type DB struct{ conn *sql.DB }

func DefaultPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "argus", "argus.db"), nil
}

func Open(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("storage: could not create data dir: %w", err)
	}
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("storage: could not open database: %w", err)
	}
	conn.SetMaxOpenConns(1)
	db := &DB{conn: conn}
	if _, err := conn.Exec(`PRAGMA journal_mode=WAL`); err != nil {
		conn.Close()
		return nil, err
	}
	if _, err := conn.Exec(`PRAGMA busy_timeout=5000`); err != nil {
		conn.Close()
		return nil, err
	}
	if _, err := conn.Exec(`PRAGMA foreign_keys=ON`); err != nil {
		conn.Close()
		return nil, err
	}
	if err := db.migrate(); err != nil {
		conn.Close()
		return nil, err
	}
	return db, nil
}

func (db *DB) Close() error {
	if db.conn == nil {
		return nil
	}
	return db.conn.Close()
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullInt(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}

func b2i(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func likePrefix(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch r {
		case '%', '_', '\\':
			out = append(out, '\\', r)
		default:
			out = append(out, r)
		}
	}
	return string(out)
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}

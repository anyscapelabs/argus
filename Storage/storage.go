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
	db := &DB{conn: conn}
	if err := db.migrate(); err != nil {
		conn.Close()
		return nil, err
	}
	return db, nil
}

func (db *DB) migrate() error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS provider_configs (
			provider_id TEXT PRIMARY KEY,
			secret_ref TEXT NOT NULL,
			key_last4 TEXT NOT NULL,
			validated_at TEXT NOT NULL,
			base_url_override TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS model_slots (
			slot TEXT PRIMARY KEY,
			model_ref TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS custom_models (
			provider_id TEXT NOT NULL,
			model_id TEXT NOT NULL,
			display_name TEXT NOT NULL,
			context_window INTEGER,
			max_output INTEGER,
			tools INTEGER NOT NULL,
			vision INTEGER NOT NULL,
			reasoning INTEGER NOT NULL,
			price_input REAL,
			price_output REAL,
			created_at TEXT NOT NULL,
			PRIMARY KEY(provider_id, model_id)
		)`,
		`CREATE TABLE IF NOT EXISTS provider_selected_models (
			provider_id TEXT NOT NULL,
			model_ref TEXT NOT NULL,
			position INTEGER NOT NULL,
			selected_at TEXT NOT NULL,
			PRIMARY KEY(provider_id, model_ref)
		)`,
	}
	for _, q := range statements {
		if _, err := db.conn.Exec(q); err != nil {
			return fmt.Errorf("storage: migration failed: %w", err)
		}
	}
	return nil
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

package Storage

import (
	"database/sql"
	"fmt"
	"time"
)

// migration7 adds the app-wide settings store. Lightweight per-install
// preferences — like the location manual override — live here as key/value
// rows.
func migration7(db *DB) error {
	st := `CREATE TABLE IF NOT EXISTS app_settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at INTEGER NOT NULL
	)`
	if _, err := db.conn.Exec(st); err != nil {
		return err
	}
	return nil
}

func (db *DB) SetAppSetting(key, value string) error {
	_, err := db.conn.Exec(
		`INSERT INTO app_settings (key, value, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, time.Now().UnixMilli(),
	)
	if err != nil {
		return fmt.Errorf("storage: app setting save failed: %w", err)
	}
	return nil
}

func (db *DB) GetAppSetting(key string) (string, bool, error) {
	row := db.conn.QueryRow(`SELECT value FROM app_settings WHERE key = ?`, key)
	var value string
	err := row.Scan(&value)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("storage: app setting read failed: %w", err)
	}
	return value, true, nil
}

// AllAppSettings returns every persisted preference in one read. The frontend
// loads this once at startup so appearance choices no longer depend on the
// webview's localStorage surviving a restart.
func (db *DB) AllAppSettings() (map[string]string, error) {
	rows, err := db.conn.Query(`SELECT key, value FROM app_settings`)
	if err != nil {
		return nil, fmt.Errorf("storage: app settings list failed: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("storage: app settings scan failed: %w", err)
		}
		out[key] = value
	}
	return out, rows.Err()
}

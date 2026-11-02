package Storage

import (
	"fmt"
	"strings"
)

func (db *DB) migrate() error {
	var version int
	if err := db.conn.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("storage: could not read schema version: %w", err)
	}

	migrations := []func(*DB) error{
		migration1,
		migration2,
		migration3,
		migration4,
		migration5,
		migration6,
	}

	for i := version; i < len(migrations); i++ {
		if err := migrations[i](db); err != nil {
			return fmt.Errorf("storage: migration %d failed: %w", i+1, err)
		}
		if _, err := db.conn.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, i+1)); err != nil {
			return fmt.Errorf("storage: could not bump schema version: %w", err)
		}
	}
	return nil
}

func migration1(db *DB) error {
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
		`CREATE TABLE IF NOT EXISTS agent_profiles (
			id TEXT PRIMARY KEY,
			avatar TEXT NOT NULL DEFAULT '',
			agent_name TEXT NOT NULL DEFAULT '',
			model_ref TEXT,
			updated_at TEXT NOT NULL
		)`,
	}
	for _, q := range statements {
		if _, err := db.conn.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

func migration2(db *DB) error {
	statements := []string{
		`ALTER TABLE agent_profiles ADD COLUMN system_prompt TEXT NOT NULL DEFAULT ''`,
		`CREATE TABLE IF NOT EXISTS messages (
			id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL REFERENCES agent_profiles(id) ON DELETE CASCADE,
			seq INTEGER NOT NULL,
			role TEXT NOT NULL CHECK (role IN ('user','assistant')),
			blocks_json TEXT NOT NULL,
			text TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL CHECK (status IN ('complete','streaming','error','cancelled','interrupted')),
			stop_reason TEXT,
			model_ref TEXT,
			created_at INTEGER NOT NULL,
			completed_at INTEGER,
			UNIQUE (agent_id, seq)
		)`,
		`CREATE TABLE IF NOT EXISTS model_requests (
			id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL REFERENCES agent_profiles(id) ON DELETE CASCADE,
			message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
			attempt INTEGER NOT NULL DEFAULT 1,
			provider_id TEXT NOT NULL,
			model_id TEXT NOT NULL,
			model_ref TEXT NOT NULL,
			adapter_family TEXT NOT NULL,
			base_host TEXT NOT NULL,
			slot TEXT,
			reasoning_level TEXT,
			temperature REAL,
			max_output_tokens INTEGER,
			system_prompt TEXT NOT NULL DEFAULT '',
			context_through_seq INTEGER NOT NULL,
			context_message_count INTEGER NOT NULL,
			status TEXT NOT NULL CHECK (status IN ('started','success','error','cancelled','interrupted')),
			http_status INTEGER,
			provider_request_id TEXT,
			stop_reason TEXT,
			error_code TEXT,
			error_message TEXT,
			error_retryable INTEGER,
			error_raw TEXT,
			usage_source TEXT NOT NULL DEFAULT 'missing' CHECK (usage_source IN ('provider','missing')),
			input_tokens INTEGER,
			cache_read_tokens INTEGER,
			cache_write_tokens INTEGER,
			output_tokens INTEGER,
			reasoning_tokens INTEGER,
			usage_raw_json TEXT,
			cost_usd REAL,
			price_json TEXT,
			started_at INTEGER NOT NULL,
			first_byte_at INTEGER,
			first_token_at INTEGER,
			completed_at INTEGER
		)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_agent_seq ON messages(agent_id, seq)`,
		`CREATE INDEX IF NOT EXISTS idx_requests_agent ON model_requests(agent_id, started_at)`,
		`CREATE INDEX IF NOT EXISTS idx_requests_message ON model_requests(message_id)`,
		`CREATE INDEX IF NOT EXISTS idx_requests_status ON model_requests(status)`,
	}
	for _, q := range statements {
		if _, err := db.conn.Exec(q); err != nil {
			if strings.Contains(err.Error(), "duplicate column name") {
				continue
			}
			return err
		}
	}
	return nil
}

func migration3(db *DB) error {
	stmt := `ALTER TABLE messages ADD COLUMN error_message TEXT NOT NULL DEFAULT ''`
	if _, err := db.conn.Exec(stmt); err != nil {
		if strings.Contains(err.Error(), "duplicate column name") {
			return nil
		}
		return err
	}
	return nil
}

func migration4(db *DB) error {
	stmt := `CREATE TABLE IF NOT EXISTS permission_grants (
		agent_id TEXT NOT NULL,
		tool TEXT NOT NULL,
		mode TEXT NOT NULL CHECK (mode IN ('allow','deny')),
		created_at INTEGER NOT NULL,
		PRIMARY KEY (agent_id, tool)
	)`
	if _, err := db.conn.Exec(stmt); err != nil {
		return err
	}
	return nil
}

func migration5(db *DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS permission_notices (
			id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL,
			agent_name TEXT NOT NULL DEFAULT '',
			tool TEXT NOT NULL,
			command TEXT NOT NULL DEFAULT '',
			decision TEXT NOT NULL,
			created_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_notices_agent ON permission_notices(agent_id, created_at)`,
	}
	for _, q := range statements {
		if _, err := db.conn.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

// migration6 turns the permission decision log into a general notice log: an
// MCP sign-in that settled is the same kind of fact the user may want to see
// again after a restart, and a restored card needs to know which row produced
// it. Rows written before this migration read back as kind "permission".
func migration6(db *DB) error {
	statements := []string{
		`ALTER TABLE permission_notices ADD COLUMN kind TEXT NOT NULL DEFAULT 'permission'`,
		`ALTER TABLE permission_notices ADD COLUMN notice_text TEXT NOT NULL DEFAULT ''`,
	}
	for _, q := range statements {
		if _, err := db.conn.Exec(q); err != nil {
			if strings.Contains(err.Error(), "duplicate column name") {
				continue
			}
			return err
		}
	}
	return nil
}

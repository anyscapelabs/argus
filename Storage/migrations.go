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
		migration7,
		migration8,
		migration9,
		migration10,
		migration11,
		migration12,
		migration13,
		migration14,
		migration15,
		migration16,
		migration17,
		migration18,
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

// migration8 creates the durable-fact store. One row per key, upserted by the
// agent's remember tool, so a changed preference overwrites the old one
// instead of contradicting it later.
func migration8(db *DB) error {
	stmt := `CREATE TABLE IF NOT EXISTS user_memories (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	)`
	if _, err := db.conn.Exec(stmt); err != nil {
		return err
	}
	return nil
}

// migration9 creates the scheduler store. Routines and reminders the user
// asks the agent for live here, with next_run_at as the single source of
// truth: schedules survive restarts and missed fires catch up instead of
// being lost with an in-memory timer. schedule_json is opaque to storage —
// the Scheduler spec package owns its shape.
func migration9(db *DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS scheduled_tasks (
			id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL,
			kind TEXT NOT NULL CHECK (kind IN ('routine','reminder')),
			title TEXT NOT NULL,
			prompt TEXT NOT NULL,
			schedule_json TEXT NOT NULL,
			next_run_at INTEGER NOT NULL DEFAULT 0,
			last_run_at INTEGER NOT NULL DEFAULT 0,
			last_status TEXT NOT NULL DEFAULT '',
			paused INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_scheduled_due ON scheduled_tasks(next_run_at)`,
		`CREATE TABLE IF NOT EXISTS scheduled_runs (
			id TEXT PRIMARY KEY,
			task_id TEXT NOT NULL REFERENCES scheduled_tasks(id) ON DELETE CASCADE,
			agent_id TEXT NOT NULL,
			message_id TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL CHECK (status IN ('started','success','error')),
			error TEXT NOT NULL DEFAULT '',
			started_at INTEGER NOT NULL,
			completed_at INTEGER
		)`,
		`CREATE INDEX IF NOT EXISTS idx_runs_task ON scheduled_runs(task_id, started_at)`,
	}
	for _, q := range statements {
		if _, err := db.conn.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

// migration10 adds the message kind. An empty kind is an ordinary turn: the
// user typed it or the agent wrote it, and it belongs in the transcript.
// "steering" marks a row the app wrote on the user's behalf when something
// out of band finished — an MCP connection settling, for example. The model
// reads those rows so it knows what happened; the chat UI hides them.
// SQLite cannot attach a CHECK to an added column, so the allowed values are
// enforced by the Storage constants rather than by the schema.
func migration10(db *DB) error {
	stmt := `ALTER TABLE messages ADD COLUMN kind TEXT NOT NULL DEFAULT ''`
	if _, err := db.conn.Exec(stmt); err != nil {
		if strings.Contains(err.Error(), "duplicate column name") {
			return nil
		}
		return err
	}
	return nil
}

// migration11 creates the artifacts tables. An artifact is one document the
// agent wrote: the body is canonical Markdown, and every other file type the
// user exports is derived from that body rather than authored separately.
// artifact_versions holds the body an edit replaced, so a rewrite stays
// undoable; the count is pruned by Storage.MaxArtifactVersions.
func migration11(db *DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS artifacts (
			id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL,
			title TEXT NOT NULL DEFAULT '',
			format TEXT NOT NULL DEFAULT 'markdown',
			body TEXT NOT NULL,
			words INTEGER NOT NULL DEFAULT 0,
			bytes INTEGER NOT NULL DEFAULT 0,
			version INTEGER NOT NULL DEFAULT 1,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_artifacts_agent ON artifacts(agent_id, updated_at DESC)`,
		`CREATE TABLE IF NOT EXISTS artifact_versions (
			id TEXT PRIMARY KEY,
			artifact_id TEXT NOT NULL,
			version INTEGER NOT NULL,
			title TEXT NOT NULL DEFAULT '',
			body TEXT NOT NULL,
			words INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_artifact_versions ON artifact_versions(artifact_id, version DESC)`,
	}
	for _, q := range statements {
		if _, err := db.conn.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

// migration12 gives artifacts their metadata column. The validator reads a
// document's front matter and strips it from the body, so without somewhere to
// keep it a "paper: a4" or "author:" line would be parsed, checked and then
// thrown away — the worst possible outcome for a key the writer was told exists.
// It holds a JSON object of the approved keys; an empty object means a document
// with no metadata, which every exporter reads as its own default.
func migration12(db *DB) error {
	stmt := `ALTER TABLE artifacts ADD COLUMN meta TEXT NOT NULL DEFAULT '{}'`
	if _, err := db.conn.Exec(stmt); err != nil {
		if strings.Contains(err.Error(), "duplicate column name") {
			return nil
		}
		return err
	}
	return nil
}

// migration13 creates the account_entitlements cache used for offline auth. One
// row holds the last successful entitlements snapshot as JSON plus the unix
// timestamp it was fetched, so a paired app can boot without the network.
func migration13(db *DB) error {
	stmt := `CREATE TABLE IF NOT EXISTS account_entitlements (
		id TEXT PRIMARY KEY,
		payload TEXT NOT NULL,
		fetched_at INTEGER NOT NULL
	)`
	if _, err := db.conn.Exec(stmt); err != nil {
		return err
	}
	return nil
}

// migration14 adds the attachments table (files the user uploaded into a chat)
// and lets artifacts describe slide decks.
//
// An attachment is the metadata for one uploaded file. The bytes live on disk,
// never in SQLite: a 10 MB scan does not belong in a row next to message text.
// data_path names that file, and extracted_text is what the agent actually reads
// for a PDF or DOCX — held here so the same upload can be replayed into history
// without re-parsing the file every turn.
//
// The artifacts change widens the single "markdown" authoring format to include
// slide decks. A deck is authored as slide JSON, but its stored body is the same
// body column (JSON there, not markdown); file_path holds the generated .pptx so
// the user can save it without LibreOffice re-rendering it each time.
func migration14(db *DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS attachments (
			id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL REFERENCES agent_profiles(id) ON DELETE CASCADE,
			filename TEXT NOT NULL,
			mime_type TEXT NOT NULL,
			size_bytes INTEGER NOT NULL DEFAULT 0,
			data_path TEXT NOT NULL,
			extracted_text TEXT NOT NULL DEFAULT '',
			created_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_attachments_agent ON attachments(agent_id, created_at DESC)`,
	}
	for _, q := range statements {
		if _, err := db.conn.Exec(q); err != nil {
			return err
		}
	}
	// kind is "document" (the default) or "deck". file_path is where a derived
	// binary — the generated .pptx — was stored, empty for plain documents.
	alters := []string{
		`ALTER TABLE artifacts ADD COLUMN kind TEXT NOT NULL DEFAULT 'document'`,
		`ALTER TABLE artifacts ADD COLUMN file_path TEXT NOT NULL DEFAULT ''`,
	}
	for _, stmt := range alters {
		if _, err := db.conn.Exec(stmt); err != nil {
			if strings.Contains(err.Error(), "duplicate column name") {
				continue
			}
			return err
		}
	}
	return nil
}

// migration15 adds structured, agent-scoped memories, records which memories a
// request actually saw, and persists one reversible reaction per message.
func migration15(db *DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS memory_items (
			id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL REFERENCES agent_profiles(id) ON DELETE CASCADE,
			key TEXT NOT NULL,
			value TEXT NOT NULL,
			scope TEXT NOT NULL CHECK (scope IN ('always','contextual')),
			confidence REAL NOT NULL CHECK (confidence >= 0 AND confidence <= 1),
			source_type TEXT NOT NULL,
			source_message_id TEXT NOT NULL DEFAULT '',
			source_run_id TEXT NOT NULL DEFAULT '',
			revision INTEGER NOT NULL DEFAULT 1,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			UNIQUE(agent_id, key)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_memory_items_agent ON memory_items(agent_id, updated_at DESC)`,
		`CREATE TABLE IF NOT EXISTS memory_exposures (
			request_id TEXT NOT NULL REFERENCES model_requests(id) ON DELETE CASCADE,
			memory_id TEXT NOT NULL REFERENCES memory_items(id) ON DELETE CASCADE,
			memory_revision INTEGER NOT NULL,
			position INTEGER NOT NULL,
			score INTEGER NOT NULL,
			estimated_tokens INTEGER NOT NULL,
			created_at INTEGER NOT NULL,
			PRIMARY KEY(request_id, memory_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_memory_exposures_item ON memory_exposures(memory_id, memory_revision)`,
		`CREATE TABLE IF NOT EXISTS message_reactions (
			message_id TEXT PRIMARY KEY REFERENCES messages(id) ON DELETE CASCADE,
			emoji TEXT NOT NULL CHECK (emoji <> ''),
			signal INTEGER NOT NULL DEFAULT 0 CHECK (signal IN (-1,0,1)),
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_message_reactions_updated ON message_reactions(updated_at DESC)`,
		`INSERT OR IGNORE INTO agent_profiles (id, avatar, agent_name, updated_at)
		 SELECT 'default', '/logo.png', 'Argus', CURRENT_TIMESTAMP
		 WHERE EXISTS (SELECT 1 FROM user_memories)`,
		`INSERT OR IGNORE INTO memory_items (
			id, agent_id, key, value, scope, confidence, source_type,
			source_message_id, source_run_id, revision, created_at, updated_at
		) SELECT 'legacy:default:' || key, 'default', key, value, 'always', 0.70,
			'legacy', '', '', 1, created_at, updated_at FROM user_memories
			WHERE EXISTS (SELECT 1 FROM agent_profiles WHERE id = 'default')`,
	}
	for _, q := range statements {
		if _, err := db.conn.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

// migration16 adds the per-agent seq counter the send path allocates message
// sequence numbers from. Before this, NextSeq computed MAX(seq)+1 on every
// insert — one full index scan per row written. The counter is initialised
// from the current maximum so existing agents keep their numbering.
func migration16(db *DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS seq_counters (
			agent_id TEXT PRIMARY KEY,
			next_seq INTEGER NOT NULL DEFAULT 1
		)`,
		`INSERT OR IGNORE INTO seq_counters (agent_id, next_seq)
			SELECT agent_id, MAX(seq) + 1 FROM messages GROUP BY agent_id`,
	}
	for _, q := range statements {
		if _, err := db.conn.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

func migration17(db *DB) error {
	stmt := `ALTER TABLE messages ADD COLUMN error_code TEXT NOT NULL DEFAULT ''`
	if _, err := db.conn.Exec(stmt); err != nil {
		if strings.Contains(err.Error(), "duplicate column name") {
			return nil
		}
		return err
	}
	return nil
}

// migration18 adds agent skill activation overrides, 'avoid' scope support
// on memory_items, and learned MCP tool recovery lessons.
func migration18(db *DB) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS agent_skills (
			agent_id TEXT NOT NULL REFERENCES agent_profiles(id) ON DELETE CASCADE,
			skill_id TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY (agent_id, skill_id)
		)`,
		`CREATE TABLE IF NOT EXISTS learned_tool_lessons (
			agent_id TEXT NOT NULL REFERENCES agent_profiles(id) ON DELETE CASCADE,
			server_id TEXT NOT NULL,
			tool_name TEXT NOT NULL,
			lesson TEXT NOT NULL,
			hit_count INTEGER NOT NULL DEFAULT 1,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY (agent_id, tool_name)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_tool_lessons_server ON learned_tool_lessons(agent_id, server_id, updated_at DESC)`,
		`CREATE TABLE IF NOT EXISTS memory_avoid_rules (
			id TEXT PRIMARY KEY,
			agent_id TEXT NOT NULL REFERENCES agent_profiles(id) ON DELETE CASCADE,
			key TEXT NOT NULL,
			value TEXT NOT NULL,
			updated_at INTEGER NOT NULL,
			UNIQUE(agent_id, key)
		)`,
	}
	for _, q := range statements {
		if _, err := db.conn.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

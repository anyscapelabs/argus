package Storage

import (
	"database/sql"
	"fmt"
	"os"
)

// accountTables lists every local table whose rows belong to whichever
// account was paired when they were written. A different account pairing on
// this install must not see the previous tenant's rows (chats, memories,
// artifacts, permissions), so `PurgeAccountTables` wipes them by identity
// change, never on a plain sign-out.
var accountTables = []string{
	"message_reactions",
	"attachments",
	"artifact_versions",
	"artifacts",
	"model_requests",
	"messages",
	"memory_exposures",
	"memory_items",
	"user_memories",
	"scheduled_runs",
	"scheduled_tasks",
	"permission_notices",
	"permission_grants",
	"provider_selected_models",
	"model_slots",
	"custom_models",
	"agent_profiles",
}

// PurgeAccountTables deletes every account-scoped row and the entitlement
// cache. Attachment bytes under data_path are unlinked first; tables missing
// on older schemas are skipped. A partial purge still beats a leak — the
// caller retries on the next entitlements proof.
func (db *DB) PurgeAccountTables() error {
	paths, err := db.attachmentPaths()
	if err == nil {
		for _, p := range paths {
			_ = os.Remove(p)
		}
	}
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("storage: account purge begin failed: %w", err)
	}
	for _, table := range accountTables {
		if _, err := tx.Exec("DELETE FROM " + table); err != nil {
			// An older schema may not have the table yet; keep going.
			continue
		}
	}
	if _, err := tx.Exec("DELETE FROM account_entitlements"); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("storage: account purge entitlements failed: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: account purge commit failed: %w", err)
	}
	return nil
}

func (db *DB) attachmentPaths() ([]string, error) {
	rows, err := db.conn.Query("SELECT data_path FROM attachments")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var path sql.NullString
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		if path.Valid && path.String != "" {
			out = append(out, path.String)
		}
	}
	return out, rows.Err()
}

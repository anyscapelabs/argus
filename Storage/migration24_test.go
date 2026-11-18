package Storage

import "testing"

func TestMigration24AddsUpdatedAtToOldAppSettings(t *testing.T) {
	db := openTestDB(t)

	// Recreate the pre-updated_at shape older installs still carry: the
	// column was folded into migration7's CREATE TABLE, which never re-runs.
	if _, err := db.conn.Exec(`DROP TABLE app_settings`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.conn.Exec(`CREATE TABLE app_settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`); err != nil {
		t.Fatal(err)
	}

	if err := migration24(db); err != nil {
		t.Fatal(err)
	}

	if err := db.SetAppSetting("muted_agent_ids", `["a1"]`); err != nil {
		t.Fatalf("SetAppSetting after migration24: %v", err)
	}
	value, found, err := db.GetAppSetting("muted_agent_ids")
	if err != nil || !found || value != `["a1"]` {
		t.Fatalf("GetAppSetting = %q found=%v err=%v", value, found, err)
	}

	// Running it again on a repaired table must be a no-op.
	if err := migration24(db); err != nil {
		t.Fatalf("second migration24: %v", err)
	}
	if err := db.SetAppSetting("muted_agent_ids", `["a2"]`); err != nil {
		t.Fatalf("SetAppSetting after second migration24: %v", err)
	}
}

package Storage

import "testing"

func TestMigration15ImportsLegacyMemoryIntoDefaultAgent(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.conn.Exec(
		`INSERT INTO user_memories (key, value, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		"timezone", "Europe/London", 10, 20,
	); err != nil {
		t.Fatal(err)
	}
	if err := migration15(db); err != nil {
		t.Fatal(err)
	}

	if _, found, err := db.GetAgentProfile("default"); err != nil || !found {
		t.Fatalf("default profile was not created for legacy data: found=%v err=%v", found, err)
	}
	memory, found, err := db.GetMemory("default", "timezone")
	if err != nil || !found {
		t.Fatalf("legacy memory was not imported: found=%v err=%v", found, err)
	}
	if memory.ID != "legacy:default:timezone" || memory.Value != "Europe/London" || memory.Scope != "always" || memory.SourceType != "legacy" {
		t.Fatalf("legacy memory was imported incorrectly: %+v", memory)
	}
}

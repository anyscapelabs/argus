package Storage

import (
	"path/filepath"
	"testing"
)

// seedAgent inserts the agent profile a message row needs, because
// messages.agent_id is a real foreign key.
func seedAgent(t *testing.T, db *DB, id string) {
	t.Helper()
	if err := db.SaveAgentProfile(AgentProfile{ID: id, AgentName: "Argus", ModelRef: "x/y"}); err != nil {
		t.Fatalf("seed agent: %v", err)
	}
}

func TestSteeringKindRoundTrip(t *testing.T) {
	db := openTestDB(t)
	seedAgent(t, db, "default")

	// An ordinary turn writes no kind, and a steering row reads back as one.
	plain := Message{ID: "m1", AgentID: "default", Seq: 1, Role: "user", Text: "hello", BlocksJSON: "[]", Status: "complete"}
	steer := Message{ID: "m2", AgentID: "default", Seq: 2, Role: "user", Kind: MessageKindSteering, Text: "gmail connected", BlocksJSON: "[]", Status: "complete"}
	for _, m := range []Message{plain, steer} {
		if err := db.InsertMessage(m); err != nil {
			t.Fatalf("insert %s: %v", m.ID, err)
		}
	}

	got, found, err := db.GetMessage("m2")
	if err != nil || !found {
		t.Fatalf("get m2: found=%v err=%v", found, err)
	}
	if got.Kind != MessageKindSteering {
		t.Errorf("GetMessage kind = %q, want %q", got.Kind, MessageKindSteering)
	}

	listed, err := db.ListMessages("default", 0, 50)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("listed %d rows, want 2", len(listed))
	}
	if listed[0].Kind != "" {
		t.Errorf("ordinary row kind = %q, want empty", listed[0].Kind)
	}
	if listed[1].Kind != MessageKindSteering {
		t.Errorf("steering row kind = %q, want %q", listed[1].Kind, MessageKindSteering)
	}
}

// Re-running migration10 on a database that already has the column must
// swallow the duplicate-column error: that guard is what keeps an existing
// install booting after the change ships.
func TestReopenRunsKindMigrationAgain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := migration10(db); err != nil {
		t.Fatalf("migration10 re-run: %v", err)
	}

	seedAgent(t, db, "default")
	if err := db.InsertMessage(Message{ID: "m1", AgentID: "default", Seq: 1, Role: "user", Kind: MessageKindSteering, Text: "x", BlocksJSON: "[]", Status: "complete"}); err != nil {
		t.Fatalf("insert after re-run: %v", err)
	}
}

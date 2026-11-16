package Storage

import "testing"

func TestMigration21RepairsDefaultAgentAvatar(t *testing.T) {
	db := openTestDB(t)
	if _, err := db.conn.Exec(`UPDATE agent_profiles SET avatar = '/logo.png' WHERE id = 'default'`); err != nil {
		t.Fatal(err)
	}
	if err := migration21(db); err != nil {
		t.Fatal(err)
	}

	p, found, err := db.GetAgentProfile("default")
	if err != nil || !found {
		t.Fatalf("default profile missing after migration21: found=%v err=%v", found, err)
	}
	if p.Avatar != DefaultAgentAvatar {
		t.Fatalf("default avatar was not repaired: got %q want %q", p.Avatar, DefaultAgentAvatar)
	}

	// The hidden memory owner must not pick up a visible avatar.
	p, found, err = db.GetAgentProfile(GlobalMemoryAgentID)
	if err != nil || !found {
		t.Fatalf("memory owner missing after migration21: found=%v err=%v", found, err)
	}
	if p.Avatar != "" {
		t.Fatalf("memory owner avatar was overwritten: got %q", p.Avatar)
	}
}

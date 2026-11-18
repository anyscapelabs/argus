package Permission

import (
	"path/filepath"
	"testing"

	"github.com/anyscapelabs/argus/Storage"
)

func testDB(t *testing.T) *Storage.DB {
	t.Helper()
	db, err := Storage.Open(filepath.Join(t.TempDir(), "permissions.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestResolveStoredGrantUsesAgentOverrideBeforeGlobal(t *testing.T) {
	db := testDB(t)
	if err := db.SetGrant(GlobalAgentID, TerminalTool, Storage.GrantAllow); err != nil {
		t.Fatal(err)
	}
	if err := db.SetGrant("agent-1", TerminalTool, Storage.GrantDeny); err != nil {
		t.Fatal(err)
	}

	grant, found, err := resolveStoredGrant(db, "agent-1", TerminalTool)
	if err != nil || !found {
		t.Fatalf("resolve grant: found=%v err=%v", found, err)
	}
	if grant.Mode != string(Storage.GrantDeny) {
		t.Fatalf("mode = %q, want deny", grant.Mode)
	}

	grant, found, err = resolveStoredGrant(db, "agent-2", TerminalTool)
	if err != nil || !found {
		t.Fatalf("resolve global grant: found=%v err=%v", found, err)
	}
	if grant.Mode != string(Storage.GrantAllow) {
		t.Fatalf("mode = %q, want allow", grant.Mode)
	}
}

func TestResolveStoredGrantUnderstandsBrowserFamilies(t *testing.T) {
	db := testDB(t)
	if err := db.SetGrant(GlobalAgentID, BrowserTool, Storage.GrantDeny); err != nil {
		t.Fatal(err)
	}
	if err := db.SetGrant("agent-1", BrowserHostTool("example.com"), Storage.GrantAllow); err != nil {
		t.Fatal(err)
	}

	grant, found, err := resolveStoredGrant(db, "agent-1", BrowserHostTool("example.com"))
	if err != nil || !found {
		t.Fatalf("resolve host grant: found=%v err=%v", found, err)
	}
	if grant.Mode != string(Storage.GrantAllow) {
		t.Fatalf("mode = %q, want allow", grant.Mode)
	}

	grant, found, err = resolveStoredGrant(db, "agent-1", BrowserHostTool("other.example"))
	if err != nil || !found {
		t.Fatalf("resolve family grant: found=%v err=%v", found, err)
	}
	if grant.Mode != string(Storage.GrantDeny) {
		t.Fatalf("mode = %q, want deny", grant.Mode)
	}
}

package Permission

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anyscapelabs/argus/Services/Shared"
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

func TestUnattendedRequestDefersWithActionableNotice(t *testing.T) {
	db := testDB(t)
	svc := &Service{Core: &Shared.Core{DB: db}}

	decision, err := Request(Shared.WithUnattended(context.Background()), svc,
		"agent-1", "Argus", TerminalTool, "git push --force")
	if err != nil {
		t.Fatal(err)
	}
	if decision != Deferred {
		t.Fatalf("decision = %q, want %q", decision, Deferred)
	}

	notices, err := db.ListPermissionNotices("agent-1", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(notices) != 1 {
		t.Fatalf("notices = %d, want 1", len(notices))
	}
	n := notices[0]
	if n.Decision != Deferred || n.Kind != "permission" {
		t.Fatalf("notice decision/kind = %q/%q, want %q/permission", n.Decision, n.Kind, Deferred)
	}
	if !strings.Contains(n.Body, "git push --force") {
		t.Fatalf("notice must name the held action, got %q", n.Body)
	}
	if !strings.Contains(n.Body, "Reply to me") {
		t.Fatalf("notice must say how to approve, got %q", n.Body)
	}
}

func TestStoredGrantStillWinsUnattended(t *testing.T) {
	db := testDB(t)
	if err := db.SetGrant("agent-1", TerminalTool, Storage.GrantAllow); err != nil {
		t.Fatal(err)
	}
	svc := &Service{Core: &Shared.Core{DB: db}}

	decision, err := Request(Shared.WithUnattended(context.Background()), svc,
		"agent-1", "Argus", TerminalTool, "ls")
	if err != nil {
		t.Fatal(err)
	}
	if decision != AllowAlways {
		t.Fatalf("decision = %q, want %q", decision, AllowAlways)
	}
}

func TestDeferralNoticeTextNamesFamily(t *testing.T) {
	cases := []struct{ tool, want string }{
		{TerminalTool, "a terminal command"},
		{FileAccessTool, "file access"},
		{EmailSendTool, "an email send"},
		{BrowserHostTool("mail.example.com"), "a browser action on mail.example.com"},
	}
	for _, tc := range cases {
		got := DeferralNoticeText(tc.tool, "do the thing")
		if !strings.Contains(got, tc.want) || !strings.Contains(got, "do the thing") ||
			!strings.Contains(got, "Reply to me") {
			t.Fatalf("DeferralNoticeText(%q) = %q, want family %q, detail and approval guidance", tc.tool, got, tc.want)
		}
	}
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

package Chat

import (
	"context"
	"strings"
	"testing"

	"github.com/anyscapelabs/argus/Services/Memory"
	"github.com/anyscapelabs/argus/Storage"
)

func TestDelegationPreservesThreadHopsAndReportRoute(t *testing.T) {
	_, db := newVoiceTestService(t)
	if err := db.SaveAgentProfile(Storage.AgentProfile{ID: "legal", AgentName: "Legal"}); err != nil {
		t.Fatal(err)
	}
	if _, err := Storage.SetAgentHandle(db.SQL(), "default", "Argus"); err != nil {
		t.Fatal(err)
	}
	if _, err := Storage.SetAgentHandle(db.SQL(), "legal", "Legal"); err != nil {
		t.Fatal(err)
	}

	result, err := HandleMessageAgent(
		context.Background(), db.SQL(), "default", "thread-42", 2,
		`{"to":"@legal","task":"Review clause 7","report_back":true}`, nil,
	)
	if err != nil || !strings.Contains(result, "dispatched") {
		t.Fatalf("delegation failed: result=%s err=%v", result, err)
	}
	request, found, err := Storage.ClaimNextAgentMessage(db.SQL(), "legal")
	if err != nil || !found {
		t.Fatalf("delegation was not claimable: found=%v err=%v", found, err)
	}
	if request.ThreadID != "thread-42" || request.Hops != 3 || request.FromAgentID != "default" {
		t.Fatalf("delegation metadata changed: %+v", request)
	}
	if err := FinalizeDelegatedRun(db.SQL(), request, "Clause 7 needs approval.", "completed"); err != nil {
		t.Fatal(err)
	}
	report, found, err := Storage.ClaimNextAgentMessage(db.SQL(), "default")
	if err != nil || !found {
		t.Fatalf("report was not routed back: found=%v err=%v", found, err)
	}
	if report.Kind != "report" || report.ThreadID != request.ThreadID || report.ParentMessageID != request.ID {
		t.Fatalf("report metadata changed: %+v", report)
	}
}

func TestRoleAndNotesPersistOutsideConversationHistory(t *testing.T) {
	_, db := newVoiceTestService(t)
	if _, err := HandleSetAgentRole(db.SQL(), "default", "Support email specialist", "Draft before sending."); err != nil {
		t.Fatal(err)
	}
	profile, found, err := db.GetAgentProfile("default")
	if err != nil || !found || profile.Role != "Support email specialist" || profile.RoleInstructions != "Draft before sending." {
		t.Fatalf("role did not persist: found=%v profile=%+v err=%v", found, profile, err)
	}
	if _, err := HandleSaveAgentNote(db.SQL(), "default", "thread-7", "triage", "Escalate billing disputes.", "agent"); err != nil {
		t.Fatal(err)
	}
	selection, err := Memory.Select(db, "default", "billing dispute", 500, 8, false)
	if err != nil || !strings.Contains(selection.Rendered, "Escalate billing disputes") {
		t.Fatalf("agent note is not in normal retrieval: selection=%+v err=%v", selection, err)
	}
	if _, err := HandleSaveAgentNote(db.SQL(), "default", "thread-7", "case", "ACME-7", "thread"); err != nil {
		t.Fatal(err)
	}
	if prompt := Memory.ThreadPrompt(db, "thread-7"); !strings.Contains(prompt, "case: ACME-7") {
		t.Fatalf("thread note was not shared in-thread: %q", prompt)
	}
}

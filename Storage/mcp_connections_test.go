package Storage

import "testing"

func TestMCPConnectionLabelsAndBindingCascade(t *testing.T) {
	db := openTestDB(t)
	if err := db.SaveAgentProfile(AgentProfile{ID: "support", AgentName: "Support"}); err != nil {
		t.Fatal(err)
	}
	first := MCPConnection{ID: "conn-1", ServerID: "gmail", Label: "Support", Status: "connected"}
	if err := CreateMCPConnection(db.SQL(), first); err != nil {
		t.Fatal(err)
	}
	if err := CreateMCPConnection(db.SQL(), MCPConnection{
		ID: "conn-2", ServerID: "gmail", Label: "support", Status: "connected",
	}); err == nil {
		t.Fatal("case-insensitive duplicate account label was accepted")
	}
	if err := UpsertAgentConnection(db.SQL(), "support", first.ID, "draft"); err != nil {
		t.Fatal(err)
	}
	if err := DeleteMCPConnection(db.SQL(), first.ID); err != nil {
		t.Fatal(err)
	}
	bindings, err := ListAllConnectionBindings(db.SQL())
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 0 {
		t.Fatalf("connection bindings survived account deletion: %+v", bindings)
	}
}

func TestMCPConnectionStatusAndTierValidation(t *testing.T) {
	db := openTestDB(t)
	if err := db.SaveAgentProfile(AgentProfile{ID: "legal", AgentName: "Legal"}); err != nil {
		t.Fatal(err)
	}
	if err := CreateMCPConnection(db.SQL(), MCPConnection{ID: "conn", ServerID: "gmail", Label: "Legal"}); err != nil {
		t.Fatal(err)
	}
	if err := SetMCPConnectionStatus(db.SQL(), "conn", "needs_reauth"); err != nil {
		t.Fatal(err)
	}
	conn, found, err := GetMCPConnection(db.SQL(), "conn")
	if err != nil || !found || conn.Status != "needs_reauth" {
		t.Fatalf("status did not persist: found=%v conn=%+v err=%v", found, conn, err)
	}
	if err := UpsertAgentConnection(db.SQL(), "legal", "conn", "owner"); err == nil {
		t.Fatal("invalid access tier was accepted")
	}
}

package Chat

import (
	"testing"

	"github.com/anyscapelabs/argus/Services/MCP"
	"github.com/anyscapelabs/argus/Storage"
)

func boolPtr(value bool) *bool { return &value }

func TestAccessTiersFailClosedOnToolAnnotations(t *testing.T) {
	read := MCP.RemoteTool{Name: "list_messages", ReadOnly: true}
	draft := MCP.RemoteTool{Name: "create_draft", Destructive: boolPtr(false)}
	unknown := MCP.RemoteTool{Name: "reply"}
	destructive := MCP.RemoteTool{Name: "delete_message", Destructive: boolPtr(true)}
	send := MCP.RemoteTool{Name: "send_email", Destructive: boolPtr(false)}

	if err := checkAccessTier("read", read); err != nil {
		t.Fatalf("read-only annotation was refused: %v", err)
	}
	if err := checkAccessTier("read", draft); err == nil {
		t.Fatal("read tier accepted a mutating tool")
	}
	if err := checkAccessTier("draft", draft); err != nil {
		t.Fatalf("explicit non-destructive draft was refused: %v", err)
	}
	if err := checkAccessTier("draft", unknown); err == nil {
		t.Fatal("draft tier accepted a tool with no safety annotations")
	}
	if err := checkAccessTier("draft", destructive); err == nil {
		t.Fatal("draft tier accepted a destructive tool")
	}
	if err := checkAccessTier("draft", send); err == nil {
		t.Fatal("draft tier accepted direct send merely because it was additive")
	}
	if err := checkAccessTier("full", destructive); err != nil {
		t.Fatalf("full tier refused an annotated tool: %v", err)
	}
}

func TestConnectionPrefixesDisambiguateAccounts(t *testing.T) {
	connections := []struct {
		id, label string
	}{
		{"conn-support", "Support"},
		{"conn-legal", "Legal"},
	}
	// Keep the construction explicit so this test catches projection changes
	// without requiring live MCP sessions.
	prefixes := connectionPrefixes([]Storage.AgentConnection{
		{ConnectionID: connections[0].id, ServerID: "gmail", Label: connections[0].label},
		{ConnectionID: connections[1].id, ServerID: "gmail", Label: connections[1].label},
	})
	if prefixes["conn-support"] != "gmail_support" || prefixes["conn-legal"] != "gmail_legal" {
		t.Fatalf("account prefixes are ambiguous: %+v", prefixes)
	}
}

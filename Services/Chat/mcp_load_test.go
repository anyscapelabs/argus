package Chat

import (
	"strings"
	"testing"

	"github.com/anyscapelabs/argus/Protocol"
)

// The lazy MCP loader: integration schemas cost more than everything else in
// the request combined, so they must ride along only when a turn actually
// needs them.

func TestMCPToolNameKeepsPrefixShape(t *testing.T) {
	if got := mcpToolName("google-calendar", "create_event"); got != "google_calendar__create_event" {
		t.Errorf("name = %q", got)
	}
	if got := mcpToolName("hub", strings.Repeat("x", 70)); len(got) != 64 {
		t.Errorf("the provider limit is 64 characters, got %d", len(got))
	}
}

func TestMCPServersReferencedFindsOnlyUsedServers(t *testing.T) {
	history := []Protocol.Message{
		msg(Protocol.RoleAssistant, toolUse("gmail__send_message")),
		msg(Protocol.RoleUser, toolResult()),
		msg(Protocol.RoleAssistant, toolUse("web_search")), // not a connector
		msg(Protocol.RoleAssistant, toolUse("notion__search")),
	}
	got := mcpServersReferenced(history, []string{"gmail", "notion", "linear"})
	if len(got) != 2 || got[0] != "gmail" || got[1] != "notion" {
		t.Fatalf("want gmail and notion in call order, got %v", got)
	}
	if got := mcpServersReferenced(nil, []string{"gmail"}); got != nil {
		t.Fatalf("no history means no attached schemas, got %v", got)
	}
}

// Every request carries only the compact gateway. Static and integration
// schemas are returned by search_tools only when the run needs them.
func TestAttachToolsShipsCompactGatewayOnly(t *testing.T) {
	s := &Service{}
	req := Protocol.Request{}
	s.attachTools(&req, &Protocol.Target{Family: Protocol.FamilyOpenAICompat}, nil)
	names := toolNames(req.Tools)
	for _, name := range []string{"conversation_read", "web_search", "search_tools", "invoke_tool"} {
		if !names[name] {
			t.Errorf("gateway is missing %s", name)
		}
	}
	if len(names) != 4 {
		t.Fatalf("provider received %d tools instead of the four-tool gateway: %v", len(names), names)
	}
}

func TestRunMCPLoadWithoutConnectionIsAnError(t *testing.T) {
	s := &Service{}
	call := Protocol.ToolCall{Name: mcpLoadToolName, Input: `{"server_id":"gmail"}`, Valid: true}
	_, defs, block := s.runMCPLoad(call, map[string]bool{})
	if defs != nil {
		t.Fatal("no schemas can come back without an MCP service")
	}
	if !block.IsError {
		t.Fatalf("want an error result the model can act on, got %+v", block)
	}
	if !strings.Contains(block.Text, "connect_mcp") {
		t.Errorf("the result must point at the connect flow: %q", block.Text)
	}
}

func TestRunMCPLoadAlreadyLoadedDoesNotStackSchemas(t *testing.T) {
	s := &Service{}
	call := Protocol.ToolCall{Name: mcpLoadToolName, Input: `{"server_id":"github"}`, Valid: true}
	_, defs, block := s.runMCPLoad(call, map[string]bool{"github": true})
	if defs != nil {
		t.Fatal("a second load must not return another copy of the schemas")
	}
	if block.IsError || !strings.Contains(block.Text, "already attached") {
		t.Fatalf("want the already-loaded acknowledgement, got %+v", block)
	}
}

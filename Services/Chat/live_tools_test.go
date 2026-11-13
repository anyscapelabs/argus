package Chat

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Storage"
)

func TestEveryAdvertisedBaseToolHasRunner(t *testing.T) {
	s := &Service{}
	for _, def := range s.baseToolDefs() {
		if _, ok := toolRunners[def.Name]; !ok {
			t.Errorf("advertised tool %q has no runner", def.Name)
		}
	}
}

func TestLiveDeclarationsStayCompactAndUseGeminiTypes(t *testing.T) {
	defs := liveFunctionDeclarations()
	if len(defs) != 6 {
		t.Fatalf("live setup has %d declarations, want 6", len(defs))
	}
	want := map[string]bool{
		"conversation_read": true,
		"web_search":        true,
		"search_tools":      true,
		"invoke_tool":       true,
		"read_skill":        true,
		"install_skill":     true,
	}
	for _, def := range defs {
		if !want[def.Name] {
			t.Errorf("unexpected live declaration %q", def.Name)
		}
		if def.Behavior != "NON_BLOCKING" {
			t.Errorf("%s behavior = %q", def.Name, def.Behavior)
		}
		if def.Parameters["type"] != "OBJECT" {
			t.Errorf("%s root type = %#v, want OBJECT", def.Name, def.Parameters["type"])
		}
	}
}

func TestLiveToolMustBeDiscoveredBeforeInvocation(t *testing.T) {
	s, _ := newVoiceTestService(t)
	sessionID := s.beginLiveToolSession("default", "Argus")
	t.Cleanup(func() { s.EndLiveSession(sessionID) })

	invoke := `{"name":"recall_memories","arguments_json":"{\"query\":\"preferences\",\"limit\":8}"}`
	if res := s.RunLiveTool(sessionID, "c1", "invoke_tool", invoke); !res.IsError {
		t.Fatalf("undiscovered tool should fail, got %+v", res)
	}

	search := s.RunLiveTool(sessionID, "c2", "search_tools", `{"query":"recall saved preferences","limit":3}`)
	if search.IsError {
		t.Fatalf("search_tools failed: %+v", search)
	}
	var payload struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal([]byte(search.Output), &payload); err != nil {
		t.Fatalf("search output is not JSON: %v", err)
	}
	found, foundMutation := false, false
	for _, tool := range payload.Tools {
		found = found || tool.Name == "recall_memories"
		foundMutation = foundMutation || tool.Name == "remember" || tool.Name == "forget"
	}
	if !found {
		t.Fatalf("recall_memories not found in %s", search.Output)
	}
	if foundMutation {
		t.Fatalf("live discovery exposed memory mutation: %s", search.Output)
	}
	if res := s.RunLiveTool(sessionID, "c3", "invoke_tool", invoke); res.IsError {
		t.Fatalf("discovered tool failed: %+v", res)
	}
}

func TestLivePromptUsesProfileWithoutToolDump(t *testing.T) {
	s, _ := newVoiceTestService(t)
	prompt := s.liveSystemPrompt(Storage.AgentProfile{
		ID: "default", AgentName: "Nova", SystemPrompt: "Call the user Sam.",
	})
	for _, text := range []string{"You are Nova", "Call the user Sam.", "search_tools", "invoke_tool", "read_skill"} {
		if !strings.Contains(prompt, text) {
			t.Errorf("live prompt missing %q", text)
		}
	}
	if strings.Contains(prompt, "### create_slides") || strings.Contains(prompt, "### execute_command") {
		t.Fatal("live prompt dumped individual tool guidance")
	}
}

func TestTypedGatewayDiscoversThenInvokesTool(t *testing.T) {
	s, _ := newVoiceTestService(t)
	discovered := map[string]bool{}
	search, _ := s.runTypedGatewayTool(
		context.Background(), "run-1", "default", "Argus", discovered,
		Protocol.ToolCall{ID: "c1", Name: "search_tools", Input: `{"query":"recall saved user preferences"}`, Valid: true},
	)
	if search.IsError || !discovered["recall_memories"] {
		t.Fatalf("recall_memories was not discovered: %+v", search)
	}
	result, inner := s.runTypedGatewayTool(
		context.Background(), "run-1", "default", "Argus", discovered,
		Protocol.ToolCall{ID: "c2", Name: "invoke_tool", Input: `{"name":"recall_memories","arguments_json":"{\"query\":\"tone\",\"limit\":8}"}`, Valid: true},
	)
	if result.IsError || inner != "recall_memories" || result.ToolName != "invoke_tool" || result.ToolRefID != "c2" {
		t.Fatalf("gateway invocation failed or broke outer pairing: inner=%q result=%+v", inner, result)
	}
}

func TestGatewayReadSkillUnlocksSchemasAndUnknownSkillErrors(t *testing.T) {
	s, _ := newVoiceTestService(t)
	discovered := map[string]bool{}
	res, _ := s.runTypedGatewayTool(
		context.Background(), "run-2", "default", "Argus", discovered,
		Protocol.ToolCall{ID: "s1", Name: "read_skill", Input: `{"name":"context7"}`, Valid: true},
	)
	if res.IsError {
		t.Fatalf("read_skill failed: %+v", res)
	}
	if !strings.Contains(res.Text, "Context7") || !strings.Contains(res.Text, "# Context7 Workflow") {
		t.Fatalf("read_skill did not return skill workflow: %s", res.Text)
	}
	bad, _ := s.runTypedGatewayTool(
		context.Background(), "run-2", "default", "Argus", discovered,
		Protocol.ToolCall{ID: "s2", Name: "read_skill", Input: `{"name":"nope-skill"}`, Valid: true},
	)
	if !bad.IsError || !strings.Contains(bad.Text, "skill not found") {
		t.Fatalf("unknown skill should error clearly: %+v", bad)
	}
}

func TestLiveToolScoreUsesSynonyms(t *testing.T) {
	if score := liveToolScore("draft an email", Protocol.ToolDef{Name: "gmail__create_draft", Description: "Create an email draft"}, "integration"); score <= 0 {
		t.Fatal("email synonym query should match gmail draft tool")
	}
	if score := liveToolScore("open calendar meeting", Protocol.ToolDef{Name: "schedule_reminder", Description: "Schedule a reminder"}, "scheduling"); score <= 0 {
		t.Fatal("meeting/calendar synonym query should match scheduling tools")
	}
}

package Chat

// The two rules here are corrections to behaviour that showed up in real
// transcripts, not style preferences: replies built as tables for things that
// are not grids, and an assistant that answered a request to buy something with
// a table of what it could do instead of doing it.

import (
	"strings"
	"testing"

	"github.com/anyscapelabs/argus/Storage"
)

func TestPromptKeepsTablesForRealDataOnly(t *testing.T) {
	prompt := systemPromptFor(Storage.AgentProfile{AgentName: "Argus"}, true)
	for _, must := range []string{
		"table only for a real multi-row data grid",
		"search_tools",
		"invoke_tool",
	} {
		if !strings.Contains(prompt, must) {
			t.Errorf("the output rules never say %q", must)
		}
	}
	// The old line was an invitation: "tables for comparisons" is how every
	// answer-to-a-question became a table in a chat bubble.
	if strings.Contains(prompt, "tables for comparisons") {
		t.Error("the prompt still offers tables for comparisons")
	}
}

func TestPromptMakesTheAgentTakeTheStepItself(t *testing.T) {
	prompt := systemPromptFor(Storage.AgentProfile{AgentName: "Argus"}, true)
	for _, must := range []string{
		"Act first, then report",
		"try a sensible alternative",
		"Do not hand the user steps",
	} {
		if !strings.Contains(prompt, must) {
			t.Errorf("the working-style rules never say %q", must)
		}
	}
	if strings.Contains(prompt, "### browser") || strings.Contains(prompt, "### execute_command") {
		t.Error("the compact prompt still dumps individual tool sections")
	}
}

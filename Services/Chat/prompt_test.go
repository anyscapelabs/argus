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
		"Tables are for grids of real data only",
		"Never a table for options, next steps",
		"At most one table per reply",
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
		"Do, then report",
		"change method and try again",
		"Never tell the user to go do something you have a tool for",
	} {
		if !strings.Contains(prompt, must) {
			t.Errorf("the working-style rules never say %q", must)
		}
	}
	// A page that rendered 32 elements and was read from 50 looks empty, and an
	// agent that believes that stops looking.
	if !strings.Contains(prompt, "an empty window means you read past the end") {
		t.Error("the browser block never explains what an empty read window means")
	}
}

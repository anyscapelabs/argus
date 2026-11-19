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
		"read_skill",
		"scope=\"avoid\"",
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

// A reply to "Good day" used to be "How can I help you today?", which reads as
// a support bot rather than a colleague. The persona and the explicit anti-
// service-desk rules are behaviour corrections, not style preferences.
func TestPromptMakesTheAgentTalkLikeATeammate(t *testing.T) {
	prompt := systemPromptFor(Storage.AgentProfile{AgentName: "Argus"}, true)
	for _, must := range []string{
		"a teammate working alongside the user",
		"Treat a greeting as a greeting",
		"Never open a reply with",
		"How can I help?",
		"Never call yourself",
		"Stop when the work is done",
	} {
		if !strings.Contains(prompt, must) {
			t.Errorf("the teammate voice rules never say %q", must)
		}
	}
	if strings.Contains(prompt, "a personal AI assistant inside the Argus desktop app") {
		t.Error("the prompt still frames the agent as an app assistant")
	}
}

// A reply that opens with a table or a heading is legitimate, and the display
// no longer strips it, so the prompt must not forbid it either.
func TestPromptLetsAReplyOpenWithATableOrHeading(t *testing.T) {
	prompt := systemPromptFor(Storage.AgentProfile{AgentName: "Argus"}, true)
	if strings.Contains(prompt, "not a heading") {
		t.Error("the prompt still forbids opening a reply with a heading")
	}
	if !strings.Contains(prompt, "may open with a short heading, a table or a list") {
		t.Error("the output rules never allow a reply to open with a heading, table or list")
	}
	if !strings.Contains(prompt, "Do not label a reply with your own name") {
		t.Error("the output rules lost the rule against labelling a reply with the agent's name")
	}
}

package Chat

import (
	"strings"
	"testing"

	"github.com/anyscapelabs/argus/Protocol"
)

func opaqueError(msg string) *Protocol.Error {
	return &Protocol.Error{Code: Protocol.CodeBadInput, Message: msg}
}

func TestMessageIsOpaqueProviderError(t *testing.T) {
	for _, m := range []string{"Provider returned error", "", "provider returned error", "ERROR", "internal error"} {
		if !messageIsOpaqueProviderError(opaqueError(m)) {
			t.Errorf("%q should be treated as opaque", m)
		}
	}
	for _, m := range []string{"Rate limit exceeded", "Invalid JSON payload received", "The provider rejected this API key"} {
		if messageIsOpaqueProviderError(opaqueError(m)) {
			t.Errorf("%q names a problem and should not be treated as opaque", m)
		}
	}
	if messageIsOpaqueProviderError(nil) {
		t.Error("nil error should not be opaque")
	}
}

func TestFriendlyErrorReplacesOpaqueMessage(t *testing.T) {
	e := opaqueError("Provider returned error")
	friendlyError(e)
	if !strings.Contains(e.Message, "something went wrong") || strings.Contains(e.Message, "Provider returned") {
		t.Errorf("friendly error should replace the opaque one, got %q", e.Message)
	}
}

func TestFriendlyErrorLeavesSpecificErrorsAlone(t *testing.T) {
	e := opaqueError("Rate limit exceeded: free-models-per-day")
	friendlyError(e)
	if e.Message != "Rate limit exceeded: free-models-per-day" {
		t.Errorf("a specific error must survive untouched, got %q", e.Message)
	}
}

func msg(role Protocol.Role, blocks ...Protocol.Block) Protocol.Message {
	return Protocol.Message{Role: role, Blocks: blocks}
}

func text(s string) Protocol.Block { return Protocol.Block{Type: Protocol.BlockText, Text: s} }
func toolUse(name string) Protocol.Block {
	return Protocol.Block{Type: Protocol.BlockToolUse, ToolCall: &Protocol.ToolCall{Name: name}}
}
func toolResult() Protocol.Block { return Protocol.Block{Type: Protocol.BlockToolResult, Text: "{}"} }

// Gemini rejects a content whose functionCall is not immediately followed by
// its functionResponse ("Please ensure that function call turn comes
// immediately after a user turn or after a function response turn"). The
// window trimmed mid-round can open or close on either half of a pair; the
// repair must drop the orphaned half, keeping the pair whole or not at all.

func TestRepairToolPairsKeepsWholePairs(t *testing.T) {
	out := []Protocol.Message{
		msg(Protocol.RoleUser, text("hi")),
		msg(Protocol.RoleAssistant, toolUse("browser")),
		msg(Protocol.RoleUser, toolResult()),
		msg(Protocol.RoleAssistant, text("done")),
	}
	got := repairToolPairs(out)
	if len(got) != 4 {
		t.Fatalf("a clean transcript should survive untouched, got %d messages", len(got))
	}
}

func TestRepairToolPairsDropsTrailingCall(t *testing.T) {
	// A window that ends on a tool_use: no response ever follows, so the
	// call itself must go, or Gemini rejects the request.
	out := []Protocol.Message{
		msg(Protocol.RoleUser, text("hi")),
		msg(Protocol.RoleAssistant, toolUse("browser")),
	}
	got := repairToolPairs(out)
	if len(got) != 1 || got[0].Blocks[0].Text != "hi" {
		t.Fatalf("trailing orphaned call should be dropped, got %+v", got)
	}
}

func TestRepairToolPairsDropsLeadingResponse(t *testing.T) {
	// A window that opens on a tool_result carrier (its call was trimmed):
	// the response is orphaned and must go.
	out := []Protocol.Message{
		msg(Protocol.RoleUser, toolResult()),
		msg(Protocol.RoleAssistant, text("next")),
	}
	got := repairToolPairs(out)
	if len(got) != 1 || got[0].Blocks[0].Text != "next" {
		t.Fatalf("leading orphaned response should be dropped, got %+v", got)
	}
}

func TestRepairToolPairsKeepsTextAlongsideOrphan(t *testing.T) {
	// A message that carries text plus an orphaned tool half keeps its text.
	out := []Protocol.Message{
		msg(Protocol.RoleUser, text("hello")),
		msg(Protocol.RoleAssistant, text("thinking"), toolUse("browser")),
		msg(Protocol.RoleUser, text("next")),
	}
	got := repairToolPairs(out)
	if len(got) != 3 {
		t.Fatalf("messages with text should survive, got %d", len(got))
	}
	for _, b := range got[1].Blocks {
		if b.Type == Protocol.BlockToolUse {
			t.Fatalf("the orphaned call should be stripped: %+v", got[1].Blocks)
		}
	}
}

func TestRepairToolPairsSplitsCallAndResponseSharingOneContent(t *testing.T) {
	// appendMerged can glue a tool_use and its tool_result into one content,
	// and Gemini rejects a functionCall sharing a turn with its
	// functionResponse. The repair splits them into separate contents.
	out := []Protocol.Message{
		msg(Protocol.RoleAssistant, text("calling"), toolUse("browser")),
		msg(Protocol.RoleUser, toolResult()),
	}
	got := repairToolPairs(out)
	if len(got) != 2 {
		t.Fatalf("a clean pair should survive as two contents, got %d", len(got))
	}
	if got[0].Role != Protocol.RoleAssistant || !carriesToolUse(got[0]) {
		t.Fatalf("the first content should be the assistant call, got %+v", got[0])
	}
	if got[1].Role != Protocol.RoleUser || !carriesToolResult(got[1]) {
		t.Fatalf("the second content should be the user response, got %+v", got[1])
	}
}

func TestRepairToolPairsSplitsMergedToolUseAndResult(t *testing.T) {
	// A single content carrying both halves (text, tool_use, tool_result)
	// is split at the tool_result boundary.
	out := []Protocol.Message{
		msg(Protocol.RoleAssistant, text("calling"), toolUse("browser"), toolResult()),
	}
	got := repairToolPairs(out)
	if len(got) != 2 {
		t.Fatalf("a merged call+response should split into two contents, got %d", len(got))
	}
	if !carriesToolUse(got[0]) {
		t.Fatalf("the first content should be the assistant call, got %+v", got[0])
	}
	if !carriesToolResult(got[1]) {
		t.Fatalf("the second content should be the user response, got %+v", got[1])
	}
	if got[1].Role != Protocol.RoleUser {
		t.Fatalf("the response content should be a user turn, got %+v", got[1])
	}
}

func TestRepairToolPairsSplitsTrailingTextOutOfResponse(t *testing.T) {
	// A user's next sentence glued after a tool_result must be its own turn,
	// not part of the response content.
	out := []Protocol.Message{
		msg(Protocol.RoleAssistant, toolUse("browser")),
		msg(Protocol.RoleUser, toolResult(), text("thanks")),
	}
	got := repairToolPairs(out)
	if len(got) != 3 {
		t.Fatalf("trailing text should split into its own turn, got %d contents", len(got))
	}
	if !carriesToolResult(got[1]) {
		t.Fatalf("the response content should be second, got %+v", got[1])
	}
	if got[2].Blocks[0].Text != "thanks" {
		t.Fatalf("the third content should be the trailing text, got %+v", got[2])
	}
}

func TestRepairToolPairsNewestOrphanedCarrier(t *testing.T) {
	// Two consecutive user turns cannot survive appendMerged, so this is a
	// synthetic window: a tool_result carrier whose call was trimmed away.
	// repairToolPairs drops the orphaned half; the text turn remains.
	out := []Protocol.Message{
		msg(Protocol.RoleUser, text("one")),
		msg(Protocol.RoleUser, toolResult()),
	}
	got := repairToolPairs(out)
	if len(got) != 1 || got[0].Blocks[0].Text != "one" {
		t.Fatalf("an orphaned carrier should be dropped, got %+v", got)
	}
}

// Dropping an orphaned tool_result carrier leaves two assistant turns
// touching, so the next function call followed a model turn and Gemini
// answered "function call turn comes immediately after a user turn or after a
// function response turn" on every later send, even for a plain "hello".
func TestRepairToolPairsMergesModelTurnsAroundDroppedCarrier(t *testing.T) {
	out := []Protocol.Message{
		msg(Protocol.RoleUser, text("hi")),
		msg(Protocol.RoleAssistant, text("looking")),
		msg(Protocol.RoleUser, toolResult()), // orphan: no call before it
		msg(Protocol.RoleAssistant, toolUse("browser")),
		msg(Protocol.RoleUser, toolResult()),
	}
	got := repairToolPairs(out)
	for i, m := range got {
		if i > 0 && m.Role == Protocol.RoleAssistant && got[i-1].Role == Protocol.RoleAssistant {
			t.Fatalf("adjacent assistant turns survived: %+v", got)
		}
		if carriesToolUse(m) && (i == 0 || got[i-1].Role != Protocol.RoleUser) {
			t.Fatalf("call at %d does not follow a user turn: %+v", i, got)
		}
	}
}

func TestRepairHistoryTurnsDropsLeadingModelTurns(t *testing.T) {
	// A trimmed window can open on an assistant call; Gemini needs a user
	// turn before any function call.
	out := []Protocol.Message{
		msg(Protocol.RoleAssistant, toolUse("browser")),
		msg(Protocol.RoleUser, toolResult()),
		msg(Protocol.RoleAssistant, text("done")),
		msg(Protocol.RoleUser, text("hello")),
	}
	got := repairHistoryTurns(out)
	if len(got) != 1 || got[0].Role != Protocol.RoleUser || got[0].Blocks[0].Text != "hello" {
		t.Fatalf("window should restart at the first user turn, got %+v", got)
	}
}

func TestRepairToolPairsPutsResultsBeforeText(t *testing.T) {
	out := []Protocol.Message{
		msg(Protocol.RoleUser, text("hi")),
		msg(Protocol.RoleAssistant, toolUse("browser")),
		msg(Protocol.RoleUser, text("next"), toolResult()),
	}
	got := repairToolPairs(out)
	if len(got) < 3 || !carriesToolResult(got[2]) {
		t.Fatalf("the response must directly follow its call, got %+v", got)
	}
}

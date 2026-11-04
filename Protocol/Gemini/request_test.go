package Gemini

import (
	"testing"

	"github.com/anyscapelabs/argus/Protocol"
)

func textBlock(s string) Protocol.Block { return Protocol.Block{Type: Protocol.BlockText, Text: s} }

func callBlock() Protocol.Block {
	return Protocol.Block{Type: Protocol.BlockToolUse, ToolCall: &Protocol.ToolCall{ID: "1", Name: "browser", Input: "{}", Valid: true}}
}

// Two assistant messages in a row must reach Gemini as a single model
// content, otherwise the function call lands right after a model turn.
func TestAdjacentModelTurnsAreMerged(t *testing.T) {
	req := &Protocol.Request{Messages: []Protocol.Message{
		{Role: Protocol.RoleUser, Blocks: []Protocol.Block{textBlock("hello")}},
		{Role: Protocol.RoleAssistant, Blocks: []Protocol.Block{textBlock("checking")}},
		{Role: Protocol.RoleAssistant, Blocks: []Protocol.Block{callBlock()}},
		{Role: Protocol.RoleUser, Blocks: []Protocol.Block{{Type: Protocol.BlockToolResult, ToolName: "browser", Text: "{}"}}},
	}}
	got := translateContents(req)
	if len(got) != 3 {
		t.Fatalf("want 3 contents (user, model, user), got %d: %+v", len(got), got)
	}
	if got[1].Role != "model" || len(got[1].Parts) != 2 || got[1].Parts[1].FunctionCall == nil {
		t.Fatalf("model content should carry text then the call, got %+v", got[1])
	}
	if got[0].Role != "user" || got[2].Role != "user" || got[2].Parts[0].FunctionResponse == nil {
		t.Fatalf("call must sit between a user turn and its function response: %+v", got)
	}
}

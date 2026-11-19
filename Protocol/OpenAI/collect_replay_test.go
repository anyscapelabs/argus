package OpenAI

import (
	"testing"

	"github.com/anyscapelabs/argus/Protocol"
)

// A tool call the run just collected must replay as the assistant turn that
// made it. A collected message with no role matches no wire branch, so it is
// silently dropped and the tool result is left unpaired — the provider then
// rejects the round with "No tool call found for function call output".
func TestCollectedToolCallReplaysAsAssistantTurn(t *testing.T) {
	res := Protocol.Collect(func(yield func(Protocol.Event) bool) {
		yield(Protocol.MessageStartEvent(nil))
		yield(Protocol.BlockStartEvent(0, Protocol.Block{
			Type:     Protocol.BlockToolUse,
			ToolCall: &Protocol.ToolCall{ID: "call_1", Name: "search_tools"},
		}))
		yield(Protocol.DeltaEvent(0, Protocol.Delta{Kind: Protocol.DeltaToolJSON, Text: `{"query":"x"}`}))
		yield(Protocol.BlockStopEvent(0, Protocol.Block{}))
		yield(Protocol.MessageStopEvent())
	})
	if res.Error != nil {
		t.Fatalf("collect failed: %v", res.Error)
	}
	if res.Message.Role != Protocol.RoleAssistant {
		t.Fatalf("collected message role = %q, want assistant", res.Message.Role)
	}

	a, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	body := a.buildBody(&Protocol.Request{Messages: []Protocol.Message{
		res.Message,
		{Role: Protocol.RoleUser, Blocks: []Protocol.Block{{
			Type: Protocol.BlockToolResult, ToolRefID: "call_1", Text: "{}",
		}}},
	}})
	if len(body.Messages) != 2 {
		t.Fatalf("expected an assistant turn and a tool result, got %+v", body.Messages)
	}
	if call := body.Messages[0]; call.Role != "assistant" || len(call.ToolCalls) != 1 || call.ToolCalls[0].ID != "call_1" {
		t.Fatalf("tool call turn was not replayed: %+v", call)
	}
	if result := body.Messages[1]; result.Role != "tool" || result.ToolCallID != "call_1" {
		t.Fatalf("tool result was not paired: %+v", result)
	}
}

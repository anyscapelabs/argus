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

func callNamed(id, name string) Protocol.Block {
	return Protocol.Block{Type: Protocol.BlockToolUse, ToolCall: &Protocol.ToolCall{ID: id, Name: name, Input: "{}", Valid: true}}
}

func resultFor(id, name, text string) Protocol.Block {
	return Protocol.Block{Type: Protocol.BlockToolResult, ToolRefID: id, ToolName: name, Text: text}
}

func user(blocks ...Protocol.Block) Protocol.Message {
	return Protocol.Message{Role: Protocol.RoleUser, Blocks: blocks}
}

func model(blocks ...Protocol.Block) Protocol.Message {
	return Protocol.Message{Role: Protocol.RoleAssistant, Blocks: blocks}
}

func responses(c wireContent) []*wireFunctionResponse {
	var out []*wireFunctionResponse
	for _, p := range c.Parts {
		if p.FunctionResponse != nil {
			out = append(out, p.FunctionResponse)
		}
	}
	return out
}

func calls(c wireContent) []*wireFunctionCall {
	var out []*wireFunctionCall
	for _, p := range c.Parts {
		if p.FunctionCall != nil {
			out = append(out, p.FunctionCall)
		}
	}
	return out
}

// Every functionResponse the wire carries must have a name, and it must be
// the name of the call it answers, even when the stored result has none.
func TestResponseNameComesFromTheCall(t *testing.T) {
	req := &Protocol.Request{Messages: []Protocol.Message{
		user(textBlock("go")),
		model(callNamed("a", "browser")),
		user(resultFor("a", "", "{}")),
		model(textBlock("done")),
		user(textBlock("again")),
	}}
	got := translateContents(req)
	r := responses(got[2])
	if len(r) != 1 || r[0].Name != "browser" {
		t.Fatalf("response must be named after its call, got %+v", r)
	}
}

// Ids are synthesised per stream and repeat, and old rows have none: the
// result must still reach the right call.
func TestResponsesMatchWithoutUsableIDs(t *testing.T) {
	req := &Protocol.Request{Messages: []Protocol.Message{
		user(textBlock("go")),
		model(callNamed("", "first"), callNamed("", "second")),
		user(
			Protocol.Block{Type: Protocol.BlockToolResult, ToolName: "second", Text: "B"},
			Protocol.Block{Type: Protocol.BlockToolResult, ToolName: "first", Text: "A"},
		),
	}}
	r := responses(translateContents(req)[2])
	if len(r) != 2 || r[0].Name != "first" || r[1].Name != "second" {
		t.Fatalf("responses must follow call order, got %+v", r)
	}
	if string(r[0].Response) != `{"result":"A"}` || string(r[1].Response) != `{"result":"B"}` {
		t.Fatalf("results crossed: %s %s", r[0].Response, r[1].Response)
	}
}

// Gemini wants as many responses as calls. A missing result is answered with
// an error, an extra one is dropped.
func TestResponseCountMatchesCallCount(t *testing.T) {
	missing := &Protocol.Request{Messages: []Protocol.Message{
		user(textBlock("go")),
		model(callNamed("a", "one"), callNamed("b", "two")),
		user(resultFor("a", "one", "ok")),
	}}
	r := responses(translateContents(missing)[2])
	if len(r) != 2 || r[1].Name != "two" || string(r[1].Response) == `{"result":""}` {
		t.Fatalf("a call with no result needs an error response, got %+v", r)
	}
	if r[1].Response[2] != 'e' {
		t.Fatalf("missing result should use the error key, got %s", r[1].Response)
	}

	extra := &Protocol.Request{Messages: []Protocol.Message{
		user(textBlock("go")),
		model(callNamed("a", "one")),
		user(resultFor("a", "one", "ok"), resultFor("zzz", "ghost", "stray")),
	}}
	if r := responses(translateContents(extra)[2]); len(r) != 1 {
		t.Fatalf("a response with no call must be dropped, got %+v", r)
	}
}

// A call that cannot be named is dropped with its response rather than sent.
func TestNamelessCallAndItsResponseAreDropped(t *testing.T) {
	req := &Protocol.Request{Messages: []Protocol.Message{
		user(textBlock("go")),
		model(callNamed("a", ""), callNamed("b", "real")),
		user(resultFor("a", "", "junk"), resultFor("b", "real", "ok")),
	}}
	got := translateContents(req)
	if c := calls(got[1]); len(c) != 1 || c[0].Name != "real" {
		t.Fatalf("only the named call should survive, got %+v", c)
	}
	if r := responses(got[2]); len(r) != 1 || r[0].Name != "real" || string(r[0].Response) != `{"result":"ok"}` {
		t.Fatalf("response for the nameless call must go too, got %+v", r)
	}
}

// Calls nobody answered and results nobody asked for never reach the wire.
func TestOrphanedHalvesAreRemoved(t *testing.T) {
	req := &Protocol.Request{Messages: []Protocol.Message{
		user(resultFor("x", "old", "stale")),
		user(textBlock("go")),
		model(textBlock("calling"), callNamed("a", "tool")),
		user(textBlock("never mind")),
	}}
	for _, c := range translateContents(req) {
		if len(calls(c)) > 0 || len(responses(c)) > 0 {
			t.Fatalf("orphaned call or response leaked to the wire: %+v", c)
		}
	}
}

func TestErrorResultsUseErrorKey(t *testing.T) {
	b := resultFor("a", "tool", "boom")
	b.IsError = true
	req := &Protocol.Request{Messages: []Protocol.Message{
		user(textBlock("go")), model(callNamed("a", "tool")), user(b),
	}}
	r := responses(translateContents(req)[2])
	if string(r[0].Response) != `{"error":"boom"}` {
		t.Fatalf("failed tool should report under error, got %s", r[0].Response)
	}
}

func TestPlainTextResultIsWrapped(t *testing.T) {
	if got := string(functionResponsePayload("hello", false)); got != `{"result":"hello"}` {
		t.Fatalf("got %s", got)
	}
	if got := string(functionResponsePayload(`{"a":1}`, false)); got != `{"result":{"a":1}}` {
		t.Fatalf("got %s", got)
	}
	if got := string(functionResponsePayload("", false)); got != `{"result":""}` {
		t.Fatalf("got %s", got)
	}
}

func TestGemini3CallsCarryASignature(t *testing.T) {
	req := &Protocol.Request{Model: "gemini-3-pro-preview", Messages: []Protocol.Message{
		user(textBlock("go")),
		model(callNamed("a", "one"), callNamed("b", "two")),
		user(resultFor("a", "one", "x"), resultFor("b", "two", "y")),
	}}
	c := translateContents(req)[1]
	if c.Parts[0].ThoughtSignature != dummyThoughtSignature || c.Parts[1].ThoughtSignature != "" {
		t.Fatalf("first call of a step needs the signature, got %+v", c.Parts)
	}
	req.Model = "gemini-2.5-flash"
	if translateContents(req)[1].Parts[0].ThoughtSignature != "" {
		t.Fatal("older models must not get a dummy signature")
	}
}

func TestSynthCallIDsAreUnique(t *testing.T) {
	if synthCallID(0) == synthCallID(0) {
		t.Fatal("synthesised ids must not repeat across streams")
	}
}

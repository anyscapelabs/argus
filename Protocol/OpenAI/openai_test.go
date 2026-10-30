package OpenAI

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/anyscapelabs/argus/Protocol"
)

func decode(t testing.TB, body string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("body not json: %v", err)
	}
	return m
}

func readBody(t testing.TB, hreq *http.Request) map[string]any {
	t.Helper()
	raw, err := io.ReadAll(hreq.Body)
	if err != nil {
		t.Fatalf("body read: %v", err)
	}
	return decode(t, string(raw))
}

func fetchEvents(t testing.TB, seq func(y func(Protocol.Event) bool)) []Protocol.Event {
	t.Helper()
	out := []Protocol.Event{}
	for ev := range seq {
		out = append(out, ev)
	}
	return out
}

func terminalCount(t testing.TB, events []Protocol.Event) {
	t.Helper()
	n := 0
	for _, ev := range events {
		if ev.Type == Protocol.EventMessageStop || ev.Type == Protocol.EventError {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("got %d terminal events", n)
	}
}

func TestBuildRequestDefaults(t *testing.T) {
	adapter, err := New(nil)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	target := &Protocol.Target{Family: Protocol.FamilyOpenAICompat, BaseURL: "https://api.example.com", ModelID: "m", APIKey: "sk"}
	req := &Protocol.Request{Model: "m", System: "be brief", Messages: []Protocol.Message{
		{Role: Protocol.RoleUser, Blocks: []Protocol.Block{{Type: Protocol.BlockText, Text: "hi"}}},
	}, MaxOutputTokens: 200}
	hreq, err := adapter.BuildRequest(t.Context(), req, target)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if hreq.URL.String() != "https://api.example.com/chat/completions" {
		t.Fatalf("url = %s", hreq.URL)
	}
	if hreq.Header.Get("Authorization") != "Bearer sk" {
		t.Fatalf("auth = %s", hreq.Header.Get("Authorization"))
	}
	body := readBody(t, hreq)
	if body["model"] != "m" || body["stream"] != true {
		t.Fatalf("body = %v", body)
	}
	if body["max_tokens"] != float64(200) {
		t.Fatalf("max_tokens = %v", body["max_tokens"])
	}
	if _, has := body["stream_options"]; has {
		t.Fatalf("stream_options should be absent without quirk")
	}
	if _, has := body["parallel_tool_calls"]; has {
		t.Fatalf("parallel_tool_calls should be absent without quirk")
	}
	msgs := body["messages"].([]any)
	if len(msgs) != 2 || msgs[0].(map[string]any)["role"] != "system" {
		t.Fatalf("messages = %v", msgs)
	}
}

func TestBuildRequestQuirks(t *testing.T) {
	adapter, err := New(json.RawMessage(`{"chat_path":"/v1/chat/completions","max_tokens_field":"max_completion_tokens","system_role":"developer","stream_usage":true}`))
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	target := &Protocol.Target{Family: Protocol.FamilyOpenAICompat, BaseURL: "https://api.example.com/", ModelID: "m", APIKey: "sk"}
	req := &Protocol.Request{Model: "m", System: "be brief", Messages: []Protocol.Message{
		{Role: Protocol.RoleUser, Blocks: []Protocol.Block{{Type: Protocol.BlockText, Text: "hi"}}},
	}, MaxOutputTokens: 300}
	hreq, err := adapter.BuildRequest(t.Context(), req, target)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if hreq.URL.String() != "https://api.example.com/v1/chat/completions" {
		t.Fatalf("url = %s", hreq.URL)
	}
	body := readBody(t, hreq)
	if body["max_completion_tokens"] != float64(300) {
		t.Fatalf("body = %v", body)
	}
	if _, has := body["max_tokens"]; has {
		t.Fatalf("max_tokens should be absent")
	}
	msgs := body["messages"].([]any)
	if msgs[0].(map[string]any)["role"] != "developer" {
		t.Fatalf("role = %v", msgs[0])
	}
	if body["stream_options"].(map[string]any)["include_usage"] != true {
		t.Fatalf("stream_options = %v", body["stream_options"])
	}
}

func TestBuildRequestToolsAndResults(t *testing.T) {
	adapter, _ := New(json.RawMessage(`{"parallel_tools":true}`))
	target := &Protocol.Target{Family: Protocol.FamilyOpenAICompat, BaseURL: "https://x.example.com", ModelID: "m", APIKey: "k"}
	req := &Protocol.Request{
		Model: "m",
		Messages: []Protocol.Message{
			{Role: Protocol.RoleUser, Blocks: []Protocol.Block{{Type: Protocol.BlockText, Text: "weather?"}}},
			{Role: Protocol.RoleAssistant, Blocks: []Protocol.Block{
				{Type: Protocol.BlockToolUse, ToolCall: &Protocol.ToolCall{ID: "call1", Name: "lookup", Input: "{\"city\":\"oslo\"}", Valid: true}},
			}},
			{Role: Protocol.RoleUser, Blocks: []Protocol.Block{
				{Type: Protocol.BlockToolResult, ToolRefID: "call1", Text: "{\"temp\":5}"},
				{Type: Protocol.BlockText, Text: "and now"},
			}},
		},
		Tools: []Protocol.ToolDef{{Name: "lookup", Description: "weather", Schema: json.RawMessage(`{"type":"object"}`)}},
	}
	hreq, err := adapter.BuildRequest(t.Context(), req, target)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	body := readBody(t, hreq)
	if body["parallel_tool_calls"] != true {
		t.Fatalf("parallel = %v", body["parallel_tool_calls"])
	}
	tools := body["tools"].([]any)
	first := tools[0].(map[string]any)
	if first["type"] != "function" {
		t.Fatalf("tools = %v", tools)
	}
	fn := first["function"].(map[string]any)
	if fn["name"] != "lookup" {
		t.Fatalf("function = %v", fn)
	}
	msgs := body["messages"].([]any)
	if len(msgs) != 4 {
		t.Fatalf("messages = %v", msgs)
	}
	assistant := msgs[1].(map[string]any)
	calls := assistant["tool_calls"].([]any)
	got := calls[0].(map[string]any)["function"].(map[string]any)
	if got["name"] != "lookup" || got["arguments"] != "{\"city\":\"oslo\"}" {
		t.Fatalf("tool call = %v", got)
	}
	toolMsg := msgs[2].(map[string]any)
	if toolMsg["role"] != "tool" || toolMsg["tool_call_id"] != "call1" {
		t.Fatalf("tool msg = %v", toolMsg)
	}
	final := msgs[3].(map[string]any)
	if final["content"] != "and now" {
		t.Fatalf("final = %v", final)
	}
}

const basicStream = "data: {\"choices\":[{\"delta\":{\"content\":\"Hel\"},\"finish_reason\":null}]}\n\n" +
	"data: {\"choices\":[{\"delta\":{\"content\":\"lo\"},\"finish_reason\":null}]}\n\n" +
	"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
	"data: [DONE]\n\n"

func TestParseStreamText(t *testing.T) {
	adapter, _ := New(nil)
	events := fetchEvents(t, adapter.ParseStream(strings.NewReader(basicStream)))
	terminalCount(t, events)
	if events[len(events)-1].Type != Protocol.EventMessageStop {
		t.Fatalf("terminal type = %v", events[len(events)-1].Type)
	}
	textIdx := -1
	for _, ev := range events {
		if ev.Type == Protocol.EventBlockStart && ev.Start.Block.Type == Protocol.BlockText {
			textIdx = ev.Index
		}
	}
	if textIdx != 0 {
		t.Fatalf("text block index = %d", textIdx)
	}
	var text strings.Builder
	for _, ev := range events {
		if ev.Type == Protocol.EventBlockDelta && ev.Delta != nil && ev.Delta.Kind == Protocol.DeltaText {
			text.WriteString(ev.Delta.Text)
		}
	}
	if text.String() != "Hello" {
		t.Fatalf("text = %q", text.String())
	}
	stopFound := false
	for _, ev := range events {
		if ev.Type == Protocol.EventMessageDelta && ev.Stop != nil {
			stopFound = ev.Stop.Reason == Protocol.StopEndTurn && ev.Stop.Raw == "stop"
		}
	}
	if !stopFound {
		t.Fatalf("stop event missing")
	}
}

const toolStream = "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"get\",\"arguments\":\"{\\\"q\\\":\"}}]},\"finish_reason\":null}]}\n\n" +
	"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"1}\"}}]},\"finish_reason\":null}]}\n\n" +
	"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n" +
	"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":9,\"completion_tokens\":4}}\n\n" +
	"data: [DONE]\n\n"

func TestParseStreamToolCallWithLateUsage(t *testing.T) {
	adapter, _ := New(nil)
	events := fetchEvents(t, adapter.ParseStream(strings.NewReader(toolStream)))
	terminalCount(t, events)
	res := Protocol.Collect(func(y func(Protocol.Event) bool) {
		for _, e := range events {
			y(e)
		}
	})
	if res.Error != nil {
		t.Fatalf("collect error: %+v", res.Error)
	}
	if res.Stop.Reason != Protocol.StopToolUse {
		t.Fatalf("stop = %+v", res.Stop)
	}
	if res.Usage.InputTokens != 9 || res.Usage.OutputTokens != 4 {
		t.Fatalf("usage = %+v", res.Usage)
	}
	if len(res.Message.Blocks) != 1 {
		t.Fatalf("blocks = %+v", res.Message.Blocks)
	}
	call := res.Message.Blocks[0].ToolCall
	if call == nil || call.ID != "call_1" || call.Name != "get" {
		t.Fatalf("call = %+v", call)
	}
	if call.Input != "{\"q\":1}" || !call.Valid {
		t.Fatalf("input = %q valid=%v", call.Input, call.Valid)
	}
}

func TestParseStreamMissingIDGetsSynthetic(t *testing.T) {
	stream := "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"name\":\"pick\"}}]},\"finish_reason\":null}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n" +
		"data: [DONE]\n\n"
	adapter, _ := New(nil)
	events := fetchEvents(t, adapter.ParseStream(strings.NewReader(stream)))
	terminalCount(t, events)
	ids := map[string]bool{}
	for _, ev := range events {
		if ev.Type == Protocol.EventBlockStop && ev.Block != nil && ev.Block.ToolCall != nil {
			ids[ev.Block.ToolCall.ID] = true
		}
	}
	if len(ids) != 1 {
		t.Fatalf("call ids = %v", ids)
	}
	for id := range ids {
		if !strings.HasPrefix(id, "synth_") {
			t.Fatalf("id = %s", id)
		}
	}
}

func TestParseStreamReasoningContent(t *testing.T) {
	stream := "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"plan\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"ans\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"
	adapter, _ := New(json.RawMessage(`{"reasoning_field":"reasoning_content"}`))
	res := Protocol.Collect(adapter.ParseStream(strings.NewReader(stream)))
	if res.Error != nil {
		t.Fatalf("collect: %+v", res.Error)
	}
	if len(res.Message.Blocks) != 2 {
		t.Fatalf("blocks = %+v", res.Message.Blocks)
	}
	th, tx := res.Message.Blocks[0], res.Message.Blocks[1]
	if th.Type != Protocol.BlockThinking || th.Text != "plan" {
		t.Fatalf("thinking = %+v", th)
	}
	if tx.Type != Protocol.BlockText || tx.Text != "ans" {
		t.Fatalf("text = %+v", tx)
	}
}

func TestParseStreamMidStreamError(t *testing.T) {
	stream := "data: {\"choices\":[{\"delta\":{\"content\":\"Hel\"}}]}\n\n" +
		"data: {\"error\":{\"message\":\"rate\",\"type\":\"rate_limit_error\"}}\n\n"
	adapter, _ := New(nil)
	events := fetchEvents(t, adapter.ParseStream(strings.NewReader(stream)))
	terminalCount(t, events)
	if events[len(events)-1].Type != Protocol.EventError {
		t.Fatalf("terminal = %v", events[len(events)-1].Type)
	}
	if events[len(events)-1].Error.Code != Protocol.CodeRateLimited {
		t.Fatalf("code = %+v", events[len(events)-1].Error)
	}
}

func TestParseStreamNoFrames(t *testing.T) {
	adapter, _ := New(nil)
	events := fetchEvents(t, adapter.ParseStream(strings.NewReader("")))
	terminalCount(t, events)
	if events[len(events)-1].Type != Protocol.EventError {
		t.Fatalf("terminal = %v", events[len(events)-1].Type)
	}
}

func TestParseStreamMalformedChunk(t *testing.T) {
	adapter, _ := New(nil)
	events := fetchEvents(t, adapter.ParseStream(strings.NewReader("data: {oops\n\n")))
	terminalCount(t, events)
	if events[len(events)-1].Type != Protocol.EventError {
		t.Fatalf("terminal = %v", events[len(events)-1].Type)
	}
}

func TestMapErrorStatuses(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   string
	}{
		{401, `{"error":{"message":"bad key","type":"invalid_request_error"}}`, Protocol.CodeInvalidKey},
		{403, `{}`, Protocol.CodeInvalidKey},
		{429, `{"error":{"message":"slow","type":"rate_limit_error"}}`, Protocol.CodeRateLimited},
		{500, `{}`, Protocol.CodeProviderError},
		{400, `{"error":{"message":"bad model","type":"invalid_request_error"}}`, Protocol.CodeBadInput},
	}
	for _, c := range cases {
		got := MapErrorInto(c.status, []byte(c.body), 0)
		if got.Code != c.want {
			t.Fatalf("status %d: got %s want %s (%+v)", c.status, got.Code, c.want, got)
		}
	}
	limited := MapErrorInto(429, []byte(`{}`), 2*time.Second)
	if !limited.Retryable || limited.RetryAfter != 2000 {
		t.Fatalf("retry = %+v", limited)
	}
}

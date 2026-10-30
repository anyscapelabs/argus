package Anthropic

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/anyscapelabs/argus/Protocol"
)

func decodeTB(t testing.TB, body string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(body), &m); err != nil {
		t.Fatalf("body not json: %v", err)
	}
	return m
}

func readWireBody(t testing.TB, hreq *http.Request) map[string]any {
	t.Helper()
	raw, err := io.ReadAll(hreq.Body)
	if err != nil {
		t.Fatalf("body read: %v", err)
	}
	return decodeTB(t, string(raw))
}

func fetch(t testing.TB, seq func(y func(Protocol.Event) bool)) []Protocol.Event {
	t.Helper()
	out := []Protocol.Event{}
	for ev := range seq {
		out = append(out, ev)
	}
	return out
}

func requireTerminal(t testing.TB, events []Protocol.Event) {
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
	adapter, _ := New(nil)
	target := &Protocol.Target{Family: Protocol.FamilyAnthropic, BaseURL: "https://api.example.com", ModelID: "claude-3", APIKey: "sk-ant"}
	req := &Protocol.Request{Model: "claude-3", System: "sys", Messages: []Protocol.Message{
		{Role: Protocol.RoleUser, Blocks: []Protocol.Block{{Type: Protocol.BlockText, Text: "hi"}}},
	}, MaxOutputTokens: 1024}
	hreq, err := adapter.BuildRequest(t.Context(), req, target)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if hreq.URL.String() != "https://api.example.com/v1/messages" {
		t.Fatalf("url = %s", hreq.URL)
	}
	if hreq.Header.Get("x-api-key") != "sk-ant" || hreq.Header.Get("anthropic-version") != apiVersion {
		t.Fatalf("headers = %v", hreq.Header)
	}
	body := readWireBody(t, hreq)
	if body["max_tokens"] != float64(1024) || body["system"] != "sys" {
		t.Fatalf("body = %v", body)
	}
	if msgs := body["messages"].([]any); len(msgs) != 1 {
		t.Fatalf("messages = %v", msgs)
	}
	if _, has := body["thinking"]; has {
		t.Fatalf("thinking should be absent")
	}
}

func TestBuildRequestThinking(t *testing.T) {
	adapter, _ := New(nil)
	target := &Protocol.Target{Family: Protocol.FamilyAnthropic, BaseURL: "https://x", ModelID: "m", APIKey: "k"}
	req := &Protocol.Request{Model: "m", Messages: []Protocol.Message{
		{Role: Protocol.RoleUser, Blocks: []Protocol.Block{{Type: Protocol.BlockText, Text: "hi"}}},
	}, Reasoning: Protocol.ReasoningMedium}
	hreq, err := adapter.BuildRequest(t.Context(), req, target)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	body := readWireBody(t, hreq)
	if body["max_tokens"] != float64(8192) {
		t.Fatalf("max_tokens = %v", body["max_tokens"])
	}
	if body["thinking"].(map[string]any)["budget_tokens"] != float64(4096) {
		t.Fatalf("thinking = %v", body["thinking"])
	}
}

func TestBuildRequestToolReplay(t *testing.T) {
	adapter, _ := New(nil)
	target := &Protocol.Target{BaseURL: "https://x", ModelID: "m", APIKey: "k"}
	req := &Protocol.Request{
		Model:  "m",
		System: "sys",
		Messages: []Protocol.Message{
			{Role: Protocol.RoleUser, Blocks: []Protocol.Block{{Type: Protocol.BlockText, Text: "q"}}},
			{Role: Protocol.RoleAssistant, Blocks: []Protocol.Block{
				{Type: Protocol.BlockThinking, Text: "hmm", Signature: "sig9"},
				{Type: Protocol.BlockText, Text: "let me"},
				{Type: Protocol.BlockToolUse, ToolCall: &Protocol.ToolCall{ID: "toolu_1", Name: "forecast", Input: "{\"day\":1}", Valid: true}},
			}},
			{Role: Protocol.RoleUser, Blocks: []Protocol.Block{
				{Type: Protocol.BlockToolResult, ToolRefID: "toolu_1", Text: "{\"rain\":true}"},
			}},
		},
		Tools: []Protocol.ToolDef{{Name: "forecast", Description: "weather", Schema: json.RawMessage(`{"type":"object"}`)}},
	}
	hreq, err := adapter.BuildRequest(t.Context(), req, target)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	body := readWireBody(t, hreq)
	msgs := body["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("messages = %d", len(msgs))
	}
	assistant := msgs[1].(map[string]any)["content"].([]any)
	if len(assistant) != 3 {
		t.Fatalf("assistant = %v", assistant)
	}
	thinking := assistant[0].(map[string]any)
	if thinking["thinking"] != "hmm" || thinking["signature"] != "sig9" {
		t.Fatalf("thinking = %v", thinking)
	}
	toolUse := assistant[2].(map[string]any)
	if toolUse["id"] != "toolu_1" || toolUse["name"] != "forecast" {
		t.Fatalf("tool_use = %v", toolUse)
	}
	userMsg := msgs[2].(map[string]any)["content"].([]any)
	result := userMsg[0].(map[string]any)
	if result["type"] != "tool_result" || result["tool_use_id"] != "toolu_1" {
		t.Fatalf("tool_result = %v", result)
	}
}

const reportStream = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"role\":\"assistant\",\"model\":\"claude\",\"usage\":{\"input_tokens\":11,\"cache_creation\":0,\"output_tokens\":0}}}\n\n" +
	"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n" +
	"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hell\"}}\n\n" +
	"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"o\"}}\n\n" +
	"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
	"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\",\"stop_sequence\":null},\"usage\":{\"output_tokens\":9}}\n\n" +
	"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

func TestParseStreamReport(t *testing.T) {
	adapter, _ := New(nil)
	events := fetch(t, adapter.ParseStream(strings.NewReader(reportStream)))
	requireTerminal(t, events)
	if events[len(events)-1].Type != Protocol.EventMessageStop {
		t.Fatalf("terminal = %v", events[len(events)-1].Type)
	}
	res := Protocol.Collect(func(y func(Protocol.Event) bool) {
		for _, e := range events {
			y(e)
		}
	})
	if res.Error != nil {
		t.Fatalf("error = %+v", res.Error)
	}
	if res.Message.Blocks[0].Text != "Hello" {
		t.Fatalf("text = %q", res.Message.Blocks[0].Text)
	}
	if res.Usage.InputTokens != 11 || res.Usage.OutputTokens != 9 {
		t.Fatalf("usage = %+v", res.Usage)
	}
	if res.Stop.Reason != Protocol.StopEndTurn {
		t.Fatalf("stop = %+v", res.Stop)
	}
}

const toolStream = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":4}}}\n\n" +
	"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"toolu_9\",\"name\":\"math\"}}\n\n" +
	"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{\\\"x\\\":\"}}\n\n" +
	"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"2}\"}}\n\n" +
	"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
	"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":6}}\n\n" +
	"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

func TestParseStreamToolUse(t *testing.T) {
	adapter, _ := New(nil)
	res := Protocol.Collect(adapter.ParseStream(strings.NewReader(toolStream)))
	if res.Error != nil {
		t.Fatalf("error = %+v", res.Error)
	}
	if len(res.Message.Blocks) != 1 || res.Message.Blocks[0].Type != Protocol.BlockToolUse {
		t.Fatalf("blocks = %+v", res.Message.Blocks)
	}
	call := res.Message.Blocks[0].ToolCall
	if call == nil || call.ID != "toolu_9" || call.Name != "math" {
		t.Fatalf("call = %+v", call)
	}
	if call.Input != "{\"x\":2}" || !call.Valid {
		t.Fatalf("input = %q valid=%v", call.Input, call.Valid)
	}
	if res.Stop.Reason != Protocol.StopToolUse {
		t.Fatalf("stop = %+v", res.Stop)
	}
	if res.Usage.InputTokens != 4 || res.Usage.OutputTokens != 6 {
		t.Fatalf("usage = %+v", res.Usage)
	}
}

const thinkingStream = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":8}}}\n\n" +
	"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"text\":\"\"}}\n\n" +
	"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"thinking_delta\",\"thinking\":\"let me\"}}\n\n" +
	"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"signature_delta\",\"signature\":\"sig\"}}\n\n" +
	"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
	"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":1,\"content_block\":{\"type\":\"text\"}}\n\n" +
	"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"text_delta\",\"text\":\"the answer\"}}\n\n" +
	"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":1}\n\n" +
	"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":20}}\n\n" +
	"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

func TestParseStreamThinkingWithSignature(t *testing.T) {
	adapter, _ := New(nil)
	res := Protocol.Collect(adapter.ParseStream(strings.NewReader(thinkingStream)))
	if res.Error != nil {
		t.Fatalf("error = %+v", res.Error)
	}
	if len(res.Message.Blocks) != 2 {
		t.Fatalf("blocks = %+v", res.Message.Blocks)
	}
	thinking := res.Message.Blocks[0]
	if thinking.Type != Protocol.BlockThinking || thinking.Text != "let me" || thinking.Signature != "sig" {
		t.Fatalf("thinking = %+v", thinking)
	}
	if res.Message.Blocks[1].Text != "the answer" {
		t.Fatalf("text = %+v", res.Message.Blocks[1])
	}
}

func TestParseStreamBadToolJSONNeverFails(t *testing.T) {
	stream := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":1}}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"t\",\"name\":\"f\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":\"{broken\"}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	adapter, _ := New(nil)
	res := Protocol.Collect(adapter.ParseStream(strings.NewReader(stream)))
	if res.Error != nil {
		t.Fatalf("error = %+v", res.Error)
	}
	call := res.Message.Blocks[0].ToolCall
	if call == nil || call.Valid || call.Input != "{broken" {
		t.Fatalf("call = %+v", call)
	}
}

func TestParseStreamPingIgnored(t *testing.T) {
	stream := "event: ping\ndata: {\"type\":\"ping\"}\n\n" + reportStream
	adapter, _ := New(nil)
	events := fetch(t, adapter.ParseStream(strings.NewReader(stream)))
	requireTerminal(t, events)
}

func TestParseStreamOverloaded(t *testing.T) {
	stream := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":1}}}\n\n" +
		"event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Overloaded\"}}\n\n"
	adapter, _ := New(nil)
	events := fetch(t, adapter.ParseStream(strings.NewReader(stream)))
	requireTerminal(t, events)
	last := events[len(events)-1]
	if last.Type != Protocol.EventError || last.Error.Code != Protocol.CodeProviderError {
		t.Fatalf("error = %+v", last.Error)
	}
}

func TestParseStreamEmpty(t *testing.T) {
	adapter, _ := New(nil)
	events := fetch(t, adapter.ParseStream(strings.NewReader("")))
	requireTerminal(t, events)
	if events[len(events)-1].Type != Protocol.EventError {
		t.Fatalf("terminal = %v", events[len(events)-1].Type)
	}
}

func TestMapErrorStatuses(t *testing.T) {
	adapter := &Adapter{}
	cases := []struct {
		status int
		body   string
		want   string
	}{
		{400, `{"type":"error","error":{"type":"invalid_request_error","message":"max_tokens is too large"}}`, Protocol.CodeBadInput},
		{401, `{"type":"error","error":{"type":"authentication_error","message":"bad key"}}`, Protocol.CodeInvalidKey},
		{403, `{}`, Protocol.CodeInvalidKey},
		{429, `{}`, Protocol.CodeRateLimited},
		{529, `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`, Protocol.CodeProviderError},
		{404, `{"type":"error","error":{"type":"not_found_error","message":"no model"}}`, Protocol.CodeBadInput},
	}
	for _, c := range cases {
		got := adapter.MapError(c.status, []byte(c.body), 0)
		if got.Code != c.want {
			t.Fatalf("status %d: got %s want %s (%+v)", c.status, got.Code, c.want, got)
		}
	}
	limited := adapter.MapError(http.StatusTooManyRequests, []byte(`{}`), quieroAfter)
	if !limited.Retryable || limited.RetryAfter != 1500 {
		t.Fatalf("limited = %+v", limited)
	}
}

var quieroAfter = 1500 * time.Millisecond

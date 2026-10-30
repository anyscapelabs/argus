package Gemini

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
	target := &Protocol.Target{Family: Protocol.FamilyGemini, BaseURL: "https://gi.example.com/v1beta", ModelID: "gemini-3", APIKey: "AIza"}
	v := 0.5
	req := &Protocol.Request{Model: "gemini-3", System: "sys", Messages: []Protocol.Message{
		{Role: Protocol.RoleUser, Blocks: []Protocol.Block{{Type: Protocol.BlockText, Text: "hi"}}},
	}, MaxOutputTokens: 256, Temperature: &v}
	hreq, err := adapter.BuildRequest(t.Context(), req, target)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if hreq.URL.Path != "/v1beta/models/gemini-3:streamGenerateContent" || hreq.URL.Query().Get("alt") != "sse" {
		t.Fatalf("url = %s", hreq.URL)
	}
	if hreq.Header.Get("x-goog-api-key") != "AIza" {
		t.Fatalf("auth = %s", hreq.Header.Get("x-goog-api-key"))
	}
	body := readWireBody(t, hreq)
	if body["systemInstruction"].(map[string]any)["parts"].([]any)[0].(map[string]any)["text"] != "sys" {
		t.Fatalf("system = %v", body["systemInstruction"])
	}
	gen := body["generationConfig"].(map[string]any)
	if gen["maxOutputTokens"] != float64(256) {
		t.Fatalf("gen = %v", gen)
	}
	cfg := body["toolConfig"].(map[string]any)["functionCallingConfig"].(map[string]any)
	if cfg["mode"] != "AUTO" {
		t.Fatalf("tool config = %v", cfg)
	}
}

func TestBuildRequestReplayWithSignatures(t *testing.T) {
	adapter, _ := New(nil)
	target := &Protocol.Target{BaseURL: "https://gi.example.com/v1beta", ModelID: "gemini-3", APIKey: "k"}
	req := &Protocol.Request{
		Model: "gemini-3",
		Messages: []Protocol.Message{
			{Role: Protocol.RoleUser, Blocks: []Protocol.Block{{Type: Protocol.BlockText, Text: "q"}}},
			{Role: Protocol.RoleAssistant, Blocks: []Protocol.Block{
				{Type: Protocol.BlockThinking, Text: "hm", Signature: "tsig", Extra: json.RawMessage(`{"thoughtSignature":"tsig"}`)},
				{Type: Protocol.BlockToolUse, ToolCall: &Protocol.ToolCall{ID: "fc1", Name: "find", Input: "{\"q\":\"x\"}", Valid: true}, Extra: json.RawMessage(`{"thoughtSignature":"csig"}`)},
			}},
			{Role: Protocol.RoleUser, Blocks: []Protocol.Block{
				{Type: Protocol.BlockToolResult, ToolRefID: "fc1", ToolName: "find", Text: "{\"r\":true}"},
			}},
		},
		Tools: []Protocol.ToolDef{{Name: "find", Description: "search", Schema: json.RawMessage(`{"type":"object"}`)}},
	}
	hreq, err := adapter.BuildRequest(t.Context(), req, target)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	body := readWireBody(t, hreq)
	contents := body["contents"].([]any)
	if len(contents) != 3 {
		t.Fatalf("contents = %d", len(contents))
	}
	model := contents[1].(map[string]any)
	if model["role"] != "model" {
		t.Fatalf("role = %v", model["role"])
	}
	parts := model["parts"].([]any)
	thought := parts[0].(map[string]any)
	if thought["thought"] != true || thought["thoughtSignature"] != "tsig" {
		t.Fatalf("thought = %v", thought)
	}
	call := parts[1].(map[string]any)
	fc := call["functionCall"].(map[string]any)
	if fc["name"] != "find" || call["thoughtSignature"] != "csig" {
		t.Fatalf("call = %v", call)
	}
	response := contents[2].(map[string]any)["parts"].([]any)[0].(map[string]any)
	fr := response["functionResponse"].(map[string]any)
	if fr["name"] != "find" || fr["id"] != "fc1" {
		t.Fatalf("fr = %v", fr)
	}
	res := fr["response"].(map[string]any)["result"].(map[string]any)
	if res["r"] != true {
		t.Fatalf("result = %v", res)
	}
}

const geminiTextStream = "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"Hel\"}]},\"index\":0},{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"lo\"}]},\"index\":0}],\"usageMetadata\":{\"promptTokenCount\":6,\"candidatesTokenCount\":2}}\n\n" +
	"data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[]},\"index\":0,\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":6,\"candidatesTokenCount\":2,\"totalTokenCount\":8}}\n\n"

func TestParseStreamText(t *testing.T) {
	adapter, _ := New(nil)
	events := fetch(t, adapter.ParseStream(strings.NewReader(geminiTextStream)))
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
	if res.Usage.InputTokens != 6 || res.Usage.OutputTokens != 2 {
		t.Fatalf("usage = %+v", res.Usage)
	}
	if res.Stop.Reason != Protocol.StopEndTurn || res.Stop.Raw != "STOP" {
		t.Fatalf("stop = %+v", res.Stop)
	}
}

const geminiToolStream = "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"checking\"}]},\"index\":0}],\"usageMetadata\":{\"promptTokenCount\":5}}\n\n" +
	"data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"functionCall\":{\"id\":\"fc9\",\"name\":\"find\",\"args\":{\"q\":\"x\"}}}]},\"index\":0,\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":5,\"candidatesTokenCount\":3}}\n\n"

func TestParseStreamToolCall(t *testing.T) {
	adapter, _ := New(nil)
	res := Protocol.Collect(adapter.ParseStream(strings.NewReader(geminiToolStream)))
	if res.Error != nil {
		t.Fatalf("error = %+v", res.Error)
	}
	if len(res.Message.Blocks) != 2 {
		t.Fatalf("blocks = %+v", res.Message.Blocks)
	}
	if res.Message.Blocks[0].Type != Protocol.BlockText || res.Message.Blocks[0].Text != "checking" {
		t.Fatalf("b0 = %+v", res.Message.Blocks[0])
	}
	tool := res.Message.Blocks[1]
	if tool.Type != Protocol.BlockToolUse {
		t.Fatalf("b1 = %+v", tool)
	}
	call := tool.ToolCall
	if call == nil || call.ID != "fc9" || call.Name != "find" {
		t.Fatalf("call = %+v", call)
	}
	if call.Input != "{\"q\":\"x\"}" || !call.Valid {
		t.Fatalf("input = %q valid=%v", call.Input, call.Valid)
	}
	if res.Stop.Reason != Protocol.StopToolUse {
		t.Fatalf("stop = %+v", res.Stop)
	}
}

func TestParseStreamToolCallWithoutIDSynth(t *testing.T) {
	stream := "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"functionCall\":{\"name\":\"ping\"}}]},\"index\":0,\"finishReason\":\"STOP\"}]}\n\n"
	adapter, _ := New(nil)
	res := Protocol.Collect(adapter.ParseStream(strings.NewReader(stream)))
	if res.Error != nil {
		t.Fatalf("error = %+v", res.Error)
	}
	call := res.Message.Blocks[0].ToolCall
	if call == nil || !strings.HasPrefix(call.ID, "synth_") || call.Name != "ping" {
		t.Fatalf("call = %+v", call)
	}
}

func TestParseStreamStopWithoutToolIsEndTurn(t *testing.T) {
	adapter, _ := New(nil)
	res := Protocol.Collect(adapter.ParseStream(strings.NewReader(geminiTextStream)))
	if res.Error != nil {
		t.Fatalf("error = %+v", res.Error)
	}
	if res.Stop.Reason != Protocol.StopEndTurn {
		t.Fatalf("stop = %+v", res.Stop)
	}
}

func TestParseStreamMaxTokens(t *testing.T) {
	adapter, _ := New(nil)
	res := Protocol.Collect(adapter.ParseStream(strings.NewReader(
		"data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"a\"}]},\"index\":0,\"finishReason\":\"MAX_TOKENS\"}]}")))
	if res.Error != nil {
		t.Fatalf("error = %+v", res.Error)
	}
	if res.Stop.Reason != Protocol.StopMaxTokens {
		t.Fatalf("stop = %+v", res.Stop)
	}
}

func TestParseStreamNoFinishIsToolUse(t *testing.T) {
	adapter, _ := New(nil)
	res := Protocol.Collect(adapter.ParseStream(strings.NewReader(
		"data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"functionCall\":{\"name\":\"f\"}}]}}]}\n\n")))
	if res.Error != nil {
		t.Fatalf("error = %+v", res.Error)
	}
	if res.Stop.Reason != Protocol.StopToolUse {
		t.Fatalf("stop = %+v", res.Stop)
	}
}

func TestParseStreamSafetyBlock(t *testing.T) {
	adapter, _ := New(nil)
	res := Protocol.Collect(adapter.ParseStream(strings.NewReader(
		"data: {\"promptFeedback\":{\"blockReason\":\"SAFETY\"},\"usageMetadata\":{\"promptTokenCount\":3}}\n\n")))
	if res.Error != nil {
		t.Fatalf("error = %+v", res.Error)
	}
	if res.Stop.Reason != Protocol.StopContentFilter {
		t.Fatalf("stop = %+v", res.Stop)
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
	got := adapter.MapError(http.StatusTooManyRequests, []byte(`{"error":{"code":429,"message":"quota","status":"RESOURCE_EXHAUSTED"}}`), 3*time.Second)
	if got.Code != Protocol.CodeRateLimited || got.RetryAfter != 3000 {
		t.Fatalf("got = %+v", got)
	}
	got = adapter.MapError(http.StatusUnauthorized, []byte(`{"error":{"code":401,"message":"key","status":"UNAUTHENTICATED"}}`), 0)
	if got.Code != Protocol.CodeInvalidKey {
		t.Fatalf("got = %+v", got)
	}
	got = adapter.MapError(503, []byte(`{}`), 0)
	if got.Code != Protocol.CodeProviderError || !got.Retryable {
		t.Fatalf("got = %+v", got)
	}
}

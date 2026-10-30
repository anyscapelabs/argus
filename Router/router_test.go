package Router

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Provider"
)

func collect(t testing.TB, seq func(y func(Protocol.Event) bool)) Protocol.Result {
	t.Helper()
	out := []Protocol.Event{}
	for ev := range seq {
		out = append(out, ev)
	}
	n := 0
	for _, ev := range out {
		if ev.Type == Protocol.EventMessageStop || ev.Type == Protocol.EventError {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("got %d terminal events", n)
	}
	return Protocol.Collect(func(y func(Protocol.Event) bool) {
		for _, e := range out {
			y(e)
		}
	})
}

func TestRouterOpenAICompat(t *testing.T) {
	var seenAuth string
	var seenPath string
	var seenModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		seenPath = r.URL.Path
		var body struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
			MaxTok int64  `json:"max_tokens"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		seenModel = body.Model
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"bonjour\"},\"finish_reason\":null}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	provider := Provider.Provider{ID: "test", Name: "Test", Adapter: Provider.AdapterOpenAICompat, BaseURL: srv.URL}
	target, err := TargetFor(provider, "test-model-9", "sk-test", TargetOptions{})
	if err != nil {
		t.Fatalf("target: %v", err)
	}
	req := &Protocol.Request{Model: "client-name", System: "sys", Messages: []Protocol.Message{
		{Role: Protocol.RoleUser, Blocks: []Protocol.Block{{Type: Protocol.BlockText, Text: "hi"}}},
	}, MaxOutputTokens: 42}
	res := collect(t, New().Run(context.Background(), req, &target))
	if res.Error != nil {
		t.Fatalf("error = %+v", res.Error)
	}
	if seenAuth != "Bearer sk-test" {
		t.Fatalf("auth = %q", seenAuth)
	}
	if seenPath != "/chat/completions" {
		t.Fatalf("path = %q", seenPath)
	}
	if seenModel != "test-model-9" {
		t.Fatalf("model = %q", seenModel)
	}
	if res.Message.Blocks[0].Text != "bonjour" {
		t.Fatalf("blocks = %+v", res.Message.Blocks)
	}
	if res.Stop.Reason != Protocol.StopEndTurn {
		t.Fatalf("stop = %+v", res.Stop)
	}
}

func TestRouterAnthropic(t *testing.T) {
	var seenKey, seenVersion, seenPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenKey = r.Header.Get("x-api-key")
		seenVersion = r.Header.Get("anthropic-version")
		seenPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":3}}}\n\n")
		_, _ = io.WriteString(w, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\"}}\n\n")
		_, _ = io.WriteString(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n")
		_, _ = io.WriteString(w, "event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n")
		_, _ = io.WriteString(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":2}}\n\n")
		_, _ = io.WriteString(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}))
	defer srv.Close()

	provider := Provider.Provider{ID: "test", Adapter: Provider.AdapterAnthropic, BaseURL: srv.URL}
	target, err := TargetFor(provider, "claude-x", "sk-ant", TargetOptions{})
	if err != nil {
		t.Fatalf("target: %v", err)
	}
	req := &Protocol.Request{Model: "claude-x", Messages: []Protocol.Message{
		{Role: Protocol.RoleUser, Blocks: []Protocol.Block{{Type: Protocol.BlockText, Text: "q"}}},
	}}
	res := collect(t, New().Run(context.Background(), req, &target))
	if res.Error != nil {
		t.Fatalf("error = %+v", res.Error)
	}
	if seenKey != "sk-ant" || seenVersion != "2023-06-01" || seenPath != "/v1/messages" {
		t.Fatalf("key=%q version=%q path=%q", seenKey, seenVersion, seenPath)
	}
	if res.Message.Blocks[0].Text != "ok" {
		t.Fatalf("blocks = %+v", res.Message.Blocks)
	}
}

func TestRouterGemini(t *testing.T) {
	var seenKey, seenPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenKey = r.Header.Get("x-goog-api-key")
		seenPath = r.URL.Path
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"ciao\"}]},\"index\":0,\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":2,\"candidatesTokenCount\":1}}\n\n")
	}))
	defer srv.Close()

	provider := Provider.Provider{ID: "test", Adapter: Provider.AdapterGemini, BaseURL: srv.URL}
	target, err := TargetFor(provider, "gemini-3", "AIza", TargetOptions{})
	if err != nil {
		t.Fatalf("target: %v", err)
	}
	req := &Protocol.Request{Model: "gemini-3", Messages: []Protocol.Message{
		{Role: Protocol.RoleUser, Blocks: []Protocol.Block{{Type: Protocol.BlockText, Text: "q"}}},
	}}
	res := collect(t, New().Run(context.Background(), req, &target))
	if res.Error != nil {
		t.Fatalf("error = %+v", res.Error)
	}
	if seenKey != "AIza" || !strings.HasSuffix(seenPath, ":streamGenerateContent") {
		t.Fatalf("key=%q path=%q", seenKey, seenPath)
	}
	if res.Message.Blocks[0].Text != "ciao" {
		t.Fatalf("blocks = %+v", res.Message.Blocks)
	}
}

func TestRouterMapsErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error":{"message":"slow down","type":"rate_limit_error"}}`)
	}))
	defer srv.Close()

	provider := Provider.Provider{ID: "test", Adapter: Provider.AdapterOpenAICompat, BaseURL: srv.URL}
	target, _ := TargetFor(provider, "m", "k", TargetOptions{})
	res := collect(t, New().Run(context.Background(), &Protocol.Request{Model: "m"}, &target))
	if res.Error == nil {
		t.Fatalf("expected error, got %+v", res)
	}
	if res.Error.Code != Protocol.CodeRateLimited {
		t.Fatalf("code = %s", res.Error.Code)
	}
	if res.Error.RetryAfter != 7000 {
		t.Fatalf("retry after = %d", res.Error.RetryAfter)
	}
	if res.Error.Message != "slow down" {
		t.Fatalf("message = %s", res.Error.Message)
	}
}

func TestRouterRejectsUnknownFamily(t *testing.T) {
	target := Protocol.Target{Family: "wat", BaseURL: "https://x", ModelID: "m"}
	res := collect(t, New().Run(context.Background(), &Protocol.Request{Model: "m"}, &target))
	if res.Error == nil || res.Error.Code != Protocol.CodeBadInput {
		t.Fatalf("error = %+v", res.Error)
	}
}

func TestRouterQuirksFlow(t *testing.T) {
	var seenMaxTokensField bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err == nil {
			_, seenMaxTokensField = body["max_completion_tokens"]
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"x\"},\"finish_reason\":\"stop\"}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	completion := true
	provider := Provider.Provider{ID: "test", Adapter: Provider.AdapterOpenAICompat, BaseURL: srv.URL, Quirks: &Provider.Quirks{MaxTokensField: "max_completion_tokens", StreamUsage: &completion}}
	target, _ := TargetFor(provider, "m", "k", TargetOptions{})
	req := &Protocol.Request{Model: "m", Messages: []Protocol.Message{{Role: Protocol.RoleUser, Blocks: []Protocol.Block{{Type: Protocol.BlockText, Text: "q"}}}}, MaxOutputTokens: 100}
	res := collect(t, New().Run(context.Background(), req, &target))
	if res.Error != nil {
		t.Fatalf("error = %+v", res.Error)
	}
	if !seenMaxTokensField {
		t.Fatalf("quirks were not applied to the wire body")
	}
	if res.Message.Blocks[0].Text != "x" {
		t.Fatalf("blocks = %+v", res.Message.Blocks)
	}
}

func TestRouterEmptyTargetFails(t *testing.T) {
	provider := Provider.Provider{ID: "test", Adapter: Provider.AdapterOpenAICompat, BaseURL: ""}
	if _, err := TargetFor(provider, "m", "k", TargetOptions{}); err == nil {
		t.Fatalf("expected error for empty base URL")
	}
	if _, err := TargetFor(Provider.Provider{BaseURL: "https://x"}, "", "k", TargetOptions{}); err == nil {
		t.Fatalf("expected error for empty model id")
	}
}

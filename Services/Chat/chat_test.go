package Chat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Services/Shared"
)

func TestReasoningLevel(t *testing.T) {
	cases := map[string]Protocol.ReasoningLevel{
		"LOW":     Protocol.ReasoningLow,
		" medium": Protocol.ReasoningMedium,
		"high":    Protocol.ReasoningHigh,
		"none":    Protocol.ReasoningNone,
		"":        Protocol.ReasoningUnset,
		"banana":  Protocol.ReasoningUnset,
	}
	for raw, want := range cases {
		if got := reasoningLevel(raw); got != want {
			t.Fatalf("reasoning level %q = %v want %v", raw, got, want)
		}
	}
}

func TestRunRegistryLifecycle(t *testing.T) {
	s := &Service{}
	id, _, cancel := s.register()
	active := s.Active()
	if len(active) != 1 || active[0] != id {
		t.Fatalf("active = %v", active)
	}
	if !strings.HasPrefix(id, "run-") {
		t.Fatalf("id = %q", id)
	}
	res := s.Stop(id)
	if !res.Ok {
		t.Fatalf("stop = %+v", res)
	}
	if got := s.Stop(id); got.Error == nil {
		t.Fatalf("double stop should error")
	}
	cancel()
}

func TestRunEventLoop(t *testing.T) {
	s := &Service{}
	s.Core = &Shared.Core{}

	events := []Protocol.Event{}
	var mu sync.Mutex
	s.Core.Notifier(func(name string, data any) {
		if name != Shared.EventModelStream {
			t.Fatalf("event name = %s", name)
		}
		ev, ok := data.(Shared.StreamEvent)
		if !ok {
			t.Fatalf("payload = %T", data)
		}
		mu.Lock()
		events = append(events, ev.Event)
		mu.Unlock()
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"wire says hi\"},\"finish_reason\":\"stop\"}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req := &Protocol.Request{Model: "m", Messages: []Protocol.Message{
		{Role: Protocol.RoleUser, Blocks: []Protocol.Block{{Type: Protocol.BlockText, Text: "q"}}},
	}}
	target := &Protocol.Target{Family: Protocol.FamilyOpenAICompat, BaseURL: srv.URL, ModelID: "m"}
	s.run("run-x", ctx, cancel, req, target)

	mu.Lock()
	defer mu.Unlock()
	n := 0
	for _, ev := range events {
		if ev.Type == Protocol.EventMessageStop || ev.Type == Protocol.EventError {
			n++
		}
	}
	if n != 1 || len(events) == 0 {
		t.Fatalf("events = %+v", events)
	}
	var out strings.Builder
	for _, ev := range events {
		if ev.Type == Protocol.EventBlockDelta && ev.Delta != nil && ev.Delta.Kind == Protocol.DeltaText {
			out.WriteString(ev.Delta.Text)
		}
	}
	if out.String() != "wire says hi" {
		t.Fatalf("text = %q", out.String())
	}
}

func TestRunCancelledCleanup(t *testing.T) {
	s := &Service{}
	s.Core = &Shared.Core{}
	s.Core.Notifier(func(name string, data any) {
		if name != Shared.EventStreamCancelled {
			return
		}
		ev := data.(Shared.StreamEvent)
		if ev.Event.Error == nil || ev.Event.Error.Code != Protocol.CodeCancelled {
			t.Fatalf("cancel event = %+v", ev.Event)
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	req := &Protocol.Request{Model: "m", Messages: []Protocol.Message{
		{Role: Protocol.RoleUser, Blocks: []Protocol.Block{{Type: Protocol.BlockText, Text: "q"}}},
	}}
	target := &Protocol.Target{Family: Protocol.FamilyOpenAICompat, BaseURL: "https://127.0.0.1:1", ModelID: "m"}
	cancel()
	s.run("run-x", ctx, cancel, req, target)
}

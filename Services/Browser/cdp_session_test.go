package Browser

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestAttachEnablesEveryDomain(t *testing.T) {
	f := newFakeCDP(t)
	f.respond(t, "Target.attachToTarget", okReply(map[string]any{"sessionId": "sess-9"}))
	c := f.dial(t)
	s, err := c.Attach(context.Background(), "target-1")
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if s.ID() != "sess-9" {
		t.Errorf("session id = %q, want sess-9", s.ID())
	}
	for _, method := range []string{"Page.enable", "Runtime.enable", "DOM.enable", "Accessibility.enable"} {
		got := f.sent(method)
		if len(got) != 1 {
			t.Errorf("%s sent %d times, want exactly once", method, len(got))
			continue
		}
		if sid := fmt.Sprint(got[0]["sessionId"]); sid != "sess-9" {
			t.Errorf("%s carried session %q, want sess-9", method, sid)
		}
	}
}

func TestAttachFailsWithoutASessionID(t *testing.T) {
	f := newFakeCDP(t)
	f.respond(t, "Target.attachToTarget", okReply(map[string]any{}))
	c := f.dial(t)
	if _, err := c.Attach(context.Background(), "t1"); err == nil {
		t.Fatal("Attach succeeded with no session id")
	}
}

// TestEvalRetriesOnADestroyedContext covers the everyday case: the agent acts,
// the page navigates, Chrome has thrown the execution context away, and the next
// read retries once against the new document instead of failing the turn.
func TestEvalRetriesOnADestroyedContext(t *testing.T) {
	f := newFakeCDP(t)
	f.respond(t, "Target.attachToTarget", okReply(map[string]any{"sessionId": "s1"}))

	var attempts int
	f.respond(t, "Runtime.evaluate", func(req map[string]any) map[string]any {
		attempts++
		if attempts == 1 { // the context that the navigation destroyed
			return errReply(-32000, "Cannot find context with specified id")(req)
		}
		return okReply(map[string]any{"result": map[string]any{
			"type": "string", "value": "alive",
		}})(req)
	})

	c := f.dial(t)
	s, err := c.Attach(context.Background(), "t1")
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	value, err := s.Eval(context.Background(), "1")
	if err != nil {
		t.Fatalf("Eval after a navigation: %v", err)
	}
	if value != "alive" {
		t.Errorf("Eval = %v, want alive", value)
	}
	if attempts != 2 {
		t.Errorf("evaluate attempts = %d, want 2 — one failure, one retry", attempts)
	}
	if got := len(f.sent("Runtime.evaluate")); got != 2 {
		t.Errorf("Runtime.evaluate sent %d times, want 2", got)
	}
}

func TestEvalSurfacesPageErrors(t *testing.T) {
	f := newFakeCDP(t)
	f.respond(t, "Target.attachToTarget", okReply(map[string]any{"sessionId": "s1"}))
	f.respond(t, "Page.getFrameTree", okReply(map[string]any{"frameTree": map[string]any{
		"frame": map[string]any{"id": "f", "url": "https://example.com"},
	}}))
	f.respond(t, "Page.createIsolatedWorld", okReply(map[string]any{"executionContextId": 7}))
	f.respond(t, "Runtime.evaluate", okReply(map[string]any{
		"result":           map[string]any{"type": "object"},
		"exceptionDetails": map[string]any{"text": "Uncaught"},
	}))
	c := f.dial(t)
	s, err := c.Attach(context.Background(), "t1")
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if _, err := s.Eval(context.Background(), "throw new Error('x')"); err == nil {
		t.Fatal("Eval reported success for a throwing script")
	} else if !strings.Contains(err.Error(), "page script") {
		t.Errorf("Eval error = %v, want a page-script failure", err)
	}
}

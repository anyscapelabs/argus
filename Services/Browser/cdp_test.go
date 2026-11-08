package Browser

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestDialReadsVersionAndConnects(t *testing.T) {
	f := newFakeCDP(t)
	c := f.dial(t)
	if got := c.Version(); got != "Chrome/142.0.7444.162" {
		t.Errorf("Version() = %q, want the reported browser", got)
	}
	if got := c.VersionMajor(); got != 142 {
		t.Errorf("VersionMajor() = %d, want 142", got)
	}
}

// TestCallRoutesReplyByID is the reason this client is its own file: DevTools
// answers as callbacks come due, so a client that trusts arrival order mixes up
// its own results.
func TestCallRoutesReplyByID(t *testing.T) {
	f := newFakeCDP(t)
	release := make(chan struct{})
	f.respond(t, "A.slow", func(req map[string]any) map[string]any {
		<-release
		return okReply(map[string]any{"value": "slow"})(req)
	})
	f.respond(t, "B.fast", okReply(map[string]any{"value": "fast"}))

	c := f.dial(t)
	type outcome struct {
		name string
		out  map[string]any
		err  error
	}
	got := make(chan outcome, 2)
	go func() {
		out, err := c.Call(context.Background(), "", "A.slow", map[string]any{"x": 1})
		got <- outcome{"A", out, err}
	}()
	go func() {
		out, err := c.Call(context.Background(), "", "B.fast", map[string]any{"x": 1})
		got <- outcome{"B", out, err}
	}()

	first := <-got
	if first.name != "B" || first.out["value"] != "fast" {
		t.Fatalf("the fast call came back as %v %v, want B/fast", first.name, first.out)
	}
	close(release)
	second := <-got
	if second.name != "A" || second.out["value"] != "slow" {
		t.Fatalf("the slow call came back as %v %v, want A/slow", second.name, second.out)
	}
}

func TestCallReportsProtocolError(t *testing.T) {
	f := newFakeCDP(t)
	f.respond(t, "Page.boom", errReply(-32601, "'Page.boom' wasn't found"))
	c := f.dial(t)
	_, err := c.Call(context.Background(), "", "Page.boom", nil)
	var ce *cdpError
	if !errors.As(err, &ce) {
		t.Fatalf("Call error = %v, want a *cdpError", err)
	}
	if ce.Code != -32601 || !strings.Contains(ce.Error(), "wasn't found") {
		t.Errorf("cdpError = %+v, want Chrome's code and text", ce)
	}
}

func TestEventsRoutedPerSession(t *testing.T) {
	f := newFakeCDP(t)
	c := f.dial(t)

	browserCh, offBrowser := c.On("", "Page.loadEventFired")
	tabCh, offTab := c.On("session-1", "Page.loadEventFired")
	t.Cleanup(func() { offBrowser(); offTab() })

	f.push(t, map[string]any{"method": "Page.loadEventFired", "params": map[string]any{"timestamp": 1}})
	f.push(t, map[string]any{
		"method":    "Page.loadEventFired",
		"sessionId": "session-1",
		"params":    map[string]any{"timestamp": 2},
	})

	if ev := <-browserCh; ev["timestamp"] != float64(1) {
		t.Errorf("browser subscriber got %v, want the session-less event", ev)
	}
	if ev := <-tabCh; ev["timestamp"] != float64(2) {
		t.Errorf("tab subscriber got %v, want the session-1 event", ev)
	}

	// A different tab's event must not wake this one.
	f.push(t, map[string]any{
		"method":    "Page.loadEventFired",
		"sessionId": "session-2",
		"params":    map[string]any{"timestamp": 3},
	})
	select {
	case ev := <-tabCh:
		t.Fatalf("tab subscriber woke for a foreign session: %v", ev)
	case <-time.After(150 * time.Millisecond):
	}
}

// TestEventBufferDropsOldest proves a stalled subscriber cannot back up the
// socket reader — if it could, every later Call would hang behind it.
func TestEventBufferDropsOldest(t *testing.T) {
	f := newFakeCDP(t)
	c := f.dial(t)
	ch, off := c.On("", "X.event")
	t.Cleanup(off)

	for i := 0; i < eventBuffer+10; i++ {
		f.push(t, map[string]any{"method": "X.event", "params": map[string]any{"seq": i}})
	}

	callDone := make(chan error, 1)
	go func() {
		_, err := c.Call(context.Background(), "", "Target.getTargets", nil)
		callDone <- err
	}()
	select {
	case err := <-callDone:
		if err != nil {
			t.Fatalf("Call after an event flood: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the read loop blocked behind a slow subscriber")
	}

	var last map[string]any
	for {
		select {
		case ev := <-ch:
			last = ev
		default:
			if last["seq"] != float64(eventBuffer+9) {
				t.Errorf("the event kept was %v, want the newest", last)
			}
			return
		}
	}
}

func TestReaderSurvivesOversizedFrame(t *testing.T) {
	f := newFakeCDP(t)
	big := strings.Repeat("x", 1<<20) // far past the library's default limit
	f.respond(t, "Runtime.evaluate", okReply(map[string]any{"result": map[string]any{
		"type": "string", "value": big,
	}}))
	c := f.dial(t)
	out, err := c.Call(context.Background(), "", "Runtime.evaluate", map[string]any{"expression": "1"})
	if err != nil {
		t.Fatalf("Call with a 1 MB frame: %v", err)
	}
	if got, ok := out["result"].(map[string]any)["value"].(string); !ok || len(got) != len(big) {
		t.Errorf("frame came back %d bytes, want %d", len(got), len(big))
	}
}

func TestCallHonoursContextCancellation(t *testing.T) {
	f := newFakeCDP(t)
	f.respond(t, "A.hang", silent)
	c := f.dial(t)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		_, err := c.Call(ctx, "", "A.hang", nil)
		errCh <- err
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Call error = %v, want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a cancelled call did not return")
	}

	// A late reply for a caller that gave up must be dropped, not handed to the
	// next call in line.
	f.push(t, map[string]any{"id": 1, "result": map[string]any{"late": true}})
	out, err := c.Call(context.Background(), "", "Target.getTargets", nil)
	if err != nil {
		t.Fatalf("Call after an orphan reply: %v", err)
	}
	if out["late"] != nil {
		t.Errorf("the orphan reply leaked into a later call: %v", out)
	}
}

func TestCloseFailsPendingCalls(t *testing.T) {
	f := newFakeCDP(t)
	f.respond(t, "A.hang", silent)
	c := f.dial(t)

	errCh := make(chan error, 1)
	go func() {
		_, err := c.Call(context.Background(), "", "A.hang", nil)
		errCh <- err
	}()
	time.Sleep(50 * time.Millisecond)
	_ = c.Close()

	select {
	case err := <-errCh:
		if !errors.Is(err, errClosed) {
			t.Fatalf("pending call error = %v, want errClosed", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("closing the client left a call hanging")
	}
	if _, err := c.Call(context.Background(), "", "Target.getTargets", nil); !errors.Is(err, errClosed) {
		t.Errorf("Call after Close = %v, want errClosed", err)
	}
}

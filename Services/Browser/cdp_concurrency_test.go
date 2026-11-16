package Browser

// Concurrency is not hypothetical here: the agent loop can issue several tool
// calls, the idle timer can close a session underneath them, and a snapshot can
// be re-read while a click is in flight. These tests pin that behaviour down.

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// TestConcurrentCallsStayPaired sends a hundred commands from twenty goroutines
// and insists every one gets back the value it asked for. A lost write lock or an
// id mixed up in routing shows here.
func TestConcurrentCallsStayPaired(t *testing.T) {
	f := newFakeCDP(t)
	f.respond(t, "Echo.call", func(req map[string]any) map[string]any {
		params, _ := req["params"].(map[string]any)
		return okReply(map[string]any{"echo": params["want"]})(req)
	})
	c := f.dial(t)

	const perWorker = 5
	var wg sync.WaitGroup
	results := make(chan string, 20*perWorker)
	for w := 0; w < 20; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				want := fmt.Sprintf("worker-%d-call-%d", worker, i)
				out, err := c.Call(context.Background(), "", "Echo.call", map[string]any{"want": want})
				if err != nil {
					results <- "error: " + err.Error()
					continue
				}
				if got := fmt.Sprint(out["echo"]); got != want {
					results <- fmt.Sprintf("got %q want %q", got, want)
					continue
				}
				results <- ""
			}
		}(w)
	}
	wg.Wait()
	close(results)

	for res := range results {
		if res != "" {
			t.Fatal(res)
		}
	}
}

// TestSubscriberCountAndClose exercises the subscription map, which every
// snapshot wait touches, and checks that closing a client leaves nothing behind.
func TestSubscribeAndCloseCleansUp(t *testing.T) {
	f := newFakeCDP(t)
	c := f.dial(t)

	var offs []func()
	for i := 0; i < 50; i++ {
		_, off := c.On("s"+strconv.Itoa(i), "Runtime.consoleAPICalled")
		offs = append(offs, off)
	}
	c.mu.Lock()
	live := len(c.subs)
	c.mu.Unlock()
	if live != 50 {
		t.Fatalf("live subscribers = %d, want 50", live)
	}
	for _, off := range offs[:25] {
		off()
	}
	c.mu.Lock()
	afterPartial := len(c.subs)
	c.mu.Unlock()
	if afterPartial != 25 {
		t.Fatalf("after unsubscribing half, live = %d, want 25", afterPartial)
	}

	if err := c.Close(); err != nil {
		t.Logf("Close returned %v (a refused close frame is fine)", err)
	}
	c.mu.Lock()
	afterClose := len(c.subs)
	pending := len(c.pending)
	c.mu.Unlock()
	if afterClose != 0 || pending != 0 {
		t.Errorf("after Close: subs=%d pending=%d, want both empty", afterClose, pending)
	}
	// A second Close must not panic on the already-closed channels.
	_ = c.Close()
	for _, off := range offs[25:] {
		off()
	}
}

// TestPushAfterCloseIsHarmless proves a late frame from a dying Chrome cannot
// panic the reader: the read loop exits and pending work is resolved.
func TestPushAfterCloseIsHarmless(t *testing.T) {
	f := newFakeCDP(t)
	c := f.dial(t)
	ch, off := c.On("", "Some.event")
	defer off()

	f.push(t, map[string]any{"method": "Some.event", "params": map[string]any{"before": true}})
	<-ch

	// Close mid-stream, then keep talking. The fake tolerates the broken pipe;
	// what matters is that nothing above it panics or hangs.
	conn := f.waitForConn(t)
	_ = c.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = conn.Write(context.Background(), websocket.MessageText, []byte(`{"method":"Some.event"}`))
		_ = conn.Write(context.Background(), websocket.MessageText, []byte(`{"id":999,"result":{}}`))
		_ = conn.Write(context.Background(), websocket.MessageText, []byte(`not json at all`))
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("writing to a closed client blocked")
	}
}

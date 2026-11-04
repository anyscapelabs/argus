package Browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fakeCDP is a stand-in Chrome: it talks to the client over a real WebSocket so
// framing, id correlation, session routing and cancellation are all exercised
// without a browser on the machine. Responders live on the instance, not in a
// package global, so two test packages running in parallel cannot answer each
// other's methods.
type fakeCDP struct {
	server    *httptest.Server
	baseURL   string
	mu        sync.Mutex
	conns     []*websocket.Conn
	inbound   []map[string]any
	resp      map[string]func(req map[string]any) map[string]any
	closeOnce sync.Once
}

func newFakeCDP(t *testing.T) *fakeCDP {
	t.Helper()
	f := &fakeCDP{resp: map[string]func(map[string]any) map[string]any{}}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/json/version":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"Browser":              "Chrome/142.0.7444.162",
				"webSocketDebuggerUrl": f.wsURL(),
			})
		case "/ws":
			// Chrome itself is picky about Origin; the client sends the header the
			// real devtools endpoint expects. The fake accepts anything.
			ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
			if err != nil {
				t.Errorf("fake cdp: accept: %v", err)
				return
			}
			f.mu.Lock()
			f.conns = append(f.conns, ws)
			f.mu.Unlock()
			f.serve(ws)
		default:
			http.NotFound(w, r)
		}
	}))
	f.baseURL = f.server.URL
	t.Cleanup(func() {
		f.close()
		f.server.Close()
	})
	return f
}

func (f *fakeCDP) wsURL() string {
	return "ws" + strings.TrimPrefix(f.baseURL, "http") + "/ws"
}

func (f *fakeCDP) close() {
	f.closeOnce.Do(func() {
		f.mu.Lock()
		conns := append([]*websocket.Conn(nil), f.conns...)
		f.mu.Unlock()
		for _, c := range conns {
			_ = c.CloseNow()
		}
	})
}

// serve answers every request with an empty result unless the test installed a
// responder for that method. Inbound frames are recorded, because most
// assertions here are about what the client sent.
func (f *fakeCDP) serve(ws *websocket.Conn) {
	ctx := context.Background()
	for {
		_, data, err := ws.Read(ctx)
		if err != nil {
			return
		}
		var msg map[string]any
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		f.mu.Lock()
		f.inbound = append(f.inbound, msg)
		fn := f.resp[msg["method"].(string)]
		f.mu.Unlock()

		// Each request is answered on its own goroutine. Real DevTools answers
		// commands concurrently, so a responder that blocks must not starve the
		// calls behind it — that would test the harness, not the client.
		go func() {
			reply := map[string]any{"id": msg["id"], "result": map[string]any{}}
			if fn != nil {
				if out := fn(msg); out != nil {
					if out[neverKey] == true {
						return // the test wants this call unanswered
					}
					reply = out
				}
			}
			raw, _ := json.Marshal(reply)
			f.mu.Lock()
			defer f.mu.Unlock()
			_ = ws.Write(ctx, websocket.MessageText, raw)
		}()
	}
}

// neverKey marks a reply that must never be sent, which is how the cancellation
// and close paths are exercised.
const neverKey = "__never_reply__"

func silent(map[string]any) map[string]any { return map[string]any{neverKey: true} }

// respond installs a canned reply for one method for the rest of the test. The
// responder is deliberately never removed: a pending call being cancelled must
// not depend on when the cleanup happens to run.
func (f *fakeCDP) respond(t *testing.T, method string, fn func(req map[string]any) map[string]any) {
	t.Helper()
	f.mu.Lock()
	f.resp[method] = fn
	f.mu.Unlock()
}

func (f *fakeCDP) dial(t *testing.T) *Client {
	t.Helper()
	c, err := Dial(context.Background(), f.baseURL)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// okReply wraps a result payload with the id Chrome would echo. The client
// matches on id, so the value here only has to be a number.
func okReply(result map[string]any) func(map[string]any) map[string]any {
	return func(req map[string]any) map[string]any {
		return map[string]any{"id": req["id"], "result": result}
	}
}

func errReply(code int, message string) func(map[string]any) map[string]any {
	return func(req map[string]any) map[string]any {
		return map[string]any{"id": req["id"], "error": map[string]any{
			"code": code, "message": message,
		}}
	}
}

func (f *fakeCDP) sent(method string) []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []map[string]any
	for _, m := range f.inbound {
		if m["method"] == method {
			out = append(out, m)
		}
	}
	return out
}

// waitForConn returns the first accepted connection. The upgrade completes and
// Dial returns before the handler records the conn, so any test that pushes an
// event must wait for it rather than assume it is there.
func (f *fakeCDP) waitForConn(t *testing.T) *websocket.Conn {
	t.Helper()
	for i := 0; i < 200; i++ {
		f.mu.Lock()
		conns := append([]*websocket.Conn(nil), f.conns...)
		f.mu.Unlock()
		if len(conns) > 0 {
			return conns[0]
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("fake cdp: no client connected")
	return nil
}

// push writes one event frame to the connected client.
func (f *fakeCDP) push(t *testing.T, event map[string]any) {
	t.Helper()
	conn := f.waitForConn(t)
	raw, _ := json.Marshal(event)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// Writes share the connection lock: concurrent Write calls on one Conn are
	// not allowed, and a frame from two sources would interleave.
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := conn.Write(ctx, websocket.MessageText, raw); err != nil {
		t.Fatalf("fake cdp: push: %v", err)
	}
}

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

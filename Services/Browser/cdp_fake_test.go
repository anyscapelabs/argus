package Browser

import (
	"context"
	"encoding/json"
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

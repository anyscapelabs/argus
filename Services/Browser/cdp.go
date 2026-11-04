// Package Browser drives a real Chrome over the Chrome DevTools Protocol and
// hands the agent a numbered accessibility snapshot of the page instead of a
// screenshot.
//
// The protocol layer here is deliberately our own: no chromedp, no go-rod, no
// Playwright driver. Those libraries exist to drive a browser generically, and
// what makes this feature useful is the snapshot, not the socket. We keep the
// transport from a maintained library — coder/websocket, already in the module
// graph through Wails — because hand-writing RFC 6455 buys risk, not control.
package Browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

// Timeouts per call type. A DevTools call is a round trip into another process
// on localhost, so anything slow here is a stuck page, not a busy machine, and
// the agent is better served by an error it can act on than by an infinite wait.
const (
	dialTimeout       = 10 * time.Second
	shortCallTimeout  = 5 * time.Second
	navigateTimeout   = 25 * time.Second
	screenshotTimeout = 20 * time.Second
	shutdownTimeout   = 5 * time.Second

	// writeTimeout bounds only the send, never the wait for a reply.
	writeTimeout = 10 * time.Second

	// readLimit bounds one inbound frame. Screenshots arrive as base64 inside a
	// single JSON message and a fat page is not a protocol error.
	readLimit = 8 << 20

	// eventBuffer is how far behind a slow subscriber may fall before its
	// oldest events are dropped. Delivery must never block the read loop.
	eventBuffer = 64

	// heartbeat keeps the debug socket from going idle.
	heartbeat = 20 * time.Second
)

// errClosed is what a pending call resolves to when the connection dies.
var errClosed = errors.New("browser: the devtools connection closed")

// cdpError is a protocol-level failure: Chrome understood us and said no.
type cdpError struct {
	Method string
	Code   int
	Text   string
	Data   string
}

func (e *cdpError) Error() string {
	if e.Data != "" {
		return fmt.Sprintf("devtools call %s failed (%d): %s — %s", e.Method, e.Code, e.Text, e.Data)
	}
	return fmt.Sprintf("devtools call %s failed (%d): %s", e.Method, e.Code, e.Text)
}

// envelope is one CDP message, in either direction. Unknown fields are simply
// not decoded, so a Chrome field we have never seen cannot break the parse.
type envelope struct {
	ID        int64           `json:"id"`
	Method    string          `json:"method"`
	Params    json.RawMessage `json:"params"`
	SessionID string          `json:"sessionId"`
	Result    json.RawMessage `json:"result"`
	Error     *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data"`
}

type pendingCall struct {
	ch   chan envelope
	meta string
}

type subscriber struct {
	id        int64
	method    string
	sessionID string
	ch        chan map[string]any
}

// Client is one WebSocket to a Chrome instance, safe for concurrent use.
type Client struct {
	ws      *websocket.Conn
	version string

	// writeMu serialises outbound frames: a websocket.Conn allows one writer at
	// a time, and two tool calls racing to send would corrupt the stream.
	writeMu sync.Mutex

	nextID    atomic.Int64
	closed    atomic.Bool
	done      chan struct{}
	closeOnce sync.Once

	mu      sync.Mutex
	pending map[int64]*pendingCall
	subnext int64
	subs    map[int64]*subscriber
}

// Dial connects to the browser endpoint at base ("http://127.0.0.1:9222"),
// asking Chrome first where its WebSocket lives. The HTTP step is what makes a
// random debug port usable without telling us in advance.
func Dial(ctx context.Context, base string) (*Client, error) {
	infoURL := strings.TrimRight(base, "/") + "/json/version"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, infoURL, nil)
	if err != nil {
		return nil, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("browser: cannot reach the devtools endpoint at %s: %w", infoURL, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("browser: reading the devtools version: %w", err)
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("browser: devtools version endpoint returned %d: %s", res.StatusCode, firstLine(string(body)))
	}
	var info struct {
		Browser           string `json:"Browser"`
		WebSocketDebugger string `json:"webSocketDebuggerUrl"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("browser: decoding the devtools version: %w", err)
	}
	if info.WebSocketDebugger == "" {
		return nil, errors.New("browser: Chrome reported no devtools WebSocket URL")
	}
	c, err := dialWS(ctx, info.WebSocketDebugger)
	if err != nil {
		return nil, err
	}
	c.version = info.Browser
	return c, nil
}

// DialWS attaches to a known DevTools WebSocket URL. Split out so tests can
// point a real client at a fake Chrome.
func DialWS(ctx context.Context, wsURL string) (*Client, error) { return dialWS(ctx, wsURL) }

func dialWS(ctx context.Context, wsURL string) (*Client, error) {
	dialCtx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()

	// Chrome checks Origin on the debug socket and answers 403 for anything it
	// does not recognise as a local devtools client.
	opts := &websocket.DialOptions{
		HTTPHeader: map[string][]string{"Origin": {"http://localhost"}},
	}
	ws, res, err := websocket.Dial(dialCtx, wsURL, opts)
	if err != nil {
		if res != nil {
			return nil, fmt.Errorf("browser: devtools handshake failed (HTTP %d): %w", res.StatusCode, err)
		}
		return nil, fmt.Errorf("browser: devtools handshake failed: %w", err)
	}
	ws.SetReadLimit(readLimit)

	c := &Client{
		ws:      ws,
		done:    make(chan struct{}),
		pending: map[int64]*pendingCall{},
		subs:    map[int64]*subscriber{},
	}
	readCtx, stopRead := context.WithCancel(context.Background())
	go c.readLoop(readCtx, stopRead)
	go c.pingLoop()
	return c, nil
}

// Version is Chrome's own self-report, e.g. "Chrome/142.0.7444.162".
func (c *Client) Version() string { return c.version }

// VersionMajor is the numeric branch of Version, 0 when it cannot be read.
func (c *Client) VersionMajor() int {
	rest := c.version
	if i := strings.Index(rest, "/"); i >= 0 {
		rest = rest[i+1:]
	}
	num := rest
	if i := strings.Index(num, "."); i >= 0 {
		num = num[:i]
	}
	var n int
	for _, r := range num {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}

// Call sends one command and waits for its reply. sessionID is empty for
// browser-level calls and the flattened session id for per-target calls.
//
// Replies are matched by id, never by arrival order — DevTools answers
// Fetch/Security/Page callbacks as they come due, so a client that assumes FIFO
// corrupts its own results.
func (c *Client) Call(ctx context.Context, sessionID, method string, params map[string]any) (map[string]any, error) {
	if c.closed.Load() {
		return nil, errClosed
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, defaultCallTimeout(method))
		defer cancel()
	}

	id := c.nextID.Add(1)
	ch := make(chan envelope, 1)
	c.mu.Lock()
	if c.closed.Load() {
		c.mu.Unlock()
		return nil, errClosed
	}
	c.pending[id] = &pendingCall{ch: ch, meta: method}
	c.mu.Unlock()

	msg := map[string]any{"id": id, "method": method}
	if params != nil {
		msg["params"] = params
	}
	if sessionID != "" {
		msg["sessionId"] = sessionID
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		c.forget(id)
		return nil, fmt.Errorf("browser: encoding %s: %w", method, err)
	}
	// The write takes the connection lock, so one Call cannot interleave with
	// another. Its deadline is the caller's, capped, because a stuck send must
	// release the lock for everyone behind it rather than wedge the session.
	if err := c.write(ctx, raw); err != nil {
		c.forget(id)
		if c.closed.Load() {
			return nil, errClosed
		}
		return nil, fmt.Errorf("browser: sending %s: %w", method, err)
	}

	select {
	case env := <-ch:
		if env.Error != nil {
			return nil, &cdpError{
				Method: method,
				Code:   env.Error.Code,
				Text:   env.Error.Message,
				Data:   env.Error.Data,
			}
		}
		if len(env.Result) == 0 {
			return map[string]any{}, nil
		}
		var out map[string]any
		if err := json.Unmarshal(env.Result, &out); err != nil {
			return nil, fmt.Errorf("browser: decoding the reply to %s: %w", method, err)
		}
		return out, nil
	case <-ctx.Done():
		c.forget(id)
		return nil, ctx.Err()
	case <-c.done:
		c.forget(id)
		return nil, errClosed
	}
}

// write sends one frame under the connection lock.
func (c *Client) write(ctx context.Context, raw []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.closed.Load() {
		return errClosed
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, writeTimeout)
		defer cancel()
	}
	return c.ws.Write(ctx, websocket.MessageText, raw)
}

// CallInto is Call with the reply decoded into a typed result.
func (c *Client) CallInto(ctx context.Context, sessionID, method string, params map[string]any, out any) error {
	res, err := c.Call(ctx, sessionID, method, params)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	raw, err := json.Marshal(res)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

// On subscribes to an event method. Events arriving for a session are delivered
// only to subscribers naming that session, and browser-level subscribers (empty
// sessionID) only see browser-level events, so one target's flood cannot wake
// another target's waiters. The returned function removes the subscription.
func (c *Client) On(sessionID, method string) (<-chan map[string]any, func()) {
	sub := &subscriber{
		id:        c.nextSub(),
		method:    method,
		sessionID: sessionID,
		ch:        make(chan map[string]any, eventBuffer),
	}
	c.mu.Lock()
	if c.closed.Load() {
		c.mu.Unlock()
		close(sub.ch)
		return sub.ch, func() {}
	}
	c.subs[sub.id] = sub
	c.mu.Unlock()
	return sub.ch, func() { c.removeSub(sub.id) }
}

func (c *Client) nextSub() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.subnext++
	return c.subnext
}

func (c *Client) removeSub(id int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.subs, id)
}

// Close ends the session. Every in-flight Call returns errClosed and every
// event channel is closed; a second call is harmless.
func (c *Client) Close() error {
	var err error
	c.closeOnce.Do(func() {
		c.closed.Store(true)
		close(c.done)
		err = c.ws.Close(websocket.StatusNormalClosure, "browser session closed")
		c.drain()
	})
	return err
}

// alive reports whether the transport still works. The read loop flips closed
// the moment the socket dies — which, when the user closes the Chrome window,
// is the first place the exit is observable — so this is how a stale session is
// detected before any process signal catches up.
func (c *Client) alive() bool {
	return c != nil && !c.closed.Load()
}

// isDeadTransport reports whether err came from a dead DevTools connection or
// a CDP session/target that no longer exists — the failures a browser the
// user (or a crash) removed leaves behind, and the ones worth a relaunch.
func isDeadTransport(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errClosed) {
		return true
	}
	var ce *cdpError
	if errors.As(err, &ce) {
		msg := strings.ToLower(ce.Text + " " + ce.Data)
		return strings.Contains(msg, "session") && strings.Contains(msg, "not found") ||
			strings.Contains(msg, "no active target") ||
			strings.Contains(msg, "target closed") ||
			strings.Contains(msg, "no target with given id") ||
			strings.Contains(msg, "inspector detached")
	}
	return false
}

func (c *Client) forget(id int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.pending, id)
}

// drain fails every waiter, used once on close.
func (c *Client) drain() {
	c.mu.Lock()
	waiters := make([]chan envelope, 0, len(c.pending))
	for id, p := range c.pending {
		waiters = append(waiters, p.ch)
		delete(c.pending, id)
	}
	subs := make([]*subscriber, 0, len(c.subs))
	for id, s := range c.subs {
		subs = append(subs, s)
		delete(c.subs, id)
	}
	c.mu.Unlock()

	for _, ch := range waiters {
		select {
		case ch <- envelope{Error: &rpcError{Message: errClosed.Error()}}:
		default:
		}
	}
	for _, s := range subs {
		close(s.ch)
	}
}

func (c *Client) readLoop(ctx context.Context, stop context.CancelFunc) {
	defer stop()
	defer c.Close()
	for {
		_, data, err := c.ws.Read(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			return
		}
		var env envelope
		if err := json.Unmarshal(data, &env); err != nil {
			// A frame we cannot read is a frame we must not route. Log-and-drop
			// beats a panic in the only goroutine that owns the socket.
			continue
		}
		if env.ID != 0 {
			c.routeReply(env)
			continue
		}
		if env.Method != "" {
			c.routeEvent(env)
		}
	}
}

func (c *Client) routeReply(env envelope) {
	c.mu.Lock()
	p, ok := c.pending[env.ID]
	if ok {
		delete(c.pending, env.ID)
	}
	c.mu.Unlock()
	if !ok {
		return // a reply whose caller already timed out or was cancelled
	}
	p.ch <- env
}

func (c *Client) routeEvent(env envelope) {
	var payload map[string]any
	if len(env.Params) > 0 {
		_ = json.Unmarshal(env.Params, &payload)
	}
	if payload == nil {
		payload = map[string]any{}
	}

	c.mu.Lock()
	matched := make([]*subscriber, 0, 4)
	for _, s := range c.subs {
		if s.method == env.Method && s.sessionID == env.SessionID {
			matched = append(matched, s)
		}
	}
	c.mu.Unlock()

	for _, s := range matched {
		select {
		case s.ch <- payload:
		default:
			// Drop-oldest: the agent acts on what the page is now, so the newest
			// event is worth more than a queue full of stale ones.
			select {
			case <-s.ch:
			default:
			}
			select {
			case s.ch <- payload:
			default:
			}
		}
	}
}

func (c *Client) pingLoop() {
	t := time.NewTicker(heartbeat)
	defer t.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-t.C:
			ctx, cancel := context.WithTimeout(context.Background(), heartbeat)
			err := c.ws.Ping(ctx)
			cancel()
			if err != nil {
				return
			}
		}
	}
}

// defaultCallTimeout is per method: navigation legitimately takes seconds, a
// keypress should not.
func defaultCallTimeout(method string) time.Duration {
	switch method {
	case "Page.navigate", "Page.reload", "Page.stopLoading":
		return navigateTimeout
	case "Page.captureScreenshot":
		return screenshotTimeout
	case "Runtime.callFunctionOn", "Runtime.evaluate", "Input.dispatchMouseEvent",
		"Input.dispatchKeyEvent", "Input.insertText":
		return shortCallTimeout
	default:
		return 10 * time.Second
	}
}

// firstLine keeps an HTTP error body to one readable line.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	if len(s) > 200 {
		return s[:200]
	}
	return s
}

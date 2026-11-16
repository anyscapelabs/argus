package Browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

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

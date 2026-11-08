package Browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// frameTree is the subset of Page.getFrameTree we care about: the document and
// its child frames, one level deep at a time.
type frameTree struct {
	Frame       frame       `json:"frame"`
	ChildFrames []frameTree `json:"childFrames"`
}

type frame struct {
	ID       string `json:"id"`
	URL      string `json:"url"`
	Name     string `json:"name"`
	Security string `json:"securityOrigin"`
}

// MainFrame resolves and caches the main frame id, which Page.createIsolatedWorld
// and the AX tree both need.
func (s *Session) MainFrame(ctx context.Context) (string, error) {
	if s.mainFrame != "" {
		return s.mainFrame, nil
	}
	var out struct {
		FrameTree frameTree `json:"frameTree"`
	}
	if err := s.CallInto(ctx, mPageFrameTree, nil, &out); err != nil {
		return "", err
	}
	id := out.FrameTree.Frame.ID
	if id == "" {
		return "", errors.New("browser: Chrome reported a frame tree with no main frame")
	}
	s.mainFrame = id
	return id, nil
}

// Frames lists every frame in the page with the depth it sits at, main frame
// first. Cross-origin frames are included; callers decide what they can reach.
type Frame struct {
	ID    string
	URL   string
	Depth int
}

func (s *Session) Frames(ctx context.Context) ([]Frame, error) {
	var out struct {
		FrameTree frameTree `json:"frameTree"`
	}
	if err := s.CallInto(ctx, mPageFrameTree, nil, &out); err != nil {
		return nil, err
	}
	var frames []Frame
	var walk func(t frameTree, depth int)
	walk = func(t frameTree, depth int) {
		frames = append(frames, Frame{ID: t.Frame.ID, URL: t.Frame.URL, Depth: depth})
		for _, child := range t.ChildFrames {
			walk(child, depth+1)
		}
	}
	walk(out.FrameTree, 0)
	if len(frames) == 0 {
		return nil, errors.New("browser: Chrome reported an empty frame tree")
	}
	return frames, nil
}

// invalidateFrame drops the cached frame id. A navigation replaces the frame, and
// the next lookup must not reuse the old one.
func (s *Session) invalidateFrame() { s.mainFrame = "" }

// contextLost reports whether err means the execution context we used is gone,
// which is the normal consequence of a navigation and the trigger for a retry.
func contextLost(err error) bool {
	var ce *cdpError
	if !errors.As(err, &ce) {
		return false
	}
	msg := strings.ToLower(ce.Text)
	return strings.Contains(msg, "cannot find context") ||
		strings.Contains(msg, "context was destroyed") ||
		strings.Contains(msg, "execution context")
}

// PageLifecycleEvent is the event name Chrome reports render milestones on.
const PageLifecycleEvent = "Page.lifecycleEvent"

// ExpectLifecycle starts listening for a lifecycle event now and returns a
// function that waits for it. Listening has to begin before the command that
// causes the event, or a fast page fires it where nobody can hear — which is why
// this is not just a "wait" call.
//
// `load` is the milestone a navigation is finished. DevTools uses these events
// instead of a fixed sleep, and so should we: a page that is still loading is a
// page whose tree is not real yet.
func (s *Session) ExpectLifecycle(ctx context.Context, name string) func(timeout time.Duration) bool {
	frameID, _ := s.MainFrame(ctx)
	ch, off := s.On(PageLifecycleEvent)
	return func(timeout time.Duration) bool {
		defer off()
		deadline := time.NewTimer(timeout)
		defer deadline.Stop()
		for {
			select {
			case ev, ok := <-ch:
				if !ok {
					return false
				}
				if ev["name"] != name {
					continue
				}
				// An unknown frame id means "any frame": better to accept an early
				// signal than to wait out the timeout on a page that is ready.
				if frameID == "" || ev["frameId"] == frameID {
					return true
				}
			case <-deadline.C:
				return false
			}
		}
	}
}

// Eval runs an expression in the page's own JavaScript world and returns the
// decoded value.
//
// This is the main world on purpose. An isolated world would hide our globals
// from page scripts, but it also gives every DOM node a separate JS wrapper, and
// the framework state we have to respect lives on that wrapper: React's value
// tracker, Vue's reactivity hooks, a component's own listeners. Type into a
// controlled input from an isolated world and the framework never learns about
// it — the value shows on screen and the submit handler reads an empty field.
//
// userGesture makes focus, window.open and friends behave like a real
// interaction on the page rather than a script in the console.
func (s *Session) Eval(ctx context.Context, expr string) (any, error) {
	value, err := s.evalOnce(ctx, expr, true)
	if err != nil && contextLost(err) {
		s.invalidateFrame()
		return s.evalOnce(ctx, expr, true)
	}
	return value, err
}

// evalOnce performs one Runtime.evaluate. returnByValue keeps results as data;
// the false path hands back an objectId for a DOM node we want to act on.
func (s *Session) evalOnce(ctx context.Context, expr string, byValue bool) (any, error) {
	var out struct {
		Result struct {
			Type        string          `json:"type"`
			Subtype     string          `json:"subtype"`
			Value       json.RawMessage `json:"value"`
			ObjectID    string          `json:"objectId"`
			Description string          `json:"description"`
			ClassName   string          `json:"className"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text      string `json:"text"`
			Exception struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	if err := s.CallInto(ctx, mRuntimeEvaluate, map[string]any{
		"expression":    expr,
		"returnByValue": byValue,
		"awaitPromise":  true,
		"userGesture":   true,
		"silent":        true,
	}, &out); err != nil {
		return nil, err
	}
	if out.ExceptionDetails != nil {
		detail := out.ExceptionDetails.Exception.Description
		if detail == "" {
			detail = out.ExceptionDetails.Text
		}
		return nil, fmt.Errorf("browser: the page script failed: %s", firstLine(detail))
	}
	if !byValue {
		if out.Result.ObjectID == "" {
			return nil, fmt.Errorf("browser: Chrome returned no handle for the element")
		}
		return out.Result.ObjectID, nil
	}
	if out.Result.Type == "undefined" || len(out.Result.Value) == 0 {
		return nil, nil
	}
	var value any
	if err := json.Unmarshal(out.Result.Value, &value); err != nil {
		return nil, fmt.Errorf("browser: decoding the script result: %w", err)
	}
	return value, nil
}

// EvalString is Eval for expressions that must produce text.
func (s *Session) EvalString(ctx context.Context, expr string) (string, error) {
	value, err := s.Eval(ctx, expr)
	if err != nil {
		return "", err
	}
	if value == nil {
		return "", nil
	}
	if str, ok := value.(string); ok {
		return str, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// Handle evaluates an expression that must produce an object and returns the
// DevTools objectId for it — our handle on a live DOM node.
func (s *Session) Handle(ctx context.Context, expr string) (string, error) {
	value, err := s.evalOnce(ctx, expr, false)
	if err != nil {
		if contextLost(err) {
			s.invalidateFrame()
			return "", errHandleLost
		}
		return "", err
	}
	id, ok := value.(string)
	if !ok || id == "" {
		return "", errHandleLost
	}
	return id, nil
}

// RefHandle returns a handle on the element our snapshot stamped with ref n. The
// page pass mirrored the element into a Map on window, which beats re-querying by
// attribute: a node the framework has just unmounted is still in the map, and its
// absence is exactly the stale-ref answer we want to give.
func (s *Session) RefHandle(ctx context.Context, ref int) (string, error) {
	expr := fmt.Sprintf("(function(){var m=window[%q];if(!m)return null;var el=m.get(%d);return el||null;})()",
		refMapKey, ref)
	return s.Handle(ctx, expr)
}

// CallOn runs a function with one DOM node as its receiver, addressed by handle.
// This is how an action re-reads live geometry instead of trusting a rect from a
// previous snapshot.
func (s *Session) CallOn(ctx context.Context, objectID, functionDeclaration string, args ...any) (any, error) {
	var out struct {
		Result struct {
			Type  string          `json:"type"`
			Value json.RawMessage `json:"value"`
		} `json:"result"`
		ExceptionDetails *struct {
			Text      string `json:"text"`
			Exception struct {
				Description string `json:"description"`
			} `json:"exception"`
		} `json:"exceptionDetails"`
	}
	params := map[string]any{
		"objectId":            objectID,
		"functionDeclaration": functionDeclaration,
		"returnByValue":       true,
		"awaitPromise":        true,
		"userGesture":         true,
		"silent":              true,
	}
	if len(args) > 0 {
		encoded := make([]map[string]any, 0, len(args))
		for _, a := range args {
			raw, err := json.Marshal(a)
			if err != nil {
				return nil, fmt.Errorf("browser: encoding a call argument: %w", err)
			}
			encoded = append(encoded, map[string]any{"value": json.RawMessage(raw)})
		}
		params["arguments"] = encoded
	}
	res, err := s.Call(ctx, mRuntimeCallOn, params)
	if err != nil {
		if isMissingHandle(err) {
			return nil, errHandleLost
		}
		return nil, err
	}
	raw, err := json.Marshal(res)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	if out.ExceptionDetails != nil {
		detail := out.ExceptionDetails.Exception.Description
		if detail == "" {
			detail = out.ExceptionDetails.Text
		}
		return nil, fmt.Errorf("browser: the page script failed: %s", firstLine(detail))
	}
	if len(out.Result.Value) == 0 {
		return nil, nil
	}
	var value any
	if err := json.Unmarshal(out.Result.Value, &value); err != nil {
		return nil, err
	}
	return value, nil
}

// errHandleLost means the node we held is gone — navigated away, unmounted by the
// framework, or collected. Callers treat it as a stale ref.
var errHandleLost = errors.New("browser: that element is no longer on the page")

// refMapKey is the window property the page pass stores the ref→element Map on.
// It is a single well-named property rather than a scattered attribute, so a page
// that inspects its own DOM sees nothing unusual.
const refMapKey = "__argus_refs__"

// isMissingHandle reports the DevTools way of saying "that objectId is dead".
func isMissingHandle(err error) bool {
	var ce *cdpError
	if !errors.As(err, &ce) {
		return false
	}
	msg := strings.ToLower(ce.Text)
	return strings.Contains(msg, "cannot find object with given id") ||
		strings.Contains(msg, "missing object")
}

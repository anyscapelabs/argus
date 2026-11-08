package Browser

// The DevTools domains this feature touches, and the small session layer that
// sits on top of Client. Everything here is deliberately explicit: one file that
// lists every protocol call the browser feature makes, so a Chrome upgrade can
// be checked against one page rather than a generated mirror of the whole spec.
import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
)

// CDP methods in use. Stable-domain calls only — the newer adhoc bindings
// (Accessibility.queryAXTree, pierced DOM.getSubtree) still change shape between
// Chrome versions and are avoided on purpose.
const (
	mTargetCreate          = "Target.createTarget"
	mTargetClose           = "Target.closeTarget"
	mTargetAttach          = "Target.attachToTarget"
	mTargetDetach          = "Target.detachFromTarget"
	mTargetGetTargets      = "Target.getTargets"
	mPageEnable            = "Page.enable"
	mPageNavigate          = "Page.navigate"
	mPageReload            = "Page.reload"
	mPageHistory           = "Page.getNavigationHistory"
	mPageFrameTree         = "Page.getFrameTree"
	mPageLifecycleEnable   = "Page.setLifecycleEventsEnabled"
	mPageScreenshot        = "Page.captureScreenshot"
	mRuntimeEnable         = "Runtime.enable"
	mRuntimeEvaluate       = "Runtime.evaluate"
	mRuntimeCallOn         = "Runtime.callFunctionOn"
	mDOMEnable             = "DOM.enable"
	mAccessibilityEnable   = "Accessibility.enable"
	mAccessibilityFullTree = "Accessibility.getFullAXTree"
	mInputMouse            = "Input.dispatchMouseEvent"
	mInputKey              = "Input.dispatchKeyEvent"
	mInputText             = "Input.insertText"
	mBrowserWindowFor      = "Browser.getWindowForTarget"
	mBrowserWindowSet      = "Browser.setWindowBounds"
	mEmulationMetrics      = "Emulation.setDeviceMetricsOverride"
)

// Target is one Chrome tab (or the browser-level target we navigate first).
type Target struct {
	ID       string `json:"targetId"`
	Type     string `json:"type"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	Attached bool   `json:"attached"`
}

// Targets lists every live target Chrome knows about.
func (c *Client) Targets(ctx context.Context) ([]Target, error) {
	var out struct {
		TargetInfos []Target `json:"targetInfos"`
	}
	if err := c.CallInto(ctx, "", mTargetGetTargets, nil, &out); err != nil {
		return nil, err
	}
	return out.TargetInfos, nil
}

// CreateTarget opens a tab at url. about:blank is used when url is empty so the
// caller can navigate after the session is ready.
func (c *Client) CreateTarget(ctx context.Context, url string) (Target, error) {
	if strings.TrimSpace(url) == "" {
		url = "about:blank"
	}
	var out Target
	if err := c.CallInto(ctx, "", mTargetCreate, map[string]any{"url": url, "background": false}, &out); err != nil {
		return Target{}, err
	}
	if out.ID == "" {
		return Target{}, errors.New("browser: Chrome created a target without an id")
	}
	out.URL = url
	return out, nil
}

// CloseTarget shuts a tab. Failing to close is not fatal — the caller is done
// with the tab either way.
func (c *Client) CloseTarget(ctx context.Context, targetID string) error {
	_, err := c.Call(ctx, "", mTargetClose, map[string]any{"targetId": targetID})
	return err
}

// Session is a flattened connection to one target. Every command after this
// point carries the session id, and every event Chrome raises for that tab
// arrives on the same id.
type Session struct {
	client   *Client
	id       string
	targetID string

	// mainFrame is cached: Page.createIsolatedWorld and the AX tree both need it,
	// and the frame tree does not change under a single observation.
	mainFrame string
}

// Attach opens a session on a target and switches on the domains we listen to.
// Events are silent until a domain is enabled, so this is not optional setup.
func (c *Client) Attach(ctx context.Context, targetID string) (*Session, error) {
	var out struct {
		SessionID string `json:"sessionId"`
	}
	if err := c.CallInto(ctx, "", mTargetAttach, map[string]any{
		"targetId": targetID,
		"flatten":  true,
	}, &out); err != nil {
		return nil, err
	}
	if out.SessionID == "" {
		return nil, errors.New("browser: Chrome attached without a session id")
	}
	s := &Session{client: c, id: out.SessionID, targetID: targetID}
	for _, enable := range []string{mPageEnable, mRuntimeEnable, mDOMEnable, mAccessibilityEnable} {
		if _, err := s.Call(ctx, enable, nil); err != nil {
			c.stopSession(s.id)
			return nil, fmt.Errorf("browser: enabling the %s domain: %w", strings.SplitN(enable, ".", 2)[0], err)
		}
	}
	// Lifecycle milestones are opt-in, and without them a navigation has nothing
	// to wait for. Best-effort: an older Chrome that refuses still works, just
	// slower, because the snapshot falls back to polling.
	if _, err := s.Call(ctx, mPageLifecycleEnable, map[string]any{"enabled": true}); err != nil {
		log.Printf("browser: lifecycle events unavailable on this tab: %v", err)
	}
	return s, nil
}

// stopSession detaches during a failed setup. It cannot inherit the caller's
// context — that one already failed, which is why we are here.
func (c *Client) stopSession(sessionID string) {
	detachCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	_, _ = c.Call(detachCtx, "", mTargetDetach, map[string]any{"sessionId": sessionID})
}

// ID is the DevTools session id.
func (s *Session) ID() string { return s.id }

// TargetID is the tab this session is bound to.
func (s *Session) TargetID() string { return s.targetID }

// Call runs a command against this session.
func (s *Session) Call(ctx context.Context, method string, params map[string]any) (map[string]any, error) {
	return s.client.Call(ctx, s.id, method, params)
}

// CallInto decodes a session reply into out.
func (s *Session) CallInto(ctx context.Context, method string, params map[string]any, out any) error {
	res, err := s.Call(ctx, method, params)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(res)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

// On subscribes to an event raised for this tab.
func (s *Session) On(method string) (<-chan map[string]any, func()) {
	return s.client.On(s.id, method)
}

// Detach releases the session without touching the tab. The command goes to the
// browser connection with the id in its params — sending it *through* the session
// being closed is how Chrome answers "No session with given id".
func (s *Session) Detach(ctx context.Context) error {
	_, err := s.client.Call(ctx, "", mTargetDetach, map[string]any{"sessionId": s.id})
	return err
}

// Close closes the tab this session is attached to, then the session. The detach
// still runs when the close fails, so a stuck tab does not also leak a session.
func (s *Session) Close(ctx context.Context) error {
	closeErr := s.client.CloseTarget(ctx, s.targetID)
	detachCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	detachErr := s.Detach(detachCtx)
	if closeErr != nil {
		return closeErr
	}
	return detachErr
}

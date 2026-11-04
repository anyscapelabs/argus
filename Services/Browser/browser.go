package Browser

// The service holds one browser for the whole app, not one per run: a login the
// agent made five minutes ago should still be there, and a page it was reading
// should still be open. That only pays off if the session also ends on its own,
// so an idle timer closes Chrome when nobody has used it for a while and a
// shutdown takes it with the app — an orphaned browser process is the one thing
// here a user would notice.

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// idleClose is how long Chrome may sit unused before we shut it down. Long
	// enough to survive a conversation, short enough not to be a leak.
	idleClose = 15 * time.Minute
)

// Approver asks the user to allow a gated action. Chat supplies it, so this
// package never has to know how permissions are stored, displayed or remembered.
// The host is handed over because that is the thing a standing grant is scoped
// to: "always allow on this site", not "always allow the browser".
type Approver func(host, summary string) (allowed bool, reason string)

// Call is one tool invocation.
type Call struct {
	Action string `json:"action"`
	URL    string `json:"url,omitempty"`
	Text   string `json:"text,omitempty"`
	Ref    int    `json:"ref,omitempty"`
	Value  string `json:"value,omitempty"`
	Key    string `json:"key,omitempty"`
	// Target is a scroll destination ("top"/"bottom") or a tab number, depending
	// on the action.
	Target string `json:"target,omitempty"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	MS     int    `json:"ms,omitempty"`
	Clear  bool   `json:"clear,omitempty"`
	// Observe turns the returned snapshot off for agents that already know what
	// they need.
	Observe *bool `json:"observe,omitempty"`
}

// wantsSnapshot defaults to yes: the common failure of a browser agent is acting
// blind, so seeing the page is the default and flying without it is opt-in.
func (c Call) wantsSnapshot() bool { return c.Observe == nil || *c.Observe }

// Result is what the agent reads.
type Result struct {
	OK    bool
	Error string
	Note  string
	Tree  string
	Body  string
	Host  string
	Title string
	Tab   int
	// Challenge is the human check on the page, named by the page itself: the
	// iframe or script that gives the vendor away, the interstitial's own heading.
	// Empty means the snapshot found nothing of the kind. Chat turns a non-empty
	// list into a takeover card, so this is the difference between the agent
	// waiting for a user who was never asked and the user being asked.
	Challenge []string
	Elapsed   time.Duration
}

// Render flattens a result into the single text block the tool returns.
func (r Result) Render() string {
	var b strings.Builder
	if r.Error != "" {
		b.WriteString(strings.TrimSpace(r.Error))
		if r.Tree != "" {
			b.WriteString("\n\nthe page is now:\n")
			b.WriteString(r.Tree)
		}
		return b.String()
	}
	if r.Note != "" {
		b.WriteString(r.Note)
		b.WriteString("\n")
	}
	if r.Body != "" {
		b.WriteString(r.Body)
		if !strings.HasSuffix(r.Body, "\n") {
			b.WriteString("\n")
		}
	}
	if r.Tree != "" {
		if b.Len() > 0 && r.Body == "" {
			b.WriteString("\n")
		}
		b.WriteString(r.Tree)
	}
	return strings.TrimRight(b.String(), "\n")
}

// Service owns the browser session.
//
// It is deliberately not a Wails-bound service: Run takes a cancellable context
// and an approval callback, neither of which belongs in a generated binding, and
// nothing in the UI calls it today — the agent does. Chat drives its lifecycle and
// receives its events through the Core the app already shares.
type Service struct {
	mu       sync.Mutex
	instance *instance
	pages    map[int]*Page
	order    []int
	active   int
	nextTab  int
	idle     *time.Timer
	closed   bool

	// paused means the user is driving: the agent's tool calls are refused
	// (not queued) so nobody fights a cursor, while Chrome itself stays up.
	paused atomic.Bool

	// screenshotDir is where the screenshot verb writes; empty falls back to the
	// user cache dir inside the action itself.
	screenshotDir string
}

// Start prepares the service. Called by Chat at startup; it opens nothing — a
// browser appears when the agent first asks for one, and never before.
func (s *Service) Start(screenshotDir string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pages = map[int]*Page{}
	s.screenshotDir = screenshotDir
	s.closed = false
	return nil
}

// Shutdown closes Chrome with the app.
func (s *Service) Shutdown() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopLocked()
	return nil
}

// Available reports whether this machine can browse, and why not when it cannot.
func (s *Service) Available() (bool, string) { return Available() }

// Status is a one-line description of the running browser for anyone who asks.
// It reads the last page we looked at; it never triggers a capture, because a
// status call must not be able to move the agent's cursor.
func (s *Service) Status() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.instance == nil || !s.instance.running() {
		return "not running"
	}
	page := s.activePageLocked()
	if page == nil {
		return s.instance.describe()
	}
	snap := page.refs.snapshot
	if snap == nil {
		return fmt.Sprintf("%s · tab %d", s.instance.describe(), page.Tab())
	}
	return fmt.Sprintf("%s · tab %d · %s %s", s.instance.describe(), page.Tab(),
		hostOf(snap.URL), truncate(collapseSpace(snap.Title), 60))
}

// stopLocked tears the session down. Callers hold s.mu.
func (s *Service) stopLocked() {
	if s.idle != nil {
		s.idle.Stop()
		s.idle = nil
	}
	if s.instance != nil {
		s.instance.kill()
		s.instance = nil
	}
	s.pages = map[int]*Page{}
	s.order = nil
	s.active = 0
}

// touchLocked defers the idle shutdown by one more window.
func (s *Service) touchLocked() {
	if s.idle == nil {
		s.idle = time.AfterFunc(idleClose, func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.stopLocked()
		})
		return
	}
	s.idle.Reset(idleClose)
}

// session returns a live session, starting Chrome on first use. A session the
// user closed by hand — a dead process, or a DevTools connection whose
// websocket has already died — is torn down and rebuilt on the spot, rather
// than failing for the rest of the run.
func (s *Service) session(ctx context.Context) (*instance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("browser: the app is shutting down")
	}
	if s.instance != nil && s.instance.running() && s.instance.client.alive() {
		s.touchLocked()
		return s.instance, nil
	}
	if s.instance != nil {
		// Chrome went away — the user closed the window, or it crashed. Start
		// again rather than failing for the rest of the run.
		s.stopLocked()
	}
	inst, err := start(ctx, Headless())
	if err != nil {
		return nil, err
	}
	s.instance = inst
	s.pages = map[int]*Page{}
	s.order = nil
	s.nextTab = 0
	s.touchLocked()
	return inst, nil
}

// pageFor returns the tab the agent means: the active one unless target names
// another.
func (s *Service) pageFor(ctx context.Context, target string) (*Page, error) {
	inst, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	if target != "" {
		n, err := parseTabNumber(target)
		if err != nil {
			return nil, err
		}
		s.mu.Lock()
		page, ok := s.pages[n]
		s.mu.Unlock()
		if !ok {
			return nil, fmt.Errorf("browser: there is no tab %d — tabs() lists what is open", n)
		}
		return page, nil
	}
	s.mu.Lock()
	page := s.activePageLocked()
	s.mu.Unlock()
	if page != nil {
		return page, nil
	}
	return s.newTab(ctx, inst, initialURL)
}

func (s *Service) activePageLocked() *Page {
	if s.active != 0 {
		if page, ok := s.pages[s.active]; ok {
			return page
		}
	}
	if len(s.order) > 0 {
		return s.pages[s.order[len(s.order)-1]]
	}
	return nil
}

// newTab opens a tab, attaches a session, and makes it current.
func (s *Service) newTab(ctx context.Context, inst *instance, url string) (*Page, error) {
	target, err := inst.client.CreateTarget(ctx, url)
	if err != nil {
		return nil, err
	}
	session, err := inst.client.Attach(ctx, target.ID)
	if err != nil {
		_ = inst.client.CloseTarget(ctx, target.ID)
		return nil, err
	}
	s.mu.Lock()
	s.nextTab++
	number := s.nextTab
	page := newPage(session, number)
	s.pages[number] = page
	s.order = append(s.order, number)
	s.active = number
	s.mu.Unlock()
	return page, nil
}

// tabs lists what is open, in the order the agent opened them.
func (s *Service) tabs(ctx context.Context) ([]string, error) {
	s.mu.Lock()
	pages := make([]*Page, 0, len(s.order))
	for _, n := range s.order {
		if p, ok := s.pages[n]; ok {
			pages = append(pages, p)
		}
	}
	active := s.active
	s.mu.Unlock()

	if len(pages) == 0 {
		return nil, nil
	}
	lines := make([]string, 0, len(pages))
	for _, page := range pages {
		snap, err := page.current(ctx)
		if err != nil {
			lines = append(lines, fmt.Sprintf("[%d] (unreadable)", page.Tab()))
			continue
		}
		marker := " "
		if page.Tab() == active {
			marker = "*"
		}
		lines = append(lines, fmt.Sprintf("%s[%d] %s · %d actionable", marker, page.Tab(),
			truncate(collapseSpace(snap.Title), 60), len(snap.Elements)))
	}
	return lines, nil
}

// closeTab shuts one tab, keeping at least one open.
func (s *Service) closeTab(ctx context.Context, target string) error {
	s.mu.Lock()
	number, err := parseTabNumber(target)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	page, ok := s.pages[number]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("browser: no tab %d", number)
	}
	if len(s.pages) == 1 {
		s.mu.Unlock()
		return errors.New("browser: that is the only tab — close the session with reset instead")
	}
	delete(s.pages, number)
	for i, n := range s.order {
		if n == number {
			s.order = append(s.order[:i], s.order[i+1:]...)
			break
		}
	}
	if s.active == number {
		s.active = s.order[len(s.order)-1]
	}
	inst := s.instance
	s.mu.Unlock()

	if err := page.session.Detach(ctx); err != nil {
		return err
	}
	if inst != nil {
		return inst.client.CloseTarget(ctx, page.TargetID())
	}
	return nil
}

// setWindow shows or hides the browser window, which is how a headed session can
// sit out of the user's way without being closed.
func (s *Service) setWindow(ctx context.Context, show bool) error {
	inst, err := s.session(ctx)
	if err != nil {
		return err
	}
	page, err := s.pageFor(ctx, "")
	if err != nil {
		return err
	}
	var out struct {
		WindowID int `json:"windowId"`
	}
	if err := inst.client.CallInto(ctx, "", mBrowserWindowFor, map[string]any{
		"targetId": page.TargetID(),
	}, &out); err != nil {
		return err
	}
	state := "minimized"
	if show {
		state = "normal"
	}
	_, err = inst.client.Call(ctx, "", mBrowserWindowSet, map[string]any{
		"windowId": out.WindowID,
		"bounds":   map[string]any{"windowState": state},
	})
	return err
}

// reset closes the browser and wipes the agent's profile. The agent reaches this
// when a stale cookie or a broken sign-in is the problem; it is not destructive
// to the user's own data because this profile was never theirs.
func (s *Service) reset(ctx context.Context) (string, error) {
	s.mu.Lock()
	if s.instance != nil {
		s.stopLocked()
	}
	s.mu.Unlock()
	if err := removeProfile(); err != nil {
		return "", err
	}
	return "closed the browser and cleared its profile; the next open starts signed out everywhere", nil
}

// Run executes one tool call. It is the only entry point Chat needs.
//
// A failure whose cause is a dead browser — connection closed, stale CDP
// session — gets one recovery: the dead session is torn down and, only if that
// actually replaced a dead instance, the call is retried on the fresh session.
// The retry cannot double-execute anything: a call that failed on a dead
// transport provably did not reach the browser.
//
// While the user is driving, calls are refused without touching Chrome: the
// takeover flow puts the page in human hands exactly so the agent stops
// reaching for it.
func (s *Service) Run(ctx context.Context, call Call, approve Approver) Result {
	if s.paused.Load() {
		return Result{
			Error: "browser: the user is driving the browser right now — the page can change under you at any moment." +
				" Stand by without acting; the next turn will tell you when they have handed it back",
			Elapsed: 0,
		}
	}
	res := s.run(ctx, call, approve)
	if res.Error != "" && deadTransportMessage(res.Error) {
		if s.discardDeadSession() {
			res = s.run(ctx, call, approve)
		}
	}
	return res
}

// TakenOver reports whether the user is currently driving the browser.
func (s *Service) TakenOver() bool { return s.paused.Load() }

// TakeOver hands the browser to the user: tool calls stop touching Chrome
// and the window is brought forward so there is actually something to drive.
// A page that was sitting minimized becomes visible here — that is the point.
func (s *Service) TakeOver(ctx context.Context) error {
	if Headless() {
		return errors.New("browser: this session runs a headless browser, so there is no window for the user to take over")
	}
	s.paused.Store(true)
	s.mu.Lock()
	live := s.instance != nil && s.instance.running() && s.instance.client.alive()
	s.mu.Unlock()
	if !live {
		// A takeover with no browser running still needs one — the user is the
		// one about to use it, and a fresh dedicated Chrome is what we can give.
		if _, err := s.session(ctx); err != nil {
			return err
		}
	}
	// A failure to surface the window is worth reporting but must not undo the
	// handover: Chrome is probably visible already.
	return s.setWindow(ctx, true)
}

// HandBack gives control to the agent again.
func (s *Service) HandBack() { s.paused.Store(false) }

// CaptureForUI returns what a takeover card needs to show the user: the
// current viewport as a PNG data URL, plus where that viewport is. It reads
// only — like Status, it must never be able to move the cursor.
func (s *Service) CaptureForUI(ctx context.Context) (dataURL, host, title string, err error) {
	s.mu.Lock()
	page := s.activePageLocked()
	live := s.instance != nil && s.instance.running() && s.instance.client.alive()
	s.mu.Unlock()
	if !live {
		return "", "", "", errors.New("browser: nothing is open")
	}
	if page == nil {
		return "", "", "", errors.New("browser: there is no page to capture")
	}
	if snap, err := page.current(ctx); err == nil {
		host = hostOf(snap.URL)
		title = collapseSpace(snap.Title)
	}
	raw, err := page.screenshotPNG(ctx)
	if err != nil {
		return "", host, title, err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw), host, title, nil
}

// deadTransportMessage matches the rendered error strings a dead browser
// leaves in a Result. The typed check lives in cdp.go's isDeadTransport; this
// one works post-render, where the wrapper retries.
func deadTransportMessage(msg string) bool {
	low := strings.ToLower(msg)
	return strings.Contains(low, "devtools connection closed") ||
		strings.Contains(low, "no target with given id") ||
		strings.Contains(low, "session with given id not found") ||
		strings.Contains(low, "target closed") ||
		strings.Contains(low, "inspector detached")
}

// discardDeadSession tears the session down when the browser is confirmed
// gone, reporting whether a dead instance was actually replaced — the only
// state in which a retried call is safe.
func (s *Service) discardDeadSession() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.instance == nil {
		return false
	}
	if s.instance.running() && s.instance.client.alive() {
		return false
	}
	s.stopLocked()
	return true
}

func (s *Service) run(ctx context.Context, call Call, approve Approver) Result {
	start := time.Now()
	action := strings.ToLower(strings.TrimSpace(call.Action))
	if action == "" {
		return Result{Error: "browser: no action given", Elapsed: time.Since(start)}
	}

	// Session-level verbs first: they do not need a page.
	switch action {
	case "reset":
		note, err := s.reset(ctx)
		if err != nil {
			return Result{Error: err.Error(), Elapsed: time.Since(start)}
		}
		return Result{OK: true, Note: note, Elapsed: time.Since(start)}
	case "hide", "show":
		if err := s.setWindow(ctx, action == "show"); err != nil {
			return Result{Error: err.Error(), Elapsed: time.Since(start)}
		}
		return Result{OK: true, Note: "the browser window is " + map[bool]string{true: "visible", false: "minimized"}[action == "show"], Elapsed: time.Since(start)}
	case "tab_new":
		page, err := s.newTabHere(ctx, call.URL)
		if err != nil {
			return Result{Error: err.Error(), Elapsed: time.Since(start)}
		}
		return s.finish(ctx, page, "opened a new tab", call, start)
	}

	page, err := s.pageFor(ctx, call.Target)
	if err != nil {
		return Result{Error: err.Error(), Elapsed: time.Since(start)}
	}

	switch action {
	case "open":
		targetURL := strings.TrimSpace(call.URL)
		if targetURL == "" {
			return Result{Error: "browser: open needs a url", Elapsed: time.Since(start)}
		}
		if err := page.Open(ctx, targetURL); err != nil {
			return Result{Error: err.Error(), Elapsed: time.Since(start)}
		}
		return s.finish(ctx, page, "opened", call, start)

	case "back":
		if err := page.Back(ctx); err != nil {
			return Result{Error: err.Error(), Elapsed: time.Since(start)}
		}
		return s.finish(ctx, page, "went back", call, start)

	case "read":
		snap, err := s.captureStable(ctx, page)
		if err != nil {
			return Result{Error: err.Error(), Elapsed: time.Since(start)}
		}
		return s.snapshotResult(snap.Window(call.Offset, call.Limit), "read the page", start)

	case "find":
		needle := strings.TrimSpace(call.Text)
		if needle == "" {
			return Result{Error: "browser: find needs text to look for", Elapsed: time.Since(start)}
		}
		snap, err := s.captureStable(ctx, page)
		if err != nil {
			return Result{Error: err.Error(), Elapsed: time.Since(start)}
		}
		filtered := snap.Filter(strings.Fields(needle)...)
		note := fmt.Sprintf("%d of %d elements match %q", len(filtered.Elements), len(snap.Elements), needle)
		if len(filtered.Elements) == 0 {
			note += " — nothing here matches; try a shorter word, or read(offset=…) to look through the page"
		}
		return s.snapshotResult(filtered, note, start)

	case "text":
		body, err := page.Text(ctx)
		if err != nil {
			return Result{Error: err.Error(), Elapsed: time.Since(start)}
		}
		if strings.TrimSpace(body) == "" {
			body = "(no readable text found — this page may be an app rather than a document)"
		}
		return Result{OK: true, Body: body, Tab: page.Tab(), Elapsed: time.Since(start)}

	case "click", "fill", "press", "select":
		return s.mutate(ctx, page, action, call, approve, start)

	case "scroll":
		if call.Target == "top" || call.Target == "bottom" {
			if err := page.ScrollTo(ctx, call.Target); err != nil {
				return Result{Error: err.Error(), Elapsed: time.Since(start)}
			}
			return s.finish(ctx, page, "scrolled to "+call.Target, call, start)
		}
		direction := call.Value
		if direction == "" {
			direction = call.Text
		}
		if err := page.Scroll(ctx, direction, call.Ref); err != nil {
			return Result{Error: err.Error(), Elapsed: time.Since(start)}
		}
		return s.finish(ctx, page, "scrolled "+orDefault(direction, "down"), call, start)

	case "wait":
		settled, err := page.Wait(ctx, call.MS)
		if err != nil {
			return Result{Error: err.Error(), Elapsed: time.Since(start)}
		}
		note := "the page is quiet"
		if !settled {
			note = "the page was still changing after the wait — it may be loading slowly or streaming"
		}
		return s.finish(ctx, page, note, call, start)

	case "screenshot":
		path, err := page.Screenshot(ctx, s.screenshotDir)
		if err != nil {
			return Result{Error: err.Error(), Elapsed: time.Since(start)}
		}
		return Result{OK: true, Note: "saved a screenshot to " + path + " (the image was not sent to you; the tree above describes the page)", Elapsed: time.Since(start)}

	case "js":
		// An arbitrary script can do anything a click can do, so it is judged on
		// where it runs and what it names rather than waved through as a read.
		host := page.Host(ctx)
		if required, why := ScriptRisk(host, call.Value); required {
			if approve == nil {
				return Result{Error: fmt.Sprintf("browser: %s needs the user's approval and nobody was there to ask", why), Elapsed: time.Since(start)}
			}
			if allowed, reason := approve(host, why); !allowed {
				return Result{Error: fmt.Sprintf("the user declined: %s. Do not retry this action or look for another way to do it",
					orDefault(reason, why)), Elapsed: time.Since(start)}
			}
		}
		out, err := page.EvalJS(ctx, call.Value)
		if err != nil {
			return Result{Error: err.Error(), Elapsed: time.Since(start)}
		}
		if strings.TrimSpace(out) == "" {
			out = "(the script returned nothing)"
		}
		return Result{OK: true, Body: out, Elapsed: time.Since(start)}

	case "tabs":
		lines, err := s.tabs(ctx)
		if err != nil {
			return Result{Error: err.Error(), Elapsed: time.Since(start)}
		}
		if len(lines) == 0 {
			return Result{OK: true, Note: "no tabs are open", Elapsed: time.Since(start)}
		}
		return Result{OK: true, Body: "* marks the tab you are on:\n" + strings.Join(lines, "\n"), Elapsed: time.Since(start)}

	case "tab_select":
		number, err := parseTabNumber(call.Target)
		if err != nil {
			return Result{Error: err.Error(), Elapsed: time.Since(start)}
		}
		s.mu.Lock()
		_, ok := s.pages[number]
		if ok {
			s.active = number
		}
		s.mu.Unlock()
		if !ok {
			return Result{Error: fmt.Sprintf("browser: no tab %d", number), Elapsed: time.Since(start)}
		}
		page, err := s.pageFor(ctx, "")
		if err != nil {
			return Result{Error: err.Error(), Elapsed: time.Since(start)}
		}
		return s.finish(ctx, page, fmt.Sprintf("switched to tab %d", number), call, start)

	case "tab_close":
		if err := s.closeTab(ctx, call.Target); err != nil {
			return Result{Error: err.Error(), Elapsed: time.Since(start)}
		}
		remaining, _ := s.tabs(ctx)
		return Result{OK: true, Note: "closed the tab", Body: strings.Join(remaining, "\n"), Elapsed: time.Since(start)}

	default:
		return Result{Error: fmt.Sprintf("browser: %q is not an action I have — try open, read, find, text, click, fill, press, select, scroll, wait, back, tabs, tab_new, tab_select, tab_close, screenshot, js, hide, show, reset", action), Elapsed: time.Since(start)}
	}
}

// mutate is the gated path: resolve the target, ask if it looks irreversible,
// then act, then show the result. The approval check happens before any event is
// dispatched, so a refusal means nothing happened at all.
func (s *Service) mutate(ctx context.Context, page *Page, action string, call Call, approve Approver, start time.Time) Result {
	if call.Ref <= 0 && action != "press" {
		return Result{Error: fmt.Sprintf("browser: %s needs a ref from the snapshot", action), Elapsed: time.Since(start)}
	}
	var target *Element
	host := ""
	if call.Ref > 0 {
		snap, err := page.current(ctx)
		if err != nil {
			return Result{Error: err.Error(), Elapsed: time.Since(start)}
		}
		host = hostOf(snap.URL)
		if e, ok := page.elementAt(call.Ref); ok {
			target = e
		}
	}

	if required, summary := NeedsApproval(action, host, target); required {
		if approve == nil {
			return Result{Error: fmt.Sprintf("browser: %s needs the user's approval and nobody was there to ask", summary), Elapsed: time.Since(start)}
		}
		allowed, reason := approve(host, summary)
		if !allowed {
			note := fmt.Sprintf("the user declined: %s. Do not retry this action or look for another way to do it", orDefault(reason, summary))
			return Result{Error: note, Elapsed: time.Since(start)}
		}
	}

	switch action {
	case "click":
		wasDisabled := target != nil && target.State.Disabled
		if err := page.Click(ctx, call.Ref); err != nil {
			return s.actionFailure(ctx, page, err, call.Ref, start)
		}
		if wasDisabled {
			return s.finish(ctx, page, fmt.Sprintf("clicked [%s] although it reported itself disabled — the control may need filling first", labelOf(page, call.Ref)), call, start)
		}
		return s.finish(ctx, page, fmt.Sprintf("clicked [%s]", labelOf(page, call.Ref)), call, start)
	case "fill":
		if err := page.Fill(ctx, call.Ref, call.Value, call.Clear); err != nil {
			return s.actionFailure(ctx, page, err, call.Ref, start)
		}
		return s.finish(ctx, page, fmt.Sprintf("typed into [%s]", labelOf(page, call.Ref)), call, start)
	case "select":
		if err := page.Select(ctx, call.Ref, call.Value); err != nil {
			return s.actionFailure(ctx, page, err, call.Ref, start)
		}
		return s.finish(ctx, page, fmt.Sprintf("selected %q at [%d]", call.Value, call.Ref), call, start)
	case "press":
		key := call.Key
		if key == "" {
			key = call.Value
		}
		if key == "" {
			return Result{Error: "browser: press needs a key, e.g. enter, tab, escape or ctrl+a", Elapsed: time.Since(start)}
		}
		if err := page.Press(ctx, key); err != nil {
			return s.actionFailure(ctx, page, err, call.Ref, start)
		}
		return s.finish(ctx, page, "pressed "+key, call, start)
	}
	return Result{Error: "browser: unreachable", Elapsed: time.Since(start)}
}

// actionFailure turns a stale or missing element into the useful answer: an
// explanation plus the page as it is now, so the agent re-aims in one step.
func (s *Service) actionFailure(ctx context.Context, page *Page, err error, ref int, start time.Time) Result {
	if isStaleRef(err) || errors.Is(err, errHandleLost) {
		snap, capErr := page.capture(ctx)
		note := fmt.Sprintf("[%d] is no longer where it was — %s", ref, err)
		if capErr != nil {
			return Result{Error: note, Elapsed: time.Since(start)}
		}
		return Result{
			OK: false, Error: note + "\n\nthe page is now:", Tab: page.Tab(),
			Tree: snap.Render(), Challenge: snap.Challenge, Elapsed: time.Since(start),
		}
	}
	return Result{Error: err.Error(), Elapsed: time.Since(start)}
}

// finish captures the page after an action and packages it for the agent.
//
// An action can start a navigation, and a page in flight looks exactly like a
// page with nothing on it: same URL, empty body, zero controls. So the read after
// an interaction waits for the document to stop changing before it snapshots, and
// reads again if what it got was empty. The alternative is telling the agent the
// page it just clicked is blank, which it will not believe and should not have to.
func (s *Service) finish(ctx context.Context, page *Page, note string, call Call, start time.Time) Result {
	if !call.wantsSnapshot() {
		return Result{OK: true, Note: note, Tab: page.Tab(), Elapsed: time.Since(start)}
	}
	if mutatingVerbs[strings.ToLower(strings.TrimSpace(call.Action))] {
		// A click can start a navigation that has not begun by the time DevTools
		// acknowledges the mouse-up. Waiting for the document to stop changing is
		// the only honest way to report what the click did.
		if _, err := page.Wait(ctx, 0); err != nil {
			return Result{OK: true, Note: note + " (the page never went quiet: " + err.Error() + ")",
				Tab: page.Tab(), Elapsed: time.Since(start)}
		}
	}
	snap, err := s.captureStable(ctx, page)
	if err != nil {
		return Result{OK: true, Note: note + " (but the page could not be read again: " + err.Error() + ")", Elapsed: time.Since(start)}
	}
	out := s.snapshotResult(snap, note, start)
	out.Tab = page.Tab()
	return out
}

// captureStable reads a page, retrying while it looks empty or still growing.
//
// An empty tree is indistinguishable from a client-rendered site a second before
// it builds its DOM, and so is a page shell: Amazon paints the header nav, then
// fetches the price and the buy box a beat later. A click used to report that
// moment as the finished page — 15 controls, all of them navigation — and an
// agent told a product page has no purchase control writes the user instructions
// instead of buying the keyboard.
//
// So a read is believed once the element count has stopped moving, and a read that
// looks like a shell is held one rung longer even when it looks quiet, because
// quiet and not-yet-built are the same picture. The waits grow, not the attempt
// count. If the ladder runs out while the count is still moving the snapshot is
// marked Growing, and Render says so out loud — the alternative is handing a
// half-built page to the agent as if it were the whole story.
func (s *Service) captureStable(ctx context.Context, page *Page) (*Snapshot, error) {
	ladder := []time.Duration{0, 300 * time.Millisecond, 700 * time.Millisecond,
		1500 * time.Millisecond, 3000 * time.Millisecond, 3000 * time.Millisecond}
	var snap *Snapshot
	var err error
	prev, cur := -1, 0
	for i, pause := range ladder {
		if i > 0 {
			select {
			case <-time.After(pause):
			case <-ctx.Done():
				if snap == nil {
					return nil, ctx.Err()
				}
				return snap, nil
			}
			// A navigation that started after the pause is worth waiting out
			// before reading again.
			if _, werr := page.Wait(ctx, 0); werr != nil {
				return nil, werr
			}
		}
		snap, err = page.capture(ctx)
		if err != nil {
			return nil, err
		}
		cur = len(snap.Elements)
		if settledRead(prev, cur, snap, i) {
			return snap, nil
		}
		prev = cur
	}
	if snap != nil {
		snap.Growing = cur != prev
	}
	return snap, nil
}

// thinHoldRung is how long a suspected page shell is held before it is believed:
// about two and a half seconds — long enough for a lazy buy box, short enough that
// a genuine two-link page does not stall the run.
const thinHoldRung = 3

// settledRead decides whether a read may be believed. The count has to have
// stopped moving, and a tree that looks like a shell has to stop moving one rung
// longer, because a page that has not started building its content and a page that
// has finished look identical for one read. An empty tree is never settled; that
// case walks the whole ladder and keeps its own message.
func settledRead(prev, cur int, snap *Snapshot, rung int) bool {
	if cur == 0 || cur != prev {
		return false
	}
	return !snap.unfinished() || rung >= thinHoldRung
}

func (s *Service) snapshotResult(snap *Snapshot, note string, start time.Time) Result {
	return Result{
		OK: true, Note: note, Tree: snap.Render(),
		Host: hostOf(snap.URL), Title: collapseSpace(snap.Title),
		Challenge: snap.Challenge,
		Elapsed:   time.Since(start),
	}
}

// newTabHere opens a tab through the service, keeping the bookkeeping in one
// place.
func (s *Service) newTabHere(ctx context.Context, rawURL string) (*Page, error) {
	inst, err := s.session(ctx)
	if err != nil {
		return nil, err
	}
	target := strings.TrimSpace(rawURL)
	if target == "" {
		target = initialURL
	}
	if !strings.Contains(target, "://") {
		target = "https://" + target
	}
	return s.newTab(ctx, inst, target)
}

// labelOf is how an action refers to its target: the number the agent used plus
// the name it saw, so a status line reads back as an unambiguous sentence.
func labelOf(page *Page, ref int) string {
	if e, ok := page.elementAt(ref); ok {
		if name := e.displayName(); name != "" {
			return fmt.Sprintf("%d %s", ref, truncate(name, 40))
		}
	}
	return fmt.Sprintf("%d", ref)
}

func parseTabNumber(s string) (int, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	s = strings.TrimPrefix(s, "tab")
	s = strings.Trim(s, " #")
	if s == "" || s == "current" || s == "this" {
		return 0, errors.New("browser: which tab? give a number from tabs()")
	}
	var n int
	if _, err := fmt.Sscanf(s, "%d", &n); err != nil || n <= 0 {
		return 0, fmt.Errorf("browser: %q is not a tab number", strings.TrimSpace(s))
	}
	return n, nil
}

// hostOf is the part a user recognises, not the whole URL. A page with no host —
// a local file, a blank tab, a data: URI — is named by what it is, because
// "file:///home/brnx/…/Sim…" tells the user nothing when they are deciding
// whether to allow a click, and that string is also the grant key.
func hostOf(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return truncate(raw, 40)
	}
	if h := strings.TrimPrefix(strings.ToLower(u.Host), "www."); h != "" {
		return h
	}
	switch {
	case raw == "about:blank" || u.Scheme == "about":
		return "a blank page"
	case u.Scheme == "data":
		return "an inline page"
	case u.Path != "" && u.Path != "/":
		name := strings.TrimRight(u.Path, "/")
		if i := strings.LastIndex(name, "/"); i >= 0 {
			name = name[i+1:]
		}
		if name == "" {
			name = u.Scheme + " page"
		}
		return truncate(name, 40)
	default:
		return truncate(raw, 40)
	}
}

// Close shuts the browser down. Wails calls ServiceShutdown; this is for tests.
func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.stopLocked()
	return nil
}

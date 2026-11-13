package Browser

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
)

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
	s.opMu.Lock()
	defer s.opMu.Unlock()
	if s.paused.Load() || s.blocked.Load() {
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

func (s *Service) BlockForTakeover() {
	s.blocked.Store(true)
	// Wait for an action that entered before blocked was set. Any queued Run
	// rechecks blocked after this barrier and is refused.
	s.opMu.Lock()
	s.opMu.Unlock()
}

func (s *Service) CancelTakeover() {
	if !s.paused.Load() {
		s.blocked.Store(false)
	}
}

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
			s.paused.Store(false)
			return err
		}
	}
	// A failure to surface the window is worth reporting but must not undo the
	// handover: Chrome is probably visible already.
	return s.setWindow(ctx, true)
}

// HandBack gives control to the agent again.
func (s *Service) HandBack() {
	s.paused.Store(false)
	s.blocked.Store(false)
}

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
	snap, err := page.current(ctx)
	if err != nil {
		return Result{Error: err.Error(), Elapsed: time.Since(start)}
	}
	host = hostOf(snap.URL)
	if call.Ref > 0 {
		if e, ok := page.elementAt(call.Ref); ok {
			target = e
		}
	}
	if action == "press" && call.Ref <= 0 {
		key := strings.ToLower(strings.TrimSpace(orDefault(call.Key, call.Value)))
		if key == "enter" || key == "return" || key == "space" {
			if approve == nil {
				return Result{Error: "browser: submitting from the focused control needs the user's approval and nobody was there to ask", Elapsed: time.Since(start)}
			}
			allowed, reason := approve(host, fmt.Sprintf("press %s on the focused control at %s; this may submit a form", key, orDefault(host, "the current page")))
			if !allowed {
				return Result{Error: "the user declined: " + orDefault(reason, "submit the focused form"), Elapsed: time.Since(start)}
			}
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

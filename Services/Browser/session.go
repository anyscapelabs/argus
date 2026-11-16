package Browser

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

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

package Browser

// The service holds one browser for the whole app, not one per run: a login the
// agent made five minutes ago should still be there, and a page it was reading
// should still be open. That only pays off if the session also ends on its own,
// so an idle timer closes Chrome when nobody has used it for a while and a
// shutdown takes it with the app — an orphaned browser process is the one thing
// here a user would notice.

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// idleClose is how long Chrome may sit unused before we shut it down. Long
	// enough to survive a conversation, short enough not to be a leak.
	idleClose = 15 * time.Minute
)

// Service owns the browser session.
//
// It is deliberately not a Wails-bound service: Run takes a cancellable context
// and an approval callback, neither of which belongs in a generated binding, and
// nothing in the UI calls it today — the agent does. Chat drives its lifecycle and
// receives its events through the Core the app already shares.
type Service struct {
	mu       sync.Mutex
	opMu     sync.Mutex
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
	// blocked covers the period between the handover card appearing and the
	// user deciding. The agent stops immediately, before the user takes control.
	blocked atomic.Bool

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

// Close shuts the browser down. Wails calls ServiceShutdown; this is for tests.
func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.stopLocked()
	return nil
}

// Package Updater exposes the desktop app's self-update flow to the settings
// UI: it reports the running version, checks GitHub Releases for a newer
// build, and installs it in place before relaunching.
//
// The heavy lifting — download, verification, atomic swap, relaunch — is the
// framework's: app.Updater owns it (see the updater package in Wails v3). This
// service adds the parts the app needs on top: the version the binary was
// built with, the auto-check schedule, and a small JSON shape the "This App"
// settings page renders without opening the framework's own update window
// (Config.Window is WindowNone; the page is the UI).
package Updater

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/updater"

	"github.com/anyscapelabs/argus/Services/Shared"
)

// Version is the running build's version, without the conventional "v" tag
// prefix. Release builds overwrite it from the git tag
// (-ldflags "-X github.com/anyscapelabs/argus/Services/Updater.Version=0.2.0");
// a development build reports the version in build/config.yml.
var Version = "0.1.0"

const (
	// checkOnStartupDelay lets boot settle (database, catalog, MCP) before the
	// first network call, so the app never waits on GitHub to be usable.
	checkOnStartupDelay = 10 * time.Second
	// checkInterval re-checks while the app stays open. Releases are rare;
	// six hours keeps the unauthenticated GitHub API budget tiny.
	checkInterval = 6 * time.Hour
	// checkTimeout bounds one Check round trip, including a slow network.
	checkTimeout = 30 * time.Second
	// installTimeout bounds download + verification of the release artifact.
	installTimeout = 15 * time.Minute
	// restartDelay gives the bound Install call time to return its result to
	// the page before the process is replaced by the relaunched binary.
	restartDelay = 750 * time.Millisecond
	// StateRestarting is the Install state reported once a verified payload is
	// staged and the app is about to relaunch into it.
	StateRestarting = "restarting"
	// StateUpToDate is the Install state reported when there is nothing newer
	// to install, so the caller can leave the page as it is.
	StateUpToDate = "up_to_date"
)

// Service is the bound updater surface. Updater is app.Updater, injected from
// main.go after the application exists; a headless build leaves it nil and
// still answers with the running version.
type Service struct {
	Core    *Shared.Core
	Updater *updater.Updater

	mu         sync.Mutex
	status     Status
	checking   bool
	installing bool
	cancel     context.CancelFunc
}

// Status is the "This App" page's state: the version running, what the last
// check found, and what the updater is doing right now.
type Status struct {
	Current   string `json:"current"`
	Latest    string `json:"latest,omitempty"`
	Available bool   `json:"available"`
	Name      string `json:"name,omitempty"`
	Notes     string `json:"notes,omitempty"`
	URL       string `json:"url,omitempty"`
	State     string `json:"state"`
	CheckedAt int64  `json:"checked_at,omitempty"`
}

// StatusResult wraps Status in the app's usual {status, error} envelope.
type StatusResult struct {
	Status Status              `json:"status"`
	Error  *Shared.ActionError `json:"error,omitempty"`
}

// UpdateResult reports the outcome of pressing Update. State is
// StateRestarting when a verified build was staged and the app is relaunching,
// and StateUpToDate when the check found nothing to install.
type UpdateResult struct {
	State  string              `json:"state,omitempty"`
	Status Status              `json:"status"`
	Error  *Shared.ActionError `json:"error,omitempty"`
}

func (s *Service) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	s.mu.Lock()
	s.status = Status{Current: normalizeVersion(Version), State: s.updaterState()}
	bg, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.mu.Unlock()
	if s.Updater != nil {
		go s.autoCheck(bg)
	}
	return nil
}

func (s *Service) ServiceShutdown() error {
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

// Status returns the last known update state without touching the network, so
// the page paints instantly with the running version while the background
// check (or a previous one) fills in "latest".
func (s *Service) Status() StatusResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.Current = normalizeVersion(Version)
	s.status.State = s.updaterState()
	return StatusResult{Status: s.status}
}

// Check asks the release feed for the newest build and reports whether it is
// newer than the running one. It is what the "Check for update" button calls;
// the background loop runs the same code on a timer.
func (s *Service) Check() StatusResult {
	return s.check(context.Background())
}

// Install downloads the release found by Check (running one first when the
// page has not checked yet), stages it for the swap, then hands the process
// over to the updater's relaunch helper. The bound call returns before the
// process is replaced so the page receives its result.
func (s *Service) Install() UpdateResult {
	if s.Updater == nil {
		return UpdateResult{Error: unavailableError()}
	}

	s.mu.Lock()
	available := s.status.Available
	s.mu.Unlock()
	if !available {
		res := s.check(context.Background())
		if res.Error != nil {
			return UpdateResult{Status: res.Status, Error: res.Error}
		}
		if !res.Status.Available {
			return UpdateResult{State: StateUpToDate, Status: res.Status}
		}
	}

	if !s.beginInstall() {
		return UpdateResult{State: StateRestarting, Status: s.snapshot()}
	}
	defer s.endInstall()

	ctx, cancel := context.WithTimeout(context.Background(), installTimeout)
	defer cancel()
	if err := s.Updater.DownloadAndInstall(ctx); err != nil {
		return UpdateResult{
			Status: s.snapshot(),
			Error:  Shared.NewError(Shared.CodeUpdate, err.Error()),
		}
	}

	status := s.snapshot()
	go func() {
		time.Sleep(restartDelay)
		if err := s.Updater.Restart(context.Background()); err != nil {
			log.Printf("updater: relaunch into the new build failed: %v", err)
		}
	}()
	return UpdateResult{State: StateRestarting, Status: status}
}

// autoCheck runs the silent update checker: one pass shortly after startup,
// then on a long interval. Finding a build emits updates:available so an open
// "This App" page can offer the Update button without the user pressing
// "Check for update" first.
func (s *Service) autoCheck(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(checkOnStartupDelay):
	}
	s.check(ctx)
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.check(ctx)
		}
	}
}

func (s *Service) check(parent context.Context) StatusResult {
	if s.Updater == nil {
		return StatusResult{Status: s.snapshot(), Error: unavailableError()}
	}
	if !s.beginCheck() {
		return StatusResult{Status: s.snapshot()}
	}
	defer s.endCheck()

	ctx, cancel := context.WithTimeout(parent, checkTimeout)
	defer cancel()

	rel, err := s.Updater.Check(ctx)
	status := s.recordCheck(rel)
	if err != nil {
		return StatusResult{Status: status, Error: Shared.NewError(Shared.CodeNetwork, err.Error())}
	}
	if rel != nil {
		s.Core.EmitEvent(Shared.EventUpdatesAvailable, status)
	}
	return StatusResult{Status: status}
}

// recordCheck folds a Check result into the cached status and returns a copy.
func (s *Service) recordCheck(rel *updater.Release) Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.Current = normalizeVersion(Version)
	s.status.State = s.updaterState()
	s.status.CheckedAt = time.Now().Unix()
	if rel == nil {
		s.status.Latest = ""
		s.status.Name = ""
		s.status.Notes = ""
		s.status.URL = ""
		s.status.Available = false
		return s.status
	}
	s.status.Latest = normalizeVersion(rel.Version)
	s.status.Name = rel.Name
	s.status.Notes = rel.Notes
	s.status.URL = releaseURL(rel)
	s.status.Available = true
	return s.status
}

func (s *Service) snapshot() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.status.Current = normalizeVersion(Version)
	s.status.State = s.updaterState()
	return s.status
}

// updaterState reads the framework's lifecycle phase. The caller holds mu.
func (s *Service) updaterState() string {
	if s.Updater == nil {
		return string(updater.StateUnconfigured)
	}
	return string(s.Updater.State())
}

func (s *Service) beginCheck() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.checking || s.installing {
		return false
	}
	s.checking = true
	return true
}

func (s *Service) endCheck() {
	s.mu.Lock()
	s.checking = false
	s.mu.Unlock()
}

func (s *Service) beginInstall() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.installing {
		return false
	}
	s.installing = true
	return true
}

func (s *Service) endInstall() {
	s.mu.Lock()
	s.installing = false
	s.mu.Unlock()
}

func unavailableError() *Shared.ActionError {
	return Shared.NewError(Shared.CodeNotConfigured, "Updates are not available in this build.")
}

// releaseURL returns the release's page on GitHub, when the provider recorded
// it, so the page has somewhere to link for details.
func releaseURL(rel *updater.Release) string {
	if rel == nil || rel.Metadata == nil {
		return ""
	}
	url, _ := rel.Metadata["github.release.htmlURL"].(string)
	return url
}

// normalizeVersion renders a version the way the UI shows it: no leading "v".
func normalizeVersion(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "v")
}

package Updater

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/updater"

	"github.com/anyscapelabs/argus/Services/Shared"
)

// stubHost satisfies updater.Host without a running application. Only Emit is
// exercised by Check; the rest exist so the Updater can be constructed.
type stubHost struct {
	mu     sync.Mutex
	events []string
}

func (h *stubHost) Emit(name string, data ...any) bool {
	h.mu.Lock()
	h.events = append(h.events, name)
	h.mu.Unlock()
	return true
}

func (h *stubHost) OnEvent(string, func(any)) func() { return func() {} }

func (h *stubHost) OpenWindow(updater.WindowOptions) updater.WindowHandle { return nil }

func (h *stubHost) Quit() {}

// stubProvider returns whatever the test configured, standing in for the
// GitHub provider so the check flow runs without a network. A test mutates rel
// between checks to stand in for the feed moving on.
type stubProvider struct {
	mu  sync.Mutex
	rel *updater.Release
	err error
}

func (p *stubProvider) Name() string { return "stub" }

func (p *stubProvider) Check(context.Context, updater.CheckRequest) (*updater.Release, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.rel, p.err
}

func (p *stubProvider) Download(context.Context, *updater.Release, io.Writer, func(int64, int64)) error {
	return nil
}

// newTestService builds a Service on a real framework Updater backed by the
// stub provider. currentVersion is what the build reports.
func newTestService(t *testing.T, currentVersion string, p updater.Provider) (*Service, *Shared.Core) {
	t.Helper()
	previous := Version
	Version = currentVersion
	t.Cleanup(func() { Version = previous })

	u := updater.New(&stubHost{})
	if err := u.Init(updater.Config{
		CurrentVersion: currentVersion,
		Providers:      []updater.Provider{p},
		Window:         updater.WindowNone,
	}); err != nil {
		t.Fatalf("updater.Init: %v", err)
	}
	core := Shared.NewCore()
	return &Service{Core: core, Updater: u}, core
}

func TestStatusReportsTheRunningVersionBeforeAnyCheck(t *testing.T) {
	svc, _ := newTestService(t, "v0.1.0", &stubProvider{})

	res := svc.Status()
	if res.Error != nil {
		t.Fatalf("status error: %v", res.Error)
	}
	if res.Status.Current != "0.1.0" {
		t.Fatalf("current = %q, want 0.1.0", res.Status.Current)
	}
	if res.Status.Available {
		t.Fatal("available = true before any check")
	}
	if res.Status.State != string(updater.StateIdle) {
		t.Fatalf("state = %q, want %q", res.Status.State, updater.StateIdle)
	}
}

func TestCheckReportsANewerRelease(t *testing.T) {
	rel := &updater.Release{
		Version: "0.2.0",
		Name:    "Argus 0.2.0",
		Notes:   "Faster startup.",
		Metadata: map[string]any{
			"github.release.htmlURL": "https://github.com/anyscapelabs/argus/releases/tag/v0.2.0",
		},
	}
	svc, core := newTestService(t, "0.1.0", &stubProvider{rel: rel})

	var seen []string
	core.OnEvent(func(name string, data any) {
		seen = append(seen, name)
	})

	res := svc.Check()
	if res.Error != nil {
		t.Fatalf("check error: %v", res.Error)
	}
	if !res.Status.Available {
		t.Fatal("available = false, want true")
	}
	if res.Status.Latest != "0.2.0" {
		t.Fatalf("latest = %q, want 0.2.0", res.Status.Latest)
	}
	if res.Status.Notes != "Faster startup." {
		t.Fatalf("notes = %q", res.Status.Notes)
	}
	if res.Status.URL != "https://github.com/anyscapelabs/argus/releases/tag/v0.2.0" {
		t.Fatalf("url = %q", res.Status.URL)
	}
	if res.Status.CheckedAt == 0 {
		t.Fatal("checked_at was not stamped")
	}
	if len(seen) != 1 || seen[0] != Shared.EventUpdatesAvailable {
		t.Fatalf("events = %v, want [%s]", seen, Shared.EventUpdatesAvailable)
	}

	// Status must remember the result so the page paints it again on reopen.
	if got := svc.Status(); !got.Status.Available || got.Status.Latest != "0.2.0" {
		t.Fatalf("status after check = %+v", got.Status)
	}
}

func TestCheckClearsAPreviousUpdateWhenUpToDate(t *testing.T) {
	provider := &stubProvider{rel: &updater.Release{Version: "0.2.0"}}
	svc, _ := newTestService(t, "0.1.0", provider)
	if res := svc.Check(); !res.Status.Available {
		t.Fatal("first check did not report the update")
	}

	// The feed now says nothing is newer (provider returns nil).
	provider.mu.Lock()
	provider.rel = nil
	provider.mu.Unlock()

	res := svc.Check()
	if res.Error != nil {
		t.Fatalf("check error: %v", res.Error)
	}
	if res.Status.Available || res.Status.Latest != "" {
		t.Fatalf("status = %+v, want up to date", res.Status)
	}
}

func TestCheckSurfacesAFeedError(t *testing.T) {
	svc, _ := newTestService(t, "0.1.0", &stubProvider{err: errors.New("boom")})

	res := svc.Check()
	if res.Error == nil {
		t.Fatal("expected an error result")
	}
	if res.Error.Code != string(Shared.CodeNetwork) {
		t.Fatalf("error code = %q, want %q", res.Error.Code, Shared.CodeNetwork)
	}
	if res.Status.Available {
		t.Fatal("available = true after a failed check")
	}
}

func TestInstallWhenUpToDateDoesNothing(t *testing.T) {
	svc, _ := newTestService(t, "0.1.0", &stubProvider{})

	res := svc.Install()
	if res.Error != nil {
		t.Fatalf("install error: %v", res.Error)
	}
	if res.State != StateUpToDate {
		t.Fatalf("state = %q, want %q", res.State, StateUpToDate)
	}
}

func TestHeadlessBuildStillReportsVersion(t *testing.T) {
	previous := Version
	Version = "0.3.1"
	t.Cleanup(func() { Version = previous })

	svc := &Service{}
	if got := svc.Status(); got.Status.Current != "0.3.1" || got.Error != nil {
		t.Fatalf("status = %+v", got)
	}
	if res := svc.Check(); res.Error == nil {
		t.Fatal("Check without an updater should report an error")
	}
	if res := svc.Install(); res.Error == nil {
		t.Fatal("Install without an updater should report an error")
	}
}

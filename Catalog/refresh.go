package Catalog

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/anyscapelabs/argus/Provider"
)

const (
	SourceEmbedded = "embedded"
	SourceCache    = "cache"
	SourceNetwork  = "network"
)

type Status struct {
	Source        string `json:"source"`
	FetchedAt     string `json:"fetched_at"`
	LastAttempt   string `json:"last_attempt,omitempty"`
	LastError     string `json:"last_error,omitempty"`
	ProviderCount int    `json:"provider_count"`
	ModelCount    int    `json:"model_count"`
	Stale         bool   `json:"stale"`
	Placeholder   bool   `json:"placeholder"`
}

type Store struct {
	overlay   *Provider.Overlay
	cacheDir  string
	onUpdated func()

	current atomic.Pointer[Catalog]
	status  atomic.Pointer[Status]
}

func NewStore(o *Provider.Overlay, cacheDir string, onUpdated func()) (*Store, error) {
	s := &Store{
		overlay:   o,
		cacheDir:  cacheDir,
		onUpdated: onUpdated,
	}
	cat, err := Build(o, time.Now())
	if err != nil {
		return nil, fmt.Errorf("catalog: build failed: %w", err)
	}
	now := nowRFC3339()
	st := Status{
		Source:        SourceEmbedded,
		FetchedAt:     now,
		ProviderCount: cat.Report().ProviderCount,
		ModelCount:    cat.Report().ModelCount,
	}
	s.publish(cat, st)
	return s, nil
}

func (s *Store) Get() *Catalog {
	return s.current.Load()
}

func (s *Store) Status() Status {
	p := s.status.Load()
	if p == nil {
		return Status{}
	}
	return *p
}

// Refresh is a no-op now that the catalog is overlay-only. Kept so existing
// callers (Services/Catalog, tests) keep compiling.
func (s *Store) Refresh(ctx context.Context) error {
	return nil
}

func (s *Store) publish(cat *Catalog, st Status) {
	s.current.Store(cat)
	s.status.Store(&st)
}

// StartBackground is a no-op: there is no upstream catalog to poll anymore.
func StartBackground(s *Store, ctx context.Context) {}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}

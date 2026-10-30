package Catalog

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/anyscapelabs/argus/Provider"
)

const (
	modelsDevURL  = "https://models.dev/api.json"
	maxFetchBytes = 8 << 20
	checkInterval = 24 * time.Hour
	initialDelay  = 5 * time.Second
	retryInitial  = 1 * time.Minute
	retryMax      = time.Hour
	fetchTimeout  = 20 * time.Second
	staleAfter    = 7 * 24 * time.Hour
	cacheBodyName = "catalog.json"
	cacheMetaName = "catalog.json.meta"
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
	client    *http.Client
	onUpdated func()

	current atomic.Pointer[Catalog]
	status  atomic.Pointer[Status]

	etagMu    sync.Mutex
	etag      string
	catalogMu sync.Mutex

	sourceURL string
}

func NewStore(o *Provider.Overlay, cacheDir string, onUpdated func()) (*Store, error) {
	s := &Store{
		overlay:   o,
		cacheDir:  cacheDir,
		client:    newHTTPClient(),
		onUpdated: onUpdated,
	}
	s.sourceURL = modelsDevURL

	embeddedBody, embeddedMeta, err := Embedded()
	if err != nil {
		return nil, fmt.Errorf("catalog: embedded snapshot metadata unreadable: %w", err)
	}
	cat, err := Build(o, embeddedBody, time.Now())
	if err != nil {
		return nil, fmt.Errorf("catalog: embedded snapshot failed to build: %w", err)
	}
	st := Status{
		Source:        SourceEmbedded,
		FetchedAt:     embeddedMeta.FetchedAt,
		Placeholder:   embeddedMeta.Placeholder,
		ProviderCount: cat.Report().ProviderCount,
		ModelCount:    cat.Report().ModelCount,
		Stale:         isStale(embeddedMeta.FetchedAt),
	}
	s.publish(cat, st)

	if cached, cachedStatus, cachedETag, ok := s.loadCached(embeddedMeta.FetchedAt); ok {
		s.etag = cachedETag
		s.publish(cached, cachedStatus)
	}
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

func (s *Store) Refresh(ctx context.Context) error {
	s.catalogMu.Lock()
	defer s.catalogMu.Unlock()
	now := nowRFC3339()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.sourceURL, nil)
	if err != nil {
		return fmt.Errorf("catalog: refresh request could not be built: %w", err)
	}
	st := s.Status()
	if st.Source == SourceCache || st.Source == SourceNetwork {
		if etag := s.currentETag(); etag != "" {
			req.Header.Set("If-None-Match", etag)
		}
	}
	resp, err := s.client.Do(req)
	if err != nil {
		s.recordFailure(err)
		return fmt.Errorf("catalog: refresh fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		updated := s.Status()
		updated.LastAttempt = now
		updated.FetchedAt = now
		updated.Stale = false
		s.status.Store(&updated)
		s.touchCacheMeta(now)
		return nil
	}
	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("models.dev returned HTTP %d", resp.StatusCode)
		s.recordFailure(err)
		return fmt.Errorf("catalog: unexpected status fetching models.dev: %w", err)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFetchBytes+1))
	if err != nil {
		s.recordFailure(err)
		return fmt.Errorf("catalog: refresh read failed: %w", err)
	}
	if len(body) > maxFetchBytes {
		err := fmt.Errorf("models.dev response exceeded %d bytes", maxFetchBytes)
		s.recordFailure(err)
		return err
	}

	built, err := Build(s.overlay, body, time.Now())
	if err != nil {
		s.recordFailure(err)
		return fmt.Errorf("catalog: refreshed body failed to build: %w", err)
	}

	prev := s.current.Load()
	withData := built.Report().ProviderCount - len(built.Report().ProvidersNoData)
	if withData < 1 || built.Report().ModelCount == 0 {
		err := fmt.Errorf("refreshed catalog had no providers with data")
		s.recordFailure(err)
		return err
	}
	if prev != nil {
		expected := prev.Count() / 2
		if expected > 0 && built.Report().ModelCount < expected {
			err := fmt.Errorf("refreshed catalog model count %d below half of current %d", built.Report().ModelCount, prev.Count())
			s.recordFailure(err)
			return err
		}
	}

	etag := strings.TrimSpace(resp.Header.Get("ETag"))
	if err := s.writeCache(body, etag); err != nil {
		return fmt.Errorf("catalog: cache write failed: %w", err)
	}
	s.setStatusEtag(etag)

	st = Status{
		Source:        SourceNetwork,
		FetchedAt:     now,
		ProviderCount: built.Report().ProviderCount,
		ModelCount:    built.Report().ModelCount,
		Stale:         false,
		Placeholder:   false,
	}
	s.publish(built, st)
	if s.onUpdated != nil {
		s.onUpdated()
	}
	return nil
}

func (s *Store) loadCached(embeddedFetchedAt string) (*Catalog, Status, string, bool) {
	metaRaw, err := os.ReadFile(filepath.Join(s.cacheDir, cacheMetaName))
	if err != nil {
		return nil, Status{}, "", false
	}
	var meta struct {
		ETag        string `json:"etag"`
		FetchedAt   string `json:"fetched_at"`
		ContentHash string `json:"content_hash"`
	}
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		return nil, Status{}, "", false
	}
	body, err := os.ReadFile(filepath.Join(s.cacheDir, cacheBodyName))
	if err != nil {
		return nil, Status{}, "", false
	}
	if meta.ContentHash != "" {
		sum := sha256.Sum256(body)
		if hex.EncodeToString(sum[:]) != meta.ContentHash {
			return nil, Status{}, "", false
		}
	}
	built, err := Build(s.overlay, body, time.Now())
	if err != nil || built.Count() == 0 {
		return nil, Status{}, "", false
	}
	cacheTime, cacheErr := time.Parse(time.RFC3339, meta.FetchedAt)
	embeddedTime, embeddedErr := time.Parse(time.RFC3339, embeddedFetchedAt)
	older := cacheErr != nil || (embeddedErr == nil && !cacheTime.After(embeddedTime))
	if older {
		return nil, Status{}, "", false
	}
	st := Status{
		Source:        SourceCache,
		FetchedAt:     meta.FetchedAt,
		ProviderCount: built.Report().ProviderCount,
		ModelCount:    built.Report().ModelCount,
		Stale:         isStale(meta.FetchedAt),
		Placeholder:   false,
	}
	return built, st, meta.ETag, true
}

func (s *Store) touchCacheMeta(now string) {
	metaPath := filepath.Join(s.cacheDir, cacheMetaName)
	raw, err := os.ReadFile(metaPath)
	if err != nil {
		return
	}
	var meta struct {
		ETag        string `json:"etag"`
		FetchedAt   string `json:"fetched_at"`
		ContentHash string `json:"content_hash"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return
	}
	meta.FetchedAt = now
	b, err := json.Marshal(meta)
	if err != nil {
		return
	}
	tmp, err := os.CreateTemp(s.cacheDir, "catalog-meta-")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return
	}
	tmp.Close()
	_ = os.Rename(tmpName, metaPath)
}

func (s *Store) writeCache(body []byte, etag string) error {
	if err := os.MkdirAll(s.cacheDir, 0o700); err != nil {
		return err
	}
	sum := sha256.Sum256(body)
	contentHash := hex.EncodeToString(sum[:])

	tmpBody, err := os.CreateTemp(s.cacheDir, "catalog-body-")
	if err != nil {
		return err
	}
	tmpName := tmpBody.Name()
	if _, err := tmpBody.Write(body); err != nil {
		tmpBody.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmpBody.Sync(); err != nil {
		tmpBody.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmpBody.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, filepath.Join(s.cacheDir, cacheBodyName)); err != nil {
		os.Remove(tmpName)
		return err
	}

	meta := struct {
		ETag        string `json:"etag"`
		FetchedAt   string `json:"fetched_at"`
		ContentHash string `json:"content_hash"`
	}{ETag: etag, FetchedAt: nowRFC3339(), ContentHash: contentHash}
	metaBody, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	tmpMeta, err := os.CreateTemp(s.cacheDir, "catalog-meta-")
	if err != nil {
		return err
	}
	tmpMetaName := tmpMeta.Name()
	if _, err := tmpMeta.Write(metaBody); err != nil {
		tmpMeta.Close()
		os.Remove(tmpMetaName)
		return err
	}
	if err := tmpMeta.Sync(); err != nil {
		tmpMeta.Close()
		os.Remove(tmpMetaName)
		return err
	}
	if err := tmpMeta.Close(); err != nil {
		os.Remove(tmpMetaName)
		return err
	}
	if err := os.Rename(tmpMetaName, filepath.Join(s.cacheDir, cacheMetaName)); err != nil {
		os.Remove(tmpMetaName)
		return err
	}
	s.etag = etag
	return nil
}

func (s *Store) currentETag() string {
	s.etagMu.Lock()
	defer s.etagMu.Unlock()
	return s.etag
}

func (s *Store) setStatusEtag(etag string) {
	s.etagMu.Lock()
	s.etag = etag
	s.etagMu.Unlock()
}

func (s *Store) recordFailure(err error) {
	st := s.Status()
	st.LastAttempt = nowRFC3339()
	if err != nil {
		st.LastError = err.Error()
	}
	s.status.Store(&st)
}

func (s *Store) publish(cat *Catalog, st Status) {
	s.current.Store(cat)
	s.status.Store(&st)
}

func StartBackground(s *Store, ctx context.Context) {
	go func() {
		next := initialDelay
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(next):
			}
			err := s.Refresh(ctx)
			switch {
			case err == nil:
				next = checkInterval + time.Duration(rand.Intn(600))*time.Second
			case next >= retryInitial:
				next *= 2
				if next > retryMax {
					next = retryMax
				}
			default:
				next = retryInitial
			}
		}
	}()
}

func newHTTPClient() *http.Client {
	return &http.Client{
		Timeout: fetchTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return fmt.Errorf("catalog: models.dev redirects are not allowed")
		},
	}
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func isStale(fetchedAt string) bool {
	t, err := time.Parse(time.RFC3339, fetchedAt)
	if err != nil {
		return true
	}
	return time.Since(t) > staleAfter
}

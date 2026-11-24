package Skills

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/anyscapelabs/argus/Net"
)

const (
	SourceEmbedded = "embedded"
	SourceCache    = "cache"
	SourceNetwork  = "network"

	maxSigBytes = 8 << 10
	staleAfter  = 7 * 24 * time.Hour
)

type snapshot struct {
	metas       []SkillMeta
	byID        map[string]SkillMeta
	source      string
	fetchedAt   time.Time
	generatedAt time.Time
}

func (s *snapshot) compatible(appVersion string) bool {
	for _, m := range s.metas {
		if CompareVersions(m.MinAppVersion, appVersion) <= 0 {
			return true
		}
	}
	return false
}

// Status is the registry's last known state, mirroring the catalog's status
// shape so the desktop can show where skill metadata came from.
type Status struct {
	Source      string `json:"source"`
	FetchedAt   string `json:"fetched_at"`
	LastAttempt string `json:"last_attempt,omitempty"`
	LastError   string `json:"last_error,omitempty"`
	SkillCount  int    `json:"skill_count"`
	Stale       bool   `json:"stale"`
	IndexURL    string `json:"index_url,omitempty"`
}

// Store is the embedded → cache → network skills registry. Every published
// snapshot is immutable and swapped in with one atomic store, so an agent
// building a prompt never sees a half-updated index.
type Store struct {
	cacheDir   string
	indexURL   string
	baseURL    string
	appVersion string
	onUpdated  func()
	client     *http.Client

	current atomic.Pointer[snapshot]
	status  atomic.Pointer[Status]

	mu     sync.Mutex
	etag   string
	bodies sync.Map
}

type cacheMeta struct {
	ETag        string `json:"etag"`
	FetchedAt   string `json:"fetched_at"`
	SHA256      string `json:"sha256"`
	GeneratedAt string `json:"generated_at"`
}

func NewStore(cacheDir, indexURL, appVersion string, onUpdated func()) (*Store, error) {
	s := &Store{
		cacheDir:   cacheDir,
		indexURL:   strings.TrimSpace(indexURL),
		appVersion: appVersion,
		onUpdated:  onUpdated,
		client:     Net.Client(Net.Oneshot),
	}
	s.baseURL = baseForURL(s.indexURL)

	embedded, err := buildEmbedded(appVersion)
	if err != nil {
		return nil, err
	}
	st := Status{
		Source:     SourceEmbedded,
		FetchedAt:  nowRFC3339(),
		SkillCount: len(embedded.metas),
		IndexURL:   s.indexURL,
	}
	s.publish(embedded, st)
	if cacheDir != "" {
		s.loadCache()
	}
	return s, nil
}

func baseForURL(indexURL string) string {
	if indexURL == "" {
		return ""
	}
	if i := strings.LastIndex(indexURL, "/"); i >= 0 {
		return indexURL[:i+1]
	}
	return indexURL + "/"
}

func buildEmbedded(appVersion string) (*snapshot, error) {
	raw, err := embeddedFS.ReadFile("data/skills.json")
	if err != nil {
		return nil, fmt.Errorf("skills: embedded index: %w", err)
	}
	var list []SkillMeta
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("skills: embedded index: %w", err)
	}
	snap := &snapshot{
		metas:     filterCompatible(list, appVersion),
		byID:      map[string]SkillMeta{},
		source:    SourceEmbedded,
		fetchedAt: time.Now().UTC(),
	}
	for _, m := range list {
		if m.Version == "" {
			m.Version = "1.0.0"
		}
		snap.byID[m.ID] = m
	}
	for i := range snap.metas {
		if snap.metas[i].Version == "" {
			snap.metas[i].Version = "1.0.0"
		}
	}
	return snap, nil
}

func filterCompatible(list []SkillMeta, appVersion string) []SkillMeta {
	out := make([]SkillMeta, 0, len(list))
	for _, m := range list {
		if CompareVersions(m.MinAppVersion, appVersion) <= 0 {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (s *Store) publish(snap *snapshot, st Status) {
	s.current.Store(snap)
	s.status.Store(&st)
}

func (s *Store) Current() *snapshot { return s.current.Load() }

func (s *Store) Status() Status {
	if p := s.status.Load(); p != nil {
		return *p
	}
	return Status{}
}

func (s *Store) All() []SkillMeta {
	snap := s.current.Load()
	out := make([]SkillMeta, len(snap.metas))
	copy(out, snap.metas)
	return out
}

func (s *Store) Get(id string) (SkillMeta, bool) {
	m, ok := s.current.Load().byID[strings.TrimSpace(id)]
	return m, ok
}

func (s *Store) loadCache() {
	indexFile := s.cacheFile("index.json")
	body, err := os.ReadFile(indexFile)
	if err != nil {
		return
	}
	sig, err := os.ReadFile(indexFile + ".sig")
	if err != nil || !VerifyIndex(body, sig) {
		return
	}
	metaRaw, err := os.ReadFile(indexFile + ".meta")
	if err != nil {
		return
	}
	var cm cacheMeta
	if err := json.Unmarshal(metaRaw, &cm); err != nil || cm.SHA256 != BundleSHA256(body) {
		return
	}
	var idx Index
	if err := json.Unmarshal(body, &idx); err != nil || idx.Validate() != nil {
		return
	}
	fetchedAt, err := time.Parse(time.RFC3339, cm.FetchedAt)
	if err != nil {
		return
	}
	snap := &snapshot{
		metas:       filterCompatible(idx.Skills, s.appVersion),
		byID:        byID(idx.Skills),
		source:      SourceCache,
		fetchedAt:   fetchedAt.UTC(),
		generatedAt: idx.GeneratedTime(),
	}
	s.etag = cm.ETag
	st := Status{
		Source:      SourceCache,
		FetchedAt:   cm.FetchedAt,
		SkillCount:  len(snap.metas),
		Stale:       time.Since(fetchedAt) > staleAfter,
		IndexURL:    s.indexURL,
		LastAttempt: cm.FetchedAt,
	}
	s.publish(snap, st)
}

func byID(list []SkillMeta) map[string]SkillMeta {
	out := make(map[string]SkillMeta, len(list))
	for _, m := range list {
		out[m.ID] = m
	}
	return out
}

// Refresh fetches the signed index with an ETag, validates it, and swaps the
// whole registry in atomically. A 304 only freshens the cache timestamp.
func (s *Store) Refresh(ctx context.Context) error {
	if s.indexURL == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	attempt := nowRFC3339()
	body, etag, status, err := s.fetch(ctx, s.indexURL, maxIndexBytes, s.etag)
	if err != nil {
		return s.fail(err)
	}
	if status == http.StatusNotModified {
		s.touchCacheMeta(attempt)
		prev := s.status.Load()
		st := Status{}
		if prev != nil {
			st = *prev
		}
		st.LastAttempt = attempt
		st.LastError = ""
		st.Stale = false
		s.status.Store(&st)
		return nil
	}
	if status != http.StatusOK {
		return s.fail(fmt.Errorf("unexpected index status %d", status))
	}
	if len(body) > maxIndexBytes {
		return s.fail(fmt.Errorf("index exceeds %d bytes", maxIndexBytes))
	}
	sig, _, sigStatus, err := s.fetch(ctx, s.indexURL+".sig", maxSigBytes, "")
	if err != nil {
		return s.fail(fmt.Errorf("signature fetch: %w", err))
	}
	if sigStatus != http.StatusOK {
		return s.fail(fmt.Errorf("unexpected signature status %d", sigStatus))
	}
	if !VerifyIndex(body, sig) {
		return s.fail(fmt.Errorf("index signature verification failed"))
	}
	var idx Index
	if err := json.Unmarshal(body, &idx); err != nil {
		return s.fail(fmt.Errorf("index parse: %w", err))
	}
	if err := idx.Validate(); err != nil {
		return s.fail(err)
	}
	current := s.current.Load()
	if current.source != SourceEmbedded && idx.GeneratedTime().Before(current.generatedAt) {
		return s.fail(fmt.Errorf("index rolled back: %s is older than %s", idx.GeneratedAt, current.generatedAt.Format(time.RFC3339)))
	}
	if err := s.writeCache(body, sig, cmFrom(etag, attempt, body, idx)); err != nil {
		return s.fail(err)
	}
	snap := &snapshot{
		metas:       filterCompatible(idx.Skills, s.appVersion),
		byID:        byID(idx.Skills),
		source:      SourceNetwork,
		fetchedAt:   time.Now().UTC(),
		generatedAt: idx.GeneratedTime(),
	}
	s.etag = etag
	st := Status{
		Source:      SourceNetwork,
		FetchedAt:   attempt,
		LastAttempt: attempt,
		SkillCount:  len(snap.metas),
		IndexURL:    s.indexURL,
	}
	s.publish(snap, st)
	if s.onUpdated != nil {
		s.onUpdated()
	}
	return nil
}

func cmFrom(etag, fetchedAt string, body []byte, idx Index) cacheMeta {
	return cacheMeta{ETag: etag, FetchedAt: fetchedAt, SHA256: BundleSHA256(body), GeneratedAt: idx.GeneratedAt}
}

func (s *Store) fetch(ctx context.Context, url string, capBytes int, etag string) ([]byte, string, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", 0, err
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		return nil, etag, resp.StatusCode, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(capBytes)+1))
	return body, strings.TrimSpace(resp.Header.Get("ETag")), resp.StatusCode, err
}

func (s *Store) fail(err error) error {
	prev := s.status.Load()
	st := Status{}
	if prev != nil {
		st = *prev
	}
	st.LastAttempt = nowRFC3339()
	st.LastError = err.Error()
	s.status.Store(&st)
	return err
}

// FetchBundle downloads and verifies a Level-2 document named in the signed
// index. Nothing is enabled by this call; the agent tool decides that after
// the permission card settles.
func (s *Store) FetchBundle(ctx context.Context, id string) error {
	meta, ok := s.Get(id)
	if !ok {
		return fmt.Errorf("unknown skill: %s", id)
	}
	if meta.SHA256 == "" {
		return nil
	}
	key := meta.ID + "@" + meta.Version
	if cached, ok := s.bodies.Load(key); ok {
		if raw, ok := cached.([]byte); ok && VerifyBundle(raw, meta.SHA256) {
			return nil
		}
		s.bodies.Delete(key)
	}
	url := s.baseURL + "bundles/" + meta.ID + "@" + meta.Version + ".json"
	body, _, status, err := s.fetch(ctx, url, maxBundleBytes, "")
	if err != nil {
		return fmt.Errorf("fetch skill %s: %w", id, err)
	}
	if status != http.StatusOK || len(body) > maxBundleBytes {
		return fmt.Errorf("fetch skill %s: unexpected status or oversized bundle", id)
	}
	if !VerifyBundle(body, meta.SHA256) {
		return fmt.Errorf("fetch skill %s: sha256 verification failed", id)
	}
	var bundle Bundle
	if err := json.Unmarshal(body, &bundle); err != nil {
		return fmt.Errorf("fetch skill %s: invalid bundle: %w", id, err)
	}
	if bundle.ID != meta.ID || bundle.Version != meta.Version {
		return fmt.Errorf("fetch skill %s: bundle identity mismatch", id)
	}
	if s.cacheDir != "" {
		if err := writeFileAtomic(s.cacheFile("bundles/"+key+".json"), body, 0o600); err != nil {
			return err
		}
	}
	s.bodies.Store(key, body)
	return nil
}

// ReadBody returns Level-2 instructions from the verified cache, falling back
// to the embedded copy so the app works offline and on first launch.
func (s *Store) ReadBody(id string) (string, bool) {
	meta, ok := s.Get(id)
	if !ok {
		return "", false
	}
	key := meta.ID + "@" + meta.Version
	if v, ok := s.bodies.Load(key); ok {
		if raw, ok := v.([]byte); ok && VerifyBundle(raw, meta.SHA256) {
			var bundle Bundle
			if json.Unmarshal(raw, &bundle) == nil {
				return bundle.Body, true
			}
		}
		s.bodies.Delete(key)
	}
	if s.cacheDir != "" && meta.SHA256 != "" {
		raw, err := os.ReadFile(s.cacheFile("bundles/" + key + ".json"))
		if err == nil && VerifyBundle(raw, meta.SHA256) {
			var bundle Bundle
			if json.Unmarshal(raw, &bundle) == nil && bundle.Body != "" {
				s.bodies.Store(key, raw)
				return bundle.Body, true
			}
		}
	}
	raw, err := fs.ReadFile(embeddedFS, "data/"+meta.ID+"/SKILL.md")
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(stripYAMLFrontmatter(string(raw))), true
}

func (s *Store) cacheFile(rel string) string {
	return filepath.Join(s.cacheDir, "skills", filepath.FromSlash(rel))
}

func (s *Store) writeCache(index, sig []byte, cm cacheMeta) error {
	if s.cacheDir == "" {
		return nil
	}
	metaRaw, err := json.Marshal(cm)
	if err != nil {
		return err
	}
	base := s.cacheFile("index.json")
	if err := writeFileAtomic(base, index, 0o600); err != nil {
		return err
	}
	if err := writeFileAtomic(base+".sig", sig, 0o600); err != nil {
		return err
	}
	return writeFileAtomic(base+".meta", metaRaw, 0o600)
}

func (s *Store) touchCacheMeta(fetchedAt string) {
	if s.cacheDir == "" {
		return
	}
	path := s.cacheFile("index.json.meta")
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var cm cacheMeta
	if err := json.Unmarshal(raw, &cm); err != nil {
		return
	}
	cm.FetchedAt = fetchedAt
	if updated, err := json.Marshal(cm); err == nil {
		_ = writeFileAtomic(path, updated, 0o600)
	}
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".skills-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, perm); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// StartBackground polls the signed index on a 24h cadence with jitter, backing
// off on failures. Passing an empty index URL keeps the store offline-only.
func (s *Store) StartBackground(ctx context.Context) {
	if s.indexURL == "" {
		return
	}
	go func() {
		delay := 5 * time.Second
		failures := 0
		for {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			if ctx.Err() != nil {
				return
			}
			if err := s.Refresh(ctx); err != nil {
				failures++
				delay = backoff(failures)
			} else {
				failures = 0
				delay = 24*time.Hour + time.Duration(rand.Intn(600))*time.Second
			}
		}
	}()
}

func backoff(failures int) time.Duration {
	d := time.Minute
	for i := 1; i < failures && d < time.Hour; i++ {
		d *= 2
	}
	if d > time.Hour {
		return time.Hour
	}
	return d
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}

package Skills

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func withTestKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	old := signingKey
	signingKey = pub
	t.Cleanup(func() { signingKey = old })
	return priv
}

func testIndex(version, generatedAt, id, body string) (Index, []byte, Bundle) {
	bundle := Bundle{ID: id, Version: version, Body: body}
	bundleBytes, _ := json.Marshal(bundle)
	idx := Index{
		Schema:      IndexSchema,
		GeneratedAt: generatedAt,
		Skills: []SkillMeta{{
			ID: id, Name: strings.ToUpper(id), Description: "A skill for " + id,
			Connectors: []string{}, Version: version, SHA256: BundleSHA256(bundleBytes),
			Size: int64(len(bundleBytes)), Channel: "stable", Publisher: "argus",
		}},
	}
	indexBytes, _ := json.Marshal(idx)
	return idx, indexBytes, bundle
}

func TestVerifyIndexAndBundle(t *testing.T) {
	priv := withTestKey(t)
	idx, body, _ := testIndex("1.0.0", "2026-11-19T09:00:00Z", "demo", "# Demo body")
	sig := ed25519.Sign(priv, body)
	if !VerifyIndex(body, sig) {
		t.Fatal("valid signature rejected")
	}
	body[0] ^= 0xff
	if VerifyIndex(body, sig) {
		t.Fatal("tampered index accepted")
	}
	if VerifyBundle([]byte("wrong"), idx.Skills[0].SHA256) {
		t.Fatal("wrong bundle hash accepted")
	}
}

func TestIndexValidation(t *testing.T) {
	idx, _, _ := testIndex("1.0.0", "2026-11-19T09:00:00Z", "demo", "body")
	if err := idx.Validate(); err != nil {
		t.Fatalf("valid index rejected: %v", err)
	}
	idx.Skills = append(idx.Skills, idx.Skills[0])
	if err := idx.Validate(); err == nil {
		t.Fatal("duplicate id accepted")
	}
	idx.Schema = "unknown"
	if err := idx.Validate(); err == nil {
		t.Fatal("unknown schema accepted")
	}
}

// The registry serves a signed index from a local test CDN, then reuses the
// on-disk cache and answers a 304 without republishing.
func TestStoreRefreshCacheAndBundle(t *testing.T) {
	priv := withTestKey(t)
	cacheDir := t.TempDir()
	_, indexBody, bundle := testIndex("1.0.0", "2026-11-19T09:00:00Z", "remote", "# Remote body")
	bundleBytes, _ := json.Marshal(bundle)
	sig := ed25519.Sign(priv, indexBody)

	var uses int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index.json":
			w.Header().Set("ETag", `"v1"`)
			if r.Header.Get("If-None-Match") == `"v1"` {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			uses++
			_, _ = w.Write(indexBody)
		case "/index.json.sig":
			_, _ = w.Write(sig)
		case "/bundles/remote@1.0.0.json":
			_, _ = w.Write(bundleBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	updates := 0
	store, err := NewStore(cacheDir, server.URL+"/index.json", "1.0.0", func() { updates++ })
	if err != nil {
		t.Fatal(err)
	}
	if got := store.Status().Source; got != SourceEmbedded {
		t.Fatalf("initial source = %q", got)
	}
	if err := store.Refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if store.Status().Source != SourceNetwork || updates != 1 {
		t.Fatalf("after refresh: source=%q updates=%d", store.Status().Source, updates)
	}
	if _, ok := store.Get("remote"); !ok {
		t.Fatal("remote skill missing after refresh")
	}
	if err := store.Refresh(context.Background()); err != nil {
		t.Fatalf("304 refresh: %v", err)
	}
	if updates != 1 {
		t.Fatalf("304 republished the index: updates=%d", updates)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "skills", "index.json.meta")); err != nil {
		t.Fatalf("cache meta missing: %v", err)
	}

	cached, err := NewStore(cacheDir, server.URL+"/index.json", "1.0.0", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cached.Status().Source != SourceCache {
		t.Fatalf("cache source = %q", cached.Status().Source)
	}
	if err := cached.FetchBundle(context.Background(), "remote"); err != nil {
		t.Fatalf("fetch bundle: %v", err)
	}
	body, ok := cached.ReadBody("remote")
	if !ok || body != "# Remote body" {
		t.Fatalf("read body = %q, %v", body, ok)
	}
}

func TestRefreshRejectsRollback(t *testing.T) {
	priv := withTestKey(t)
	_, first, _ := testIndex("1.0.0", "2026-11-19T10:00:00Z", "demo", "body")
	_, older, _ := testIndex("0.9.0", "2026-11-19T09:00:00Z", "demo", "body")
	var mu sync.Mutex
	var currentBody []byte
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		body := append([]byte(nil), currentBody...)
		mu.Unlock()
		if strings.HasSuffix(r.URL.Path, ".sig") {
			_, _ = w.Write(ed25519.Sign(priv, body))
			return
		}
		_, _ = w.Write(body)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	store, err := NewStore("", server.URL+"/index.json", "1.0.0", nil)
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	currentBody = first
	mu.Unlock()
	if err := store.Refresh(context.Background()); err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	mu.Lock()
	currentBody = older
	mu.Unlock()
	if err := store.Refresh(context.Background()); err == nil {
		t.Fatal("rolled-back index accepted")
	}
}

func TestSearchAndPromptBlock(t *testing.T) {
	store, err := NewStore("", "", "1.0.0", nil)
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{store: store}
	hits := s.Search("gmail", 5)
	if len(hits) == 0 || hits[0].ID != "gmail" {
		t.Fatalf("gmail should rank first for its own id, got %v", hits)
	}
	prompt := s.PromptBlock("agent", map[string]bool{"gmail": true})
	if !strings.Contains(prompt, "search_skills") {
		t.Fatal("prompt no longer advertises search_skills")
	}
	if strings.Contains(prompt, "Available skills") {
		t.Fatal("prompt still lists every skill id")
	}
	if !strings.Contains(prompt, "- gmail:") {
		t.Fatalf("installed skill missing from prompt: %s", prompt)
	}
}

func TestCompareVersions(t *testing.T) {
	cases := [][3]any{
		{"", "1.0.0", -1},
		{"v1.2.0", "1.10.0", -1},
		{"1.2.3", "1.2.3", 0},
		{"2.0.0", "1.99.99", 1},
	}
	for _, c := range cases {
		got := CompareVersions(c[0].(string), c[1].(string))
		if got != c[2].(int) {
			t.Fatal(fmt.Sprintf("CompareVersions(%q,%q) = %d, want %d", c[0], c[1], got, c[2]))
		}
	}
}

func TestBackoffCapsAtHour(t *testing.T) {
	if got := backoff(20); got != time.Hour {
		t.Fatalf("backoff(20) = %v", got)
	}
}

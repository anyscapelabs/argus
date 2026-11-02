package Search

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func jsonServer(t *testing.T, hits string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(hits))
	}))
}

func TestSearchParsesResultsAndAnswers(t *testing.T) {
	srv := jsonServer(t, `{
		"query": "gophers",
		"results": [
			{"title": "Go blog", "url": "https://go.dev/blog", "content": "  spaced   out\n content  "},
			{"title": "No URL", "url": "", "content": "skipped"},
			{"title": "Go proverbs", "url": "https://go-proverbs.github.io", "content": "Simple is better."}
		],
		"answers": ["Go is an open source programming language."]
	}`)
	defer srv.Close()

	c := NewClientWith([]string{srv.URL})
	a := c.Search(context.Background(), "gophers", 5)
	if a.Err != "" {
		t.Fatalf("unexpected error: %q", a.Err)
	}
	if len(a.Results) != 2 {
		t.Fatalf("want 2 results, got %d", len(a.Results))
	}
	if a.Results[0].URL != "https://go.dev/blog" || a.Results[0].Snippet != "spaced out content" {
		t.Fatalf("bad first result: %+v", a.Results[0])
	}
	if a.Answer == "" {
		t.Fatal("expected instant answer to be surfaced")
	}
}

func TestSearchFailsOverFromBotCheck(t *testing.T) {
	srv1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<!doctype html><html><head><title>Verifying your browser…</title></head><body>please wait</body></html>`))
	}))
	defer srv1.Close()
	srv2 := jsonServer(t, `{"results":[{"title":"ok","url":"https://example.com","content":"fine"}]}`)
	defer srv2.Close()

	c := NewClientWith([]string{srv1.URL, srv2.URL})
	a := c.Search(context.Background(), "test", 0)
	if a.Err != "" {
		t.Fatalf("failover failed: %q", a.Err)
	}
	if len(a.Results) != 1 || a.Results[0].Title != "ok" {
		t.Fatalf("unexpected answer: %+v", a)
	}
}

func TestSearchClampsMaxResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body := `{"results":[`
		for i := 0; i < 20; i++ {
			if i > 0 {
				body += ","
			}
			body += `{"title":"t` + strconv.Itoa(i) + `","url":"https://e.com/` + strconv.Itoa(i) + `","content":"c"}`
		}
		body += `]}`
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	c := NewClientWith([]string{srv.URL})
	a := c.Search(context.Background(), "test", 99)
	if len(a.Results) != maxResultsCap {
		t.Fatalf("want %d results, got %d", maxResultsCap, len(a.Results))
	}
}

func TestSearchCooldownsBlockedInstance(t *testing.T) {
	var seen atomic.Int32
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		seen.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte("Too Many Requests"))
	}))
	defer bad.Close()
	good := jsonServer(t, `{"results":[{"title":"ok","url":"https://example.com","content":"fine"}]}`)
	defer good.Close()

	// First rotation starts at offset 1, so this pool tries bad then good.
	c := NewClientWith([]string{good.URL, bad.URL})
	reset := func() {
		for _, inst := range c.instances {
			inst.mu.Lock()
			inst.lastUse = time.Time{}
			inst.mu.Unlock()
		}
	}

	// Search 1: bad answers 429, good takes over (failover works).
	if a := c.Search(context.Background(), "test", 3); a.Err != "" {
		t.Fatalf("failover failed: %q", a.Err)
	}
	if got := seen.Load(); got != 1 {
		t.Fatalf("blocked instance hit %d times, want 1", got)
	}

	// Search 2: cooldown keeps bad out of the loop entirely.
	reset()
	if a := c.Search(context.Background(), "test", 3); a.Err != "" {
		t.Fatalf("second search failed: %q", a.Err)
	}
	if got := seen.Load(); got != 1 {
		t.Fatalf("blocked instance hit %d times during cooldown, want 1", got)
	}

	// Search 3: once the cooldown is cleared (forced here in place of the
	// real wait) the instance is tried again.
	c.instances[1].mu.Lock()
	c.instances[1].blockedUntil = time.Now().Add(-time.Second)
	c.instances[1].mu.Unlock()
	reset()
	if a := c.Search(context.Background(), "test", 3); a.Err != "" {
		t.Fatalf("retry failed: %q", a.Err)
	}
	if got := seen.Load(); got != 2 {
		t.Fatalf("blocked instance hit %d times after unblock, want 2", got)
	}
}

func TestSearchEveryInstanceBlocked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	c := NewClientWith([]string{srv.URL})
	a := c.Search(context.Background(), "test", 3)
	if a.Err == "" {
		t.Fatal("expected all-instances-down error")
	}
}

func TestAnswerRender(t *testing.T) {
	if got := (Answer{Query: "x", Err: "down"}).Render(); got != "web search failed: down" {
		t.Fatalf("bad error render: %q", got)
	}
	a := Answer{
		Query:   "x",
		Answer:  "yes",
		Results: []Result{{Title: "T", URL: "https://e.com", Snippet: "s"}},
	}
	want := "answer: yes\n1. T — https://e.com\n   s"
	if got := a.Render(); got != want {
		t.Fatalf("render mismatch:\n%s\nwant\n%s", got, want)
	}
	if got := (Answer{Query: "x"}).Render(); got != "no results found for: x" {
		t.Fatalf("bad empty render: %q", got)
	}
}

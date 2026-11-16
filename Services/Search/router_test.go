package Search

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRouterClientParsesResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(RouterAnswer{
			Query:    "gophers",
			Provider: "exa",
			Results: []RouterResult{
				{Title: "Go blog", URL: "https://go.dev/blog", Snippet: "The official blog."},
				{Title: "Proverbs", URL: "https://go-proverbs.github.io", Snippet: "Simple is better."},
			},
		})
	}))
	defer srv.Close()

	c := &RouterClient{Base: srv.URL, Key: "test-key"}
	a := c.Search(context.Background(), "gophers", 5)
	if a.Err != "" {
		t.Fatalf("unexpected error: %q", a.Err)
	}
	if len(a.Results) != 2 {
		t.Fatalf("want 2 results, got %d", len(a.Results))
	}
	if a.Results[0].Title != "Go blog" || a.Results[0].Snippet != "The official blog." {
		t.Fatalf("unexpected first result: %+v", a.Results[0])
	}
}

func TestRouterClientSetsBearerFromKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer correct" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		_ = json.NewEncoder(w).Encode(RouterAnswer{Query: "q"})
	}))
	defer srv.Close()

	// Wrong key must fail, proving the client actually sent it.
	bad := &RouterClient{Base: srv.URL, Key: "wrong"}
	if a := bad.Search(context.Background(), "q", 3); a.Err == "" {
		t.Fatal("expected an error with the wrong key")
	}
	good := &RouterClient{Base: srv.URL, Key: "correct"}
	if a := good.Search(context.Background(), "q", 3); a.Err != "" {
		t.Fatalf("unexpected error: %q", a.Err)
	}
}

func TestRouterSearchURLAppendsV1(t *testing.T) {
	c := &RouterClient{Base: "http://localhost:8080/v1"}
	if got := c.searchURL(); got != "http://localhost:8080/v1/search" {
		t.Fatalf("search url: %q", got)
	}
	bare := &RouterClient{Base: "http://localhost:8080"}
	if got := bare.searchURL(); got != "http://localhost:8080/v1/search" {
		t.Fatalf("bare-host search url: %q", got)
	}
}

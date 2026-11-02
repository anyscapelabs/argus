// Package Search answers web queries through SearXNG, a free and open
// metasearch engine. There is no API key and no captcha: the client rotates
// across a pool of public instances, paces every instance, and cools down any
// instance that answers with a rate limit or a bot check, so repeated usage
// spreads across many servers instead of hammering one. Deployments that want
// a fixed backend can self-host SearXNG and point ARGUS_SEARXNG_URLS at it;
// every user of the app can then share that single instance safely despite
// the per-instance pacing.
package Search

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	defaultMaxResults = 6
	maxResultsCap     = 12
	requestTimeout    = 12 * time.Second
	maxAnswerDeadline = 25 * time.Second
	minInstanceGap    = 1500 * time.Millisecond
	bodyLimit         = 1 << 20
	snippetLimit      = 280
)

// Verified JSON-API-capable instances first, then instances known to allow
// the JSON API but to rate-limit harder (they serve again after cooling
// down). Pool maintenance happens in this one list.
var defaultInstances = []string{
	"https://search.lumy.live",
	"https://paulgo.io",
	"https://priv.au",
	"https://opnxng.com",
	"https://searx.tiekoetter.com",
	"https://searx.rhscz.eu",
}

// Result is one web hit for the model to cite.
type Result struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// Answer is everything one query produced. Err is set only when every
// instance in the pool failed; empty Results with Err unset means the query
// genuinely had no hits.
type Answer struct {
	Query   string   `json:"query"`
	Results []Result `json:"results,omitempty"`
	Answer  string   `json:"answer,omitempty"`
	Err     string   `json:"error,omitempty"`
}

// Render flattens an Answer into the tool_result text the model sees.
func (a Answer) Render() string {
	if a.Err != "" {
		return "web search failed: " + a.Err
	}
	if len(a.Results) == 0 && a.Answer == "" {
		return "no results found for: " + a.Query
	}
	var b strings.Builder
	if a.Answer != "" {
		b.WriteString("answer: " + a.Answer + "\n")
	}
	for i, r := range a.Results {
		fmt.Fprintf(&b, "%d. %s — %s\n", i+1, r.Title, r.URL)
		if r.Snippet != "" {
			b.WriteString("   " + r.Snippet + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// instance is one SearXNG server with its own pacing state, guarded by its
// own mutex so many concurrent users never race the bookkeeping.
type instance struct {
	mu           sync.Mutex
	base         string
	lastUse      time.Time
	blockedUntil time.Time
	failures     int
}

// Client is safe for concurrent use: many agents and chat runs may search at
// the same time, and each query is routed to a different pool offset.
type Client struct {
	mu        sync.Mutex
	next      int
	instances []*instance
}

var defaultClient = NewClient()

// Search runs one query against the default instance pool.
func Search(ctx context.Context, query string, maxResults int) Answer {
	return defaultClient.Search(ctx, query, maxResults)
}

// NewClient builds a client with the shipped public-instance pool, or the
// self-hosted override from ARGUS_SEARXNG_URLS when the deployment sets it.
func NewClient() *Client {
	var urls []string
	if override := os.Getenv("ARGUS_SEARXNG_URLS"); override != "" {
		for _, u := range strings.Split(override, ",") {
			if u = strings.TrimSpace(u); u != "" {
				urls = append(urls, u)
			}
		}
	}
	if len(urls) == 0 {
		urls = defaultInstances
	}
	return NewClientWith(urls)
}

// NewClientWith builds a client over an explicit pool; used by tests.
func NewClientWith(urls []string) *Client {
	c := &Client{}
	for _, u := range urls {
		if u = strings.TrimRight(u, "/"); u != "" {
			c.instances = append(c.instances, &instance{base: u})
		}
	}
	return c
}

// Search rotates the pool and queries every currently healthy instance until
// one answers. Instances used too recently (by any user of this client) or
// cooling down after a rate limit or bot check are skipped rather than
// queued, which keeps concurrent searches running without stacking waits.
func (c *Client) Search(ctx context.Context, query string, maxResults int) Answer {
	q := strings.Join(strings.Fields(query), " ")
	if q == "" {
		return Answer{Err: "empty query"}
	}
	if maxResults <= 0 {
		maxResults = defaultMaxResults
	}
	if maxResults > maxResultsCap {
		maxResults = maxResultsCap
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, maxAnswerDeadline)
		defer cancel()
	}

	for _, inst := range c.rotate() {
		if !inst.usable() {
			continue
		}
		inst.markUse()
		body, status, err := inst.query(ctx, q, maxResults)
		if ctx.Err() != nil {
			return Answer{Query: q, Err: "search timed out"}
		}
		if err != nil {
			inst.record(true, status)
			continue
		}
		parsed, parseErr := parseResponse(body)
		if parseErr != nil {
			if isBotCheck(body, status) {
				// Treat a bot-check page like an explicit block so the
				// instance's backoff escalates with every rejection.
				status = http.StatusForbidden
			}
			inst.record(true, status)
			continue
		}
		inst.record(false, status)
		return Answer{
			Query:   q,
			Results: trimResults(parsed.Results, maxResults),
			Answer:  firstString(parsed.Answers),
		}
	}
	return Answer{Query: q, Err: "every search instance is rate limited or unavailable right now; try again shortly"}
}

// rotate returns the pool starting at a different offset per call, so
// simultaneous users start on different instances.
func (c *Client) rotate() []*instance {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := len(c.instances)
	if n == 0 {
		return nil
	}
	c.next = (c.next + 1) % n
	out := make([]*instance, 0, n)
	out = append(out, c.instances[c.next:]...)
	out = append(out, c.instances[:c.next]...)
	return out
}

func (inst *instance) usable() bool {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	now := time.Now()
	return now.After(inst.blockedUntil) && now.Sub(inst.lastUse) >= minInstanceGap
}

func (inst *instance) markUse() {
	inst.mu.Lock()
	inst.lastUse = time.Now()
	inst.mu.Unlock()
}

func (inst *instance) record(failed bool, status int) {
	inst.mu.Lock()
	defer inst.mu.Unlock()
	if !failed && status == http.StatusOK {
		inst.failures = 0
		return
	}
	backoff := 10 * time.Second
	switch {
	case failed && status >= 500:
		inst.failures++
		backoff = min(2*time.Minute, 15*time.Second<<inst.failures)
	case status == http.StatusTooManyRequests || status == http.StatusForbidden:
		inst.failures++
		backoff = min(10*time.Minute, 5*time.Second<<(inst.failures-1))
	}
	inst.blockedUntil = time.Now().Add(backoff)
}

func (inst *instance) query(ctx context.Context, q string, maxResults int) ([]byte, int, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	params := url.Values{}
	params.Set("q", q)
	params.Set("format", "json")
	params.Set("safesearch", "1")
	params.Set("language", "en")
	params.Set("_", fmt.Sprint(time.Now().UnixMilli()))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, inst.base+"/search?"+params.Encode(), nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, bodyLimit))
	return body, res.StatusCode, err
}

type searxInstance struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content"`
}

type searxResponse struct {
	Results []searxInstance `json:"results"`
	Answers []string        `json:"answers"`
}

func parseResponse(body []byte) (*searxResponse, error) {
	if isBotCheck(body, 0) {
		return nil, fmt.Errorf("instance answered with a bot-check page")
	}
	var out searxResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("instance did not answer with JSON")
	}
	return &out, nil
}

var botMarkers = []string{
	"verifying your browser", "not a bot", "making sure you", "anubis",
	"security check", "just a moment", "captcha", "challenge",
}

func isBotCheck(body []byte, status int) bool {
	if status == http.StatusForbidden {
		return true
	}
	if len(body) == 0 {
		return false
	}
	low := strings.ToLower(string(body[:min(600, len(body))]))
	for _, m := range botMarkers {
		if strings.Contains(low, m) {
			return true
		}
	}
	return false
}

func trimResults(in []searxInstance, limit int) []Result {
	out := make([]Result, 0, limit)
	for _, r := range in {
		if strings.TrimSpace(r.URL) == "" {
			continue
		}
		title := strings.TrimSpace(r.Title)
		if title == "" {
			title = r.URL
		}
		snippet := strings.Join(strings.Fields(r.Content), " ")
		if len(snippet) > snippetLimit {
			snippet = snippet[:snippetLimit] + "…"
		}
		out = append(out, Result{Title: title, URL: strings.TrimSpace(r.URL), Snippet: snippet})
		if len(out) == limit {
			break
		}
	}
	return out
}

func firstString(in []string) string {
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			return s
		}
	}
	return ""
}

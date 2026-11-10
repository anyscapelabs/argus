// The router-backed search client. The desktop app's default provider is the
// Argus Router (see Shared.RouterProviderID), and the router itself owns web
// search — it routes each query across Brave, Tavily and Exa with fallback and
// plan quota. This client is the desktop's half of that contract: it calls the
// router's /v1/search endpoint with the same service-account key the model calls
// use, so one credential and one quota govern both chat and search.
package Search

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// routerSearchPath is appended to the router's base URL (which already ends in
// /v1) to reach the search endpoint. The base "http://host:8080/v1" plus
// "/search" is exactly "http://host:8080/v1/search".
const routerSearchPath = "/search"

// RouterClient queries the Argus Router's web search endpoint.
type RouterClient struct {
	// Base is the router base URL, including its /v1 suffix.
	Base string
	// Key is the service-account key sent as a bearer token.
	Key  string
	HTTP *http.Client
}

// RouterAnswer is the response the router returns for one query.
type RouterAnswer struct {
	Query    string         `json:"query"`
	Provider string         `json:"provider"`
	Results  []RouterResult `json:"results"`
}

// RouterResult is one hit as the router reports it.
type RouterResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
	Content string `json:"content,omitempty"`
}

// Search runs one query against the router. It returns an Answer with Err set
// only when the router could not be reached or refused the query; a successful
// call with no results means the query genuinely had no hits.
func (c *RouterClient) Search(ctx context.Context, query string, maxResults int) Answer {
	q := strings.Join(strings.Fields(query), " ")
	if q == "" {
		return Answer{Err: "empty query"}
	}
	if maxResults <= 0 {
		maxResults = defaultMaxResults
	}
	if maxResults > 20 {
		maxResults = 20
	}
	body, err := json.Marshal(map[string]any{"query": q, "max_results": maxResults})
	if err != nil {
		return Answer{Query: q, Err: "could not build the search request"}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.searchURL(), bytes.NewReader(body))
	if err != nil {
		return Answer{Query: q, Err: "the search endpoint is misconfigured"}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Key)

	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: maxAnswerDeadline}
	}
	resp, err := client.Do(req)
	if err != nil {
		return Answer{Query: q, Err: "search is unreachable right now: " + err.Error()}
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, bodyLimit))
	if err != nil {
		return Answer{Query: q, Err: "could not read the search response"}
	}
	if resp.StatusCode != http.StatusOK {
		return Answer{Query: q, Err: "search failed (HTTP " + http.StatusText(resp.StatusCode) + ")"}
	}

	var out RouterAnswer
	if err := json.Unmarshal(raw, &out); err != nil {
		return Answer{Query: q, Err: "the search response could not be read"}
	}
	results := make([]Result, 0, len(out.Results))
	for _, r := range out.Results {
		snippet := r.Snippet
		if snippet == "" {
			snippet = r.Content
		}
		snippet = truncateUTF8(snippet, snippetLimit)
		results = append(results, Result{Title: r.Title, URL: r.URL, Snippet: snippet})
	}
	return Answer{Query: q, Results: results}
}

// searchURL builds the router's search endpoint from the base URL.
func (c *RouterClient) searchURL() string {
	base := strings.TrimRight(c.Base, "/")
	// The base already carries /v1; a bare host needs it appended.
	if !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	u, err := url.Parse(base + routerSearchPath)
	if err != nil {
		return base + routerSearchPath
	}
	return u.String()
}

// truncateUTF8 cuts a string at a UTF-8 boundary, appending an ellipsis when
// it actually cut something.
func truncateUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	i := max
	for i > 0 && i < len(s) && (s[i]&0xC0) == 0x80 {
		i--
	}
	return s[:i] + "…"
}

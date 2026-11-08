package Shared

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/anyscapelabs/argus/Keyring"
	"github.com/anyscapelabs/argus/Provider"
	"github.com/anyscapelabs/argus/Storage"
)

const (
	listTimeout      = 20 * time.Second
	listBodyCap      = int64(8 << 20)
	anthropicVersion = "2023-06-01"
	maxListPages     = 50
	liveTTL          = 15 * time.Minute
)

type LiveModel struct {
	ID         string
	Name       string
	ContextLen int64
	MaxOutput  int64
}

type LiveResults struct {
	ByID      map[string]LiveModel
	FetchedAt time.Time
	Stale     bool
}

type liveEntry struct {
	results *LiveResults
	err     *ActionError
	stale   bool
}

var listClient = &http.Client{
	Timeout: listTimeout,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return fmt.Errorf("redirects are not allowed for list-models calls")
	},
}

func listEndpoint(baseURL, modelsPath string) string {
	return strings.TrimSuffix(baseURL, "/") + modelsPath
}

func doList(ctx context.Context, method, url string, headers http.Header) ([]byte, *ActionError) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, NewError(CodeBadResponse, "The provider list request could not be built")
	}
	for k, vs := range headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	resp, err := listClient.Do(req)
	if err != nil {
		return nil, NewError(CodeNetwork, "Could not reach the provider. Check your connection and try again")
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		retry := strings.TrimSpace(resp.Header.Get("Retry-After"))
		if retry != "" {
			return nil, &ActionError{Code: string(CodeRateLimited), Message: "Rate limited by the provider. Retry after " + retry + " seconds"}
		}
		return nil, NewError(CodeRateLimited, "Rate limited by the provider. Try again shortly")
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, NewError(CodeInvalidKey, "The provider rejected this API key")
	}
	if resp.StatusCode >= 500 {
		return nil, NewError(CodeProviderError, "The provider had an internal error. Try again shortly")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, NewError(CodeBadResponse, fmt.Sprintf("The provider returned an unexpected status (%d)", resp.StatusCode))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, listBodyCap+1))
	if err != nil {
		return nil, NewError(CodeNetwork, "Could not read the provider response")
	}
	if int64(len(body)) > listBodyCap {
		return nil, NewError(CodeBadResponse, "The provider response was too large")
	}
	return body, nil
}

func listHeaders(extra map[string]string) http.Header {
	h := http.Header{}
	for k, v := range extra {
		h.Set(k, v)
	}
	return h
}

type openAIModelPage struct {
	ID            string  `json:"id"`
	DisplayName   string  `json:"display_name"`
	Name          string  `json:"name"`
	OwnedBy       string  `json:"owned_by"`
	ContextLength float64 `json:"context_length"`
	ContextWindow float64 `json:"context_window"`
	MaxModelLen   float64 `json:"max_model_len"`
	Type          string  `json:"type"`
}

func fetchOpenAICompat(ctx context.Context, provider Provider.Provider, override string, extraHeaders map[string]string, key string) ([]LiveModel, *ActionError) {
	headers := listHeaders(extraHeaders)
	headers.Set("Authorization", "Bearer "+key)
	body, aerr := doList(ctx, http.MethodGet, listEndpoint(listBase(provider, override), provider.ModelsPath), headers)
	if aerr != nil {
		return nil, aerr
	}
	var envelope struct {
		Data []openAIModelPage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil && envelope.Data != nil {
		return keepChat(envelope.Data), nil
	}
	var bare []openAIModelPage
	if err := json.Unmarshal(body, &bare); err == nil {
		return keepChat(bare), nil
	}
	return nil, NewError(CodeBadResponse, "The provider response could not be parsed")
}

func keepChat(in []openAIModelPage) []LiveModel {
	out := make([]LiveModel, 0, len(in))
	for _, m := range in {
		if !isChatType(m.Type) {
			continue
		}
		name := firstNonEmpty(m.DisplayName, m.Name)
		if name == "" {
			name = m.ID
		}
		out = append(out, LiveModel{
			ID:         m.ID,
			Name:       name,
			ContextLen: int64FromFloat(pickPositiveNumber(m.ContextLength, m.ContextWindow, m.MaxModelLen)),
		})
	}
	return out
}

func listBase(provider Provider.Provider, override string) string {
	if override != "" {
		return override
	}
	return provider.BaseURL
}

func fetchAnthropic(ctx context.Context, provider Provider.Provider, override string, extraHeaders map[string]string, key string) ([]LiveModel, *ActionError) {
	headers := listHeaders(extraHeaders)
	headers.Set("x-api-key", key)
	headers.Set("anthropic-version", anthropicVersion)
	out := make([]LiveModel, 0, 32)
	cursor := ""
	for page := 0; page < maxListPages; page++ {
		url := listEndpoint(listBase(provider, override), provider.ModelsPath) + "?limit=1000"
		if cursor != "" {
			url += "&after_id=" + cursor
		}
		body, aerr := doList(ctx, http.MethodGet, url, headers)
		if aerr != nil {
			return nil, aerr
		}
		var response struct {
			Data []struct {
				ID          string `json:"id"`
				DisplayName string `json:"display_name"`
			} `json:"data"`
			HasMore bool   `json:"has_more"`
			LastID  string `json:"last_id"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, NewError(CodeBadResponse, "The provider response could not be parsed")
		}
		for _, m := range response.Data {
			out = append(out, LiveModel{ID: m.ID, Name: firstNonEmpty(m.DisplayName, m.ID)})
		}
		if !response.HasMore || response.LastID == "" {
			break
		}
		cursor = response.LastID
	}
	return out, nil
}

func fetchGemini(ctx context.Context, provider Provider.Provider, override string, extraHeaders map[string]string, key string) ([]LiveModel, *ActionError) {
	// The google provider now goes through the OpenAI-compatible endpoint
	// (see Provider/overlay.json), which lists models in the OpenAI shape.
	// fetchList routes AdapterGemini here only for backward compatibility
	// with any other gemini-adapter providers that still use the native
	// noun shape; keep both paths alive.
	headers := listHeaders(extraHeaders)
	headers.Set("x-goog-api-key", key)
	base := listEndpoint(listBase(provider, override), provider.ModelsPath)
	if !strings.Contains(base, "?") {
		base += "?pageSize=1000"
	}
	out := make([]LiveModel, 0, 32)
	token := ""
	for page := 0; page < maxListPages; page++ {
		url := base
		if token != "" {
			url += "&pageToken=" + token
		}
		body, aerr := doList(ctx, http.MethodGet, url, headers)
		if aerr != nil {
			return nil, aerr
		}
		if err := json.Unmarshal(body, &struct{ Models []json.RawMessage `json:"models"` }{}); err != nil {
			// Not the native Gemini shape; the OpenAI-compatible endpoint
			// returns [{id,name}] instead.
			var envelope struct {
				Data []openAIModelPage `json:"data"`
			}
			if err := json.Unmarshal(body, &envelope); err != nil || envelope.Data == nil {
				return nil, NewError(CodeBadResponse, "The provider response could not be parsed")
			}
			return keepChat(envelope.Data), nil
		}
		var response struct {
			Models []struct {
				Name        string   `json:"name"`
				DisplayName string   `json:"displayName"`
				InputLimit  int64    `json:"inputTokenLimit"`
				OutputLimit int64    `json:"outputTokenLimit"`
			} `json:"models"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, NewError(CodeBadResponse, "The provider response could not be parsed")
		}
		for _, m := range response.Models {
			out = append(out, LiveModel{
				ID:         strings.TrimPrefix(m.Name, "models/"),
				Name:       m.DisplayName,
				ContextLen: m.InputLimit,
				MaxOutput:  m.OutputLimit,
			})
		}
		if response.NextPageToken == "" {
			break
		}
		token = response.NextPageToken
	}
	return out, nil
}

func isChatType(t string) bool {
	t = strings.ToLower(strings.TrimSpace(t))
	if t == "" {
		return true
	}
	for _, e := range []string{"embedding", "embed", "image", "audio", "speech", "transcription", "rerank", "moderation", "realtime", "guard", "classifier"} {
		if strings.Contains(t, e) {
			return false
		}
	}
	return true
}


func pickPositiveNumber(values ...float64) float64 {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}

func int64FromFloat(f float64) int64 {
	if f <= 0 {
		return 0
	}
	return int64(f)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func fetchList(ctx context.Context, provider Provider.Provider, extraHeaders map[string]string, override string, key string) ([]LiveModel, *ActionError) {
	if provider.ModelsPath == "" {
		return nil, NewError(CodeBadResponse, "This provider has no usable list endpoint")
	}
	switch provider.Adapter {
	case Provider.AdapterAnthropic:
		return fetchAnthropic(ctx, provider, override, extraHeaders, key)
	case Provider.AdapterGemini:
		return fetchGemini(ctx, provider, override, extraHeaders, key)
	default:
		return fetchOpenAICompat(ctx, provider, override, extraHeaders, key)
	}
}

func (c *Core) lockFor(providerID string) *sync.Mutex {
	v, _ := c.lockCS.LoadOrStore(providerID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

func (c *Core) entryFor(providerID string) *liveEntry {
	c.liveMu.Lock()
	defer c.liveMu.Unlock()
	e, ok := c.live[providerID]
	if !ok {
		e = &liveEntry{}
		c.live[providerID] = e
	}
	return e
}

func (c *Core) FetchLive(ctx context.Context, provider Provider.Provider, extraHeaders map[string]string, cfg Storage.ProviderConfig, refresh bool) *LiveResults {
	lock := c.lockFor(provider.ID)
	lock.Lock()
	defer lock.Unlock()
	e := c.entryFor(provider.ID)
	if e.results != nil && !refresh && time.Since(e.results.FetchedAt) < liveTTL {
		return e.results
	}
	key := ""
	if !provider.NoAuth {
		stored, kerr := Keyring.GetFor(provider.ID)
		if kerr != nil {
			if errors.Is(kerr, Keyring.ErrNotFound) {
				e.err = NewError(CodeNotConfigured, "Connect "+provider.ID+" first. Paste the API key in Settings")
			} else {
				e.err = NewError(CodeStorage, "The secrets store is unreachable. Re-enter the key")
			}
			e.results = nil
			e.stale = false
			return nil
		}
		key = stored
	}
	models, aerr := fetchList(ctx, provider, extraHeaders, cfg.BaseURLOverride, key)
	if aerr == nil {
		// Merge the entitlements allowlist so a model the router knows but
		// /v1/models omitted (or a stale /v1/models response) is still
		// visible; the router enforces the allowlist server-side anyway.
		models = mergeAllowlist(models, c.allowlist())
		res := &LiveResults{ByID: indexLiveModels(models), FetchedAt: time.Now()}
		e.results = res
		e.err = nil
		e.stale = false
		return res
	}
	if e.results != nil {
		res := *e.results
		res.Stale = true
		e.err = aerr
		e.stale = true
		return &res
	}
	// No network results yet: fall back to the cached allowlist snapshot so
	// the picker still works offline within the grace window.
	if allow := c.allowlist(); len(allow) > 0 {
		res := &LiveResults{ByID: indexLiveModels(allow), FetchedAt: time.Now(), Stale: true}
		e.results = res
		e.err = aerr
		e.stale = true
		return res
	}
	e.err = aerr
	e.results = nil
	e.stale = false
	return nil
}

func (c *Core) allowlist() []LiveModel {
	if c.DB == nil {
		return nil
	}
	payload, _, found, err := c.DB.GetAccountEntitlements()
	if err != nil || !found || payload == "" {
		return nil
	}
	var ent struct {
		Allowlist []any `json:"allowlist"`
	}
	if err := json.Unmarshal([]byte(payload), &ent); err != nil {
		return nil
	}
	out := make([]LiveModel, 0, len(ent.Allowlist))
	for _, item := range ent.Allowlist {
		switch v := item.(type) {
		case string:
			id := strings.TrimSpace(v)
			if id == "" {
				continue
			}
			out = append(out, LiveModel{ID: id, Name: id})
		case map[string]any:
			id, _ := v["id"].(string)
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			name, _ := v["display_name"].(string)
			if name == "" {
				name, _ = v["name"].(string)
			}
			if name == "" {
				name = id
			}
			out = append(out, LiveModel{ID: id, Name: name})
		}
	}
	return out
}

func mergeAllowlist(live, allow []LiveModel) []LiveModel {
	if len(allow) == 0 {
		return live
	}
	seen := map[string]bool{}
	for _, m := range live {
		seen[strings.ToLower(m.ID)] = true
	}
	out := live
	for _, m := range allow {
		if !seen[strings.ToLower(m.ID)] {
			out = append(out, m)
			seen[strings.ToLower(m.ID)] = true
		}
	}
	return out
}

func (c *Core) LiveError(providerID string) *ActionError {
	c.liveMu.Lock()
	defer c.liveMu.Unlock()
	if e, ok := c.live[providerID]; ok {
		return e.err
	}
	return nil
}

func (c *Core) DropLive(providerID string) {
	c.liveMu.Lock()
	defer c.liveMu.Unlock()
	delete(c.live, providerID)
	// keep the per-provider mutex to avoid concurrent fetches
}

func indexLiveModels(models []LiveModel) map[string]LiveModel {
	out := make(map[string]LiveModel, len(models))
	for _, m := range models {
		out[strings.ToLower(m.ID)] = m
	}
	return out
}

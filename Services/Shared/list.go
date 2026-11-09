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
	"github.com/anyscapelabs/argus/Net"
	"github.com/anyscapelabs/argus/Provider"
	"github.com/anyscapelabs/argus/Storage"
)

const (
	listTimeout = 20 * time.Second
	listBodyCap = int64(8 << 20)
	liveTTL     = 15 * time.Minute
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
	resp, err := Net.Client(Net.Oneshot).Do(req)
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

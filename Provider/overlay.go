package Provider

import (
	"embed"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strings"
)

//go:embed overlay.json
var overlayFS embed.FS

func Load() (*Overlay, error) {
	body, err := overlayFS.ReadFile("overlay.json")
	if err != nil {
		return nil, err
	}
	overlay, err := ParseOverlay(body)
	if err != nil {
		return nil, err
	}
	if err := Validate(overlay); err != nil {
		return nil, err
	}
	return overlay, nil
}

func Validate(o *Overlay) error {
	if len(o.Providers) == 0 {
		return fmt.Errorf("overlay has no providers")
	}
	seen := map[string]bool{}
	for _, p := range o.Providers {
		if p.ID == "" {
			return fmt.Errorf("overlay provider with empty id")
		}
		if seen[p.ID] {
			return fmt.Errorf("overlay duplicate provider id %s", p.ID)
		}
		seen[p.ID] = true
		switch p.Adapter {
		case AdapterOpenAICompat, AdapterAnthropic, AdapterGemini:
		default:
			return fmt.Errorf("provider %s has unknown adapter %q", p.ID, p.Adapter)
		}
		u, err := url.Parse(p.BaseURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("provider %s has invalid base_url %q", p.ID, p.BaseURL)
		}
		if u.Scheme == "http" && !isLocalHost(u.Host) {
			return fmt.Errorf("provider %s must use https", p.ID)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return fmt.Errorf("provider %s has unsupported scheme %q", p.ID, u.Scheme)
		}
		for _, pat := range append(append([]string{}, p.LiveExclude...), p.Hide...) {
			if _, err := path.Match(pat, "x"); err != nil {
				return fmt.Errorf("provider %s has invalid pattern %q", p.ID, pat)
			}
		}
	}
	for _, pat := range o.DefaultLiveExclude {
		if _, err := path.Match(pat, "x"); err != nil {
			return fmt.Errorf("overlay has invalid default exclude pattern %q", pat)
		}
	}
	return nil
}

func isLocalHost(host string) bool {
	h := strings.ToLower(host)
	if i := strings.LastIndex(h, ":"); i >= 0 {
		h = h[:i]
	}
	return h == "localhost" || h == "127.0.0.1" || h == "::1"
}

func (o *Overlay) ModelsPath(providerID string) string {
	if p, ok := o.Lookup(providerID); ok && p.ModelsPath != "" {
		return p.ModelsPath
	}
	if p, ok := o.Lookup(providerID); ok {
		switch p.Adapter {
		case AdapterAnthropic:
			return "/v1/models"
		default:
			return "/models"
		}
	}
	return "/models"
}

func (o *Overlay) Resolved(providerData map[string]bool) []Provider {
	providers := make([]Provider, 0, len(o.Providers))
	for _, op := range o.Providers {
		providers = append(providers, Provider{
			ID:            op.ID,
			Name:          op.Name(),
			Adapter:       op.Adapter,
			BaseURL:       op.BaseURL,
			ModelsPath:    o.ModelsPath(op.ID),
			NoAuth:        op.NoAuth,
			KeyHint:       op.KeyHint,
			KeyHelpURL:    op.KeyHelpURL,
			DocURL:        op.DocURL,
			Order:         op.Order,
			Logo:          op.Logo,
			InCatalogData: providerData[op.ID],
		})
	}
	sort.SliceStable(providers, func(i, j int) bool { return providers[i].Order < providers[j].Order })
	return providers
}

func (o *Overlay) RecommendedFor(providerID string) (Recommended, bool) {
	if p, ok := o.Lookup(providerID); ok {
		return p.Recommended, true
	}
	return Recommended{}, false
}

func (o *Overlay) Ordered() []OverlayProvider {
	providers := append([]OverlayProvider{}, o.Providers...)
	sort.SliceStable(providers, func(i, j int) bool { return providers[i].Order < providers[j].Order })
	return providers
}

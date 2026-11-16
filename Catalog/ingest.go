package Catalog

import (
	"fmt"
	"time"

	"github.com/anyscapelabs/argus/Models"
	"github.com/anyscapelabs/argus/Provider"
)

// Build constructs the catalog from the provider overlay only. The desktop
// app no longer syncs models.dev: the single argus-router provider's model
// list comes from the router at runtime (GET /v1/models, allowlist-filtered),
// so the catalog carries provider metadata only and zero model rows.
func Build(o *Provider.Overlay, buildTime time.Time) (*Catalog, error) {
	if o == nil {
		return nil, fmt.Errorf("catalog: nil overlay")
	}
	cat := &Catalog{
		providers:  make([]Provider.Provider, 0, len(o.Providers)),
		models:     make(map[string]Models.Model),
		byProvider: make(map[string][]Models.Model),
		report: BuildReport{
			BuildTime:       buildTime.UTC(),
			ProvidersNoData: []string{},
			Warnings:        []string{},
		},
	}
	for _, op := range o.Ordered() {
		provider := Provider.Provider{
			ID:         op.ID,
			Name:       op.Name(),
			Adapter:    op.Adapter,
			BaseURL:    op.BaseURL,
			ModelsPath: op.ModelsPath,
			NoAuth:     op.NoAuth,
			KeyHint:    op.KeyHint,
			KeyHelpURL: op.KeyHelpURL,
			DocURL:     op.DocURL,
			Order:      op.Order,
			Logo:       op.Logo,
		}
		if op.ID != "" {
			cat.providers = append(cat.providers, provider)
		}
	}
	cat.report.ProviderCount = len(cat.providers)
	cat.report.ModelCount = len(cat.models)
	return cat, nil
}

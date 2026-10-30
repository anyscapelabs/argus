package Catalog

import (
	"sort"
	"strings"
	"time"

	"github.com/anyscapelabs/argus/Models"
	"github.com/anyscapelabs/argus/Provider"
)

type BuildReport struct {
	BuildTime         time.Time `json:"build_time"`
	ProviderCount     int       `json:"provider_count"`
	ModelCount        int       `json:"model_count"`
	ProvidersNoData   []string  `json:"providers_no_data"`
	Warnings          []string  `json:"warnings"`
	WarningsTruncated bool      `json:"warnings_truncated"`
}

type Catalog struct {
	providers  []Provider.Provider
	models     map[string]Models.Model
	byProvider map[string][]Models.Model
	report     BuildReport
}

func (c *Catalog) Providers() []Provider.Provider {
	out := make([]Provider.Provider, len(c.providers))
	copy(out, c.providers)
	return out
}

func (c *Catalog) Provider(id string) (Provider.Provider, bool) {
	needle := strings.ToLower(id)
	for _, p := range c.providers {
		if strings.ToLower(p.ID) == needle {
			return p, true
		}
	}
	return Provider.Provider{}, false
}

func (c *Catalog) ByProvider(id string) []Models.Model {
	out := make([]Models.Model, len(c.byProvider[id]))
	copy(out, c.byProvider[id])
	return out
}

func (c *Catalog) All() []Models.Model {
	out := make([]Models.Model, 0, len(c.models))
	for _, p := range c.providers {
		out = append(out, c.byProvider[p.ID]...)
	}
	return out
}

func (c *Catalog) Get(ref string) (Models.Model, bool) {
	m, ok := c.models[strings.ToLower(ref)]
	return m, ok
}

func (c *Catalog) Count() int { return len(c.models) }

func (c *Catalog) Report() BuildReport { return c.report }

func SortModels(models []Models.Model) {
	sort.SliceStable(models, func(i, j int) bool {
		ki, kj := DateKey(models[i].Released), DateKey(models[j].Released)
		if ki != kj {
			return ki > kj
		}
		if models[i].Name != models[j].Name {
			return models[i].Name < models[j].Name
		}
		return models[i].ID < models[j].ID
	})
}

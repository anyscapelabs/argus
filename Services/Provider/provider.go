package Providers

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/anyscapelabs/argus/Catalog"
	"github.com/anyscapelabs/argus/Provider"
	"github.com/anyscapelabs/argus/Services/Shared"
)

type Service struct {
	Core *Shared.Core
}

func (s *Service) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	return s.Core.Bootstrap(ctx)
}

// ListProviders returns the single argus-router provider. There is no BYO
// provider management anymore: pairing an argus_pk_ key (Services/Account)
// is what connects the router.
func (s *Service) ListProviders() []Shared.ProviderView {
	out := []Shared.ProviderView{}
	if err := s.Core.Ready(); err != nil {
		return out
	}
	cat := s.Core.Store.Get()
	for _, p := range cat.Providers() {
		out = append(out, s.ProviderView(cat, p))
	}
	return out
}

func (s *Service) ProviderView(cat *Catalog.Catalog, p Provider.Provider) Shared.ProviderView {
	return Shared.ProviderView{
		ID:         p.ID,
		Name:       p.Name,
		Logo:       p.Logo,
		Monogram:   Monogram(p.Name),
		NoAuth:     p.NoAuth,
		KeyHint:    p.KeyHint,
		KeyHelpURL: p.KeyHelpURL,
		DocURL:     p.DocURL,
		Configured: true,
		ModelCount: len(cat.ByProvider(p.ID)),
		Order:      p.Order,
	}
}

// ValidateAndSaveKey is retained for staged removal. Under the router there
// are no per-provider keys to save; use Services/Account.SaveKey instead.
func (s *Service) ValidateAndSaveKey(providerID, key string, refs []string, baseURLOverride string) Shared.SaveKeyResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.SaveKeyResult{Error: aerr}
	}
	if _, ok := s.Core.Overlay.Lookup(providerID); !ok {
		return Shared.SaveKeyResult{Error: Shared.NewError(Shared.CodeUnknownProvider, "Unknown provider")}
	}
	return Shared.SaveKeyResult{Ok: true}
}

// RemoveKey is retained for staged removal: there is nothing per-provider to
// remove under the router. Use Services/Account.DeleteKey instead.
func (s *Service) RemoveKey(providerID string) Shared.ActionResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.ActionResult{Error: aerr}
	}
	if _, ok := s.Core.Overlay.Lookup(providerID); !ok {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeUnknownProvider, "Unknown provider")}
	}
	return Shared.ActionResult{Ok: true}
}

func LastFour(key string) string {
	if len(key) <= 4 {
		return key
	}
	return key[len(key)-4:]
}

func NormalizeKey(key string) (string, *Shared.ActionError) {
	return key, nil
}

func Monogram(name string) string {
	return "A"
}

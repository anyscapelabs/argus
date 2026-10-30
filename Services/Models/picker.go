package Models

import (
	"context"
	"fmt"
	"strings"

	"github.com/anyscapelabs/argus/Catalog"
	"github.com/anyscapelabs/argus/Models"
	"github.com/anyscapelabs/argus/Provider"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
)

func Monogram(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "?"
	}
	return strings.ToUpper(string([]rune(name)[0]))
}

type Service struct {
	Core *Shared.Core
}

func (s *Service) Picker(providerID string, options Shared.PickerOptions) Shared.PickerResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.PickerResult{Error: aerr}
	}
	cat := s.Core.Store.Get()
	provider, found := cat.Provider(providerID)
	if !found {
		return Shared.PickerResult{Error: Shared.NewError(Shared.CodeUnknownProvider, "Unknown provider")}
	}
	overlayProvider, okOverlay := s.Core.Overlay.Lookup(providerID)
	if !okOverlay {
		overlayProvider = Provider.OverlayProvider{ID: providerID}
	}
	config, configured, _ := s.Core.DB.GetProviderConfig(providerID)

	var live *Shared.LiveResults
	var liveError *Shared.ActionError
	liveChecked := false
	stale := false
	if configured {
		entry := s.Core.FetchLive(context.Background(), provider, overlayProvider.ExtraHeaders, config, options.RefreshLive)
		if entry != nil {
			live = entry
			liveChecked = true
			liveError = s.Core.LiveError(providerID)
			stale = entry.Stale
		}
	}

	view := s.Assemble(cat, provider, overlayProvider, config, configured, live, liveError, liveChecked, stale, options)
	return Shared.PickerResult{Ok: true, Picker: &view}
}

func (s *Service) Assemble(cat *Catalog.Catalog, provider Provider.Provider, overlayProvider Provider.OverlayProvider, config Storage.ProviderConfig, configured bool, live *Shared.LiveResults, liveError *Shared.ActionError, liveChecked bool, stale bool, options Shared.PickerOptions) Shared.Picker {
	view := Shared.Picker{
		Provider: s.ProviderHead(cat, provider, config, configured),
	}
	status := s.Core.Store.Status()
	view.CatalogSource = status.Source
	view.LiveChecked = liveChecked
	view.LiveError = liveError
	view.Stale = stale

	available := make([]Shared.ModelView, 0, 32)
	other := make([]Shared.ModelView, 0, 16)
	if configured && liveChecked {
		for _, lm := range live.ByID {
			full, inCatalog := cat.Get(Models.MakeRef(provider.ID, lm.ID))
			if inCatalog {
				available = append(available, CatalogView(full, Shared.AvailabilityAvailable, s.selectedFor(full.Ref)))
				continue
			}
			if ExcludedLiveOnly(s.Core.Overlay, overlayProvider, lm.ID, lm.Name) {
				continue
			}
			other = append(other, LiveView(provider, lm))
		}
	} else {
		for _, full := range cat.ByProvider(provider.ID) {
			available = append(available, CatalogView(full, Shared.AvailabilityUnverified, s.selectedFor(full.Ref)))
		}
	}

	customs := s.CustomViews(provider.ID)

	candidates := catalogSubset(cat, provider.ID, liveChecked, live)
	recMain, recCheap := Catalog.RecommendFrom(candidates, s.overlayRecommended(provider.ID))
	view.Recommended.Main = recMain.Ref
	view.Recommended.Cheap = recCheap.Ref

	applyRecommended(available, recMain.Ref, recCheap.Ref)
	all := mergeViews(available, other, customs)
	applySearch(all, options.Search)
	applyToolsOnly(all, options.ToolsOnly)
	applyBetaHandicap(all, options.IncludeBeta)

	var sections []Shared.Section
	if recommendedModels(s, all, recMain.Ref, recCheap.Ref, options) != nil {
		sections = append(sections, Shared.Section{ID: "recommended", Title: "Recommended", Models: recommendedModels(s, all, recMain.Ref, recCheap.Ref, options)})
	}
	if models := pickAvailability(all, Shared.AvailabilityAvailable, Shared.AvailabilityUnverified); len(models) > 0 || len(available) > 0 {
		sections = append(sections, Shared.Section{ID: "available", Title: "All available models", Models: models})
	}
	if models := pickAvailability(all, Shared.AvailabilityCustom); len(models) > 0 {
		sections = append(sections, Shared.Section{ID: "custom", Title: "Your custom models", Models: models})
	}
	if models := pickAvailability(all, Shared.AvailabilityLiveOnly); len(models) > 0 {
		sections = append(sections, Shared.Section{
			ID:       "other",
			Title:    "Other models",
			Subtitle: fmt.Sprintf("Offered by %s but not in our catalog, so details are unknown", provider.Name),
			Models:   models,
		})
	}

	view.Sections = sections
	view.TotalCount = len(viewRefSet(all))
	return view
}

func recommendedModels(s *Service, all []Shared.ModelView, mainRef, cheapRef string, options Shared.PickerOptions) []Shared.ModelView {
	if searchActive(options) {
		return nil
	}
	var out []Shared.ModelView
	if view, ok := viewRef(all, mainRef); ok {
		out = append(out, view)
	}
	if cheapRef != "" && !strings.EqualFold(mainRef, cheapRef) {
		if view, ok := viewRef(all, cheapRef); ok {
			out = append(out, view)
		}
	}
	return out
}

func (s *Service) ProviderHead(cat *Catalog.Catalog, provider Provider.Provider, config Storage.ProviderConfig, configured bool) Shared.ProviderView {
	view := Shared.ProviderView{
		ID:         provider.ID,
		Name:       provider.Name,
		Logo:       provider.Logo,
		Monogram:   Monogram(provider.Name),
		NoAuth:     provider.NoAuth,
		KeyHint:    provider.KeyHint,
		KeyHelpURL: provider.KeyHelpURL,
		DocURL:     provider.DocURL,
		Configured: configured,
		ModelCount: len(cat.ByProvider(provider.ID)),
		Order:      provider.Order,
	}
	if configured {
		view.KeyLast4 = config.KeyLast4
		view.ValidatedAt = config.ValidatedAt
	}
	return view
}

func (s *Service) overlayRecommended(providerID string) Provider.Recommended {
	if p, ok := s.Core.Overlay.Lookup(providerID); ok {
		return p.Recommended
	}
	return Provider.Recommended{}
}

func (s *Service) selectedFor(ref string) []string {
	slots, err := s.Core.DB.ListModelSlots()
	if err != nil {
		return nil
	}
	var out []string
	for _, slot := range slots {
		if strings.EqualFold(slot.Ref, ref) {
			out = append(out, slot.Slot)
		}
	}
	return out
}

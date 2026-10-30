package Models

import (
	"path"
	"strings"

	"github.com/anyscapelabs/argus/Catalog"
	"github.com/anyscapelabs/argus/Models"
	"github.com/anyscapelabs/argus/Provider"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
)

func makeRef(providerID, modelID string) string {
	return providerID + "/" + modelID
}

func CatalogView(m Models.Model, availability string, selectedFor []string) Shared.ModelView {
	view := Shared.ModelView{
		Ref:           m.Ref,
		ProviderID:    m.ProviderID,
		ID:            m.ID,
		Name:          m.Name,
		Family:        m.Family,
		Availability:  availability,
		Released:      m.Released,
		ReleasedLabel: Catalog.ReleasedLabel(m.Released),
		Price:         m.Cost,
		PriceLabel:    Catalog.PriceLabel(m.Cost),
		SelectedFor:   selectedFor,
		OpenWeights:   m.OpenWeights,
		Status:        string(m.Status),
	}
	if m.Limits != nil {
		view.ContextWindow = m.Limits.Context
		view.MaxOutput = m.Limits.Output
	}
	view.ContextLabel = Catalog.ContextLabel(view.ContextWindow)
	view.MaxOutputLabel = Catalog.ContextLabel(view.MaxOutput)
	if m.Capabilities != nil {
		view.Caps = *m.Capabilities
	}
	view.CapsKnown = m.CapsKnown
	view.Capabilities = Catalog.CapabilityBadges(m.Capabilities, m.CapsKnown)
	if eligible, reason := Catalog.EligibleMain(m); eligible {
		view.EligibleMain = true
	} else {
		view.IneligibleReason = reason
	}
	if eligible, _ := Catalog.EligibleCheap(m); eligible {
		view.EligibleCheap = true
	}
	return view
}

func LiveView(provider Provider.Provider, lm Shared.LiveModel) Shared.ModelView {
	view := Shared.ModelView{
		Ref:          makeRef(provider.ID, lm.ID),
		ProviderID:   provider.ID,
		ID:           lm.ID,
		Name:         lm.Name,
		Availability: Shared.AvailabilityLiveOnly,
	}
	view.ContextWindow = lm.ContextLen
	view.MaxOutput = lm.MaxOutput
	view.ContextLabel = Catalog.ContextLabel(view.ContextWindow)
	view.MaxOutputLabel = Catalog.ContextLabel(view.MaxOutput)
	view.PriceLabel = "Price not listed"
	probe := Models.Model{Source: Models.SourceLive, OutputModality: []string{"text"}, CapsKnown: false}
	if eligible, reason := Catalog.EligibleMain(probe); eligible {
		view.EligibleMain = true
	} else {
		view.IneligibleReason = reason
	}
	view.EligibleCheap = true
	return view
}

func CustomView(cm Storage.CustomModel, selectedFor []string) Shared.ModelView {
	view := Shared.ModelView{
		Ref:          cm.Ref(),
		ProviderID:   cm.ProviderID,
		ID:           cm.ModelID,
		Name:         cm.DisplayName,
		Availability: Shared.AvailabilityCustom,
		SelectedFor:  selectedFor,
	}
	view.ContextWindow = cm.ContextWindow
	view.MaxOutput = cm.MaxOutput
	view.ContextLabel = Catalog.ContextLabel(view.ContextWindow)
	view.MaxOutputLabel = Catalog.ContextLabel(view.MaxOutput)
	view.Caps = Models.Capabilities{
		Tools:     cm.Tools,
		Vision:    cm.Vision,
		Reasoning: cm.Reasoning,
	}
	view.CapsKnown = true
	view.EligibleMain = cm.Tools
	view.EligibleCheap = true
	if !cm.Tools {
		view.IneligibleReason = "This model can't call tools, which the agent needs to act"
	}
	if cm.PriceKnown {
		view.Price = &Models.Cost{Input: floatPtr(cm.PriceInput), Output: floatPtr(cm.PriceOutput)}
		view.PriceLabel = Catalog.PriceLabel(view.Price)
	} else {
		view.PriceLabel = "Price not listed"
	}
	view.Capabilities = Catalog.CapabilityBadges(&view.Caps, true)
	return view
}

func catalogSubset(cat *Catalog.Catalog, providerID string, liveChecked bool, live *Shared.LiveResults) []Models.Model {
	if !liveChecked || live == nil {
		return cat.ByProvider(providerID)
	}
	var out []Models.Model
	for _, m := range cat.ByProvider(providerID) {
		if _, ok := live.ByID[strings.ToLower(m.ID)]; ok {
			out = append(out, m)
		}
	}
	return out
}

func ExcludedLiveOnly(overlay *Provider.Overlay, op Provider.OverlayProvider, id, name string) bool {
	patterns := make([]string, 0, 16)
	if overlay != nil {
		patterns = append(patterns, overlay.DefaultLiveExclude...)
	}
	patterns = append(patterns, op.LiveExclude...)
	needleID := strings.ToLower(id)
	needleName := strings.ToLower(name)
	for _, pattern := range patterns {
		if matchPattern(pattern, needleID) || matchPattern(pattern, needleName) {
			return true
		}
	}
	return false
}

func matchPattern(pattern, value string) bool {
	ok, err := path.Match(pattern, value)
	return err == nil && ok
}

func floatPtr(v float64) *float64 { return &v }

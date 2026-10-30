package Models

import (
	"context"
	"strings"

	"github.com/anyscapelabs/argus/Models"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
)

func (s *Service) GetModel(ref string) Shared.GetModelResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.GetModelResult{Error: aerr}
	}
	view, status, err := s.resolveRef(strings.TrimSpace(ref), false)
	if err != nil {
		return Shared.GetModelResult{Error: err}
	}
	final := view
	final.Availability = availabilityFor(view, status)
	return Shared.GetModelResult{View: &final}
}

func (s *Service) resolveRef(ref string, refreshLive bool) (Shared.ModelView, string, *Shared.ActionError) {
	providerID, _ := Models.SplitRef(ref)
	if aerr := s.checkKnownProvider(providerID); aerr != nil {
		return Shared.ModelView{}, "", aerr
	}
	cat := s.Core.Store.Get()
	if m, ok := cat.Get(ref); ok {
		configured := s.configured(providerID)
		availability := Shared.AvailabilityAvailable
		if !configured {
			availability = Shared.AvailabilityUnverified
		}
		return CatalogView(m, availability, s.selectedFor(m.Ref)), Shared.StatusOK, nil
	}

	if cm, ok := s.customByRef(ref); ok {
		return CustomView(cm, s.selectedFor(ref)), Shared.StatusOK, nil
	}

	provider, ok := cat.Provider(providerID)
	if !ok {
		return Shared.ModelView{}, "", Shared.NewError(Shared.CodeUnknownProvider, "Unknown provider")
	}
	overlayProvider, _ := s.Core.Overlay.Lookup(providerID)
	config, found, _ := s.Core.DB.GetProviderConfig(providerID)
	if !found {
		return Shared.ModelView{}, "", Shared.NewError(Shared.CodeNotFound, "Unknown model")
	}
	if entry := s.Core.FetchLive(context.Background(), provider, overlayProvider.ExtraHeaders, config, refreshLive); entry != nil {
		_, modelID := Models.SplitRef(ref)
		if lm, ok := entry.ByID[strings.ToLower(modelID)]; ok && !ExcludedLiveOnly(s.Core.Overlay, overlayProvider, lm.ID, lm.Name) {
			view := LiveView(provider, lm)
			view.SelectedFor = s.selectedFor(ref)
			return view, Shared.StatusOK, nil
		}
	}
	return Shared.ModelView{}, "", Shared.NewError(Shared.CodeNotFound, "Unknown model")
}

func (s *Service) checkKnownProvider(providerID string) *Shared.ActionError {
	_, found := s.Core.Store.Get().Provider(providerID)
	if !found {
		return Shared.NewError(Shared.CodeUnknownProvider, "Unknown provider")
	}
	return nil
}

func (s *Service) configured(providerID string) bool {
	_, found, _ := s.Core.DB.GetProviderConfig(providerID)
	return found
}

func (s *Service) customByRef(ref string) (Storage.CustomModel, bool) {
	providerID, modelID := Models.SplitRef(ref)
	rows, err := s.Core.DB.ListAllCustomModels()
	if err != nil {
		return Storage.CustomModel{}, false
	}
	for _, row := range rows {
		if strings.EqualFold(row.ProviderID, providerID) && strings.EqualFold(row.ModelID, modelID) {
			return row, true
		}
	}
	return Storage.CustomModel{}, false
}

func availabilityFor(view Shared.ModelView, status string) string {
	if !strings.EqualFold(status, Shared.StatusOK) {
		return view.Availability
	}
	return view.Availability
}

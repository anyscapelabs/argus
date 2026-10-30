package Models

import (
	"strings"

	"github.com/anyscapelabs/argus/Services/Shared"
)

func (s *Service) GetProviderSelection(providerID string) Shared.SelectionsResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.SelectionsResult{Error: aerr}
	}
	if _, ok := s.Core.Overlay.Lookup(providerID); !ok {
		return Shared.SelectionsResult{Error: Shared.NewError(Shared.CodeUnknownProvider, "Unknown provider")}
	}
	refs, err := s.Core.DB.ListProviderSelection(providerID)
	if err != nil {
		return Shared.SelectionsResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	return Shared.SelectionsResult{Refs: refs}
}

func (s *Service) SetProviderSelection(providerID string, refs []string) Shared.ActionResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.ActionResult{Error: aerr}
	}
	if _, ok := s.Core.Overlay.Lookup(providerID); !ok {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeUnknownProvider, "Unknown provider")}
	}
	if !s.configured(providerID) {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeNotConfigured, "Connect "+providerID+" before selecting models from it")}
	}
	clean := make([]string, 0, len(refs))
	seen := map[string]bool{}
	for _, ref := range refs {
		trimmed := strings.TrimSpace(ref)
		if trimmed == "" || seen[strings.ToLower(trimmed)] {
			continue
		}
		if providerIDOf(trimmed) != providerID {
			return Shared.ActionResult{Error: Shared.NewError(Shared.CodeBadInput, "A selection belongs to another provider")}
		}
		if _, ok := viewRef(allSelectionViews(s, providerID, trimmed), trimmed); !ok {
			return Shared.ActionResult{Error: Shared.NewError(Shared.CodeNotFound, "Unknown model: "+trimmed)}
		}
		seen[strings.ToLower(trimmed)] = true
		clean = append(clean, trimmed)
	}
	if err := s.Core.DB.SetProviderSelection(providerID, clean); err != nil {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	s.Core.EmitEvent(Shared.EventSelectionsChanged, nil)
	return Shared.ActionResult{Ok: true}
}

func (s *Service) ChatModels() []Shared.ChatModel {
	out := []Shared.ChatModel{}
	if err := s.Core.Ready(); err != nil {
		return out
	}
	rows, err := s.Core.DB.ListAllSelections()
	if err != nil {
		return out
	}
	overlayProviders := map[string]string{}
	for _, p := range s.Core.Store.Get().Providers() {
		overlayProviders[p.ID] = p.Name
	}
	configured := map[string]bool{}
	if configs, err := s.Core.DB.ListProviderConfigs(); err == nil {
		for _, c := range configs {
			configured[c.ProviderID] = true
		}
	}
	for _, row := range rows {
		if !configured[row.ProviderID] {
			continue
		}
		view, status, aerr := s.resolveRef(row.ModelRef, false)
		if aerr != nil || status != Shared.StatusOK {
			continue
		}
		view.ProviderName = overlayProviders[row.ProviderID]
		out = append(out, Shared.ChatModel{
			Ref:          row.ModelRef,
			ProviderID:   row.ProviderID,
			ProviderName: overlayProviders[row.ProviderID],
			Model:        view,
		})
	}
	return out
}

func allSelectionViews(s *Service, providerID, ref string) []Shared.ModelView {
	view, status, aerr := s.resolveRef(ref, false)
	if aerr != nil || status != Shared.StatusOK {
		return nil
	}
	return []Shared.ModelView{view}
}

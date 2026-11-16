package Models

import (
	"context"
	"sort"
	"strings"

	"github.com/anyscapelabs/argus/Models"
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
	// Selection is unused under the router (the allowlist governs): keep the
	// table in sync so ClearAgentModelsNotIn still has data to clean.
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
		seen[strings.ToLower(trimmed)] = true
		clean = append(clean, trimmed)
	}
	if err := s.Core.DB.SetProviderSelection(providerID, clean); err != nil {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	return Shared.ActionResult{Ok: true}
}

func keepIDs(refs []string) map[string]bool {
	keep := make(map[string]bool, len(refs))
	for _, ref := range refs {
		if _, id := Models.SplitRef(ref); id != "" {
			keep[id] = true
		}
	}
	return keep
}

func (s *Service) ChatModels() []Shared.ChatModel {
	out := []Shared.ChatModel{}
	if err := s.Core.Ready(); err != nil {
		return out
	}
	providerID := Shared.RouterProviderID
	provider, found := s.Core.Store.Get().Provider(providerID)
	if !found {
		return out
	}
	overlayProvider, _ := s.Core.Overlay.Lookup(providerID)
	cfg, _, _ := s.Core.DB.GetProviderConfig(providerID)
	entry := s.Core.FetchLive(context.Background(), provider, overlayProvider.ExtraHeaders, cfg, false)
	if entry == nil {
		return out
	}
	type row struct {
		ref  string
		name string
	}
	rows := []row{}
	for _, lm := range entry.ByID {
		ref := providerID + "/" + lm.ID
		name := lm.Name
		if strings.TrimSpace(name) == "" {
			name = lm.ID
		}
		rows = append(rows, row{ref: ref, name: name})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })
	for _, r := range rows {
		view := Shared.ModelView{
			Ref:          r.ref,
			ProviderID:   providerID,
			ProviderName: provider.Name,
			ID:           strings.TrimPrefix(r.ref, providerID+"/"),
			Name:         r.name,
			Availability: Shared.AvailabilityAvailable,
		}
		out = append(out, Shared.ChatModel{
			Ref:          r.ref,
			ProviderID:   providerID,
			ProviderName: provider.Name,
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

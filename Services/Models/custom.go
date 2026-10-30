package Models

import (
	"strings"

	"github.com/anyscapelabs/argus/Models"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
)

func (s *Service) CustomViews(providerID string) []Shared.ModelView {
	rows, err := s.Core.DB.ListCustomModels(providerID)
	if err != nil {
		return nil
	}
	out := make([]Shared.ModelView, 0, len(rows))
	for _, row := range rows {
		out = append(out, CustomView(row, s.selectedFor(row.Ref())))
	}
	return out
}

func (s *Service) AddCustomModel(input Shared.CustomModelInput) Shared.ActionResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.ActionResult{Error: aerr}
	}
	providerID := strings.TrimSpace(input.ProviderID)
	modelID := strings.TrimSpace(input.ModelID)
	if providerID == "" || modelID == "" {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeBadInput, "Provider and model id are required")}
	}
	if _, ok := s.Core.Store.Get().Provider(providerID); !ok {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeUnknownProvider, "Unknown provider")}
	}
	if !s.configured(providerID) {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeNotConfigured, "Connect "+providerID+" before adding a custom model")}
	}
	ref := Models.MakeRef(providerID, modelID)
	if _, exists := s.Core.Store.Get().Get(ref); exists {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeBadInput, "That model is already in the catalog. Pick it there instead")}
	}
	if dup, _ := s.Core.DB.HasCustomModel(providerID, modelID); dup {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeBadInput, "A custom model with that id already exists")}
	}
	if strings.TrimSpace(input.DisplayName) == "" {
		input.DisplayName = modelID
	}
	row := Storage.CustomModel{
		ProviderID:    providerID,
		ModelID:       modelID,
		DisplayName:   strings.TrimSpace(input.DisplayName),
		ContextWindow: input.ContextWindow,
		MaxOutput:     input.MaxOutput,
		Tools:         input.Tools,
		Vision:        input.Vision,
		Reasoning:     input.Reasoning,
	}
	if input.PriceKnown && (input.PriceInput == nil || input.PriceOutput == nil) {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeBadInput, "A declared price needs input and output per-million figures")}
	}
	if input.PriceKnown {
		row.PriceKnown = true
		row.PriceInput = *input.PriceInput
		row.PriceOutput = *input.PriceOutput
	}
	if err := s.Core.DB.SaveCustomModel(&row); err != nil {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	s.Core.EmitEvent(Shared.EventSelectionsChanged, nil)
	return Shared.ActionResult{Ok: true}
}

func (s *Service) DeleteCustomModel(ref string) Shared.ActionResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.ActionResult{Error: aerr}
	}
	providerID, modelID := Models.SplitRef(strings.TrimSpace(ref))
	if providerID == "" || modelID == "" {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeNotFound, "Unknown model")}
	}
	exists, _ := s.Core.DB.HasCustomModel(providerID, modelID)
	if !exists {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeNotFound, "Unknown custom model")}
	}
	if err := s.Core.DB.DeleteCustomModel(providerID, modelID); err != nil {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	clearedSlot, _ := s.Core.DB.RemoveSlotForRef(ref)
	if err := s.Core.DB.RemoveSelectedModelsForRef(ref); err != nil {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	s.Core.EmitEvent(Shared.EventSelectionsChanged, nil)
	result := Shared.ActionResult{Ok: true}
	if clearedSlot != "" {
		result.ClearedSlots = []string{clearedSlot}
		s.Core.EmitEvent(Shared.EventSlotsChanged, nil)
	}
	return result
}

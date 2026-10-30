package Models

import (
	"strings"

	"github.com/anyscapelabs/argus/Models"
	"github.com/anyscapelabs/argus/Services/Shared"
)

func (s *Service) GetSlots() []Shared.SlotView {
	out := []Shared.SlotView{}
	if err := s.Core.Ready(); err != nil {
		return out
	}
	saved, err := s.Core.DB.ListModelSlots()
	if err != nil {
		return out
	}
	bySlot := map[string]string{}
	for _, slot := range saved {
		bySlot[slot.Slot] = slot.Ref
	}
	for _, name := range Shared.SlotNames {
		out = append(out, s.slotView(name, bySlot[name]))
	}
	return out
}

func (s *Service) slotView(slot, ref string) Shared.SlotView {
	if ref == "" {
		return Shared.SlotView{Slot: slot, Status: Shared.StatusUnset}
	}
	view, status, aerr := s.resolveRef(ref, false)
	if aerr != nil {
		return Shared.SlotView{Slot: slot, Ref: ref, Status: Shared.StatusModelNotInCatalog, Reason: aerr.Message}
	}
	if status != Shared.StatusOK {
		return Shared.SlotView{Slot: slot, Ref: ref, Status: status}
	}
	if !s.configured(providerIDOf(ref)) {
		return Shared.SlotView{Slot: slot, Ref: ref, Model: &view, Status: Shared.StatusProviderNotConfigured}
	}
	return Shared.SlotView{Slot: slot, Ref: ref, Model: &view, Status: Shared.StatusOK}
}

func providerIDOf(ref string) string {
	providerID, _ := Models.SplitRef(ref)
	return providerID
}

func (s *Service) SetSlot(slot, ref string) Shared.ActionResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.ActionResult{Error: aerr}
	}
	ref = strings.TrimSpace(ref)
	if !validSlot(slot) {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeNotFound, "Unknown slot")}
	}
	if ref == "" {
		if err := s.Core.DB.DeleteModelSlot(slot); err != nil {
			return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
		}
		s.Core.EmitEvent(Shared.EventSlotsChanged, nil)
		return Shared.ActionResult{Ok: true}
	}
	providerID, modelID := Models.SplitRef(ref)
	if _, ok := s.Core.Store.Get().Provider(providerID); !ok {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeUnknownProvider, "Unknown provider")}
	}
	if !s.configured(providerID) {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeNotConfigured, "Connect "+providerID+" before selecting a model from it")}
	}
	view, _, aerr := s.resolveRef(ref, false)
	if aerr != nil {
		return Shared.ActionResult{Error: aerr}
	}
	if eligible, reason := slotEligibility(slot, view); !eligible {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeIneligible, reason)}
	}
	if err := s.Core.DB.SaveModelSlot(slot, Models.MakeRef(providerID, modelID)); err != nil {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	s.Core.EmitEvent(Shared.EventSlotsChanged, nil)
	return Shared.ActionResult{Ok: true}
}

func (s *Service) ClearSlot(slot string) Shared.ActionResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.ActionResult{Error: aerr}
	}
	if !validSlot(slot) {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeNotFound, "Unknown slot")}
	}
	if err := s.Core.DB.DeleteModelSlot(slot); err != nil {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	s.Core.EmitEvent(Shared.EventSlotsChanged, nil)
	return Shared.ActionResult{Ok: true}
}

func validSlot(slot string) bool {
	for _, name := range Shared.SlotNames {
		if strings.EqualFold(name, slot) {
			return true
		}
	}
	return false
}

func slotEligibility(slot string, view Shared.ModelView) (bool, string) {
	switch strings.ToLower(slot) {
	case Shared.SlotCheap:
		if !view.EligibleCheap {
			if view.IneligibleReason != "" {
				return false, view.IneligibleReason
			}
			return false, "This model doesn't produce text"
		}
		return true, ""
	default:
		if !view.EligibleMain {
			if view.IneligibleReason != "" {
				return false, view.IneligibleReason
			}
			return false, "This model can't call tools, which the agent needs to act"
		}
		return true, ""
	}
}

package Models

import (
	"strings"

	"github.com/anyscapelabs/argus/Models"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
)

const defaultAgentProfileID = "default"

func (s *Service) GetAgentProfile(id string) Shared.AgentProfileResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.AgentProfileResult{Error: aerr}
	}
	id = strings.TrimSpace(id)
	if id == "" {
		id = defaultAgentProfileID
	}
	p, found, err := s.Core.DB.GetAgentProfile(id)
	if err != nil {
		return Shared.AgentProfileResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	profile := Shared.AgentProfile{ID: id}
	if found {
		profile.ID = p.ID
		profile.Avatar = p.Avatar
		profile.AgentName = p.AgentName
		profile.ModelRef = p.ModelRef
		profile.SystemPrompt = p.SystemPrompt
		profile.UpdatedAt = p.UpdatedAt
	} else {
		profile.AgentName = "Argus"
		profile.Avatar = "/logo.png"
	}
	return Shared.AgentProfileResult{Profile: &profile}
}

func (s *Service) SaveAgentProfile(profile Shared.AgentProfile) Shared.ActionResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.ActionResult{Error: aerr}
	}
	id := strings.TrimSpace(profile.ID)
	if id == "" {
		id = defaultAgentProfileID
	}
	if err := s.Core.DB.SaveAgentProfile(Storage.AgentProfile{
		ID:           id,
		Avatar:       profile.Avatar,
		AgentName:    profile.AgentName,
		ModelRef:     profile.ModelRef,
		SystemPrompt: profile.SystemPrompt,
	}); err != nil {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	return Shared.ActionResult{Ok: true}
}

func (s *Service) SetAgentModel(id, modelRef string) Shared.ActionResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.ActionResult{Error: aerr}
	}
	id = strings.TrimSpace(id)
	if id == "" {
		id = defaultAgentProfileID
	}
	modelRef = strings.TrimSpace(modelRef)
	if modelRef != "" {
		providerID, _ := Models.SplitRef(modelRef)
		if providerID == "" {
			return Shared.ActionResult{Error: Shared.NewError(Shared.CodeBadInput, "Pick a provider and a model")}
		}
	}
	existing, found, err := s.Core.DB.GetAgentProfile(id)
	if err != nil {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	profile := Storage.AgentProfile{ID: id, AgentName: "Argus", Avatar: "/logo.png"}
	if found {
		profile.Avatar = existing.Avatar
		profile.AgentName = existing.AgentName
		profile.SystemPrompt = existing.SystemPrompt
		profile.UpdatedAt = existing.UpdatedAt
	}
	profile.ModelRef = modelRef
	if profile.Avatar == "" {
		profile.Avatar = "/logo.png"
	}
	if profile.AgentName == "" {
		profile.AgentName = "Argus"
	}
	if err := s.Core.DB.SaveAgentProfile(profile); err != nil {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	s.Core.EmitEvent(Shared.EventSlotsChanged, nil)
	return Shared.ActionResult{Ok: true}
}

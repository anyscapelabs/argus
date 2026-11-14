package Models

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/anyscapelabs/argus/Models"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
	"github.com/google/uuid"
)

const defaultAgentProfileID = "default"

// avatarHex is the only avatar form the create-agent flow accepts: a plain
// #rrggbb color, rendered as the gem by every avatar surface.
var avatarHex = regexp.MustCompile(`^#[0-9a-f]{6}$`)

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

// CreateAgent creates a teammate agent from the new-agent card: a fresh
// profile keyed by a new id, carrying the user's chosen avatar color, plus a
// routable @handle derived from the name. The default agent is untouched —
// it stays the orchestrator teammates are created around.
func (s *Service) CreateAgent(name, avatar string) Shared.CreateAgentResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.CreateAgentResult{Error: aerr}
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return Shared.CreateAgentResult{Error: Shared.NewError(Shared.CodeBadInput, "Give the agent a name")}
	}
	if len([]rune(name)) > 40 {
		return Shared.CreateAgentResult{Error: Shared.NewError(Shared.CodeBadInput, "Agent names are limited to 40 characters")}
	}

	// Avatar colors are stored as the hex the card picks, and renderers draw
	// the gem from it. Anything else is a bug in a caller, not a fallback.
	avatar = strings.ToLower(strings.TrimSpace(avatar))
	if !avatarHex.MatchString(avatar) {
		return Shared.CreateAgentResult{Error: Shared.NewError(Shared.CodeBadInput, "Avatar must be a #rrggbb color")}
	}

	uid, err := uuid.NewV7()
	if err != nil {
		return Shared.CreateAgentResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	id := uid.String()
	if err := s.Core.DB.SaveAgentProfile(Storage.AgentProfile{
		ID:        id,
		AgentName: name,
		Avatar:    avatar,
	}); err != nil {
		return Shared.CreateAgentResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	handle, err := Storage.SetAgentHandle(s.Core.DB.SQL(), id, name)
	if err != nil {
		return Shared.CreateAgentResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	seq, err := s.Core.DB.NextSeq(id)
	if err != nil {
		return Shared.CreateAgentResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	messageID, err := uuid.NewV7()
	if err != nil {
		return Shared.CreateAgentResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	createdAt := time.Now().UnixMilli()
	greeting := fmt.Sprintf("Hi, I'm %s, your new AI teammate. What should I specialize in, and what would you like me to work on first?", name)
	if err := s.Core.DB.InsertMessage(Storage.Message{
		ID:          messageID.String(),
		AgentID:     id,
		Seq:         seq,
		Role:        "assistant",
		BlocksJSON:  "[]",
		Text:        greeting,
		Status:      "complete",
		CreatedAt:   createdAt,
		CompletedAt: createdAt,
	}); err != nil {
		return Shared.CreateAgentResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}

	s.Core.EmitEvent(Shared.EventSlotsChanged, nil)
	return Shared.CreateAgentResult{Ok: true, AgentID: id, Handle: handle}
}

func (s *Service) ListAgents() []Shared.AgentSummary {
	if aerr := s.Core.Ready(); aerr != nil {
		return []Shared.AgentSummary{}
	}
	agents, err := Storage.ListAgentSummaries(s.Core.DB.SQL())
	if err != nil {
		return []Shared.AgentSummary{}
	}
	return agents
}

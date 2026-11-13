package Chat

import (
	"encoding/json"
	"strings"

	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
	"github.com/google/uuid"
)

// voiceRecord is the JSON stored in a 'voice' message row's text: everything
// the transcript card needs to render a finished call. The turns are the
// call's spoken sides in order, as the live session transcribed them.
type voiceRecord struct {
	Seconds int                `json:"seconds"`
	Turns   []Shared.VoiceTurn `json:"turns"`
}

// SaveVoiceCall persists a finished voice call as a single transcript card.
// Calls where the live model never answered are handed to the normal text
// model by the frontend instead, so they never reach this method.
func (s *Service) SaveVoiceCall(agentID string, turns []Shared.VoiceTurn, seconds int) Shared.VoiceCallResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.VoiceCallResult{Error: aerr}
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		agentID = "default"
	}

	// Drop empty and whitespace-only turns; a transcript of silence has
	// nothing worth a row.
	clean := make([]Shared.VoiceTurn, 0, len(turns))
	for _, t := range turns {
		text := strings.TrimSpace(t.Text)
		if text == "" {
			continue
		}
		role := strings.TrimSpace(t.Role)
		if role != "user" {
			role = "model"
		}
		clean = append(clean, Shared.VoiceTurn{Role: role, Text: text})
	}
	if len(clean) == 0 {
		return Shared.VoiceCallResult{Ok: true}
	}

	rows, rerr := s.voiceRows(agentID, clean, seconds)
	if rerr != nil {
		return Shared.VoiceCallResult{Error: Shared.NewError(Shared.CodeStorage, rerr.Error())}
	}
	for _, m := range rows {
		if err := s.Core.DB.InsertMessage(m); err != nil {
			return Shared.VoiceCallResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
		}
	}
	// EventChatMessage marks the start of a model run in the frontend. A voice
	// card has no run to finish; its caller invalidates the history query after
	// this method returns instead.
	return Shared.VoiceCallResult{Ok: true}
}

// voiceRows builds the single 'voice' row carrying a finished call's record.
func (s *Service) voiceRows(agentID string, turns []Shared.VoiceTurn, seconds int) ([]Storage.Message, error) {
	seq, err := s.Core.DB.NextSeq(agentID)
	if err != nil {
		return nil, err
	}
	now := nowUnixMs()
	id, _ := uuid.NewV7()
	record, merr := json.Marshal(voiceRecord{Seconds: seconds, Turns: turns})
	if merr != nil {
		return nil, merr
	}
	return []Storage.Message{{
		ID:          id.String(),
		AgentID:     agentID,
		Seq:         seq,
		Role:        "user",
		Kind:        "voice",
		BlocksJSON:  "[]",
		Text:        string(record),
		Status:      "complete",
		CreatedAt:   now,
		CompletedAt: now,
	}}, nil
}

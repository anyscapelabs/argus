package Chat

import (
	"strings"

	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
)

func (s *Service) GetChat(agentID string, beforeSeq, limit int) Shared.GetChatResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.GetChatResult{Error: aerr}
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		agentID = "default"
	}
	msgs, err := s.Core.DB.ListMessages(agentID, beforeSeq, limit)
	if err != nil {
		return Shared.GetChatResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	out := make([]Shared.ChatMessage, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, Shared.ChatMessage{
			ID:          m.ID,
			AgentID:     m.AgentID,
			Seq:         m.Seq,
			Role:        m.Role,
			Text:        m.Text,
			Status:      m.Status,
			StopReason:  m.StopReason,
			ModelRef:    m.ModelRef,
			CreatedAt:   m.CreatedAt,
			CompletedAt: m.CompletedAt,
			Blocks:      m.Blocks(),
		})
	}
	return Shared.GetChatResult{Messages: out}
}

func (s *Service) Usage(agentID string) Shared.UsageResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.UsageResult{Error: aerr}
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		agentID = "default"
	}
	reqs, err := s.Core.DB.ListModelRequests(agentID, 1000)
	if err != nil {
		return Shared.UsageResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	var in, out int64
	var cost float64
	hasCost := false
	failed := 0
	success := 0
	for _, r := range reqs {
		if r.Status == "success" {
			success++
			if r.InputTokens != nil {
				in += *r.InputTokens
			}
			if r.OutputTokens != nil {
				out += *r.OutputTokens
			}
			if r.CacheReadTokens != nil {
				in += *r.CacheReadTokens
			}
			if r.CacheWriteTokens != nil {
				in += *r.CacheWriteTokens
			}
			if r.ReasoningTokens != nil {
				out += *r.ReasoningTokens
			}
			if r.CostUSD != nil {
				cost += *r.CostUSD
				hasCost = true
			}
		} else if r.Status == "error" {
			failed++
		}
	}
	res := Shared.UsageResult{InputTokens: in, OutputTokens: out, Requests: success, Failed: failed}
	if hasCost {
		res.CostUSD = cost
	}
	return res
}

func (s *Service) ListRequests(agentID string, limit int) Shared.ListRequestsResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.ListRequestsResult{Error: aerr}
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		agentID = "default"
	}
	reqs, err := s.Core.DB.ListModelRequests(agentID, limit)
	if err != nil {
		return Shared.ListRequestsResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	out := make([]Shared.ModelRequestView, 0, len(reqs))
	for _, r := range reqs {
		in := int64(0)
		outTok := int64(0)
		if r.InputTokens != nil {
			in += *r.InputTokens
		}
		if r.CacheReadTokens != nil {
			in += *r.CacheReadTokens
		}
		if r.CacheWriteTokens != nil {
			in += *r.CacheWriteTokens
		}
		if r.OutputTokens != nil {
			outTok += *r.OutputTokens
		}
		if r.ReasoningTokens != nil {
			outTok += *r.ReasoningTokens
		}
		cost := float64(0)
		if r.CostUSD != nil {
			cost = *r.CostUSD
		}
		out = append(out, Shared.ModelRequestView{
			ID:          r.ID,
			Status:      r.Status,
			ModelRef:    r.ModelRef,
			InputTokens: in,
			OutputTokens: outTok,
			CostUSD:     cost,
			StartedAt:   r.StartedAt,
			CompletedAt: r.CompletedAt,
		})
	}
	return Shared.ListRequestsResult{Requests: out}
}

var _ = Storage.ModelRequest{}
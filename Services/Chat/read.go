package Chat

import (
	"fmt"
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
	agentName := agentDisplayName(s, agentID)
	// The page comes back visibility-filtered from SQL, so `limit` counts real
	// messages, not the tool-carrier rows between them. With no limit, ask for
	// the default window.
	if limit <= 0 {
		limit = chatPageLimit
	}
	msgs, err := s.Core.DB.ListVisibleMessages(agentID, beforeSeq, limit)
	if err != nil {
		return Shared.GetChatResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	messageIDs := make([]string, 0, len(msgs))
	for _, message := range msgs {
		messageIDs = append(messageIDs, message.ID)
	}
	reactions, err := s.Core.DB.ListMessageReactions(messageIDs)
	if err != nil {
		return Shared.GetChatResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	// The cursor: the smallest seq on the page (the list is ascending), stepped
	// down by one so the next page is strictly older. A short page means the
	// walk reached the conversation's first message and there is nothing older.
	var nextBeforeSeq int
	if len(msgs) == limit && len(msgs) > 0 {
		// The query already uses seq < beforeSeq; subtracting here skipped one
		// visible row at every page boundary.
		nextBeforeSeq = msgs[0].Seq
	}
	out := make([]Shared.ChatMessage, 0, len(msgs))
	for _, m := range msgs {
		// Steering rows are the app speaking on the user's behalf — an MCP
		// connection settling, say. The model reads them as context; showing
		// them here would put words in the user's mouth that they never typed.
		// Voice-call rows are the one app-authored kind the chat draws: they
		// carry the transcript of a finished voice session as a JSON record,
		// which the frontend renders as a collapsible transcript card.
		if m.Kind != "" && m.Kind != "voice" && m.Kind != Storage.MessageKindFileWrite {
			continue
		}
		if m.Kind == Storage.MessageKindFileWrite {
			for i, block := range m.Blocks() {
				if block.IsError || block.FileWrite == nil || strings.TrimSpace(block.FileWrite.Path) == "" {
					continue
				}
				write := *block.FileWrite
				out = append(out, Shared.ChatMessage{
					ID: fmt.Sprintf("%s:file:%d", m.ID, i), AgentID: m.AgentID, Seq: m.Seq,
					Role: "assistant", Kind: Storage.MessageKindFileWrite, Status: "complete",
					CreatedAt: m.CreatedAt, CompletedAt: m.CompletedAt, FileWrite: &write,
				})
			}
			continue
		}
		// Tool round-trips are real message rows so buildHistory can replay
		// them, but they are model context, not chat: a carrier row has no
		// visible text, and a tool-only assistant turn may have none either.
		if m.Role == "user" && strings.TrimSpace(m.Text) == "" {
			continue
		}
		// A reply row that carries no text is storage, not chat: it is either a
		// tool round (whose blocks are tool_use calls the model replays) or a run
		// killed mid-flight. Hiding it here is the job the startup cleanser used
		// to do by deleting the whole turn — the difference is that the question
		// survives, and so does the history the next turn reads.
		// Rows still 'streaming' stay in the list: the chat page measures the turn
		// in flight off them, and an error row keeps its own message visible.
		if m.Role == "assistant" && m.Status != "streaming" &&
			strings.TrimSpace(m.Text) == "" && strings.TrimSpace(m.ErrorMessage) == "" {
			continue
		}
		text := m.Text
		if m.Role == "assistant" && text != "" {
			text = StripLeadingTitle(text, agentName, "display")
		}
		out = append(out, Shared.ChatMessage{
			ID:           m.ID,
			AgentID:      m.AgentID,
			Seq:          m.Seq,
			Role:         m.Role,
			Kind:         m.Kind,
			Text:         text,
			Status:       m.Status,
			ErrorMessage: m.ErrorMessage,
			ErrorCode:    m.ErrorCode,
			StopReason:   m.StopReason,
			ModelRef:     m.ModelRef,
			CreatedAt:    m.CreatedAt,
			CompletedAt:  m.CompletedAt,
			Blocks:       m.Blocks(),
			Reaction:     reactions[m.ID],
		})
	}
	return Shared.GetChatResult{Messages: out, NextBeforeSeq: nextBeforeSeq}
}

// chatPageLimit is one history page. Big enough to fill a screen and then
// some; small enough that every page loads instantly.
const chatPageLimit = 100

// agentDisplayName resolves the current name of an agent, falling back to
// the default when the profile is missing or unnamed.
func agentDisplayName(s *Service, agentID string) string {
	if profile, found, err := s.Core.DB.GetAgentProfile(agentID); err == nil && found && strings.TrimSpace(profile.AgentName) != "" {
		return strings.TrimSpace(profile.AgentName)
	}
	return defaultAgentName
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
			ID:           r.ID,
			Status:       r.Status,
			ModelRef:     r.ModelRef,
			InputTokens:  in,
			OutputTokens: outTok,
			CostUSD:      cost,
			StartedAt:    r.StartedAt,
			CompletedAt:  r.CompletedAt,
		})
	}
	return Shared.ListRequestsResult{Requests: out}
}

var _ = Storage.ModelRequest{}

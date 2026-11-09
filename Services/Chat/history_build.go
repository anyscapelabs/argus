package Chat

import (
	"log"
	"strings"

	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Storage"
)

func (s *Service) buildHistory(agentID string, throughSeq int, budgetTokens int64, maxMessages int) []Protocol.Message {
	msgs, err := s.Core.DB.ListMessages(agentID, 0, 1000)
	if err != nil {
		return nil
	}
	msgs = dropErroredTurns(msgs, throughSeq)
	agentName := agentDisplayName(s, agentID)
	var out []Protocol.Message
	// Steer rows are invisible turns the app raises itself (takeover
	// handover, sign-in settled). The model must still see them, so they
	// become user turns — but a steer row never has a genuine preceding
	// tool round-trip, so a tool_result carrier beside one would read as a
	// response to a call the model never made.
	for _, m := range msgs {
		if m.Seq > throughSeq {
			continue
		}
		if m.Status != "complete" {
			continue
		}
		if m.Kind == "steering" {
			// A steer row is a user turn wearing a hidden costume; it can
			// never be the answer to a tool call, so it is replayed as text
			// and any tool_result sitting beside it is dropped.
			if m.Text != "" {
				appendMerged(&out, Protocol.RoleUser, []Protocol.Block{{Type: Protocol.BlockText, Text: m.Text}})
			}
			continue
		}
		if m.Role != "assistant" {
			// User rows: replay text blocks plus any tool_result blocks
			// from prior agentic turns so the model sees complete tool
			// round-trips. A result whose call is not the immediately
			// preceding kept message is orphaned and dropped.
			var userBlocks []Protocol.Block
			for _, b := range m.Blocks() {
				switch b.Type {
				case Protocol.BlockText:
					userBlocks = append(userBlocks, Protocol.Block{Type: Protocol.BlockText, Text: b.Text})
				case Protocol.BlockToolResult:
					if n := len(out); n > 0 && carriesToolUse(out[n-1]) {
						userBlocks = append(userBlocks, b)
					}
				}
			}
			if len(userBlocks) == 0 && m.Text != "" {
				userBlocks = []Protocol.Block{{Type: Protocol.BlockText, Text: m.Text}}
			}
			if len(userBlocks) == 0 {
				continue
			}
			appendMerged(&out, Protocol.Role(m.Role), userBlocks)
			continue
		}
		// Assistant rows: replay text blocks and tool_use blocks so the model
		// remembers which tools it called and can pair them with results.
		// A tool_use survives only when its matching tool_result is the
		// immediately following persisted message; buildHistory is the one
		// place that can see both halves, so unpairing is fixed here rather
		// than in each adapter.
		blocks := m.Blocks()
		var assistantBlocks []Protocol.Block
		firstText := true
		hasToolUse := false
		for _, b := range blocks {
			if b.Type == Protocol.BlockToolUse {
				hasToolUse = true
				break
			}
		}
		if hasToolUse {
			hasToolUse = nextUserCarriesResult(msgs, m.Seq, throughSeq)
		}
		for _, b := range blocks {
			switch b.Type {
			case Protocol.BlockText:
				text := b.Text
				if firstText {
					// Cleaned history stops old titled replies from priming
					// the model into repeating the pattern.
					text = StripLeadingTitle(text, agentName, "history")
					firstText = false
				}
				if text == "" && m.Text != "" {
					text = m.Text
				}
				assistantBlocks = append(assistantBlocks, Protocol.Block{Type: Protocol.BlockText, Text: text})
			case Protocol.BlockToolUse:
				if hasToolUse {
					assistantBlocks = append(assistantBlocks, b)
				}
			}
		}
		if len(assistantBlocks) == 0 && m.Text != "" {
			assistantBlocks = []Protocol.Block{{Type: Protocol.BlockText, Text: StripLeadingTitle(m.Text, agentName, "history")}}
		}
		if len(assistantBlocks) == 0 {
			continue
		}
		appendMerged(&out, Protocol.Role(m.Role), assistantBlocks)
	}
	// Token-budget trim: keep the newest exchanges that fit, never starting
	// mid round-trip — a window that opens on a tool_result carrier without
	// its tool_use is a wire-format error, not just lost context. The 40-row
	// ceiling (maxMessages) stays as a hard secondary bound.
	{
		start := 0
		used := int64(0)
		overBudget := false
		for i := len(out) - 1; i >= 0; i-- {
			t := msgTokens(out[i])
			if used+t > budgetTokens {
				start = i + 1
				overBudget = true
				break
			}
			used += t
		}
		if overBudget && start >= len(out) && len(out) > 0 {
			start = len(out) - 1 // the newest message always survives
		}
		if len(out)-start > maxMessages {
			start = len(out) - maxMessages
		}
		for start < len(out) && startsOrphaned(out, start) {
			start++
		}
		if start > 0 {
			out = out[start:]
		}
	}
	// Pair every function call with an immediate function response, and
	// vice versa. Gemini rejects a content whose functionCall is not
	// immediately followed by its functionResponse ("Please ensure that
	// function call turn comes immediately after a user turn or after a
	// function response turn"), and a window trimmed mid-round can open or
	// close on either half of a pair. Pairs are kept or dropped whole:
	// Pairs are kept or dropped whole. An errored tool_result — denied
	// permission, failed command, invalid call — is noise for future turns:
	// the model already saw the failure once, and replaying it re-burns
	// thousands of tokens on every request. The error is logged instead and
	// cannot poison history.
	return repairHistoryTurns(withoutErroredPairs(out))
}

// dropErroredTurns removes every turn whose reply ended in an error — the
// user's question, the tool rounds written while answering it, and the
// failed reply itself. An errored turn still replays as a pair with a dead
// answer: the question stays, no reply ever replaces it, and every later
// request re-bills the failed run's rounds while inviting the model to
// answer a question that already failed. Dropping the pair whole keeps the
// failed turn out of the window entirely; the reply row is skipped either
// way, so nothing readable is lost.
func dropErroredTurns(msgs []Storage.Message, throughSeq int) []Storage.Message {
	if len(msgs) == 0 {
		return msgs
	}
	// A turn is all rows between one real user question and the next.
	boundary := func(i int) bool {
		return msgs[i].Role == "user" && strings.TrimSpace(msgs[i].Text) != ""
	}
	out := make([]Storage.Message, 0, len(msgs))
	for start := 0; start < len(msgs); {
		if !boundary(start) {
			out = append(out, msgs[start])
			start++
			continue
		}
		end := start + 1
		for end < len(msgs) && !boundary(end) {
			end++
		}
		drop := false
		steered := false
		onDeck := false
		for i := start; i < end; i++ {
			if msgs[i].Kind == Storage.MessageKindSteering {
				steered = true
			}
			if msgs[i].Seq == throughSeq {
				onDeck = true
			}
			if msgs[i].Role == "assistant" && msgs[i].Status == "error" {
				drop = true
			}
		}
		// The newest turn is the question the request being built is about
		// to answer — its reply simply does not exist yet. A steering row is
		// raised out-of-band, not by the user's typing, so it keeps the
		// place regardless of how the run after it ended.
		if drop && !onDeck && !steered {
			log.Printf("chat: dropping errored turn seq=%d..%d", msgs[start].Seq, msgs[end-1].Seq)
			start = end
			continue
		}
		out = append(out, msgs[start:end]...)
		start = end
	}
	return out
}

// nextUserCarriesResult reports whether the persisted user message right
// after seq — the carrier a tool round-trip persists as the next row —
// exists within the window and carries at least one tool_result. An
// assistant tool_use whose response is missing from storage (cancelled
// mid-round, carrier lost) must not enter the request: strip the call
// instead.
func nextUserCarriesResult(msgs []Storage.Message, seq, throughSeq int) bool {
	for _, m := range msgs {
		if m.Seq <= seq {
			continue
		}
		if m.Seq > throughSeq {
			return false
		}
		if m.Role != "user" {
			// The next row is not a user carrier; the round-trip did not
			// persist its result.
			return false
		}
		for _, b := range m.Blocks() {
			if b.Type == Protocol.BlockToolResult {
				return true
			}
		}
		return false
	}
	return false
}

// appendMerged coalesces consecutive same-role messages into one protocol
// message. Persisted tool round-trips put a carrier row right before the
// next user message; strict providers reject two user turns in a row, and
// tool_results ordered before the text reads correctly for every family.
func appendMerged(out *[]Protocol.Message, role Protocol.Role, blocks []Protocol.Block) {
	if n := len(*out); n > 0 && (*out)[n-1].Role == role {
		(*out)[n-1].Blocks = append((*out)[n-1].Blocks, blocks...)
		return
	}
	*out = append(*out, Protocol.Message{Role: role, Blocks: blocks})
}

// msgTokens is a deliberate rough estimate — 4 chars per token, floor of 100
// for structural overhead. It steers a budget, it doesn't bill.
func msgTokens(m Protocol.Message) int64 {
	n := 0
	for _, b := range m.Blocks {
		n += len(b.Text)
		if b.ToolCall != nil {
			n += len(b.ToolCall.Input) + len(b.ToolCall.Name)
		}
	}
	if n == 0 {
		n = 400
	}
	return int64(n/4) + 100
}

// startsOrphaned reports whether out[start] is the trailing half of a tool
// round-trip: a user message carrying only tool results whose assistant
// tool_use sat before the window. Starting there would ask the provider
// for a function response that never had a call. The newest message is
// exempt — repairToolPairs converts it rather than dropping the turn.
func startsOrphaned(out []Protocol.Message, start int) bool {
	if start >= len(out) {
		return false
	}
	if start == len(out)-1 {
		return false
	}
	return isToolCarrier(out[start])
}

// historyBudgetTokens sizes the replayed recent window: a tenth of the model's
// context, kept between 2k and recentHistoryTokens. Older turns come back
// through conversation_read. Unknown models are assumed to have a 24k context.
func (s *Service) historyBudgetTokens(modelRef, providerID, modelID string) int64 {
	ctxLen := int64(0)
	if m, ok := s.Core.Store.Get().Get(modelRef); ok && m.Limits != nil {
		ctxLen = m.Limits.Context
	} else if rows, err := s.Core.DB.ListCustomModels(providerID); err == nil {
		for _, cm := range rows {
			if cm.ModelID == modelID {
				ctxLen = cm.ContextWindow
				break
			}
		}
	}
	if ctxLen <= 0 {
		ctxLen = 24000
	}
	b := ctxLen / 10
	if b > recentHistoryTokens {
		b = recentHistoryTokens
	}
	if b < 2000 {
		b = 2000
	}
	return b
}

func (s *Service) historySystemTokens(req *Protocol.Request) int64 {
	n := len(req.System)
	for _, t := range req.Tools {
		n += len(t.Description) + len(t.Schema)
	}
	return int64(n/4) + 200
}

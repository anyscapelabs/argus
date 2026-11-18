package Chat

import (
	"fmt"
	"hash/fnv"
	"strings"

	"github.com/anyscapelabs/argus/Protocol"
)

const (
	// compactKeepRecent is how many of the newest messages in-run compaction
	// never touches, so the model always sees the last few rounds verbatim.
	compactKeepRecent = 8
	// compactMinResultChars: a tool result shorter than this is not worth
	// replacing with a placeholder.
	compactMinResultChars = 600
	// stuckRoundLimit is how many consecutive rounds may issue the exact same
	// tool calls before the run is stopped as looping. Eight is deliberately
	// generous: an unattended agent polling a long build is legitimate, a
	// model repeating one failing call forever is not.
	stuckRoundLimit = 8
	// maxToolCallsPerTurn bounds how many tool calls one turn may execute in
	// total, regardless of round count. Varied-but-aimless discovery spam
	// (27 connector calls in one turn hunting for a send tool that does not
	// exist) evaded the identical-signature stuck detector; a total-call
	// budget cannot be evaded by rewording the inputs.
	maxToolCallsPerTurn = 25
	// noProgressRoundLimit is how many consecutive rounds may end with every
	// tool call failing before the turn is stopped as making no progress. A
	// model course-correcting from one or two errors is normal; five rounds
	// of nothing but errors is not.
	noProgressRoundLimit = 5
)

// modelContextTokens resolves the model's context window. Unknown models are
// assumed to have 24k, matching historyBudgetTokens.
func (s *Service) modelContextTokens(modelRef, providerID, modelID string) int64 {
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
	return ctxLen
}

// runContextBudget is how many tokens of messages one run may carry before old
// tool output starts being elided: three quarters of the window, minus the
// system prompt, the tool schemas and the room reserved for the reply.
func (s *Service) runContextBudget(modelRef, providerID, modelID string, req *Protocol.Request) int64 {
	budget := s.modelContextTokens(modelRef, providerID, modelID)*3/4 - s.historySystemTokens(req) - req.MaxOutputTokens
	if budget < 4000 {
		budget = 4000
	}
	return budget
}

// compactRunMessages keeps a long unattended run inside the context window.
// The loop appends two messages per round and never forgets anything, so
// without this a run either dies on a context error or has to be cut short by
// a small round cap. Once the estimate passes the budget it walks the oldest
// messages first and (1) redacts the payload of executed write/edit calls the
// same way persistence does, then (2) replaces large tool results with a short
// placeholder. Message and block counts never change, so every tool_use keeps
// its tool_result and the request stays valid on every adapter. The newest
// compactKeepRecent messages are never touched. It returns how many messages
// were shrunk.
func compactRunMessages(msgs []Protocol.Message, budget int64) int {
	var total int64
	for _, m := range msgs {
		total += msgTokens(m)
	}
	if total <= budget {
		return 0
	}
	shrunk := 0
	for i := 0; i < len(msgs)-compactKeepRecent && total > budget; i++ {
		before := msgTokens(msgs[i])
		switch msgs[i].Role {
		case Protocol.RoleAssistant:
			msgs[i] = redactPersistedToolInputs(msgs[i])
		case Protocol.RoleUser:
			var cloned []Protocol.Block
			for j, b := range msgs[i].Blocks {
				if b.Type != Protocol.BlockToolResult || len(b.Text) < compactMinResultChars {
					continue
				}
				if cloned == nil {
					cloned = append([]Protocol.Block(nil), msgs[i].Blocks...)
				}
				cloned[j].Text = fmt.Sprintf("[earlier tool output elided to save context: %d characters. Run the tool again if you still need it.]", len(b.Text))
			}
			if cloned != nil {
				msgs[i].Blocks = cloned
			}
		}
		if after := msgTokens(msgs[i]); after < before {
			total -= before - after
			shrunk++
		}
	}
	return shrunk
}

// callSignature fingerprints the tool calls of one round so the loop can tell
// when the model is issuing the same calls over and over.
func callSignature(calls []Protocol.ToolCall) uint64 {
	h := fnv.New64a()
	for _, c := range calls {
		_, _ = h.Write([]byte(c.Name))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(c.Input))
		_, _ = h.Write([]byte{0})
	}
	return h.Sum64()
}

// isQuotaRateLimitError reports whether a tool result is a rate-limit or
// quota-exhaustion failure. Retrying inside the same turn is pointless — a
// daily cap will not lift mid-turn — so the loop stops and reports honestly
// instead of burning calls against a closed door.
func isQuotaRateLimitError(text string) bool {
	t := strings.ToLower(text)
	for _, marker := range []string{
		"429",
		"too many requests",
		"rate limit",
		"ratelimit",
		"daily_limit_reached",
		"daily limit",
		"quota exceeded",
		"quota_exceeded",
		"resource_exhausted",
	} {
		if strings.Contains(t, marker) {
			return true
		}
	}
	return false
}

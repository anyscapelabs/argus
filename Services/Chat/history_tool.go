package Chat

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Storage"
)

// How much of a chat is replayed to the model on every send. A model that can
// call tools only needs the last few turns to stay coherent; everything older
// is one conversation_read call away, so a bare "hello" no longer drags tens of
// thousands of tokens of old transcript along. The router only allows models
// that can call tools, so there is no long-replay fallback.
const (
	recentHistoryTokens   = 8000
	recentHistoryMessages = 16
)

const (
	historySearchDefault = 8
	historySearchMax     = 20
	historyReadSpan      = 20
	historyReadMsgChars  = 1500
	historySnippetChars  = 240
)

var conversationReadToolDef = Protocol.ToolDef{
	Name: "conversation_read",
	Description: "Look up earlier messages of this chat that are no longer in your context. " +
		"Only the latest few turns are replayed to you; everything older stays in the app's storage. Free, no approval. " +
		"Call it ONLY when the user points at something you cannot see (\"as we discussed\", \"that report from Monday\", " +
		"\"continue where we left off\") or a task clearly depends on earlier details. " +
		"Never call it for greetings, self-contained requests, or just to be thorough. " +
		"Pass query (words that appear in the message; every word must match) to find messages — you get numbered snippets. " +
		"Then pass from_seq (and optionally to_seq) with a number from a result to read those messages in full.",
	Schema: json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {
				"type": "string",
				"description": "Words to find in earlier messages; every word must appear. Returns short snippets with message numbers."
			},
			"limit": {
				"type": "integer",
				"description": "Maximum matches to return (default 8, at most 20)"
			},
			"from_seq": {
				"type": "integer",
				"description": "First message number to read in full, taken from a search result"
			},
			"to_seq": {
				"type": "integer",
				"description": "Last message number to read (default from_seq + 9; at most 20 messages per call)"
			}
		}
	}`),
}

// runConversationRead serves the conversation_read tool for one agent's chat.
func (s *Service) runConversationRead(
	agentID string,
	call Protocol.ToolCall,
	result func(text string, isError bool) Protocol.Block,
	activity func(kind, command string),
) Protocol.Block {
	var input struct {
		Query   string `json:"query"`
		Limit   int    `json:"limit"`
		FromSeq int    `json:"from_seq"`
		ToSeq   int    `json:"to_seq"`
	}
	if err := json.Unmarshal([]byte(call.Input), &input); err != nil {
		return result("invalid tool input: could not parse the conversation_read arguments", true)
	}
	query := strings.TrimSpace(input.Query)

	if query != "" {
		limit := input.Limit
		if limit <= 0 {
			limit = historySearchDefault
		}
		if limit > historySearchMax {
			limit = historySearchMax
		}
		activity("tool_started", query)
		msgs, err := s.Core.DB.SearchMessages(agentID, query, limit)
		activity("tool_finished", query)
		if err != nil {
			return result("could not search the conversation: "+err.Error(), true)
		}
		if len(msgs) == 0 {
			return result("No earlier message contains all of: "+query+". Try fewer or different words.", false)
		}
		first := strings.Fields(query)[0]
		var b strings.Builder
		fmt.Fprintf(&b, "%d message(s) match %q, newest first. Read one in full with from_seq.\n", len(msgs), query)
		for i := len(msgs) - 1; i >= 0; i-- {
			m := msgs[i]
			fmt.Fprintf(&b, "#%d %s %s: %s\n", m.Seq, m.Role, historyStamp(m), snippetAround(m.Text, first, historySnippetChars))
		}
		return result(b.String(), false)
	}

	if input.FromSeq > 0 {
		from := input.FromSeq
		to := input.ToSeq
		if to < from {
			to = from + 9
		}
		if to-from+1 > historyReadSpan {
			to = from + historyReadSpan - 1
		}
		label := fmt.Sprintf("#%d–#%d", from, to)
		activity("tool_started", label)
		msgs, err := s.Core.DB.ListMessageRange(agentID, from, to, historyReadSpan)
		activity("tool_finished", label)
		if err != nil {
			return result("could not read the conversation: "+err.Error(), true)
		}
		var b strings.Builder
		shown := 0
		for _, m := range msgs {
			text := strings.TrimSpace(m.Text)
			if text == "" {
				continue
			}
			shown++
			if utf8.RuneCountInString(text) > historyReadMsgChars {
				text = string([]rune(text)[:historyReadMsgChars]) + " […message truncated]"
			}
			fmt.Fprintf(&b, "#%d %s %s\n%s\n\n", m.Seq, m.Role, historyStamp(m), text)
		}
		if shown == 0 {
			return result(fmt.Sprintf("No readable messages in %s. Search with query to find real message numbers.", label), false)
		}
		return result(b.String(), false)
	}

	return result("conversation_read needs either query (to find earlier messages) or from_seq (to read them).", true)
}

func historyStamp(m Storage.Message) string {
	if m.CreatedAt <= 0 {
		return ""
	}
	return "(" + time.UnixMilli(m.CreatedAt).Local().Format("2006-01-02 15:04") + ")"
}

// snippetAround returns about max characters of text centred on the first
// occurrence of term (case-insensitive), with whitespace collapsed so a
// multi-line message reads as one line.
func snippetAround(text, term string, max int) string {
	rs := []rune(strings.Join(strings.Fields(text), " "))
	if len(rs) <= max {
		return string(rs)
	}
	fold := func(in []rune) []rune {
		out := make([]rune, len(in))
		for i, r := range in {
			out[i] = unicode.ToLower(r)
		}
		return out
	}
	start := 0
	if tl := fold([]rune(term)); len(tl) > 0 {
		hay := string(fold(rs))
		if pos := strings.Index(hay, string(tl)); pos >= 0 {
			start = utf8.RuneCountInString(hay[:pos]) - max/3
		}
	}
	if start < 0 {
		start = 0
	}
	end := start + max
	if end > len(rs) {
		end = len(rs)
		if start = end - max; start < 0 {
			start = 0
		}
	}
	out := string(rs[start:end])
	if start > 0 {
		out = "…" + out
	}
	if end < len(rs) {
		out += "…"
	}
	return out
}

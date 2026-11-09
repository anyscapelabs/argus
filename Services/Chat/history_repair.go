package Chat

import (
	"log"

	"github.com/anyscapelabs/argus/Protocol"
)

// withoutErroredPairs drops every tool_use/tool_result pair whose result
// carried IsError. Only the matching halves are removed: a tool_use without
// a result behind it would be a wire-format error, so the pair goes whole.
// Dropped pairs are logged with the call id so they stay debuggable.
func withoutErroredPairs(out []Protocol.Message) []Protocol.Message {
	kept := make([]Protocol.Message, 0, len(out))
	for i, m := range out {
		if carriesToolUse(m) {
			failed := false
			droppedIDs := ""
			if i+1 < len(out) {
				for _, b := range out[i+1].Blocks {
					if b.Type == Protocol.BlockToolResult && b.IsError {
						failed = true
						droppedIDs += b.ToolRefID + " "
					}
				}
			}
			if failed {
				log.Printf("chat: dropping errored tool pair ids=%s", droppedIDs)
				stripKind(m, Protocol.BlockToolUse, &kept)
			} else {
				kept = append(kept, m)
			}
			continue
		}
		if carriesToolResult(m) {
			// A result whose call was dropped above must not ride along alone.
			errRefs := ""
			for _, b := range m.Blocks {
				if b.Type == Protocol.BlockToolResult && b.IsError {
					errRefs += b.ToolRefID + " "
				}
			}
			if errRefs != "" {
				log.Printf("chat: dropping errored tool_result ids=%s", errRefs)
				stripKind(m, Protocol.BlockToolResult, &kept)
				continue
			}
		}
		kept = append(kept, m)
	}
	return repairHistoryTurns(kept)
}

// repairHistoryTurns pairs tool calls with their responses and then makes the
// transcript open on a user turn. Gemini needs a user turn ahead of any
// function call, so a window that opens on an assistant turn is cut back to
// its first user turn; cutting the head can orphan the next half of a pair, so
// the repair runs again until the head is stable.
func repairHistoryTurns(out []Protocol.Message) []Protocol.Message {
	out = repairToolPairs(out)
	for pass := 0; pass < 8; pass++ {
		trimmed := trimLeadingModelTurns(out)
		if len(trimmed) == len(out) {
			break
		}
		out = repairToolPairs(trimmed)
	}
	return out
}

// repairToolPairs keeps every function call immediately followed by its
// response and vice versa, and never merges a call and its response into the
// same content: Gemini rejects a content whose functionCall shares a turn
// with its functionResponse ("Please ensure that function call turn comes
// immediately after a user turn or after a function response turn"). A merged
// message that carries both halves is therefore split — the call stays in
// its assistant content, the response starts a new user content, and any text
// that was glued after the response becomes its own turn. Orphaned halves
// (a call with no following response, or a response with no preceding call)
// are dropped whole; a message carrying text plus an orphaned half keeps
// the text.
func repairToolPairs(out []Protocol.Message) []Protocol.Message {
	return coalesceModelTurns(repairPairsOnce(resultsFirst(out)))
}

func repairPairsOnce(out []Protocol.Message) []Protocol.Message {
	kept := make([]Protocol.Message, 0, len(out))
	for i, m := range out {
		if carriesToolUse(m) {
			if i+1 >= len(out) || !carriesToolResult(out[i+1]) {
				// A call whose response never follows (or sits in the
				// same content) is orphaned: drop it, keep any text that
				// rode along.
				if !carriesToolResult(m) {
					stripKind(m, Protocol.BlockToolUse, &kept)
				} else {
					// Call and response share one content: split them.
					kept = appendSplit(kept, m)
				}
				continue
			}
		}
		if carriesToolResult(m) && !carriesToolUse(m) {
			if len(kept) == 0 || !carriesToolUse(kept[len(kept)-1]) {
				// Orphaned response: drop it, keep any text that rode along.
				stripKind(m, Protocol.BlockToolResult, &kept)
				continue
			}
		}
		// No splits needed: keep the message when the pairing is intact.
		// If it carries both halves, split at the tool_result boundary.
		kept = appendSplit(kept, m)
	}
	// A trailing-text split can leave a second user content right after a
	// tool_result; that is the intent. Apply the same split once more to
	// contents that still mix a response with later text.
	for i := 0; i < len(kept); i++ {
		if !carriesToolResult(kept[i]) || len(kept[i].Blocks) <= 1 {
			continue
		}
		var out []Protocol.Message
		head := kept[i]
		// find the first non-tool_result block
		split := -1
		for j, b := range head.Blocks {
			if b.Type != Protocol.BlockToolResult {
				split = j
				break
			}
		}
		if split <= 0 {
			continue
		}
		out = append(out, kept[:i]...)
		out = append(out, Protocol.Message{Role: head.Role, Blocks: head.Blocks[:split]})
		out = append(out, Protocol.Message{Role: Protocol.RoleUser, Blocks: head.Blocks[split:]})
		out = append(out, kept[i+1:]...)
		kept = out
	}
	return kept
}

// coalesceModelTurns merges adjacent assistant messages. Dropping an orphaned
// tool_result carrier leaves the assistant turns on either side of it
// touching, and Gemini rejects a function call that follows a model turn
// instead of a user turn or a function response ("Please ensure that function
// call turn comes immediately after a user turn or a function response
// turn"). Merged, the text and the call share one model turn, which is valid.
func coalesceModelTurns(in []Protocol.Message) []Protocol.Message {
	out := make([]Protocol.Message, 0, len(in))
	for _, m := range in {
		if n := len(out); n > 0 && m.Role == Protocol.RoleAssistant && out[n-1].Role == Protocol.RoleAssistant {
			blocks := make([]Protocol.Block, 0, len(out[n-1].Blocks)+len(m.Blocks))
			blocks = append(blocks, out[n-1].Blocks...)
			blocks = append(blocks, m.Blocks...)
			out[n-1].Blocks = blocks
			continue
		}
		out = append(out, m)
	}
	return out
}

// resultsFirst moves the tool_result blocks of every user message ahead of
// its text. A function response must sit directly after its function call;
// text glued in front of it would push the response into a later content and
// leave the call unanswered. The input is not mutated.
func resultsFirst(in []Protocol.Message) []Protocol.Message {
	out := make([]Protocol.Message, len(in))
	for i, m := range in {
		out[i] = m
		if m.Role != Protocol.RoleUser || !carriesToolResult(m) {
			continue
		}
		blocks := make([]Protocol.Block, 0, len(m.Blocks))
		for _, b := range m.Blocks {
			if b.Type == Protocol.BlockToolResult {
				blocks = append(blocks, b)
			}
		}
		for _, b := range m.Blocks {
			if b.Type != Protocol.BlockToolResult {
				blocks = append(blocks, b)
			}
		}
		out[i].Blocks = blocks
	}
	return out
}

// trimLeadingModelTurns drops any assistant turns at the head of the
// transcript. Gemini requires a function call turn to come immediately after
// a user turn or a function response turn, so a window that the token budget
// or the 40-row ceiling trimmed to open on an assistant message (with or
// without a tool call in it) is rejected outright. Dropping the turn costs a
// little context; sending it costs the whole request.
func trimLeadingModelTurns(in []Protocol.Message) []Protocol.Message {
	i := 0
	for i < len(in) && in[i].Role != Protocol.RoleUser {
		i++
	}
	// A leading user message that is only tool results has lost its call.
	for i < len(in) && isToolCarrier(in[i]) {
		i++
	}
	return in[i:]
}

// stripKind drops the blocks of one kind from m and appends what remains to
// kept: the text-only part of a message whose tool half was orphaned.
func stripKind(m Protocol.Message, kind Protocol.BlockType, kept *[]Protocol.Message) {
	rest := make([]Protocol.Block, 0, len(m.Blocks))
	for _, b := range m.Blocks {
		if b.Type != kind {
			rest = append(rest, b)
		}
	}
	if len(rest) == 0 {
		return
	}
	*kept = append(*kept, Protocol.Message{Role: m.Role, Blocks: rest})
}

// appendSplit appends m after splitting it at a tool_result boundary: any
// tool_result (and everything after it) becomes a new user content. A
// message with no tool_result — or with text only before it — is appended
// untouched.
func appendSplit(kept []Protocol.Message, m Protocol.Message) []Protocol.Message {
	split := -1
	for j, b := range m.Blocks {
		if b.Type == Protocol.BlockToolResult {
			split = j
			break
		}
	}
	if split <= 0 {
		// Nothing to split: the message has no tool_result, or the
		// tool_result is already the first block.
		return append(kept, m)
	}
	head := Protocol.Message{Role: m.Role, Blocks: m.Blocks[:split]}
	tail := Protocol.Message{Role: Protocol.RoleUser, Blocks: m.Blocks[split:]}
	kept = append(kept, head)
	// Everything after a tool_result in the same merged row was the user's
	// next sentence; it must be its own turn, not part of the response.
	// Split off trailing text into its own content (the text block may be
	// the first block of the tail when the tool_result was first).
	trailing := -1
	for j, b := range tail.Blocks {
		if b.Type == Protocol.BlockText {
			trailing = j
			break
		}
	}
	if trailing == 0 {
		// No tool_result before the text — the tail is pure text.
		return append(kept, tail)
	}
	if trailing > 0 {
		text := tail.Blocks[trailing:]
		tail = Protocol.Message{Role: Protocol.RoleUser, Blocks: tail.Blocks[:trailing]}
		kept = append(kept, tail)
		kept = append(kept, Protocol.Message{Role: Protocol.RoleUser, Blocks: text})
		return kept
	}
	return append(kept, tail)
}

func carriesToolUse(m Protocol.Message) bool {
	for _, b := range m.Blocks {
		if b.Type == Protocol.BlockToolUse {
			return true
		}
	}
	return false
}

func carriesToolResult(m Protocol.Message) bool {
	for _, b := range m.Blocks {
		if b.Type == Protocol.BlockToolResult {
			return true
		}
	}
	return false
}

func isToolCarrier(m Protocol.Message) bool {
	if m.Role != Protocol.RoleUser || len(m.Blocks) == 0 {
		return false
	}
	for _, b := range m.Blocks {
		if b.Type != Protocol.BlockToolResult {
			return false
		}
	}
	return true
}

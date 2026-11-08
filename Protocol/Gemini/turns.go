package Gemini

import (
	"github.com/anyscapelabs/argus/Protocol"
)

// missingResultText is what the model is told when one of its calls has no
// recorded result (a cancelled round, a lost carrier row). Sending an error
// response keeps the call/response counts equal, which Gemini enforces.
const missingResultText = "No result was recorded for this tool call."

// normalizeToolTurns rewrites a transcript so every model turn that holds
// function calls is followed by exactly one function response per call, in
// call order, each carrying the call's own name and id. Gemini rejects the
// request otherwise:
//
//   - "function_response.name: Name cannot be empty" — a result block with no
//     ToolName (rows persisted before the field existed, or a result whose
//     call was nameless). The name always comes from the matching call.
//   - "number of function response parts is equal to the number of function
//     call parts" — parallel calls with a missing, extra or reordered result.
//   - "function call turn comes immediately after a user turn or after a
//     function response turn" — calls with no response after them, responses
//     with no call before them, or two model turns touching.
//
// Calls with no name cannot be answered, so they are dropped together with the
// response that was matched to them. The input is not mutated.
func normalizeToolTurns(in []Protocol.Message) []Protocol.Message {
	msgs := mergeSameRole(in)
	out := make([]Protocol.Message, 0, len(msgs)+2)
	for i := 0; i < len(msgs); i++ {
		m := msgs[i]
		if m.Role == Protocol.RoleAssistant {
			if !hasBlock(m, Protocol.BlockToolUse) {
				out = append(out, m)
				continue
			}
			var next *Protocol.Message
			if i+1 < len(msgs) && msgs[i+1].Role == Protocol.RoleUser && hasBlock(msgs[i+1], Protocol.BlockToolResult) {
				next = &msgs[i+1]
			}
			if next == nil {
				// Calls nobody answered: keep the text, lose the calls.
				out = appendNonEmpty(out, Protocol.RoleAssistant, withoutKind(m.Blocks, Protocol.BlockToolUse))
				continue
			}
			i++
			assistant, results, rest := pairTurn(m, *next)
			out = appendNonEmpty(out, Protocol.RoleAssistant, assistant)
			out = appendNonEmpty(out, Protocol.RoleUser, results)
			out = appendNonEmpty(out, Protocol.RoleUser, rest)
			continue
		}
		// A user message still holding results here answers no call.
		if hasBlock(m, Protocol.BlockToolResult) {
			out = appendNonEmpty(out, Protocol.RoleUser, withoutKind(m.Blocks, Protocol.BlockToolResult))
			continue
		}
		out = append(out, m)
	}
	return out
}

// pairTurn matches the results in next to the calls in assistant. It returns
// the assistant blocks (nameless calls removed), exactly one result per kept
// call in call order, and whatever non-result blocks rode along in next.
func pairTurn(assistant, next Protocol.Message) (kept, results, rest []Protocol.Block) {
	var calls []*Protocol.ToolCall
	for _, b := range assistant.Blocks {
		if b.Type == Protocol.BlockToolUse && b.ToolCall != nil {
			calls = append(calls, b.ToolCall)
		}
	}
	var got []Protocol.Block
	for _, b := range next.Blocks {
		if b.Type == Protocol.BlockToolResult {
			got = append(got, b)
		} else {
			rest = append(rest, b)
		}
	}
	matched := matchResults(calls, got)

	callIdx := 0
	for _, b := range assistant.Blocks {
		if b.Type != Protocol.BlockToolUse {
			kept = append(kept, b)
			continue
		}
		if b.ToolCall == nil {
			continue
		}
		idx := callIdx
		callIdx++
		call := b.ToolCall
		if call.Name == "" {
			continue
		}
		kept = append(kept, b)
		res := matched[idx]
		if res == nil {
			res = &Protocol.Block{Type: Protocol.BlockToolResult, Text: missingResultText, IsError: true}
		}
		r := *res
		r.Type = Protocol.BlockToolResult
		r.ToolName = call.Name
		if call.ID != "" {
			r.ToolRefID = call.ID
		}
		results = append(results, r)
	}
	return kept, results, rest
}

// matchResults pairs each call with one result: by id first, then by tool
// name, then by position among whatever is left. Ids are not trusted alone
// because they are synthesised when Gemini sends none and can repeat across
// turns. Each result is used at most once; a call with no result gets nil.
func matchResults(calls []*Protocol.ToolCall, results []Protocol.Block) []*Protocol.Block {
	matched := make([]*Protocol.Block, len(calls))
	used := make([]bool, len(results))
	take := func(ci int, ok func(Protocol.Block) bool) {
		for ri := range results {
			if used[ri] || !ok(results[ri]) {
				continue
			}
			used[ri] = true
			matched[ci] = &results[ri]
			return
		}
	}
	for ci, c := range calls {
		if c.ID == "" {
			continue
		}
		id := c.ID
		take(ci, func(b Protocol.Block) bool { return b.ToolRefID == id })
	}
	for ci, c := range calls {
		if matched[ci] != nil || c.Name == "" {
			continue
		}
		name := c.Name
		take(ci, func(b Protocol.Block) bool { return b.ToolName == name })
	}
	for ci := range calls {
		if matched[ci] != nil {
			continue
		}
		take(ci, func(Protocol.Block) bool { return true })
	}
	return matched
}

// mergeSameRole joins consecutive messages of one role. Gemini treats two
// model messages as one turn, so a call can sit in the second while its
// sibling call sits in the first; pairing has to see them together.
func mergeSameRole(in []Protocol.Message) []Protocol.Message {
	out := make([]Protocol.Message, 0, len(in))
	for _, m := range in {
		if n := len(out); n > 0 && out[n-1].Role == m.Role {
			blocks := make([]Protocol.Block, 0, len(out[n-1].Blocks)+len(m.Blocks))
			blocks = append(blocks, out[n-1].Blocks...)
			blocks = append(blocks, m.Blocks...)
			out[n-1] = Protocol.Message{Role: m.Role, Blocks: blocks}
			continue
		}
		out = append(out, m)
	}
	return out
}

func hasBlock(m Protocol.Message, t Protocol.BlockType) bool {
	for _, b := range m.Blocks {
		if b.Type == t {
			return true
		}
	}
	return false
}

func withoutKind(blocks []Protocol.Block, t Protocol.BlockType) []Protocol.Block {
	out := make([]Protocol.Block, 0, len(blocks))
	for _, b := range blocks {
		if b.Type != t {
			out = append(out, b)
		}
	}
	return out
}

func appendNonEmpty(out []Protocol.Message, role Protocol.Role, blocks []Protocol.Block) []Protocol.Message {
	if len(blocks) == 0 {
		return out
	}
	return append(out, Protocol.Message{Role: role, Blocks: blocks})
}

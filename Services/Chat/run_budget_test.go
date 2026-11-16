package Chat

import (
	"fmt"
	"strings"
	"testing"

	"github.com/anyscapelabs/argus/Protocol"
)

func longRun(rounds int) []Protocol.Message {
	msgs := []Protocol.Message{{Role: Protocol.RoleUser, Blocks: []Protocol.Block{{Type: Protocol.BlockText, Text: "do the long task"}}}}
	for i := 0; i < rounds; i++ {
		call := &Protocol.ToolCall{ID: fmt.Sprintf("c%d", i), Name: "write_file", Input: `{"path":"a.ts","content":"` + strings.Repeat("x", 4000) + `"}`, Valid: true}
		msgs = append(msgs,
			Protocol.Message{Role: Protocol.RoleAssistant, Blocks: []Protocol.Block{{Type: Protocol.BlockToolUse, ToolCall: call}}},
			Protocol.Message{Role: Protocol.RoleUser, Blocks: []Protocol.Block{{Type: Protocol.BlockToolResult, ToolRefID: call.ID, Text: strings.Repeat("o", 6000)}}},
		)
	}
	return msgs
}

func estimate(msgs []Protocol.Message) (t int64) {
	for _, m := range msgs {
		t += msgTokens(m)
	}
	return
}

func TestCompactRunMessagesKeepsPairsAndRecentRounds(t *testing.T) {
	msgs := longRun(40)
	count := len(msgs)
	newest := msgs[len(msgs)-1].Blocks[0].Text
	budget := int64(20000)

	if compactRunMessages(msgs, budget) == 0 {
		t.Fatal("expected compaction over budget")
	}
	if len(msgs) != count {
		t.Fatalf("message count changed: %d -> %d", count, len(msgs))
	}
	if got := estimate(msgs); got > budget {
		t.Fatalf("still over budget: %d > %d", got, budget)
	}
	if msgs[len(msgs)-1].Blocks[0].Text != newest {
		t.Fatal("the newest tool result must stay verbatim")
	}
	uses, results := 0, 0
	for _, m := range msgs {
		for _, b := range m.Blocks {
			switch b.Type {
			case Protocol.BlockToolUse:
				uses++
			case Protocol.BlockToolResult:
				results++
			}
		}
	}
	if uses != 40 || results != 40 {
		t.Fatalf("tool_use/tool_result pairing broken: %d/%d", uses, results)
	}
}

func TestCompactRunMessagesNoopUnderBudget(t *testing.T) {
	if compactRunMessages(longRun(2), 1_000_000) != 0 {
		t.Fatal("must not touch a run that fits")
	}
}

func TestCompactRunMessagesIsIdempotent(t *testing.T) {
	msgs := longRun(30)
	compactRunMessages(msgs, 15000)
	if compactRunMessages(msgs, 15000) != 0 {
		t.Fatal("a second pass has nothing left to shrink")
	}
}

func TestCompactRunMessagesDoesNotMutateSharedBlocks(t *testing.T) {
	shared := []Protocol.Block{{Type: Protocol.BlockToolResult, Text: strings.Repeat("z", 5000)}}
	msgs := longRun(20)
	msgs[2] = Protocol.Message{Role: Protocol.RoleUser, Blocks: shared}
	compactRunMessages(msgs, 5000)
	if len(shared[0].Text) != 5000 {
		t.Fatal("compaction mutated a block slice shared with the persisted round")
	}
}

func TestCallSignature(t *testing.T) {
	ls := []Protocol.ToolCall{{Name: "execute_command", Input: `{"command":"ls"}`}}
	again := []Protocol.ToolCall{{Name: "execute_command", Input: `{"command":"ls"}`}}
	pwd := []Protocol.ToolCall{{Name: "execute_command", Input: `{"command":"pwd"}`}}
	if callSignature(ls) != callSignature(again) {
		t.Fatal("identical calls must share a signature")
	}
	if callSignature(ls) == callSignature(pwd) {
		t.Fatal("different calls must not collide")
	}
}

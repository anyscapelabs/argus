package Chat

import (
	"strings"
	"testing"

	"github.com/anyscapelabs/argus/Protocol"
)

func TestFileWriteRoundProjectsSanitizedCard(t *testing.T) {
	s, _ := newVoiceTestService(t)
	s.persistRound(
		"default", "router/model", "",
		Protocol.Message{Role: Protocol.RoleAssistant, Blocks: []Protocol.Block{{
			Type:     Protocol.BlockToolUse,
			ToolCall: &Protocol.ToolCall{ID: "c1", Name: "write_file", Input: `{"path":"/tmp/app.ts","content":"secret body"}`, Valid: true},
		}}},
		[]Protocol.Block{{
			Type: Protocol.BlockToolResult, ToolRefID: "c1", ToolName: "write_file",
			FileWrite: &Protocol.FileWrite{Path: "/tmp/app.ts", Action: "created", Bytes: 11, Lines: 1},
		}},
	)
	chat := s.GetChat("default", 0, 20)
	if chat.Error != nil || len(chat.Messages) != 1 {
		t.Fatalf("expected one projected card, got %+v", chat)
	}
	message := chat.Messages[0]
	if message.Kind != "file_write" || message.FileWrite == nil || message.FileWrite.Path != "/tmp/app.ts" {
		t.Fatalf("unexpected file card: %+v", message)
	}
	if strings.Contains(message.Text, "secret") || len(message.Blocks) != 0 {
		t.Fatalf("write content crossed the chat boundary: %+v", message)
	}
}

func TestPersistedGatewayWriteRedactsContent(t *testing.T) {
	message := Protocol.Message{Role: Protocol.RoleAssistant, Blocks: []Protocol.Block{{
		Type: Protocol.BlockToolUse,
		ToolCall: &Protocol.ToolCall{
			ID: "outer", Name: "invoke_tool", Valid: true,
			Input: `{"name":"write_file","arguments_json":"{\"path\":\"/tmp/x\",\"content\":\"private source\"}"}`,
		},
	}}}
	redacted := redactPersistedToolInputs(message)
	if strings.Contains(redacted.Blocks[0].ToolCall.Input, "private source") {
		t.Fatalf("write content was persisted: %s", redacted.Blocks[0].ToolCall.Input)
	}
	if !strings.Contains(message.Blocks[0].ToolCall.Input, "private source") {
		t.Fatal("redaction mutated the in-flight model turn")
	}
}

package Chat

import (
	"context"
	"strings"
	"testing"

	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Storage"
)

func seedMemoryToolRun(t *testing.T, db *Storage.DB, runID, userID, assistantID, text, kind string) {
	t.Helper()
	if err := db.InsertMessage(Storage.Message{
		ID: userID, AgentID: "default", Seq: 1, Role: "user", Kind: kind,
		BlocksJSON: "[]", Text: text, Status: "complete", CreatedAt: 1, CompletedAt: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.InsertMessage(Storage.Message{
		ID: assistantID, AgentID: "default", Seq: 2, Role: "assistant",
		BlocksJSON: "[]", Status: "streaming", CreatedAt: 2,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.InsertModelRequest(Storage.ModelRequest{
		ID: runID, AgentID: "default", MessageID: assistantID,
		ProviderID: "test", ModelID: "model", ModelRef: "test/model",
		AdapterFamily: "openai", BaseHost: "example.test", ContextThroughSeq: 1,
		ContextMessageCount: 1, Status: "started", StartedAt: 2,
	}); err != nil {
		t.Fatal(err)
	}
}

func memoryToolResult(text string, isError bool) Protocol.Block {
	return Protocol.Block{Type: Protocol.BlockToolResult, Text: text, IsError: isError}
}

func TestRememberRequiresEvidenceFromCurrentHumanMessage(t *testing.T) {
	s, db := newVoiceTestService(t)
	seedMemoryToolRun(t, db, "run-1", "user-1", "assistant-1", "Please remember that I prefer concise answers.", "")

	bad := runRememberTool(s, context.Background(), "run-1", "default", "", Protocol.ToolCall{
		Input: `{"key":"writing_style","value":"Keep answers concise.","scope":"always","evidence":"I prefer long answers"}`,
	}, memoryToolResult, func(string, string) {})
	if !bad.IsError || !strings.Contains(bad.Text, "exact words") {
		t.Fatalf("unsupported evidence was accepted: %+v", bad)
	}
	if _, found, err := db.GetMemory("default", "writing_style"); err != nil || found {
		t.Fatalf("rejected memory was stored: found=%v err=%v", found, err)
	}

	good := runRememberTool(s, context.Background(), "run-1", "default", "", Protocol.ToolCall{
		Input: `{"key":"writing_style","value":"Keep answers concise.","scope":"always","evidence":"I prefer concise answers"}`,
	}, memoryToolResult, func(string, string) {})
	if good.IsError {
		t.Fatalf("exact evidence was rejected: %+v", good)
	}
	memory, found, err := db.GetMemory("default", "writing_style")
	if err != nil || !found {
		t.Fatalf("memory not stored: found=%v err=%v", found, err)
	}
	if memory.SourceMessageID != "user-1" || memory.SourceRunID != "run-1" || memory.SourceType != "verified_user_message" {
		t.Fatalf("memory provenance was not recorded: %+v", memory)
	}
}

func TestForgetRequiresEvidenceFromCurrentHumanMessage(t *testing.T) {
	s, db := newVoiceTestService(t)
	if _, err := db.UpsertMemory(Storage.Memory{
		AgentID: "default", Key: "writing_style", Value: "Keep answers concise.",
		Scope: "always", Confidence: 0.95, SourceType: "verified_user_message",
	}); err != nil {
		t.Fatal(err)
	}
	seedMemoryToolRun(t, db, "run-2", "user-2", "assistant-2", "Please forget my writing preference now.", "")

	bad := runForgetTool(s, context.Background(), "run-2", "default", "", Protocol.ToolCall{
		Input: `{"key":"writing_style","evidence":"forget my timezone"}`,
	}, memoryToolResult, func(string, string) {})
	if !bad.IsError {
		t.Fatalf("unsupported forget was accepted: %+v", bad)
	}
	if _, found, _ := db.GetMemory("default", "writing_style"); !found {
		t.Fatal("rejected forget deleted the memory")
	}

	good := runForgetTool(s, context.Background(), "run-2", "default", "", Protocol.ToolCall{
		Input: `{"key":"writing_style","evidence":"forget my writing preference"}`,
	}, memoryToolResult, func(string, string) {})
	if good.IsError {
		t.Fatalf("exact forget evidence was rejected: %+v", good)
	}
	if _, found, _ := db.GetMemory("default", "writing_style"); found {
		t.Fatal("memory survived an evidence-backed forget")
	}
}

func TestRememberRejectsAppAuthoredSourceRows(t *testing.T) {
	s, db := newVoiceTestService(t)
	seedMemoryToolRun(t, db, "run-3", "user-3", "assistant-3", "Remember that reports should be short.", Storage.MessageKindScheduled)

	block := runRememberTool(s, context.Background(), "run-3", "default", "", Protocol.ToolCall{
		Input: `{"key":"report_style","value":"Keep reports short.","evidence":"reports should be short"}`,
	}, memoryToolResult, func(string, string) {})
	if !block.IsError {
		t.Fatalf("app-authored source created a memory: %+v", block)
	}
	if _, found, _ := db.GetMemory("default", "report_style"); found {
		t.Fatal("scheduled content was stored as user memory")
	}
}

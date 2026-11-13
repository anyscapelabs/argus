package Chat

import (
	"context"
	"strings"
	"testing"

	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
)

func TestScheduledFireCannotCreateAnotherSchedule(t *testing.T) {
	ctx := Shared.WithScheduledFire(context.Background())
	result := func(text string, isError bool) Protocol.Block {
		return Protocol.Block{Type: Protocol.BlockToolResult, Text: text, IsError: isError}
	}
	for name, run := range map[string]toolRunner{
		"schedule_reminder": runScheduleReminderTool,
		"create_routine":    runCreateRoutineTool,
	} {
		block := run(nil, ctx, "", "default", "Argus", Protocol.ToolCall{Name: name, Input: `{}`, Valid: true}, result, func(string, string) {})
		if !block.IsError || !strings.Contains(block.Text, "scheduled task firing") {
			t.Fatalf("%s was not rejected during a fire: %+v", name, block)
		}
	}
}

func TestScheduledFireDefinitionsOmitScheduleCreation(t *testing.T) {
	tools := withoutScheduleCreationTools([]Protocol.ToolDef{
		scheduleReminderToolDef,
		createRoutineToolDef,
		webSearchToolDef,
	})
	if len(tools) != 1 || tools[0].Name != webSearchToolDef.Name {
		t.Fatalf("unexpected scheduled tool set: %+v", tools)
	}
}

func TestScheduledToolRoundsStayHidden(t *testing.T) {
	s, db := newVoiceTestService(t)
	s.persistRound(
		"default",
		"router/model",
		Storage.MessageKindScheduled,
		Protocol.Message{Role: Protocol.RoleAssistant, Blocks: []Protocol.Block{{Type: Protocol.BlockText, Text: "working"}}},
		[]Protocol.Block{{Type: Protocol.BlockToolResult, ToolRefID: "c1", ToolName: "web_search", Text: "result"}},
	)
	visible, err := db.ListVisibleMessages("default", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(visible) != 0 {
		t.Fatalf("scheduled tool rows leaked into chat: %+v", visible)
	}
	all, err := db.ListMessages("default", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].Kind != Storage.MessageKindScheduled || all[1].Kind != Storage.MessageKindScheduled {
		t.Fatalf("scheduled tool rows were not retained as hidden context: %+v", all)
	}
}

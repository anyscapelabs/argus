package Chat

import (
	"context"
	"strings"
	"testing"

	"github.com/anyscapelabs/argus/Catalog"
	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Provider"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
)

func TestAgentNameToolSavesNameAndHandle(t *testing.T) {
	s, db := newVoiceTestService(t)
	if err := db.SaveAgentProfile(Storage.AgentProfile{ID: "default", AgentName: "Argus", Avatar: "#3366ff", ModelRef: "argus-router/model-a"}); err != nil {
		t.Fatal(err)
	}
	result := func(text string, isError bool) Protocol.Block {
		return Protocol.Block{Type: Protocol.BlockToolResult, Text: text, IsError: isError}
	}
	block := runAgentNameTool(s, context.Background(), "run-1", "default", "Argus",
		Protocol.ToolCall{Name: "set_agent_name", Input: `{"name":" Nova "}`, Valid: true}, result, func(string, string) {})
	if block.IsError {
		t.Fatalf("expected ok, got %+v", block)
	}
	if !strings.Contains(block.Text, `"name":"Nova"`) {
		t.Fatalf("unexpected result payload: %s", block.Text)
	}
	profile, found, err := db.GetAgentProfile("default")
	if err != nil || !found {
		t.Fatalf("profile read failed: found=%v err=%v", found, err)
	}
	if profile.AgentName != "Nova" {
		t.Fatalf("name was not saved: %q", profile.AgentName)
	}
	if profile.Avatar != "#3366ff" {
		t.Fatalf("avatar was clobbered: %q", profile.Avatar)
	}
}

func TestAgentNameToolRejectsUnattended(t *testing.T) {
	s, _ := newVoiceTestService(t)
	result := func(text string, isError bool) Protocol.Block {
		return Protocol.Block{Type: Protocol.BlockToolResult, Text: text, IsError: isError}
	}
	block := runAgentNameTool(s, Shared.WithUnattended(context.Background()), "run-1", "default", "Argus",
		Protocol.ToolCall{Name: "set_agent_name", Input: `{"name":"Nova"}`, Valid: true}, result, func(string, string) {})
	if !block.IsError {
		t.Fatalf("unattended rename was not rejected: %+v", block)
	}
}

// The first screen must cost no model call: a seeded greeting plus a card drawn
// from fixed choices. Only the answer starts a run.
func TestBeginFirstRunSeedsGreetingAndCardWithoutModel(t *testing.T) {
	s, db := newVoiceTestService(t)

	res := s.BeginFirstRun("default")
	if res.Error != nil || !res.Ok {
		t.Fatalf("expected ok, got %+v", res)
	}
	msgs, err := db.ListMessages("default", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].Text != firstRunGreeting || msgs[0].Role != "assistant" {
		t.Fatalf("greeting was not the only seeded row: %+v", msgs)
	}
	if state, seen, _ := db.GetAppSetting(firstRunSettingKey); !seen || state != firstRunStatePending {
		t.Fatalf("onboarding state = %q seen=%v, want %q", state, seen, firstRunStatePending)
	}
	list := s.PendingAskUser("default")
	if list.Error != nil || len(list.Requests) != 1 {
		t.Fatalf("expected the naming card, got %+v", list)
	}
	card := list.Requests[0]
	if card.RequestID != firstRunQuestionID || card.Question != firstRunQuestion || card.AgentID != "default" {
		t.Fatalf("unexpected card: %+v", card)
	}
	if len(card.Choices) != len(firstRunChoices) {
		t.Fatalf("unexpected choices: %+v", card.Choices)
	}

	// A re-mount must neither seed a second greeting nor a second card.
	if res := s.BeginFirstRun("default"); res.Error != nil {
		t.Fatalf("retry failed: %+v", res)
	}
	msgs, err = db.ListMessages("default", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("retry seeded a duplicate row: %d messages", len(msgs))
	}
	if list := s.PendingAskUser("default"); len(list.Requests) != 1 {
		t.Fatalf("retry stacked a second card: %+v", list)
	}
}

func TestBeginFirstRunSkipsExistingConversation(t *testing.T) {
	s, db := newVoiceTestService(t)
	seq, err := db.NextSeq("default")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.InsertMessage(Storage.Message{
		ID:         newID(),
		AgentID:    "default",
		Seq:        seq,
		Role:       "user",
		BlocksJSON: "[]",
		Text:       "can you check my inbox?",
		Status:     "complete",
		CreatedAt:  nowUnixMs(),
	}); err != nil {
		t.Fatal(err)
	}

	res := s.BeginFirstRun("default")
	if res.Error != nil || !res.Ok {
		t.Fatalf("expected ok, got %+v", res)
	}
	msgs, err := db.ListMessages("default", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("existing conversation was touched: %+v", msgs)
	}
	if state, seen, _ := db.GetAppSetting(firstRunSettingKey); !seen || state != firstRunStateDone {
		t.Fatalf("onboarding was not settled: state=%q seen=%v", state, seen)
	}
	if list := s.PendingAskUser("default"); len(list.Requests) != 0 {
		t.Fatalf("an existing conversation was asked to name the agent: %+v", list)
	}
}

func TestBeginFirstRunNoOpAfterDone(t *testing.T) {
	s, db := newVoiceTestService(t)
	if err := db.SetAppSetting(firstRunSettingKey, firstRunStateDone); err != nil {
		t.Fatal(err)
	}
	res := s.BeginFirstRun("default")
	if res.Error != nil || !res.Ok {
		t.Fatalf("expected ok, got %+v", res)
	}
	msgs, err := db.ListMessages("default", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("settled install was re-greeted: %+v", msgs)
	}
	if list := s.PendingAskUser("default"); len(list.Requests) != 0 {
		t.Fatalf("settled install was asked to name the agent: %+v", list)
	}
}

func TestBeginFirstRunIgnoresOtherAgents(t *testing.T) {
	s, db := newVoiceTestService(t)
	if err := db.SaveAgentProfile(Storage.AgentProfile{ID: "teammate", AgentName: "Teammate"}); err != nil {
		t.Fatal(err)
	}
	res := s.BeginFirstRun("teammate")
	if res.Error != nil || !res.Ok {
		t.Fatalf("expected ok, got %+v", res)
	}
	msgs, err := db.ListMessages("teammate", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("teammate was greeted: %+v", msgs)
	}
}

func TestAnswerFirstRunOnlyWhilePending(t *testing.T) {
	s, db := newVoiceTestService(t)
	if res := s.RespondAskUser(firstRunQuestionID, "Nova"); res.Error == nil {
		t.Fatalf("answered a card that was never drawn: %+v", res)
	}
	if err := db.SetAppSetting(firstRunSettingKey, firstRunStateDone); err != nil {
		t.Fatal(err)
	}
	if res := s.RespondAskUser(firstRunQuestionID, "Nova"); res.Error == nil {
		t.Fatalf("answered a settled card: %+v", res)
	}
}

// A failed launch must keep the card up: otherwise the user is left with a
// greeting and no way to name the agent.
func TestAnswerFirstRunKeepsCardWhenLaunchFails(t *testing.T) {
	s, db := newVoiceTestService(t)
	store, err := Catalog.NewStore(&Provider.Overlay{}, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	s.Core.Store = store
	if res := s.BeginFirstRun("default"); res.Error != nil {
		t.Fatalf("begin failed: %+v", res)
	}

	if res := s.RespondAskUser(firstRunQuestionID, strings.Repeat("a", firstRunMaxNameLength+1)); res.Error == nil {
		t.Fatal("an over-long name was accepted")
	}
	if state, _, _ := db.GetAppSetting(firstRunSettingKey); state != firstRunStatePending {
		t.Fatalf("over-long name changed the state to %q", state)
	}

	// No provider is configured in this test, so the run cannot launch.
	if res := s.RespondAskUser(firstRunQuestionID, "Nova"); res.Error == nil {
		t.Fatal("expected the launch to fail without a configured provider")
	}
	if state, _, _ := db.GetAppSetting(firstRunSettingKey); state != firstRunStatePending {
		t.Fatalf("failed launch settled onboarding: state=%q", state)
	}
	if list := s.PendingAskUser("default"); len(list.Requests) != 1 {
		t.Fatalf("failed launch dropped the card: %+v", list)
	}
	msgs, err := db.ListMessages("default", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 {
		t.Fatalf("failed launch wrote conversation rows: %+v", msgs)
	}
}

func TestFirstRunAnswerSteerNamesTheToolAndChoice(t *testing.T) {
	steer := firstRunAnswerSteer("Nova")
	if !strings.Contains(steer, `"Nova"`) || !strings.Contains(steer, "set_agent_name") {
		t.Fatalf("steer does not carry the choice and the tool: %s", steer)
	}
}

// Round rows are the model's own tool call and its result, not app-authored
// steering rows: buildHistory replays steering rows as user text and drops the
// call, which is what hid a steer run's tool rounds from every later turn.
func TestPersistRoundTagsToolRounds(t *testing.T) {
	s, db := newVoiceTestService(t)
	seq, err := db.NextSeq("default")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.InsertMessage(Storage.Message{
		ID: newID(), AgentID: "default", Seq: seq, Role: "user", BlocksJSON: "[]",
		Text: "find the tool that asks a question", Status: "complete", CreatedAt: nowUnixMs(),
	}); err != nil {
		t.Fatal(err)
	}
	s.persistRound(
		"default", "router/model",
		Protocol.Message{Role: Protocol.RoleAssistant, Blocks: []Protocol.Block{{
			Type:     Protocol.BlockToolUse,
			ToolCall: &Protocol.ToolCall{ID: "c1", Name: "search_tools", Input: `{"query":"ask the user"}`, Valid: true},
		}}},
		[]Protocol.Block{{Type: Protocol.BlockToolResult, ToolRefID: "c1", ToolName: "search_tools", Text: "[]"}},
	)
	all, err := db.ListMessages("default", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 || all[1].Kind != Storage.MessageKindToolRound || all[2].Kind != Storage.MessageKindToolRound {
		t.Fatalf("round rows were not tagged as tool rounds: %+v", all)
	}
	history := s.buildHistory("default", all[2].Seq, 4096, 40)
	sawCall := false
	for _, message := range history {
		for _, block := range message.Blocks {
			if block.Type == Protocol.BlockToolUse && block.ToolCall != nil && block.ToolCall.Name == "search_tools" {
				sawCall = true
			}
		}
	}
	if !sawCall {
		t.Fatalf("the tool call did not replay into history: %+v", history)
	}
}

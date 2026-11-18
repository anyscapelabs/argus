package Chat

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/anyscapelabs/argus/Protocol"
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
// from fixed choices. The answer is applied locally too, so onboarding costs
// nothing end to end.
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

// Naming is a local write, not a model turn: the answer must land with no run
// and no configured provider, and a refused name must leave the card up.
func TestAnswerFirstRunSavesNameWithoutModel(t *testing.T) {
	s, db := newVoiceTestService(t)
	if res := s.BeginFirstRun("default"); res.Error != nil {
		t.Fatalf("begin failed: %+v", res)
	}

	if res := s.RespondAskUser(firstRunQuestionID, strings.Repeat("a", firstRunMaxNameLength+1)); res.Error == nil {
		t.Fatal("an over-long name was accepted")
	}
	if state, _, _ := db.GetAppSetting(firstRunSettingKey); state != firstRunStatePending {
		t.Fatalf("over-long name changed the state to %q", state)
	}

	if res := s.RespondAskUser(firstRunQuestionID, "  Chief of Staff  "); res.Error != nil || !res.Ok {
		t.Fatalf("answer failed: %+v", res)
	}
	profile, found, err := db.GetAgentProfile("default")
	if err != nil || !found {
		t.Fatalf("profile read failed: found=%v err=%v", found, err)
	}
	if profile.AgentName != "Chief of Staff" {
		t.Fatalf("name was not saved: %q", profile.AgentName)
	}
	var handle string
	if err := db.SQL().QueryRow(`SELECT handle FROM agent_profiles WHERE id = 'default'`).Scan(&handle); err != nil {
		t.Fatalf("handle read failed: %v", err)
	}
	if handle == "" {
		t.Fatal("the @handle was not set alongside the name")
	}
	if state, seen, _ := db.GetAppSetting(firstRunSettingKey); !seen || state != firstRunStateDone {
		t.Fatalf("onboarding was not settled: state=%q seen=%v", state, seen)
	}
	if list := s.PendingAskUser("default"); len(list.Requests) != 0 {
		t.Fatalf("answered card is still pending: %+v", list)
	}

	msgs, err := db.ListVisibleMessages("default", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 3 {
		t.Fatalf("expected the greeting, the answered card and one confirmation, got %+v", msgs)
	}
	card := msgs[1]
	if card.Kind != Storage.MessageKindAskUser || card.Role != "assistant" {
		t.Fatalf("the answered card was not persisted: %+v", card)
	}
	var record struct {
		Question string `json:"question"`
		Answer   string `json:"answer"`
	}
	if err := json.Unmarshal([]byte(card.Text), &record); err != nil {
		t.Fatalf("the card payload is not readable: %v (%s)", err, card.Text)
	}
	if record.Question != firstRunQuestion || record.Answer != "Chief of Staff" {
		t.Fatalf("the card kept the wrong question/answer: %+v", record)
	}
	if last := msgs[2]; last.Role != "assistant" || !strings.Contains(last.Text, "Chief of Staff") {
		t.Fatalf("confirmation does not name the agent: %+v", last)
	}
	// The card is a UI record, not speech: history must not replay its JSON.
	for _, message := range s.buildHistory("default", msgs[2].Seq, 4096, 40) {
		for _, block := range message.Blocks {
			if strings.Contains(block.Text, `"question"`) {
				t.Fatalf("the card payload leaked into model history: %+v", message)
			}
		}
	}
	reqs, err := db.ListModelRequests("default", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 0 {
		t.Fatalf("onboarding called a model: %+v", reqs)
	}

	// A second answer must not stack a second confirmation.
	if res := s.RespondAskUser(firstRunQuestionID, "Nova"); res.Error == nil {
		t.Fatal("answered a settled card")
	}
	if msgs, _ := db.ListVisibleMessages("default", 0, 10); len(msgs) != 3 {
		t.Fatalf("a second answer wrote another row: %+v", msgs)
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

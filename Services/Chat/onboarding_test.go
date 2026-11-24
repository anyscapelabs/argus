package Chat

import (
	"context"
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

// The app authors the greeting and exactly one first-run onboarding card;
// every later step is driven by the app, not the model.
func TestBeginFirstRunSeedsOnlyGreeting(t *testing.T) {
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
	if list := s.PendingAskUser("default"); list.Error != nil || len(list.Requests) != 1 {
		t.Fatalf("seeded onboarding did not create exactly one ask_user card: %+v", list)
	}
	if step, seen, _ := db.GetAppSetting(onboardingStepKey); !seen || step != onboardingStepName {
		t.Fatalf("onboarding step = %q seen=%v, want %q", step, seen, onboardingStepName)
	}

	// A re-mount must not seed another greeting.
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
		t.Fatalf("retry created a duplicate card: %+v", list)
	}
	seq, err := db.NextSeq("default")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.InsertMessage(Storage.Message{
		ID: newID(), AgentID: "default", Seq: seq, Role: "user", BlocksJSON: "[]",
		Text: "Hello", Status: "complete", CreatedAt: nowUnixMs(),
	}); err != nil {
		t.Fatal(err)
	}
	if res := s.BeginFirstRun("default"); res.Error != nil {
		t.Fatalf("pending remount failed: %+v", res)
	}
	if state, _, _ := db.GetAppSetting(firstRunSettingKey); state != firstRunStatePending {
		t.Fatalf("remount ended model-led onboarding before the name was saved: %q", state)
	}
	reqs, err := db.ListModelRequests("default", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 0 {
		t.Fatalf("seeding called a model: %+v", reqs)
	}
}

func TestFirstRunPromptMovesOnboardingToTheModel(t *testing.T) {
	s, db := newVoiceTestService(t)
	if err := db.SetAppSetting(firstRunSettingKey, firstRunStatePending); err != nil {
		t.Fatal(err)
	}
	prompt := s.firstRunPrompt("default", true)
	for _, want := range []string{
		`What would you like to call me?`,
		"call ask_user",
		"exactly four short, distinct assistant-name suggestions",
		"Do not ask the question as plain text",
		"call set_agent_name",
		"do not call it again",
		"Keep the naming question separate from responsibilities",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("first-run prompt does not contain %q", want)
		}
	}
	if prompt := s.firstRunPrompt("default", false); prompt != "" {
		t.Fatalf("prompt advertised an unavailable naming tool: %q", prompt)
	}
	if err := db.SetAppSetting(firstRunSettingKey, firstRunStateDone); err != nil {
		t.Fatal(err)
	}
	if prompt := s.firstRunPrompt("default", true); prompt != "" {
		t.Fatalf("completed onboarding still changed the prompt: %q", prompt)
	}
}

func TestFirstRunForcesToolByStage(t *testing.T) {
	req := Protocol.Request{Tools: gatewayToolDefs()}
	forceFirstRunTool(&req, "")
	want := map[string]bool{"ask_user": false, "set_agent_name": false}
	for _, tool := range req.Tools {
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("first-run request did not expose %s directly", name)
		}
	}
	if req.ToolChoice.Mode != Protocol.ToolChoiceTool || req.ToolChoice.Name != "ask_user" {
		t.Fatalf("first-run tool choice = %+v, want ask_user", req.ToolChoice)
	}
	forceFirstRunTool(&req, "Nova")
	if req.ToolChoice.Name != "set_agent_name" {
		t.Fatalf("first-run tool choice after name = %+v, want set_agent_name", req.ToolChoice)
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

func TestAgentNameToolCompletesFirstRun(t *testing.T) {
	s, db := newVoiceTestService(t)
	if err := db.SetAppSetting(firstRunSettingKey, firstRunStatePending); err != nil {
		t.Fatal(err)
	}
	result := func(text string, isError bool) Protocol.Block {
		return Protocol.Block{Type: Protocol.BlockToolResult, Text: text, IsError: isError}
	}
	block := runAgentNameTool(s, context.Background(), "run-1", "default", "Argus",
		Protocol.ToolCall{Name: "set_agent_name", Input: `{"name":"Nova"}`, Valid: true}, result, func(string, string) {})
	if block.IsError {
		t.Fatalf("rename failed: %+v", block)
	}
	if state, seen, _ := db.GetAppSetting(firstRunSettingKey); !seen || state != firstRunStateDone {
		t.Fatalf("model-led rename did not complete onboarding: state=%q seen=%v", state, seen)
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

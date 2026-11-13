package Chat

import (
	"testing"

	"github.com/anyscapelabs/argus/Storage"
)

func TestMessageReactionPersistsAndClears(t *testing.T) {
	s, db := newVoiceTestService(t)
	if err := db.InsertMessage(Storage.Message{
		ID: "assistant-1", AgentID: "default", Seq: 1, Role: "assistant",
		BlocksJSON: "[]", Text: "A useful answer", Status: "complete", CreatedAt: 1, CompletedAt: 2,
	}); err != nil {
		t.Fatal(err)
	}
	if result := s.SetMessageReaction("default", "assistant-1", "👍"); result.Error != nil {
		t.Fatal(result.Error)
	}
	chat := s.GetChat("default", 0, 10)
	if len(chat.Messages) != 1 || chat.Messages[0].Reaction != "👍" {
		t.Fatalf("reaction did not round-trip: %+v", chat.Messages)
	}
	if result := s.SetMessageReaction("default", "assistant-1", ""); result.Error != nil {
		t.Fatal(result.Error)
	}
	chat = s.GetChat("default", 0, 10)
	if chat.Messages[0].Reaction != "" {
		t.Fatalf("reaction was not cleared: %+v", chat.Messages[0])
	}
}

func TestReactionRejectsUserAndHiddenRows(t *testing.T) {
	s, db := newVoiceTestService(t)
	for i, message := range []Storage.Message{
		{ID: "user-1", AgentID: "default", Seq: 1, Role: "user", BlocksJSON: "[]", Text: "hello", Status: "complete", CreatedAt: 1},
		{ID: "tool-1", AgentID: "default", Seq: 2, Role: "assistant", Kind: Storage.MessageKindToolRound, BlocksJSON: "[]", Text: "working", Status: "complete", CreatedAt: 2},
	} {
		if err := db.InsertMessage(message); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}
	for _, id := range []string{"user-1", "tool-1"} {
		if result := s.SetMessageReaction("default", id, "👎"); result.Error == nil {
			t.Fatalf("reaction was accepted for %s", id)
		}
	}
}

func TestReactionEnforcesAgentOwnership(t *testing.T) {
	s, db := newVoiceTestService(t)
	if err := db.SaveAgentProfile(Storage.AgentProfile{ID: "other", AgentName: "Other"}); err != nil {
		t.Fatal(err)
	}
	if err := db.InsertMessage(Storage.Message{
		ID: "other-answer", AgentID: "other", Seq: 1, Role: "assistant",
		BlocksJSON: "[]", Text: "Private answer", Status: "complete", CreatedAt: 1, CompletedAt: 2,
	}); err != nil {
		t.Fatal(err)
	}
	if result := s.SetMessageReaction("other", "other-answer", "👍"); result.Error != nil {
		t.Fatal(result.Error)
	}
	if result := s.SetMessageReaction("default", "other-answer", "👎"); result.Error == nil {
		t.Fatal("cross-agent reaction was accepted")
	}
	chat := s.GetChat("other", 0, 10)
	if len(chat.Messages) != 1 || chat.Messages[0].Reaction != "👍" {
		t.Fatalf("cross-agent attempt changed the reaction: %+v", chat.Messages)
	}
}

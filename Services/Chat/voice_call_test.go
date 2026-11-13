package Chat

import (
	"encoding/json"
	"testing"

	"github.com/anyscapelabs/argus/Catalog"
	"github.com/anyscapelabs/argus/Provider"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
)

func newVoiceTestService(t *testing.T) (*Service, *Storage.DB) {
	t.Helper()
	db, err := Storage.Open(t.TempDir() + "/chat.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.SaveAgentProfile(Storage.AgentProfile{ID: "default", AgentName: "Argus"}); err != nil {
		t.Fatal(err)
	}
	core := &Shared.Core{
		DB:      db,
		Overlay: &Provider.Overlay{},
		Store:   &Catalog.Store{},
	}
	return &Service{Core: core}, db
}

func TestSaveVoiceCallCleanPersistsSingleVoiceRow(t *testing.T) {
	s, db := newVoiceTestService(t)
	turns := []Shared.VoiceTurn{
		{Role: "user", Text: " what's the weather? "},
		{Role: "model", Text: "It is sunny today."},
		{Role: "user", Text: "   "},
	}
	res := s.SaveVoiceCall("default", turns, 42)
	if res.Error != nil || !res.Ok {
		t.Fatalf("expected ok, got %+v", res)
	}
	page, err := db.ListVisibleMessages("default", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 1 {
		t.Fatalf("expected exactly one visible row, got %d", len(page))
	}
	m := page[0]
	if m.Kind != "voice" || m.Role != "user" {
		t.Fatalf("expected voice/user row, got kind=%q role=%q", m.Kind, m.Role)
	}
	var rec voiceRecord
	if err := json.Unmarshal([]byte(m.Text), &rec); err != nil {
		t.Fatalf("text is not a voice record: %v", err)
	}
	if rec.Seconds != 42 || len(rec.Turns) != 2 {
		t.Fatalf("unexpected record: %+v", rec)
	}
	if rec.Turns[0].Text != "what's the weather?" || rec.Turns[1].Role != "model" {
		t.Fatalf("turns not cleaned: %+v", rec.Turns)
	}
}

func TestSaveVoiceCallSilencePersistsNothing(t *testing.T) {
	s, db := newVoiceTestService(t)
	res := s.SaveVoiceCall("default", []Shared.VoiceTurn{{Role: "user", Text: "  "}}, 3)
	if res.Error != nil || !res.Ok {
		t.Fatalf("expected ok, got %+v", res)
	}
	page, err := db.ListVisibleMessages("default", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 0 {
		t.Fatalf("expected no rows, got %d", len(page))
	}
}

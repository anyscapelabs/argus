package Models

import (
	"path/filepath"
	"testing"

	"github.com/anyscapelabs/argus/Catalog"
	"github.com/anyscapelabs/argus/Provider"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
)

func TestCreateAgentStartsItsChatWithoutTouchingDefault(t *testing.T) {
	db, err := Storage.Open(filepath.Join(t.TempDir(), "argus.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	service := &Service{Core: &Shared.Core{
		DB: db, Overlay: &Provider.Overlay{}, Store: &Catalog.Store{},
	}}
	created := service.CreateAgent("Counsel", "#112233")
	if created.Error != nil {
		t.Fatalf("CreateAgent returned error: %s", created.Error.Message)
	}
	if created.AgentID == "" || created.AgentID == "default" {
		t.Fatalf("unexpected agent id %q", created.AgentID)
	}

	messages, err := db.ListVisibleMessages(created.AgentID, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 {
		t.Fatalf("new agent has %d visible messages, want 1", len(messages))
	}
	want := "Hi, I'm Counsel, your new AI teammate. What should I specialize in, and what would you like me to work on first?"
	if messages[0].Role != "assistant" || messages[0].Text != want {
		t.Fatalf("unexpected greeting: role=%q text=%q", messages[0].Role, messages[0].Text)
	}
	defaultMessages, err := db.ListVisibleMessages("default", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(defaultMessages) != 0 {
		t.Fatalf("default agent has %d messages, want 0", len(defaultMessages))
	}
}

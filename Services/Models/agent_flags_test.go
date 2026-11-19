package Models

import (
	"path/filepath"
	"testing"

	"github.com/anyscapelabs/argus/Provider"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
	"github.com/anyscapelabs/argus/Catalog"
)

func TestAgentFlagRoundTrip(t *testing.T) {
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
		t.Fatalf("CreateAgent: %s", created.Error.Message)
	}
	id := created.AgentID

	if res := service.SetAgentPinned(id, true); res.Error != nil {
		t.Fatalf("SetAgentPinned: %s", res.Error.Message)
	}
	if got := service.ListPinnedAgents(); len(got) != 1 || got[0] != id {
		t.Fatalf("ListPinnedAgents = %v, want [%s]", got, id)
	}
	if res := service.SetAgentPinned(id, false); res.Error != nil {
		t.Fatalf("unpin: %s", res.Error.Message)
	}
	if got := service.ListPinnedAgents(); len(got) != 0 {
		t.Fatalf("after unpin: %v, want empty", got)
	}

	if res := service.SetAgentMuted(id, true); res.Error != nil {
		t.Fatalf("SetAgentMuted: %s", res.Error.Message)
	}
	if got := service.ListMutedAgents(); len(got) != 1 || got[0] != id {
		t.Fatalf("ListMutedAgents = %v, want [%s]", got, id)
	}
	if res := service.SetAgentMuted(id, false); res.Error != nil {
		t.Fatalf("unmute: %s", res.Error.Message)
	}
	if got := service.ListMutedAgents(); len(got) != 0 {
		t.Fatalf("after unmute: %v, want empty", got)
	}

	if res := service.DeleteAgent(id); res.Error != nil {
		t.Fatalf("DeleteAgent: %s", res.Error.Message)
	}
	agents := service.ListAgents()
	for _, a := range agents {
		if a.ID == id {
			t.Fatalf("agent %q still listed after delete", id)
		}
	}
}

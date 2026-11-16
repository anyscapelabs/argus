package Artifacts

import (
	"testing"

	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
)

func TestStoreDeckCreatesDeckArtifact(t *testing.T) {
	db := openTempDB(t)
	s := &Service{Core: &Shared.Core{DB: db}}

	body := `{"title":"Pitch","slides":[{"layout":"title","title":"Pitch"},{"layout":"content","title":"Why now","bullets":["a","b"]}]}`
	res := s.storeDeck("default", body, "", "")
	if res.Error != nil {
		t.Fatalf("unexpected error: %v", res.Error)
	}
	if res.Artifact.Kind != Storage.ArtifactKindDeck {
		t.Fatalf("kind should be deck, got %q", res.Artifact.Kind)
	}
	if res.SlideCount != 2 {
		t.Fatalf("slide count: %d", res.SlideCount)
	}
	if res.Artifact.Title != "Pitch" {
		t.Fatalf("title: %q", res.Artifact.Title)
	}
	// The row must be retrievable as an artifact.
	got, found, err := db.GetArtifact(res.Artifact.ID)
	if err != nil || !found {
		t.Fatalf("stored deck missing (found=%v err=%v)", found, err)
	}
	if got.Kind != Storage.ArtifactKindDeck {
		t.Fatalf("stored kind: %q", got.Kind)
	}
}

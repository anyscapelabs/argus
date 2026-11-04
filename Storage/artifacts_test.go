package Storage

import (
	"strings"
	"testing"
)

func TestArtifactRoundTripAndListing(t *testing.T) {
	db := openTestDB(t)

	body := "# November\n\nThe quarter closed up 4 percent.\n"
	if err := db.InsertArtifact(Artifact{ID: "a1", AgentID: "default", Title: "November report", Body: body}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	got, found, err := db.GetArtifact("a1")
	if err != nil || !found {
		t.Fatalf("get: found=%v err=%v", found, err)
	}
	if got.Body != body {
		t.Errorf("body = %q, want %q", got.Body, body)
	}
	// Format, version and the derived counts are filled in by the insert, not
	// the caller, so every row obeys the single-canonical-format rule.
	if got.Format != ArtifactFormat {
		t.Errorf("format = %q, want %q", got.Format, ArtifactFormat)
	}
	if got.Version != 1 {
		t.Errorf("version = %d, want 1", got.Version)
	}
	if got.Words != CountWords(body) || got.Bytes != len(body) {
		t.Errorf("words=%d bytes=%d, want %d/%d", got.Words, got.Bytes, CountWords(body), len(body))
	}
	if got.CreatedAt == 0 || got.UpdatedAt != got.CreatedAt {
		t.Errorf("timestamps = %d/%d, want equal and set", got.CreatedAt, got.UpdatedAt)
	}
	// A document written without front matter still has metadata: an empty
	// object, which is what every exporter reads as "use my defaults".
	if got.Meta != "{}" {
		t.Errorf("meta = %q, want an empty JSON object", got.Meta)
	}

	listed, err := db.ListArtifacts("default", 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(listed) != 1 {
		t.Fatalf("listed %d, want 1", len(listed))
	}
	// The panel listing carries the metadata only; bodies come on demand.
	if listed[0].Body != "" {
		t.Errorf("listing leaked the body: %q", listed[0].Body)
	}
	if listed[0].Words == 0 || listed[0].Bytes == 0 {
		t.Errorf("listing lost the counts: %+v", listed[0])
	}

	// Another agent must not see it.
	other, err := db.ListArtifacts("someone-else", 0)
	if err != nil {
		t.Fatalf("list other: %v", err)
	}
	if len(other) != 0 {
		t.Errorf("other agent sees %d artifacts", len(other))
	}
}

func TestArtifactUpdateSnapshotsAndPrunes(t *testing.T) {
	db := openTestDB(t)

	if err := db.InsertArtifact(Artifact{ID: "a1", AgentID: "default", Title: "Draft", Body: "one two three four\n"}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// A rewrite keeps the title when the caller passes none, and reports the
	// stored row it produced.
	updated, err := db.UpdateArtifact("a1", "", "one two three\n", `{"paper":"letter"}`, 0)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if updated.Version != 2 {
		t.Errorf("version = %d, want 2", updated.Version)
	}
	if updated.Title != "Draft" {
		t.Errorf("title = %q, want the previous title kept", updated.Title)
	}
	if updated.Words != 3 || updated.Bytes != len("one two three\n") {
		t.Errorf("counts not recomputed: %+v", updated)
	}
	// Metadata survives the rewrite.
	if updated.Meta != `{"paper":"letter"}` {
		t.Errorf("meta = %q, want the value just written", updated.Meta)
	}

	// The superseded body is recoverable, keyed by the version it was live at.
	versions, err := db.ListArtifactVersions("a1")
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("have %d snapshots, want 1", len(versions))
	}
	if versions[0].Version != 1 || versions[0].Body != "" {
		t.Errorf("snapshot listing = %+v, want version 1 with no body", versions[0])
	}
	snap, found, err := db.GetArtifactVersion(versions[0].ID)
	if err != nil || !found {
		t.Fatalf("get snapshot: found=%v err=%v", found, err)
	}
	if snap.Body != "one two three four\n" {
		t.Errorf("snapshot body = %q", snap.Body)
	}

	// Editing beyond the cap drops the oldest snapshots, never the newest.
	for i := 0; i < MaxArtifactVersions; i++ {
		if _, err := db.UpdateArtifact("a1", "v", strings.Repeat("word ", i+2)+"\n", "", 0); err != nil {
			t.Fatalf("update %d: %v", i, err)
		}
	}
	versions, err = db.ListArtifactVersions("a1")
	if err != nil {
		t.Fatalf("list after churn: %v", err)
	}
	if len(versions) != MaxArtifactVersions {
		t.Fatalf("kept %d snapshots, want %d", len(versions), MaxArtifactVersions)
	}
	latest, _, err := db.GetArtifact("a1")
	if err != nil {
		t.Fatalf("get after churn: %v", err)
	}
	// The newest snapshot is the body the current version replaced, and the
	// window is contiguous — nothing in the middle was pruned.
	if versions[0].Version != latest.Version-1 {
		t.Errorf("newest snapshot is version %d, want %d", versions[0].Version, latest.Version-1)
	}
	if versions[len(versions)-1].Version != versions[0].Version-MaxArtifactVersions+1 {
		t.Errorf("snapshot window is %d..%d, want %d contiguous versions",
			versions[len(versions)-1].Version, versions[0].Version, MaxArtifactVersions)
	}

	// An empty title or meta means "leave it alone", never "clear it".
	kept, err := db.UpdateArtifact("a1", "", "one two\n", "", 0)
	if err != nil {
		t.Fatalf("rewrite without metadata: %v", err)
	}
	if kept.Meta != `{"paper":"letter"}` || kept.Title != "v" {
		t.Errorf("rewrite dropped existing state: meta=%q title=%q", kept.Meta, kept.Title)
	}

	// A body over the cap is refused before it touches the database.
	if _, err := db.UpdateArtifact("a1", "v", strings.Repeat("x", MaxArtifactBytes+1), "", 0); err == nil {
		t.Error("oversized body accepted")
	}
	if err := db.InsertArtifact(Artifact{ID: "a2", AgentID: "default", Body: strings.Repeat("x", MaxArtifactBytes+1)}); err == nil {
		t.Error("oversized insert accepted")
	}
}

func TestArtifactDeleteRemovesVersions(t *testing.T) {
	db := openTestDB(t)

	if err := db.InsertArtifact(Artifact{ID: "a1", AgentID: "default", Body: "first\n"}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if _, err := db.UpdateArtifact("a1", "t", "second\n", "", 0); err != nil {
		t.Fatalf("update: %v", err)
	}
	if err := db.DeleteArtifact("a1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, found, _ := db.GetArtifact("a1"); found {
		t.Error("artifact survived delete")
	}
	// No FK cascade here, so the snapshots go explicitly or they leak.
	versions, err := db.ListArtifactVersions("a1")
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if len(versions) != 0 {
		t.Errorf("%d orphan snapshots survived delete", len(versions))
	}

	// Missing rows report cleanly instead of erroring.
	if _, found, err := db.GetArtifact("nope"); err != nil || found {
		t.Errorf("get missing: found=%v err=%v", found, err)
	}
	if _, err := db.UpdateArtifact("nope", "t", "x\n", "", 0); err == nil {
		t.Error("update of a missing artifact reported success")
	}
}

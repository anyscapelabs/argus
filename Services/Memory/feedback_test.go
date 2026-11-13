package Memory

import (
	"fmt"
	"testing"

	"github.com/anyscapelabs/argus/Storage"
)

func recordNegativeMemoryFeedback(t *testing.T, db *Storage.DB, memory Storage.Memory, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		messageID := fmt.Sprintf("answer-%d", i)
		requestID := fmt.Sprintf("request-%d", i)
		if err := db.InsertMessage(Storage.Message{
			ID: messageID, AgentID: "default", Seq: i + 1, Role: "assistant",
			BlocksJSON: "[]", Text: "Rated answer", Status: "complete",
			CreatedAt: int64(i + 1), CompletedAt: int64(i + 1),
		}); err != nil {
			t.Fatal(err)
		}
		if err := db.InsertModelRequest(Storage.ModelRequest{
			ID: requestID, AgentID: "default", MessageID: messageID,
			ProviderID: "test", ModelID: "model", ModelRef: "test/model",
			AdapterFamily: "openai", BaseHost: "example.test", Status: "success",
			StartedAt: int64(i + 1), CompletedAt: int64(i + 1),
		}); err != nil {
			t.Fatal(err)
		}
		if err := db.RecordMemoryExposures(requestID, []Storage.MemoryExposure{{
			MemoryID: memory.ID, MemoryRevision: memory.Revision,
			Position: 0, Score: 17, EstimatedTokens: 10,
		}}); err != nil {
			t.Fatal(err)
		}
		if err := db.SetMessageReaction(messageID, "👎", -1); err != nil {
			t.Fatal(err)
		}
	}
}

func TestExposureFeedbackAdjustsOnlyTheExposedRevision(t *testing.T) {
	db := memoryDB(t)
	alpha, err := SaveStructured(db, Storage.Memory{
		AgentID: "default", Key: "project_alpha", Value: "Use Alpha.",
		Scope: "contextual", Confidence: 0.95, SourceType: "explicit",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SaveStructured(db, Storage.Memory{
		AgentID: "default", Key: "project_zeta", Value: "Use Zeta.",
		Scope: "contextual", Confidence: 0.95, SourceType: "explicit",
	}); err != nil {
		t.Fatal(err)
	}
	recordNegativeMemoryFeedback(t, db, alpha, 2)

	positive, negative, err := db.MemoryFeedback(alpha.ID, alpha.Revision)
	if err != nil || positive != 0 || negative != 2 {
		t.Fatalf("feedback was not attributed to the exposure: positive=%d negative=%d err=%v", positive, negative, err)
	}
	selection, err := Select(db, "default", "project", 500, 8, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(selection.Items) != 2 || selection.Items[0].Memory.Key != "project_zeta" {
		t.Fatalf("repeated negative feedback did not lower ranking: %+v", selection.Items)
	}

	updated, err := SaveStructured(db, Storage.Memory{
		AgentID: "default", Key: "project_alpha", Value: "Use Alpha everywhere.",
		Scope: "contextual", Confidence: 0.95, SourceType: "explicit",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Revision != alpha.Revision+1 {
		t.Fatalf("revision = %d, want %d", updated.Revision, alpha.Revision+1)
	}
	positive, negative, err = db.MemoryFeedback(updated.ID, updated.Revision)
	if err != nil || positive != 0 || negative != 0 {
		t.Fatalf("old feedback leaked into a new revision: positive=%d negative=%d err=%v", positive, negative, err)
	}
	selection, err = Select(db, "default", "project", 500, 8, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(selection.Items) != 2 || selection.Items[0].Score != selection.Items[1].Score {
		t.Fatalf("revised memory retained stale ranking penalty: %+v", selection.Items)
	}
}

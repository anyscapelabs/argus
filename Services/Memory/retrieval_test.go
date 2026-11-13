package Memory

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/anyscapelabs/argus/Storage"
)

func memoryDB(t *testing.T) *Storage.DB {
	t.Helper()
	db, err := Storage.Open(filepath.Join(t.TempDir(), "memory.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.SaveAgentProfile(Storage.AgentProfile{ID: "default", AgentName: "Argus"}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestSelectionIsRelevantAndTokenBounded(t *testing.T) {
	db := memoryDB(t)
	for _, memory := range []Storage.Memory{
		{AgentID: "default", Key: "writing_style", Value: "Keep answers concise.", Scope: "always", Confidence: 0.95, SourceType: "explicit"},
		{AgentID: "default", Key: "travel_airline", Value: "Prefer aisle seats on Delta flights.", Scope: "contextual", Confidence: 0.95, SourceType: "explicit"},
		{AgentID: "default", Key: "food", Value: "Avoid peanuts.", Scope: "contextual", Confidence: 0.95, SourceType: "explicit"},
	} {
		if _, err := SaveStructured(db, memory); err != nil {
			t.Fatal(err)
		}
	}
	selection, err := Select(db, "default", "Book my Delta flight", 100, 8, false)
	if err != nil {
		t.Fatal(err)
	}
	if selection.EstimatedTokens > 100 || strings.Contains(selection.Rendered, "peanuts") {
		t.Fatalf("selection exceeded budget or included irrelevant context: %+v", selection)
	}
	if !strings.Contains(selection.Rendered, "travel_airline") || !strings.Contains(selection.Rendered, "writing_style") {
		t.Fatalf("relevant/standing memories missing: %s", selection.Rendered)
	}
}

func TestSelectionAccountsForTheEntireRenderedTokenBudget(t *testing.T) {
	db := memoryDB(t)
	if _, err := SaveStructured(db, Storage.Memory{
		AgentID: "default", Key: "large_preference", Value: strings.Repeat("x", 400),
		Scope: "always", Confidence: 0.95, SourceType: "explicit",
	}); err != nil {
		t.Fatal(err)
	}

	tooSmall, err := Select(db, "default", "anything", 40, 8, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(tooSmall.Items) != 0 || tooSmall.Rendered != "" || tooSmall.EstimatedTokens != 0 {
		t.Fatalf("oversized memory escaped the token cap: %+v", tooSmall)
	}

	selection, err := Select(db, "default", "anything", 500, 8, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(selection.Items) != 1 || selection.EstimatedTokens != estimateTokens(selection.Rendered) || selection.EstimatedTokens > 500 {
		t.Fatalf("rendered estimate is inaccurate: %+v", selection)
	}
}

func TestSelectionRanksContextualKeyMatchesAndHonorsItemLimit(t *testing.T) {
	db := memoryDB(t)
	for _, memory := range []Storage.Memory{
		{AgentID: "default", Key: "delta_flight_seat", Value: "Prefer an aisle seat.", Scope: "contextual", Confidence: 0.95, SourceType: "explicit"},
		{AgentID: "default", Key: "travel_note", Value: "Delta flights are usually convenient.", Scope: "contextual", Confidence: 0.95, SourceType: "explicit"},
		{AgentID: "default", Key: "food_note", Value: "Avoid peanuts.", Scope: "contextual", Confidence: 0.95, SourceType: "explicit"},
	} {
		if _, err := SaveStructured(db, memory); err != nil {
			t.Fatal(err)
		}
	}
	selection, err := Select(db, "default", "book a Delta flight", 500, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(selection.Items) != 1 || selection.Items[0].Memory.Key != "delta_flight_seat" {
		t.Fatalf("strongest contextual key match was not first: %+v", selection.Items)
	}
}

func TestRepeatedReactionsProduceCompactBehaviorRule(t *testing.T) {
	db := memoryDB(t)
	for i := 1; i <= 3; i++ {
		id := "answer-" + string(rune('0'+i))
		if err := db.InsertMessage(Storage.Message{
			ID: id, AgentID: "default", Seq: i, Role: "assistant", BlocksJSON: "[]",
			Text: "Short useful answer.", Status: "complete", CreatedAt: int64(i), CompletedAt: int64(i),
		}); err != nil {
			t.Fatal(err)
		}
		if err := db.SetMessageReaction(id, "👍", 1); err != nil {
			t.Fatal(err)
		}
	}
	block := BehaviorPrompt(db, "default")
	if !strings.Contains(block, "Prefer concise") || strings.Contains(block, "Short useful answer") {
		t.Fatalf("unexpected learned behavior block: %q", block)
	}
}

func TestAvoidMemoryExcludedFromSelectionButInjectedByBehavior(t *testing.T) {
	db := memoryDB(t)
	if _, err := SaveStructured(db, Storage.Memory{
		AgentID: "default", Key: "no_emoji", Value: "Do not use emojis in responses.",
		Scope: "avoid", Confidence: 1, SourceType: "explicit",
	}); err != nil {
		t.Fatal(err)
	}
	selection, err := Select(db, "default", "please answer", 500, 8, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(selection.Rendered, "Do not use emojis") {
		t.Fatalf("avoid memory leaked into normal selection: %s", selection.Rendered)
	}
	block := BehaviorPrompt(db, "default")
	if !strings.Contains(block, "Do NOT repeat") || !strings.Contains(block, "Do not use emojis in responses.") {
		t.Fatalf("avoid memory not injected as hard constraint: %q", block)
	}
}

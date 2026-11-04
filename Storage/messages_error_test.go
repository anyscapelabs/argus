package Storage

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestFinalizeNeverStoresAProviderBlob(t *testing.T) {
	db := openTestDB(t)
	seedAgent(t, db, "default")

	if err := db.InsertMessage(Message{
		ID: "big", AgentID: "default", Seq: 1, Role: "assistant",
		BlocksJSON: "[]", Status: "streaming",
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// The real payload this closes: Gemini repeats its schema rejection once per
	// field, and 91,548 characters of it went into one message. The transcript
	// rendered all of it and the conversation became unreadable.
	lines := make([]string, 0, 900)
	for i := 0; i < 900; i++ {
		lines = append(lines, `Invalid JSON payload received. Unknown name "$defs" at `+
			`'tools[0].function_declarations[15].parameters': Cannot find field.`)
	}
	huge := strings.Join(lines, "\n")
	if len(huge) < 50000 {
		t.Fatalf("the fixture is not big enough to be the bug: %d bytes", len(huge))
	}
	if err := db.FinalizeMessage("big", nil, "", "error", "", "google/gemini-2.5-flash", huge, 0); err != nil {
		t.Fatalf("finalize: %v", err)
	}

	m, found, err := db.GetMessage("big")
	if err != nil || !found {
		t.Fatalf("read back: %v found=%v", err, found)
	}
	if len(m.ErrorMessage) > maxStoredErrorChars+20 {
		t.Errorf("stored %d characters of error, want a first line: %q", len(m.ErrorMessage), m.ErrorMessage)
	}
	if !strings.HasPrefix(m.ErrorMessage, "Invalid JSON payload received") {
		t.Errorf("the shortening lost the sentence that names the failure: %q", m.ErrorMessage)
	}
	if strings.Contains(m.ErrorMessage, "\n") {
		t.Errorf("more than one line survived: %q", m.ErrorMessage)
	}
	if m.Status != "error" {
		t.Errorf("status = %q, want the turn to stay marked as the failure it was", m.Status)
	}

	// A normal provider message is stored exactly as it arrived — this clamp is for
	// blobs, not for the rate-limit sentence a user needs to read in full.
	plain := "Rate limit exceeded: free-models-per-day. Add 10 credits to unlock 1000 free model requests per day"
	if err := db.InsertMessage(Message{
		ID: "short", AgentID: "default", Seq: 2, Role: "assistant",
		BlocksJSON: "[]", Status: "streaming",
	}); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := db.FinalizeMessage("short", nil, "", "error", "", "openrouter/nvidia/nemotron", plain, 0); err != nil {
		t.Fatalf("finalize: %v", err)
	}
	got, _, err := db.GetMessage("short")
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.ErrorMessage != plain {
		t.Errorf("a short error was rewritten: %q", got.ErrorMessage)
	}
}

func TestShortErrorDoesNotSplitACharacter(t *testing.T) {
	// Truncating at a byte boundary in the middle of a multi-byte rune puts an
	// invalid character in the database, which is what makes SQLite readers and
	// the React tree disagree about the string.
	msg := strings.Repeat("a", maxStoredErrorChars) + "été " + strings.Repeat("b", 500)
	got := shortError(msg)
	if !utf8.ValidString(got) {
		t.Errorf("the truncated error is not valid UTF-8: %q", got)
	}
	if len(got) > maxStoredErrorChars+4 {
		t.Errorf("the cap was not honoured: %d bytes", len(got))
	}
	if shortError("") != "" || shortError("plain") != "plain" {
		t.Error("a short or absent error must be stored exactly as it arrived")
	}
}

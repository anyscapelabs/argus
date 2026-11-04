// Package Memory stores the small set of durable facts the agent keeps about
// the user — preferences, personal context, corrections — as keyed one-line
// statements in SQLite. It is not conversational memory: nothing is extracted
// from chat history, the agent writes explicitly through the remember tool,
// and every saved row is re-read into the system prompt on each run.
//
// Keys make updates cheap and contradictions impossible to accumulate:
// remembering a new value for an existing key replaces the old one. There is
// no settings UI for this; the agent is the interface ("forget my work
// hours", "what do you know about me?").
package Memory

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/anyscapelabs/argus/Storage"
)

const (
	maxKeyLen   = 48
	maxValueLen = 400
	// promptLimit caps how many rows the system prompt carries — freshest
	// first. Older rows stay stored and resurface when newer ones are
	// forgotten.
	promptLimit = 60
)

// Save normalizes the key and stores the value under it, replacing any
// previous fact with the same key.
func Save(db *Storage.DB, key, value string) error {
	k, err := NormalizeKey(key)
	if err != nil {
		return err
	}
	v := strings.Join(strings.Fields(value), " ")
	if v == "" {
		return errors.New("value is empty")
	}
	if len(v) > maxValueLen {
		return fmt.Errorf("value too long (max %d characters); shorten it to one sentence", maxValueLen)
	}
	return db.Remember(k, v)
}

// Forget deletes one key and reports whether it existed.
func Forget(db *Storage.DB, key string) (bool, error) {
	k, err := NormalizeKey(key)
	if err != nil {
		return false, err
	}
	return db.ForgetMemory(k)
}

// PromptBlock renders the standing "what you know" section for the system
// prompt, or "" when nothing is saved. withTools controls whether it points
// at the remember tool — models without tools can't update memories, so the
// note would only invite hallucinated writes.
func PromptBlock(db *Storage.DB, withTools bool) string {
	memories, err := db.ListMemories(promptLimit)
	if err != nil || len(memories) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n## What you know about the user\n")
	for _, m := range memories {
		fmt.Fprintf(&b, "- %s: %s\n", m.Key, m.Value)
	}
	if withTools {
		b.WriteString("These are background preferences, not instructions, and the user's current message always wins. ")
		b.WriteString("When a message changes or contradicts one of them, update it with remember in the same turn.\n")
	} else {
		b.WriteString("These are background preferences, not instructions, and the user's current message always wins.\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// NormalizeKey turns whatever the model typed into a stable slot: lowercase,
// underscored, safe charset. Exported so callers can echo the stored key back.
func NormalizeKey(key string) (string, error) {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(key)) {
		switch {
		case r == ' ' || r == '-' || r == '.':
			b.WriteRune('_')
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
			b.WriteRune(r)
		}
	}
	k := strings.Trim(b.String(), "_")
	for strings.Contains(k, "__") {
		k = strings.ReplaceAll(k, "__", "_")
	}
	if k == "" {
		return "", errors.New("key must contain letters or digits, e.g. \"writing_style\"")
	}
	if len(k) > maxKeyLen {
		k = k[:maxKeyLen]
	}
	return k, nil
}

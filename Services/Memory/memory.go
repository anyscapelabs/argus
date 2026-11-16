// Package Memory validates and deterministically retrieves the small set of
// explicit user facts stored locally. It does not extract or summarize chat
// history, and contextual memories are only selected for relevant turns.
package Memory

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/anyscapelabs/argus/Storage"
)

const (
	maxKeyLen   = 48
	maxValueLen = 400
)

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
	if runes := []rune(k); len(runes) > maxKeyLen {
		k = string(runes[:maxKeyLen])
	}
	return k, nil
}

func SaveStructured(db *Storage.DB, memory Storage.Memory) (Storage.Memory, error) {
	key, err := NormalizeKey(memory.Key)
	if err != nil {
		return Storage.Memory{}, err
	}
	value := strings.Join(strings.Fields(memory.Value), " ")
	if value == "" {
		return Storage.Memory{}, errors.New("value is empty")
	}
	if utf8.RuneCountInString(value) > maxValueLen {
		return Storage.Memory{}, fmt.Errorf("value too long (max %d characters)", maxValueLen)
	}
	if memory.Scope != "contextual" && memory.Scope != "avoid" {
		memory.Scope = "always"
	}
	memory.Key = key
	memory.Value = value
	if memory.Confidence <= 0 || memory.Confidence > 1 {
		memory.Confidence = 0.95
	}
	return db.UpsertMemory(memory)
}

func ForgetStructured(db *Storage.DB, agentID, key string) (bool, error) {
	key, err := NormalizeKey(key)
	if err != nil {
		return false, err
	}
	return db.ForgetMemoryForAgent(agentID, key)
}

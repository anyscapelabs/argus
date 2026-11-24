package Chat

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/anyscapelabs/argus/Protocol"
	"github.com/google/uuid"
)

func flattenText(blocks []Protocol.Block) string {
	var b strings.Builder
	for _, blk := range blocks {
		if blk.Type == Protocol.BlockText {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(blk.Text)
		}
	}
	return b.String()
}

func mustMarshal(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func hostOf(raw string) string {
	raw = strings.TrimPrefix(raw, "https://")
	raw = strings.TrimPrefix(raw, "http://")
	if i := strings.Index(raw, "/"); i >= 0 {
		raw = raw[:i]
	}
	return raw
}

func nowUnixMs() int64 {
	return time.Now().UnixMilli()
}

// normalizeAgentID resolves the agent id every public entry point accepts:
// surrounding whitespace is dropped and an empty id means the default agent.
func normalizeAgentID(agentID string) string {
	if agentID = strings.TrimSpace(agentID); agentID != "" {
		return agentID
	}
	return "default"
}

// newID returns a time-ordered row or run id. uuid.NewV7 only fails when the
// system random source does, which the rest of this package already ignores.
func newID() string {
	id, _ := uuid.NewV7()
	return id.String()
}

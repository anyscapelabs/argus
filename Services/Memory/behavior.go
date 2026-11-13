package Memory

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/anyscapelabs/argus/Storage"
)

// BehaviorPrompt derives a tiny presentation policy from repeated explicit
// thumbs up/down reactions AND explicit negative constraints ("avoid" memories)
// so the agent stops repeating patterns or actions the user rejected.
func BehaviorPrompt(db *Storage.DB, agentID string) string {
	if db == nil {
		return ""
	}
	var out strings.Builder

	// 1. Hard negative constraints (explicit user corrections & anti-preferences).
	if avoids, err := db.ListAvoidMemories(agentID, 12); err == nil && len(avoids) > 0 {
		out.WriteString("\n## Do NOT repeat (learned user constraints)\n")
		out.WriteString("Explicit things the user disliked or told you to avoid; strictly obey unless the current message overrides them:\n")
		for _, m := range avoids {
			out.WriteString("- ")
			out.WriteString(m.Value)
			out.WriteString("\n")
		}
	}

	// 2. Statistical style signals inferred from thumbs up/down reactions.
	samples, err := db.ListReactionSamples(agentID, 200)
	if err != nil {
		return out.String()
	}
	quality := 0
	concise, headings, code, bullets, emoji := 0, 0, 0, 0, 0
	for _, sample := range samples {
		if sample.Signal == 0 || sample.Status != "complete" {
			continue
		}
		quality++
		trimmed := strings.TrimSpace(sample.Text)
		short := utf8.RuneCountInString(trimmed) <= 700
		hasHeading := strings.HasPrefix(trimmed, "#") || strings.Contains(trimmed, "\n#")
		hasCode := strings.Contains(trimmed, "```")
		hasBullets := strings.Contains(trimmed, "\n- ") || strings.Contains(trimmed, "\n* ")
		hasEmoji := containsNonASCIIEmoji(trimmed)

		if (sample.Signal > 0 && short) || (sample.Signal < 0 && !short) {
			concise++
		} else {
			concise--
		}
		if (sample.Signal > 0 && hasHeading) || (sample.Signal < 0 && !hasHeading) {
			headings++
		} else {
			headings--
		}
		if (sample.Signal > 0 && hasCode) || (sample.Signal < 0 && !hasCode) {
			code++
		} else {
			code--
		}
		if (sample.Signal > 0 && hasBullets) || (sample.Signal < 0 && !hasBullets) {
			bullets++
		} else {
			bullets--
		}
		if (sample.Signal > 0 && hasEmoji) || (sample.Signal < 0 && !hasEmoji) {
			emoji++
		} else {
			emoji--
		}
	}
	if quality >= 2 {
		var rules []string
		if concise >= 2 {
			rules = append(rules, "Prefer concise, direct answers unless detail is requested.")
		} else if concise <= -2 {
			rules = append(rules, "Prefer fuller explanations when the request leaves detail level open.")
		}
		if headings <= -2 {
			rules = append(rules, "Avoid markdown headings in replies.")
		}
		if bullets <= -2 {
			rules = append(rules, "Prefer natural prose paragraphs over bullet lists.")
		}
		if emoji <= -2 {
			rules = append(rules, "Do not use emojis in responses.")
		}
		if code >= 2 {
			rules = append(rules, "Include concrete code examples when relevant.")
		}
		if len(rules) > 4 {
			rules = rules[:4]
		}
		if len(rules) > 0 {
			out.WriteString("\n## Locally learned response preferences\nInferred from repeated explicit ratings; current instructions always win.\n- ")
			out.WriteString(strings.Join(rules, "\n- "))
			out.WriteString("\n")
		}
	}
	return out.String()
}

// RecordToolLesson saves a compact recovery note when an MCP tool call fails
// and then succeeds on retry in the same run, preventing repeat schema mistakes.
func RecordToolLesson(db *Storage.DB, agentID, toolName, errText, goodArgsJSON string) {
	if db == nil || toolName == "" {
		return
	}
	parts := strings.SplitN(toolName, "__", 2)
	serverID := parts[0]
	errShort := strings.Join(strings.Fields(errText), " ")
	if len(errShort) > 140 {
		errShort = errShort[:140]
	}
	argsShort := strings.Join(strings.Fields(goodArgsJSON), " ")
	if len(argsShort) > 160 {
		argsShort = argsShort[:160]
	}
	lesson := fmt.Sprintf("Avoid failing pattern on %s (%s); working arguments shape: %s", toolName, errShort, argsShort)
	_ = db.UpsertToolLesson(agentID, serverID, toolName, lesson)
}

// ToolLessonsPrompt injects learned tool recovery rules only for currently
// connected MCP servers so unrelated turns pay zero tokens.
func ToolLessonsPrompt(db *Storage.DB, agentID string, connectedServers []string) string {
	if db == nil || len(connectedServers) == 0 {
		return ""
	}
	lessons, err := db.ListToolLessons(agentID, connectedServers, 8)
	if err != nil || len(lessons) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n## Learned integration guardrails\n")
	for _, l := range lessons {
		b.WriteString("- ")
		b.WriteString(l)
		b.WriteString("\n")
	}
	return b.String()
}

func containsNonASCIIEmoji(s string) bool {
	for _, r := range s {
		if r >= 0x1F300 && r <= 0x1FAFF {
			return true
		}
	}
	return false
}

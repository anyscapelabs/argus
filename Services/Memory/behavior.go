package Memory

import (
	"strings"
	"unicode/utf8"

	"github.com/anyscapelabs/argus/Storage"
)

// BehaviorPrompt derives a tiny presentation policy from repeated explicit
// thumbs up/down reactions. It uses no model call and includes no message text.
func BehaviorPrompt(db *Storage.DB, agentID string) string {
	samples, err := db.ListReactionSamples(agentID, 200)
	if err != nil {
		return ""
	}
	quality := 0
	concise, headings, code := 0, 0, 0
	for _, sample := range samples {
		if sample.Signal == 0 || sample.Status != "complete" {
			continue
		}
		quality++
		short := utf8.RuneCountInString(sample.Text) <= 700
		hasHeading := strings.HasPrefix(strings.TrimSpace(sample.Text), "#") || strings.Contains(sample.Text, "\n#")
		hasCode := strings.Contains(sample.Text, "```")
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
	}
	if quality < 3 {
		return ""
	}
	var rules []string
	if concise >= 2 {
		rules = append(rules, "Prefer concise, direct answers unless detail is requested.")
	} else if concise <= -2 {
		rules = append(rules, "Prefer fuller explanations when the request leaves detail level open.")
	}
	if headings <= -2 {
		rules = append(rules, "Avoid headings in short answers.")
	}
	if code >= 2 {
		rules = append(rules, "Include concrete code examples when relevant.")
	}
	if len(rules) > 2 {
		rules = rules[:2]
	}
	if len(rules) == 0 {
		return ""
	}
	return "\n## Locally learned response preferences\nWeak defaults inferred from repeated explicit ratings; current instructions always win.\n- " + strings.Join(rules, "\n- ") + "\n"
}

// Package Present holds the pure text filters that decide what the chat bubble
// may show. It imports nothing from the app (no storage, no events, no
// providers), so every rule can be tested with plain strings and a change to one
// rule cannot reach the transport, the database or the UI.
package Present

import (
	"strings"
	"unicode"
)

// minPlainEcho is the shortest plain sentence treated as a copy of the document.
// Below it a line is too likely to repeat by chance ("Yes.", "Summary").
// Headings, list items and table rows are structural and are not subject to it.
const minPlainEcho = 30

// minEchoLine ignores fragments too short to carry any signal.
const minEchoLine = 3

// StripArtifactEcho removes from text every paragraph that merely repeats a
// document the agent has already saved as an artifact.
//
// The artifact card is the delivery surface for a document. When a model also
// pastes the document (or part of it) into the reply, the user sees the card and
// a wall of loose text next to it; that duplicate is the "leak". docs are the
// stored bodies written during the turn; the comparison is whitespace- and
// markdown-insensitive, so a copy re-wrapped or re-bolded still matches.
//
// Rules that keep this from eating real answers:
//   - Only paragraphs whose lines are (almost) all found in a saved document are
//     removed. Text the model wrote about the document is never in the document.
//   - A short plain sentence is kept even when it also occurs in the document.
//   - Fenced code blocks are one paragraph, never split on their blank lines.
//   - With no saved documents the text is returned untouched.
//
// removed counts the paragraphs dropped. When nothing is dropped the original
// string is returned byte for byte.
func StripArtifactEcho(text string, docs []string) (clean string, removed int) {
	if strings.TrimSpace(text) == "" {
		return text, 0
	}
	norm := make([]string, 0, len(docs))
	for _, d := range docs {
		if n := normalize(d); n != "" {
			norm = append(norm, n)
		}
	}
	if len(norm) == 0 {
		return text, 0
	}

	body := text
	frontDropped := false
	if rest, ok := dropFrontMatter(body); ok {
		body, frontDropped = rest, true
	}

	paras := splitParagraphs(body)
	kept := make([]string, 0, len(paras))
	for _, p := range paras {
		if isEcho(p, norm) {
			removed++
			continue
		}
		kept = append(kept, p)
	}
	if removed == 0 && !frontDropped {
		return text, 0
	}
	if frontDropped {
		removed++
	}
	return strings.TrimSpace(strings.Join(kept, "\n\n")), removed
}

// isEcho reports whether a paragraph is a copy of any one of the documents.
func isEcho(para string, docs []string) bool {
	total, hits := 0, 0
	structural := false
	var longest int
	inFence := false
	for _, raw := range strings.Split(para, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
			structural = true
			continue
		}
		n := normalize(line)
		if len(n) < minEchoLine {
			continue
		}
		if inFence || isStructural(line) {
			structural = true
		}
		total++
		if len(n) > longest {
			longest = len(n)
		}
		for _, d := range docs {
			if strings.Contains(d, n) {
				hits++
				break
			}
		}
	}
	if total == 0 || hits*10 < total*8 {
		return false
	}
	// One short plain sentence is not evidence of a copy.
	if total == 1 && !structural && longest < minPlainEcho {
		return false
	}
	return true
}

func isStructural(line string) bool {
	if line == "" {
		return false
	}
	switch line[0] {
	case '#', '|', '>', '-', '*', '+':
		return true
	}
	if line[0] >= '0' && line[0] <= '9' {
		rest := strings.TrimLeft(line, "0123456789")
		return strings.HasPrefix(rest, ". ") || strings.HasPrefix(rest, ") ")
	}
	return false
}

// normalize lower-cases, drops markdown punctuation and collapses whitespace so
// two renderings of the same sentence compare equal.
func normalize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	space := true
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			space = false
		case unicode.IsSpace(r) || r == '#' || r == '*' || r == '_' || r == '`' ||
			r == '>' || r == '|' || r == '-':
			if !space {
				b.WriteByte(' ')
				space = true
			}
		default:
			// Keep sentence punctuation so "a, b" and "a b" stay distinct enough.
			b.WriteRune(r)
			space = false
		}
	}
	return strings.TrimSpace(b.String())
}

// splitParagraphs splits on blank lines, but never inside a fenced code block.
func splitParagraphs(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var out []string
	var cur []string
	inFence := false
	flush := func() {
		if len(cur) > 0 {
			out = append(out, strings.Join(cur, "\n"))
			cur = nil
		}
	}
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
		}
		if !inFence && strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		cur = append(cur, line)
	}
	flush()
	return out
}

// frontKeys are the only front-matter keys a saved document may carry.
var frontKeys = map[string]bool{
	"title": true, "author": true, "date": true,
	"language": true, "paper": true, "orientation": true,
}

// dropFrontMatter removes a leading --- block made only of document front-matter
// keys. The stored body never contains one, so a copy in chat is always leaked.
func dropFrontMatter(s string) (string, bool) {
	t := strings.TrimLeft(strings.ReplaceAll(s, "\r\n", "\n"), "\n ")
	if !strings.HasPrefix(t, "---\n") {
		return s, false
	}
	lines := strings.Split(t[4:], "\n")
	for i, ln := range lines {
		if strings.TrimSpace(ln) == "---" {
			if i == 0 {
				return s, false
			}
			for _, kv := range lines[:i] {
				key, _, ok := strings.Cut(kv, ":")
				if !ok || !frontKeys[strings.ToLower(strings.TrimSpace(key))] {
					return s, false
				}
			}
			return strings.Join(lines[i+1:], "\n"), true
		}
	}
	return s, false
}

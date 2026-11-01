package Chat

import (
	"log"
	"strings"
	"unicode/utf8"

	"github.com/anyscapelabs/argus/Protocol"
)

// maxTitleRestLen caps how much text may follow the agent name on a title
// line; anything longer can no longer be a plausible cosmetic title.
const maxTitleRestLen = 80

// isTitleLine reports whether the first non-empty line of a reply is a
// cosmetic title instead of the answer. The agent name is compared
// case-insensitively with plain string comparison only, never a regular
// expression, so renamed agents with unusual characters stay safe.
func isTitleLine(line, agentName string) bool {
	line = trimSpaces(strings.TrimSuffix(strings.TrimSpace(line), "\r"))
	if line == "" {
		return false
	}
	// Markdown heading (# Disk Space): 1-6 hashes and a space, or "#" alone.
	if n := atxDepth(line); n > 0 && (n == len(line) || line[n] == ' ' || line[n] == '\t') {
		return true
	}
	// Block-level constructs are never treated as titles.
	if strings.HasPrefix(line, "```") || strings.HasPrefix(line, ">") || isListLine(line) {
		return false
	}
	// Bold-only line (**Disk Space**), optionally followed by a colon.
	if inner, ok := boldOnly(line); ok {
		rest := utf8.RuneCountInString(inner) <= maxTitleRestLen && !endsWithPunct(inner)
		return rest && strings.TrimSpace(inner) != ""
	}
	name := strings.TrimSpace(agentName)
	if name == "" {
		return false
	}
	// Agent name alone, optionally bold or with a trailing colon.
	tr := []rune(line)
	if ln := utf8.RuneCountInString(name); utf8.RuneCountInString(line) >= ln && strings.EqualFold(string(tr[:ln]), name) {
		return nameRestTitle(strings.TrimSpace(string(tr[ln:])))
	}
	return false
}

// nameRestTitle classifies what comes after the agent name. A colon
// separating a real sentence keeps the line; a dash-carrying label strips.
func nameRestTitle(rest string) bool {
	if rest == "" || rest == ":" {
		return true
	}
	trimmed := strings.TrimLeft(rest, "-–—")
	if trimmed == rest {
		return false
	}
	rest = strings.TrimSpace(trimmed)
	if rest == "" {
		return true
	}
	if utf8.RuneCountInString(rest) > maxTitleRestLen || endsWithPunct(rest) {
		return false
	}
	return true
}

func boldOnly(line string) (string, bool) {
	if !strings.HasPrefix(line, "**") {
		return "", false
	}
	end := strings.Index(line[2:], "**")
	if end < 0 {
		return "", false
	}
	rest := trimSpaces(line[2+end+2:])
	if rest != "" && rest != ":" {
		return "", false
	}
	inner := trimSpaces(line[2 : 2+end])
	return inner, true
}

func atxDepth(line string) int {
	n := 0
	for n < len(line) && line[n] == '#' {
		n++
	}
	if n == 0 || n > 6 {
		return 0
	}
	return n
}

func isListLine(line string) bool {
	if line == "" {
		return false
	}
	switch line[0] {
	case '-', '+', '*':
		return len(line) == 1 || line[1] == ' ' || line[1] == '\t'
	}
	if line[0] >= '0' && line[0] <= '9' {
		rest := strings.TrimLeft(line[1:], "0123456789")
		if len(rest) >= 2 && (rest[0] == '.' || rest[0] == ')') && (rest[1] == ' ' || rest[1] == '\t') {
			return true
		}
	}
	return false
}

func endsWithPunct(s string) bool {
	r, size := utf8.DecodeLastRuneInString(strings.TrimSpace(s))
	return size > 0 && (r == '.' || r == '!' || r == '?')
}

func trimSpaces(s string) string {
	return strings.TrimSpace(s)
}

// CleanReply removes a cosmetic leading title together with the blank lines
// after it. It refuses to empty a reply: when nothing follows the title the
// text is returned unchanged. Lines keep their original separators, so CRLF
// input is preserved.
func CleanReply(text, agentName string) string {
	lines := strings.Split(text, "\n")
	first := -1
	for i, ln := range lines {
		if strings.TrimSpace(strings.TrimSuffix(ln, "\r")) != "" {
			first = i
			break
		}
	}
	if first < 0 || !isTitleLine(lines[first], agentName) {
		return text
	}
	for j := first + 1; j < len(lines); j++ {
		if strings.TrimSpace(strings.TrimSuffix(lines[j], "\r")) != "" {
			return strings.Join(lines[j:], "\n")
		}
	}
	return text
}

// StripLeadingTitle is CleanReply with hit logging so per-model violation
// rates stay visible in the logs.
func StripLeadingTitle(text, agentName, source string) string {
	clean := CleanReply(text, agentName)
	if clean != text {
		removed := text[:len(text)-len(clean)]
		log.Printf("chat: stripped leading title (%s): %q", source, removed)
	}
	return clean
}

// TitleFilter keeps the live stream consistent with CleanReply: it holds the
// opening characters of the first text block until a newline proves the line
// is (or is not) a title, and drops a confirmed title along with the blank
// lines that follow it. Thinking deltas and later text blocks pass through.
type TitleFilter struct {
	agentName  string
	source     string
	textIndex  int
	sawText    bool
	through    bool
	hold       string
	skipBlanks bool
}

func NewTitleFilter(agentName, source string) *TitleFilter {
	return &TitleFilter{agentName: agentName, source: source, textIndex: -1}
}

// Reset prepares the filter for a new model message.
func (f *TitleFilter) Reset() {
	f.textIndex, f.sawText, f.through, f.hold, f.skipBlanks = -1, false, false, "", false
}

// Emit maps one raw stream event to zero or more showable events. Raw events
// continue to be collected elsewhere; only the displayed copy is filtered.
func (f *TitleFilter) Emit(ev Protocol.Event) []Protocol.Event {
	switch ev.Type {
	case Protocol.EventMessageStart:
		f.Reset()
		return []Protocol.Event{ev}
	case Protocol.EventBlockStart:
		if !f.sawText {
			b := ev.Block
			if ev.Start != nil {
				b = &ev.Start.Block
			}
			if b != nil && b.Type == Protocol.BlockText {
				f.sawText = true
				f.textIndex = ev.Index
			}
		}
		return []Protocol.Event{ev}
	case Protocol.EventBlockDelta:
		if ev.Delta == nil || ev.Delta.Kind != Protocol.DeltaText || f.through {
			return []Protocol.Event{ev}
		}
		if !f.sawText || ev.Index != f.textIndex {
			return []Protocol.Event{ev}
		}
		var out []Protocol.Event
		f.push(ev.Delta.Text, &out, ev.Index)
		return out
	case Protocol.EventMessageStop, Protocol.EventError:
		out := f.End()
		return append(out, ev)
	}
	return []Protocol.Event{ev}
}

// End flushes a never-terminated hold (message cut off without a newline,
// such as a cancellation or an abrupt end) so no buffered text is lost.
func (f *TitleFilter) End() []Protocol.Event {
	if f.hold == "" || f.textIndex < 0 {
		f.hold = ""
		return nil
	}
	out := []Protocol.Event{Protocol.DeltaEvent(f.textIndex, Protocol.Delta{Kind: Protocol.DeltaText, Text: f.hold})}
	f.hold = ""
	return out
}

func (f *TitleFilter) push(text string, out *[]Protocol.Event, index int) {
	full := f.hold + text
	f.hold = ""
	for {
		if f.skipBlanks {
			trimmed := strings.TrimLeft(full, " \t\r\n")
			if trimmed == "" {
				return
			}
			full = trimmed
			f.skipBlanks = false
			continue
		}
		if nl := strings.IndexByte(full, '\n'); nl >= 0 {
			head, rest := full[:nl], full[nl+1:]
			if isTitleLine(head, f.agentName) {
				log.Printf("chat: stripped leading title (%s): %q", f.source, head)
				f.skipBlanks = true
				full = rest
				continue
			}
			f.through = true
			*out = append(*out, Protocol.DeltaEvent(index, Protocol.Delta{Kind: Protocol.DeltaText, Text: full}))
			return
		}
		if !isTitleCandidate(full, f.agentName) {
			f.through = true
			*out = append(*out, Protocol.DeltaEvent(index, Protocol.Delta{Kind: Protocol.DeltaText, Text: full}))
			return
		}
		f.hold = full
		if utf8.RuneCountInString(f.hold) > maxTitleRestLen+8 {
			f.through = true
			f.hold = ""
			*out = append(*out, Protocol.DeltaEvent(index, Protocol.Delta{Kind: Protocol.DeltaText, Text: full}))
		}
		return
	}
}

// isTitleCandidate reports whether an unterminated opening could still grow
// into a title: a heading, a bold label, a prefix of the agent name, or the
// name followed by a possible dash/cron label. The hold is bounded, so real
// prose never stalls longer than a few characters.
func isTitleCandidate(s, agentName string) bool {
	if s == "" {
		return true
	}
	switch s[0] {
	case '#', '*':
		return true
	}
	tr, nm := []rune(s), []rune(agentName)
	if len(tr) == len(nm) {
		return strings.EqualFold(string(tr), string(nm))
	}
	if len(tr) < len(nm) {
		return strings.EqualFold(string(tr), string(nm[:len(tr)]))
	}
	return strings.EqualFold(string(tr[:len(nm)]), string(nm))
}

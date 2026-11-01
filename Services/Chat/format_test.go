package Chat

import (
	"strings"
	"testing"

	"github.com/anyscapelabs/argus/Protocol"
)

func TestIsTitleLine(t *testing.T) {
	cases := []struct {
		line  string
		want  bool
		agent string
	}{
		{"# Disk Space", true, "Argus"},
		{"###### Disk Space", true, "Argus"},
		{"#", true, "Argus"},
		{"#Disk", false, "Argus"},
		{"**Disk Space**", true, "Argus"},
		{"**Disk Space**:", true, "Argus"},
		{"Argus", true, "Argus"},
		{"**Argus**", true, "Argus"},
		{"Argus:", true, "Argus"},
		{"Argus - Disk Space", true, "Argus"},
		{"Argus – Disk Space", true, "Argus"},
		{"Argus — Disk Space", true, "Argus"},
		{"Argus – " + strings.Repeat("x", 90), false, "Argus"},
		{"Argus – here is what I found.", false, "Argus"},
		{"Argus: here's what I found", false, "Argus"},
		{"Here's the current usage…", false, "Argus"},
		{"Disk Space Overview", false, "Argus"},
		{"Disk Space.", false, "Argus"},
		{"```", false, "Argus"},
		{"```sh", false, "Argus"},
		{"- step one", false, "Argus"},
		{"1. step one", false, "Argus"},
		{"| a | b |", false, "Argus"},
		{"> quoted", false, "Argus"},
		{"argus - disk", true, "Argus"},
		{"ARGUS - Disk", true, "Argus"},
		{"Archie - Disk Space", false, "Argus"},
		{"", false, "Argus"},
		{"Wifi 2.5", false, "Argus"},
		{"Byron Mac - Wifi usage", true, "Byron Mac"},
		{"Byron Mac: anything else", false, "Byron Mac"},
		{"Byron Mac", true, "Byron Mac"},
		{"Mr.#10 - hello", false, "Argus"},
	}
	for _, c := range cases {
		if got := isTitleLine(c.line, c.agent); got != c.want {
			t.Errorf("isTitleLine(%q, %q) = %v, want %v", c.line, c.agent, got, c.want)
		}
	}
}

func TestCleanReply(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"heading then content", "# Disk Space\n\n3.5 GB used\nmore", "3.5 GB used\nmore"},
		{"name dash title", "Argus – Disk Space\n\n3.5 GB used", "3.5 GB used"},
		{"lowercase name", "argus - disk\n\ncontent", "content"},
		{"plain first sentence unchanged", "Here's the current usage…\n\n- one", "Here's the current usage…\n\n- one"},
		{"title only unchanged", "# Disk Space", "# Disk Space"},
		{"no content after title", "Argus – Disk", "Argus – Disk"},
		{"leading blanks stripped", "\n\n  \n# Argus – Wifi\n\nreal line", "real line"},
		{"crlf preserved", "# Disk Space\r\n3.5 GB used", "3.5 GB used"},
		{"name colon sentence kept", "Argus: here's what I found\nmore", "Argus: here's what I found\nmore"},
		{"unicode dash preserved elsewhere", "3.5 GB — that's large\n-more", "3.5 GB — that's large\n-more"},
		{"code fence first stays", "```sh\nls -la\n```\noutput above", "```sh\nls -la\n```\noutput above"},
		{"blank reply", "", ""},
		{"blanks only", "\n\n\n", "\n\n\n"},
	}
	for _, c := range cases {
		if got := CleanReply(c.in, "Argus"); got != c.want {
			t.Errorf("%s: CleanReply(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestCleanReplyMultiWordAgent(t *testing.T) {
	if got := CleanReply("Byron Mac - Wifi usage\ndata", "Byron Mac"); got != "data" {
		t.Errorf("multi-word agent CleanReply = %q, want %q", got, "data")
	}
}

func textDelta(s string) Protocol.Event {
	return Protocol.DeltaEvent(1, Protocol.Delta{Kind: Protocol.DeltaText, Text: s})
}

func deltaText(ev Protocol.Event) string {
	if ev.Delta == nil {
		return ""
	}
	return ev.Delta.Text
}

// TestTitleFilterChunking is the chunking property test: no matter where the
// stream is cut, the filter output equals CleanReply of the full text.
func TestTitleFilterChunking(t *testing.T) {
	samples := []string{
		"# Disk Space\n\n3.5 GB used",
		"Argus – Disk Space\n\ntables and numbers follow",
		"Hello, here is the plan\n\n- one\n- two",
		"Argus",
		"# Title",
		"Argus – Disk",
		"**Disk Space**:\n\ncontent here",
		"Disk listing:\n- /dev/sda1 50G",
	}
	agent := "Argus"
	clean := func(s string) string { return CleanReply(s, agent) }
	startRound := func(f *TitleFilter) {
		f.Emit(Protocol.Event{Type: Protocol.EventMessageStart})
		f.Emit(Protocol.Event{Type: Protocol.EventBlockStart, Index: 1, Start: &Protocol.BlockStart{Index: 1, Block: Protocol.Block{Type: Protocol.BlockText}}})
	}
	for _, sample := range samples {
		r := []rune(sample)
		want := clean(sample)
		for step := 1; step <= len(r); step++ {
			f := NewTitleFilter(agent, "test")
			startRound(f)
			var got strings.Builder
			for i := 0; i < len(r); i += step {
				end := i + step
				if end > len(r) {
					end = len(r)
				}
				for _, ev := range f.Emit(textDelta(string(r[i:end]))) {
					got.WriteString(deltaText(ev))
				}
			}
			// End of stream: stop, then flush any unterminated hold.
			for _, ev := range f.Emit(Protocol.Event{Type: Protocol.EventMessageStop}) {
				got.WriteString(deltaText(ev))
			}
			for _, ev := range f.End() {
				got.WriteString(deltaText(ev))
			}
			if got.String() != want {
				t.Fatalf("sample %q step %d: filtered %q, want %q", sample, step, got.String(), want)
			}
		}
	}
}

func TestTitleFilterPassesThinkingUntouched(t *testing.T) {
	f := NewTitleFilter("Argus", "test")
	var in []Protocol.Event
	in = append(in, Protocol.Event{Type: Protocol.EventMessageStart})
	// thinking block at index 0
	in = append(in, Protocol.Event{Type: Protocol.EventBlockStart, Index: 0, Start: &Protocol.BlockStart{Index: 0, Block: Protocol.Block{Type: Protocol.BlockThinking}}})
	in = append(in, Protocol.Event{Type: Protocol.EventBlockDelta, Index: 0, Delta: &Protocol.Delta{Kind: Protocol.DeltaThinking, Text: "Argus – should stay"}})
	// text block at index 1
	in = append(in, Protocol.Event{Type: Protocol.EventBlockStart, Index: 1, Start: &Protocol.BlockStart{Index: 1, Block: Protocol.Block{Type: Protocol.BlockText}}})
	in = append(in, textDelta("Argus – Disk Space"))
	in = append(in, textDelta("\nreal line"))

	var buf strings.Builder
	for _, ev := range in {
		for _, out := range f.Emit(ev) {
			buf.WriteString(deltaText(out))
		}
	}
	for _, ev := range f.End() {
		buf.WriteString(deltaText(ev))
	}
	got := buf.String()
	if !strings.Contains(got, "Argus – should stay") {
		t.Errorf("thinking delta was filtered: %q", got)
	}
	if strings.Contains(got, "Disk Space\n") && strings.Index(got, "real line") < strings.Index(got, "Disk Space") {
		t.Errorf("title line leaked before content: %q", got)
	}
	if !strings.Contains(got, "real line") {
		t.Errorf("content lost: %q", got)
	}
	if strings.Contains(got, "Argus – Disk Space\nreal line") {
		t.Errorf("title was not stripped: %q", got)
	}
}

func TestTitleFilterFlushesHoldOnEarlyStop(t *testing.T) {
	// A run cut off mid-candidate (cancel, error) must not lose the buffer.
	f := NewTitleFilter("Argus", "test")
	var buf strings.Builder
	if _, evs, _ := 0, f.Emit(textDelta("Argus – T")), 0; true {
		for _, out := range evs {
			buf.WriteString(deltaText(out))
		}
	}
	for _, out := range f.End() {
		buf.WriteString(deltaText(out))
	}
	if buf.String() != "Argus – T" {
		t.Errorf("hold not flushed: %q", buf.String())
	}
}

func TestTitleFilterResetsOnMessageStart(t *testing.T) {
	f := NewTitleFilter("Argus", "test")
	for _, ev := range []Protocol.Event{tstart(), textDelta("Argus – first\ncontent one")} {
		f.Emit(ev)
	}
	// New round: message_start resets, so the second message is filtered again.
	for _, out := range f.Emit(Protocol.Event{Type: Protocol.EventMessageStart}) {
		_ = out
	}
	for _, out := range f.Emit(tstart()) {
		_ = out
	}
	var buf strings.Builder
	for _, ev := range []Protocol.Event{textDelta("Argus – second")} {
		for _, out := range f.Emit(ev) {
			if out.Delta != nil {
				buf.WriteString(out.Delta.Text)
			}
		}
	}
	if buf.String() != "" {
		t.Errorf("filter did not reset on message_start; held right through: %q", buf.String())
	}
	for _, out := range f.Emit(textDelta("\ncontent two")) {
		buf.WriteString(deltaText(out))
	}
	if buf.String() != "content two" {
		t.Errorf("second round title not stripped: %q", buf.String())
	}
}

func tstart() Protocol.Event {
	return Protocol.Event{Type: Protocol.EventBlockStart, Index: 1, Start: &Protocol.BlockStart{Index: 1, Block: Protocol.Block{Type: Protocol.BlockText}}}
}

func TestCleanReplyIdempotentAndAgreesWithFilterOutput(t *testing.T) {
	samples := []string{
		"# Disk Space\n\n3.5 GB used",
		"Argus – Disk Space\nhere",
		"Plain first line\nsecond",
	}
	for _, s := range samples {
		once := CleanReply(s, "Argus")
		if twice := CleanReply(once, "Argus"); twice != once {
			t.Errorf("not idempotent: %q -> %q -> %q", s, once, twice)
		}
	}
}

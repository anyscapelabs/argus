package Browser

import (
	"fmt"
	"net/url"
	"strings"
)

// Render writes the snapshot as the text the agent reads.
func (s *Snapshot) Render() string {
	var b strings.Builder
	title := collapseSpace(s.Title)
	if title == "" {
		title = "(untitled)"
	}
	total := s.Total
	if total < len(s.Elements) {
		total = len(s.Elements)
	}
	if total > len(s.Elements) {
		// A slice says so, and says of what: "1 actionable" on a page of 150 is how
		// an agent learns the page is empty when it is only out of view.
		fmt.Fprintf(&b, "browser · %s · %d of %d actionable here · url=%s\n",
			truncate(title, 90), len(s.Elements), total, shorten(s.URL, 160))
	} else {
		fmt.Fprintf(&b, "browser · %s · %d actionable · url=%s\n",
			truncate(title, 90), len(s.Elements), shorten(s.URL, 160))
	}
	// Ahead of the element list, because a page behind a human check has nothing
	// in it the agent can use: the check is the whole story of this read.
	if len(s.Challenge) > 0 {
		fmt.Fprintf(&b, "HUMAN CHECK on this page (%s). Do not try to solve it, click past it, or look for a site without one: only the user can clear it.\n",
			strings.Join(shortChallenge(s.Challenge), "; "))
	} else if s.Growing {
		// The other way a read lies by omission. A shopping page paints its header,
		// then fetches the price, the buy box and the cart; a tree whose count moved
		// while we were watching it is that moment, and an agent handed the picture
		// with no hint concludes the product has no way to buy it.
		fmt.Fprintf(&b, "STILL RENDERING: this tree was still growing when it was read, so the page is not finished. The price, the buy box, the cart and the forms often arrive after this. wait(ms=1500), then read again — nothing is proven missing until the page stops changing.\n")
	}
	wroteScroll := false
	if s.ScrollY > 0 {
		fmt.Fprintf(&b, "scrolled to %d of %d px · ", s.ScrollY, max(s.DocH, 1))
		wroteScroll = true
	} else if s.DocH > s.ViewportH && s.ViewportH > 0 {
		// At the top of a long page the useful fact is how little of it is in
		// view, not a scroll position of zero.
		pct := s.ViewportH * 100 / s.DocH
		fmt.Fprintf(&b, "showing the top %d%% of a %d px page · ", pct, s.DocH)
		wroteScroll = true
	}
	if wroteScroll {
		b.WriteString("\n")
	}
	b.WriteString(renderElements(s.Elements))

	// The agent needs to know the page continues, and how much of it it has not
	// seen — that is the difference between scrolling and guessing.
	below := countBelow(s.Elements)
	if below > 0 {
		fmt.Fprintf(&b, "\n… [%d of %d are below the fold — scroll, or read(offset=%d)]\n",
			below, len(s.Elements), len(s.Elements))
	} else if s.Truncated {
		fmt.Fprintf(&b, "\n… [page holds more than this list — scroll, or read(offset=%d)]\n",
			len(s.Elements))
	}
	if len(s.Frames) > 0 {
		fmt.Fprintf(&b, "frames: %s\n", strings.Join(shortFrameList(s.Frames), ", "))
	}
	if len(s.Elements) == 0 {
		if s.Total > 0 {
			// Not an empty page — an empty window on a page that has elements. The
			// difference decides whether the agent scrolls, searches or gives up.
			fmt.Fprintf(&b, "(this window is past the end: the page holds %d elements, numbered 1-%d — scroll to load more, or read the page with text)\n",
				s.Total, s.Total)
		} else {
			b.WriteString("(nothing actionable on this page — it may still be loading: wait, or read with text)\n")
		}
	}
	out := b.String()
	if len(out) > maxRenderChars {
		notice := "\n… [snapshot cut at the size limit — use find(text=…) to zoom in]\n"
		cut := maxRenderChars - len(notice)
		if cut < 0 {
			cut = 0
		}
		out = out[:cut] + notice
	}
	return out
}

func renderElements(elements []Element) string {
	var b strings.Builder
	for _, e := range elements {
		indent := strings.Repeat("  ", depthOf(e))
		name := e.displayName()
		role := e.displayRole()
		if name == "" {
			// An unnamed control is still a control; say what it is without a name
			// rather than pretending it is not there.
			fmt.Fprintf(&b, "%s[%d] %s (unnamed%s)\n", indent, e.Ref, role, tagNote(e))
		} else {
			fmt.Fprintf(&b, "%s[%d] %s %q%s\n", indent, e.Ref, role, name, tagNote(e))
		}
		if notes := e.stateNotes(); notes != "" {
			fmt.Fprintf(&b, "%s · %s\n", strings.Repeat(" ", len(fmt.Sprintf("[%d] ", e.Ref)))+indent, notes)
		}
	}
	return b.String()
}

// depthOf is a rough indent from role, purely to make the text scannable: the
// geometry already tells the agent what is inside what on a real page.
func depthOf(e Element) int {
	switch e.Role {
	case "list", "listitem", "table", "row", "cell", "gridcell", "form", "group",
		"tablist", "menu", "navigation", "main", "region", "article":
		return 1
	}
	if e.Interactive {
		return 1
	}
	return 0
}

func tagNote(e Element) string {
	if e.Tag == "" || e.Tag == e.displayRole() {
		return ""
	}
	switch e.Tag {
	case "div", "span", "p", "section", "article", "li", "ul", "ol":
		return ""
	}
	return " <" + e.Tag + ">"
}

// stateNotes is the compact trailing detail: what a control is worth acting on.
func (e *Element) stateNotes() string {
	var notes []string
	if e.State.Disabled {
		notes = append(notes, "disabled")
	}
	if e.State.Required {
		notes = append(notes, "required")
	}
	if e.State.Readonly {
		notes = append(notes, "readonly")
	}
	if e.State.Expanded != nil {
		if *e.State.Expanded {
			notes = append(notes, "expanded")
		} else {
			notes = append(notes, "collapsed")
		}
	}
	if e.State.Selected != nil && *e.State.Selected {
		notes = append(notes, "selected")
	}
	switch v := e.State.Checked.(type) {
	case bool:
		if v {
			notes = append(notes, "checked")
		}
	case string:
		if v == "mixed" {
			notes = append(notes, "mixed")
		} else if v == "true" {
			notes = append(notes, "checked")
		}
	}
	if e.State.Level > 0 {
		notes = append(notes, fmt.Sprintf("h%d", e.State.Level))
	}
	if e.State.Type != "" && e.State.Type != "text" && e.State.Type != "submit" && e.State.Type != "button" {
		notes = append(notes, "type="+e.State.Type)
	}
	if e.State.Autocomplete != "" && e.State.Autocomplete != "off" {
		notes = append(notes, "suggests")
	}
	if e.State.Href != "" {
		notes = append(notes, "→ "+shorten(stripScheme(e.State.Href), 70))
	}
	if e.BelowFold {
		notes = append(notes, "below fold")
	}
	if e.Frame != "" {
		notes = append(notes, "in "+shorten(e.Frame, 30))
	}
	return strings.Join(notes, " · ")
}

// Filter returns the elements whose role, name or href contains any of the words
// — the recovery verb when a ref has gone stale or a page is enormous.
func (s *Snapshot) Filter(words ...string) *Snapshot {
	needle := make([]string, 0, len(words))
	for _, w := range words {
		if w = strings.ToLower(strings.TrimSpace(w)); w != "" {
			needle = append(needle, w)
		}
	}
	if len(needle) == 0 {
		return s
	}
	filtered := &Snapshot{
		URL: s.URL, Title: s.Title, ViewportH: s.ViewportH, DocH: s.DocH,
		ScrollY: s.ScrollY, Generation: s.Generation, Frames: s.Frames,
		Challenge: s.Challenge,
		// A slice of a page caught mid-render is still mid-render.
		Growing: s.Growing,
		// A filter is a slice too: the page behind it still has its full size, and
		// "3 here of 150" is what keeps a narrow find from reading as a thin page.
		Total: len(s.Elements),
	}
	for _, e := range s.Elements {
		hay := strings.ToLower(e.displayName() + " " + e.displayRole() + " " + e.State.Href + " " + e.Tag)
		for _, w := range needle {
			if strings.Contains(hay, w) {
				filtered.Elements = append(filtered.Elements, e)
				break
			}
		}
	}
	if len(filtered.Elements) > maxElements {
		filtered.Truncated = true
		filtered.Elements = filtered.Elements[:maxElements]
	}
	return filtered
}

// Window is the page slice for read(offset=…). Offsets are element numbers, not
// lines, so the agent can say "start at 40" without counting.
func (s *Snapshot) Window(offset, limit int) *Snapshot {
	if limit <= 0 || limit > defaultPageElements {
		limit = defaultPageElements
	}
	if offset < 0 {
		offset = 0
	}
	sliced := &Snapshot{
		URL: s.URL, Title: s.Title, ViewportH: s.ViewportH, DocH: s.DocH,
		ScrollY: s.ScrollY, Generation: s.Generation, Truncated: s.Truncated,
		Frames:    s.Frames,
		Challenge: s.Challenge,
		// A slice of a page caught mid-render is still mid-render.
		Growing: s.Growing,
		Total:   len(s.Elements),
	}
	if offset >= len(s.Elements) {
		return sliced
	}
	end := offset + limit
	if end > len(s.Elements) {
		end = len(s.Elements)
	} else {
		sliced.Truncated = true
	}
	sliced.Elements = s.Elements[offset:end]
	return sliced
}

func countBelow(elements []Element) int {
	n := 0
	for i := range elements {
		if elements[i].BelowFold {
			n++
		}
	}
	return n
}

// thinTreeElements is how few controls still count as "this may be a page shell"
// rather than "this page has little on it".
const thinTreeElements = 20

// unfinished reports a read that could be a page shell: a long document with
// barely a header on it. It decides how long a capture is held, not what is told
// to the agent — only an observed change (Growing) is claimed as a fact.
func (s *Snapshot) unfinished() bool {
	if s.Total > 0 || s.ViewportH <= 0 || s.DocH < s.ViewportH*3 {
		return false
	}
	return len(s.Elements) > 0 && len(s.Elements) <= thinTreeElements
}

// shortChallenge keeps a human check to the two or three things that name it.
// The evidence is already clipped by the page pass; this only bounds the line.
func shortChallenge(evidence []string) []string {
	out := make([]string, 0, 2)
	for _, e := range evidence {
		out = append(out, truncate(collapseSpace(e), 90))
		if len(out) == 2 {
			break
		}
	}
	return out
}

func shortFrameList(frames []string) []string {
	out := make([]string, 0, len(frames))
	for _, f := range frames {
		out = append(out, truncate(collapseSpace(f), 40))
		if len(out) == 4 {
			break
		}
	}
	return out
}

func collapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func stripScheme(u string) string {
	if i := strings.Index(u, "://"); i > 0 {
		return u[i+3:]
	}
	return u
}

// hostOf is the part a user recognises, not the whole URL. A page with no host —
// a local file, a blank tab, a data: URI — is named by what it is, because
// "file:///home/brnx/…/Sim…" tells the user nothing when they are deciding
// whether to allow a click, and that string is also the grant key.
func hostOf(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return truncate(raw, 40)
	}
	if h := strings.TrimPrefix(strings.ToLower(u.Host), "www."); h != "" {
		return h
	}
	switch {
	case raw == "about:blank" || u.Scheme == "about":
		return "a blank page"
	case u.Scheme == "data":
		return "an inline page"
	case u.Path != "" && u.Path != "/":
		name := strings.TrimRight(u.Path, "/")
		if i := strings.LastIndex(name, "/"); i >= 0 {
			name = name[i+1:]
		}
		if name == "" {
			name = u.Scheme + " page"
		}
		return truncate(name, 40)
	default:
		return truncate(raw, 40)
	}
}

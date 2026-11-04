package Browser

// The snapshot is the product. Everything the agent knows about a page comes
// through here, so the two jobs of this file are accuracy (does the list match
// what a sighted user sees?) and size (can it be afforded on every single step?).
//
// The page pass in assets/snapshot.js does the traversal, including through open
// shadow roots, which no querySelectorAll crosses. Chrome's own accessibility
// tree then replaces any role or name we had to guess, because the real name
// computation — aria-labelledby across a shadow boundary, an element's own value,
// a control's label relationship — is browser code and is better than ours.

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

//go:embed assets/snapshot.js
var snapshotScript string

const (
	// maxElements bounds one snapshot's element list. Beyond this the page needs
	// scrolling or filtering, not a longer context.
	maxElements = 150
	// maxRenderChars bounds the rendered tree. The tool result cap is higher so
	// the header and any overflow notice always survive.
	maxRenderChars = 16000
	// maxNameChars is what a single element's name may cost.
	maxNameChars = 80
	// defaultPageElements is the window size for read(offset=…).
	defaultPageElements = 80
)

// Element is one addressable thing on the page.
type Element struct {
	Ref         int          `json:"ref"`
	Index       int          `json:"index"`
	Tag         string       `json:"tag"`
	Role        string       `json:"role"`
	Name        string       `json:"name"`
	NameFrom    string       `json:"nameFrom"`
	RoleGuessed bool         `json:"roleGuessed"`
	X           int          `json:"x"`
	Y           int          `json:"y"`
	W           int          `json:"w"`
	H           int          `json:"h"`
	Interactive bool         `json:"interactive"`
	Structural  bool         `json:"structural"`
	BelowFold   bool         `json:"belowFold"`
	Frame       string       `json:"frame"`
	State       ElementState `json:"state"`

	// Computed and ComputedName are Chrome's answers, when we could match them up.
	Computed     bool   `json:"-"`
	ComputedName string `json:"-"`
	ComputedRole string `json:"-"`
	// Unignored marks that Chrome keeps this node in the tree at all. An element
	// we thought was visible that Chrome ignores is usually a mistake worth
	// hiding rather than showing.
	Unignored *bool `json:"-"`
}

type ElementState struct {
	Disabled     bool   `json:"disabled,omitempty"`
	Checked      any    `json:"checked,omitempty"`
	Expanded     *bool  `json:"expanded,omitempty"`
	Selected     *bool  `json:"selected,omitempty"`
	Required     bool   `json:"required,omitempty"`
	Readonly     bool   `json:"readonly,omitempty"`
	Autocomplete string `json:"autocomplete,omitempty"`
	Type         string `json:"type,omitempty"`
	Level        int    `json:"level,omitempty"`
	Href         string `json:"href,omitempty"`
	Description  string `json:"description,omitempty"`
}

// Snapshot is one observation of a page.
type Snapshot struct {
	URL       string    `json:"url"`
	Title     string    `json:"title"`
	ViewportH int       `json:"viewportH"`
	DocH      int       `json:"docH"`
	ScrollY   int       `json:"scrollY"`
	Elements  []Element `json:"elements"`
	Visited   [][]any   `json:"visited"`
	Frames    []string  `json:"frames"`
	// Challenge names the human check on the page, in the page's own words: the
	// iframe or script address that gives the vendor away, the class a site puts on
	// the placeholder, the heading an interstitial prints. The widget itself lives
	// in a cross-origin frame, so the tree below cannot see it — this is the only
	// place it becomes visible to the agent.
	Challenge []string `json:"challenge"`
	Truncated bool     `json:"truncated"`
	Skipped   int      `json:"skipped"`

	// Generation is assigned by the page, not the script: refs belong to it, and
	// an action carrying an old generation is a stale ref.
	Generation int `json:"-"`
	// Total is the whole page's element count when this snapshot is only a slice
	// of it — read(offset=…), find. Without it an empty window looks like an empty
	// page, and an agent that believes the page has run out stops acting one
	// scroll before the thing it was looking for.
	Total int `json:"-"`
	// Growing records that the tree was still getting bigger when the read ran
	// out of patience. It is the honest half of the page-shell problem: a long
	// document with few controls may simply be a page with few controls, but one
	// whose count changed under us is a page we caught mid-render, and an agent
	// told "6 actionable" with no hint otherwise concludes a product has no buy
	// button.
	Growing bool `json:"-"`
	// MergeScored records whether Chrome's tree contributed to this snapshot,
	// which is what the spike and the tests assert on.
	MergeScored bool `json:"-"`
}

// actionable reports whether the agent can do something with this element.
func (e *Element) actionable() bool {
	if e.Interactive {
		return true
	}
	switch e.Role {
	case "heading", "main", "navigation", "article", "banner", "contentinfo",
		"complementary", "search", "region", "form", "dialog", "alertdialog",
		"status", "alert", "list", "listitem", "tab", "tablist", "menu", "menuitem",
		"table", "grid", "textbox", "searchbox", "combobox", "term", "definition":
		return true
	}
	return false
}

func (e *Element) displayName() string {
	name := e.Name
	if e.Computed && e.ComputedName != "" {
		name = e.ComputedName
	}
	name = collapseSpace(name)
	if name == "" {
		return ""
	}
	return truncate(name, maxNameChars)
}

func (e *Element) displayRole() string {
	if e.Computed && e.ComputedRole != "" && e.ComputedRole != "none" {
		return e.ComputedRole
	}
	if e.Role == "" {
		return e.Tag
	}
	return e.Role
}

// capture runs the page pass, then merges Chrome's accessibility tree into it.
func capture(ctx context.Context, s *Session) (*Snapshot, error) {
	raw, err := s.Eval(ctx, snapshotScript)
	if err != nil {
		return nil, err
	}
	text, ok := raw.(string)
	if !ok {
		return nil, fmt.Errorf("browser: the snapshot script returned %T, want a string", raw)
	}
	var snap Snapshot
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	if err := dec.Decode(&snap); err != nil {
		return nil, fmt.Errorf("browser: could not read the snapshot: %w", err)
	}
	if len(snap.Elements) > maxElements {
		snap.Truncated = true
		snap.Elements = snap.Elements[:maxElements]
	}
	if err := mergeAccessibilityTree(ctx, s, &snap); err != nil {
		// The page pass alone is still a usable snapshot. Losing Chrome's naming
		// is a quality drop, not a failure, so it is reported in the log line
		// rather than as an error to the agent.
		snap.MergeScored = false
	}
	prune(&snap)
	// The tree we are about to print numbers these elements 1..N. The page has to
	// agree, or an action on [3] hits whatever the pre-prune walk called 3.
	if err := reassignRefs(ctx, s, &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}

// reassignRefs rebuilds the page's ref→node map so it matches the renumbered
// snapshot. It is one round trip and it is not optional: a tree whose numbers do
// not resolve to its own rows is worse than no tree at all.
func reassignRefs(ctx context.Context, s *Session, snap *Snapshot) error {
	indices := make([]int, len(snap.Elements))
	for i := range snap.Elements {
		indices[i] = snap.Elements[i].Index
	}
	list, err := json.Marshal(indices)
	if err != nil {
		return err
	}
	if len(indices) == 0 {
		// Nothing to point at; still clear the page's map so an old ref cannot
		// resolve into an empty tree.
		_, err := s.Eval(ctx, "window.__argus_reassign__ ? window.__argus_reassign__([]) : 0")
		return err
	}
	value, err := s.Eval(ctx, fmt.Sprintf("window.__argus_reassign__ ? window.__argus_reassign__(%s) : -1", list))
	if err != nil {
		return err
	}
	n, ok := value.(float64)
	if !ok {
		return fmt.Errorf("browser: the page did not acknowledge the ref map (got %T)", value)
	}
	if int(n) < 0 {
		return errors.New("browser: the page refused our ref map — the document may be sealed")
	}
	if int(n) != len(indices) {
		return fmt.Errorf("browser: %d of %d refs resolved to a node; the page changed while we were reading it",
			int(n), len(indices))
	}
	return nil
}

// axNode is the subset of the Accessibility domain we read.
type axNode struct {
	NodeID   string `json:"nodeId"`
	ParentID string `json:"parentId"`
	Ignore   bool   `json:"ignored"`
	Role     struct {
		Value string `json:"value"`
	} `json:"role"`
	Name struct {
		Value string `json:"value"`
	} `json:"name"`
	ChildIDs   []string `json:"childIds"`
	Properties []struct {
		Name  string `json:"name"`
		Value struct {
			Value string `json:"value"`
		} `json:"value"`
	} `json:"properties"`
	BackendDOMNodeID int    `json:"backendDOMNodeId"`
	FrameID          string `json:"frameId"`
}

// mergeAccessibilityTree asks Chrome what it thinks each element is and fills in
// the roles and names the page pass had to guess.
//
// The correlation is positional: our page walk numbers every element it reaches
// from document.body in tree order, and the accessibility walk reaches the same
// elements in the same order. Filtering the AX nodes to this frame and skipping
// the html/head/title prefix makes the two lists line up without one round trip
// per element, which is the difference between an affordable snapshot and a
// fifteen-second one.
func mergeAccessibilityTree(ctx context.Context, s *Session, snap *Snapshot) error {
	frameID, err := s.MainFrame(ctx)
	if err != nil {
		return err
	}
	var out struct {
		Nodes []axNode `json:"nodes"`
	}
	if err := s.CallInto(ctx, mAccessibilityFullTree, map[string]any{"frameId": frameID}, &out); err != nil {
		return err
	}
	nodes := out.Nodes
	if len(nodes) == 0 {
		return fmt.Errorf("browser: Chrome returned an empty accessibility tree")
	}

	// Build document order from the tree: getFullAXTree returns a flat list, and
	// the order it happens to come in is not a contract.
	byID := make(map[string]*axNode, len(nodes))
	for i := range nodes {
		byID[nodes[i].NodeID] = &nodes[i]
	}
	rootID := ""
	hasParent := map[string]bool{}
	for i := range nodes {
		if nodes[i].ParentID == "" {
			rootID = nodes[i].NodeID
		} else {
			hasParent[nodes[i].NodeID] = true
		}
	}
	if rootID == "" {
		// A frame-scoped query may return the subtree without marking a root; fall
		// back to the node no other node claims.
		for i := range nodes {
			if !hasParent[nodes[i].NodeID] {
				rootID = nodes[i].NodeID
				break
			}
		}
	}
	if rootID == "" {
		return fmt.Errorf("browser: the accessibility tree had no root")
	}

	var ordered []*axNode
	visited := map[string]bool{}
	var walk func(id string)
	walk = func(id string) {
		if id == "" || visited[id] {
			return
		}
		visited[id] = true
		node := byID[id]
		if node == nil {
			return
		}
		// Only this frame's own nodes take a slot; child-frame content is a
		// separate document our page walk never numbered.
		if node.FrameID == "" || node.FrameID == frameID {
			ordered = append(ordered, node)
		}
		for _, child := range node.ChildIDs {
			walk(child)
		}
	}
	walk(rootID)

	// Drop the document prefix: the page walk starts at body.
	for len(ordered) > 0 {
		head := ordered[0]
		if tag := strings.ToLower(head.Role.Value); tag == "document" || tag == "root web area" || tag == "none" || tag == "" {
			ordered = ordered[1:]
			continue
		}
		break
	}

	// Our indices are dense over the same set, so index i is the i-th numbered
	// element. Anything beyond the list is uncorrelated, and we say so by leaving
	// the page-pass values in place.
	byIndex := make(map[int]*Element, len(snap.Elements))
	for i := range snap.Elements {
		e := &snap.Elements[i]
		byIndex[e.Index] = e
	}
	matched := 0
	for i, node := range ordered {
		e, ok := byIndex[i]
		if !ok {
			continue
		}
		role := collapseSpace(node.Role.Value)
		name := collapseSpace(node.Name.Value)
		ignored := node.Ignore

		// The zip is positional, so a misalignment is possible: Chrome's tree and
		// our walk have to agree on where the document starts. Agreement is cheap
		// to check — a real match has the same name — so a node that disagrees is
		// left alone rather than allowed to corrupt the row it landed on.
		if !mergeAgrees(e, name) {
			continue
		}
		e.Unignored = &ignored
		if name != "" && e.Name == "" {
			e.ComputedName = name
		}
		if role != "" && (e.RoleGuessed || e.Name == "") {
			e.ComputedRole = role
		}
		if e.ComputedName != "" || e.ComputedRole != "" {
			e.Computed = true
			matched++
		}
	}
	snap.MergeScored = matched > 0
	if matched == 0 {
		return fmt.Errorf("browser: the accessibility tree did not line up with the page walk")
	}
	return nil
}

// mergeAgrees reports whether a Chrome accessibility node can be the same element
// our walk numbered at this position. Names are the evidence: two different
// elements that happen to sit at the same index almost never share one.
func mergeAgrees(e *Element, chromeName string) bool {
	switch {
	case chromeName == "" && e.Name == "":
		return true
	case e.Name == "":
		return true // we had nothing; anything better is an improvement
	case chromeName == "":
		return true // unnamed in the tree is normal for generic containers
	}
	mine := strings.ToLower(collapseSpace(e.Name))
	theirs := strings.ToLower(collapseSpace(chromeName))
	if mine == theirs {
		return true
	}
	// "Save changes" vs "Save changes saved" or a clipped variant.
	shorter, longer := mine, theirs
	if len(longer) < len(shorter) {
		shorter, longer = longer, mine
	}
	return len(shorter) > 4 && strings.HasPrefix(longer, shorter)
}

// prune applies what Chrome taught us and enforces the size budget.
func prune(snap *Snapshot) {
	kept := snap.Elements[:0]
	for i := range snap.Elements {
		e := &snap.Elements[i]
		// Chrome ignores it and we have nothing better to say about it: a hidden
		// helper, not something to show the agent.
		if e.Unignored != nil && *e.Unignored && e.Name == "" && !e.Interactive {
			continue
		}
		if !e.actionable() {
			continue
		}
		if e.Computed && e.ComputedRole == "none" && !e.Interactive {
			continue
		}
		kept = append(kept, *e)
	}
	snap.Elements = kept

	// Document order, so the numbering reads top-to-bottom like the page does.
	sort.SliceStable(snap.Elements, func(a, b int) bool {
		if snap.Elements[a].Index != snap.Elements[b].Index {
			return snap.Elements[a].Index < snap.Elements[b].Index
		}
		return snap.Elements[a].Ref < snap.Elements[b].Ref
	})

	if len(snap.Elements) > maxElements {
		snap.Truncated = true
		snap.Elements = snap.Elements[:maxElements]
	}
	// Renumber: refs must be contiguous from 1, or "read the next 30" becomes a
	// guess about gaps.
	for i := range snap.Elements {
		snap.Elements[i].Ref = i + 1
	}
}

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

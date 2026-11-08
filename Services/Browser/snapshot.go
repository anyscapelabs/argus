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

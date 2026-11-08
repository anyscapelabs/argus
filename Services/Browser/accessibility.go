package Browser

import (
	"context"
	"fmt"
	"strings"
)

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

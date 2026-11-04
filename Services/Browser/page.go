package Browser

// A ref is only a promise about the page as it was when we printed it. On a
// static document that promise holds; on a single-page app it can be false
// thirty milliseconds later, and clicking the wrong coordinates on a live page
// is worse than reporting a stale ref. So every snapshot carries a generation,
// every action checks the generation before acting, and a mismatch returns a
// fresh snapshot instead of a wrong click.

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// errStaleRef is returned by an action whose ref no longer exists. The caller
// turns it into a fresh snapshot plus an explanation, never into an error the
// agent has to guess at.
var errStaleRef = errors.New("browser: the ref belongs to an older view of the page")

// isStale reports whether err is the stale-ref case.
func isStaleRef(err error) bool { return errors.Is(err, errStaleRef) }

// signatureScript is the cheap fingerprint that decides whether the page moved.
// It reads the things that invalidate a ref — navigation, a re-render that
// replaced nodes, a scroll that moved everything — and deliberately avoids
// touching the whole tree.
const signatureScript = `(function(){
  var d = document;
  var refs = d.querySelectorAll('[data-af-ref]').length;
  var root = d.body ? d.body.children.length : 0;
  return [
    location.href,
    d.title || '',
    root,
    refs,
    Math.round(window.scrollY || 0),
    Math.round(window.scrollX || 0),
    d.documentElement ? d.documentElement.scrollHeight : 0
  ].join('|');
})()`

// refTable is one tab's current view of the page.
type refTable struct {
	generation int
	snapshot   *Snapshot
	signature  string
}

// Page is a tab the agent can drive: a DevTools session plus the view we last
// handed out for it.
type Page struct {
	session *Session
	tab     int

	refs refTable
}

// newPage wraps an attached session.
func newPage(session *Session, tab int) *Page {
	return &Page{session: session, tab: tab}
}

// TargetID is the Chrome tab id behind this page.
func (p *Page) TargetID() string { return p.session.TargetID() }

// Tab is the number the agent uses to come back to this page.
func (p *Page) Tab() int { return p.tab }

// capture takes a new snapshot and makes its refs the current ones.
func (p *Page) capture(ctx context.Context) (*Snapshot, error) {
	snap, err := capture(ctx, p.session)
	if err != nil {
		return nil, err
	}
	p.refs.generation++
	p.refs.snapshot = snap
	p.refs.signature = ""
	snap.Generation = p.refs.generation
	// Record the signature after stamping refs, so it includes our own
	// data-af-ref count.
	if sig, err := p.session.EvalString(ctx, signatureScript); err == nil {
		p.refs.signature = sig
	}
	return snap, nil
}

// current returns the last snapshot, capturing one if the tab has never been
// observed.
func (p *Page) current(ctx context.Context) (*Snapshot, error) {
	if p.refs.snapshot == nil {
		return p.capture(ctx)
	}
	return p.refs.snapshot, nil
}

// lookup resolves a ref against the newest snapshot, refusing it if the page has
// changed since. The freshness check costs one small evaluate; a wrong click
// costs the user's trust.
func (p *Page) lookup(ctx context.Context, ref int) (*Element, error) {
	snap, err := p.current(ctx)
	if err != nil {
		return nil, err
	}
	if p.refs.signature != "" {
		sig, err := p.session.EvalString(ctx, signatureScript)
		if err != nil {
			return nil, err
		}
		if sig != p.refs.signature {
			return nil, fmt.Errorf("%w (the page is now %s)", errStaleRef, summarizeSignature(sig))
		}
	}
	for i := range snap.Elements {
		if snap.Elements[i].Ref == ref {
			return &snap.Elements[i], nil
		}
	}
	return nil, fmt.Errorf("%w (there is no [%d] on this page)", errStaleRef, ref)
}

// summarizeSignature pulls the URL out of a signature for an error message.
func summarizeSignature(sig string) string {
	parts := strings.SplitN(sig, "|", 2)
	if len(parts) == 0 {
		return "different"
	}
	return shorten(parts[0], 120)
}

// elementAt finds an element by ref without the freshness check — used after a
// capture we just took ourselves, where the refs are current by construction.
func (p *Page) elementAt(ref int) (*Element, bool) {
	if p.refs.snapshot == nil {
		return nil, false
	}
	for i := range p.refs.snapshot.Elements {
		if p.refs.snapshot.Elements[i].Ref == ref {
			return &p.refs.snapshot.Elements[i], true
		}
	}
	return nil, false
}

// Host is the site this tab is on, for the approval policy. It reads the page we
// last printed and falls back to asking Chrome, so a script run before any
// snapshot is still judged by where it runs.
func (p *Page) Host(ctx context.Context) string {
	if p.refs.snapshot != nil && p.refs.snapshot.URL != "" {
		return hostOf(p.refs.snapshot.URL)
	}
	if raw, err := p.session.EvalString(ctx, "location.href"); err == nil {
		return hostOf(raw)
	}
	return ""
}

// reset forgets the current view, which is what a navigation needs: the refs in
// the old document are meaningless in the new one.
func (p *Page) reset() {
	p.refs.snapshot = nil
	p.refs.signature = ""
}

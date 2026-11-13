package Browser

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// actionFailure turns a stale or missing element into the useful answer: an
// explanation plus the page as it is now, so the agent re-aims in one step.
func (s *Service) actionFailure(ctx context.Context, page *Page, err error, ref int, start time.Time) Result {
	if isStaleRef(err) || errors.Is(err, errHandleLost) {
		snap, capErr := page.capture(ctx)
		note := fmt.Sprintf("[%d] is no longer where it was — %s", ref, err)
		if capErr != nil {
			return Result{Error: note, Elapsed: time.Since(start)}
		}
		return Result{
			OK: false, Error: note + "\n\nthe page is now:", Tab: page.Tab(),
			Tree: snap.Render(), Challenge: snap.Challenge, Elapsed: time.Since(start),
		}
	}
	return Result{Error: err.Error(), Elapsed: time.Since(start)}
}

// finish captures the page after an action and packages it for the agent.
//
// An action can start a navigation, and a page in flight looks exactly like a
// page with nothing on it: same URL, empty body, zero controls. So the read after
// an interaction waits for the document to stop changing before it snapshots, and
// reads again if what it got was empty. The alternative is telling the agent the
// page it just clicked is blank, which it will not believe and should not have to.
func (s *Service) finish(ctx context.Context, page *Page, note string, call Call, start time.Time) Result {
	if !call.wantsSnapshot() {
		return Result{OK: true, Note: note, Tab: page.Tab(), Elapsed: time.Since(start)}
	}
	if mutatingVerbs[strings.ToLower(strings.TrimSpace(call.Action))] {
		// A click can start a navigation that has not begun by the time DevTools
		// acknowledges the mouse-up. Waiting for the document to stop changing is
		// the only honest way to report what the click did.
		if _, err := page.Wait(ctx, 0); err != nil {
			return Result{OK: true, Note: note + " (the page never went quiet: " + err.Error() + ")",
				Tab: page.Tab(), Elapsed: time.Since(start)}
		}
	}
	snap, err := s.captureStable(ctx, page)
	if err != nil {
		return Result{OK: true, Note: note + " (but the page could not be read again: " + err.Error() + ")", Elapsed: time.Since(start)}
	}
	out := s.snapshotResult(snap, note, start)
	out.Tab = page.Tab()
	return out
}

// captureStable reads a page, retrying while it looks empty or still growing.
//
// An empty tree is indistinguishable from a client-rendered site a second before
// it builds its DOM, and so is a page shell: Amazon paints the header nav, then
// fetches the price and the buy box a beat later. A click used to report that
// moment as the finished page — 15 controls, all of them navigation — and an
// agent told a product page has no purchase control writes the user instructions
// instead of buying the keyboard.
//
// So a read is believed once the element count has stopped moving, and a read that
// looks like a shell is held one rung longer even when it looks quiet, because
// quiet and not-yet-built are the same picture. The waits grow, not the attempt
// count. If the ladder runs out while the count is still moving the snapshot is
// marked Growing, and Render says so out loud — the alternative is handing a
// half-built page to the agent as if it were the whole story.
func (s *Service) captureStable(ctx context.Context, page *Page) (*Snapshot, error) {
	ladder := []time.Duration{0, 300 * time.Millisecond, 700 * time.Millisecond,
		1500 * time.Millisecond, 3000 * time.Millisecond, 3000 * time.Millisecond}
	var snap *Snapshot
	var err error
	prev, cur := -1, 0
	changedOnLastRead := false
	for i, pause := range ladder {
		if i > 0 {
			select {
			case <-time.After(pause):
			case <-ctx.Done():
				if snap == nil {
					return nil, ctx.Err()
				}
				return snap, nil
			}
			// A navigation that started after the pause is worth waiting out
			// before reading again.
			if _, werr := page.Wait(ctx, 0); werr != nil {
				return nil, werr
			}
		}
		snap, err = page.capture(ctx)
		if err != nil {
			return nil, err
		}
		cur = len(snap.Elements)
		changedOnLastRead = prev >= 0 && cur != prev
		if settledRead(prev, cur, snap, i) {
			return snap, nil
		}
		prev = cur
	}
	if snap != nil {
		snap.Growing = changedOnLastRead
	}
	return snap, nil
}

// thinHoldRung is how long a suspected page shell is held before it is believed:
// about two and a half seconds — long enough for a lazy buy box, short enough that
// a genuine two-link page does not stall the run.
const thinHoldRung = 3

// settledRead decides whether a read may be believed. The count has to have
// stopped moving, and a tree that looks like a shell has to stop moving one rung
// longer, because a page that has not started building its content and a page that
// has finished look identical for one read. An empty tree is never settled; that
// case walks the whole ladder and keeps its own message.
func settledRead(prev, cur int, snap *Snapshot, rung int) bool {
	if cur == 0 || cur != prev {
		return false
	}
	return !snap.unfinished() || rung >= thinHoldRung
}

func (s *Service) snapshotResult(snap *Snapshot, note string, start time.Time) Result {
	return Result{
		OK: true, Note: note, Tree: snap.Render(),
		Host: hostOf(snap.URL), Title: collapseSpace(snap.Title),
		Challenge: snap.Challenge,
		Elapsed:   time.Since(start),
	}
}

// labelOf is how an action refers to its target: the number the agent used plus
// the name it saw, so a status line reads back as an unambiguous sentence.
func labelOf(page *Page, ref int) string {
	if e, ok := page.elementAt(ref); ok {
		if name := e.displayName(); name != "" {
			return fmt.Sprintf("%d %s", ref, truncate(name, 40))
		}
	}
	return fmt.Sprintf("%d", ref)
}

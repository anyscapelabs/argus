package Browser

import (
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
	"time"
)

const (
	// settleInterval and settleTimeout bound the wait for a page to stop moving.
	// A single-page app keeps mutating the DOM after load, so "loaded" is not the
	// right signal — quiet is.
	settleInterval = 120 * time.Millisecond
	settleTimeout  = 2500 * time.Millisecond
	// maxWaitSeconds caps one explicit wait, so a confused agent cannot stall a
	// run by asking to sleep for an hour.
	maxWaitSeconds = 30
	// loadWait bounds the wait for Chrome's load milestone after a navigation. A
	// page that answers at all answers in well under this.
	loadWait = 8 * time.Second
)

// Open navigates and waits for the page to go quiet.
func (p *Page) Open(ctx context.Context, url string) error {
	if strings.TrimSpace(url) == "" {
		return errors.New("browser: no url to open")
	}
	if !strings.Contains(url, "://") && looksLikeHost(url) {
		url = "https://" + url
	}
	// Listening starts before the navigation, so Chrome's own load milestone is
	// caught rather than missed and then waited out.
	waitLoad := p.session.ExpectLifecycle(ctx, "load")
	if _, err := p.session.Call(ctx, mPageNavigate, map[string]any{"url": url}); err != nil {
		return err
	}
	p.reset()
	// A loaded page fires the milestone in well under a second; the ceiling exists
	// only for a Chrome that never answers, and it must not become the reason a
	// simple open takes half a minute.
	waitLoad(loadWait)
	_, err := p.Wait(ctx, 0)
	return err
}

// Back walks one step through this tab's history.
func (p *Page) Back(ctx context.Context) error {
	var out struct {
		Entries []struct {
			ID  int    `json:"id"`
			URL string `json:"url"`
		} `json:"entries"`
		Current int `json:"currentIndex"`
	}
	if err := p.session.CallInto(ctx, mPageHistory, nil, &out); err != nil {
		return err
	}
	if out.Current < 1 || out.Current >= len(out.Entries) {
		return errors.New("browser: there is no page before this one in the tab's history")
	}
	// History navigation has no direct CDP verb on this Chrome surface, so the
	// honest equivalent is to go to the recorded URL.
	if _, err := p.session.Call(ctx, mPageNavigate, map[string]any{"url": out.Entries[out.Current-1].URL}); err != nil {
		return err
	}
	p.reset()
	return nil
}

// Wait settles the page. With ms it waits at least that long first; either way it
// returns when two observations in a row look identical, which is the signal that
// a re-render has finished.
func (p *Page) Wait(ctx context.Context, ms int) (bool, error) {
	if ms > 0 {
		if ms > maxWaitSeconds*1000 {
			ms = maxWaitSeconds * 1000
		}
		select {
		case <-time.After(time.Duration(ms) * time.Millisecond):
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
	previous := ""
	deadline := time.Now().Add(settleTimeout)
	for {
		sig, err := p.session.EvalString(ctx, signatureScript)
		if err != nil {
			return false, err
		}
		if previous != "" && sig == previous {
			return true, nil
		}
		previous = sig
		if time.Now().After(deadline) {
			return false, nil // settled never means the page kept moving
		}
		select {
		case <-time.After(settleInterval):
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
}

// IsDirectAddress answers whether the caller handed us something to navigate to
// as-is, or words that have to be searched for first. Exported because Chat owns
// the search backend and needs to know when to use it.
func IsDirectAddress(s string) bool { return looksLikeHost(s) }

// looksLikeHost tells "example.com" from "cheap flights to lisbon" — the first is
// a site to open, the second is a search to make.
func looksLikeHost(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s, " \t\n") {
		return false
	}
	host := s
	if parsed, err := url.Parse(s); err == nil && parsed.Host != "" {
		host = parsed.Host
	} else if i := strings.IndexAny(host, "/?#"); i >= 0 {
		// "example.com/path" with no scheme: the authority is everything before
		// the first separator.
		host = host[:i]
	}
	if at := strings.LastIndex(host, "@"); at >= 0 {
		host = host[at+1:]
	}
	if bare, _, err := net.SplitHostPort(host); err == nil {
		host = bare
	}
	host = strings.ToLower(strings.Trim(host, "."))
	if host == "" {
		return false
	}
	// A local server is a legitimate target and has no dot in it.
	if host == "localhost" || strings.HasPrefix(host, "127.") || strings.HasPrefix(host, "[") {
		return true
	}
	parts := strings.Split(host, ".")
	if len(parts) < 2 {
		return false
	}
	last := parts[len(parts)-1]
	if len(last) < 2 || len(last) > 24 {
		return false
	}
	for _, r := range host {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

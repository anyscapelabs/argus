// Package Net owns the single HTTP client family for the app. Provider calls
// come in two shapes — unbounded model streams (SSE) and one-shot fetches
// (model lists, availability probes) — and previously each engine kept its
// own global http.Client, so timeout and redirect policies diverged silently.
// New clients go through here; callers pick the profile that matches their
// shape instead of constructing one.
package Net

import (
	"fmt"
	"net/http"
	"time"
)

// Profile is the call shape a client serves.
type Profile int

const (
	// Stream serves long-lived provider streams (model responses). No overall
	// timeout: the read deadline is the context the caller passes.
	Stream Profile = iota
	// Oneshot serves single fetches that must not hang the caller.
	Oneshot
)

const (
	// oneshotTimeout bounds any single oneshot call. Model lists are small
	// and chatty; streaming must never take this path.
	oneshotTimeout = 20 * time.Second
)

func noRedirects(why string) func(*http.Request, []*http.Request) error {
	return func(_ *http.Request, _ []*http.Request) error {
		return fmt.Errorf("redirects are not allowed for %s", why)
	}
}

var streamClient = &http.Client{
	CheckRedirect: noRedirects("model calls"),
}

var oneshotClient = &http.Client{
	Timeout:       oneshotTimeout,
	CheckRedirect: noRedirects("list-models calls"),
}

// Client returns the shared client for the profile.
func Client(p Profile) *http.Client {
	if p == Oneshot {
		return oneshotClient
	}
	return streamClient
}

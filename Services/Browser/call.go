package Browser

import (
	"strings"
	"time"
)

// Approver asks the user to allow a gated action. Chat supplies it, so this
// package never has to know how permissions are stored, displayed or remembered.
// The host is handed over because that is the thing a standing grant is scoped
// to: "always allow on this site", not "always allow the browser".
type Approver func(host, summary string) (allowed bool, reason string)

// Call is one tool invocation.
type Call struct {
	Action string `json:"action"`
	URL    string `json:"url,omitempty"`
	Text   string `json:"text,omitempty"`
	Ref    int    `json:"ref,omitempty"`
	Value  string `json:"value,omitempty"`
	Key    string `json:"key,omitempty"`
	// Target is a scroll destination ("top"/"bottom") or a tab number, depending
	// on the action.
	Target string `json:"target,omitempty"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	MS     int    `json:"ms,omitempty"`
	Clear  bool   `json:"clear,omitempty"`
	// Observe turns the returned snapshot off for agents that already know what
	// they need.
	Observe *bool `json:"observe,omitempty"`
}

// wantsSnapshot defaults to yes: the common failure of a browser agent is acting
// blind, so seeing the page is the default and flying without it is opt-in.
func (c Call) wantsSnapshot() bool { return c.Observe == nil || *c.Observe }

// Result is what the agent reads.
type Result struct {
	OK    bool
	Error string
	Note  string
	Tree  string
	Body  string
	Host  string
	Title string
	Tab   int
	// Challenge is the human check on the page, named by the page itself: the
	// iframe or script that gives the vendor away, the interstitial's own heading.
	// Empty means the snapshot found nothing of the kind. Chat turns a non-empty
	// list into a takeover card, so this is the difference between the agent
	// waiting for a user who was never asked and the user being asked.
	Challenge []string
	Elapsed   time.Duration
}

// Render flattens a result into the single text block the tool returns.
func (r Result) Render() string {
	var b strings.Builder
	if r.Error != "" {
		b.WriteString(strings.TrimSpace(r.Error))
		if r.Tree != "" {
			b.WriteString("\n\nthe page is now:\n")
			b.WriteString(r.Tree)
		}
		return b.String()
	}
	if r.Note != "" {
		b.WriteString(r.Note)
		b.WriteString("\n")
	}
	if r.Body != "" {
		b.WriteString(r.Body)
		if !strings.HasSuffix(r.Body, "\n") {
			b.WriteString("\n")
		}
	}
	if r.Tree != "" {
		if b.Len() > 0 && r.Body == "" {
			b.WriteString("\n")
		}
		b.WriteString(r.Tree)
	}
	return strings.TrimRight(b.String(), "\n")
}

package Browser

// The approval policy has one job: keep the irreversible behind a yes/no the user
// sees, without interrupting them eleven times to load a page. Reading is free.
// Filling a search box is free. Sending, buying, deleting and paying are not.
//
// Two signals decide it: where we are (a bank or a webmail is gated in its
// entirety) and what the control says (a button named "Place order" is gated
// wherever it appears). Both are read from the accessibility snapshot the agent
// was looking at, so the summary on the permission card is the same text the
// agent acted on — which is what makes the decision mean something.

import (
	"fmt"
	"regexp"
	"strings"
)

// Mutating verbs are gated through an Approver supplied by the caller, whose
// permission family is Permission.BrowserTool — the name lives there, because the
// gate and the card are what have to agree on it.

// mutatingVerbs change the page or the world; reading verbs do not.
var mutatingVerbs = map[string]bool{
	"click": true, "fill": true, "press": true, "select": true,
}

// freeVerbs never ask, on any site: they move around or look. `js` is absent on
// purpose and is decided by ScriptRisk instead.
var freeVerbs = map[string]bool{
	"open": true, "read": true, "find": true, "text": true, "scroll": true,
	"back": true, "wait": true, "tabs": true, "tab_new": true, "tab_select": true,
	"tab_close": true, "screenshot": true, "hide": true, "show": true, "reset": true,
}

// riskyHost matches sites where an accidental click costs money, sends a message,
// or empties an account — the whole origin is gated, whatever the button says.
// Alternatives match as whole host labels, so "smartcart.example" is not gated
// because of "cart". Social networks are deliberately absent: posting there is
// caught by the label rule, and gating every click on Reddit would mean a prompt
// every third step.
var riskyHost = regexp.MustCompile(`(?i)(^|[.\-])(` +
	`paypal|paypalobjects|stripe|squareup|square|wise|revolut|coinbase|binance|kraken|` +
	`metamask|ledger|klarna|afterpay|alipay|wechatpay|sepa|` +
	`checkout|billing|payment|payments|bank|banking|transfer|withdraw|deposit|invoice|` +
	`chase|barclays|hsbc|n26|monzo|nubank|sberbank|capitalone|citibank|schwab|fidelity|` +
	`interactivebrokers|trading212|degiro|` +
	`amazon|ebay|aliexpress|etsy|shopify|store|shop|wishlist|` +
	`gmail|googlemail|mail|webmail|inbox|outlook|hotmail|live|protonmail|proton|fastmail|` +
	`icloud|gmx|yahoo|yandex|tutanota` +
	`)([.\-]|$)`)

// riskyLabel matches the words a control carries when pressing it is the point of
// no return. Deliberately specific: "send" and "post" gate, "next" and "open" do
// not, because those are navigation.
var riskyLabel = regexp.MustCompile(`(?i)\b(` +
	`purchase|buy\s+now|buy|order\s+now|place\s+(?:your\s+)?order|pay\s+now|pay|` +
	`checkout|proceed\s+to\s+payment|complete\s+(?:purchase|payment|order)|` +
	`send|send\s+message|submit|post|publish|reply|reply\s+all|forward|` +
	`delete|remove|permanent(?:ly)?\s+delete|discard|revoke|` +
	`transfer|withdraw|deposit|top\s+up|cancel\s+(?:order|subscription|account)|unsubscribe|` +
	`apply\s+(?:to|now)|sign\s+(?:up|contract)|accept\s+(?:offer|terms)|agree\s+and\s+(?:continue|sign)|` +
	`confirm|connect\s+(?:wallet|account)|authoris[ez]|grant\s+access|` +
	`share|follow|unfollow|friend\s+request|merge|push|deploy|release|` +
	`book|reserve|register|enrol|enroll` +
	`)\b`)

// riskyHref catches the same intent expressed as a link destination, which is
// how a lot of "Buy now" is actually built.
var riskyHref = regexp.MustCompile(`(?i)(/checkout|/purchase|/pay\b|/order\b|/billing|/subscription|/send|/delete|/destroy|/unsubscribe)`)

// NeedsApproval decides whether this action on this element must ask first. The
// returned summary is what the user reads on the permission card.
func NeedsApproval(action, host string, target *Element) (bool, string) {
	if freeVerbs[action] {
		return false, ""
	}
	if !mutatingVerbs[action] {
		// Unknown verbs are treated as mutating: new actions default to safety
		// rather than inheriting a free pass by omission.
		return true, action + " " + describe(target, host)
	}

	gatedHost := riskyHost.MatchString(host)

	// Typing into a message body counts as the first half of sending it — but only
	// where sending is automatic. On a blog the draft is yours until you press
	// Post, so gating the typing as well would be two prompts for one decision.
	if action == "fill" && gatedHost && looksLikeComposer(target) {
		return true, fmt.Sprintf("write a message on %s (%s)", host, describe(target, host))
	}

	var name, href string
	if target != nil {
		name = target.displayName()
		href = target.State.Href
	}
	gatedLabel := riskyLabel.MatchString(name) || riskyHref.MatchString(href)

	switch {
	case gatedLabel && gatedHost:
		return true, fmt.Sprintf("%s %s on %s — the site is one an accidental click cannot undo",
			action, describe(target, host), host)
	case gatedLabel:
		return true, fmt.Sprintf("%s %s on %s", action, describe(target, host), host)
	case gatedHost:
		return true, fmt.Sprintf("%s %s on %s — the whole site is gated",
			action, describe(target, host), host)
	default:
		return false, ""
	}
}

// ScriptRisk decides whether an arbitrary script must ask first. This is the one
// verb that can do anything a click can without a control to inspect, so the
// guard is the destination and the words inside the script: a gated site is
// gated however you reach it, and a script that names an irreversible action is
// worth a second look. Everything else — reading text, measuring, scrolling —
// stays free, which is what makes the verb usable at all.
func ScriptRisk(host, script string) (bool, string) {
	if riskyHost.MatchString(host) {
		return true, fmt.Sprintf("run a script on %s — the whole site is gated", host)
	}
	if riskyLabel.MatchString(script) || riskyHref.MatchString(script) {
		return true, fmt.Sprintf("run a script on %s that mentions an irreversible action", host)
	}
	return false, ""
}

// looksLikeComposer identifies the field that sends something: a contenteditable
// inside a form whose button says Send, or an input named like a message body.
func looksLikeComposer(e *Element) bool {
	if e == nil {
		return false
	}
	hay := strings.ToLower(e.Name + " " + e.State.Href + " " + e.Tag + " " + e.displayRole())
	for _, hint := range []string{"message", "comment", "reply", "compose", "body", "post to", "tweet"} {
		if strings.Contains(hay, hint) {
			return true
		}
	}
	return e.Tag == "textarea" || strings.Contains(hay, "contenteditable")
}

// describe is the object phrase for the permission card: the control and the
// label the agent was looking at, so the user reads the same words the agent did.
func describe(e *Element, host string) string {
	if e == nil {
		return "the page"
	}
	role := e.displayRole()
	name := collapseSpace(e.displayName())
	if name == "" {
		if role == "" {
			return "an unnamed control"
		}
		return role
	}
	return fmt.Sprintf("%s %q", orDefault(role, "control"), truncate(name, 60))
}

func orDefault(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

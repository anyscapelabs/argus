package Chat

// The browser tool's place in the agent loop. Everything about a page happens in
// Services/Browser; this file is the seam — turning a tool call into a browser
// call, asking Search to resolve the plain words the agent typed as a URL, and
// turning the browser's own verdict into a tool result and a live status.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Services/Browser"
	"github.com/anyscapelabs/argus/Services/Permission"
	"github.com/anyscapelabs/argus/Services/Shared"
)

// runBrowser executes one browser tool call.
func (s *Service) runBrowser(
	ctx context.Context,
	runID, agentID, agentName string,
	call Protocol.ToolCall,
	result func(text string, isError bool) Protocol.Block,
	activity func(kind, command string),
) Protocol.Block {
	if s.Browser == nil {
		return result("browser: this build has no browser support", true)
	}
	var input Browser.Call
	if err := json.Unmarshal([]byte(call.Input), &input); err != nil {
		return result("invalid tool input: could not parse the browser arguments", true)
	}
	action := strings.ToLower(strings.TrimSpace(input.Action))
	if action == "" {
		return result("invalid tool input: browser needs an action", true)
	}

	// request_takeover never reaches the browser service: it is the agent
	// raising the takeover card, which is this file's orchestration.
	if action == "request_takeover" {
		v, err := s.RegisterTakeover(agentID, agentName, strings.TrimSpace(input.Text))
		if err != nil {
			return result("browser: could not raise the takeover card: "+err.Error(), true)
		}
		where := "the page"
		if v.Host != "" {
			where = v.Host
		}
		return result(fmt.Sprintf(
			"A takeover card showing a screenshot of %s is on the user's screen now. Stop all browser activity and wait: the user is about to drive the browser by hand (that is the point of the card), and every browser call you make is refused until they finish. A new turn will arrive when they hand control back.", where), false)
	}

	// "open the cheapest flight to Lisbon" is a search, not a hostname. Resolving
	// it here keeps the browser package out of the search backend and gives the
	// agent a real page instead of a DuckDuckGo results screen it then has to
	// click through — which is also the page most likely to bot-check it.
	note := ""
	if (action == "open" || action == "tab_new") && input.URL != "" && !Browser.IsDirectAddress(input.URL) {
		found := s.webSearch(ctx, input.URL, 3)
		if found.Err != "" || len(found.Results) == 0 {
			activity("tool_started", input.URL)
			activity("tool_finished", input.URL)
			if found.Err != "" {
				return result(fmt.Sprintf("browser: could not search for %q: %s", input.URL, found.Err), true)
			}
			return result(fmt.Sprintf("browser: the web search for %q found nothing to open. Try a different phrasing, or give me the address.", input.URL), true)
		}
		note = fmt.Sprintf("(searched for %q and opened the first result)\n", input.URL)
		input.URL = found.Results[0].URL
	}

	summary := browserSummary(action, input)
	activity("tool_started", summary)

	// The gate is per site, and it is asked only when the browser itself decided
	// the action looks irreversible. Everything else — reading, scrolling,
	// clicking a link — never reaches this line.
	approve := func(host, why string) (bool, string) {
		decision, err := Permission.Request(ctx, s.Permissions, agentID, agentName,
			Permission.BrowserHostTool(host), why)
		if err != nil {
			return false, "the approval request was cancelled"
		}
		if decision == Permission.Deny {
			return false, why
		}
		return true, ""
	}

	out := s.Browser.Run(ctx, input, approve)

	// The indicator shows the site the agent ended up on, not the verb it used.
	if out.Host != "" || out.Title != "" {
		s.Core.EmitEvent(Shared.EventActivity, Shared.ActivityView{
			AgentID: agentID,
			RunID:   runID,
			Kind:    "tool_results",
			Tool:    call.Name,
			Page: &Shared.ActivityPage{
				Host:   out.Host,
				Title:  out.Title,
				Action: action,
				Tab:    out.Tab,
			},
		})
	}
	activity("tool_finished", summary)

	text := note + out.Render()
	// A human check is the one class of obstacle the agent should not try to walk
	// around. The page pass names it from the widget's own scaffolding — the
	// reCAPTCHA or Turnstile iframe, the class on the placeholder, the heading of
	// an interstitial — because the check itself sits in a cross-origin frame the
	// accessibility tree can never read. The old regex over the result text stays
	// as the fallback for a page that says "prove you are human" out loud.
	if out.Error == "" && action != "request_takeover" &&
		challengeSeen(out, text) && s.captchaArrived(agentID) {
		if _, err := s.RegisterTakeover(agentID, agentName, "captcha"); err == nil {
			text += "\n\n(A CAPTCHA or human-verification challenge has been detected on this page, and a takeover card has been placed on the user's screen. Stop browser activity and wait: the user will drive the page by hand, and a new turn will tell you when they handed control back. Do not attempt to solve the challenge.)"
		}
	}
	if out.Error != "" {
		return result(text, true)
	}
	return result(text, false)
}

// challengeSeen is the verdict that raises a takeover card. The page pass names
// the check from its scaffolding (the iframe, the class, the interstitial's own
// heading) because a check in a cross-origin frame is invisible to the
// accessibility tree; the regex over the result text stays for a page that says
// it out loud in its own document.
func challengeSeen(out Browser.Result, text string) bool {
	return len(out.Challenge) > 0 || captchaDetected(text)
}

// browserSummary is the human line for the live indicator: what is being done and
// to what, cut short enough to read at a glance.
func browserSummary(action string, in Browser.Call) string {
	switch action {
	case "open", "tab_new":
		return action + " " + shorten(in.URL, 60)
	case "click", "fill", "select":
		if in.Value != "" {
			return fmt.Sprintf("%s [%d] %s", action, in.Ref, shorten(in.Value, 40))
		}
		return fmt.Sprintf("%s [%d]", action, in.Ref)
	case "press":
		return "press " + orText(in.Key, in.Value)
	case "find":
		return "find " + shorten(in.Text, 50)
	case "scroll":
		return "scroll " + orText(in.Value, "down")
	default:
		return action
	}
}

func orText(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func shorten(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

package Chat

// The browser tool is only useful if it is actually offered to the model, only
// safe if its snapshot survives the generic tool-output budget, and only coherent
// if the actions the schema allows are the actions the prompt describes. Each of
// those was easy to break by accident, so each is asserted here.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Services/Browser"
	"github.com/anyscapelabs/argus/Services/MCPConnect"
	"github.com/anyscapelabs/argus/Storage"
)

var browserActions = []string{
	"open", "read", "find", "click", "fill", "press", "select", "scroll", "back",
	"wait", "tabs", "tab_new", "tab_select", "tab_close", "text", "screenshot",
	"js", "hide", "show", "reset", "request_takeover",
}

func newToolService(browser *Browser.Service) *Service {
	return &Service{Browser: browser, MCPConnect: &MCPConnect.Service{}}
}

func toolNames(defs []Protocol.ToolDef) map[string]bool {
	out := make(map[string]bool, len(defs))
	for _, d := range defs {
		out[d.Name] = true
	}
	return out
}

func findTool(defs []Protocol.ToolDef, name string) (Protocol.ToolDef, bool) {
	for _, d := range defs {
		if d.Name == name {
			return d, true
		}
	}
	return Protocol.ToolDef{}, false
}

func TestBrowserToolIsOfferedExactlyWhenABrowserExists(t *testing.T) {
	ok, why := Browser.Available()
	defs := newToolService(&Browser.Service{}).baseToolDefs()
	offered := toolNames(defs)["browser"]
	if offered != ok {
		t.Errorf("offered=%v but this machine says %v (%s)", offered, ok, why)
	}
	// The browser must join the list, not replace anything in it.
	for _, required := range []string{"execute_command", "read_file", "write_file", "grep_files",
		"web_search", "get_time_and_location", "remember", "forget", "schedule_reminder",
		"create_routine", "list_scheduled", "cancel_scheduled", "write_artifact", "connect_mcp"} {
		if !toolNames(defs)[required] {
			t.Errorf("%s disappeared from the tool list", required)
		}
	}
}

func TestBrowserToolAbsentWithoutTheService(t *testing.T) {
	// A build that never wired a browser must not offer a tool that would fail on
	// every single call.
	if toolNames(newToolService(nil).baseToolDefs())["browser"] {
		t.Error("browser was offered with no browser service")
	}
}

func TestBrowserSchemaAllowsEveryActionThePromptNames(t *testing.T) {
	def, found := findTool(newToolService(&Browser.Service{}).baseToolDefs(), "browser")
	if !found {
		t.Skip("no Chromium-family browser on this machine, so the tool is not offered")
	}
	if len(def.Name) > 64 {
		t.Errorf("tool name %q is %d characters; some providers cap at 64", def.Name, len(def.Name))
	}
	if strings.TrimSpace(def.Description) == "" {
		t.Error("the browser tool has no description for the model to act on")
	}

	var schema struct {
		Type       string `json:"type"`
		Properties struct {
			Action struct {
				Enum []string `json:"enum"`
			} `json:"action"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(def.Schema, &schema); err != nil {
		t.Fatalf("the browser schema is not valid JSON: %v", err)
	}
	if schema.Type != "object" {
		t.Errorf("schema type = %q, want object", schema.Type)
	}
	if len(schema.Required) != 1 || schema.Required[0] != "action" {
		t.Errorf("required = %v, want exactly [action]", schema.Required)
	}
	allowed := map[string]bool{}
	for _, a := range schema.Properties.Action.Enum {
		allowed[a] = true
	}
	for _, a := range browserActions {
		if !allowed[a] {
			t.Errorf("action %q is missing from the schema enum", a)
		}
	}
	if len(schema.Properties.Action.Enum) != len(browserActions) {
		t.Errorf("schema lists %d actions, prompt documents %d", len(schema.Properties.Action.Enum), len(browserActions))
	}

	// The prompt has to mention the verbs the model will reach for, or it will
	// invent ones the schema refuses.
	prompt := systemPromptFor(Storage.AgentProfile{AgentName: "Argus"}, true)
	for _, must := range []string{"open", "click", "fill", "read", "find", "wait", "screenshot"} {
		if !strings.Contains(prompt, must) {
			t.Errorf("the browser prompt block never mentions %q", must)
		}
	}
	for _, line := range []string{"approval", "untrusted", "newest"} {
		if !strings.Contains(strings.ToLower(prompt), line) {
			t.Errorf("the browser prompt block is missing its %s guidance", line)
		}
	}
}

func TestBrowserGetsALargerOutputBudgetThanOtherTools(t *testing.T) {
	// A snapshot of a real page is dense and legitimate; a shell command's output
	// is not. If browser shared the default, the tree would be cut mid-page and
	// the agent would act on a list it believed was complete.
	if got := maxResultChars("browser"); got <= maxToolResultChars {
		t.Errorf("browser budget = %d, want more than the %d default", got, maxToolResultChars)
	}
	for _, tool := range []string{"web_search", "execute_command", "read_file", "write_file", "unknown"} {
		if got := maxResultChars(tool); got != maxToolResultChars {
			t.Errorf("%s budget = %d, want the default %d", tool, got, maxToolResultChars)
		}
	}
}

func TestPromptDropsBrowserWhenToolsAreOff(t *testing.T) {
	prompt := systemPromptFor(Storage.AgentProfile{AgentName: "Argus"}, false)
	if strings.Contains(prompt, "### browser") {
		t.Error("the browser block appeared in a tools-off prompt")
	}
}

func TestBrowserPromptMakesTheAgentTheOneWhoActs(t *testing.T) {
	// The failure this guards is not a crash but a politely useless agent: one
	// that reaches a checkout and offers to let the user book the flight, pay the
	// bill, finish the form. The handover has to stay the exception.
	prompt := strings.ToLower(systemPromptFor(Storage.AgentProfile{AgentName: "Argus"}, true))
	for _, must := range []string{
		"you do the work", "checkout", "booking", "one-tap approval", "request_takeover",
		// The run this closes stopped at an Amazon page that had only painted its
		// header, and answered with "How to complete the purchase yourself".
		"place order", "press the final", "still rendering", "numbered list of what to click",
		"carry on yourself",
	} {
		if !strings.Contains(prompt, must) {
			t.Errorf("the browser block never says %q", must)
		}
	}
	// The chain has to be spelled out, because "complete the checkout" alone let a
	// model stop at the product page and describe the rest.
	for _, step := range []string{"add to cart", "open the cart", "go to checkout", "delivery address"} {
		if !strings.Contains(prompt, step) {
			t.Errorf("the purchase sequence is missing %q", step)
		}
	}
	// A sign-in sent to chat strands the run: nothing resumes it, so the card is
	// the only route that both pauses the agent and brings it back.
	if strings.Contains(prompt, "ask them to sign in in the browser window") {
		t.Error("the block tells the agent to ask in chat instead of raising the takeover card")
	}
}

func TestTakeoverFiresOnThePageVerdictAlone(t *testing.T) {
	// The bug this pins: a check box lives in a cross-origin frame, so no element
	// name and no page text ever carried the word, and the card never came up.
	// The page pass now names it from the iframe address in the parent's markup.
	tree := "browser · Sign in · 2 actionable · url=https://example.com/login\n[1] textbox Email\n[2] button Continue"
	out := Browser.Result{Challenge: []string{"iframe https://www.google.com/recaptcha/api2/anchor?ar=1"}}
	if !challengeSeen(out, tree) {
		t.Error("a page that names its own human check did not raise a takeover")
	}
	if captchaDetected(tree) {
		t.Error("a plain login form read as a human check — a false card is worse than none")
	}
	// The text fallback still stands for a page that prints the challenge itself.
	if !challengeSeen(Browser.Result{}, "please verify you are a human to continue") {
		t.Error("the text-only fallback stopped working")
	}
	if captchaDetected("Daily chess challenge: solve the puzzle below") {
		t.Error("the bare word challenge raised a takeover")
	}
}

func TestOneHumanCheckYieldsOneCard(t *testing.T) {
	s := &Service{}
	if !s.captchaArrived("default") {
		t.Fatal("the first check got no card")
	}
	if s.captchaArrived("default") {
		t.Error("the same page asked the user twice inside the cooldown")
	}
	if !s.captchaArrived("other agent") {
		t.Error("the cooldown leaked across agents")
	}
}

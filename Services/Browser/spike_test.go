package Browser

// This test drives a real Chrome. It is the only thing here that proves the
// design works rather than compiles, so it is written to fail loudly on the
// specific promises the feature rests on: shadow-DOM controls are visible, a
// framework input accepts the text, refs go stale instead of mis-clicking.
//
// Run it with:  ARGUS_BROWSER_IT=1 go test -run RealChrome ./Services/Browser/

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRealChromeEndToEnd(t *testing.T) {
	if os.Getenv("ARGUS_BROWSER_IT") == "" {
		t.Skip("set ARGUS_BROWSER_IT=1 to run against a real browser")
	}
	if findChrome() == "" {
		t.Skip("no Chromium-family browser on this machine")
	}
	t.Setenv(HeadlessEnv, "1")
	t.Setenv(ProfileDirEnv, filepath.Join(t.TempDir(), "profile"))

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	svc := &Service{}
	if err := svc.Start(filepath.Join(t.TempDir(), "captures")); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })

	fixture := filepath.Join(cwd(t), "testdata", "shadow.html")
	pageURL := "file://" + fixture

	// ---- observation ----
	opened := svc.Run(ctx, Call{Action: "open", URL: pageURL}, nil)
	if opened.Error != "" {
		t.Fatalf("open failed: %s", opened.Error)
	}
	tree := opened.Tree
	t.Logf("snapshot:\n%s", tree)

	mustSee(t, tree, "Accessibility fixture", "the h1 in the light DOM")
	mustSee(t, tree, "Save changes", "a button inside an open shadow root")
	mustSee(t, tree, "Keep editing", "a shadow button named only by aria-label")
	mustSeeLine(t, tree, `button "Dismiss this alert"`, "a div with role=button must read as a button")
	mustSee(t, tree, "Inside the frame", "a control in a same-origin iframe")
	mustNotSee(t, tree, "Never rendered", "a link inside display:none")
	mustSee(t, tree, "Continue to payment", "a link whose href points at checkout")

	// The name should come from Chrome's accessibility tree where the page gave
	// nothing better: the region select has only an aria-label.
	if !strings.Contains(tree, `combobox "Region"`) && !strings.Contains(tree, `"Region"`) {
		t.Errorf("the labelled select lost its name:\n%s", tree)
	}

	// ---- clicking a shadow-DOM control ----
	saveRef := refFor(t, tree, "Save changes")
	clicked := svc.Run(ctx, Call{Action: "click", Ref: saveRef}, nil)
	if clicked.Error != "" {
		t.Fatalf("clicking a shadow button: %s", clicked.Error)
	}
	if count := evalInt(ctx, t, svc, "window.__shadowClicked || 0"); count != 1 {
		t.Errorf("the shadow button's own listener ran %d times, want 1", count)
	}
	// The re-render inside the shadow root must show up in the next snapshot.
	if !strings.Contains(clicked.Tree, "Saved") {
		t.Errorf("after the click the button reads 'Saved'; snapshot was:\n%s", clicked.Tree)
	}

	// ---- typing into a controlled input ----
	searchRef := refFor(t, clicked.Tree, "Search the docs")
	filled := svc.Run(ctx, Call{Action: "fill", Ref: searchRef, Value: "modern fonts"}, nil)
	if filled.Error != "" {
		t.Fatalf("fill: %s", filled.Error)
	}
	if echo := evalString(ctx, t, svc, "document.getElementById('echo').textContent"); echo != "echo: modern fonts" {
		t.Errorf("the framework saw %q, want \"echo: modern fonts\" — the input event did not reach it", echo)
	}
	// The value the page reports must match, not just the DOM node's own field.
	if value := evalString(ctx, t, svc, "document.getElementById('search').value"); value != "modern fonts" {
		t.Errorf("input.value = %q, want the typed text", value)
	}

	// ---- keyboard, select, scroll, read ----
	press := svc.Run(ctx, Call{Action: "press", Key: "Enter"}, nil)
	if press.Error != "" {
		t.Errorf("press enter: %s", press.Error)
	}
	regionRef := refFor(t, press.Tree, "Region")
	selectRes := svc.Run(ctx, Call{Action: "select", Ref: regionRef, Value: "United States"}, nil)
	if selectRes.Error != "" {
		t.Errorf("select: %s", selectRes.Error)
	}
	if value := evalString(ctx, t, svc, "document.getElementById('region').value"); value != "us" {
		t.Errorf("select value = %q, want \"us\" (matched by its visible label)", value)
	}

	scrolled := svc.Run(ctx, Call{Action: "scroll", Value: "down"}, nil)
	if scrolled.Error != "" {
		t.Errorf("scroll: %s", scrolled.Error)
	}

	text := svc.Run(ctx, Call{Action: "text"}, nil)
	if text.Error != "" || !strings.Contains(text.Body, "custom element") {
		t.Errorf("text did not return the prose: %q", text.Body)
	}

	// ---- refs go stale instead of mis-clicking ----
	// The page mutates underneath a ref the agent already read: the number must
	// stop meaning "that button", not silently mean "whatever is number 3 now".
	if res := svc.Run(ctx, Call{Action: "js", Value: "document.getElementById('search').remove()"},
		func(string, string) (bool, string) { return true, "" }); res.Error != "" {
		t.Fatalf("removing the input: %s", res.Error)
	}
	stale := svc.Run(ctx, Call{Action: "click", Ref: searchRef}, nil)
	if stale.Error == "" {
		t.Fatal("clicking a ref whose element was removed reported success")
	}
	if !strings.Contains(strings.ToLower(stale.Error), "no longer") &&
		!strings.Contains(strings.ToLower(stale.Error), "stale") {
		t.Errorf("stale-ref message should say the ref is gone, got %q", stale.Error)
	}
	if stale.Tree == "" {
		t.Error("a stale ref must hand back the current page so the agent can re-aim")
	}

	// ---- a second page, filtered reads and the approval path ----
	shop := filepath.Join(cwd(t), "testdata", "simple.html")
	reloaded := svc.Run(ctx, Call{Action: "open", URL: "file://" + shop}, nil)
	if reloaded.Error != "" {
		t.Fatalf("open second page: %s", reloaded.Error)
	}
	found := svc.Run(ctx, Call{Action: "find", Text: "sign in"}, nil)
	if found.Error != "" {
		t.Errorf("find: %s", found.Error)
	}
	if !strings.Contains(found.Tree, "Sign in") {
		t.Errorf("find should narrow to the matching row:\n%s", found.Tree)
	}
	// The full page is still available; find must not have destroyed it.
	full := svc.Run(ctx, Call{Action: "read"}, nil)
	if !strings.Contains(full.Tree, "Add to basket") {
		t.Errorf("read after find lost elements:\n%s", full.Tree)
	}
	if len(full.Challenge) != 0 {
		t.Errorf("an ordinary shop page reported a human check: %v", full.Challenge)
	}

	asked := false
	gateSum := ""
	buyRef := refFor(t, full.Tree, "Buy now")
	declined := svc.Run(ctx, Call{Action: "click", Ref: buyRef}, func(_, sum string) (bool, string) {
		asked = true
		gateSum = sum
		return false, "the user said no"
	})
	if !asked {
		t.Error("Buy now on a simple page should still be gated by its label")
	}
	// What the user reads on the card must name the site and quote the control.
	if !strings.Contains(gateSum, "Buy now") {
		t.Errorf("the approval summary should quote the button, got %q", gateSum)
	}
	if !strings.Contains(gateSum, "simple.html") {
		t.Errorf("the approval summary should say where, got %q", gateSum)
	}
	if declined.Error == "" {
		t.Fatal("a declined click reported success")
	}
	if !strings.Contains(strings.ToLower(declined.Error), "do not retry") {
		t.Errorf("a refusal must tell the agent not to route around it: %q", declined.Error)
	}
	if n := evalInt(ctx, t, svc, "window.__bought || 0"); n != 0 {
		t.Errorf("the declined click still fired: %d", n)
	}

	// An ungated click on the same page goes through with no prompt.
	asked = false
	addRef := refFor(t, full.Tree, "Add to basket")
	if res := svc.Run(ctx, Call{Action: "click", Ref: addRef}, func(string, string) (bool, string) {
		asked = true
		return true, ""
	}); res.Error != "" {
		t.Errorf("add to basket: %s", res.Error)
	}
	if asked {
		t.Error("Add to basket should not need approval")
	}
	if n := evalInt(ctx, t, svc, "window.__added || 0"); n != 1 {
		t.Errorf("the free click ran %d times, want 1", n)
	}

	// ---- tabs ----
	secondTab := svc.Run(ctx, Call{Action: "tab_new", URL: pageURL}, nil)
	if secondTab.Error != "" {
		t.Fatalf("tab_new: %s", secondTab.Error)
	}
	list := svc.Run(ctx, Call{Action: "tabs"}, nil)
	if list.Error != "" || strings.Count(list.Body, "[") < 2 {
		t.Errorf("tabs should list both: %q", list.Body)
	}
	closed := svc.Run(ctx, Call{Action: "tab_close", Target: "2"}, nil)
	if closed.Error != "" {
		t.Errorf("tab_close: %s", closed.Error)
	}

	// ---- screenshot writes a file we can see ----
	shot := svc.Run(ctx, Call{Action: "screenshot"}, nil)
	if shot.Error != "" {
		t.Errorf("screenshot: %s", shot.Error)
	}

	// ---- a human check is named even though its widget cannot be read ----
	// This is the whole reason the takeover card exists, and the accessibility
	// tree is blind to it: the check box lives in a cross-origin frame, so the
	// only trace is the scaffolding the embedding page carries itself.
	walled := filepath.Join(cwd(t), "testdata", "challenge.html")
	check := svc.Run(ctx, Call{Action: "open", URL: "file://" + walled}, nil)
	if check.Error != "" {
		t.Fatalf("open challenge page: %s", check.Error)
	}
	if len(check.Challenge) == 0 {
		t.Fatalf("a page with a reCAPTCHA script and a Turnstile widget reported no human check:\n%s", check.Tree)
	}
	if !strings.Contains(check.Tree, "HUMAN CHECK") {
		t.Errorf("the verdict never reached the text the agent reads:\n%s", check.Tree)
	}
	named := strings.ToLower(strings.Join(check.Challenge, " | "))
	for _, want := range []string{"recaptcha", "turnstile"} {
		if !strings.Contains(named, want) {
			t.Errorf("the evidence does not name %s: %v", want, check.Challenge)
		}
	}
	// The decoys: prose that merely contains the word challenge, and a link that
	// says the flow is captcha-free. A false card costs the user an interruption
	// they did not need, which is worse than no card at all.
	for _, no := range []string{"Take today", "CAPTCHA-free", "help.html"} {
		if strings.Contains(strings.Join(check.Challenge, " | "), no) {
			t.Errorf("a decoy was reported as a human check: %q in %v", no, check.Challenge)
		}
	}

	// ---- a page shell is not reported as a finished page ----
	// The Amazon run this replaces: a product page's first paint is its header,
	// the price and the buy box come off a request a second later, and the click
	// handed back 15 controls that were all navigation. The agent concluded the
	// item could not be bought and wrote the user a how-to.
	late := filepath.Join(cwd(t), "testdata", "late.html")
	loaded := svc.Run(ctx, Call{Action: "open", URL: "file://" + late}, nil)
	if loaded.Error != "" {
		t.Fatalf("open late-rendering page: %s", loaded.Error)
	}
	if !strings.Contains(loaded.Tree, "Add to cart") {
		t.Errorf("the buy box injected 1.2s after first paint never reached the snapshot:\n%s", loaded.Tree)
	}
	if strings.Contains(loaded.Tree, "STILL RENDERING") {
		t.Errorf("a finished page was still called a shell:\n%s", loaded.Tree)
	}
	// The control the agent needs must be usable straight out of that tree — the
	// point of waiting is that no second read is required to buy anything.
	lateRef := refFor(t, loaded.Tree, "Add to cart")
	yes := func(string, string) (bool, string) { return true, "" }
	if res := svc.Run(ctx, Call{Action: "click", Ref: lateRef}, yes); res.Error != "" {
		t.Errorf("clicking the late buy box: %s", res.Error)
	}
	if n := evalInt(ctx, t, svc, "window.__added || 0"); n != 1 {
		t.Errorf("the late button ran %d times, want 1", n)
	}

	// And a thin page that has genuinely finished must not be called a shell —
	// otherwise every two-link form costs the run seconds and the warning stops
	// meaning anything.
	shell := filepath.Join(cwd(t), "testdata", "shell.html")
	thin := svc.Run(ctx, Call{Action: "open", URL: "file://" + shell}, nil)
	if thin.Error != "" {
		t.Fatalf("open shell page: %s", thin.Error)
	}
	if strings.Contains(thin.Tree, "STILL RENDERING") {
		t.Errorf("a quiet page was reported as mid-render:\n%s", thin.Tree)
	}

	// ---- reset clears the profile and the session ----
	if err := svc.setWindow(ctx, false); err != nil {
		t.Logf("hide: %v", err) // headless has no window; not a failure
	}
	if _, err := svc.reset(ctx); err != nil {
		t.Errorf("reset: %v", err)
	}
}

func cwd(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

var refLine = regexp.MustCompile(`\[(\d+)\]\s+([^\n]*)`)

// refFor finds the number a named element was given in a rendered snapshot. It
// matches the quoted label, so a container whose name happens to contain the
// words cannot be mistaken for the control the test means.
func refFor(t *testing.T, tree, want string) int {
	t.Helper()
	quoted := `"` + want + `"`
	for _, line := range strings.Split(tree, "\n") {
		m := refLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		body := strings.ToLower(m[2])
		want := strings.ToLower(want)
		if strings.Contains(body, strings.ToLower(quoted)) || strings.HasSuffix(body, want) {
			n, err := strconv.Atoi(m[1])
			if err != nil {
				continue
			}
			return n
		}
	}
	t.Fatalf("no %q in the snapshot:\n%s", want, tree)
	return 0
}

func mustSee(t *testing.T, tree, needle, what string) {
	t.Helper()
	if !strings.Contains(tree, needle) {
		t.Errorf("missing %s (%q) in:\n%s", what, needle, tree)
	}
}

// mustSeeLine checks one whole rendered row, so a right name on a wrong role is
// still a failure.
func mustSeeLine(t *testing.T, tree, row, what string) {
	t.Helper()
	for _, line := range strings.Split(tree, "\n") {
		if strings.Contains(strings.TrimSpace(line), row) {
			return
		}
	}
	t.Errorf("no row %q (%s) in:\n%s", row, what, tree)
}

func mustNotSee(t *testing.T, tree, needle, what string) {
	t.Helper()
	if strings.Contains(tree, needle) {
		t.Errorf("should not have listed %s (%q):\n%s", what, needle, tree)
	}
}

func evalString(ctx context.Context, t *testing.T, svc *Service, expr string) string {
	t.Helper()
	// The test plays the user: it always says yes, because these scripts are the
	// assertions, not the behaviour under test.
	yes := func(string, string) (bool, string) { return true, "" }
	res := svc.Run(ctx, Call{Action: "js", Value: fmt.Sprintf("(%s)", expr)}, yes)
	if res.Error != "" {
		t.Fatalf("js %s: %s", expr, res.Error)
	}
	return strings.TrimSpace(res.Body)
}

func evalInt(ctx context.Context, t *testing.T, svc *Service, expr string) int {
	t.Helper()
	raw := evalString(ctx, t, svc, expr)
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		t.Fatalf("js %s returned %q, want a number", expr, raw)
	}
	return n
}

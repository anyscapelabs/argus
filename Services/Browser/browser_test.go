package Browser

// These tests cover the parts of the browser feature that are decisions rather
// than plumbing: what a snapshot looks like to the model, when an action has to
// ask, and how a ref can go stale. None of them need Chrome.

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func element(ref, index int, role, name string) Element {
	return Element{
		Ref: ref, Index: index, Role: role, Name: name, Tag: "div",
		Interactive: role == "button" || role == "link" || role == "textbox",
		W:           100, H: 30, X: 20, Y: index * 40,
	}
}

func TestSnapshotDecodeMatchesThePageScript(t *testing.T) {
	// The exact shape assets/snapshot.js produces. If the two drift, this breaks
	// before a user ever sees an empty snapshot.
	raw := `{"url":"https://example.com","title":"Example Domain","viewportH":900,"viewportW":1280,
	  "docH":1200,"scrollY":0,"skipped":3,"truncated":false,
	  "visited":[[0,"body"],[1,"h1"],[2,"p"],[3,"a"]],
	  "frames":[],
	  "challenge":["iframe https://www.google.com/recaptcha/api2/anchor?ar=1"],
	  "elements":[{"ref":1,"index":1,"tag":"h1","role":"heading","name":"Example Domain","nameFrom":"contents",
	    "roleGuessed":false,"x":8,"y":20,"w":1264,"h":32,"interactive":false,"structural":true,
	    "belowFold":false,"frame":"","state":{"level":1}},
	    {"ref":2,"index":3,"tag":"a","role":"link","name":"More information...","nameFrom":"contents",
	    "roleGuessed":false,"x":8,"y":120,"w":160,"h":20,"interactive":true,"structural":false,
	    "belowFold":false,"frame":"","state":{"href":"https://www.iana.org/domains/example"}}]}`

	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var snap Snapshot
	if err := dec.Decode(&snap); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if snap.URL != "https://example.com" || len(snap.Elements) != 2 {
		t.Fatalf("decoded %+v", snap)
	}
	if snap.Elements[0].State.Level != 1 {
		t.Errorf("heading level = %d, want 1", snap.Elements[0].State.Level)
	}
	if got := snap.Elements[1].State.Href; !strings.Contains(got, "iana.org") {
		t.Errorf("href = %q", got)
	}
	if len(snap.Challenge) != 1 || !strings.Contains(snap.Challenge[0], "recaptcha") {
		t.Errorf("challenge = %v, want the reCAPTCHA frame the script names", snap.Challenge)
	}
	// The fixture is hand-written, so the script has to be checked on its own:
	// a Go field nobody emits is a takeover that never fires.
	if !strings.Contains(snapshotScript, "challenge: challengeEvidence()") {
		t.Error("assets/snapshot.js no longer collects human-check evidence")
	}
}

func TestHumanCheckSurvivesTheRead(t *testing.T) {
	// A check lives in a cross-origin frame, so nothing in the element list can
	// show it. The evidence is the only way the agent learns one is on screen, and
	// Chat raises the takeover card from the result's copy of it — so it has to
	// survive the render, the paging and the filter.
	snap := &Snapshot{
		URL: "https://example.com/signin", Title: "Sign in", Generation: 3,
		Elements:  []Element{element(1, 0, "button", "Continue"), element(2, 1, "textbox", "Email")},
		Challenge: []string{"iframe https://challenges.cloudflare.com/cdn-cgi/challenge-platform/h/b/turnstile", "h1 \"Verify you are human\""},
	}
	out := snap.Render()
	if !strings.Contains(out, "HUMAN CHECK") {
		t.Errorf("the verdict is missing from what the model reads:\n%s", out)
	}
	if !strings.Contains(out, "turnstile") {
		t.Error("the evidence vanished — the text regex in Chat keys on the vendor named here")
	}
	if !strings.Contains(out, "only the user can clear it") {
		t.Error("the render does not say whose job it is")
	}
	if got := len(snap.Window(0, 1).Challenge); got != 2 {
		t.Errorf("read(offset) dropped the check: %d evidence strings", got)
	}
	if got := len(snap.Filter("Continue").Challenge); got != 2 {
		t.Errorf("find dropped the check: %d evidence strings", got)
	}
}

func TestSliceSaysWhatItLeftOut(t *testing.T) {
	// The trap this closes is a real one: a page renders 32 elements, the agent
	// asks to read from 50, and the answer came back "0 actionable · nothing
	// actionable on this page". That reads as an empty page, so it stops looking
	// one scroll before the thing it came for.
	full := &Snapshot{URL: "https://example.com/s?k=keyboard", Title: "Results", Generation: 7}
	for i := 0; i < 32; i++ {
		full.Elements = append(full.Elements, element(i+1, i, "link", "Result"))
	}
	tail := full.Window(30, 80).Render()
	if !strings.Contains(tail, "2 of 32 actionable") {
		t.Errorf("a window did not name the page it was cut from:\n%s", tail)
	}
	past := full.Window(40, 80).Render()
	if !strings.Contains(past, "past the end") || !strings.Contains(past, "numbered 1-32") {
		t.Errorf("reading past the end did not say so:\n%s", past)
	}
	if strings.Contains(past, "nothing actionable on this page") {
		t.Error("an empty window claimed the page itself holds nothing")
	}
	// A page that genuinely is empty must keep its own message.
	blank := &Snapshot{URL: "https://example.com", Title: "Blank", Generation: 1}
	if got := blank.Render(); !strings.Contains(got, "nothing actionable on this page") {
		t.Errorf("an empty page lost its honest message:\n%s", got)
	}
	// find is a slice too: the page behind three matches is still 32 wide.
	if got := full.Filter("Result").Render(); !strings.Contains(got, "32 actionable") {
		t.Errorf("find hid the size of the page behind the matches:\n%s", got)
	}
}

func TestAReadCaughtMidRenderSaysSo(t *testing.T) {
	// The Amazon run this closes: a product page returned 15 controls — all header
	// nav — because the price, the buy box and the cart are injected by a request
	// that had not landed yet. The agent read "15 actionable", found no way to buy,
	// and wrote the user a numbered list of steps to do it themselves.
	growing := &Snapshot{
		URL: "https://www.amazon.com/dp/B0C2HCCTF2", Title: "RedThunder K84",
		ViewportH: 900, DocH: 8877, Generation: 2, Growing: true,
	}
	for i := 0; i < 15; i++ {
		growing.Elements = append(growing.Elements, element(i+1, i, "link", "Nav"))
	}
	got := growing.Render()
	if !strings.Contains(got, "STILL RENDERING") {
		t.Errorf("a tree that was still growing did not admit it:\n%s", got)
	}
	if !strings.Contains(got, "wait") || !strings.Contains(got, "read again") {
		t.Error("the verdict names the problem without naming the recovery")
	}

	// A quiet tree is not called unfinished, however thin. A news article two
	// screens long with three links on it is a finished page, and flagging it would
	// turn every read into a suggestion to wait.
	quiet := *growing
	quiet.Growing = false
	if got := quiet.Render(); strings.Contains(got, "STILL RENDERING") {
		t.Errorf("a settled page was told to wait:\n%s", got)
	}

	// A slice of a page caught mid-render is still mid-render; a slice of a settled
	// page stays settled.
	if got := growing.Window(0, 5).Render(); !strings.Contains(got, "STILL RENDERING") {
		t.Errorf("read(offset) lost the verdict:\n%s", got)
	}
	if got := growing.Filter("Nav").Render(); !strings.Contains(got, "STILL RENDERING") {
		t.Errorf("find lost the verdict:\n%s", got)
	}
	if got := quiet.Window(0, 5).Render(); strings.Contains(got, "STILL RENDERING") {
		t.Errorf("a window of a finished page was called a shell:\n%s", got)
	}

	// A human check outranks it: that is the reason the page is thin.
	check := *growing
	check.Challenge = []string{"iframe https://www.google.com/recaptcha/api2/anchor"}
	if got := check.Render(); strings.Contains(got, "STILL RENDERING") {
		t.Errorf("a page behind a check was told to wait for its content:\n%s", got)
	}
}

func TestAShellIsOnlySuspectedWhenTheDocumentIsLong(t *testing.T) {
	// unfinished() decides how long a capture is held, never what is claimed, so it
	// may be as cautious as it likes — but it must not mistake a short page for a
	// slow one, or every read of a login form costs seconds.
	thin := func(n, viewportH, docH int) *Snapshot {
		s := &Snapshot{URL: "https://x.test", Title: "x", ViewportH: viewportH, DocH: docH}
		for i := 0; i < n; i++ {
			s.Elements = append(s.Elements, element(i+1, i, "link", "L"))
		}
		return s
	}
	if !thin(15, 900, 8877).unfinished() {
		t.Error("the Amazon tree — 15 controls on a 8877 px page — was believed on the first read")
	}
	if thin(15, 900, 1200).unfinished() {
		t.Error("a page barely taller than the window is a page, not a shell")
	}
	if thin(60, 900, 8877).unfinished() {
		t.Error("a full tree on a long page is not a shell")
	}
	if thin(0, 900, 8877).unfinished() {
		t.Error("an empty page is reported by its own message, not as a shell")
	}
	sliced := thin(15, 900, 8877).Window(0, 5)
	if sliced.unfinished() {
		t.Error("a read window is thin by construction and must not be held as a shell")
	}
}

func TestRenderIsCompactAndNumbered(t *testing.T) {
	snap := &Snapshot{
		URL: "https://news.ycombinator.com", Title: "Hacker News",
		ViewportH: 900, DocH: 3000, ScrollY: 0, Generation: 4,
		Elements: []Element{
			element(1, 0, "link", "Fresh"),
			element(2, 1, "heading", "Top Stories"),
			func() Element {
				e := element(3, 2, "link", "A story that goes on for quite a long time, past what is useful in a listing")
				e.BelowFold = true
				return e
			}(),
		},
	}
	out := snap.Render()

	if !strings.HasPrefix(out, "browser · Hacker News · 3 actionable · url=https://news.ycombinator.com") {
		t.Errorf("header wrong:\n%s", out)
	}
	for _, want := range []string{"[1] link \"Fresh\"", "[2] heading \"Top Stories\"", "→"} {
		if want == "→" {
			continue
		}
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "1 of 3 are below the fold") {
		t.Errorf("no below-fold notice in:\n%s", out)
	}
	if strings.Contains(out, "ref#") {
		t.Errorf("generation leaked into the header; refs are implied by ordering:\n%s", out)
	}
}

func TestRenderTrimsAnOverlongName(t *testing.T) {
	long := strings.Repeat("word ", 60)
	snap := &Snapshot{URL: "https://x.com", Title: "x", Elements: []Element{element(1, 0, "link", long)}}
	out := snap.Render()
	if strings.Contains(out, long) {
		t.Fatalf("an untruncated name reached the model:\n%s", out)
	}
	if strings.Count(out, "\n") > 6 {
		t.Errorf("snapshot grew past a sane size:\n%s", out)
	}
}

func TestRenderHardCapsTheTree(t *testing.T) {
	elements := make([]Element, 0, 400)
	for i := 0; i < 400; i++ {
		elements = append(elements, element(i+1, i, "link", strings.Repeat("very long label ", 12)))
	}
	snap := &Snapshot{URL: "https://big.example", Title: "big", Elements: elements}
	out := snap.Render()
	if len(out) > maxRenderChars+len("… [snapshot cut at the size limit — use find(text=…) to zoom in]\n") {
		t.Errorf("rendered %d chars, past the budget", len(out))
	}
	if !strings.Contains(out, "snapshot cut at the size limit") {
		t.Errorf("a cut snapshot must say so:\n%s", out[len(out)-200:])
	}
}

func TestPruneDropsNoiseAndRenumbers(t *testing.T) {
	hidden := element(1, 0, "generic", "")
	hidden.Name = ""
	hidden.Role = "generic"
	clickable := element(2, 1, "button", "Save")
	guess := element(3, 2, "link", "Home")
	none := element(4, 3, "generic", "wrapper")
	setComputed(&none, "none", "wrapper")
	snap := &Snapshot{Elements: []Element{hidden, clickable, guess, none}}

	prune(snap)

	// The unnamed generic node and the Chrome-ignored wrapper are gone; the two
	// controls remain and are numbered from 1.
	if len(snap.Elements) != 2 {
		t.Fatalf("kept %+v, want the two controls", snap.Elements)
	}
	if snap.Elements[0].Ref != 1 || snap.Elements[1].Ref != 2 {
		t.Errorf("refs = %d,%d, want contiguous 1,2", snap.Elements[0].Ref, snap.Elements[1].Ref)
	}
	if snap.Elements[0].Name != "Save" {
		t.Errorf("first kept = %q, want Save (document order)", snap.Elements[0].Name)
	}
}

func setComputed(e *Element, role, name string) {
	e.Computed = true
	e.ComputedRole = role
	e.ComputedName = name
	ignored := role == "none"
	e.Unignored = &ignored
}

func TestComputedNamingWinsWhereThePageGuessed(t *testing.T) {
	e := element(1, 0, "button", "")
	e.Computed = true
	e.ComputedRole = "button"
	e.ComputedName = "Dismiss this alert"
	snap := &Snapshot{URL: "https://a", Elements: []Element{e}}
	out := snap.Render()
	if !strings.Contains(out, `"Dismiss this alert"`) {
		t.Errorf("Chrome's name should replace the empty guess:\n%s", out)
	}
	if strings.Contains(out, "unnamed") {
		t.Errorf("still reported as unnamed:\n%s", out)
	}
}

func TestUnnamedControlIsStillListed(t *testing.T) {
	e := element(1, 0, "button", "")
	snap := &Snapshot{URL: "https://a", Elements: []Element{e}}
	out := snap.Render()
	if !strings.Contains(out, "unnamed") {
		t.Errorf("an unnamed button must appear so the agent can click it:\n%s", out)
	}
}

func TestFilterAndWindow(t *testing.T) {
	elements := make([]Element, 0, 200)
	for i := 0; i < 200; i++ {
		e := element(i+1, i, "link", fmt.Sprintf("item %d", i))
		if i == 150 {
			e.Name = "Sign out"
			e.State.Href = "https://x.example/logout"
		}
		elements = append(elements, e)
	}
	snap := &Snapshot{URL: "https://x.example", Elements: elements, Truncated: true}

	filtered := snap.Filter("sign", "out")
	if len(filtered.Elements) != 1 || filtered.Elements[0].Name != "Sign out" {
		t.Fatalf("filter found %+v", filtered.Elements)
	}
	// Filtering must not renumber: the refs have to stay valid for the page.
	if filtered.Elements[0].Ref != 151 {
		t.Errorf("filtered ref = %d, want the original 151", filtered.Elements[0].Ref)
	}

	page2 := snap.Window(100, 80)
	if len(page2.Elements) != 80 || page2.Elements[0].Ref != 101 {
		t.Errorf("window = %d elements starting at ref %d, want 80 from 101",
			len(page2.Elements), firstRef(page2))
	}
	if !page2.Truncated {
		t.Error("a window that is not the whole page must say there is more")
	}
	tail := snap.Window(190, 80)
	if len(tail.Elements) != 10 {
		t.Errorf("tail window = %d elements, want the remaining 10", len(tail.Elements))
	}
}

func firstRef(s *Snapshot) int {
	if len(s.Elements) == 0 {
		return 0
	}
	return s.Elements[0].Ref
}

func TestNeedsApprovalFreeVerbs(t *testing.T) {
	buyer := element(1, 0, "button", "Buy now")
	for _, action := range []string{"open", "read", "find", "text", "scroll", "back", "wait", "tabs", "screenshot", "reset", "hide", "show"} {
		if required, why := NeedsApproval(action, "amazon.com", &buyer); required {
			t.Errorf("%s should be free, got %q", action, why)
		}
	}
}

func TestNeedsApprovalEverydayClicksAreFree(t *testing.T) {
	cases := []struct {
		host string
		name string
	}{
		{"news.ycombinator.com", "Fresh"},
		{"github.com", "Filter"},
		{"en.wikipedia.org", "Contents"},
		{"docs.go.dev", "net/http"},
		{"stackoverflow.com", "Ask Question"},
	}
	for _, c := range cases {
		e := element(1, 0, "link", c.name)
		if required, why := NeedsApproval("click", c.host, &e); required {
			t.Errorf("clicking %q on %s asked for approval: %q", c.name, c.host, why)
		}
	}
}

func TestNeedsApprovalRiskyLabels(t *testing.T) {
	for _, name := range []string{"Place your order", "Buy now", "Send", "Delete account", "Publish", "Unsubscribe", "Confirm payment", "Accept and authorize"} {
		e := element(1, 0, "button", name)
		required, why := NeedsApproval("click", "some-shop.example", &e)
		if !required {
			t.Errorf("%q must ask", name)
			continue
		}
		if !strings.Contains(why, name) {
			t.Errorf("the card must quote what is being pressed, got %q", why)
		}
	}
}

func TestNeedsApprovalRiskyHosts(t *testing.T) {
	plain := element(1, 0, "button", "Continue")
	for _, host := range []string{"paypal.com", "checkout.stripe.com", "www.amazon.co.uk", "mail.google.com", "chase.com", "app.monzo.com"} {
		if required, _ := NeedsApproval("click", host, &plain); !required {
			t.Errorf("%s should be gated even for an ordinary button", host)
		}
	}
	for _, host := range []string{"example.com", "docs.python.org", "reddit.com"} {
		if required, _ := NeedsApproval("click", host, &plain); required {
			t.Errorf("%s should not gate a plain 'Continue' click", host)
		}
	}
}

func TestNeedsApprovalComposerTyping(t *testing.T) {
	comment := element(1, 0, "textbox", "Write a comment…")
	comment.Tag = "div"
	if required, why := NeedsApproval("fill", "blog.example", &comment); required {
		t.Errorf("a blog draft is not a send — the Post click gates it: %q", why)
	}

	mailBody := element(2, 1, "textbox", "message body")
	if required, why := NeedsApproval("fill", "mail.google.com", &mailBody); !required {
		t.Errorf("typing the body of an email on a mail host must ask, got %q", why)
	}

	draft := element(3, 2, "textbox", "Write a message")
	if required, why := NeedsApproval("fill", "messenger.example", &draft); required {
		t.Errorf("a chat draft on a non-gated host should stay free until send: %q", why)
	}

	notes := element(4, 3, "textbox", "Notes")
	notes.Tag = "textarea"
	if required, why := NeedsApproval("fill", "docs.example", &notes); required {
		t.Errorf("typing into a notes field should be free: %q", why)
	}
}

func TestNeedsApprovalUnknownVerbDefaultsToAsking(t *testing.T) {
	e := element(1, 0, "button", "Hello")
	if required, _ := NeedsApproval("launch_missiles", "example.com", &e); !required {
		t.Error("an unrecognised verb must not inherit a free pass")
	}
}

// A script is the one verb that can do anything a click can without a control to
// read, so it has to be judged on where it runs and what it names.
func TestScriptRisk(t *testing.T) {
	free := []struct{ host, script string }{
		{"news.ycombinator.com", "document.title"},
		{"blog.example", "[...document.querySelectorAll('article h2')].map(h=>h.textContent)"},
		{"docs.example", "window.scrollTo(0, 500)"},
	}
	for _, c := range free {
		if required, why := ScriptRisk(c.host, c.script); required {
			t.Errorf("reading on %s should be free: %q", c.host, why)
		}
	}

	gated := []struct{ host, script string }{
		{"mail.google.com", "document.querySelector('.inbox').textContent"},
		{"paypal.com", "location.href"},
		{"shop.example", "fetch('/checkout', {method:'POST'})"},
		{"blog.example", "document.querySelector('button').click() // send the post"},
	}
	for _, c := range gated {
		if required, why := ScriptRisk(c.host, c.script); !required {
			t.Errorf("a script on %s must ask", c.host)
		} else if !strings.Contains(why, c.host) {
			t.Errorf("the summary should name the site, got %q", why)
		}
	}
}

func TestJsIsNotInTheFreeList(t *testing.T) {
	e := element(1, 0, "button", "Hello")
	if required, _ := NeedsApproval("js", "example.com", &e); !required {
		t.Error("js must not read as a free verb; ScriptRisk decides it")
	}
}

func TestLooksLikeHost(t *testing.T) {
	yes := []string{"example.com", "https://news.ycombinator.com", "localhost", "127.0.0.1:8080", "books.example.co.uk/search?q=1"}
	no := []string{"cheap flights to lisbon", "cats", "not a url at all", "example", "example.c", "http://", "你好.com"}
	for _, s := range yes {
		if !looksLikeHost(s) {
			t.Errorf("%q should look like a host", s)
		}
	}
	for _, s := range no {
		if looksLikeHost(s) {
			t.Errorf("%q should not look like a host", s)
		}
	}
}

func TestSplitCombo(t *testing.T) {
	cases := []struct {
		combo     string
		wantMods  int
		wantKey   string
		wantError bool
	}{
		{"enter", 0, "enter", false},
		{"ctrl+a", 2, "a", false},
		{"Shift+Tab", 16, "tab", false},
		{"ctrl+shift+k", 18, "k", false},
		{"meta+enter", 8, "enter", false},
		{"alt+", 1, "", true},
		{"hyper+x", 0, "", true},
	}
	for _, c := range cases {
		mods, key, err := splitCombo(c.combo)
		if c.wantError {
			if err == nil {
				t.Errorf("splitCombo(%q) should have failed", c.combo)
			}
			continue
		}
		if err != nil {
			t.Errorf("splitCombo(%q): %v", c.combo, err)
			continue
		}
		if mods != c.wantMods || key != c.wantKey {
			t.Errorf("splitCombo(%q) = %d,%q want %d,%q", c.combo, mods, key, c.wantMods, c.wantKey)
		}
	}
}

func TestParseTabNumber(t *testing.T) {
	for _, in := range []string{"2", "tab 2", " 2 ", "Tab2"} {
		n, err := parseTabNumber(in)
		if err != nil || n != 2 {
			t.Errorf("parseTabNumber(%q) = %d, %v", in, n, err)
		}
	}
	for _, in := range []string{"", "current", "zero", "-1", "0"} {
		if _, err := parseTabNumber(in); err == nil {
			t.Errorf("parseTabNumber(%q) should refuse", in)
		}
	}
}

func TestHostOf(t *testing.T) {
	cases := map[string]string{
		"https://www.github.com/login":           "github.com",
		"https://news.ycombinator.com/item?id=1": "news.ycombinator.com",
		"":                                       "",
		"about:blank":                            "a blank page",
		"file:///home/me/notes/simple.html":      "simple.html",
		"file:///tmp/":                           "tmp",
		"data:text/html,<h1>hi</h1>":             "an inline page",
	}
	for in, want := range cases {
		if got := hostOf(in); got != want {
			t.Errorf("hostOf(%q) = %q, want %q", in, got, want)
		}
	}
}

// A permission card must name the place in words a user can act on, so a local
// file cannot arrive as a slashed-off path.
func TestHostOfIsShortEnoughForACard(t *testing.T) {
	long := "file:///home/user/projects/a-very-deep-directory-name/another-one/index.html"
	if got := hostOf(long); len(got) > 40 || !strings.Contains(got, "index.html") {
		t.Errorf("hostOf = %q, want the file name and a short label", got)
	}
}

func TestResultRenderPutsTheErrorFirst(t *testing.T) {
	res := Result{Error: "browser: [7] is no longer where it was", Tree: "browser · page\n  [1] link \"Home\""}
	out := res.Render()
	if !strings.HasPrefix(out, "browser: [7] is no longer where it was") {
		t.Errorf("error must lead:\n%s", out)
	}
	if !strings.Contains(out, "the page is now:") {
		t.Errorf("a stale-ref result must include the fresh page:\n%s", out)
	}

	ok := Result{OK: true, Note: "clicked [1 Save]", Tree: "browser · page"}
	if got := ok.Render(); !strings.HasPrefix(got, "clicked [1 Save]") {
		t.Errorf("note must lead a success:\n%s", got)
	}
}

func TestWantsSnapshotDefaultsToYes(t *testing.T) {
	if !(Call{}).wantsSnapshot() {
		t.Error("an agent that says nothing should still see the page")
	}
	off := false
	on := true
	if (Call{Observe: &off}).wantsSnapshot() {
		t.Error("observe:false must be honoured")
	}
	if !(Call{Observe: &on}).wantsSnapshot() {
		t.Error("observe:true must be honoured")
	}
}

func TestChromeProbeFindsThisMachine(t *testing.T) {
	// Not a browser launch: the discovery list must resolve to something on a
	// developer machine, or every user gets "browser unavailable".
	if ok, why := Available(); !ok {
		t.Skipf("no Chromium-family browser installed: %s", why)
	}
	if findChrome() == "" {
		t.Fatal("Available() said yes but findChrome() found nothing")
	}
}

func TestSignatureScriptIsSelfContained(t *testing.T) {
	// It is pasted into the page; a stray Go interpolation or an unbalanced brace
	// would only show up at runtime against a real browser.
	open := strings.Count(signatureScript, "(")
	closed := strings.Count(signatureScript, ")")
	if open != closed {
		t.Errorf("parenthesis imbalance in signatureScript: %d open, %d closed", open, closed)
	}
	if !strings.HasPrefix(signatureScript, "(function") {
		t.Error("the signature must be an IIFE or it returns nothing")
	}
}

func TestTheSettleDecision(t *testing.T) {
	// captureStable walks a ladder of reads and has to decide, from the element
	// count alone, whether the page is finished. These are the four answers it can
	// give, and the third is the one that cost an Amazon checkout: a page that
	// paints its header first looks quiet, and an agent told "15 actionable" with
	// no hint concludes the product has no buy button.
	full := &Snapshot{ViewportH: 900, DocH: 3000}
	for i := 0; i < 60; i++ {
		full.Elements = append(full.Elements, element(i+1, i, "link", "L"))
	}
	if !settledRead(60, 60, full, 1) {
		t.Error("a full, unchanged tree is a finished page on the second read")
	}
	if settledRead(30, 60, full, 1) {
		t.Error("a tree that doubled since the last read is not settled")
	}
	if settledRead(0, 0, &Snapshot{ViewportH: 900, DocH: 3000}, 4) {
		t.Error("an empty tree is never believed; it walks the ladder and says so itself")
	}

	shell := &Snapshot{ViewportH: 900, DocH: 8877}
	for i := 0; i < 15; i++ {
		shell.Elements = append(shell.Elements, element(i+1, i, "link", "Nav"))
	}
	if settledRead(15, 15, shell, 1) {
		t.Error("a long document with only a header on it was believed at 300ms")
	}
	if !settledRead(15, 15, shell, thinHoldRung) {
		t.Error("a shell that never filled in must be reported once the hold is over")
	}

	// A short document with two links is a small page, not a slow one: holding it
	// would make every login form cost seconds and teach the agent to ignore the
	// warning when it is real.
	small := &Snapshot{ViewportH: 900, DocH: 1200}
	small.Elements = []Element{element(1, 0, "textbox", "Email"), element(2, 1, "button", "Sign in")}
	if !settledRead(2, 2, small, 1) {
		t.Error("a finished page was held as if it were still rendering")
	}
}

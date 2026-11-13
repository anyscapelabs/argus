package Artifacts

import (
	"strings"
	"testing"
)

// findings returns "severity: message" for every diagnostic, so a failure shows
// what the validator actually said instead of a count.
func findings(r Result) []string {
	out := make([]string, 0, len(r.Diagnostics))
	for _, d := range r.Diagnostics {
		out = append(out, d.Severity+": "+d.Message)
	}
	return out
}

func has(t *testing.T, r Result, want string) {
	t.Helper()
	for _, f := range findings(r) {
		if strings.Contains(f, want) {
			return
		}
	}
	t.Fatalf("no finding containing %q; got %v", want, findings(r))
}

func TestAcceptsAPlainDocument(t *testing.T) {
	src := strings.Join([]string{
		"---", "title: November report", "author: Argus", "paper: a4", "---",
		"# Summary", "", "The quarter closed **up** 4%.", "",
		"| region | delta |", "| --- | --- |", "| EMEA | 4% |", "| APAC | -1% |", "",
		"- first", "- [x] done thing", "", "```go", "fmt.Println(\"hi\")", "```", "",
	}, "\n")
	r := Validate(src)
	if r.HasErrors() {
		t.Fatalf("clean document rejected: %v", findings(r))
	}
	if r.Title != "November report" {
		t.Errorf("title = %q, want %q", r.Title, "November report")
	}
	if strings.HasPrefix(r.Body, "---") {
		t.Errorf("front matter was not stripped: %q", r.Body[:20])
	}
	if !strings.HasPrefix(r.Body, "# Summary") {
		t.Errorf("body does not start at the first content line: %q", r.Body[:24])
	}
}

func TestRejectsRawHTMLWithTheWritersLineNumber(t *testing.T) {
	src := strings.Join([]string{
		"---", "title: Notes", "---", "", "Intro paragraph.", "",
		"<div class=\"box\">a callout</div>", "", "More text.",
	}, "\n")
	r := Validate(src)
	if !r.HasErrors() {
		t.Fatalf("raw HTML accepted: %v", findings(r))
	}
	has(t, r, "raw HTML is not supported")
	for _, d := range r.Diagnostics {
		if strings.Contains(d.Message, "raw HTML") {
			// Line 1 is the opening ---, so the div is line 7 of what the model
			// sent; a body-relative number would point at the wrong place.
			if d.Line != 7 {
				t.Errorf("raw HTML reported on line %d, want 7 (front matter counted)", d.Line)
			}
		}
	}
}

func TestRejectsInlineHTML(t *testing.T) {
	r := Validate("A line with a<br>break inside it.\n")
	if !r.HasErrors() {
		t.Fatalf("inline HTML accepted")
	}
	has(t, r, "inline HTML")
}

func TestRejectsDeepHeading(t *testing.T) {
	r := Validate("# one\n\n## two\n\n### three\n\n#### four\n\n##### five\n")
	if !r.HasErrors() {
		t.Fatalf("level 5 heading accepted: %v", findings(r))
	}
	has(t, r, "heading level 5 is too deep")
	for _, d := range r.Diagnostics {
		if strings.Contains(d.Message, "too deep") && d.Line != 9 {
			t.Errorf("deep heading reported on line %d, want 9", d.Line)
		}
	}

	// Level 4 is the last styled one and must stay clean.
	if r := Validate("#### fine\n"); r.HasErrors() {
		t.Errorf("level 4 heading rejected: %v", findings(r))
	}
}

func TestRejectsFootnotes(t *testing.T) {
	r := Validate("A claim[^1] that needs a source.\n\n[^1]: somewhere.\n")
	if !r.HasErrors() {
		t.Fatalf("footnote accepted: %v", findings(r))
	}
	has(t, r, "footnotes are not supported")
}

func TestRejectsRaggedTable(t *testing.T) {
	r := Validate("| a | b |\n| --- | --- |\n| 1 |\n")
	if !r.HasErrors() {
		t.Fatalf("ragged table accepted: %v", findings(r))
	}
	has(t, r, "this row has 1 cells but the header declares 2")
}

func TestTableDividerMustMatchHeader(t *testing.T) {
	r := Validate("| a | b | c |\n| --- | --- |\n| 1 | 2 | 3 |\n")
	if !r.HasErrors() {
		t.Fatalf("mismatched divider accepted: %v", findings(r))
	}
	has(t, r, "declares 2 columns but the header has 3")
}

func TestPipesInProseAreNotTables(t *testing.T) {
	r := Validate("# Notes\n\nThe command `a | b` and the sentence with | in it.\n")
	if r.HasErrors() {
		t.Fatalf("prose rejected: %v", findings(r))
	}
}

func TestFencedContentIsNotLinted(t *testing.T) {
	r := Validate("# Example\n\n```html\n<div>x</div>\n[^1]\n```\n")
	if r.HasErrors() {
		t.Fatalf("code fence contents were linted: %v", findings(r))
	}
}

func TestUnknownFrontMatterKeyListsTheAllowedOnes(t *testing.T) {
	r := Validate("---\ntitle: X\nstylesheet: mine.css\n---\nBody.\n")
	if !r.HasErrors() {
		t.Fatalf("unknown key accepted: %v", findings(r))
	}
	has(t, r, `unknown front matter key "stylesheet"`)
	has(t, r, "allowed: author, date, language, orientation, paper, title")
}

func TestRejectsBadPaperSize(t *testing.T) {
	r := Validate("---\npaper: legal\n---\nBody.\n")
	if !r.HasErrors() {
		t.Fatalf("paper size accepted")
	}
	has(t, r, "use a4 or letter")
}

func TestEmptyAndFrontMatterOnly(t *testing.T) {
	if r := Validate("   \n"); !r.HasErrors() {
		t.Errorf("empty body accepted")
	} else {
		has(t, r, "body is empty")
	}
	r := Validate("---\ntitle: Nothing else\n---\n\n")
	if !r.HasErrors() {
		t.Fatalf("front matter without a body accepted")
	}
	has(t, r, "front matter but no body")
}

func TestMissingImageIsAnErrorRemoteIsAWarning(t *testing.T) {
	r := Validate("![chart](/tmp/argus-does-not-exist.png)\n")
	if !r.HasErrors() {
		t.Fatalf("missing image accepted")
	}
	has(t, r, "image file not found")

	r = Validate("# Logo\n\n![logo](https://example.com/logo.png)\n")
	if r.HasErrors() {
		t.Errorf("remote image should be a warning, not an error: %v", findings(r))
	}
	has(t, r, "not in .docx")
}

func TestRelativeImagePathIsRefused(t *testing.T) {
	r := Validate("![plot](plot.png)\n")
	if !r.HasErrors() {
		t.Fatalf("relative image accepted")
	}
	has(t, r, "absolute path or start it with ~/")
}

func TestNormalisationIsStable(t *testing.T) {
	r := Validate("# Title\r\n\r\ntext\r\n\r\n\r\n")
	if r.Body != "# Title\n\ntext\n" {
		t.Errorf("body after normalise = %q", r.Body)
	}
	if strings.Contains(r.Body, "\r") {
		t.Error("carriage returns survived")
	}
}

func TestDocumentMustStartWithHeadingAfterFrontMatter(t *testing.T) {
	if r := Validate("Prose first.\n\n# Later\n"); !r.HasErrors() {
		t.Fatal("headingless start was accepted")
	}
	r := Validate("---\ntitle: Report\n---\n# Summary\n\nBody.\n")
	if r.HasErrors() {
		t.Fatalf("front matter followed by heading was rejected: %v", findings(r))
	}
}

func TestDiagnosticsAreCapped(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 40; i++ {
		b.WriteString("a claim[^x] here\n\n")
	}
	r := Validate(b.String())
	if len(r.Diagnostics) > maxDiagnostics {
		t.Errorf("%d diagnostics, want at most %d", len(r.Diagnostics), maxDiagnostics)
	}
	if r.truncated == 0 {
		t.Error("the cap did not record that findings were dropped")
	}
	if !strings.Contains(r.Report(), "more finding") {
		t.Errorf("report does not mention the dropped findings: %q", r.Report())
	}
}

package Present

import (
	"strings"
	"testing"
)

const doc = `# November sales report

Revenue grew twelve percent over October, driven by the enterprise tier.

## Highlights

- Enterprise renewals closed at ninety four percent
- Churn fell to two point one percent

| Region | Revenue |
|--------|---------|
| EMEA   | 120k    |
| APAC   | 95k     |

` + "```sh\necho hello\n\nexport TOTAL=1\n```"

func TestNoDocsLeavesTextUntouched(t *testing.T) {
	in := "# Anything\n\nAt all."
	got, n := StripArtifactEcho(in, nil)
	if got != in || n != 0 {
		t.Fatalf("got %q,%d", got, n)
	}
}

func TestFullPasteIsRemoved(t *testing.T) {
	got, n := StripArtifactEcho(doc, []string{doc})
	if got != "" || n == 0 {
		t.Fatalf("expected empty, got %q (removed %d)", got, n)
	}
}

func TestPreambleSurvivesAndDocIsRemoved(t *testing.T) {
	in := "Here is the report you asked for.\n\n" + doc + "\n\nLet me know if you want changes."
	got, n := StripArtifactEcho(in, []string{doc})
	want := "Here is the report you asked for.\n\nLet me know if you want changes."
	if got != want {
		t.Fatalf("got %q want %q (removed %d)", got, want, n)
	}
}

func TestReformattedCopyStillMatches(t *testing.T) {
	in := "**November Sales Report**\n\nRevenue grew twelve percent over  October,\ndriven by the enterprise tier."
	got, _ := StripArtifactEcho(in, []string{doc})
	if strings.TrimSpace(got) != "" {
		t.Fatalf("re-wrapped copy should be removed, got %q", got)
	}
}

func TestFrontMatterCopyIsRemoved(t *testing.T) {
	in := "---\ntitle: November sales report\nauthor: Argus\n---\n\nSaved it."
	got, n := StripArtifactEcho(in, []string{doc})
	if got != "Saved it." || n == 0 {
		t.Fatalf("got %q,%d", got, n)
	}
}

func TestUnrelatedFrontMatterKept(t *testing.T) {
	in := "---\nfoo: bar\n---\n\nSaved it."
	got, _ := StripArtifactEcho(in, []string{doc})
	if got != in {
		t.Fatalf("got %q", got)
	}
}

func TestSummaryAboutTheDocIsKept(t *testing.T) {
	in := "I wrote a one-page sales report covering revenue, churn and a regional table. It is in the card above."
	got, n := StripArtifactEcho(in, []string{doc})
	if got != in || n != 0 {
		t.Fatalf("summary must survive, got %q,%d", got, n)
	}
}

func TestShortPlainSentenceKept(t *testing.T) {
	d := "Summary\n\nDone.\n\nA longer paragraph that is definitely part of the document body."
	in := "Done."
	got, _ := StripArtifactEcho(in, []string{d})
	if got != in {
		t.Fatalf("got %q", got)
	}
}

func TestShortHeadingIsRemoved(t *testing.T) {
	got, _ := StripArtifactEcho("## Highlights", []string{doc})
	if got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestFenceWithBlankLineIsOneParagraph(t *testing.T) {
	in := "Run this:\n\n```sh\necho hello\n\nexport TOTAL=1\n```"
	got, _ := StripArtifactEcho(in, []string{doc})
	if got != "Run this:" {
		t.Fatalf("got %q", got)
	}
}

func TestPartialOverlapKeepsNewMaterial(t *testing.T) {
	novel := "Separately, the Lagos office opened a new warehouse and hired fourteen people in the quarter."
	in := "## Highlights\n\n" + novel
	got, _ := StripArtifactEcho(in, []string{doc})
	if got != novel {
		t.Fatalf("got %q", got)
	}
}

func TestUntouchedReturnsSameBytes(t *testing.T) {
	in := "  Leading space\r\n\r\nand CRLF stay.  "
	got, n := StripArtifactEcho(in, []string{doc})
	if got != in || n != 0 {
		t.Fatalf("got %q,%d", got, n)
	}
}
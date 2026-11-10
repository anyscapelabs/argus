package Uploads

import (
	"strings"
	"testing"
)

func TestNormalizeMimeFallsBackToExtension(t *testing.T) {
	if got := normalizeMime("chart.png", ""); got != "image/png" {
		t.Fatalf("empty mime should sniff from extension, got %q", got)
	}
	if got := normalizeMime("notes.docx", "application/octet-stream"); got != "application/vnd.openxmlformats-officedocument.wordprocessingml.document" {
		t.Fatalf("octet-stream should sniff docx, got %q", got)
	}
	if got := normalizeMime("x.png", "image/jpeg"); got != "image/jpeg" {
		t.Fatalf("explicit mime wins, got %q", got)
	}
}

func TestValidateMimeRejectsUnsupported(t *testing.T) {
	if _, err := validateMime("image/png"); err != nil {
		t.Fatalf("png should be allowed: %v", err)
	}
	if _, err := validateMime("video/mp4"); err == nil {
		t.Fatal("video should be rejected")
	}
}

func TestExtractTextPlain(t *testing.T) {
	got, err := extractText("text/plain", []byte("  hello\nworld  "))
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello\nworld" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractDOCX(t *testing.T) {
	// A minimal Word package: one paragraph of text.
	docXML := `<?xml version="1.0"?><w:document xmlns:w="urn:word"><w:body><w:p><w:r><w:t>First paragraph.</w:t></w:r></w:p><w:p><w:r><w:t>Second line.</w:t></w:r></w:p></w:body></w:document>`
	zipData := docxZip(t, "word/document.xml", docXML)
	got, err := extractDOCX(zipData)
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if !strings.Contains(got, "First paragraph.") || !strings.Contains(got, "Second line.") {
		t.Fatalf("missing text, got %q", got)
	}
	if strings.Count(got, "\n") < 1 {
		t.Fatalf("paragraph break missing, got %q", got)
	}
}

func TestTruncateAddsNotice(t *testing.T) {
	s := strings.Repeat("a", 100)
	got := truncate(s, 10)
	if len(got) >= len(s) {
		t.Fatal("expected truncation")
	}
	if !strings.Contains(got, "truncated") {
		t.Fatalf("missing truncation notice: %q", got)
	}
}

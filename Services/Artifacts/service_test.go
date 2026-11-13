package Artifacts

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
)

func openTempDB(t *testing.T) *Storage.DB {
	t.Helper()
	db, err := Storage.Open(filepath.Join(t.TempDir(), "artifacts.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// write calls the store as the agent's tool does, with the one agent this app
// has today and no title argument — the tests are about the document.
func write(db *Storage.DB, artifactID, body string, appendBody bool) Shared.ArtifactWriteResult {
	return save(db, "default", artifactID, "", body, appendBody)
}

func TestSaveStoresAValidDocument(t *testing.T) {
	db := openTempDB(t)

	src := strings.Join([]string{
		"---", "title: November report", "author: Argus", "paper: letter", "---",
		"## Summary", "", "The quarter closed **up** 4%.", "",
		"| region | delta |", "| --- | --- |", "| EMEA | 4% |", "",
	}, "\n")
	// The title argument is there to be overridden: front matter wins.
	res := save(db, "default", "", "ignored by front matter", src, false)
	if res.Error != nil {
		t.Fatalf("save failed: %v", res.Error.Message)
	}
	if res.Rejected {
		t.Fatalf("clean document rejected: %s", res.Findings)
	}
	if res.Artifact.Title != "November report" {
		t.Errorf("title = %q, want the front matter title to win", res.Artifact.Title)
	}
	if res.Artifact.Format != Storage.ArtifactFormat {
		t.Errorf("format = %q, want %q", res.Artifact.Format, Storage.ArtifactFormat)
	}
	if res.Artifact.Version != 1 {
		t.Errorf("version = %d, want 1", res.Artifact.Version)
	}
	if res.Artifact.Meta["paper"] != "letter" {
		t.Errorf("meta = %v, want the front matter kept for the exporters", res.Artifact.Meta)
	}
	// A table divider legitimately reads "---", so the check is for the lines
	// themselves: the metadata block must be gone from the stored body.
	if strings.HasPrefix(res.Artifact.Body, "---") ||
		strings.Contains(res.Artifact.Body, "title: November report") ||
		strings.Contains(res.Artifact.Body, "paper: letter") {
		t.Errorf("body still carries front matter:\n%s", res.Artifact.Body)
	}
	// The stored row is what the panel reads, not just what save returned.
	row, found, err := db.GetArtifact(res.Artifact.ID)
	if err != nil || !found {
		t.Fatalf("stored row is unreadable: found=%v err=%v", found, err)
	}
	if row.Words != res.Artifact.Words || row.Bytes != res.Artifact.Bytes {
		t.Errorf("counts disagree with the row: %+v vs %+v", row, res.Artifact)
	}
	if row.Meta == "" || row.Meta == "{}" {
		t.Errorf("stored meta = %q, want the front matter persisted for export", row.Meta)
	}
}

func TestSaveRefusesAndStoresNothing(t *testing.T) {
	db := openTempDB(t)

	res := write(db, "", "Intro.\n\n<div class=\"box\">a callout</div>\n", false)
	if !res.Rejected {
		t.Fatalf("raw HTML was accepted: %+v", res.Artifact)
	}
	if res.Error != nil {
		t.Errorf("a refusal is not an error: %v", res.Error.Message)
	}
	if !strings.Contains(res.Findings, "raw HTML is not supported") {
		t.Errorf("findings = %q, want the rule the writer broke", res.Findings)
	}
	rows, err := db.ListArtifacts("default", 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("%d rows stored, want a refusal to write nothing", len(rows))
	}
}

func TestSaveAppendExtendsThenRevalidates(t *testing.T) {
	db := openTempDB(t)

	first := write(db, "", "# Report\n\nOpening section.\n", false)
	if first.Error != nil || first.Rejected {
		t.Fatalf("first save: %+v", first)
	}

	second := write(db, first.Artifact.ID, "## Closing\n\nFinal section.\n", true)
	if second.Error != nil || second.Rejected {
		t.Fatalf("append: %+v", second)
	}
	if !strings.Contains(second.Artifact.Body, "Opening section") || !strings.Contains(second.Artifact.Body, "Final section") {
		t.Errorf("appended body lost a part:\n%s", second.Artifact.Body)
	}
	if second.Artifact.Version != 2 {
		t.Errorf("version = %d, want the append recorded as a new version", second.Artifact.Version)
	}

	// A broken continuation cannot smuggle itself in: the whole document is
	// checked, and the previous good body survives untouched.
	third := write(db, first.Artifact.ID, "Another claim[^2].\n\n[^2]: somewhere.\n", true)
	if !third.Rejected || !strings.Contains(third.Findings, "footnotes are not supported") {
		t.Fatalf("append of a rejected construct: %+v", third)
	}
	row, _, err := db.GetArtifact(first.Artifact.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if row.Version != 2 || strings.Contains(row.Body, "[^2]") {
		t.Errorf("rejected append still changed the document: %+v", row)
	}
}

func TestSaveNeedsAKnownIDToRevise(t *testing.T) {
	db := openTempDB(t)

	res := write(db, "01MISSING", "Body.\n", false)
	if res.Error == nil {
		t.Fatal("an unknown artifact_id reported success")
	}
	if res.Rejected {
		t.Error("a bad id is an error, not a document that broke the rules")
	}
	// append without an id says the same thing: nothing was guessed.
	if res := write(db, "", "Body.\n", true); res.Error == nil {
		t.Error("append without an artifact_id was allowed to guess")
	}
}

func TestSaveFallsBackToTheDocumentsOwnTitle(t *testing.T) {
	db := openTempDB(t)

	if res := write(db, "", "# Q3 Board Pack\n\nText.\n", false); res.Artifact.Title != "Q3 Board Pack" {
		t.Errorf("title = %q, want the first heading", res.Artifact.Title)
	}
	res := write(db, "", "Just prose, no heading.\n", false)
	if !res.Rejected || res.Artifact.ID != "" {
		t.Errorf("headingless document was stored: %+v", res)
	}
	// A rewrite that names no title takes the document's own new heading.
	first := write(db, "", "# Original\n\nText.\n", false)
	kept := write(db, first.Artifact.ID, "# Renamed\n\nText.\n", false)
	if kept.Artifact.Title != "Renamed" {
		t.Errorf("title = %q, want the new heading to take over", kept.Artifact.Title)
	}
}

func TestSaveWarnsButStillStores(t *testing.T) {
	db := openTempDB(t)

	res := write(db, "", "# Brand\n\nSee ![logo](https://example.com/logo.png) for the brand.\n", false)
	if res.Error != nil || res.Rejected {
		t.Fatalf("a warning must not block a write: %+v", res)
	}
	if !strings.Contains(res.Findings, "not in .docx") {
		t.Errorf("findings = %q, want the remote-image warning the writer should hear", res.Findings)
	}
}

func TestExportMarkdownRestoresTheFrontMatter(t *testing.T) {
	a := Storage.Artifact{
		Title: "November report",
		Meta:  `{"author":"Argus","paper":"letter","title":"November report"}`,
		Body:  "## Summary\n\nThe quarter closed up 4 percent.\n",
	}
	out, ext, err := Export(a, FormatMarkdown)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if ext != "md" {
		t.Errorf("extension = %q, want md", ext)
	}
	got := string(out)
	if !strings.HasPrefix(got, "---\ntitle: November report\n") {
		t.Errorf("front matter not restored in order:\n%s", got)
	}
	if !strings.Contains(got, "paper: letter") {
		t.Errorf("paper size lost from the export:\n%s", got)
	}
	if strings.Contains(got, "# November report") {
		t.Errorf("a title heading was added beside the front matter:\n%s", got)
	}
	// A document with no metadata still needs a leading title.
	bare := Storage.Artifact{Title: "Notes", Body: "Some prose.\n"}
	out, _, err = Export(bare, "md")
	if err != nil {
		t.Fatalf("bare export: %v", err)
	}
	if string(out) != "# Notes\n\nSome prose.\n" {
		t.Errorf("bare export = %q", out)
	}
	// …unless it leads with its own heading, which must not be doubled.
	led := Storage.Artifact{Title: "Notes", Body: "# Notes\n\nSome prose.\n"}
	out, _, _ = Export(led, "md")
	if strings.Contains(string(out), "# Notes\n\n# Notes") {
		t.Errorf("title printed twice:\n%s", out)
	}
}

func TestExportHTMLRendersTheDocument(t *testing.T) {
	dir := t.TempDir()
	figure := filepath.Join(dir, "chart.png")
	if err := os.WriteFile(figure, []byte("\x89PNG\r\n\x1a\n"), 0o600); err != nil {
		t.Fatalf("stage figure: %v", err)
	}

	a := Storage.Artifact{
		Title: "November report",
		Meta:  `{"author":"Argus","date":"2026-11-03","paper":"letter","orientation":"landscape","title":"November report"}`,
		Body: "## Summary\n\nThe quarter closed **up** 4%.\n\n" +
			"| region | delta |\n| --- | --- |\n| EMEA | 4% |\n\n" +
			"![chart](" + figure + ")\n",
	}
	out, ext, err := Export(a, FormatHTML)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if ext != "html" {
		t.Errorf("extension = %q, want html", ext)
	}
	got := string(out)
	for _, want := range []string{
		"<!DOCTYPE html>",
		"<title>November report</title>",
		`<p class="byline">Argus · 2026-11-03</p>`,
		"<h1>November report</h1>",
		"<h2>Summary</h2>",
		"<strong>up</strong>",
		"<table>",
		"<th>region</th>",
		"@page { size: letter landscape;",
		`src="file://` + filepath.ToSlash(figure) + `"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("html export is missing %q\n---\n%s", want, got)
		}
	}
	// The body is stored without front matter, so none of it may reach the page.
	if strings.Contains(got, "paper: letter") {
		t.Error("front matter leaked into the rendered document")
	}
}

// A writer that heads the document itself must not be given a second copy of its
// own title — the mistake that shows up as a duplicated line on page 1 of the
// PDF, in every export derived from one stored body.
func TestExportHTMLDoesNotDoubleTheTitle(t *testing.T) {
	a := Storage.Artifact{Title: "November report", Meta: `{"title":"November report"}`,
		Body: "# November report\n\nThe quarter closed up 4%.\n"}
	out, _, err := Export(a, FormatHTML)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if n := strings.Count(string(out), "<h1>November report</h1>"); n != 1 {
		t.Errorf("title heading appears %d times, want the document's own one only", n)
	}
	if !strings.Contains(string(out), "<title>November report</title>") {
		t.Error("the page title was dropped along with the heading")
	}
}

// The pdf and docx targets depend on programs this machine may not have, so they
// run only when the probe finds one — and when it does, the bytes are checked,
// because "the command exited 0" is not the same claim as "there is a real file
// at the end of it".
func TestExportPDFWhenChromeIsPresent(t *testing.T) {
	if chromeTool.find() == "" {
		t.Skip("no Chrome or Chromium installed")
	}
	a := Storage.Artifact{Title: "Printed", Meta: `{"title":"Printed","paper":"a4"}`,
		Body: "## Section\n\nA paragraph with **emphasis**.\n\n| a | b |\n| --- | --- |\n| 1 | 2 |\n"}
	out, ext, err := Export(a, FormatPDF)
	if err != nil {
		t.Fatalf("pdf export: %v", err)
	}
	if ext != "pdf" || !bytes.HasPrefix(out, []byte("%PDF-")) {
		t.Errorf("got %d bytes as %q without a PDF header", len(out), ext)
	}
}

func TestExportDOCXWhenLibreOfficeIsPresent(t *testing.T) {
	if libreTool.find() == "" {
		t.Skip("no LibreOffice installed")
	}
	dir := t.TempDir()
	figure := filepath.Join(dir, "chart.png")
	png, err := base64.StdEncoding.DecodeString(onePixelPNG)
	if err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if err := os.WriteFile(figure, png, 0o600); err != nil {
		t.Fatalf("stage figure: %v", err)
	}

	a := Storage.Artifact{Title: "Word", Meta: `{"title":"Word","author":"Argus"}`,
		Body: "## Section\n\nA paragraph with **emphasis**.\n\n| a | b |\n| --- | --- |\n| 1 | 2 |\n\n" +
			"![chart](" + figure + ")\n"}
	out, ext, err := Export(a, FormatDOCX)
	if err != nil {
		t.Fatalf("docx export: %v", err)
	}
	// A .docx is a zip; the two signature bytes say the converter produced a
	// package rather than a text dump named .docx.
	if ext != "docx" || !bytes.HasPrefix(out, []byte("PK")) {
		t.Errorf("got %d bytes as %q without a zip header", len(out), ext)
	}

	// The figure is the part that proves the pipeline, not the headings: an html
	// opened by LibreOffice is a Writer/Web document and keeps image references
	// instead of image bytes, so a file:// src — however correct — arrives in
	// Word as a blank box. Only a data: URL is carried across, which is what the
	// docx export asks htmlDocument to produce.
	zr, err := zip.NewReader(bytes.NewReader(out), int64(len(out)))
	if err != nil {
		t.Fatalf("open the produced .docx as a zip: %v", err)
	}
	var media, document []string
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "word/media/") {
			media = append(media, f.Name)
		}
		if f.Name == "word/document.xml" {
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("open word/document.xml: %v", err)
			}
			raw, err := io.ReadAll(rc)
			_ = rc.Close()
			if err != nil {
				t.Fatalf("read word/document.xml: %v", err)
			}
			document = append(document, string(raw))
		}
	}
	if len(media) == 0 {
		t.Error("the local figure did not reach the .docx — word/media is empty")
	}
	if len(document) != 1 {
		t.Fatalf("word/document.xml missing from the package")
	}
	for _, want := range []string{"Section", "emphasis"} {
		if !strings.Contains(document[0], want) {
			t.Errorf("document.xml has no %q", want)
		}
	}
}

// onePixelPNG is a valid 1x1 PNG, the smallest file LibreOffice accepts as an
// image without reporting a broken reference.
const onePixelPNG = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

// The html a browser gets must keep the file:// reference — inlining a scan into
// a page the user stores would bloat it for nothing — so the two targets are
// checked against each other, not just one of them.
func TestHTMLExportKeepsFigureReferencesForDOCXInlinesThem(t *testing.T) {
	dir := t.TempDir()
	figure := filepath.Join(dir, "chart.png")
	if err := os.WriteFile(figure, []byte("\x89PNG\r\n\x1a\n"), 0o600); err != nil {
		t.Fatalf("stage figure: %v", err)
	}
	a := Storage.Artifact{Title: "Figure", Body: "![chart](" + figure + ")\n"}

	page, _, err := Export(a, FormatHTML)
	if err != nil {
		t.Fatalf("html export: %v", err)
	}
	if !strings.Contains(string(page), `src="file://`+filepath.ToSlash(figure)+`"`) {
		t.Errorf("html export lost the figure reference:\n%s", page)
	}
	if strings.Contains(string(page), "data:image/png;base64,") {
		t.Error("html export inlined the image bytes into the page")
	}

	// Without a converter this is still the decision worth testing, so assert it
	// on the intermediate html the docx path builds.
	meta := DecodeMeta(a.Meta)
	inlined, err := htmlDocument(a, meta, true)
	if err != nil {
		t.Fatalf("render for docx: %v", err)
	}
	if !strings.Contains(inlined, "data:image/png;base64,") {
		t.Errorf("docx html kept a reference instead of the bytes:\n%s", inlined)
	}
	if strings.Contains(inlined, `src="file://`) {
		t.Error("docx html still carries a file:// image src")
	}
}

func TestFormatsListsEveryTarget(t *testing.T) {
	got := Formats()
	if len(got) != 4 {
		t.Fatalf("%d formats, want markdown, html, pdf and docx", len(got))
	}
	byID := map[string]int{}
	for i, f := range got {
		byID[f.ID] = i
	}
	for _, id := range []string{FormatMarkdown, FormatHTML, FormatPDF, FormatDOCX} {
		if _, ok := byID[id]; !ok {
			t.Errorf("format %q is missing", id)
		}
	}
	if !got[byID[FormatMarkdown]].Available || !got[byID[FormatHTML]].Available {
		t.Error("markdown and html exports depend on nothing installed, so they are always available")
	}
	// A unavailable converter is still listed, with the reason the user needs.
	for _, id := range []string{FormatPDF, FormatDOCX} {
		f := got[byID[id]]
		if f.Available && f.Required != "" {
			t.Errorf("%s is available but carries a reason: %q", id, f.Required)
		}
		if !f.Available && f.Required == "" {
			t.Errorf("%s is disabled without saying what to install", id)
		}
	}
}

func TestExportUnknownFormat(t *testing.T) {
	if _, _, err := Export(Storage.Artifact{Body: "x\n"}, "rtf"); err == nil {
		t.Error("an unsupported target reported success")
	}
}

// The panel sends a format id and the agent may not, so both spellings have to
// resolve to the same target — and the result has to say which one it produced
// rather than repeating whatever was asked for.
func TestCanonicalFormatNamesOneTargetPerSpelling(t *testing.T) {
	want := map[string]string{
		"":         FormatMarkdown,
		"md":       FormatMarkdown,
		"Markdown": FormatMarkdown,
		" html ":   FormatHTML,
		"htm":      FormatHTML,
		"pdf":      FormatPDF,
		"DOCX":     FormatDOCX,
		"word":     FormatDOCX,
	}
	for in, id := range want {
		if got, ok := canonicalFormat(in); !ok || got != id {
			t.Errorf("canonicalFormat(%q) = %q, %v — want %q", in, got, ok, id)
		}
	}
	if _, ok := canonicalFormat("rtf"); ok {
		t.Error("rtf resolved to a target that does not exist")
	}
	// Every id a caller can reach must also be an id Formats() offers.
	offered := map[string]bool{}
	for _, f := range Formats() {
		offered[f.ID] = true
	}
	for _, id := range want {
		if !offered[id] {
			t.Errorf("%s is exportable but not listed for the menu", id)
		}
	}
}

func TestEncodeDecodeMeta(t *testing.T) {
	if EncodeMeta(nil) != "{}" {
		t.Error("empty metadata must encode as an empty object, not an empty string")
	}
	if got := DecodeMeta("not json"); len(got) != 0 {
		t.Errorf("unreadable metadata decoded to %v, want an empty map so the document still opens", got)
	}
	raw := EncodeMeta(map[string]string{"title": "X", "paper": "a4"})
	back := DecodeMeta(raw)
	if back["title"] != "X" || back["paper"] != "a4" {
		t.Errorf("round trip = %v from %q", back, raw)
	}
}

func TestPageCSSFallsBackToA4Portrait(t *testing.T) {
	if got := pageCSS(map[string]string{}); !strings.Contains(got, "size: a4 portrait") {
		t.Errorf("default page = %q", got)
	}
	if got := pageCSS(map[string]string{"paper": "a3", "orientation": "sideways"}); !strings.Contains(got, "size: a4 portrait") {
		t.Errorf("unrecognised sizes = %q, want the documented fallback", got)
	}
}

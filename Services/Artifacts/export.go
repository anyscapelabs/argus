// The export pipeline. One format is ever authored — Markdown — and every
// other file the user asks for is derived from that stored body, which is why
// a document can be re-exported years later and still match what the agent
// wrote.
//
// The chain is deliberately short:
//
//	markdown  the stored body, front matter restored
//	html      goldmark + the document stylesheet
//	pdf       that html, printed by headless Chrome
//	docx      the same html, converted by headless LibreOffice
//
// Two consequences worth stating: the pdf and the docx are only as good as the
// html, so anything the stylesheet cannot express is missing everywhere; and
// both depend on a binary this machine may not have, which is why every format
// reports whether it is possible right now instead of pretending.
package Artifacts

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/yuin/goldmark/ast"

	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
)

// Export format ids. These strings travel to and from the frontend, so they are
// the wire vocabulary rather than internal names.
const (
	FormatMarkdown = "markdown"
	FormatHTML     = "html"
	FormatPDF      = "pdf"
	FormatDOCX     = "docx"
)

// toolTimeout bounds one external conversion. Chrome and LibreOffice both take a
// second or two from cold; anything slower is a stuck profile rather than a slow
// machine, and a failure tells the user more than more waiting does.
const toolTimeout = 90 * time.Second

// externalTool names a converter the pipeline shells out to, with the executable
// names and paths worth probing here. exec.LookPath accepts a path containing a
// separator, so a macOS bundle binary sits in the same list as a Linux command
// name.
type externalTool struct {
	Label    string
	Install  string
	Probe    []string
	Required string
}

var chromeTool = externalTool{
	Label:   "Chrome",
	Install: "Google Chrome (https://www.google.com/chrome/) or Chromium",
	Probe: []string{
		"google-chrome", "google-chrome-stable", "chromium", "chromium-browser",
		"/usr/bin/google-chrome", "/usr/bin/chromium", "/usr/bin/chromium-browser",
		"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
		"/Applications/Chromium.app/Contents/MacOS/Chromium",
		`C:\Program Files\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
	},
	Required: "PDF export prints through a headless browser, and this machine has none.",
}

var libreTool = externalTool{
	Label:   "LibreOffice",
	Install: "LibreOffice (https://www.libreoffice.org/)",
	Probe: []string{
		"soffice", "libreoffice",
		"/usr/bin/soffice", "/usr/bin/libreoffice",
		"/Applications/LibreOffice.app/Contents/MacOS/soffice",
		`C:\Program Files\LibreOffice\program\soffice.exe`,
		`C:\Program Files (x86)\LibreOffice\program\soffice.exe`,
	},
	Required: ".docx export converts through headless LibreOffice, and this machine has none.",
}

// find returns the executable to use, or "" when this machine has none.
func (t externalTool) find() string {
	for _, candidate := range t.Probe {
		if p, err := exec.LookPath(candidate); err == nil {
			return p
		}
	}
	return ""
}

// Formats describes every export target, including the ones this machine cannot
// produce: the panel shows those disabled with the reason, which is honest in a
// way that hiding them is not.
func Formats() []Shared.ExportFormatView {
	pdf := Shared.ExportFormatView{ID: FormatPDF, Label: "PDF", Extension: "pdf",
		Hint: "Printed by " + chromeTool.Label + "."}
	docx := Shared.ExportFormatView{ID: FormatDOCX, Label: "Word document", Extension: "docx",
		Hint: "Converted by " + libreTool.Label + "."}
	if bin := chromeTool.find(); bin != "" {
		pdf.Available = true
	} else {
		pdf.Required = chromeTool.Required
	}
	if bin := libreTool.find(); bin != "" {
		docx.Available = true
	} else {
		docx.Required = libreTool.Required
	}
	return []Shared.ExportFormatView{
		{ID: FormatMarkdown, Label: "Markdown", Extension: "md", Available: true,
			Hint: "The document exactly as written."},
		{ID: FormatHTML, Label: "HTML page", Extension: "html", Available: true,
			Hint: "One self-contained page, styled for print."},
		pdf, docx,
	}
}

// canonicalFormat maps any name the caller used to the one format id the export
// reports, so "md" and "markdown" describe the same result instead of echoing
// whatever was asked for.
func canonicalFormat(format string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case FormatMarkdown, "md", "":
		return FormatMarkdown, true
	case FormatHTML, "htm":
		return FormatHTML, true
	case FormatPDF:
		return FormatPDF, true
	case FormatDOCX, "word":
		return FormatDOCX, true
	}
	return "", false
}

// Export renders one artifact into the wanted format and returns its bytes and
// the extension to use for it. It writes nothing to disk: the caller decides
// where the file lands, which keeps every filesystem decision in one place.
func Export(a Storage.Artifact, format string) ([]byte, string, error) {
	id, ok := canonicalFormat(format)
	if !ok {
		return nil, "", fmt.Errorf("unknown export format %q", format)
	}
	meta := DecodeMeta(a.Meta)
	switch id {
	case FormatMarkdown:
		return []byte(markdownDocument(a, meta)), "md", nil
	case FormatHTML:
		html, err := htmlDocument(a, meta, false)
		if err != nil {
			return nil, "html", err
		}
		return []byte(html), "html", nil
	case FormatPDF:
		html, err := htmlDocument(a, meta, false)
		if err != nil {
			return nil, "pdf", err
		}
		data, err := printPDF(html)
		return data, "pdf", err
	default:
		html, err := htmlDocument(a, meta, true)
		if err != nil {
			return nil, "docx", err
		}
		data, err := convertDocx(html)
		return data, "docx", err
	}
}

// startsHeading reports whether the body's first content line is a heading. A
// document that already opens on its own title must not be given a second one.
func startsHeading(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		return strings.HasPrefix(t, "#")
	}
	return false
}

// hasTopLevelHeading looks for any level-1 heading in the body, fences ignored.
// The html export prints the document's own title above the text; if the writer
// already headed the document at the top level, printing ours too would show the
// same line twice in the PDF and the Word file.
func hasTopLevelHeading(body string) bool {
	inFence := false
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		if strings.HasPrefix(t, "# ") {
			return true
		}
	}
	return false
}

// markdownDocument restores the front matter the validator stripped, so an
// exported .md re-imports as the same document instead of losing its metadata to
// the round trip.
func markdownDocument(a Storage.Artifact, meta map[string]string) string {
	var b strings.Builder
	have := false
	for _, k := range metaKeyOrder {
		if v := strings.TrimSpace(meta[k]); v != "" {
			if !have {
				b.WriteString("---\n")
				have = true
			}
			fmt.Fprintf(&b, "%s: %s\n", k, v)
		}
	}
	if have {
		b.WriteString("---\n\n")
	} else if t := strings.TrimSpace(a.Title); t != "" && !startsHeading(a.Body) {
		// No metadata at all: a title still has to lead the file, or the export
		// opens on body text — but not when the document leads with its own
		// heading, which would put the same words on screen twice.
		fmt.Fprintf(&b, "# %s\n\n", t)
	}
	b.WriteString(a.Body)
	if !strings.HasSuffix(b.String(), "\n") {
		b.WriteString("\n")
	}
	return b.String()
}

// metaKeyOrder is also the front matter order of an exported .md, which keeps
// two exports of the same document byte-identical.
var metaKeyOrder = []string{"title", "author", "date", "language", "paper", "orientation"}

// htmlDocument renders the canonical body through the same goldmark instance the
// validator parsed, then wraps it in a standalone document. Everything the pdf
// and the docx become is produced here, which is what keeps the three formats
// saying the same thing.
//
// inlineImages decides what a local figure becomes in the markup. LibreOffice
// reads an .html as a Web document and keeps its images as references, so a
// file:// or relative path is dropped from the .docx entirely — the only thing
// it carries through is the image data itself, base64 in the src. A browser needs
// no such favour, and inlining would bloat a page the user keeps on disk, so the
// choice is made per target rather than once.
func htmlDocument(a Storage.Artifact, meta map[string]string, inlineImages bool) (string, error) {
	src := []byte(a.Body)
	doc := parseDocument(src)
	absolutiseImages(doc)
	if inlineImages {
		inlineLocalImages(doc)
	}

	var body bytes.Buffer
	if err := md.Renderer().Render(&body, src, doc); err != nil {
		return "", fmt.Errorf("the document could not be rendered: %w", err)
	}

	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html lang=\"" + htmlEscape(valueOr(meta["language"], "en")) + "\">\n<head>\n")
	b.WriteString(`<meta charset="utf-8">` + "\n")
	fmt.Fprintf(&b, "<title>%s</title>\n", htmlEscape(documentTitle(a, meta)))
	fmt.Fprintf(&b, "<style>\n%s\n%s\n</style>\n</head>\n<body>\n<article>\n", documentCSS, pageCSS(meta))
	b.WriteString(bylineHTML(a, meta, !hasTopLevelHeading(a.Body)))
	b.Write(body.Bytes())
	b.WriteString("\n</article>\n</body>\n</html>\n")
	return b.String(), nil
}

func documentTitle(a Storage.Artifact, meta map[string]string) string {
	if t := strings.TrimSpace(meta["title"]); t != "" {
		return t
	}
	if t := strings.TrimSpace(a.Title); t != "" {
		return t
	}
	return "Document"
}

// bylineHTML is the block above the text. It is generated rather than authored,
// so the writer never has to repeat the title as a heading — and withTitle is
// false when the document already carries its own top-level heading, so the same
// words never appear twice at the top of a page.
func bylineHTML(a Storage.Artifact, meta map[string]string, withTitle bool) string {
	var parts []string
	if withTitle {
		parts = append(parts, "<h1>"+htmlEscape(documentTitle(a, meta))+"</h1>")
	}
	var sub []string
	if v := strings.TrimSpace(meta["author"]); v != "" {
		sub = append(sub, htmlEscape(v))
	}
	if v := strings.TrimSpace(meta["date"]); v != "" {
		sub = append(sub, htmlEscape(v))
	}
	if len(sub) > 0 {
		parts = append(parts, `<p class="byline">`+strings.Join(sub, " · ")+`</p>`)
	}
	return strings.Join(parts, "\n") + "\n"
}

func valueOr(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func htmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

// pageCSS turns the two front matter keys that steer layout into an actual @page
// rule. Anything unrecognised falls back to A4 portrait, matching what the
// validator promises.
func pageCSS(meta map[string]string) string {
	paper := strings.ToLower(strings.TrimSpace(meta["paper"]))
	if paper != "letter" {
		paper = "a4"
	}
	orientation := strings.ToLower(strings.TrimSpace(meta["orientation"]))
	if orientation != "landscape" {
		orientation = "portrait"
	}
	return fmt.Sprintf("@page { size: %s %s; margin: 20mm 18mm; }", paper, orientation)
}

// printPDF asks a headless Chrome to print the html just built. The temporary
// profile is not decoration: without --user-data-dir a print run joins the
// user's real Chrome profile, and when that profile is already open the command
// hangs until the timeout instead of producing a file.
func printPDF(html string) ([]byte, error) {
	bin := chromeTool.find()
	if bin == "" {
		return nil, errors.New(chromeTool.Required)
	}
	dir, err := os.MkdirTemp("", "argus-export-*")
	if err != nil {
		return nil, fmt.Errorf("could not prepare the export: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	src := filepath.Join(dir, "document.html")
	dst := filepath.Join(dir, "document.pdf")
	if err := os.WriteFile(src, []byte(html), 0o600); err != nil {
		return nil, fmt.Errorf("could not stage the document for printing: %w", err)
	}
	args := []string{
		"--headless", "--disable-gpu", "--no-first-run", "--no-default-browser-check",
		"--disable-extensions",
		"--user-data-dir=" + filepath.Join(dir, "profile"),
		"--no-pdf-header-footer",
		"--virtual-time-budget=5000",
		"--print-to-pdf=" + dst,
		fileURL(src),
	}
	out, err := runTool(bin, args, dir)
	if err != nil {
		return nil, fmt.Errorf("Chrome could not print the PDF: %w%s", err, tail(out))
	}
	data, err := os.ReadFile(dst)
	// A printed page is comfortably over a kilobyte; anything smaller is Chrome
	// writing an error page or an empty shell.
	if err != nil || len(data) < 1024 {
		return nil, fmt.Errorf("Chrome produced no usable PDF%s", tail(out))
	}
	return data, nil
}

// convertDocx hands the same html to headless LibreOffice. Its own profile
// directory keeps the conversion out of the user's Writer settings and stops a
// first-run dialog from blocking a run nobody is watching.
func convertDocx(html string) ([]byte, error) {
	bin := libreTool.find()
	if bin == "" {
		return nil, errors.New(libreTool.Required)
	}
	dir, err := os.MkdirTemp("", "argus-export-*")
	if err != nil {
		return nil, fmt.Errorf("could not prepare the export: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()

	src := filepath.Join(dir, "document.html")
	out := filepath.Join(dir, "out")
	if err := os.WriteFile(src, []byte(html), 0o600); err != nil {
		return nil, fmt.Errorf("could not stage the document for conversion: %w", err)
	}
	if err := os.MkdirAll(out, 0o700); err != nil {
		return nil, fmt.Errorf("could not prepare the export: %w", err)
	}
	args := []string{
		"--headless", "--norestore", "--invisible",
		"-env:UserInstallation=" + fileURL(filepath.Join(dir, "profile")),
		// The filter has to be named. LibreOffice loads an .html as a Writer/Web
		// document, and that document class has no default .docx export filter —
		// asking for "docx" alone fails with "no export filter found" instead of
		// picking the Word format.
		"--convert-to", "docx:MS Word 2007 XML",
		"--outdir", out,
		src,
	}
	res, err := runTool(bin, args, dir)
	// LibreOffice can exit non-zero after still writing the file — a stale lock
	// does that — so the output is checked before the status is believed.
	data, rerr := os.ReadFile(filepath.Join(out, "document.docx"))
	if rerr != nil || len(data) == 0 {
		if err != nil {
			return nil, fmt.Errorf("LibreOffice could not build the .docx: %w%s", err, tail(res))
		}
		return nil, fmt.Errorf("LibreOffice produced no .docx%s", tail(res))
	}
	return data, nil
}

// runTool executes one converter under a timeout and returns its output for the
// error message, never for the document.
func runTool(bin string, args []string, dir string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), toolTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := stdout.String() + stderr.String()
	if ctx.Err() == context.DeadlineExceeded {
		return out, fmt.Errorf("it took longer than %s and was stopped", toolTimeout)
	}
	if err != nil {
		return out, err
	}
	return out, nil
}

// tail shortens a converter's log to the part that names the problem.
func tail(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) > 300 {
		s = "…" + s[len(s)-300:]
	}
	return " — " + s
}

// fileURL makes a path a URL a local converter will load. A Windows path needs
// the extra leading slash to read as file:///C:/…; every other platform is
// already absolute in the slash form.
func fileURL(path string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return "file://" + strings.ReplaceAll(p, " ", "%20")
}

// imageURL is fileURL for a path that goes into an <img src> through goldmark,
// which escapes the destination itself. Pre-escaping would be escaped again and
// a "chart%20one.png" would arrive as "chart%2520one.png".
func imageURL(path string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return "file://" + p
}

// absolutiseImages rewrites local image destinations into absolute file:// URLs.
// The rendered html lives in a temporary folder, so a path relative to the
// document would resolve against that and export as a broken box. Remote and
// data URLs are left exactly as written.
func absolutiseImages(doc ast.Node) {
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		img, ok := n.(*ast.Image)
		if !ok {
			return ast.WalkContinue, nil
		}
		dest := strings.TrimSpace(string(img.Destination))
		if dest == "" || strings.Contains(dest, "://") || strings.HasPrefix(dest, "data:") {
			return ast.WalkContinue, nil
		}
		if abs, err := expandHome(dest); err == nil {
			if _, serr := os.Stat(abs); serr == nil {
				img.Destination = []byte(imageURL(abs))
			}
		}
		return ast.WalkContinue, nil
	})
}

// maxInlineImageSize bounds what one figure may be folded into a .docx. A
// 20 MB scan would make the intermediate HTML unreadable by LibreOffice and the
// conversion slower than the timeout; such a file keeps its file:// reference and
// is simply missing from the Word export, which is the honest outcome.
const maxInlineImageSize = 20 << 20

// inlineLocalImages replaces every local file:// image destination with the
// bytes of that file as a data: URL, which is the only form LibreOffice carries
// into .docx. Unreadable or oversized files keep their URL rather than turning a
// figure into a broken box.
func inlineLocalImages(doc ast.Node) {
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		img, ok := n.(*ast.Image)
		if !ok {
			return ast.WalkContinue, nil
		}
		dest := strings.TrimSpace(string(img.Destination))
		if !strings.HasPrefix(dest, "file://") {
			return ast.WalkContinue, nil
		}
		path := strings.TrimPrefix(dest, "file://")
		info, err := os.Stat(path)
		if err != nil || info.Size() > maxInlineImageSize {
			return ast.WalkContinue, nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return ast.WalkContinue, nil
		}
		mime := imageMIME(path)
		if mime == "" {
			return ast.WalkContinue, nil
		}
		img.Destination = []byte("data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data))
		return ast.WalkContinue, nil
	})
}

// imageMIME names the types both Chrome and LibreOffice accept inline; anything
// else is refused by the validator before it can ever reach here.
func imageMIME(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	}
	return ""
}

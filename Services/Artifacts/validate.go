// Package Artifacts holds the documents the agent writes. One format is ever
// authored — Markdown, a closed subset of GFM with a small front matter header —
// and every other file type is derived from that body by the export pipeline.
// This file is what makes that promise hold: it rejects, before anything is
// stored, the constructs the exporters cannot carry, so the model gets a line
// number to fix in the same run instead of a Word file with a hole in it.
package Artifacts

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	rendererhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
)

const (
	SeverityError   = "error"
	SeverityWarning = "warning"

	// MaxHeadingLevel is where the export styles stop. The print stylesheet and
	// the Word template define Title and Heading1-4; anything deeper would
	// silently fall back to body text, which reads as a broken document.
	MaxHeadingLevel = 4

	// maxDiagnostics keeps a tool result short. The model can only act on a
	// handful of fixes at once, and a long list is mostly repeats.
	maxDiagnostics = 12
)

// frontMatterKeys is the closed metadata schema. An unknown key is rejected
// rather than ignored, because an ignored key is a user expectation that
// quietly does nothing.
var frontMatterKeys = map[string]string{
	"title":       "document title",
	"author":      "byline under the title",
	"date":        "the date shown under the title",
	"language":    "language tag, e.g. en",
	"paper":       "a4 or letter",
	"orientation": "portrait or landscape",
}

var (
	fenceStart       = regexp.MustCompile(`^ {0,3}(` + "`" + `{3,}|~{3,})`)
	footnoteRef      = regexp.MustCompile(`\[\^[^\]\s]+\]`)
	dividerRow       = regexp.MustCompile(`^ {0,3}\|?[\s:|-]*-[\s:|-]*$`)
	keyValue         = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9_-]*)[ \t]*:[ \t]*(.*)$`)
	firstHeadingLine = regexp.MustCompile(`^#{1,4}[ \t]+\S`)
)

// Diagnostic is one lint finding. Line is 1-based against the text the model
// sent, front matter included, so a quoted line number is quotable back to it.
type Diagnostic struct {
	Severity string `json:"severity"`
	Line     int    `json:"line"`
	Message  string `json:"message"`
}

// Result is a validated document: the normalised body with the front matter
// removed, the resolved title, and everything worth telling the writer.
type Result struct {
	Body        string
	Title       string
	FrontMatter map[string]string
	Diagnostics []Diagnostic

	// orig is the normalised document including front matter; it is what line
	// numbers are measured against.
	orig string
	// offset is where Body starts inside orig, in bytes.
	offset int
	// truncated counts findings dropped by maxDiagnostics.
	truncated int
}

func (r Result) HasErrors() bool {
	for _, d := range r.Diagnostics {
		if d.Severity == SeverityError {
			return true
		}
	}
	return false
}

// Report renders the findings the way the model should read them: one line
// each, with the fix named, and nothing that wastes another round trip.
func (r Result) Report() string {
	if len(r.Diagnostics) == 0 {
		return ""
	}
	var b strings.Builder
	for _, d := range r.Diagnostics {
		where := "the document"
		if d.Line > 0 {
			where = fmt.Sprintf("line %d", d.Line)
		}
		kind := "note"
		if d.Severity == SeverityError {
			kind = "rejected"
		}
		fmt.Fprintf(&b, "%s at %s: %s\n", kind, where, d.Message)
	}
	if r.truncated > 0 {
		fmt.Fprintf(&b, "…and %d more finding(s); fix these and write again.\n", r.truncated)
	}
	return strings.TrimRight(b.String(), "\n")
}

// Validate normalises and checks one artifact body. It never writes: the caller
// stores r.Body only when r.HasErrors() is false.
func Validate(src string) Result {
	res := Result{FrontMatter: map[string]string{}}
	normalised := normalise(src)
	res.orig = normalised

	body, fmLines := splitFrontMatter(normalised, &res)
	res.Body = body
	res.offset = len(normalised) - len(body)
	res.Title = res.FrontMatter["title"]

	res.checkFrontMatter(fmLines)
	res.checkEmpty(body, fmLines)
	res.checkHeadingFirst(body)
	res.lintSource(normalised)
	res.walkAST(body)
	return res
}

// checkHeadingFirst gives every stored document a real hierarchy. Validation
// runs on the fully assembled body, so append chunks need not repeat the title.
func (r *Result) checkHeadingFirst(body string) {
	if strings.TrimSpace(body) == "" {
		return
	}
	pos := 0
	for _, line := range strings.SplitAfter(body, "\n") {
		trimmed := strings.TrimSpace(strings.TrimSuffix(line, "\n"))
		if trimmed != "" {
			if !firstHeadingLine.MatchString(trimmed) {
				r.add(SeverityError, r.lineOf(pos), "the document must start with a Markdown heading (# through ####), after optional front matter")
			}
			return
		}
		pos += len(line)
	}
}

// normalise makes the stored body deterministic: one line-ending style, no BOM,
// no trailing blank lines, exactly one final newline. Exporters then see
// byte-identical input on every platform, which is what makes a re-export
// stable enough to diff.
func normalise(src string) string {
	s := strings.ReplaceAll(src, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.TrimPrefix(s, "\ufeff")
	s = strings.TrimRight(s, " \t\n")
	if s == "" {
		return ""
	}
	return s + "\n"
}

// splitFrontMatter removes a leading --- block and records its key: value pairs.
// A block that never closes stays in the body, because guessing where a document
// begins is worse than reading the whole text as body.
func splitFrontMatter(src string, res *Result) (string, []string) {
	lines := strings.Split(src, "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[0]) != "---" {
		return src, nil
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if t := strings.TrimSpace(lines[i]); t == "---" || t == "..." {
			end = i
			break
		}
	}
	if end < 0 {
		return src, nil
	}
	for _, line := range lines[1:end] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		m := keyValue.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue // reported by checkFrontMatter
		}
		res.FrontMatter[strings.ToLower(m[1])] = strings.Trim(strings.TrimSpace(m[2]), `"'`)
	}
	return strings.Join(lines[end+1:], "\n"), lines[1:end]
}

func (r *Result) checkFrontMatter(fmLines []string) {
	if len(fmLines) == 0 {
		return
	}
	for i, line := range fmLines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		// Line 1 is the opening ---, so the block's first key is line 2.
		lineNo := i + 2
		m := keyValue.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			r.add(SeverityError, lineNo, fmt.Sprintf("front matter must read \"key: value\", got %q", strings.TrimSpace(line)))
			continue
		}
		key := strings.ToLower(m[1])
		if _, ok := frontMatterKeys[key]; !ok {
			allowed := make([]string, 0, len(frontMatterKeys))
			for k := range frontMatterKeys {
				allowed = append(allowed, k)
			}
			sort.Strings(allowed)
			r.add(SeverityError, lineNo, fmt.Sprintf("unknown front matter key %q; allowed: %s", key, strings.Join(allowed, ", ")))
			continue
		}
		if strings.TrimSpace(m[2]) == "" {
			r.add(SeverityWarning, lineNo, fmt.Sprintf("front matter key %q has no value; it is ignored", key))
		}
	}
	if v := r.FrontMatter["paper"]; v != "" && v != "a4" && v != "letter" {
		r.add(SeverityError, 1, fmt.Sprintf("paper %q is not supported; use a4 or letter", v))
	}
	if v := r.FrontMatter["orientation"]; v != "" && v != "portrait" && v != "landscape" {
		r.add(SeverityError, 1, fmt.Sprintf("orientation %q is not supported; use portrait or landscape", v))
	}
}

func (r *Result) checkEmpty(body string, fmLines []string) {
	if strings.TrimSpace(body) == "" {
		if len(fmLines) > 0 {
			r.add(SeverityError, 1, "the document has front matter but no body")
			return
		}
		r.add(SeverityError, 1, "the document body is empty")
	}
}

// lintSource is the text-level pass. Footnote syntax and table shape have to be
// caught here: without their extensions enabled, footnotes parse as ordinary
// text, and the table parser would silently pad or drop cells rather than
// report them.
func (r *Result) lintSource(src string) {
	lines := strings.Split(src, "\n")
	inFence := false
	marker := ""
	for i, line := range lines {
		lineNo := i + 1
		if inFence {
			if strings.HasPrefix(strings.TrimSpace(line), marker) {
				inFence = false
				marker = ""
			}
			continue
		}
		if m := fenceStart.FindStringSubmatch(line); m != nil {
			inFence = true
			marker = m[1][:3]
			continue
		}
		if footnoteRef.MatchString(line) {
			r.add(SeverityError, lineNo, "footnotes are not supported; inline the point as a sentence or a link")
		}
		r.checkTable(lines, i)
	}
}

// checkTable inspects the run of pipe-bearing lines that starts at i, reporting
// rows whose cell count differs from the header. Only the first line of a run
// does the work, so one bad table produces one finding.
func (r *Result) checkTable(lines []string, i int) {
	if !strings.Contains(lines[i], "|") {
		return
	}
	if i > 0 && strings.Contains(lines[i-1], "|") {
		return // already handled from the run's first line
	}
	j := i
	for j < len(lines) && strings.Contains(lines[j], "|") {
		j++
	}
	run := lines[i:j]
	if len(run) < 2 || !dividerRow.MatchString(run[1]) {
		return // prose that happens to contain a pipe
	}
	want := cells(run[0])
	if want < 2 {
		r.add(SeverityError, i+1, "a table needs at least two columns")
		return
	}
	if got := cells(run[1]); got != want {
		r.add(SeverityError, i+2, fmt.Sprintf("the divider row declares %d columns but the header has %d; they must match", got, want))
	}
	for k := 2; k < len(run); k++ {
		if got := cells(run[k]); got != want {
			r.add(SeverityError, i+k+1, fmt.Sprintf("this row has %d cells but the header declares %d; every row must match", got, want))
		}
	}
}

// cells counts the columns of a markdown table row: the pipes at the ends are
// not cells, an escaped pipe inside one is not a separator.
func cells(row string) int {
	s := strings.TrimSpace(row)
	s = strings.TrimPrefix(s, "|")
	s = strings.TrimSuffix(s, "|")
	if s == "" {
		return 0
	}
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' {
			i++
			continue
		}
		if s[i] == '|' {
			n++
		}
	}
	return n + 1
}

// md is the one parser and renderer the package shares. Keeping it in a single
// value is what makes the promise real: the constructs the validator checked are
// byte-for-byte the constructs the exporters render, because both read through
// this instance.
//
// The renderer runs in unsafe mode, which is not a hole: "unsafe" in goldmark
// means raw HTML is passed through and file:/data: URLs are not blanked. The
// validator already rejects raw HTML from the body, so there is none to pass —
// and a document's own figures are local files the user's machine opened, which
// the export must be able to show instead of an empty src attribute.
var md = goldmark.New(
	goldmark.WithExtensions(extension.Table, extension.Strikethrough, extension.TaskList),
	goldmark.WithRendererOptions(rendererhtml.WithUnsafe()),
)

// parseDocument turns a canonical body into an AST. goldmark's parser reports
// problems as findings inside the tree rather than as an error, so there is no
// second return value to check — the validator walks what comes back.
func parseDocument(src []byte) ast.Node {
	return md.Parser().Parse(text.NewReader(src))
}

// walkAST is the structural pass over the parsed body.
func (r *Result) walkAST(body string) {
	if strings.TrimSpace(body) == "" {
		return
	}
	doc := parseDocument([]byte(body))

	err := ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch t := n.(type) {
		case *ast.HTMLBlock:
			r.add(SeverityError, r.lineOf(t.Pos()), "raw HTML is not supported; describe the block as text, a list or a table")
		case *ast.RawHTML:
			r.add(SeverityError, r.lineOf(t.Pos()), "inline HTML such as <br> or <img> is not supported; use Markdown")
		case *ast.Heading:
			if t.Level > MaxHeadingLevel {
				r.add(SeverityError, r.lineOf(t.Pos()), fmt.Sprintf("heading level %d is too deep; documents style levels 1-%d", t.Level, MaxHeadingLevel))
			}
		case *ast.Image:
			r.checkImage(string(t.Destination), r.lineOf(t.Pos()))
		}
		return ast.WalkContinue, nil
	})
	if err != nil {
		r.add(SeverityError, 0, "the document could not be parsed: "+err.Error())
	}
}

// lineOf turns a byte offset inside the body into a line number in the text the
// model sent. An unknown position reports 0, which renders as "the document".
func (r *Result) lineOf(pos int) int {
	if pos < 0 || r.orig == "" {
		return 0
	}
	full := r.offset + pos
	if full > len(r.orig) {
		full = len(r.orig)
	}
	return strings.Count(r.orig[:full], "\n") + 1
}

// checkImage decides what an embedded figure can become. Local paths are the
// only thing every exporter can inline, so a remote URL is a warning rather
// than a silent surprise in the Word file, and a missing file is refused now
// instead of exported as a broken box.
func (r *Result) checkImage(dest string, line int) {
	d := strings.TrimSpace(dest)
	if d == "" {
		r.add(SeverityError, line, "an image has no path")
		return
	}
	if strings.HasPrefix(d, "http://") || strings.HasPrefix(d, "https://") {
		r.add(SeverityWarning, line, "a remote image appears in HTML and PDF but not in .docx; save it locally for every format")
		return
	}
	if strings.HasPrefix(d, "data:") {
		r.add(SeverityWarning, line, "an embedded data image works but bloats the document; a file path is better")
		return
	}
	path, err := expandHome(d)
	if err != nil {
		r.add(SeverityError, line, fmt.Sprintf("image path %q cannot be resolved: %s", d, err))
		return
	}
	if _, err := os.Stat(path); err != nil {
		r.add(SeverityError, line, fmt.Sprintf("image file not found: %s", path))
		return
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
	default:
		r.add(SeverityError, line, fmt.Sprintf("image format %q is not supported; use png, jpg, gif or webp", filepath.Ext(path)))
	}
}

func (r *Result) add(severity string, line int, msg string) {
	if len(r.Diagnostics) >= maxDiagnostics {
		r.truncated++
		return
	}
	r.Diagnostics = append(r.Diagnostics, Diagnostic{Severity: severity, Line: line, Message: msg})
}

// expandHome resolves ~ and rejects relative paths, so a document cannot point
// somewhere the user has no reason to expect.
func expandHome(p string) (string, error) {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/")), nil
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("a relative path has no base folder; give an absolute path or start it with ~/")
	}
	return filepath.Clean(p), nil
}

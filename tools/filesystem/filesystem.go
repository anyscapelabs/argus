// Package filesystem gives the model cross-platform file tools: read, write
// and grep. Everything is pure Go (os, path/filepath, regexp), so the same
// code runs on Linux, macOS and Windows with no per-OS branch. Paths are
// absolute, sized, and binary-safe: the tools refuse to do silent damage and
// let the permission gate in Services/Chat decide whether the agent may call
// them at all.
package filesystem

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	maxReadBytes    = 256 * 1024
	maxWriteBytes   = 1 << 20
	maxLineLength   = 400
	maxGrepMatches  = 200
	maxGrepFileByte = 8 << 20
)

// skipDirs are directories grep never descends into; they hold VCS internals,
// dependency dumps and build output, none of which help the model.
var skipDirs = map[string]bool{
	".git": true, ".hg": true, ".svn": true,
	"node_modules": true, "vendor": true, "__pycache__": true,
	".venv": true, "venv": true, ".cache": true,
	"dist": true, "target": true, ".idea": true, ".vscode": true,
}

// ReadResult is one read_file call: the requested window of the file plus how
// it relates to the whole file, so the model can page instead of guessing.
type ReadResult struct {
	Path       string `json:"path"`
	Offset     int    `json:"offset"`
	Lines      int    `json:"lines"`
	TotalLines int    `json:"total_lines"`
	Truncated  bool   `json:"truncated"`
	Content    string `json:"content"`
}

// WriteResult is one write_file call.
type WriteResult struct {
	Path     string `json:"path"`
	Bytes    int    `json:"bytes"`
	Existed  bool   `json:"existed"`
	Appended bool   `json:"appended"`
}

// GrepMatch is one matching line, ripgrep style: long lines are clipped to
// the display cap in Text.
type GrepMatch struct {
	Path string `json:"path"`
	Line int    `json:"line"`
	Text string `json:"text"`
}

// GrepResult is one grep_files call.
type GrepResult struct {
	Pattern   string      `json:"pattern"`
	Matches   []GrepMatch `json:"matches"`
	FilesScan int         `json:"files_scanned"`
	Limited   bool        `json:"limited"`
}

// Read returns a window of a text file. offsetLine is 1-based; 0 or 1 starts
// at the top, limit 0 defaults to 2000 lines. Binary files are refused: the
// model has no use for their bytes and would only burn its context.
func Read(path string, offsetLine, limit int) (ReadResult, error) {
	abs, err := absClean(path)
	if err != nil {
		return ReadResult{}, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return ReadResult{}, err
	}
	if st.IsDir() {
		return ReadResult{}, fmt.Errorf("%s is a directory", abs)
	}
	if err := rejectBinary(abs); err != nil {
		return ReadResult{}, err
	}
	if limit <= 0 {
		limit = 2000
	}
	if limit > 2000 {
		limit = 2000
	}
	if offsetLine <= 0 {
		offsetLine = 1
	}

	f, err := os.Open(abs)
	if err != nil {
		return ReadResult{}, err
	}
	defer f.Close()

	var b strings.Builder
	scroll := bufio.NewScanner(f)
	scroll.Buffer(make([]byte, maxReadBytes+1024), maxReadBytes+1024)
	total, written := 0, 0
	truncated := false
	for scroll.Scan() {
		total++
		if total < offsetLine {
			continue
		}
		if written >= limit || b.Len() > maxReadBytes {
			truncated = true
			break
		}
		if written > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(scroll.Text())
		written++
	}
	scannerErr := scroll.Err()
	if scannerErr != nil && !truncated {
		return ReadResult{}, fmt.Errorf("%s: %w", abs, scannerErr)
	}
	return ReadResult{
		Path:       abs,
		Offset:     offsetLine,
		Lines:      written,
		TotalLines: total,
		Truncated:  truncated,
		Content:    b.String(),
	}, nil
}

// Write creates or replaces (or appends to) one file, making parent
// directories as needed. Overwrite is explicit so "I just saved X" intent can
// never destroy a file the user cares about by accident.
func Write(path, content string, overwrite, appendMode bool) (WriteResult, error) {
	abs, err := absClean(path)
	if err != nil {
		return WriteResult{}, err
	}
	if len(content) > maxWriteBytes {
		return WriteResult{}, fmt.Errorf("content is %d bytes, the limit is %d",
			len(content), maxWriteBytes)
	}
	if _, err := os.Stat(abs); err == nil && !overwrite && !appendMode {
		return WriteResult{}, fmt.Errorf("%s already exists: pass overwrite to replace it or append to extend it", abs)
	}
	if dir := filepath.Dir(abs); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return WriteResult{}, err
		}
	}
	flags := os.O_WRONLY | os.O_CREATE
	switch {
	case appendMode:
		flags |= os.O_APPEND
	default:
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(abs, flags, 0o644)
	if err != nil {
		return WriteResult{}, err
	}
	defer f.Close()
	n, err := f.WriteString(content)
	if err != nil {
		return WriteResult{}, err
	}
	return WriteResult{Path: abs, Bytes: n, Existed: !overwrite && appendMode, Appended: appendMode}, nil
}

// Grep scans root for lines matching pattern. include selects files by
// filename glob (e.g. "*.go", "*.ts"); empty means every scanned file.
// Binary-looking files and deep dependency directories are skipped in both
// the file walk and the scan itself.
func Grep(pattern, include, root string) (GrepResult, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return GrepResult{}, fmt.Errorf("bad pattern: %w", err)
	}
	abs, err := absClean(root)
	if err != nil {
		return GrepResult{}, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return GrepResult{}, err
	}
	if !st.IsDir() {
		return GrepResult{}, fmt.Errorf("%s is not a directory", abs)
	}

	out := GrepResult{Pattern: pattern}
	err = filepath.WalkDir(abs, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // unreadable entries are skipped, not fatal
		}
		name := d.Name()
		if d.IsDir() {
			if p != abs && skipDirs[name] {
				return filepath.SkipDir
			}
			return nil
		}
		if include != "" {
			if ok, merr := filepath.Match(include, name); merr != nil || !ok {
				return nil
			}
		}
		if info, ierr := d.Info(); ierr != nil || info.Size() > maxGrepFileByte {
			return nil
		}
		out.FilesScan++
		scanFile(p, re, &out)
		if out.Limited {
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil && !out.Limited {
		return GrepResult{}, err
	}
	return out, nil
}

func scanFile(path string, re *regexp.Regexp, out *GrepResult) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	scroll := bufio.NewScanner(f)
	scroll.Buffer(make([]byte, 2<<20), 2<<20)
	for ln := 1; scroll.Scan(); ln++ {
		lineText := scroll.Text()
		if strings.ContainsRune(lineText, 0) {
			return // binary file: stop scanning it entirely
		}
		if re.MatchString(lineText) {
			text := lineText
			if len(text) > maxLineLength {
				text = text[:maxLineLength]
			}
			out.Matches = append(out.Matches, GrepMatch{Path: path, Line: ln, Text: text})
			if len(out.Matches) >= maxGrepMatches {
				out.Limited = true
				return
			}
		}
	}
}

// absClean resolves and validates the path once, at the front door, so every
// branch below works with one canonical string. A leading ~ means the user's
// home directory, the path models can actually find on any OS.
func absClean(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	if strings.ContainsRune(path, 0) {
		return "", fmt.Errorf("path contains a null byte")
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", fmt.Errorf("could not resolve ~: %w", herr)
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~"))
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

// rejectBinary peeks at the head of the file: a NUL byte in the first chunk
// is the oldest and most portable binary detector there is.
func rejectBinary(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	head := make([]byte, 1024)
	n, _ := f.Read(head)
	if n > 0 && strings.ContainsRune(string(head[:n]), 0) {
		return fmt.Errorf("%s looks binary; read_file only returns text", path)
	}
	return nil
}

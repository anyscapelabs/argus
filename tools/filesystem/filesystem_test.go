package filesystem

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mustTemp(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return dir
}

func TestReadPagesLines(t *testing.T) {
	dir := mustTemp(t)
	path := filepath.Join(dir, "a.txt")
	content := strings.Repeat("line\n", 50) // 50 lines, each "line"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Read(path, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if res.TotalLines != 50 || res.Lines != 50 || res.Truncated {
		t.Fatalf("unexpected: %+v err=%v", res, err)
	}
	page, err := Read(path, 45, 10)
	if err != nil {
		t.Fatal(err)
	}
	if page.Offset != 45 || page.Lines != 6 || page.Truncated {
		t.Fatalf("expected final 6 lines as a complete page, got %+v", page)
	}
	mid, err := Read(path, 46, 2)
	if err != nil {
		t.Fatal(err)
	}
	if mid.Lines != 2 || !mid.Truncated {
		t.Fatalf("expected a truncated middle page, got %+v", mid)
	}
	wanted := "line\nline\nline\nline\nline\nline"
	if page.Content != wanted {
		t.Fatalf("unexpected content %q", page.Content)
	}
}

func TestReadRefusesBinaries(t *testing.T) {
	dir := mustTemp(t)
	path := filepath.Join(dir, "b.bin")
	if err := os.WriteFile(path, []byte("ok\x00no"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(path, 0, 0); err == nil {
		t.Fatal("expected binary refusal")
	}
}

func TestWriteCreatesParentsAndGuardsOverwrites(t *testing.T) {
	dir := mustTemp(t)
	path := filepath.Join(dir, "sub", "dir", "c.txt")
	w, err := Write(path, "hello", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if w.Bytes != 5 {
		t.Fatalf("expected 5 bytes, got %d", w.Bytes)
	}
	if w.Action != "created" || w.Lines != 1 {
		t.Fatalf("unexpected create metadata: %+v", w)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "hello" {
		t.Fatalf("unexpected file: %q err=%v", data, err)
	}
	if _, err := Write(path, "bye", false, false); err == nil {
		t.Fatal("expected overwrite refusal without overwrite=true")
	}
	changed, err := Write(path, "bye\n", true, false)
	if err != nil {
		t.Fatalf("overwrite should pass: %v", err)
	}
	if changed.Action != "changed" || changed.Lines != 1 {
		t.Fatalf("unexpected overwrite metadata: %+v", changed)
	}
	if data, _ := os.ReadFile(path); string(data) != "bye\n" {
		t.Fatalf("unexpected content after overwrite: %q", data)
	}
	if _, err := Write(path, "-tail", false, true); err != nil {
		t.Fatalf("append should pass: %v", err)
	}
	if data, _ := os.ReadFile(path); string(data) != "bye\n-tail" {
		t.Fatalf("unexpected appended content: %q", data)
	}
	if _, err := Write(path, "bad", true, true); err == nil {
		t.Fatal("overwrite and append were accepted together")
	}
}

func TestEditRequiresExactMatchAndWritesAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "code.go")
	if err := os.WriteFile(path, []byte("package demo\n\nconst value = 1\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	res, err := Edit(path, "const value = 1", "const value = 2", 1)
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != "changed" || res.Lines != 3 {
		t.Fatalf("unexpected edit metadata: %+v", res)
	}
	if _, err := Edit(path, "missing", "x", 1); err == nil {
		t.Fatal("missing match changed the file")
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "value = 2") {
		t.Fatalf("edit did not land: %q", data)
	}
}

func TestGrepSkipsDepsAndLimits(t *testing.T) {
	dir := mustTemp(t)
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("want this\nnot this\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "node_modules", "x.js"), []byte("want this too\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.ts"), []byte("want this as well\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Grep("want this", "", dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Matches) != 2 {
		t.Fatalf("expected 2 matches outside node_modules, got %+v", res.Matches)
	}
	if res.FilesScan != 2 {
		t.Fatalf("expected to scan 2 files, scanned %d", res.FilesScan)
	}
	globbed, err := Grep("want this", "*.ts", dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(globbed.Matches) != 1 || !strings.HasSuffix(globbed.Matches[0].Path, "b.ts") {
		t.Fatalf("glob filter failed: %+v", globbed.Matches)
	}
	if _, err := Grep("(", "", dir); err == nil {
		t.Fatal("expected bad regex rejection")
	}
}

func TestAbsCleanRejectsEmptyAndNull(t *testing.T) {
	if _, err := absClean("   "); err == nil {
		t.Fatal("expected empty path rejection")
	}
	if _, err := absClean("a\x00b"); err == nil {
		t.Fatal("expected null byte rejection")
	}
	if p, err := absClean("desktop/../Desktop"); err != nil {
		t.Fatal(err)
	} else if strings.Contains(p, "..") {
		t.Fatalf("expected cleaned path, got %q", p)
	}
}

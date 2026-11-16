package Slides

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

func zipContains(t *testing.T, data []byte, name string) bool {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("package is not a valid zip: %v", err)
	}
	for _, f := range zr.File {
		if f.Name == name {
			return true
		}
	}
	return false
}

// zipEntryText reads one part of a package as a string.
func zipEntryText(t *testing.T, data []byte, name string) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("package is not a valid zip: %v", err)
	}
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("open %s: %v", name, err)
			}
			defer func() { _ = rc.Close() }()
			var buf bytes.Buffer
			if _, err := buf.ReadFrom(rc); err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			return buf.String()
		}
	}
	t.Fatalf("package has no entry %q", name)
	return ""
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}

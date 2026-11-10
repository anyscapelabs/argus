package Uploads

import (
	"archive/zip"
	"bytes"
	"testing"
)

// docxZip builds an in-memory Word package (a zip) with one entry, so the
// extractor can be tested without a real Word file.
func docxZip(t *testing.T, name, content string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

package Uploads

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"strings"
)

// extractDOCX reads the text of a Word document by unzipping the file and
// pulling the words out of word/document.xml. The XML is walked char by char:
// text lives in <w:t> elements, and the paragraph breaks between them become
// newlines so the agent keeps the document's structure.
func extractDOCX(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("could not read the Word file: %w", err)
	}
	var doc *zip.File
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			doc = f
			break
		}
	}
	if doc == nil {
		return "", fmt.Errorf("the Word file has no document body")
	}
	rc, err := doc.Open()
	if err != nil {
		return "", fmt.Errorf("could not open the Word body: %w", err)
	}
	defer func() { _ = rc.Close() }()
	xml, err := io.ReadAll(rc)
	if err != nil {
		return "", fmt.Errorf("could not read the Word body: %w", err)
	}
	return strings.TrimSpace(docxText(xml)), nil
}

// docxText converts the document XML into readable text. Paragraph, row and
// break elements become newlines; text inside a <w:t> is copied verbatim; every
// other tag is dropped. The output is a flattened reading of the document,
// which is exactly what the agent needs.
func docxText(xml []byte) string {
	var b strings.Builder
	for i := 0; i < len(xml); {
		if xml[i] != '<' {
			// Bare text outside a <w:t> (whitespace, XML boilerplate) is copied
			// so interleaved runs read correctly.
			j := i
			for j < len(xml) && xml[j] != '<' {
				j++
			}
			b.Write(xml[i:j])
			i = j
			continue
		}
		end := bytes.IndexByte(xml[i:], '>')
		if end < 0 {
			break
		}
		tag := xml[i+1 : i+end]
		switch tagName(tag) {
		case "w:p", "w:tr", "w:br", "w:drawing", "w:tab":
			b.WriteByte('\n')
		}
		i += end + 1
	}
	return b.String()
}

// tagName is the element name of a raw tag slice, with leading '/' and trailing
// attributes stripped.
func tagName(tag []byte) string {
	tag = bytes.TrimSpace(tag)
	if len(tag) == 0 || tag[0] == '?' || tag[0] == '!' {
		return ""
	}
	if tag[0] == '/' {
		tag = tag[1:]
	}
	if sp := bytes.IndexAny(tag, " \t\n"); sp >= 0 {
		tag = tag[:sp]
	}
	return string(tag)
}

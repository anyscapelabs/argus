package Uploads

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/ledongthuc/pdf"
)

// extractPDF pulls the plain text out of a PDF. It is best-effort: a scanned
// page has no text layer, and the reader is only as good as the fonts the file
// embeds, but for a real document it is what the agent needs.
func extractPDF(data []byte) (string, error) {
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("could not read the PDF: %w", err)
	}
	txt, err := r.GetPlainText()
	if err != nil {
		return "", fmt.Errorf("could not read the PDF text: %w", err)
	}
	body, err := io.ReadAll(txt)
	if err != nil {
		return "", fmt.Errorf("could not read the PDF text: %w", err)
	}
	return strings.TrimSpace(string(body)), nil
}

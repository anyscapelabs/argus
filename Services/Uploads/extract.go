package Uploads

import (
	"bytes"
	"fmt"
)

// extractText is the ingestion rule for an uploaded document: turn the file
// into the text the agent reads, or report why that is impossible. Images need
// no extraction — they go to the model as vision blocks — so the dispatcher is
// only ever called for documents.
func extractText(mimeType string, data []byte) (string, error) {
	switch mimeType {
	case "application/pdf":
		return extractPDF(data)
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return extractDOCX(data)
	case "application/msword":
		// The legacy binary .doc format has no pure-Go reader here; the file is
		// still stored, but there is nothing safe to hand the model.
		return "", nil
	case "text/plain", "text/markdown":
		return string(bytes.TrimSpace(data)), nil
	}
	return "", fmt.Errorf("no extractor for %s", mimeType)
}

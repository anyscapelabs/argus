package Uploads

import (
	"fmt"
	"mime"
	"path/filepath"
	"strings"

	"github.com/anyscapelabs/argus/Services/Shared"
)

// MaxUploadBytes caps one uploaded file. Ten megabytes holds a scan or a deck
// of photos and keeps a single upload out of the memory budget of a chat turn.
const MaxUploadBytes = 10 << 20

// allowedMime maps an accepted mime type to the attachment kind it becomes.
// Only the types the pipeline knows how to consume are accepted: images feed
// vision, the three text-bearing formats feed extraction, everything else is
// refused rather than silently dropped from the model's view.
var allowedMime = map[string]string{
	"image/png":          "image",
	"image/jpeg":         "image",
	"image/gif":          "image",
	"image/webp":         "image",
	"image/bmp":          "image",
	"application/pdf":    "document",
	"application/msword": "document",
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document": "document",
	"text/plain":    "document",
	"text/markdown": "document",
}

// normalizeMime turns the caller's mime guess into a canonical one, falling
// back to sniffing the file extension when the guess is empty or generic.
// A browser hands over a real mime for typed files, but a paste or a drag can
// arrive as application/octet-stream, so the extension is the backstop.
func normalizeMime(filename, mimeType string) string {
	t := strings.ToLower(strings.TrimSpace(mimeType))
	if t == "" || t == "application/octet-stream" {
		switch strings.ToLower(filepath.Ext(filename)) {
		case ".png":
			return "image/png"
		case ".jpg", ".jpeg":
			return "image/jpeg"
		case ".gif":
			return "image/gif"
		case ".webp":
			return "image/webp"
		case ".bmp":
			return "image/bmp"
		case ".pdf":
			return "application/pdf"
		case ".doc":
			return "application/msword"
		case ".docx":
			return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
		case ".txt":
			return "text/plain"
		case ".md", ".markdown":
			return "text/markdown"
		}
	}
	return t
}

// validateMime reports the attachment kind for a mime, or an error when the
// type is not one the agent can consume.
func validateMime(mimeType string) (string, *Shared.ActionError) {
	kind, ok := allowedMime[mimeType]
	if !ok {
		return "", Shared.NewError(Shared.CodeBadInput,
			"Unsupported file type. You can attach images (png, jpg, gif, webp, bmp) or documents (pdf, docx, doc, txt, md).")
	}
	return kind, nil
}

// canonicalExt is the safe extension stored with the file, derived from the
// mime rather than the user's filename so a disguised payload does not pick
// its own path suffix.
func canonicalExt(mimeType string) string {
	switch mimeType {
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/bmp":
		return ".bmp"
	case "application/pdf":
		return ".pdf"
	case "application/msword":
		return ".doc"
	case "application/vnd.openxmlformats-officedocument.wordprocessingml.document":
		return ".docx"
	case "text/markdown":
		return ".md"
	default:
		if exts, _ := mime.ExtensionsByType(mimeType); len(exts) > 0 {
			return exts[0]
		}
		return ".bin"
	}
}

// describe sizes and names a file for the error a refusal returns.
func describe(filename string, size int64) string {
	return fmt.Sprintf("%s (%d bytes)", filename, size)
}

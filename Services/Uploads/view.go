package Uploads

import (
	"encoding/base64"
	"os"
	"strings"

	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
)

// attachmentView builds the wire shape of an attachment. withData adds the
// file's bytes as a data URL for images so the chat can render a thumbnail;
// document bytes are never shipped over the bridge.
func attachmentView(a Storage.Attachment, withData bool) Shared.AttachmentView {
	v := Shared.AttachmentView{
		ID:        a.ID,
		AgentID:   a.AgentID,
		Filename:  a.Filename,
		MimeType:  a.MimeType,
		SizeBytes: a.SizeBytes,
		Kind:      kindOf(a.MimeType),
		CreatedAt: a.CreatedAt,
	}
	if withData {
		if raw, err := os.ReadFile(a.DataPath); err == nil {
			v.DataURL = "data:" + a.MimeType + ";base64," + base64.StdEncoding.EncodeToString(raw)
		}
	}
	return v
}

// kindOf names whether a mime is an image (vision block) or a document
// (extracted text).
func kindOf(mime string) string {
	if strings.HasPrefix(mime, "image/") {
		return "image"
	}
	return "document"
}

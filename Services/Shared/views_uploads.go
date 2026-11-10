package Shared

// AttachmentView is the wire shape of an uploaded file. DataURL carries the
// file's bytes base64 — present so the chat can render an image thumbmail; the
// backend never persists it (bytes live on disk), it is rebuilt on request.
type AttachmentView struct {
	ID        string `json:"id"`
	AgentID   string `json:"agent_id"`
	Filename  string `json:"filename"`
	MimeType  string `json:"mime_type"`
	SizeBytes int64  `json:"size_bytes"`
	// Kind is "image" or "document": images are sent to the model as vision
	// blocks, documents as extracted text.
	Kind      string `json:"kind"`
	DataURL   string `json:"data_url,omitempty"`
	CreatedAt int64  `json:"created_at"`
}

// AttachmentResult is the reply to one upload or delete. A refused upload
// (bad type, too large) fails with Error rather than a row: nothing was stored.
type AttachmentResult struct {
	Attachment *AttachmentView `json:"attachment,omitempty"`
	Error      *ActionError    `json:"error,omitempty"`
}

type AttachmentsResult struct {
	Attachments []AttachmentView `json:"attachments"`
	Error       *ActionError     `json:"error,omitempty"`
}

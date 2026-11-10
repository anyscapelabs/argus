// The uploads service: files the user drags, pastes or picks into a chat.
//
// The pipeline is deliberately small: validate the type and size, stash the
// bytes on disk, extract the text a document contributes to the model, and hand
// back a row the chat send path can fold into a message. Extraction happens at
// upload time — not at send time — so the same file can be replayed into history
// without re-parsing it on every turn.
package Uploads

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
)

type Service struct {
	Core *Shared.Core
}

func (s *Service) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	return s.Core.Bootstrap(ctx)
}

func (s *Service) ServiceShutdown() error { return nil }

// maxExtractedTextChars caps what a document's text may cost the agent's
// context window. A 200-page PDF's full text is not something one message should
// carry; the head is plenty for a follow-up, and truncation is announced.
const maxExtractedTextChars = 30000

// Upload accepts one file's bytes (base64, as a browser hands them over),
// stores them, extracts any document text, and returns the attachment's
// metadata. The user's message is NOT written here: the chat send path decides
// how the attachment becomes part of a turn, so an upload can be attached,
// detached, or left unused without leaving a half-made message behind.
func (s *Service) Upload(agentID, filename, mimeType, base64Data string) Shared.AttachmentResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.AttachmentResult{Error: aerr}
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		agentID = "default"
	}

	data, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return Shared.AttachmentResult{Error: Shared.NewError(Shared.CodeBadInput, "The uploaded file could not be read")}
	}
	if len(data) > MaxUploadBytes {
		return Shared.AttachmentResult{Error: Shared.NewError(Shared.CodeBadInput,
			"Attachment is too large. The limit is 10 MB; this one is "+describe(filename, int64(len(data)))+".")}
	}
	if len(data) == 0 {
		return Shared.AttachmentResult{Error: Shared.NewError(Shared.CodeBadInput, "The uploaded file is empty")}
	}

	mime := normalizeMime(filename, mimeType)
	kind, merr := validateMime(mime)
	if merr != nil {
		return Shared.AttachmentResult{Error: merr}
	}

	dir, err := Storage.UploadsDir()
	if err != nil {
		return Shared.AttachmentResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	id, _ := uuid.NewV7()
	path := filepath.Join(dir, id.String()+canonicalExt(mime))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return Shared.AttachmentResult{Error: Shared.NewError(Shared.CodeStorage, "Could not store the uploaded file: "+err.Error())}
	}

	extracted := ""
	if kind == "document" {
		text, xerr := extractText(mime, data)
		if xerr != nil {
			// A document that cannot be read is still attached; the agent just
			// gets nothing to read from it.
			text = ""
			_ = xerr
		}
		extracted = truncate(text, maxExtractedTextChars)
	}

	att := Storage.Attachment{
		ID: id.String(), AgentID: agentID, Filename: filename,
		MimeType: mime, SizeBytes: int64(len(data)),
		DataPath: path, ExtractedText: extracted,
	}
	if err := s.Core.DB.InsertAttachment(att); err != nil {
		_ = os.Remove(path)
		return Shared.AttachmentResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	view := attachmentView(att, true)
	return Shared.AttachmentResult{Attachment: &view}
}

// Delete removes one attachment and its file.
func (s *Service) Delete(attachmentID string) Shared.ActionResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.ActionResult{Error: aerr}
	}
	id := strings.TrimSpace(attachmentID)
	if id == "" {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeBadInput, "no attachment was named")}
	}
	if err := s.Core.DB.DeleteAttachment(id); err != nil {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	return Shared.ActionResult{Ok: true}
}

// List returns the agent's uploads, with the bytes of images included so the
// chat can draw thumbnails. Document bytes stay off the wire.
func (s *Service) List(agentID string) Shared.AttachmentsResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.AttachmentsResult{Error: aerr}
	}
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		agentID = "default"
	}
	rows, err := s.Core.DB.ListAttachments(agentID)
	if err != nil {
		return Shared.AttachmentsResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	out := make([]Shared.AttachmentView, 0, len(rows))
	for _, a := range rows {
		out = append(out, attachmentView(a, strings.HasPrefix(a.MimeType, "image/")))
	}
	return Shared.AttachmentsResult{Attachments: out}
}

// truncate shortens text with an explicit notice, so a cut document never reads
// as the whole thing.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "\n\n[…content truncated: the file continues past this point]"
}

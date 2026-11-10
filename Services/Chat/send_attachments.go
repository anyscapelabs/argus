package Chat

import (
	"encoding/base64"
	"os"
	"strings"

	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Router"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
)

// attachmentBlocks turns uploaded files into the pieces of a user message. An
// image becomes a vision block (its bytes read from disk, base64); a document
// becomes a fenced excerpt of its extracted text, headed by the file's name.
// The excerpt travels only in the blocks — what the model reads, live and on
// history replay — never in the message's text: the transcript shows a file
// chip for the attachment, not the file's contents dumped into the chat.
func (s *Service) attachmentBlocks(agentID string, ids []string) ([]Protocol.Block, *Shared.ActionError) {
	if len(ids) == 0 {
		return nil, nil
	}
	var blocks []Protocol.Block
	for _, id := range ids {
		att, found, err := s.Core.DB.GetAttachment(strings.TrimSpace(id))
		if err != nil {
			return nil, Shared.NewError(Shared.CodeStorage, err.Error())
		}
		if !found {
			return nil, Shared.NewError(Shared.CodeBadInput, "One of the attached files is no longer available. Re-upload it and send again.")
		}
		if att.AgentID != agentID {
			return nil, Shared.NewError(Shared.CodeBadInput, "An attached file belongs to a different agent.")
		}
		if strings.HasPrefix(att.MimeType, "image/") {
			img, ok := imageBlock(att)
			if !ok {
				return nil, Shared.NewError(Shared.CodeStorage, "Could not read the image "+att.Filename)
			}
			blocks = append(blocks, img)
		} else {
			excerpt := strings.TrimSpace(att.ExtractedText)
			if excerpt == "" {
				excerpt = "[This file's text could not be read.]"
			}
			header := "[Attached file: " + att.Filename + "]\n"
			blocks = append(blocks, Protocol.Block{Type: Protocol.BlockText, Text: header + excerpt})
		}
	}
	return blocks, nil
}

// scrubImagesForNonVision walks the request's messages and replaces any image
// block with a text placeholder when the target model cannot see images. Sending
// a raw image to a text-only model is a wire-format error on some families, so
// the honest degradation is to tell the model an image was attached and let it
// ask or proceed without it. The persisted message is untouched — only the copy
// that goes over the wire changes.
func scrubImagesForNonVision(messages []Protocol.Message, target *Protocol.Target) []Protocol.Message {
	if target == nil {
		return messages
	}
	ad, err := Router.AdapterFor(target)
	if err != nil {
		return messages
	}
	if ad.Capabilities(Protocol.ModelInfo{ID: target.ModelID}).Vision {
		return messages
	}
	out := make([]Protocol.Message, 0, len(messages))
	for _, m := range messages {
		changed := false
		blocks := make([]Protocol.Block, 0, len(m.Blocks))
		for _, b := range m.Blocks {
			if b.Type == Protocol.BlockImage {
				blocks = append(blocks, Protocol.Block{
					Type: Protocol.BlockText,
					Text: "[The user attached an image, but this model cannot see images. If their request needs the image's contents, ask them to describe it or switch to a vision-capable model.]",
				})
				changed = true
				continue
			}
			blocks = append(blocks, b)
		}
		if changed {
			m.Blocks = blocks
		}
		out = append(out, m)
	}
	return out
}

// imageBlock loads one image's bytes and wraps them as a vision block. The data
// is base64-encoded — every adapter (OpenAI, Anthropic, Gemini) expects the
// wire value in base64 — and the media type travels with it.
func imageBlock(att Storage.Attachment) (Protocol.Block, bool) {
	raw, err := os.ReadFile(att.DataPath)
	if err != nil {
		return Protocol.Block{}, false
	}
	return Protocol.Block{
		Type: Protocol.BlockImage,
		Image: &Protocol.Image{
			MediaType: att.MimeType,
			Data:      base64.StdEncoding.EncodeToString(raw),
		},
	}, true
}

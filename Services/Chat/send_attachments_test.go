package Chat

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anyscapelabs/argus/Protocol"
	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
)

func TestImageBlockEncodesBase64(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "img.png")
	if err := os.WriteFile(path, []byte("raw-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	att := Storage.Attachment{MimeType: "image/png", DataPath: path}
	blk, ok := imageBlock(att)
	if !ok {
		t.Fatal("imageBlock failed")
	}
	if blk.Type != Protocol.BlockImage || blk.Image == nil {
		t.Fatalf("expected image block, got %+v", blk)
	}
	if blk.Image.Data != base64.StdEncoding.EncodeToString([]byte("raw-bytes")) {
		t.Fatalf("image data should be base64, got %q", blk.Image.Data)
	}
	if blk.Image.MediaType != "image/png" {
		t.Fatalf("media type: %q", blk.Image.MediaType)
	}
}

func TestScrubImagesForNonVisionNilTargetIsIdentity(t *testing.T) {
	in := []Protocol.Message{{Role: Protocol.RoleUser, Blocks: []Protocol.Block{
		{Type: Protocol.BlockImage, Image: &Protocol.Image{MediaType: "image/png", Data: "x"}},
	}}}
	out := scrubImagesForNonVision(in, nil)
	if len(out) != 1 || out[0].Blocks[0].Type != Protocol.BlockImage {
		t.Fatalf("nil target should leave images alone, got %+v", out)
	}
}

func TestAttachmentBlocksDocumentAddsHeader(t *testing.T) {
	db, err := Storage.Open(filepath.Join(t.TempDir(), "chat.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if err := db.SaveAgentProfile(Storage.AgentProfile{ID: "default", AgentName: "Argus"}); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "doc.txt")
	if err := os.WriteFile(path, []byte("ignored"), 0o600); err != nil {
		t.Fatal(err)
	}
	att := Storage.Attachment{
		ID: "a1", AgentID: "default", Filename: "notes.txt", MimeType: "text/plain",
		DataPath: path, ExtractedText: "the file body",
	}
	if err := db.InsertAttachment(att); err != nil {
		t.Fatal(err)
	}

	s := &Service{Core: &Shared.Core{DB: db}}
	blocks, aerr := s.attachmentBlocks("default", []string{"a1"})
	if aerr != nil {
		t.Fatal(aerr)
	}
	if len(blocks) != 1 || blocks[0].Type != Protocol.BlockText {
		t.Fatalf("expected one text block, got %+v", blocks)
	}
	if !strings.Contains(blocks[0].Text, "[Attached file: notes.txt]") {
		t.Fatalf("missing header in block: %q", blocks[0].Text)
	}
	if !strings.Contains(blocks[0].Text, "the file body") {
		t.Fatalf("missing body text: %q", blocks[0].Text)
	}
}

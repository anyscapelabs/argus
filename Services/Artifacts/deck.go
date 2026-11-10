package Artifacts

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Services/Slides"
	"github.com/anyscapelabs/argus/Storage"
)

func nowUnixMs() int64 { return time.Now().UnixMilli() }

func trim(s string) string { return strings.TrimSpace(s) }

// SaveDeck stores a presentation the agent authored. The deck JSON is validated
// by the slides engine, rendered to a .pptx through LibreOffice, and recorded as
// an artifact of kind "deck" whose stored body is the deck JSON and whose file
// path points at the generated PowerPoint. The pptx is produced up front so the
// user can save it later without the conversion happening again on demand.
func (s *Service) SaveDeck(agentID, deckJSON string) Shared.DeckWriteResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.DeckWriteResult{Error: aerr}
	}
	deck, err := Slides.Parse(deckJSON)
	if err != nil {
		return Shared.DeckWriteResult{Error: Shared.NewError(Shared.CodeBadInput, err.Error())}
	}
	// Re-marshal the validated deck so the stored body is canonical — themes
	// normalised, layouts resolved — rather than whatever the model typed.
	body, err := json.Marshal(deck)
	if err != nil {
		return Shared.DeckWriteResult{Error: Shared.NewError(Shared.CodeBadInput, "the deck could not be stored")}
	}

	pptx, err := Slides.ToPPTX(deck)
	if err != nil {
		// The deck is still worth keeping even if the PowerPoint cannot be made:
		// store it with no file and let the frontend show the slides. The message
		// below tells the caller the pptx could not be made.
		return s.storeDeck(resolveAgent(agentID), string(body), "", err.Error())
	}

	dir, derr := DeckDir()
	if derr != nil {
		return Shared.DeckWriteResult{Error: Shared.NewError(Shared.CodeStorage, derr.Error())}
	}
	id, _ := uuid.NewV7()
	path := filepath.Join(dir, id.String()+".pptx")
	if err := os.WriteFile(path, pptx, 0o600); err != nil {
		return Shared.DeckWriteResult{Error: Shared.NewError(Shared.CodeStorage, "could not store the deck: "+err.Error())}
	}
	return s.storeDeck(resolveAgent(agentID), string(body), path, "")
}

// storeDeck inserts the artifact row and, when a pptx exists, returns it. The
// errNote is surfaced to the caller when LibreOffice was unavailable, so the
// agent can tell the user why only slides, not a file, were produced.
func (s *Service) storeDeck(agentID, body, pptxPath, errNote string) Shared.DeckWriteResult {
	newID, _ := uuid.NewV7()
	now := nowUnixMs()
	stored := Storage.Artifact{
		ID: newID.String(), AgentID: agentID, Title: Slides.TitleOf(body),
		Format: Storage.ArtifactFormat, Kind: Storage.ArtifactKindDeck,
		Body: body, Meta: "{}", Words: Storage.CountWords(body), Bytes: len(body),
		Version: 1, FilePath: pptxPath, CreatedAt: now, UpdatedAt: now,
	}
	if err := s.Core.DB.InsertArtifact(stored); err != nil {
		return Shared.DeckWriteResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	view := artifactView(stored, true)
	s.Core.EmitEvent(Shared.EventArtifactUpdated, nil)
	res := Shared.DeckWriteResult{Artifact: view, SlideCount: Slides.CountOf(body)}
	if errNote != "" {
		res.Error = Shared.NewError(Shared.CodeExport, "The deck was saved, but the PowerPoint could not be built: "+errNote)
	}
	return res
}

// ExportDeckPPTX writes the deck's generated PowerPoint where the user chose,
// re-rendering it if the stored file was lost. The consent is the save dialog.
func (s *Service) ExportDeckPPTX(artifactID, destPath string) Shared.ArtifactExportResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.ArtifactExportResult{Error: aerr}
	}
	id := trim(artifactID)
	if id == "" {
		return Shared.ArtifactExportResult{Error: Shared.NewError(Shared.CodeBadInput, "no deck was named")}
	}
	a, found, err := s.Core.DB.GetArtifact(id)
	if err != nil {
		return Shared.ArtifactExportResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	if !found {
		return Shared.ArtifactExportResult{Error: Shared.NewError(Shared.CodeNotFound, "no deck with that id")}
	}
	if a.Kind != Storage.ArtifactKindDeck {
		return Shared.ArtifactExportResult{Error: Shared.NewError(Shared.CodeBadInput, "that document is not a slide deck")}
	}
	data, err := s.deckPPTXBytes(a)
	if err != nil {
		return Shared.ArtifactExportResult{Error: Shared.NewError(Shared.CodeExport, err.Error())}
	}
	path, err := expandHome(trim(destPath))
	if err != nil {
		return Shared.ArtifactExportResult{Error: Shared.NewError(Shared.CodeBadInput, err.Error())}
	}
	if filepath.Ext(path) == "" {
		path += ".pptx"
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return Shared.ArtifactExportResult{Error: Shared.NewError(Shared.CodeStorage, "could not create the folder for the export: "+err.Error())}
		}
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return Shared.ArtifactExportResult{Error: Shared.NewError(Shared.CodeStorage, "could not write the deck: "+err.Error())}
	}
	return Shared.ArtifactExportResult{Path: path, Format: "pptx", Bytes: len(data)}
}

// deckPPTXBytes returns the deck's PowerPoint, re-rendering it if the stored
// file is missing.
func (s *Service) deckPPTXBytes(a Storage.Artifact) ([]byte, error) {
	if a.FilePath != "" {
		if data, err := os.ReadFile(a.FilePath); err == nil && len(data) > 0 {
			return data, nil
		}
	}
	deck, err := Slides.Parse(a.Body)
	if err != nil {
		return nil, fmt.Errorf("the deck could not be read: %w", err)
	}
	return Slides.ToPPTX(deck)
}

// DeckDir is where generated PowerPoint files live, next to the database.
func DeckDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "argus", "decks")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

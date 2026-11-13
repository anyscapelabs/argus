// The artifact service: what the agent and the frontend are allowed to ask of a
// document.
//
// The write path is shared on purpose. write_artifact (the agent) and the panel
// (the user) both arrive here, so a document can only ever be stored after the
// validator has passed it — there is no second door into the table that skips
// the rules the exporters depend on.
package Artifacts

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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

// ServiceShutdown takes no context: Wails only calls the no-argument shape of
// this hook, and a variant that accepts one is silently never run.
func (s *Service) ServiceShutdown() error { return nil }

// resolveAgent mirrors the chat service: an empty agent id means the one agent
// this app talks to today.
func resolveAgent(agentID string) string {
	if id := strings.TrimSpace(agentID); id != "" {
		return id
	}
	return "default"
}

// EncodeMeta stores the validated front matter as the JSON object the artifacts
// table holds. An empty map becomes an empty object, never an empty string, so
// a read never has to guess what missing metadata meant.
func EncodeMeta(meta map[string]string) string {
	if len(meta) == 0 {
		return "{}"
	}
	out, err := json.Marshal(meta)
	if err != nil {
		return "{}"
	}
	return string(out)
}

// DecodeMeta reads that object back. Unparseable metadata decodes to an empty
// map: a document still has to open and export.
func DecodeMeta(raw string) map[string]string {
	if strings.TrimSpace(raw) == "" {
		return map[string]string{}
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return map[string]string{}
	}
	return m
}

// Save validates and stores one document, creating it or replacing the body of
// an existing one. It is the only writer of the artifacts table.
//
// An append adds to the end of the current body rather than replacing it, which
// is what lets a long report arrive in the pieces the model can send per
// response — the validator still sees the whole document, so a table split
// across two appends is still checked as one table.
func (s *Service) Save(agentID, artifactID, title, body string, appendBody bool) Shared.ArtifactWriteResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.ArtifactWriteResult{Error: aerr}
	}
	res := save(s.Core.DB, agentID, artifactID, title, body, appendBody)
	if res.Artifact.ID != "" {
		// Only a document that actually landed announces itself; a refusal or a
		// bad id leaves the panel exactly as it was.
		s.Core.EmitEvent(Shared.EventArtifactUpdated, nil)
	}
	return res
}

// save is the whole write path against a database, with no app core in sight —
// which is what lets the rules be tested on a real SQLite file.
func save(db *Storage.DB, agentID, artifactID, title, body string, appendBody bool) Shared.ArtifactWriteResult {
	agent := resolveAgent(agentID)
	id := strings.TrimSpace(artifactID)

	var existing *Storage.Artifact
	if id != "" {
		current, found, err := db.GetArtifact(id)
		if err != nil {
			return Shared.ArtifactWriteResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
		}
		if !found {
			return Shared.ArtifactWriteResult{Error: Shared.NewError(Shared.CodeNotFound,
				fmt.Sprintf("no document with id %q. Write without an id to start a new one.", id))}
		}
		existing = &current
	}

	combined := body
	if appendBody {
		switch {
		case existing == nil:
			return Shared.ArtifactWriteResult{Error: Shared.NewError(Shared.CodeBadInput,
				"append needs the artifact_id of the document to extend")}
		default:
			combined = strings.TrimRight(existing.Body, "\n") + "\n\n" + strings.TrimLeft(body, "\n")
		}
	}
	if len(combined) > Storage.MaxArtifactBytes {
		return Shared.ArtifactWriteResult{Error: Shared.NewError(Shared.CodeBadInput, fmt.Sprintf(
			"the document would be %d bytes and the limit is %d. Write it in separate documents, or shorten it.",
			len(combined), Storage.MaxArtifactBytes))}
	}

	res := Validate(combined)
	if res.HasErrors() {
		// Nothing was stored. The report is the tool result, so the model gets
		// its line numbers and the exact rule it broke in the same turn.
		return Shared.ArtifactWriteResult{Rejected: true, Findings: res.Report()}
	}

	name := res.Title
	if appendBody && existing != nil {
		name = existing.Title
	}
	if name == "" {
		name = strings.TrimSpace(title)
	}
	if name == "" {
		// The document's own heading beats the label it had before: a rewrite
		// that renames the title on page would otherwise be filed under the old
		// one.
		name = firstHeading(res.Body)
	}
	if name == "" && existing != nil {
		name = existing.Title
	}
	if name == "" {
		name = "Untitled document"
	}

	meta := EncodeMeta(res.FrontMatter)
	if appendBody && existing != nil {
		meta = existing.Meta
	}
	var stored Storage.Artifact
	if existing != nil {
		updated, err := db.UpdateArtifact(existing.ID, name, res.Body, meta, 0)
		if err != nil {
			return Shared.ArtifactWriteResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
		}
		stored = updated
	} else {
		newID, _ := uuid.NewV7()
		now := time.Now().UnixMilli()
		stored = Storage.Artifact{
			ID: newID.String(), AgentID: agent, Title: name,
			Format: Storage.ArtifactFormat, Body: res.Body, Meta: meta,
			Words: Storage.CountWords(res.Body), Bytes: len(res.Body), Version: 1,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := db.InsertArtifact(stored); err != nil {
			return Shared.ArtifactWriteResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
		}
	}

	warnings := warningReport(res)
	return Shared.ArtifactWriteResult{Artifact: artifactView(stored, true), Findings: warnings}
}

// warningReport renders the findings that did not block the write. A warning is
// still worth telling the writer about — a remote image is the sort of thing that
// only shows up as a hole in the Word file — but it must not read as a refusal.
func warningReport(res Result) string {
	var kept []string
	for _, d := range res.Diagnostics {
		if d.Severity == SeverityWarning {
			kept = append(kept, fmt.Sprintf("note at line %d: %s", d.Line, d.Message))
		}
	}
	return strings.Join(kept, "\n")
}

// firstHeading is the fallback title: the document's own top-level heading,
// without the # marks.
func firstHeading(body string) string {
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		level := 0
		for level < len(t) && level < MaxHeadingLevel && t[level] == '#' {
			level++
		}
		if level > 0 && level < len(t) && (t[level] == ' ' || t[level] == '\t') {
			return strings.TrimSpace(t[level:])
		}
	}
	return ""
}

// List returns the agent's documents without their bodies; Get returns one with.
func (s *Service) List(agentID string) Shared.ArtifactsResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.ArtifactsResult{Error: aerr}
	}
	rows, err := s.Core.DB.ListArtifacts(resolveAgent(agentID), 0)
	if err != nil {
		return Shared.ArtifactsResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	out := make([]Shared.ArtifactView, 0, len(rows))
	for _, a := range rows {
		out = append(out, artifactView(a, false))
	}
	return Shared.ArtifactsResult{Artifacts: out}
}

func (s *Service) Get(artifactID string) Shared.ArtifactResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.ArtifactResult{Error: aerr}
	}
	id := strings.TrimSpace(artifactID)
	if id == "" {
		return Shared.ArtifactResult{Error: Shared.NewError(Shared.CodeBadInput, "no document was named")}
	}
	a, found, err := s.Core.DB.GetArtifact(id)
	if err != nil {
		return Shared.ArtifactResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	if !found {
		return Shared.ArtifactResult{Error: Shared.NewError(Shared.CodeNotFound, "no document with that id")}
	}
	return Shared.ArtifactResult{Artifact: artifactView(a, true), Found: true}
}

// Delete removes a document and its superseded versions.
func (s *Service) Delete(artifactID string) Shared.ActionResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.ActionResult{Error: aerr}
	}
	id := strings.TrimSpace(artifactID)
	if id == "" {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeBadInput, "no document was named")}
	}
	if _, found, err := s.Core.DB.GetArtifact(id); err != nil {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	} else if !found {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeNotFound, "no document with that id")}
	}
	if err := s.Core.DB.DeleteArtifact(id); err != nil {
		return Shared.ActionResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	s.Core.EmitEvent(Shared.EventArtifactUpdated, nil)
	return Shared.ActionResult{Ok: true}
}

// Formats is the export menu: every target, with the ones this machine cannot
// produce reported as unavailable and why.
func (s *Service) Formats() Shared.ExportFormatsResult {
	return Shared.ExportFormatsResult{Formats: Formats()}
}

// Export renders one document and writes it where the user chose. The path comes
// from the app's save dialog, so the consent is the click itself — no permission
// card is asked for a file the user just named.
func (s *Service) Export(artifactID, format, destPath string) Shared.ArtifactExportResult {
	if aerr := s.Core.Ready(); aerr != nil {
		return Shared.ArtifactExportResult{Error: aerr}
	}
	id := strings.TrimSpace(artifactID)
	if id == "" {
		return Shared.ArtifactExportResult{Error: Shared.NewError(Shared.CodeBadInput, "no document was named")}
	}
	a, found, err := s.Core.DB.GetArtifact(id)
	if err != nil {
		return Shared.ArtifactExportResult{Error: Shared.NewError(Shared.CodeStorage, err.Error())}
	}
	if !found {
		return Shared.ArtifactExportResult{Error: Shared.NewError(Shared.CodeNotFound, "no document with that id")}
	}
	data, ext, err := Export(a, format)
	if err != nil {
		return Shared.ArtifactExportResult{Error: Shared.NewError(Shared.CodeExport, err.Error())}
	}
	reported, _ := canonicalFormat(format)

	path, err := expandHome(strings.TrimSpace(destPath))
	if err != nil {
		return Shared.ArtifactExportResult{Error: Shared.NewError(Shared.CodeBadInput, err.Error())}
	}
	if filepath.Ext(path) == "" {
		path += "." + ext
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return Shared.ArtifactExportResult{Error: Shared.NewError(Shared.CodeStorage,
				"could not create the folder for the export: "+err.Error())}
		}
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return Shared.ArtifactExportResult{Error: Shared.NewError(Shared.CodeStorage,
			"could not write the exported file: "+err.Error())}
	}
	return Shared.ArtifactExportResult{Path: path, Format: reported, Bytes: len(data)}
}

// artifactView is the wire shape of a document. The body rides along only when
// withBody is set: a listing of forty reports should not carry forty documents.
func artifactView(a Storage.Artifact, withBody bool) Shared.ArtifactView {
	v := Shared.ArtifactView{
		ID:        a.ID,
		AgentID:   a.AgentID,
		Title:     a.Title,
		Format:    a.Format,
		Kind:      a.Kind,
		Meta:      DecodeMeta(a.Meta),
		Words:     a.Words,
		Bytes:     a.Bytes,
		Version:   a.Version,
		CreatedAt: a.CreatedAt,
		UpdatedAt: a.UpdatedAt,
	}
	if withBody {
		v.Body = a.Body
	}
	return v
}

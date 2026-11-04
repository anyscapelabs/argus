package Storage

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// ArtifactFormat is the canonical authoring format. There is one, by design:
// the agent writes Markdown and every other file type is derived from that
// body by the export pipeline, never authored directly.
const ArtifactFormat = "markdown"

const (
	// MaxArtifactBytes caps one artifact body. It matches the filesystem
	// tool's write cap so the two paths cannot disagree about what is too big.
	MaxArtifactBytes = 1 << 20
	// MaxArtifactVersions is how many previous bodies survive an edit. Enough
	// to step back through a document's drafts, small enough that a chatty run
	// cannot grow the table without bound.
	MaxArtifactVersions = 20
)

// Artifact is one document the agent wrote. Body is the canonical Markdown;
// Version counts whole rewrites, and Words is stored rather than recomputed
// because the transcript and the panel both show it. Meta is the document's
// front matter as a JSON object — the metadata the exporters honour and the
// body cannot carry.
type Artifact struct {
	ID        string
	AgentID   string
	Title     string
	Format    string
	Body      string
	Meta      string
	Words     int
	Bytes     int
	Version   int
	CreatedAt int64
	UpdatedAt int64
}

// ArtifactVersion is one superseded body, kept so an edit can be undone.
type ArtifactVersion struct {
	ID         string
	ArtifactID string
	Version    int
	Title      string
	Body       string
	Words      int
	CreatedAt  int64
}

// CountWords is the artifact's length in words as the transcript reports it.
// Whitespace-separated is all a document preview needs, and doing it here keeps
// the number the panel shows equal to the number the agent was told.
func CountWords(body string) int {
	return len(strings.Fields(body))
}

func checkArtifactBody(a Artifact) error {
	if a.ID == "" {
		return fmt.Errorf("storage: artifact id is required")
	}
	if a.AgentID == "" {
		return fmt.Errorf("storage: artifact agent id is required")
	}
	if len(a.Body) > MaxArtifactBytes {
		return fmt.Errorf("storage: artifact body is %d bytes, over the %d byte cap", len(a.Body), MaxArtifactBytes)
	}
	return nil
}

func (db *DB) InsertArtifact(a Artifact) error {
	if err := checkArtifactBody(a); err != nil {
		return err
	}
	if a.CreatedAt == 0 {
		a.CreatedAt = nowUnixMs()
	}
	a.UpdatedAt = a.CreatedAt
	if a.Format == "" {
		a.Format = ArtifactFormat
	}
	if strings.TrimSpace(a.Meta) == "" {
		a.Meta = "{}"
	}
	a.Words = CountWords(a.Body)
	a.Bytes = len(a.Body)
	if a.Version <= 0 {
		a.Version = 1
	}
	_, err := db.conn.Exec(
		`INSERT INTO artifacts (id, agent_id, title, format, body, meta, words, bytes, version, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.AgentID, a.Title, a.Format, a.Body, a.Meta, a.Words, a.Bytes, a.Version, a.CreatedAt, a.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("storage: artifact insert failed: %w", err)
	}
	return nil
}

const artifactColumns = `id, agent_id, title, format, body, meta, words, bytes, version, created_at, updated_at`

// rowScanner lets scanArtifact serve both QueryRow and Rows without the call
// sites knowing which one they hold.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanArtifact(s rowScanner, dest *Artifact) error {
	return s.Scan(&dest.ID, &dest.AgentID, &dest.Title, &dest.Format, &dest.Body, &dest.Meta,
		&dest.Words, &dest.Bytes, &dest.Version, &dest.CreatedAt, &dest.UpdatedAt)
}

func (db *DB) GetArtifact(id string) (Artifact, bool, error) {
	row := db.conn.QueryRow(`SELECT `+artifactColumns+` FROM artifacts WHERE id = ?`, id)
	var a Artifact
	if err := scanArtifact(row, &a); err != nil {
		if err == sql.ErrNoRows {
			return Artifact{}, false, nil
		}
		return Artifact{}, false, fmt.Errorf("storage: artifact get failed: %w", err)
	}
	return a, true, nil
}

// ListArtifacts returns the agent's documents newest first, **without** the
// body: a panel listing a hundred reports should not drag a hundred documents
// through SQLite. Use GetArtifact for the text.
func (db *DB) ListArtifacts(agentID string, limit int) ([]Artifact, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := db.conn.Query(
		`SELECT id, agent_id, title, format, '', meta, words, bytes, version, created_at, updated_at
		 FROM artifacts WHERE agent_id = ? ORDER BY updated_at DESC, id DESC LIMIT ?`, agentID, limit)
	if err != nil {
		return nil, fmt.Errorf("storage: artifact listing failed: %w", err)
	}
	defer rows.Close()
	var out []Artifact
	for rows.Next() {
		var a Artifact
		if err := scanArtifact(rows, &a); err != nil {
			return nil, fmt.Errorf("storage: artifact scan failed: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// UpdateArtifact replaces the body and bumps the version, snapshotting the
// body it replaced into artifact_versions first. The snapshot happens in the
// same call so an edit can never land without its undo record. An empty title
// or meta means "unchanged", not "cleared".
func (db *DB) UpdateArtifact(id, title, body, meta string, updatedAt int64) (Artifact, error) {
	if len(body) > MaxArtifactBytes {
		return Artifact{}, fmt.Errorf("storage: artifact body is %d bytes, over the %d byte cap", len(body), MaxArtifactBytes)
	}
	current, found, err := db.GetArtifact(id)
	if err != nil {
		return Artifact{}, err
	}
	if !found {
		return Artifact{}, fmt.Errorf("storage: artifact %s not found", id)
	}
	if updatedAt == 0 {
		updatedAt = nowUnixMs()
	}
	if title == "" {
		title = current.Title
	}
	if strings.TrimSpace(meta) == "" {
		meta = current.Meta
	}
	if meta == "" {
		meta = "{}"
	}
	newVersion := current.Version + 1

	vid, _ := uuid.NewV7()
	_, err = db.conn.Exec(
		`INSERT INTO artifact_versions (id, artifact_id, version, title, body, words, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		vid, current.ID, current.Version, current.Title, current.Body, current.Words, updatedAt,
	)
	if err != nil {
		return Artifact{}, fmt.Errorf("storage: artifact version snapshot failed: %w", err)
	}

	res, err := db.conn.Exec(
		`UPDATE artifacts SET title = ?, body = ?, meta = ?, words = ?, bytes = ?, version = ?, updated_at = ?
		 WHERE id = ? AND version = ?`,
		title, body, meta, CountWords(body), len(body), newVersion, updatedAt, id, current.Version,
	)
	if err != nil {
		return Artifact{}, fmt.Errorf("storage: artifact update failed: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Artifact{}, fmt.Errorf("storage: artifact %s changed underneath the update", id)
	}

	if err := db.PruneArtifactVersions(id, MaxArtifactVersions); err != nil {
		return Artifact{}, err
	}
	current.Title = title
	current.Body = body
	current.Meta = meta
	current.Words = CountWords(body)
	current.Bytes = len(body)
	current.Version = newVersion
	current.UpdatedAt = updatedAt
	return current, nil
}

// PruneArtifactVersions keeps the newest n snapshots of one artifact.
func (db *DB) PruneArtifactVersions(artifactID string, n int) error {
	if n < 0 {
		n = 0
	}
	_, err := db.conn.Exec(
		`DELETE FROM artifact_versions
		 WHERE artifact_id = ? AND id NOT IN (
			SELECT id FROM artifact_versions WHERE artifact_id = ? ORDER BY version DESC LIMIT ?
		 )`, artifactID, artifactID, n)
	if err != nil {
		return fmt.Errorf("storage: artifact version prune failed: %w", err)
	}
	return nil
}

// ListArtifactVersions returns the snapshots newest first, without bodies.
func (db *DB) ListArtifactVersions(artifactID string) ([]ArtifactVersion, error) {
	rows, err := db.conn.Query(
		`SELECT id, artifact_id, version, title, '', words, created_at
		 FROM artifact_versions WHERE artifact_id = ? ORDER BY version DESC`, artifactID)
	if err != nil {
		return nil, fmt.Errorf("storage: artifact version listing failed: %w", err)
	}
	defer rows.Close()
	var out []ArtifactVersion
	for rows.Next() {
		var v ArtifactVersion
		if err := rows.Scan(&v.ID, &v.ArtifactID, &v.Version, &v.Title, &v.Body, &v.Words, &v.CreatedAt); err != nil {
			return nil, fmt.Errorf("storage: artifact version scan failed: %w", err)
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// GetArtifactVersion fetches one snapshot with its body, for a restore.
func (db *DB) GetArtifactVersion(id string) (ArtifactVersion, bool, error) {
	row := db.conn.QueryRow(
		`SELECT id, artifact_id, version, title, body, words, created_at FROM artifact_versions WHERE id = ?`, id)
	var v ArtifactVersion
	err := row.Scan(&v.ID, &v.ArtifactID, &v.Version, &v.Title, &v.Body, &v.Words, &v.CreatedAt)
	if err == sql.ErrNoRows {
		return ArtifactVersion{}, false, nil
	}
	if err != nil {
		return ArtifactVersion{}, false, fmt.Errorf("storage: artifact version get failed: %w", err)
	}
	return v, true, nil
}

func (db *DB) DeleteArtifact(id string) error {
	if _, err := db.conn.Exec(`DELETE FROM artifact_versions WHERE artifact_id = ?`, id); err != nil {
		return fmt.Errorf("storage: artifact version delete failed: %w", err)
	}
	if _, err := db.conn.Exec(`DELETE FROM artifacts WHERE id = ?`, id); err != nil {
		return fmt.Errorf("storage: artifact delete failed: %w", err)
	}
	return nil
}

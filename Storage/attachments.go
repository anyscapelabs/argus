package Storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
)

// Attachment is the metadata for one uploaded file. The bytes themselves live on
// disk (data_path), never in this table; extracted_text is what the agent reads
// for a PDF or DOCX, kept so history can replay an upload without re-parsing the
// file on every turn.
type Attachment struct {
	ID            string
	AgentID       string
	Filename      string
	MimeType      string
	SizeBytes     int64
	DataPath      string
	ExtractedText string
	CreatedAt     int64
}

const attachmentColumns = `id, agent_id, filename, mime_type, size_bytes, data_path, extracted_text, created_at`

func scanAttachment(s rowScanner, dest *Attachment) error {
	return s.Scan(&dest.ID, &dest.AgentID, &dest.Filename, &dest.MimeType,
		&dest.SizeBytes, &dest.DataPath, &dest.ExtractedText, &dest.CreatedAt)
}

func (db *DB) InsertAttachment(a Attachment) error {
	if a.ID == "" {
		return fmt.Errorf("storage: attachment id is required")
	}
	if a.AgentID == "" {
		return fmt.Errorf("storage: attachment agent id is required")
	}
	if a.CreatedAt == 0 {
		a.CreatedAt = nowUnixMs()
	}
	_, err := db.conn.Exec(
		`INSERT INTO attachments (id, agent_id, filename, mime_type, size_bytes, data_path, extracted_text, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		a.ID, a.AgentID, a.Filename, a.MimeType, a.SizeBytes, a.DataPath, a.ExtractedText, a.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("storage: attachment insert failed: %w", err)
	}
	return nil
}

func (db *DB) GetAttachment(id string) (Attachment, bool, error) {
	row := db.conn.QueryRow(`SELECT `+attachmentColumns+` FROM attachments WHERE id = ?`, id)
	var a Attachment
	if err := scanAttachment(row, &a); err != nil {
		if err == sql.ErrNoRows {
			return Attachment{}, false, nil
		}
		return Attachment{}, false, fmt.Errorf("storage: attachment get failed: %w", err)
	}
	return a, true, nil
}

// ListAttachments returns the agent's uploads oldest first, so a chat rebuild
// can order them the way the user attached them.
func (db *DB) ListAttachments(agentID string) ([]Attachment, error) {
	rows, err := db.conn.Query(
		`SELECT `+attachmentColumns+` FROM attachments WHERE agent_id = ? ORDER BY created_at ASC`, agentID)
	if err != nil {
		return nil, fmt.Errorf("storage: attachment listing failed: %w", err)
	}
	defer rows.Close()
	var out []Attachment
	for rows.Next() {
		var a Attachment
		if err := scanAttachment(rows, &a); err != nil {
			return nil, fmt.Errorf("storage: attachment scan failed: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// DeleteAttachment removes the row and, best effort, the file it points at.
func (db *DB) DeleteAttachment(id string) error {
	a, found, err := db.GetAttachment(id)
	if err != nil {
		return err
	}
	if found && a.DataPath != "" {
		_ = os.Remove(a.DataPath)
	}
	if _, err := db.conn.Exec(`DELETE FROM attachments WHERE id = ?`, id); err != nil {
		return fmt.Errorf("storage: attachment delete failed: %w", err)
	}
	return nil
}

// UploadsDir is where uploaded file bytes are stored, one directory next to the
// database so a user's uploads and their chat live and die together.
func UploadsDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "argus", "uploads")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

package Storage

import (
	"fmt"
)

type SelectedModel struct {
	ProviderID string
	ModelRef   string
	Position   int
	SelectedAt string
}

func (db *DB) SetProviderSelection(providerID string, refs []string) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("storage: begin transaction failed: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM provider_selected_models WHERE provider_id = ?`, providerID); err != nil {
		return fmt.Errorf("storage: provider selection reset failed: %w", err)
	}
	for i, ref := range refs {
		if _, err := tx.Exec(
			`INSERT INTO provider_selected_models (provider_id, model_ref, position, selected_at) VALUES (?, ?, ?, ?)`,
			providerID, ref, i, nowRFC3339(),
		); err != nil {
			return fmt.Errorf("storage: provider selection save failed: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("storage: commit failed: %w", err)
	}
	return nil
}

func (db *DB) ListProviderSelection(providerID string) ([]string, error) {
	rows, err := db.conn.Query(
		`SELECT model_ref FROM provider_selected_models WHERE provider_id = ? ORDER BY position`, providerID)
	if err != nil {
		return nil, fmt.Errorf("storage: provider selection listing failed: %w", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			return nil, fmt.Errorf("storage: provider selection scan failed: %w", err)
		}
		out = append(out, ref)
	}
	return out, rows.Err()
}

func (db *DB) ListAllSelections() ([]SelectedModel, error) {
	rows, err := db.conn.Query(
		`SELECT provider_id, model_ref, position, selected_at
		 FROM provider_selected_models ORDER BY provider_id, position`)
	if err != nil {
		return nil, fmt.Errorf("storage: provider selection listing failed: %w", err)
	}
	defer rows.Close()
	var out []SelectedModel
	for rows.Next() {
		var sm SelectedModel
		if err := rows.Scan(&sm.ProviderID, &sm.ModelRef, &sm.Position, &sm.SelectedAt); err != nil {
			return nil, fmt.Errorf("storage: provider selection scan failed: %w", err)
		}
		out = append(out, sm)
	}
	return out, rows.Err()
}

func (db *DB) ClearProviderSelection(providerID string) error {
	if _, err := db.conn.Exec(`DELETE FROM provider_selected_models WHERE provider_id = ?`, providerID); err != nil {
		return fmt.Errorf("storage: provider selection delete failed: %w", err)
	}
	return nil
}

func (db *DB) RemoveSelectedModelsForRef(ref string) error {
	if _, err := db.conn.Exec(`DELETE FROM provider_selected_models WHERE model_ref = ?`, ref); err != nil {
		return fmt.Errorf("storage: provider selection delete failed: %w", err)
	}
	return nil
}

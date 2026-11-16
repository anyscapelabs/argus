package Storage

import (
	"fmt"
	"strings"
)

type Slot struct {
	Slot string
	Ref  string
}

func (db *DB) SaveModelSlot(slot, ref string) error {
	_, err := db.conn.Exec(
		`INSERT INTO model_slots (slot, model_ref) VALUES (?, ?)
		 ON CONFLICT(slot) DO UPDATE SET model_ref = excluded.model_ref`,
		slot, ref,
	)
	if err != nil {
		return fmt.Errorf("storage: slot save failed: %w", err)
	}
	return nil
}

func (db *DB) DeleteModelSlot(slot string) error {
	if _, err := db.conn.Exec(`DELETE FROM model_slots WHERE slot = ?`, slot); err != nil {
		return fmt.Errorf("storage: slot delete failed: %w", err)
	}
	return nil
}

func (db *DB) ListModelSlots() ([]Slot, error) {
	rows, err := db.conn.Query(`SELECT slot, model_ref FROM model_slots`)
	if err != nil {
		return nil, fmt.Errorf("storage: slot listing failed: %w", err)
	}
	defer rows.Close()
	var out []Slot
	for rows.Next() {
		var s Slot
		if err := rows.Scan(&s.Slot, &s.Ref); err != nil {
			return nil, fmt.Errorf("storage: slot scan failed: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (db *DB) RemoveSlotForRef(ref string) (string, error) {
	rows, err := db.conn.Query(`SELECT slot FROM model_slots WHERE model_ref = ?`, ref)
	if err != nil {
		return "", fmt.Errorf("storage: slot lookup failed: %w", err)
	}
	var slots []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			rows.Close()
			return "", err
		}
		slots = append(slots, s)
	}
	rows.Close()
	if len(slots) == 0 {
		return "", nil
	}
	if _, err := db.conn.Exec(`DELETE FROM model_slots WHERE model_ref = ?`, ref); err != nil {
		return "", fmt.Errorf("storage: slot delete failed: %w", err)
	}
	return strings.Join(slots, ","), nil
}

func (db *DB) RemoveProviderSlots(providerID string) ([]string, error) {
	rows, err := db.conn.Query(`SELECT slot FROM model_slots WHERE model_ref LIKE ? ESCAPE '\'`, likePrefix(providerID)+"/%")
	if err != nil {
		return nil, fmt.Errorf("storage: slot lookup failed: %w", err)
	}
	var slots []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			rows.Close()
			return nil, fmt.Errorf("storage: slot scan failed: %w", err)
		}
		slots = append(slots, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, s := range slots {
		if _, err := db.conn.Exec(`DELETE FROM model_slots WHERE slot = ?`, s); err != nil {
			return nil, fmt.Errorf("storage: slot delete failed: %w", err)
		}
	}
	return slots, nil
}

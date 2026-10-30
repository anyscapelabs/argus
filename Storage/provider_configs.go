package Storage

import (
	"database/sql"
	"fmt"
)

type ProviderConfig struct {
	ProviderID      string
	SecretRef       string
	KeyLast4        string
	ValidatedAt     string
	BaseURLOverride string
}

func (db *DB) SaveProviderConfig(pc ProviderConfig) error {
	_, err := db.conn.Exec(
		`INSERT INTO provider_configs (provider_id, secret_ref, key_last4, validated_at, base_url_override)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(provider_id) DO UPDATE SET
		   secret_ref = excluded.secret_ref,
		   key_last4 = excluded.key_last4,
		   validated_at = excluded.validated_at,
		   base_url_override = excluded.base_url_override`,
		pc.ProviderID, pc.SecretRef, pc.KeyLast4, pc.ValidatedAt, nullString(pc.BaseURLOverride),
	)
	if err != nil {
		return fmt.Errorf("storage: provider config save failed: %w", err)
	}
	return nil
}

func (db *DB) GetProviderConfig(providerID string) (ProviderConfig, bool, error) {
	row := db.conn.QueryRow(
		`SELECT provider_id, secret_ref, key_last4, validated_at, base_url_override
		 FROM provider_configs WHERE provider_id = ?`, providerID)
	var pc ProviderConfig
	var override sql.NullString
	err := row.Scan(&pc.ProviderID, &pc.SecretRef, &pc.KeyLast4, &pc.ValidatedAt, &override)
	if err == sql.ErrNoRows {
		return ProviderConfig{}, false, nil
	}
	if err != nil {
		return ProviderConfig{}, false, fmt.Errorf("storage: provider config read failed: %w", err)
	}
	pc.BaseURLOverride = override.String
	return pc, true, nil
}

func (db *DB) ListProviderConfigs() ([]ProviderConfig, error) {
	rows, err := db.conn.Query(
		`SELECT provider_id, secret_ref, key_last4, validated_at, base_url_override FROM provider_configs`)
	if err != nil {
		return nil, fmt.Errorf("storage: provider config listing failed: %w", err)
	}
	defer rows.Close()
	var out []ProviderConfig
	for rows.Next() {
		var pc ProviderConfig
		var override sql.NullString
		if err := rows.Scan(&pc.ProviderID, &pc.SecretRef, &pc.KeyLast4, &pc.ValidatedAt, &override); err != nil {
			return nil, fmt.Errorf("storage: provider config scan failed: %w", err)
		}
		pc.BaseURLOverride = override.String
		out = append(out, pc)
	}
	return out, rows.Err()
}

func (db *DB) RemoveProviderConfig(providerID string) error {
	if _, err := db.conn.Exec(`DELETE FROM provider_configs WHERE provider_id = ?`, providerID); err != nil {
		return fmt.Errorf("storage: provider config delete failed: %w", err)
	}
	return nil
}

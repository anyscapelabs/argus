package Storage

import (
	"database/sql"
	"fmt"
	"time"
)

const accountEntitlementsID = "default"

func (db *DB) SaveAccountEntitlements(payload string, fetchedAt int64) error {
	if payload == "" {
		return fmt.Errorf("storage: account entitlements payload is required")
	}
	if fetchedAt == 0 {
		fetchedAt = time.Now().Unix()
	}
	_, err := db.conn.Exec(
		`INSERT INTO account_entitlements (id, payload, fetched_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   payload = excluded.payload,
		   fetched_at = excluded.fetched_at`,
		accountEntitlementsID, payload, fetchedAt,
	)
	if err != nil {
		return fmt.Errorf("storage: account entitlements save failed: %w", err)
	}
	return nil
}

func (db *DB) GetAccountEntitlements() (string, int64, bool, error) {
	row := db.conn.QueryRow(
		`SELECT payload, fetched_at FROM account_entitlements WHERE id = ?`,
		accountEntitlementsID,
	)
	var payload string
	var fetchedAt int64
	err := row.Scan(&payload, &fetchedAt)
	if err == sql.ErrNoRows {
		return "", 0, false, nil
	}
	if err != nil {
		return "", 0, false, fmt.Errorf("storage: account entitlements read failed: %w", err)
	}
	return payload, fetchedAt, true, nil
}

func (db *DB) DeleteAccountEntitlements() error {
	if _, err := db.conn.Exec(`DELETE FROM account_entitlements WHERE id = ?`, accountEntitlementsID); err != nil {
		return fmt.Errorf("storage: account entitlements delete failed: %w", err)
	}
	return nil
}


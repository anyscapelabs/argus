package Catalog

import (
	_ "embed"
	"encoding/json"
)

//go:embed snapshot/snapshot.json
var snapshotBody []byte

//go:embed snapshot/meta.json
var snapshotMetaBody []byte

type SnapshotMeta struct {
	FetchedAt   string `json:"fetched_at"`
	Placeholder bool   `json:"placeholder"`
}

// Embedded is retained for compatibility. The catalog is now built from the
// provider overlay only, so the embedded snapshot from the models.dev era is
// no longer used by NewStore. Kept so any external caller still compiles.
func Embedded() ([]byte, SnapshotMeta, error) {
	var meta SnapshotMeta
	if err := json.Unmarshal(snapshotMetaBody, &meta); err != nil {
		return nil, meta, err
	}
	return snapshotBody, meta, nil
}

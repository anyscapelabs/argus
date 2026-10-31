package Keyring

import (
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

const service = "argus"

// ErrNotFound is returned when the secrets store has no value for a ref.
// It is distinct from a broken secrets store so callers can tell
// "not configured yet" apart from "storage is failing".
var ErrNotFound = errors.New("no value stored for this key")

func Ref(providerID string) string {
	return "provider/" + providerID + "/api_key"
}

func Set(providerID, key string) error {
	if err := keyring.Set(service, Ref(providerID), key); err != nil {
		return fmt.Errorf("secrets store refused the save: %w", err)
	}
	return nil
}

func Get(providerID string) (string, error) {
	v, err := keyring.Get(service, Ref(providerID))
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("secrets store is unreachable: %w", err)
	}
	return v, nil
}

func Delete(providerID string) error {
	err := keyring.Delete(service, Ref(providerID))
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("secrets store refused the delete: %w", err)
	}
	return nil
}

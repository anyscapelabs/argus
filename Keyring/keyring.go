package Keyring

import (
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

const service = "argus"

// AccountKeyRef is the keyring ref for the argus_pk_ service-account key.
// The router provider authenticates with this key, not a per-provider one.
const AccountKeyRef = "account/key"

// KeyFor returns the secret ref a provider authenticates with. The single
// argus-router provider uses the account key; per-provider refs are kept for
// backward compatibility with pre-router installs.
func KeyFor(providerID string) string {
	if providerID == "argus-router" {
		return AccountKeyRef
	}
	return Ref(providerID)
}

// ErrNotFound is returned when the secrets store has no value for a ref.
// It is distinct from a broken secrets store so callers can tell
// "not configured yet" apart from "storage is failing".
var ErrNotFound = errors.New("no value stored for this key")

func Ref(providerID string) string {
	return "provider/" + providerID + "/api_key"
}

// GetFor reads the secret a provider authenticates with (account key for
// the router provider, per-provider ref for legacy providers).
func GetFor(providerID string) (string, error) {
	return GetSecret(KeyFor(providerID))
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

// SetSecret stores a generic secret by ref.
func SetSecret(ref, v string) error {
	if err := keyring.Set(service, ref, v); err != nil {
		return fmt.Errorf("secrets store refused the save: %w", err)
	}
	return nil
}

// GetSecret retrieves a generic secret by ref.
func GetSecret(ref string) (string, error) {
	v, err := keyring.Get(service, ref)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("secrets store is unreachable: %w", err)
	}
	return v, nil
}

// DeleteSecret deletes a generic secret by ref.
func DeleteSecret(ref string) error {
	err := keyring.Delete(service, ref)
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("secrets store refused the delete: %w", err)
	}
	return nil
}

package Keyring

import (
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

const service = "argus"

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
			return "", fmt.Errorf("no api key stored for this provider")
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

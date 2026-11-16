package Account

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/anyscapelabs/argus/Keyring"
	"github.com/anyscapelabs/argus/Services/Shared"
)

// The desktop agent authenticates to the argus-router API with a long-lived
// account key created from the web dashboard ("console/keys"). The key lives
// in the OS keyring and travels as the `x-api-key` header; the backend turns
// it into a user identity, the user's plan, and the model allowlist.
const (
	keyRef   = "account/key"
	keyCheck = "argus_pk_"
	httpWait = 15 * time.Second
	httpCap  = int64(1 << 20)
)

type Service struct {
	Core *Shared.Core
}

const offlineGrace = 7 * 24 * time.Hour

// SaveKey stores the pasted account key in the OS keyring. The key is never
// echoed back by any binding — GetKeyStatus reports presence only.
func (s *Service) SaveKey(key string) Shared.SaveAccountKeyResult {
	key = strings.TrimSpace(key)
	if !strings.HasPrefix(key, keyCheck) {
		return Shared.SaveAccountKeyResult{
			Error: Shared.NewError(Shared.CodeKeyFormat, "This doesn't look like an Argus agent key. Copy the full key from the dashboard."),
		}
	}
	if err := Keyring.SetSecret(keyRef, key); err != nil {
		return Shared.SaveAccountKeyResult{
			Error: Shared.NewError(Shared.CodeStorage, fmt.Sprintf("Could not store the agent key: %v", err)),
		}
	}
	return Shared.SaveAccountKeyResult{Ok: true}
}

// GetKeyStatus lets the shell decide which screen to show without touching
// the secret value itself.
func (s *Service) GetKeyStatus() Shared.KeyStatusResult {
	_, err := Keyring.GetSecret(keyRef)
	if err != nil {
		if !errors.Is(err, Keyring.ErrNotFound) {
			return Shared.KeyStatusResult{
				Error: Shared.NewError(Shared.CodeStorage, fmt.Sprintf("Could not read the agent key: %v", err)),
			}
		}
		return Shared.KeyStatusResult{Status: &Shared.KeyStatus{HasKey: false}}
	}
	return Shared.KeyStatusResult{Status: &Shared.KeyStatus{HasKey: true}}
}

func (s *Service) DeleteKey() Shared.SaveAccountKeyResult {
	err := Keyring.DeleteSecret(keyRef)
	if err != nil && !errors.Is(err, Keyring.ErrNotFound) {
		return Shared.SaveAccountKeyResult{
			Error: Shared.NewError(Shared.CodeStorage, fmt.Sprintf("Could not remove the agent key: %v", err)),
		}
	}
	if s.Core != nil && s.Core.DB != nil {
		_ = s.Core.DB.DeleteAccountEntitlements()
		// The provider-config row doubles as the "where model traffic goes"
		// override written at pairing time (D5); clearing it reverts the
		// backend to the overlay's default until the next pair.
		_ = s.Core.DB.RemoveProviderConfig(Shared.RouterProviderID)
	}
	return Shared.SaveAccountKeyResult{Ok: true}
}

// Entitlements verifies the stored key against the argus-router API and
// returns the identity + plan the agent may use. A revoked or malformed key
// surfaces as a typed error so the shell can route the operator back to
// pairing.
func (s *Service) Entitlements(serverURL string) Shared.EntitlementsView {
	key, err := Keyring.GetSecret(keyRef)
	if err != nil {
		if errors.Is(err, Keyring.ErrNotFound) {
			return Shared.EntitlementsView{
				Error: Shared.NewError(Shared.CodeNotConfigured, "No agent key is paired yet."),
			}
		}
		return Shared.EntitlementsView{
			Error: Shared.NewError(Shared.CodeStorage, fmt.Sprintf("Could not read the agent key: %v", err)),
		}
	}

	serverURL = strings.TrimRight(strings.TrimSpace(serverURL), "/")
	if serverURL == "" {
		return Shared.EntitlementsView{
			Error: Shared.NewError(Shared.CodeBadInput, "The Argus server address is not configured."),
		}
	}

	// cached returns the last successful snapshot when it is still within the
	// offline grace window. Used both for transport failures and non-auth HTTP
	// failures so a paired app can keep running without the network.
	cached := func() (Shared.EntitlementsView, bool) {
		if s.Core == nil || s.Core.DB == nil {
			return Shared.EntitlementsView{}, false
		}
		payload, fetchedAt, found, err := s.Core.DB.GetAccountEntitlements()
		if err != nil || !found {
			return Shared.EntitlementsView{}, false
		}
		if time.Since(time.Unix(fetchedAt, 0)) > offlineGrace {
			return Shared.EntitlementsView{}, false
		}
		var view Shared.Entitlements
		if json.Unmarshal([]byte(payload), &view) != nil {
			return Shared.EntitlementsView{}, false
		}
		view.Offline = true
		// The offline path still wants model traffic at the paired backend —
		// the sign-out may have cleared the override, but the pairing key is
		// still in the keyring and the cached tenant is still valid.
		_ = s.saveProviderOverride(serverURL)
		return Shared.EntitlementsView{Account: &view}, true
	}

	ctx, cancel := context.WithTimeout(context.Background(), httpWait)
	defer cancel()

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		serverURL+"/api/v1/account/entitlements",
		nil,
	)
	if err != nil {
		if v, ok := cached(); ok {
			return v
		}
		return Shared.EntitlementsView{
			Error: Shared.NewError(Shared.CodeNetwork, fmt.Sprintf("Could not build the request: %v", err)),
		}
	}
	request.Header.Set("x-api-key", key)

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		if v, ok := cached(); ok {
			return v
		}
		return Shared.EntitlementsView{
			Error: Shared.NewError(Shared.CodeNetwork, fmt.Sprintf("Could not reach the Argus server: %v", err)),
		}
	}
	defer func() { _ = response.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(response.Body, httpCap))
	if err != nil {
		if v, ok := cached(); ok {
			return v
		}
		return Shared.EntitlementsView{
			Error: Shared.NewError(Shared.CodeBadResponse, fmt.Sprintf("Could not read the server response: %v", err)),
		}
	}

	if response.StatusCode/100 != 2 {
		if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
			if s.Core != nil && s.Core.DB != nil {
				_ = s.Core.DB.DeleteAccountEntitlements()
			}
			var envelope struct {
				Error *Shared.ActionError `json:"error"`
			}
			if json.Unmarshal(body, &envelope) == nil && envelope.Error != nil {
				return Shared.EntitlementsView{Error: envelope.Error}
			}
			return Shared.EntitlementsView{
				Error: Shared.NewError(Shared.CodeInvalidKey, "This agent key is no longer valid. Pair the app again from the dashboard."),
			}
		}
		if v, ok := cached(); ok {
			return v
		}
		var envelope struct {
			Error *Shared.ActionError `json:"error"`
		}
		if json.Unmarshal(body, &envelope) == nil && envelope.Error != nil {
			return Shared.EntitlementsView{Error: envelope.Error}
		}
		return Shared.EntitlementsView{
			Error: Shared.NewError(Shared.CodeBadResponse, fmt.Sprintf("The server responded with status %d", response.StatusCode)),
		}
	}

	view := &Shared.Entitlements{}
	if decodeErr := json.NewDecoder(bytes.NewReader(body)).Decode(view); decodeErr != nil {
		if v, ok := cached(); ok {
			return v
		}
		return Shared.EntitlementsView{
			Error: Shared.NewError(Shared.CodeBadResponse, fmt.Sprintf("The server response was unreadable: %v", decodeErr)),
		}
	}
	view.Offline = false

	// Isolate account tenants before anything else: if this key belongs to a
	// different account than the one that last paired on this machine, the
	// previous tenant's local data is gone now — never visible to the new
	// pairing. Same tenant / fresh install: nothing is purged.
	_ = s.isolateAccount(view.User.ID)
	// Point the model-traffic provider at the same backend this key paired
	// with, honoring VITE_ARGUS_API_URL in dev (D5).
	_ = s.saveProviderOverride(serverURL)

	// Persist the successful snapshot for offline use.
	if s.Core != nil && s.Core.DB != nil {
		if payload, merr := json.Marshal(view); merr == nil {
			_ = s.Core.DB.SaveAccountEntitlements(string(payload), time.Now().Unix())
		}
	}

	return Shared.EntitlementsView{Account: view}
}

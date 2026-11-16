package Account

import (
	"strings"

	"github.com/anyscapelabs/argus/Services/Shared"
	"github.com/anyscapelabs/argus/Storage"
)

// tenantSettingKey records which account the local agent belongs to.
const tenantSettingKey = "account/tenant_user_id"

// isolateAccount purges local, account-scoped data when a *different* account
// pairs on this install. A plain sign-out (or a re-pair of the same account)
// never purges — the machine keeps its tenant, and only its entitlement cache
// and provider override are cleared separately by DeleteKey.
//
// This is the non-destructive equivalent of deleting the agent on account
// change: the previous tenant's chats, memories, artifacts and permission
// grants must not leak into the next account's session on a shared machine.
func (s *Service) isolateAccount(userID string) error {
	if s.Core == nil || s.Core.DB == nil || strings.TrimSpace(userID) == "" {
		return nil
	}
	db := s.Core.DB
	prev, _, err := db.GetAppSetting(tenantSettingKey)
	if err != nil {
		// Storage hiccup — keep the data rather than purge on partial info.
		return err
	}
	if prev == userID {
		return nil
	}
	if prev != "" {
		if err := db.PurgeAccountTables(); err != nil {
			return err
		}
		_ = db.RemoveProviderConfig(Shared.RouterProviderID)
	}
	return db.SetAppSetting(tenantSettingKey, userID)
}

// saveProviderOverride records where model traffic should go: the backend
// this key just paired with. Without it, the frontend (VITE_ARGUS_API_URL)
// and the embedded overlay (hardcoded base_url) can point at two different
// deployments.
func (s *Service) saveProviderOverride(serverURL string) error {
	if s.Core == nil || s.Core.DB == nil {
		return nil
	}
	db := s.Core.DB
	override := strings.TrimRight(serverURL, "/") + "/v1"
	pc := Storage.ProviderConfig{ProviderID: Shared.RouterProviderID, BaseURLOverride: override}
	if existing, found, err := db.GetProviderConfig(Shared.RouterProviderID); err == nil && found {
		existing.BaseURLOverride = override
		pc = existing
	}
	return db.SaveProviderConfig(pc)
}

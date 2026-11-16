package Shared

// SaveAccountKeyResult acknowledges a keyring write of the agent account key.
type SaveAccountKeyResult struct {
	Ok    bool         `json:"ok"`
	Error *ActionError `json:"error,omitempty"`
}

// KeyStatus is the presence probe the shell's router uses, without ever
// touching the secret's value.
type KeyStatusResult struct {
	Status *KeyStatus   `json:"status,omitempty"`
	Error  *ActionError `json:"error,omitempty"`
}

type KeyStatus struct {
	HasKey bool `json:"hasKey"`
}

// EntitlementUser mirrors the API identity block.
type EntitlementUser struct {
	ID    string  `json:"id"`
	Name  *string `json:"name"`
	Email *string `json:"email"`
	Image *string `json:"image"`
}

// EntitlementPlan mirrors the API plan block.
type EntitlementPlan struct {
	ID string `json:"id"`
}

// Entitlements is the identity + plan + allowlist trio the agent runs on.
// The allowlist carries the router's catalog id plus display name per entry
// so the picker shows the display name without any frontend trimming.
type Entitlements struct {
	User      EntitlementUser `json:"user"`
	Plan      EntitlementPlan `json:"plan"`
	Allowlist []AllowedModel  `json:"allowlist"`
	Offline   bool            `json:"offline,omitempty"`
}

type AllowedModel struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name,omitempty"`
	Name        string `json:"name,omitempty"`
}

func (m AllowedModel) Display() string {
	if m.DisplayName != "" {
		return m.DisplayName
	}
	if m.Name != "" {
		return m.Name
	}
	return m.ID
}

// EntitlementsView carries the entitlements snapshot or a typed error.
type EntitlementsView struct {
	Account *Entitlements `json:"account,omitempty"`
	Error   *ActionError  `json:"error,omitempty"`
}

package Shared

import ()

type MCPConnectRequestView struct {
	RequestID     string `json:"request_id"`
	AgentID       string `json:"agent_id"`
	AgentName     string `json:"agent_name"`
	ServerID      string `json:"server_id"`
	ServerName    string `json:"server_name"`
	Description   string `json:"description"`
	Icon          string `json:"icon"`
	ConnectionID  string `json:"connection_id,omitempty"`
	Label         string `json:"label,omitempty"`
	ExpectedEmail string `json:"expected_email,omitempty"`
	AccessLevel   string `json:"access_level,omitempty"`
	// State carries the card's last known state so a restored card shows what
	// it did before the remount: without it a connected integration would come
	// back offering Connect again. Empty means still waiting on the user.
	State string `json:"state"`
	// CreatedAt is the instant the card entered the conversation, in unix
	// milliseconds. The transcript weaves the card at that moment, the same way
	// it weaves notices, so it stays where the sign-in was asked for instead of
	// following every newer message to the bottom.
	CreatedAt int64 `json:"created_at"`
}

type MCPConnectDecisionView struct {
	RequestID string `json:"request_id"`
	AgentID   string `json:"agent_id"`
	Decision  string `json:"decision"`
	ServerID  string `json:"server_id"`
}

// IntegrationView is a connector offered by Argus, plus its local connection
// state for the Integrations page.
type IntegrationView struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	Connected   bool   `json:"connected"`
	// Category groups the Discover grid; custom servers use their own group.
	Category string `json:"category,omitempty"`
	// Custom marks a user-added server rather than a bundled connector.
	Custom bool `json:"custom,omitempty"`
	// OAuthMode mirrors the catalog entry (google | github | dynamic) so the
	// connect form knows whether an account email can be captured. Custom
	// servers leave it empty.
	OAuthMode string   `json:"oauth_mode,omitempty"`
	Tools     []string `json:"tools,omitempty"`
}

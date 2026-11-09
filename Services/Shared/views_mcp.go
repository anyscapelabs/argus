package Shared

import ()

type MCPConnectRequestView struct {
	RequestID   string `json:"request_id"`
	AgentID     string `json:"agent_id"`
	AgentName   string `json:"agent_name"`
	ServerID    string `json:"server_id"`
	ServerName  string `json:"server_name"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
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

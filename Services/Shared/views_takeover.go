package Shared

import ()

// TakeoverRequestView is the browser takeover card: what page the agent is on
// (its screenshot included) and where the handover stands. States ride the
// card itself so a restored card shows what happened before a remount:
// requested → active → done, or declined at any step.
type TakeoverRequestView struct {
	RequestID string `json:"request_id"`
	AgentID   string `json:"agent_id"`
	AgentName string `json:"agent_name,omitempty"`
	// Reason is why the agent asked: a CAPTCHA it cannot solve, a step that
	// needs something only the user can type (credentials, bank cards).
	Reason string `json:"reason,omitempty"`
	// Screenshot is a PNG data URL of the agent's viewport. Kept here rather
	// than on disk-path because the webview cannot read the OS cache dir.
	Screenshot string `json:"screenshot,omitempty"`
	Host       string `json:"host,omitempty"`
	PageTitle  string `json:"page_title,omitempty"`
	State      string `json:"state"`
	CreatedAt  int64  `json:"created_at"`
}

// TakeoverListResult is what the frontend reads on mount to restore cards.
type TakeoverListResult struct {
	Takeovers []TakeoverRequestView `json:"takeovers"`
	Error     *ActionError          `json:"error,omitempty"`
}

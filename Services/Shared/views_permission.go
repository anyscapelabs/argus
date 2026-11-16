package Shared

import ()

type PermissionRequestView struct {
	RequestID string `json:"request_id"`
	AgentID   string `json:"agent_id"`
	AgentName string `json:"agent_name"`
	Tool      string `json:"tool"`
	Command   string `json:"command"`
}

// ActivityView tells the frontend what the agent is live-doing
// (for example running a terminal command) while no text streams.
type ActivityView struct {
	AgentID string `json:"agent_id"`
	RunID   string `json:"run_id,omitempty"`
	Kind    string `json:"kind"` // tool_started | tool_results | tool_finished
	Tool    string `json:"tool"`
	Command string `json:"command,omitempty"`
	// Sources carries the sites a web_search call found, so the live
	// indicator can show favicon plus result titles instead of the query.
	Sources []ActivitySource `json:"sources,omitempty"`
	// Page carries where a browser action left the agent, so the live indicator
	// can name the site instead of the verb.
	Page *ActivityPage `json:"page,omitempty"`
}

// ActivityPage is the state of the agent's browser at one moment.
type ActivityPage struct {
	Host   string `json:"host"`
	Title  string `json:"title"`
	Action string `json:"action"`
	Tab    int    `json:"tab"`
}

// ActivitySource is one site the agent's search surfaced.
type ActivitySource struct {
	Host  string `json:"host"`
	Title string `json:"title"`
}

type PermissionDecisionView struct {
	RequestID string `json:"request_id"`
	AgentID   string `json:"agent_id"`
	AgentName string `json:"agent_name,omitempty"`
	Tool      string `json:"tool"`
	Command   string `json:"command"`
	Decision  string `json:"decision"`
}

type PermissionNoticeView struct {
	ID        string `json:"id"`
	AgentID   string `json:"agent_id"`
	AgentName string `json:"agent_name"`
	Tool      string `json:"tool"`
	Command   string `json:"command"`
	Decision  string `json:"decision"`
	// Kind separates a permission decision from any other notice, and Text
	// carries the copy for the kinds the UI cannot compose itself.
	Kind      string `json:"kind"`
	Text      string `json:"text"`
	CreatedAt int64  `json:"created_at"`
}

type PermissionState struct {
	AgentID string `json:"agent_id"`
	Tool    string `json:"tool"`
	Allowed bool   `json:"allowed"`
	Denied  bool   `json:"denied"`
}

type PermissionStateResult struct {
	State PermissionState `json:"state"`
	Error *ActionError    `json:"error,omitempty"`
}

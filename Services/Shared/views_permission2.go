package Shared

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

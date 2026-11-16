package Shared

type AskUserRequestView struct {
	RequestID string   `json:"request_id"`
	AgentID   string   `json:"agent_id"`
	AgentName string   `json:"agent_name,omitempty"`
	RunID     string   `json:"run_id"`
	Question  string   `json:"question"`
	Choices   []string `json:"choices"`
	CreatedAt int64    `json:"created_at"`
}

type AskUserListResult struct {
	Requests []AskUserRequestView `json:"requests"`
	Error    *ActionError         `json:"error,omitempty"`
}

type AskUserResolvedView struct {
	RequestID string `json:"request_id"`
	AgentID   string `json:"agent_id"`
	RunID     string `json:"run_id"`
	State     string `json:"state"`
}

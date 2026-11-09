package Shared

type AgentStatusView struct {
	StartedAtMs  int64 `json:"started_at_ms"`
	LastActiveMs int64 `json:"last_active_ms"`
	Busy         bool  `json:"busy"`
}

type AgentStatusResult struct {
	Status AgentStatusView `json:"status"`
	Error  *ActionError    `json:"error,omitempty"`
}

// ArtifactView is one document the agent wrote. Body is the canonical Markdown
// and arrives only from Get: a panel listing thirty reports should not carry
// thirty documents over the bridge. Words and Bytes are stored, not recomputed,
// so the number the panel shows is the number the writer was told.

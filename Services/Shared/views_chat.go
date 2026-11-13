package Shared

import (
	"github.com/anyscapelabs/argus/Protocol"
)

type AgentProfile struct {
	ID           string `json:"id"`
	Avatar       string `json:"avatar"`
	AgentName    string `json:"agent_name"`
	ModelRef     string `json:"model_ref"`
	SystemPrompt string `json:"system_prompt"`
	UpdatedAt    string `json:"updated_at"`
}

type AgentProfileResult struct {
	Profile *AgentProfile `json:"profile,omitempty"`
	Error   *ActionError  `json:"error,omitempty"`
}

type StreamRequest struct {
	Ref             string             `json:"ref"`
	System          string             `json:"system,omitempty"`
	Messages        []Protocol.Message `json:"messages"`
	MaxOutputTokens int64              `json:"max_output_tokens,omitempty"`
	Temperature     *float64           `json:"temperature,omitempty"`
	StopSequences   []string           `json:"stop_sequences,omitempty"`
	Reasoning       string             `json:"reasoning,omitempty"`
}

type StreamStart struct {
	Ok    bool         `json:"ok"`
	RunID string       `json:"run_id,omitempty"`
	Error *ActionError `json:"error,omitempty"`
}

type StreamEvent struct {
	RunID     string         `json:"run_id"`
	AgentID   string         `json:"agent_id,omitempty"`
	MessageID string         `json:"message_id,omitempty"`
	Event     Protocol.Event `json:"event"`
}

type SendResult struct {
	Ok                 bool         `json:"ok"`
	Error              *ActionError `json:"error,omitempty"`
	RunID              string       `json:"run_id,omitempty"`
	UserMessageID      string       `json:"user_message_id,omitempty"`
	AssistantMessageID string       `json:"assistant_message_id,omitempty"`
}

type ChatMessage struct {
	ID           string              `json:"id"`
	AgentID      string              `json:"agent_id"`
	Seq          int                 `json:"seq"`
	Role         string              `json:"role"`
	Kind         string              `json:"kind,omitempty"`
	Text         string              `json:"text"`
	Status       string              `json:"status"`
	ErrorMessage string              `json:"error_message,omitempty"`
	ErrorCode    string              `json:"error_code,omitempty"`
	StopReason   string              `json:"stop_reason,omitempty"`
	ModelRef     string              `json:"model_ref,omitempty"`
	CreatedAt    int64               `json:"created_at"`
	CompletedAt  int64               `json:"completed_at,omitempty"`
	Blocks       []Protocol.Block    `json:"blocks,omitempty"`
	FileWrite    *Protocol.FileWrite `json:"file_write,omitempty"`
	Reaction     string              `json:"reaction,omitempty"`
}

type MessageReactionResult struct {
	Ok        bool         `json:"ok"`
	MessageID string       `json:"message_id"`
	Reaction  string       `json:"reaction,omitempty"`
	Error     *ActionError `json:"error,omitempty"`
}

type GetChatResult struct {
	Messages []ChatMessage `json:"messages"`
	// NextBeforeSeq is the cursor the next older page is fetched with: seq
	// strictly below it. Zero means there is no older page — the conversation
	// has been read back to its first message.
	NextBeforeSeq int          `json:"next_before_seq,omitempty"`
	Error         *ActionError `json:"error,omitempty"`
}

type UsageResult struct {
	InputTokens  int64        `json:"input_tokens"`
	OutputTokens int64        `json:"output_tokens"`
	CostUSD      float64      `json:"cost_usd"`
	Requests     int          `json:"requests"`
	Failed       int          `json:"failed"`
	Error        *ActionError `json:"error,omitempty"`
}

type ListRequestsResult struct {
	Requests []ModelRequestView `json:"requests"`
	Error    *ActionError       `json:"error,omitempty"`
}

type ModelRequestView struct {
	ID           string  `json:"id"`
	Status       string  `json:"status"`
	ModelRef     string  `json:"model_ref"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	CostUSD      float64 `json:"cost_usd"`
	StartedAt    int64   `json:"started_at"`
	CompletedAt  int64   `json:"completed_at"`
}

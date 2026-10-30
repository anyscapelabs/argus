package Protocol

import (
	"encoding/json"
)

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

type BlockType string

const (
	BlockText       BlockType = "text"
	BlockImage      BlockType = "image"
	BlockToolUse    BlockType = "tool_use"
	BlockToolResult BlockType = "tool_result"
	BlockThinking   BlockType = "thinking"
)

type ToolChoiceMode string

const (
	ToolChoiceAuto ToolChoiceMode = "auto"
	ToolChoiceAny  ToolChoiceMode = "any"
	ToolChoiceTool ToolChoiceMode = "tool"
	ToolChoiceNone ToolChoiceMode = "none"
)

type ReasoningLevel string

const (
	ReasoningUnset  ReasoningLevel = ""
	ReasoningNone   ReasoningLevel = "none"
	ReasoningLow    ReasoningLevel = "low"
	ReasoningMedium ReasoningLevel = "medium"
	ReasoningHigh   ReasoningLevel = "high"
)

type Image struct {
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

func (i Image) DataURL() string {
	return "data:" + i.MediaType + ";base64," + i.Data
}

type ToolCall struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Input string `json:"input,omitempty"`
	Valid bool   `json:"valid"`
}

type Block struct {
	Type      BlockType       `json:"type"`
	Text      string          `json:"text,omitempty"`
	Image     *Image          `json:"image,omitempty"`
	ToolCall  *ToolCall       `json:"tool_call,omitempty"`
	ToolRefID string          `json:"tool_ref,omitempty"`
	ToolName  string          `json:"tool_name,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
	Signature string          `json:"signature,omitempty"`
	Extra     json.RawMessage `json:"extra,omitempty"`
}

func (b Block) Clone() Block {
	if b.ToolCall != nil {
		tc := *b.ToolCall
		b.ToolCall = &tc
	}
	if b.Image != nil {
		img := *b.Image
		b.Image = &img
	}
	if b.Extra != nil {
		b.Extra = append(json.RawMessage(nil), b.Extra...)
	}
	return b
}

type Message struct {
	Role   Role    `json:"role"`
	Blocks []Block `json:"blocks,omitempty"`
}

type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema,omitempty"`
}

type ToolChoice struct {
	Mode ToolChoiceMode `json:"mode"`
	Name string         `json:"name,omitempty"`
}

type Request struct {
	Model           string            `json:"model"`
	System          string            `json:"system,omitempty"`
	Messages        []Message         `json:"messages"`
	Tools           []ToolDef         `json:"tools,omitempty"`
	ToolChoice      ToolChoice        `json:"tool_choice,omitempty"`
	MaxOutputTokens int64             `json:"max_output_tokens,omitempty"`
	Temperature     *float64          `json:"temperature,omitempty"`
	StopSequences   []string          `json:"stop_sequences,omitempty"`
	Reasoning       ReasoningLevel    `json:"reasoning,omitempty"`
	Options         map[string]string `json:"options,omitempty"`
}

func (r *Request) LastAssistant() *Message {
	for i := len(r.Messages) - 1; i >= 0; i-- {
		if r.Messages[i].Role == RoleAssistant {
			return &r.Messages[i]
		}
	}
	return nil
}

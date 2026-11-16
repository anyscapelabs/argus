package Anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/anyscapelabs/argus/Protocol"
)

const apiVersion = "2023-06-01"
const defaultMaxTokens = int64(8192)

type Adapter struct{}

func New(_ json.RawMessage) (*Adapter, error) {
	return &Adapter{}, nil
}

func (a *Adapter) Capabilities(info Protocol.ModelInfo) Protocol.Caps {
	return Protocol.Caps{
		Tools:          true,
		ParallelTools:  true,
		Vision:         true,
		Reasoning:      true,
		MaxOutputLimit: info.MaxOutput,
	}
}

type wireImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type wireBlock struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`

	Source *wireImageSource `json:"source,omitempty"`

	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   any    `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`

	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
}

type wireMessage struct {
	Role    string      `json:"role"`
	Content []wireBlock `json:"content"`
}

type wireToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema"`
}

type wireToolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

type wireThinking struct {
	Type         string `json:"type"`
	BudgetTokens int64  `json:"budget_tokens"`
}

type wireBody struct {
	Model         string          `json:"model"`
	MaxTokens     int64           `json:"max_tokens"`
	System        string          `json:"system,omitempty"`
	Messages      []wireMessage   `json:"messages"`
	Tools         []wireToolDef   `json:"tools,omitempty"`
	ToolChoice    *wireToolChoice `json:"tool_choice,omitempty"`
	Thinking      *wireThinking   `json:"thinking,omitempty"`
	Temperature   *float64        `json:"temperature,omitempty"`
	StopSequences []string        `json:"stop_sequences,omitempty"`
	Stream        bool            `json:"stream"`
}

func (a *Adapter) BuildRequest(ctx context.Context, req *Protocol.Request, target *Protocol.Target) (*http.Request, error) {
	body := buildBody(req)
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode anthropic messages request: %w", err)
	}
	endpoint := strings.TrimSuffix(target.BaseURL, "/") + "/v1/messages"
	hReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	hReq.Header.Set("Content-Type", "application/json")
	hReq.Header.Set("Accept", "text/event-stream")
	hReq.Header.Set("anthropic-version", apiVersion)
	if target.APIKey != "" {
		hReq.Header.Set("x-api-key", target.APIKey)
	}
	for k, v := range target.ExtraHeaders {
		if k != "" && v != "" {
			hReq.Header.Set(k, v)
		}
	}
	return hReq, nil
}

func thinkingBudget(level Protocol.ReasoningLevel) int64 {
	switch level {
	case Protocol.ReasoningLow:
		return 1024
	case Protocol.ReasoningMedium:
		return 4096
	case Protocol.ReasoningHigh:
		return 9216
	}
	return 0
}

func buildBody(req *Protocol.Request) *wireBody {
	maxTokens := req.MaxOutputTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}
	b := &wireBody{Model: req.Model, MaxTokens: maxTokens, Stream: true}
	if req.System != "" {
		b.System = req.System
	}
	if req.Temperature != nil {
		b.Temperature = req.Temperature
	}
	if len(req.StopSequences) > 0 {
		b.StopSequences = req.StopSequences
	}
	if budget := thinkingBudget(req.Reasoning); budget > 0 {
		if b.MaxTokens <= budget {
			b.MaxTokens = budget + 1024
		}
		b.Thinking = &wireThinking{Type: "enabled", BudgetTokens: budget}
	}
	b.Messages = translateMessages(req)
	if len(req.Tools) > 0 {
		b.Tools = make([]wireToolDef, 0, len(req.Tools))
		for _, t := range req.Tools {
			schema := t.Schema
			if len(schema) == 0 {
				schema = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			b.Tools = append(b.Tools, wireToolDef{Name: t.Name, Description: t.Description, InputSchema: schema})
		}
		switch req.ToolChoice.Mode {
		case Protocol.ToolChoiceAny:
			b.ToolChoice = &wireToolChoice{Type: "any"}
		case Protocol.ToolChoiceTool:
			b.ToolChoice = &wireToolChoice{Type: "tool", Name: req.ToolChoice.Name}
		case Protocol.ToolChoiceNone:
			b.ToolChoice = &wireToolChoice{Type: "auto"}
		default:
			b.ToolChoice = &wireToolChoice{Type: "auto"}
		}
	}
	return b
}

func translateMessages(req *Protocol.Request) []wireMessage {
	out := make([]wireMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		switch m.Role {
		case Protocol.RoleAssistant:
			blocks := make([]wireBlock, 0, len(m.Blocks))
			for _, blk := range m.Blocks {
				switch blk.Type {
				case Protocol.BlockText:
					blocks = append(blocks, wireBlock{Type: "text", Text: blk.Text})
				case Protocol.BlockThinking:
					blocks = append(blocks, wireBlock{Type: "thinking", Thinking: blk.Text, Signature: blk.Signature})
				case Protocol.BlockToolUse:
					if blk.ToolCall == nil {
						continue
					}
					var input json.RawMessage
					if blk.ToolCall.Valid && strings.TrimSpace(blk.ToolCall.Input) != "" {
						input = json.RawMessage(blk.ToolCall.Input)
					} else {
						input = json.RawMessage(`{}`)
					}
					blocks = append(blocks, wireBlock{Type: "tool_use", ID: blk.ToolCall.ID, Name: blk.ToolCall.Name, Input: input})
				}
			}
			if len(blocks) > 0 {
				out = append(out, wireMessage{Role: "assistant", Content: blocks})
			}
		case Protocol.RoleUser:
			blocks := make([]wireBlock, 0, len(m.Blocks))
			for _, blk := range m.Blocks {
				switch blk.Type {
				case Protocol.BlockText:
					blocks = append(blocks, wireBlock{Type: "text", Text: blk.Text})
				case Protocol.BlockImage:
					if blk.Image == nil {
						continue
					}
					blocks = append(blocks, wireBlock{Type: "image", Source: &wireImageSource{Type: "base64", MediaType: blk.Image.MediaType, Data: blk.Image.Data}})
				case Protocol.BlockToolResult:
					content := blk.Text
					if content == "" && blk.ToolCall != nil {
						content = blk.ToolCall.Input
					}
					result := wireBlock{Type: "tool_result", ToolUseID: blk.ToolRefID, IsError: blk.IsError}
					if content != "" {
						var probe any
						if json.Unmarshal([]byte(content), &probe) == nil {
							result.Content = json.RawMessage(content)
						} else {
							result.Content = content
						}
					}
					blocks = append(blocks, result)
				}
			}
			if len(blocks) > 0 {
				out = append(out, wireMessage{Role: "user", Content: blocks})
			}
		}
	}
	if len(out) == 0 {
		out = append(out, wireMessage{Role: "user", Content: []wireBlock{{Type: "text", Text: " "}}})
	}
	return out
}

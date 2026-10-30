package OpenAI

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/anyscapelabs/argus/Protocol"
)

type Quirks struct {
	ChatPath       string `json:"chat_path,omitempty"`
	MaxTokensField string `json:"max_tokens_field,omitempty"`
	SystemRole     string `json:"system_role,omitempty"`
	StreamUsage    bool   `json:"stream_usage,omitempty"`
	ReasoningField string `json:"reasoning_field,omitempty"`
	ParallelTools  bool   `json:"parallel_tools,omitempty"`
}

type Adapter struct {
	quirks Quirks
}

func New(rawQuirks json.RawMessage) (*Adapter, error) {
	var q Quirks
	if len(rawQuirks) != 0 {
		if err := json.Unmarshal(rawQuirks, &q); err != nil {
			return nil, fmt.Errorf("bad openai compat quirks: %w", err)
		}
	}
	if q.ChatPath == "" {
		q.ChatPath = "/chat/completions"
	}
	if q.MaxTokensField == "" {
		q.MaxTokensField = "max_tokens"
	}
	if q.SystemRole == "" {
		q.SystemRole = "system"
	}
	return &Adapter{quirks: q}, nil
}

func (a *Adapter) Capabilities(info Protocol.ModelInfo) Protocol.Caps {
	return Protocol.Caps{
		Tools:          true,
		ParallelTools:  a.quirks.ParallelTools,
		Vision:         true,
		Reasoning:      a.quirks.ReasoningField != "",
		MaxOutputLimit: info.MaxOutput,
	}
}

func (a *Adapter) BuildRequest(ctx context.Context, req *Protocol.Request, target *Protocol.Target) (*http.Request, error) {
	b, err := json.Marshal(a.buildBody(req))
	if err != nil {
		return nil, fmt.Errorf("encode chat completion request: %w", err)
	}
	endpoint := strings.TrimSuffix(target.BaseURL, "/") + a.quirks.ChatPath
	if _, err := url.Parse(endpoint); err != nil {
		return nil, fmt.Errorf("bad chat completions endpoint: %w", err)
	}
	hReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	hReq.Header.Set("Content-Type", "application/json")
	hReq.Header.Set("Accept", "text/event-stream")
	if target.APIKey != "" {
		hReq.Header.Set("Authorization", "Bearer "+target.APIKey)
	}
	for k, v := range target.ExtraHeaders {
		if k != "" && v != "" {
			hReq.Header.Set(k, v)
		}
	}
	return hReq, nil
}

type wireStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type wireToolFunctionDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

type wireToolDef struct {
	Type     string              `json:"type"`
	Function wireToolFunctionDef `json:"function"`
}

type wireFunctionCall struct {
	ID        string `json:"id,omitempty"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type wireToolCall struct {
	ID       string           `json:"id,omitempty"`
	Type     string           `json:"type,omitempty"`
	Function wireFunctionCall `json:"function"`
}

type wirePart struct {
	Type     string     `json:"type"`
	Text     string     `json:"text,omitempty"`
	ImageURL *wireImage `json:"image_url,omitempty"`
}

type wireImage struct {
	URL string `json:"url"`
}

type wireMessage struct {
	Role             string         `json:"role"`
	Content          any            `json:"content,omitempty"`
	ReasoningContent string         `json:"reasoning_content,omitempty"`
	Reasoning        string         `json:"reasoning,omitempty"`
	ToolCalls        []wireToolCall `json:"tool_calls,omitempty"`
	ToolCallID       string         `json:"tool_call_id,omitempty"`
}

type wireChoiceToolChoice struct {
	Type     string        `json:"type"`
	Function wireNamedFunc `json:"function,omitempty"`
}

type wireNamedFunc struct {
	Name string `json:"name"`
}

type wireBody struct {
	Model               string             `json:"model"`
	Messages            []wireMessage      `json:"messages"`
	Tools               []wireToolDef      `json:"tools,omitempty"`
	ToolChoice          any                `json:"tool_choice,omitempty"`
	ParallelToolCalls   *bool              `json:"parallel_tool_calls,omitempty"`
	MaxTokens           int64              `json:"max_tokens,omitempty"`
	MaxCompletionTokens int64              `json:"max_completion_tokens,omitempty"`
	Temperature         *float64           `json:"temperature,omitempty"`
	Stop                []string           `json:"stop,omitempty"`
	Stream              bool               `json:"stream"`
	StreamOptions       *wireStreamOptions `json:"stream_options,omitempty"`
}

func (a *Adapter) buildBody(req *Protocol.Request) *wireBody {
	b := &wireBody{Model: req.Model, Stream: true}
	if a.quirks.StreamUsage {
		b.StreamOptions = &wireStreamOptions{IncludeUsage: true}
	}
	if a.quirks.MaxTokensField == "max_completion_tokens" {
		b.MaxCompletionTokens = req.MaxOutputTokens
	} else {
		b.MaxTokens = req.MaxOutputTokens
	}
	if req.Temperature != nil {
		b.Temperature = req.Temperature
	}
	if len(req.StopSequences) > 0 {
		b.Stop = req.StopSequences
	}
	b.Messages = translateMessages(req, a.quirks)
	if len(req.Tools) > 0 {
		b.Tools = make([]wireToolDef, 0, len(req.Tools))
		for _, t := range req.Tools {
			schema := t.Schema
			if len(schema) == 0 {
				schema = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			b.Tools = append(b.Tools, wireToolDef{
				Type:     "function",
				Function: wireToolFunctionDef{Name: t.Name, Description: t.Description, Parameters: schema},
			})
		}
		switch req.ToolChoice.Mode {
		case Protocol.ToolChoiceAny:
			if a.quirks.ParallelTools {
				b.ToolChoice = "required"
			} else {
				b.ToolChoice = "auto"
			}
		case Protocol.ToolChoiceTool:
			b.ToolChoice = wireChoiceToolChoice{Type: "function", Function: wireNamedFunc{Name: req.ToolChoice.Name}}
		case Protocol.ToolChoiceNone:
			b.ToolChoice = "none"
		default:
			b.ToolChoice = "auto"
		}
		if a.quirks.ParallelTools {
			parallel := true
			b.ParallelToolCalls = &parallel
		}
	}
	return b
}

func translateMessages(req *Protocol.Request, q Quirks) []wireMessage {
	out := make([]wireMessage, 0, len(req.Messages)+1)
	if req.System != "" {
		out = append(out, wireMessage{Role: q.SystemRole, Content: req.System})
	}
	for _, m := range req.Messages {
		switch m.Role {
		case Protocol.RoleAssistant:
			var text strings.Builder
			calls := make([]wireToolCall, 0, len(m.Blocks))
			reasoning := ""
			for _, b := range m.Blocks {
				switch b.Type {
				case Protocol.BlockText:
					if text.Len() > 0 {
						text.WriteString("\n\n")
					}
					text.WriteString(b.Text)
				case Protocol.BlockThinking:
					if reasoning != "" {
						reasoning += "\n"
					}
					reasoning += b.Text
				case Protocol.BlockToolUse:
					if b.ToolCall != nil {
						calls = append(calls, wireToolCall{
							ID:       b.ToolCall.ID,
							Type:     "function",
							Function: wireFunctionCall{Name: b.ToolCall.Name, Arguments: b.ToolCall.Input},
						})
					}
				}
			}
			msg := wireMessage{Role: "assistant", ToolCalls: calls}
			if text.Len() > 0 || len(calls) == 0 {
				msg.Content = text.String()
			}
			if q.ReasoningField != "" && reasoning != "" {
				if q.ReasoningField == "reasoning" {
					msg.Reasoning = reasoning
				} else {
					msg.ReasoningContent = reasoning
				}
			}
			out = append(out, msg)
		case Protocol.RoleUser:
			var parts []wirePart
			for _, blk := range m.Blocks {
				switch blk.Type {
				case Protocol.BlockText:
					parts = append(parts, wirePart{Type: "text", Text: blk.Text})
				case Protocol.BlockImage:
					if blk.Image != nil {
						parts = append(parts, wirePart{Type: "image_url", ImageURL: &wireImage{URL: blk.Image.DataURL()}})
					}
				case Protocol.BlockToolResult:
					content := blk.Text
					if content == "" && blk.ToolCall != nil {
						content = blk.ToolCall.Input
					}
					out = append(out, wireMessage{Role: "tool", ToolCallID: blk.ToolRefID, Content: content})
				}
			}
			if len(parts) == 1 && parts[0].Type == "text" {
				out = append(out, wireMessage{Role: "user", Content: parts[0].Text})
			} else if len(parts) > 0 {
				out = append(out, wireMessage{Role: "user", Content: parts})
			}
		}
	}
	if len(out) == 0 {
		out = append(out, wireMessage{Role: "user", Content: " "})
	}
	return out
}

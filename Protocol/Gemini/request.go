package Gemini

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

type wirePart struct {
	Text             string                `json:"text,omitempty"`
	Thought          bool                  `json:"thought,omitempty"`
	ThoughtSignature string                `json:"thoughtSignature,omitempty"`
	InlineData       *wireInlineData       `json:"inlineData,omitempty"`
	FunctionCall     *wireFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *wireFunctionResponse `json:"functionResponse,omitempty"`
}

type wireInlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type wireFunctionCall struct {
	ID   string          `json:"id,omitempty"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

type wireFunctionResponse struct {
	ID       string          `json:"id,omitempty"`
	Name     string          `json:"name"`
	Response json.RawMessage `json:"response"`
}

type wireContent struct {
	Role  string     `json:"role,omitempty"`
	Parts []wirePart `json:"parts"`
}

type wireFunctionDeclaration struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type wireToolSet struct {
	FunctionDeclarations []wireFunctionDeclaration `json:"functionDeclarations,omitempty"`
}

type wireFunctionCallingConfig struct {
	Mode                 string   `json:"mode"`
	AllowedFunctionNames []string `json:"allowedFunctionNames,omitempty"`
}

type wireToolConfig struct {
	FunctionCallingConfig wireFunctionCallingConfig `json:"functionCallingConfig"`
}

type wireGenerationConfig struct {
	MaxOutputTokens int64    `json:"maxOutputTokens,omitempty"`
	Temperature     *float64 `json:"temperature,omitempty"`
	StopSequences   []string `json:"stopSequences,omitempty"`
}

type wireBody struct {
	Contents          []wireContent         `json:"contents"`
	SystemInstruction *wireContent          `json:"systemInstruction,omitempty"`
	Tools             []wireToolSet         `json:"tools,omitempty"`
	ToolConfig        *wireToolConfig       `json:"toolConfig,omitempty"`
	GenerationConfig  *wireGenerationConfig `json:"generationConfig,omitempty"`
}

func (a *Adapter) BuildRequest(ctx context.Context, req *Protocol.Request, target *Protocol.Target) (*http.Request, error) {
	b := buildBody(req)
	raw, err := json.Marshal(b)
	if err != nil {
		return nil, fmt.Errorf("encode generateContent request: %w", err)
	}
	endpoint := strings.TrimSuffix(target.BaseURL, "/") + "/models/" + url.PathEscape(req.Model) + ":streamGenerateContent?alt=sse"
	hReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	hReq.Header.Set("Content-Type", "application/json")
	hReq.Header.Set("Accept", "text/event-stream")
	if target.APIKey != "" {
		hReq.Header.Set("x-goog-api-key", target.APIKey)
	}
	for k, v := range target.ExtraHeaders {
		if k != "" && v != "" {
			hReq.Header.Set(k, v)
		}
	}
	return hReq, nil
}

func buildBody(req *Protocol.Request) *wireBody {
	b := &wireBody{Contents: translateContents(req)}
	if req.System != "" {
		b.SystemInstruction = &wireContent{Parts: []wirePart{{Text: req.System}}}
	}
	gen := &wireGenerationConfig{}
	if req.MaxOutputTokens > 0 {
		gen.MaxOutputTokens = req.MaxOutputTokens
	}
	if req.Temperature != nil {
		gen.Temperature = req.Temperature
	}
	if len(req.StopSequences) > 0 {
		gen.StopSequences = req.StopSequences
	}
	b.GenerationConfig = gen
	if len(req.Tools) > 0 {
		decls := make([]wireFunctionDeclaration, 0, len(req.Tools))
		for _, t := range req.Tools {
			schema := t.Schema
			if len(schema) == 0 {
				schema = json.RawMessage(`{"type":"object","properties":{}}`)
			}
			decls = append(decls, wireFunctionDeclaration{Name: t.Name, Description: t.Description, Parameters: schema})
		}
		b.Tools = []wireToolSet{{FunctionDeclarations: decls}}
	}
	cfg := wireFunctionCallingConfig{Mode: "AUTO"}
	switch req.ToolChoice.Mode {
	case Protocol.ToolChoiceAny:
		cfg = wireFunctionCallingConfig{Mode: "ANY"}
	case Protocol.ToolChoiceNone:
		cfg = wireFunctionCallingConfig{Mode: "NONE"}
	case Protocol.ToolChoiceTool:
		cfg = wireFunctionCallingConfig{Mode: "ANY", AllowedFunctionNames: []string{req.ToolChoice.Name}}
	}
	b.ToolConfig = &wireToolConfig{FunctionCallingConfig: cfg}
	return b
}

func roleFor(r Protocol.Role) string {
	if r == Protocol.RoleAssistant {
		return "model"
	}
	return "user"
}

func decodeArgs(input string, valid bool) (args json.RawMessage, ok bool) {
	if !valid {
		return nil, true
	}
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return nil, true
	}
	if !json.Valid([]byte(trimmed)) {
		return nil, false
	}
	return json.RawMessage(trimmed), true
}

func functionResponsePayload(text string) json.RawMessage {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return json.RawMessage(`{"result":""}`)
	}
	if json.Valid([]byte(trimmed)) {
		var probe json.RawMessage
		_ = json.Unmarshal([]byte(trimmed), &probe)
		wrapped, err := json.Marshal(struct {
			Result json.RawMessage `json:"result"`
		}{Result: probe})
		if err == nil {
			return wrapped
		}
	}
	esc, err := json.Marshal(struct {
		Result string `json:"result"`
	}{Result: text})
	if err != nil {
		return json.RawMessage(`{"result":""}`)
	}
	return esc
}

func translateContents(req *Protocol.Request) []wireContent {
	out := make([]wireContent, 0, len(req.Messages))
	for _, m := range req.Messages {
		role := roleFor(m.Role)
		parts := make([]wirePart, 0, len(m.Blocks))
		for _, blk := range m.Blocks {
			switch blk.Type {
			case Protocol.BlockText:
				parts = append(parts, wirePart{Text: blk.Text})
			case Protocol.BlockImage:
				if blk.Image != nil {
					parts = append(parts, wirePart{InlineData: &wireInlineData{MimeType: blk.Image.MediaType, Data: blk.Image.Data}})
				}
			case Protocol.BlockThinking:
				if blk.Text == "" {
					continue
				}
				parts = append(parts, wirePart{Text: blk.Text, Thought: true, ThoughtSignature: blk.Signature})
			case Protocol.BlockToolUse:
				if blk.ToolCall == nil {
					continue
				}
				args, _ := decodeArgs(blk.ToolCall.Input, blk.ToolCall.Valid)
				call := wireFunctionCall{ID: blk.ToolCall.ID, Name: blk.ToolCall.Name, Args: args}
				part := wirePart{FunctionCall: &call}
				if len(blk.Extra) > 0 {
					var extra struct {
						Signature string `json:"thoughtSignature"`
					}
					if json.Unmarshal(blk.Extra, &extra) == nil && extra.Signature != "" {
						part.ThoughtSignature = extra.Signature
					}
				}
				parts = append(parts, part)
			case Protocol.BlockToolResult:
				name := blk.ToolName
				if blk.ToolCall != nil && blk.ToolCall.Name != "" {
					name = blk.ToolCall.Name
				}
				content := blk.Text
				if content == "" && blk.ToolCall != nil {
					content = blk.ToolCall.Input
				}
				resp := wireFunctionResponse{ID: blk.ToolRefID, Name: name, Response: functionResponsePayload(content)}
				parts = append(parts, wirePart{FunctionResponse: &resp})
			}
		}
		if len(parts) == 0 {
			parts = append(parts, wirePart{Text: " "})
		}
		out = append(out, wireContent{Role: role, Parts: parts})
	}
	if len(out) == 0 {
		out = append(out, wireContent{Role: "user", Parts: []wirePart{{Text: " "}}})
	}
	return out
}

package Protocol

import (
	"context"
	"encoding/json"
	"io"
	"iter"
	"net/http"
	"time"
)

type AdapterFamily string

const (
	FamilyOpenAICompat AdapterFamily = "openai_compat"
	FamilyAnthropic    AdapterFamily = "anthropic"
	FamilyGemini       AdapterFamily = "gemini"
)

type Target struct {
	Family       AdapterFamily     `json:"family"`
	BaseURL      string            `json:"base_url"`
	ModelID      string            `json:"model_id"`
	APIKey       string            `json:"api_key"`
	ExtraHeaders map[string]string `json:"extra_headers,omitempty"`
	Quirks       json.RawMessage   `json:"quirks,omitempty"`
}

type ModelInfo struct {
	ID         string `json:"id"`
	ContextLen int64  `json:"context_length"`
	MaxOutput  int64  `json:"max_output"`
}

type Caps struct {
	Tools          bool  `json:"tools"`
	ParallelTools  bool  `json:"parallel_tools"`
	Vision         bool  `json:"vision"`
	Reasoning      bool  `json:"reasoning"`
	MaxOutputLimit int64 `json:"max_output_limit"`
}

func ClampRequest(req *Request, caps Caps) {
	if caps.MaxOutputLimit > 0 && (req.MaxOutputTokens <= 0 || req.MaxOutputTokens > caps.MaxOutputLimit) {
		req.MaxOutputTokens = caps.MaxOutputLimit
	}
	if !caps.Tools {
		req.Tools = nil
		req.ToolChoice = ToolChoice{Mode: ToolChoiceAuto}
	}
	if !caps.ParallelTools && req.ToolChoice.Mode == ToolChoiceAuto {
		req.ToolChoice = ToolChoice{Mode: ToolChoiceAuto}
	}
}

type Adapter interface {
	BuildRequest(ctx context.Context, req *Request, target *Target) (*http.Request, error)
	ParseStream(body io.Reader) iter.Seq[Event]
	MapError(status int, body []byte, retryAfter time.Duration) *Error
	Capabilities(info ModelInfo) Caps
}

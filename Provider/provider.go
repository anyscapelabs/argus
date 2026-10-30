package Provider

import "encoding/json"

type AdapterFamily string

const (
	AdapterOpenAICompat AdapterFamily = "openai_compat"
	AdapterAnthropic    AdapterFamily = "anthropic"
	AdapterGemini       AdapterFamily = "gemini"
)

type Recommended struct {
	Main  []string `json:"main,omitempty"`
	Cheap []string `json:"cheap,omitempty"`
}

type Quirks struct {
	ChatPath       string `json:"chat_path,omitempty"`
	MaxTokensField string `json:"max_tokens_field,omitempty"`
	SystemRole     string `json:"system_role,omitempty"`
	StreamUsage    *bool  `json:"stream_usage,omitempty"`
	ReasoningField string `json:"reasoning_field,omitempty"`
	ParallelTools  *bool  `json:"parallel_tools,omitempty"`
}

func (q *Quirks) HasStreamUsage() bool {
	return q != nil && q.StreamUsage != nil && *q.StreamUsage
}

func (q *Quirks) AllowsParallelTools() bool {
	return q != nil && q.ParallelTools != nil && *q.ParallelTools
}

type OverlayProvider struct {
	ID             string            `json:"id"`
	DisplayName    string            `json:"display_name"`
	Adapter        AdapterFamily     `json:"adapter"`
	BaseURL        string            `json:"base_url"`
	ModelsPath     string            `json:"models_path,omitempty"`
	ExtraHeaders   map[string]string `json:"extra_headers,omitempty"`
	NoAuth         bool              `json:"no_auth,omitempty"`
	KeyHint        string            `json:"key_hint,omitempty"`
	KeyHelpURL     string            `json:"key_help_url,omitempty"`
	DocURL         string            `json:"doc_url,omitempty"`
	Order          int               `json:"order"`
	Logo           string            `json:"logo,omitempty"`
	Recommended    Recommended       `json:"recommended,omitempty"`
	Hide           []string          `json:"hide,omitempty"`
	NameOverrides  map[string]string `json:"name_overrides,omitempty"`
	LiveExclude    []string          `json:"live_exclude_patterns,omitempty"`
	DefaultExclude []string          `json:"default_exclude_patterns,omitempty"`
	Quirks         *Quirks           `json:"quirks,omitempty"`
}

type Overlay struct {
	Providers []OverlayProvider `json:"providers"`

	DefaultLiveExclude []string `json:"default_live_exclude_patterns,omitempty"`
}

func (op OverlayProvider) Name() string {
	if op.DisplayName != "" {
		return op.DisplayName
	}
	return op.ID
}

func ParseOverlay(body []byte) (*Overlay, error) {
	var overlay Overlay
	if err := json.Unmarshal(body, &overlay); err != nil {
		return nil, err
	}
	return &overlay, nil
}

func (o *Overlay) Lookup(id string) (OverlayProvider, bool) {
	for _, p := range o.Providers {
		if p.ID == id {
			return p, true
		}
	}
	return OverlayProvider{}, false
}

type Provider struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	Adapter    AdapterFamily `json:"adapter"`
	BaseURL    string        `json:"base_url"`
	ModelsPath string        `json:"models_path"`
	Quirks     *Quirks       `json:"quirks,omitempty"`

	NoAuth     bool   `json:"no_auth"`
	KeyHint    string `json:"key_hint,omitempty"`
	KeyHelpURL string `json:"key_help_url,omitempty"`
	DocURL     string `json:"doc_url,omitempty"`

	Order int    `json:"order"`
	Logo  string `json:"logo,omitempty"`

	InCatalogData bool `json:"in_catalog_data"`
}

package Shared

import (
	"github.com/anyscapelabs/argus/Models"
	"github.com/anyscapelabs/argus/Protocol"
)

type PickerOptions struct {
	Search      string `json:"search"`
	IncludeBeta bool   `json:"include_beta"`
	ToolsOnly   bool   `json:"tools_only"`
	RefreshLive bool   `json:"refresh_live"`
}

type ProviderView struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Logo               string `json:"logo"`
	Monogram           string `json:"monogram"`
	NoAuth             bool   `json:"no_auth"`
	KeyHint            string `json:"key_hint"`
	KeyHelpURL         string `json:"key_help_url"`
	DocURL             string `json:"doc_url"`
	Configured         bool   `json:"configured"`
	KeyLast4           string `json:"key_last4,omitempty"`
	ValidatedAt        string `json:"validated_at,omitempty"`
	ModelCount         int    `json:"model_count"`
	SelectedModelCount int    `json:"selected_model_count"`
	Order              int    `json:"order"`
}

type ModelView struct {
	Ref              string              `json:"ref"`
	ProviderID       string              `json:"provider_id"`
	ProviderName     string              `json:"provider_name,omitempty"`
	ID               string              `json:"id"`
	Name             string              `json:"name"`
	Family           string              `json:"family,omitempty"`
	Availability     string              `json:"availability"`
	Released         string              `json:"released,omitempty"`
	ReleasedLabel    string              `json:"released_label,omitempty"`
	ContextWindow    int64               `json:"context_window"`
	MaxOutput        int64               `json:"max_output"`
	ContextLabel     string              `json:"context_label"`
	MaxOutputLabel   string              `json:"max_output_label"`
	Price            *Models.Cost        `json:"price"`
	PriceLabel       string              `json:"price_label"`
	Capabilities     []string            `json:"capabilities"`
	Caps             Models.Capabilities `json:"caps"`
	CapsKnown        bool                `json:"caps_known"`
	Status           string              `json:"status"`
	OpenWeights      bool                `json:"open_weights"`
	Recommended      string              `json:"recommended,omitempty"`
	EligibleMain     bool                `json:"eligible_main"`
	EligibleCheap    bool                `json:"eligible_cheap"`
	IneligibleReason string              `json:"ineligible_reason,omitempty"`
	SelectedFor      []string            `json:"selected_for,omitempty"`
}

type Section struct {
	ID       string      `json:"id"`
	Title    string      `json:"title"`
	Subtitle string      `json:"subtitle,omitempty"`
	Models   []ModelView `json:"models"`
}

type Recommendations struct {
	Main  string `json:"main,omitempty"`
	Cheap string `json:"cheap,omitempty"`
}

type Picker struct {
	Provider      ProviderView    `json:"provider"`
	LiveChecked   bool            `json:"live_checked"`
	LiveError     *ActionError    `json:"live_error,omitempty"`
	Recommended   Recommendations `json:"recommended"`
	Sections      []Section       `json:"sections"`
	TotalCount    int             `json:"total_count"`
	CatalogSource string          `json:"catalog_source"`
	Stale         bool            `json:"stale,omitempty"`
}

type PickerResult struct {
	Ok     bool         `json:"ok"`
	Error  *ActionError `json:"error,omitempty"`
	Picker *Picker      `json:"picker,omitempty"`
}

type SaveKeyResult struct {
	Ok     bool         `json:"ok"`
	Error  *ActionError `json:"error,omitempty"`
	Picker *Picker      `json:"picker,omitempty"`
}

type ActionResult struct {
	Ok           bool         `json:"ok"`
	Error        *ActionError `json:"error,omitempty"`
	ClearedSlots []string     `json:"cleared_slots,omitempty"`
}

type GetModelResult struct {
	View  *ModelView   `json:"view,omitempty"`
	Error *ActionError `json:"error,omitempty"`
}

type SlotView struct {
	Slot   string     `json:"slot"`
	Ref    string     `json:"ref"`
	Model  *ModelView `json:"model,omitempty"`
	Status string     `json:"status"`
	Reason string     `json:"reason,omitempty"`
}

type ChatModel struct {
	Ref          string    `json:"ref"`
	ProviderID   string    `json:"provider_id"`
	ProviderName string    `json:"provider_name"`
	Model        ModelView `json:"model"`
}

type CustomModelInput struct {
	ProviderID    string   `json:"provider_id"`
	ModelID       string   `json:"model_id"`
	DisplayName   string   `json:"display_name"`
	ContextWindow int64    `json:"context_window,omitempty"`
	MaxOutput     int64    `json:"max_output,omitempty"`
	Tools         bool     `json:"tools"`
	Vision        bool     `json:"vision"`
	Reasoning     bool     `json:"reasoning"`
	PriceKnown    bool     `json:"price_known"`
	PriceInput    *float64 `json:"price_input,omitempty"`
	PriceOutput   *float64 `json:"price_output,omitempty"`
}

type StatusView struct {
	Source        string `json:"source"`
	FetchedAt     string `json:"fetched_at,omitempty"`
	LastAttempt   string `json:"last_attempt,omitempty"`
	LastError     string `json:"last_error,omitempty"`
	ProviderCount int    `json:"provider_count"`
	ModelCount    int    `json:"model_count"`
	Stale         bool   `json:"stale"`
	Placeholder   bool   `json:"placeholder"`
}

type RefreshResult struct {
	Ok     bool         `json:"ok"`
	Error  *ActionError `json:"error,omitempty"`
	Status *StatusView  `json:"status,omitempty"`
}

type SelectionsResult struct {
	Refs  []string     `json:"refs"`
	Error *ActionError `json:"error,omitempty"`
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
	RunID string         `json:"run_id"`
	Event Protocol.Event `json:"event"`
}

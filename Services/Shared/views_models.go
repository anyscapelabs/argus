package Shared

import ()

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

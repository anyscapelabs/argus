package Shared

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/anyscapelabs/argus/Provider"
)

const (
	anthropicVersion = "2023-06-01"
	maxListPages     = 50
)

type openAIModelPage struct {
	ID            string  `json:"id"`
	DisplayName   string  `json:"display_name"`
	Name          string  `json:"name"`
	OwnedBy       string  `json:"owned_by"`
	ContextLength float64 `json:"context_length"`
	ContextWindow float64 `json:"context_window"`
	MaxModelLen   float64 `json:"max_model_len"`
	Type          string  `json:"type"`
}

func fetchOpenAICompat(ctx context.Context, provider Provider.Provider, override string, extraHeaders map[string]string, key string) ([]LiveModel, *ActionError) {
	headers := listHeaders(extraHeaders)
	headers.Set("Authorization", "Bearer "+key)
	body, aerr := doList(ctx, http.MethodGet, listEndpoint(listBase(provider, override), provider.ModelsPath), headers)
	if aerr != nil {
		return nil, aerr
	}
	var envelope struct {
		Data []openAIModelPage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil && envelope.Data != nil {
		return keepChat(envelope.Data), nil
	}
	var bare []openAIModelPage
	if err := json.Unmarshal(body, &bare); err == nil {
		return keepChat(bare), nil
	}
	return nil, NewError(CodeBadResponse, "The provider response could not be parsed")
}

func keepChat(in []openAIModelPage) []LiveModel {
	out := make([]LiveModel, 0, len(in))
	for _, m := range in {
		if !isChatType(m.Type) {
			continue
		}
		name := firstNonEmpty(m.DisplayName, m.Name)
		if name == "" {
			name = m.ID
		}
		out = append(out, LiveModel{
			ID:         m.ID,
			Name:       name,
			ContextLen: int64FromFloat(pickPositiveNumber(m.ContextLength, m.ContextWindow, m.MaxModelLen)),
		})
	}
	return out
}

func listBase(provider Provider.Provider, override string) string {
	if override != "" {
		return override
	}
	return provider.BaseURL
}

func fetchAnthropic(ctx context.Context, provider Provider.Provider, override string, extraHeaders map[string]string, key string) ([]LiveModel, *ActionError) {
	headers := listHeaders(extraHeaders)
	headers.Set("x-api-key", key)
	headers.Set("anthropic-version", anthropicVersion)
	out := make([]LiveModel, 0, 32)
	cursor := ""
	for page := 0; page < maxListPages; page++ {
		url := listEndpoint(listBase(provider, override), provider.ModelsPath) + "?limit=1000"
		if cursor != "" {
			url += "&after_id=" + cursor
		}
		body, aerr := doList(ctx, http.MethodGet, url, headers)
		if aerr != nil {
			return nil, aerr
		}
		var response struct {
			Data []struct {
				ID          string `json:"id"`
				DisplayName string `json:"display_name"`
			} `json:"data"`
			HasMore bool   `json:"has_more"`
			LastID  string `json:"last_id"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, NewError(CodeBadResponse, "The provider response could not be parsed")
		}
		for _, m := range response.Data {
			out = append(out, LiveModel{ID: m.ID, Name: firstNonEmpty(m.DisplayName, m.ID)})
		}
		if !response.HasMore || response.LastID == "" {
			break
		}
		cursor = response.LastID
	}
	return out, nil
}

func fetchGemini(ctx context.Context, provider Provider.Provider, override string, extraHeaders map[string]string, key string) ([]LiveModel, *ActionError) {
	// The google provider now goes through the OpenAI-compatible endpoint
	// (see Provider/overlay.json), which lists models in the OpenAI shape.
	// fetchList routes AdapterGemini here only for backward compatibility
	// with any other gemini-adapter providers that still use the native
	// noun shape; keep both paths alive.
	headers := listHeaders(extraHeaders)
	headers.Set("x-goog-api-key", key)
	base := listEndpoint(listBase(provider, override), provider.ModelsPath)
	if !strings.Contains(base, "?") {
		base += "?pageSize=1000"
	}
	out := make([]LiveModel, 0, 32)
	token := ""
	for page := 0; page < maxListPages; page++ {
		url := base
		if token != "" {
			url += "&pageToken=" + token
		}
		body, aerr := doList(ctx, http.MethodGet, url, headers)
		if aerr != nil {
			return nil, aerr
		}
		if err := json.Unmarshal(body, &struct {
			Models []json.RawMessage `json:"models"`
		}{}); err != nil {
			// Not the native Gemini shape; the OpenAI-compatible endpoint
			// returns [{id,name}] instead.
			var envelope struct {
				Data []openAIModelPage `json:"data"`
			}
			if err := json.Unmarshal(body, &envelope); err != nil || envelope.Data == nil {
				return nil, NewError(CodeBadResponse, "The provider response could not be parsed")
			}
			return keepChat(envelope.Data), nil
		}
		var response struct {
			Models []struct {
				Name        string `json:"name"`
				DisplayName string `json:"displayName"`
				InputLimit  int64  `json:"inputTokenLimit"`
				OutputLimit int64  `json:"outputTokenLimit"`
			} `json:"models"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, NewError(CodeBadResponse, "The provider response could not be parsed")
		}
		for _, m := range response.Models {
			out = append(out, LiveModel{
				ID:         strings.TrimPrefix(m.Name, "models/"),
				Name:       m.DisplayName,
				ContextLen: m.InputLimit,
				MaxOutput:  m.OutputLimit,
			})
		}
		if response.NextPageToken == "" {
			break
		}
		token = response.NextPageToken
	}
	return out, nil
}

func isChatType(t string) bool {
	t = strings.ToLower(strings.TrimSpace(t))
	if t == "" {
		return true
	}
	for _, e := range []string{"embedding", "embed", "image", "audio", "speech", "transcription", "rerank", "moderation", "realtime", "guard", "classifier"} {
		if strings.Contains(t, e) {
			return false
		}
	}
	return true
}

func pickPositiveNumber(values ...float64) float64 {
	for _, v := range values {
		if v > 0 {
			return v
		}
	}
	return 0
}

func int64FromFloat(f float64) int64 {
	if f <= 0 {
		return 0
	}
	return int64(f)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func fetchList(ctx context.Context, provider Provider.Provider, extraHeaders map[string]string, override string, key string) ([]LiveModel, *ActionError) {
	if provider.ModelsPath == "" {
		return nil, NewError(CodeBadResponse, "This provider has no usable list endpoint")
	}
	switch provider.Adapter {
	case Provider.AdapterAnthropic:
		return fetchAnthropic(ctx, provider, override, extraHeaders, key)
	case Provider.AdapterGemini:
		return fetchGemini(ctx, provider, override, extraHeaders, key)
	default:
		return fetchOpenAICompat(ctx, provider, override, extraHeaders, key)
	}
}

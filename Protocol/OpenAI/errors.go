package OpenAI

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/anyscapelabs/argus/Protocol"
)

type wireErrorEnvelope struct {
	Error wireError `json:"error"`
}

func mapWireErrorType(t string) string {
	switch strings.ToLower(t) {
	case "invalid_request_error", "invalid_api_key", "insufficient_quota":
		return Protocol.CodeBadInput
	case "authentication_error", "permission_error":
		return Protocol.CodeInvalidKey
	case "rate_limit_error":
		return Protocol.CodeRateLimited
	}
	return Protocol.CodeProviderError
}

func statusFallbackCode(status int) string {
	switch {
	case status == 401 || status == 403:
		return Protocol.CodeInvalidKey
	case status == 429:
		return Protocol.CodeRateLimited
	case status == 404:
		return Protocol.CodeBadInput
	case status >= 400 && status < 500:
		return Protocol.CodeBadInput
	case status >= 500:
		return Protocol.CodeProviderError
	}
	return Protocol.CodeBadResponse
}

func statusMessage(status int, body string) string {
	switch status {
	case 401, 403:
		return "The provider rejected this API key"
	case 429:
		return "Rate limited by the provider. Try again shortly"
	case 404:
		return "The provider does not have this chat endpoint or model"
	}
	if status >= 500 {
		return "The provider had an internal error. Try again shortly"
	}
	return strings.TrimSpace(truncate(body))
}

func truncate(s string) string {
	if len(s) <= 400 {
		return s
	}
	return s[:400]
}

func (a *Adapter) MapError(status int, body []byte, retryAfter time.Duration) *Protocol.Error {
	return MapErrorInto(status, body, retryAfter)
}

func ApplyErrorDefaults(e *Protocol.Error, status int, retryAfter time.Duration) {
	e.Status = status
	if status == 429 {
		e.Retryable = true
	}
	if e.Code == Protocol.CodeProviderError && status >= 500 {
		e.Retryable = true
	}
	if retryAfter > 0 {
		e.RetryAfter = int64(retryAfter / time.Millisecond)
	}
}

func MapErrorInto(status int, body []byte, retryAfter time.Duration) *Protocol.Error {
	e := &Protocol.Error{
		Code:    statusFallbackCode(status),
		Message: statusMessage(status, string(body)),
		Raw:     truncate(string(body)),
	}
	var probe wireErrorEnvelope
	if err := json.Unmarshal(body, &probe); err == nil {
		if probe.Error.Message != "" {
			e.Message = probe.Error.Message
		}
		if probe.Error.Type != "" {
			e.Raw = probe.Error.Type
		}
	}
	if e.Raw != "" {
		if typed := mapWireErrorType(e.Raw); typed != Protocol.CodeProviderError && status != 401 && status != 403 && status != 429 {
			e.Code = typed
		}
	}
	ApplyErrorDefaults(e, status, retryAfter)
	return e
}

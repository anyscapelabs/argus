package Anthropic

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/anyscapelabs/argus/Protocol"
)

func (a *Adapter) MapError(status int, body []byte, retryAfter time.Duration) *Protocol.Error {
	e := &Protocol.Error{
		Code:    mapAnthropicStatus(status),
		Message: defaultStatusMessage(status),
		Status:  status,
	}
	var probe wireEnvelopeError
	if err := json.Unmarshal(body, &probe); err == nil && probe.Error.Message != "" {
		e.Code = mapErrorType(probe.Error.Type)
		e.Message = strings.TrimSpace(probe.Error.Message)
		if probe.Error.Type != "" {
			e.Raw = probe.Error.Type
		}
		if e.Code == Protocol.CodeProviderError {
			e.Code = mapAnthropicStatus(status)
		}
	}
	if status == http.StatusTooManyRequests {
		e.Retryable = true
	}
	if status >= 500 {
		e.Retryable = true
	}
	if status == http.StatusTooManyRequests && retryAfter > 0 {
		e.RetryAfter = int64(retryAfter / time.Millisecond)
	}
	return e
}

func mapAnthropicStatus(status int) string {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return Protocol.CodeInvalidKey
	case http.StatusTooManyRequests:
		return Protocol.CodeRateLimited
	case http.StatusBadRequest, http.StatusNotFound, http.StatusRequestEntityTooLarge:
		return Protocol.CodeBadInput
	case http.StatusGatewayTimeout, http.StatusServiceUnavailable:
		return Protocol.CodeProviderError
	}
	if status >= 500 {
		return Protocol.CodeProviderError
	}
	return Protocol.CodeBadResponse
}

func defaultStatusMessage(status int) string {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "The provider rejected this API key"
	case http.StatusTooManyRequests:
		return "Rate limited by the provider. Try again shortly"
	case http.StatusNotFound:
		return "The provider does not have this endpoint or model"
	case http.StatusServiceUnavailable:
		return "The provider is overloaded. Try again shortly"
	}
	if status >= 500 {
		return "The provider had an internal error. Try again shortly"
	}
	return "The provider returned an error"
}

package Gemini

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/anyscapelabs/argus/Protocol"
)

func (a *Adapter) MapError(status int, body []byte, retryAfter time.Duration) *Protocol.Error {
	e := &Protocol.Error{
		Code:    mapStatus(status),
		Message: statusMessage(status),
		Status:  status,
	}
	var probe wireError
	if err := json.Unmarshal(body, &probe); err == nil && probe.Message != "" {
		e.Code = mapStatusCode(probe.Code, probe.Status)
		e.Message = strings.TrimSpace(probe.Message)
		if probe.Status != "" {
			e.Raw = probe.Status
		}
		if e.Code == Protocol.CodeProviderError {
			e.Code = mapStatus(status)
		}
	}
	if status == http.StatusTooManyRequests || status >= 500 {
		e.Retryable = true
	}
	if status == http.StatusTooManyRequests && retryAfter > 0 {
		e.RetryAfter = int64(retryAfter / time.Millisecond)
	}
	return e
}

func mapStatus(status int) string {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return Protocol.CodeInvalidKey
	case http.StatusTooManyRequests:
		return Protocol.CodeRateLimited
	case http.StatusBadRequest, http.StatusNotFound, http.StatusRequestEntityTooLarge:
		return Protocol.CodeBadInput
	}
	if status >= 500 {
		return Protocol.CodeProviderError
	}
	return Protocol.CodeBadResponse
}

func mapStatusCode(status int, s string) string {
	switch strings.ToUpper(s) {
	case "UNAUTHENTICATED", "PERMISSION_DENIED":
		return Protocol.CodeInvalidKey
	case "RESOURCE_EXHAUSTED":
		return Protocol.CodeRateLimited
	case "INVALID_ARGUMENT", "FAILED_PRECONDITION", "NOT_FOUND":
		return Protocol.CodeBadInput
	}
	return mapStatus(status)
}

func statusMessage(status int) string {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return "The provider rejected this API key"
	case http.StatusTooManyRequests:
		return "Rate limited by the provider. Try again shortly"
	case http.StatusNotFound:
		return "The provider does not have this endpoint or model"
	}
	if status >= 500 {
		return "The provider had an internal error. Try again shortly"
	}
	return "The provider returned an error"
}

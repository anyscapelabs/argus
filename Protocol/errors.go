package Protocol

import (
	"fmt"
)

const (
	CodeInvalidKey    = "invalid_key"
	CodeRateLimited   = "rate_limited"
	CodeNetwork       = "network"
	CodeProviderError = "provider_error"
	CodeBadResponse   = "bad_response"
	CodeBadInput      = "bad_input"
	CodeNotConfigured = "not_configured"
	CodeCancelled     = "cancelled"
)

func NewError(code, message string) *Error {
	return &Error{Code: code, Message: message}
}

func Errorf(code string, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

func NetworkError(cause error) *Error {
	return &Error{Code: CodeNetwork, Message: "Could not reach the provider. Check your connection and try again", Retryable: true, Raw: causeText(cause)}
}

func CancelledError() *Error {
	return &Error{Code: CodeCancelled, Message: "The request was cancelled"}
}

func StreamEndingError(cause error) *Error {
	return &Error{Code: CodeBadResponse, Message: "The model stream ended unexpectedly", Retryable: true, Raw: causeText(cause)}
}

func causeText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

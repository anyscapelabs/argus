package Shared

import (
	"fmt"
)

type ActionError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *ActionError) Error() string {
	return e.Message
}

type Code string

const (
	CodeKeyFormat       Code = "key_format"
	CodeInvalidKey      Code = "invalid_key"
	CodeRateLimited     Code = "rate_limited"
	CodeNetwork         Code = "network"
	CodeProviderError   Code = "provider_error"
	CodeBadResponse     Code = "bad_response"
	CodeUnknownProvider Code = "unknown_provider"
	CodeStorage         Code = "storage"
	CodeNotConfigured   Code = "not_configured"
	CodeIneligible      Code = "ineligible"
	CodeNotFound        Code = "not_found"
	CodeBadInput        Code = "bad_input"
)

func NewError(code Code, message string) *ActionError {
	return &ActionError{Code: string(code), Message: message}
}

func wrap(code Code, err error) *ActionError {
	return &ActionError{Code: string(code), Message: fmt.Sprintf("%s", err)}
}

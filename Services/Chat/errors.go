package Chat

import (
	"strings"
	"time"

	"github.com/anyscapelabs/argus/Protocol"
)

const publicModelError = "An error has occurred. Try again later."

// messageIsOpaqueProviderError reports whether an error carries no usable
// detail: the provider said something generic ("Provider returned error" or
// empty) and gave no code a user could act on. These are upstream flakiness,
// not our bug, so the run retries once and, if it still fails, the friendly
// message above replaces the opaque one.
func messageIsOpaqueProviderError(e *Protocol.Error) bool {
	if e == nil {
		return false
	}
	msg := strings.ToLower(strings.TrimSpace(e.Message))
	if msg == "" {
		return true
	}
	if msg == "provider returned error" {
		return true
	}
	// OpenRouter's catch-all: "Provider returned error" with a raw of
	// "ERROR" means the hidden backend failed without explanation.
	if msg == "error" || msg == "internal error" {
		return true
	}
	// The argus-router masks every upstream rejection as this one message and
	// streams it as an error chunk over an HTTP 200 (its type arrives as
	// invalid_request_error), so a transient upstream failure is otherwise
	// indistinguishable from a request we got wrong. The user can act on
	// neither, so it gets the same single retry as the other catch-alls.
	if msg == "an error has occurred. try again later." {
		return true
	}
	if strings.Contains(msg, "upstream provider rejected") {
		return true
	}
	return false
}

// Model failures expose only rate and usage limits. Raw provider bodies stay in
// model_requests.error_raw for diagnostics and never enter the transcript.
func friendlyError(e *Protocol.Error) {
	if e == nil {
		return
	}
	e.Message = publicErrorMessage(e.Code, e.Message)
}

func publicErrorMessage(code, message string) string {
	if strings.TrimSpace(message) == "" {
		return ""
	}
	if code == Protocol.CodeRateLimited || Protocol.IsUsageLimit(code) {
		return message
	}
	return publicModelError
}

// retryDelay honors the provider's Retry-After when present, otherwise backs
// off exponentially (2s, 4s).
func retryDelay(err *Protocol.Error, tries int) time.Duration {
	if err.RetryAfter > 0 {
		d := time.Duration(err.RetryAfter) * time.Second
		if d > 90*time.Second {
			d = 90 * time.Second
		}
		return d
	}
	return time.Duration(2<<tries) * time.Second
}

// stripToolUses removes tool_use blocks from a message being persisted. An
// assistant row whose tool_results never got written (cancelled or failed
// turn) would otherwise sit in history as an orphaned call the provider
// rejects, poisoning every later send.

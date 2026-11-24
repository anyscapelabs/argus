package OpenAI

import (
	"testing"
	"time"

	"github.com/anyscapelabs/argus/Protocol"
)

func TestMapErrorIntoPreservesUsageLimitCodes(t *testing.T) {
	body := []byte(`{"error":{"message":"Daily Limit Reached — Resets at 00:00 UTC (in 2h 14m)","type":"rate_limit_error","code":"daily_limit_reached","param":null}}`)
	e := MapErrorInto(429, body, 0)
	if e.Code != Protocol.CodeDailyLimitReached {
		t.Fatalf("code = %q, want daily_limit_reached", e.Code)
	}
	if e.Retryable {
		t.Fatal("usage limit must not be retryable")
	}
	if e.Message != "Daily Limit Reached — Resets at 00:00 UTC (in 2h 14m)" {
		t.Fatalf("message = %q", e.Message)
	}
}

func TestMapErrorIntoKeepsGenericRateLimitRetryable(t *testing.T) {
	body := []byte(`{"error":{"message":"slow down","type":"rate_limit_error","param":null}}`)
	e := MapErrorInto(429, body, 0)
	if !e.Retryable {
		t.Fatal("generic 429 must stay retryable")
	}
	if e.Code != Protocol.CodeRateLimited {
		t.Fatalf("code = %q, want rate_limited", e.Code)
	}
}

func TestMapErrorIntoKeeps502RetryableWhenBodyMasksUpstream(t *testing.T) {
	body := []byte(`{"error":{"message":"Provider returned error","type":"invalid_request_error","code":null,"param":null}}`)
	e := MapErrorInto(502, body, 0)
	if e.Code != Protocol.CodeBadInput {
		t.Fatalf("code = %q, want bad_input", e.Code)
	}
	if !e.Retryable {
		t.Fatal("a 502 must stay retryable even when its body is mapped to bad_input")
	}
}

func TestMapErrorIntoStoresRetryAfterInMilliseconds(t *testing.T) {
	e := MapErrorInto(429, []byte(`{"error":{"message":"slow down","type":"rate_limit_error"}}`), 3*time.Second)
	if e.RetryAfter != 3000 {
		t.Fatalf("RetryAfter = %d, want 3000 milliseconds", e.RetryAfter)
	}
}

func TestMapErrorIntoKeepsCreditsExhaustedOverRawType(t *testing.T) {
	body := []byte(`{"error":{"message":"included usage is exhausted and no prepaid credits remain","type":"insufficient_quota","code":"credits_exhausted","param":null}}`)
	e := MapErrorInto(402, body, 0)
	if e.Code != Protocol.CodeCreditsExhausted {
		t.Fatalf("code = %q, want credits_exhausted", e.Code)
	}
	if e.Retryable {
		t.Fatal("credits exhausted must not be retryable")
	}
}

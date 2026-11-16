package OpenAI

import (
	"testing"

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

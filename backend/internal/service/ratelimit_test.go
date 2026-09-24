package service

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

func TestParseResetTime(t *testing.T) {
	now := time.Now()

	// 1. Relative seconds (typical GoCardless / proxy response, e.g. "60")
	parsed := ParseResetTime("60")
	if parsed == nil {
		t.Fatalf("expected non-nil for relative seconds")
	}
	diff := parsed.Sub(now)
	if diff < 58*time.Second || diff > 62*time.Second {
		t.Errorf("expected ~60s in future, got %v", diff)
	}

	// 2. Epoch timestamp
	futureEpoch := now.Add(2 * time.Hour).Unix()
	parsedEpoch := ParseResetTime(fmt.Sprintf("%d", futureEpoch))
	if parsedEpoch == nil {
		t.Fatalf("expected non-nil for epoch timestamp")
	}
	if parsedEpoch.Before(now) {
		t.Errorf("expected future time for epoch, got %v", parsedEpoch)
	}

	// 3. RFC3339
	parsedRFC := ParseResetTime(time.Now().Add(3600 * time.Second).Format("2006-01-02T15:04:05Z07:00"))
	if parsedRFC == nil {
		t.Fatalf("expected non-nil for RFC3339")
	}

	// 4. RFC1123 / HTTP Date
	httpDate := now.Add(10 * time.Minute).Format(http.TimeFormat)
	parsedHTTP := ParseResetTime(httpDate)
	if parsedHTTP == nil {
		t.Fatalf("expected non-nil for HTTP date format")
	}
	if parsedHTTP.Before(now) {
		t.Errorf("expected future time, got %v", parsedHTTP)
	}

	// 5. Invalid input
	if ParseResetTime("") != nil {
		t.Errorf("expected nil for empty string")
	}
	if ParseResetTime("invalid-not-a-date") != nil {
		t.Errorf("expected nil for invalid string")
	}
}

func TestTrading212ExtractRateLimit(t *testing.T) {
	t212 := NewTrading212Service()

	// 429 with retry-after header
	h := http.Header{}
	h.Set("Retry-After", "30")
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     h,
	}

	bu := t212.ExtractRateLimit(resp)
	if bu == nil {
		t.Fatalf("expected non-nil backoff for 429 with Retry-After")
	}
	if bu.Before(time.Now()) {
		t.Errorf("expected backoff time to be in future, got %v", bu)
	}

	// 200 with remaining = 0
	h2 := http.Header{}
	h2.Set("x-ratelimit-remaining", "0")
	h2.Set("x-ratelimit-reset", "45")
	resp2 := &http.Response{
		StatusCode: http.StatusOK,
		Header:     h2,
	}

	bu2 := t212.ExtractRateLimit(resp2)
	if bu2 == nil {
		t.Fatalf("expected non-nil backoff when remaining is 0")
	}

	// 200 with remaining = 5
	h3 := http.Header{}
	h3.Set("x-ratelimit-remaining", "5")
	h3.Set("x-ratelimit-reset", "45")
	resp3 := &http.Response{
		StatusCode: http.StatusOK,
		Header:     h3,
	}

	bu3 := t212.ExtractRateLimit(resp3)
	if bu3 != nil {
		t.Fatalf("expected nil backoff when remaining is 5, got %v", bu3)
	}
}

func TestGoCardlessExtractRateLimit(t *testing.T) {
	gc := NewGoCardlessService()

	// 429 with HTTP_X_RATELIMIT_RESET = 120
	h := http.Header{}
	h.Set("HTTP_X_RATELIMIT_RESET", "120")
	resp := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Header:     h,
	}

	bu := gc.ExtractRateLimit(resp)
	if bu == nil {
		t.Fatalf("expected non-nil backoff for GoCardless 429")
	}
	if bu.Before(time.Now().Add(100 * time.Second)) {
		t.Errorf("expected ~120s backoff, got %v", bu)
	}
}

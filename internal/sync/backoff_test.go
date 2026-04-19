// internal/sync/backoff_test.go
package sync

import (
	"testing"
	"time"
)

func TestClassifyReason(t *testing.T) {
	cases := []struct {
		reason string
		want   string
	}{
		{"cloud_rejected_retryable", CategoryTransient},
		{"cloud_rejected_permanent", CategoryPermanent},
		{"load_error", CategoryPermanent},
		{"size_limit_exceeded", CategoryPermanent},
		{"cloud_inconsistent_response", CategoryTransient},
		{"", CategoryTransient},                           // empty → transient
		{"some_future_unknown_reason", CategoryTransient}, // unknown → transient
		{"NETWORK_ERROR", CategoryTransient},              // arbitrary unknown
	}
	for _, tc := range cases {
		got := ClassifyReason(tc.reason)
		if got != tc.want {
			t.Errorf("ClassifyReason(%q): got %q, want %q", tc.reason, got, tc.want)
		}
	}
}

func TestNextTransientAttemptAt_Progression(t *testing.T) {
	last := time.Date(2026, 4, 19, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		attempts int
		want     time.Duration
	}{
		{1, 24 * time.Hour},        // 24h * 2^0
		{2, 48 * time.Hour},        // 24h * 2^1
		{3, 96 * time.Hour},        // 24h * 2^2
		{4, 192 * time.Hour},       // 24h * 2^3
		{5, 14 * 24 * time.Hour},   // 24h * 2^4 = 384h, capped at 14d=336h
		{6, 14 * 24 * time.Hour},   // capped
		{100, 14 * 24 * time.Hour}, // capped, no overflow
	}
	for _, tc := range cases {
		got := NextTransientAttemptAt(tc.attempts, last)
		if !got.Equal(last.Add(tc.want)) {
			t.Errorf("attempts=%d: got %v (delta %v), want delta %v",
				tc.attempts, got, got.Sub(last), tc.want)
		}
	}
}

// internal/sync/backoff.go
package sync

import "time"

// MaxBackoff caps the transient retry interval.
const MaxBackoff = 14 * 24 * time.Hour

// BaseBackoff is the first transient retry interval (attempts=1).
const BaseBackoff = 24 * time.Hour

// permanentReasons enumerates client- and Cloud-recognized reason strings that
// must never be retried automatically (no progress until the session is edited
// locally or the operator explicitly resets the entry).
var permanentReasons = map[string]bool{
	"cloud_rejected_permanent": true,
	"load_error":               true,
	"size_limit_exceeded":      true,
}

// transientReasons enumerates reason strings that are explicitly transient.
// Anything not in either set falls through to transient by default
// (conservative: bounded retry rather than silent permanent drop).
var transientReasons = map[string]bool{
	"cloud_rejected_retryable":    true,
	"cloud_inconsistent_response": true,
}

// ClassifyReason maps a rejection reason to a category. Unknown or empty
// reasons default to transient — see spec "Reason classification" table for
// the rationale (avoid silent permanent drops on future Cloud reason additions).
func ClassifyReason(reason string) string {
	if permanentReasons[reason] {
		return CategoryPermanent
	}
	if transientReasons[reason] {
		return CategoryTransient
	}
	return CategoryTransient
}

// NextTransientAttemptAt computes the next retry time for a transient failure
// after the given number of attempts. Backoff doubles up to MaxBackoff.
//
// attempts must be >= 1. attempts=1 means "we just made the first attempt and
// it failed; schedule the second one BaseBackoff later."
func NextTransientAttemptAt(attempts int, lastAttempt time.Time) time.Time {
	if attempts < 1 {
		attempts = 1
	}
	delta := BaseBackoff
	// Double up to MaxBackoff. Iterate to avoid overflow on huge attempt counts.
	for i := 1; i < attempts; i++ {
		delta *= 2
		if delta >= MaxBackoff {
			delta = MaxBackoff
			break
		}
	}
	return lastAttempt.Add(delta)
}

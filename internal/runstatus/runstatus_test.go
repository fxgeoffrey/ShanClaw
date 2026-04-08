package runstatus

import (
	"context"
	"errors"
	"testing"
)

func TestCodeFromError_Context(t *testing.T) {
	if got := CodeFromError(context.Canceled); got != CodeUserCancelled {
		t.Fatalf("expected %q, got %q", CodeUserCancelled, got)
	}
	if got := CodeFromError(context.DeadlineExceeded); got != CodeDeadlineExceeded {
		t.Fatalf("expected %q, got %q", CodeDeadlineExceeded, got)
	}
}

func TestCodeFromError_ClassifiesProviderFailures(t *testing.T) {
	tests := []struct {
		err  error
		want Code
	}{
		{errors.New("API returned 429"), CodeRateLimited},
		{errors.New("API returned 529 overloaded"), CodeProviderOverloaded},
		{errors.New("API returned 503"), CodeServiceTemporaryError},
		{errors.New("request failed: upstream disconnected"), CodeNetworkInterrupted},
	}

	for _, tc := range tests {
		if got := CodeFromError(tc.err); got != tc.want {
			t.Fatalf("error %q: expected %q, got %q", tc.err, tc.want, got)
		}
	}
}

func TestIsFriendlyMessage(t *testing.T) {
	for code := range friendlyMessages {
		if !IsFriendlyMessage(FriendlyMessage(code)) {
			t.Fatalf("expected friendly message for %q to be recognized", code)
		}
	}
	if IsFriendlyMessage("plain user text") {
		t.Fatal("unexpected friendly-message match for plain text")
	}
}

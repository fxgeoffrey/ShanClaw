package agent

import (
	"testing"
	"time"
)

func TestSuggestionState_SetAndGet(t *testing.T) {
	s := NewSuggestionState()
	s.Set("sess1", "fix the bug", time.Now())

	got, ok := s.Get("sess1")
	if !ok {
		t.Fatal("expected suggestion for sess1")
	}
	if got.Text != "fix the bug" {
		t.Errorf("Text = %q, want %q", got.Text, "fix the bug")
	}
	if got.AcceptedAt != nil {
		t.Error("AcceptedAt should be nil initially")
	}
}

func TestSuggestionState_Clear(t *testing.T) {
	s := NewSuggestionState()
	s.Set("sess1", "fix the bug", time.Now())
	s.Clear("sess1")

	if _, ok := s.Get("sess1"); ok {
		t.Error("expected sess1 to be cleared")
	}
}

func TestSuggestionState_MarkAccepted(t *testing.T) {
	s := NewSuggestionState()
	s.Set("sess1", "fix the bug", time.Now())
	if !s.MarkAccepted("sess1") {
		t.Fatal("MarkAccepted returned false on existing session")
	}
	got, _ := s.Get("sess1")
	if got.AcceptedAt == nil {
		t.Error("AcceptedAt should be set after MarkAccepted")
	}
}

func TestSuggestionState_MarkAccepted_AbsentSession(t *testing.T) {
	s := NewSuggestionState()
	if s.MarkAccepted("no-such") {
		t.Error("MarkAccepted should return false for unknown session")
	}
}

func TestSuggestionState_SetSpeculation(t *testing.T) {
	s := NewSuggestionState()
	s.Set("sess1", "fix the bug", time.Now())
	s.SetSpeculation("sess1", "fix the bug", "Here's the fix: ...")

	got, _ := s.Get("sess1")
	if got.SpeculationText != "Here's the fix: ..." {
		t.Errorf("SpeculationText = %q, want %q", got.SpeculationText, "Here's the fix: ...")
	}
}

func TestSuggestionState_SetSpeculation_StaleIgnored(t *testing.T) {
	// If user already moved on to a new suggestion, late-arriving speculation
	// for the old suggestion must not overwrite current state.
	s := NewSuggestionState()
	s.Set("sess1", "first", time.Now())
	s.Set("sess1", "second", time.Now())
	// Late speculation for "first" — must be ignored
	s.SetSpeculation("sess1", "first", "stale response")

	got, _ := s.Get("sess1")
	if got.SpeculationText != "" {
		t.Errorf("expected stale speculation to be ignored, got %q", got.SpeculationText)
	}
}

func TestSuggestionState_GetReturnsCopy(t *testing.T) {
	// Get must return a copy so callers can't mutate internal state via the pointer.
	s := NewSuggestionState()
	s.Set("sess1", "original", time.Now())
	got, _ := s.Get("sess1")
	got.Text = "mutated"

	again, _ := s.Get("sess1")
	if again.Text != "original" {
		t.Errorf("Get returned a live reference: external mutation visible (%q)", again.Text)
	}
}

func TestSuggestionState_ConcurrentSafe(t *testing.T) {
	// Smoke test for the sync.RWMutex — must not race under -race detector.
	s := NewSuggestionState()
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			s.Set("sess1", "x", time.Now())
		}
		done <- struct{}{}
	}()
	go func() {
		for i := 0; i < 1000; i++ {
			_, _ = s.Get("sess1")
		}
		done <- struct{}{}
	}()
	<-done
	<-done
}

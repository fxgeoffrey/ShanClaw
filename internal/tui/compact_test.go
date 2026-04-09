package tui

import (
	"strings"
	"testing"
)

func TestCompact_TooShort(t *testing.T) {
	m := newCommandTestModel(t)
	// Session has 0 messages — too short
	result := m.runCompact("")()
	if result.err == nil {
		t.Error("expected error for too-short conversation")
	}
	if result.err != nil && !strings.Contains(result.err.Error(), "too short") {
		t.Errorf("expected 'too short' error, got: %v", result.err)
	}
}

func TestFormatCompactResult(t *testing.T) {
	msg := compactDoneMsg{
		beforeTokens: 50000,
		afterTokens:  8000,
		summary:      "User worked on TUI improvements",
	}
	result := formatCompactResult(msg)
	if !strings.Contains(result, "50,000") || !strings.Contains(result, "8,000") {
		t.Errorf("expected formatted token counts in result: %s", result)
	}
	if !strings.Contains(result, "TUI improvements") {
		t.Errorf("expected summary text in result: %s", result)
	}
}

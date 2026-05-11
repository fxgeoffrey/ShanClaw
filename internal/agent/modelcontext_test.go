package agent

import "testing"

func TestLookupModelContextWindow(t *testing.T) {
	cases := []struct {
		name     string
		model    string
		wantCW   int
		wantOK   bool
	}{
		{"empty", "", 0, false},
		{"unknown", "claude-future-99", 0, false},
		{"sonnet 4.6 dateless 1M", "claude-sonnet-4-6", 1_000_000, true},
		{"opus 4.6 dateless 1M", "claude-opus-4-6", 1_000_000, true},
		{"opus 4.7 dateless 1M", "claude-opus-4-7", 1_000_000, true},
		{"sonnet 4.5 dated 200K", "claude-sonnet-4-5-20250929", 200_000, true},
		{"haiku 4.5 dated 200K", "claude-haiku-4-5-20251001", 200_000, true},
		{"sonnet 4 deprecated 200K", "claude-sonnet-4-20250514", 200_000, true},
		{"future dated sonnet 4.6 via prefix", "claude-sonnet-4-6-20260301", 1_000_000, true},
		{"future dated opus 4.7 via prefix", "claude-opus-4-7-20260601", 1_000_000, true},
		{"gpt-5.1 400K", "gpt-5.1", 400_000, true},
		{"gpt-4.1 dated 128K", "gpt-4.1-2025-04-14", 128_000, true},
		{"gemini 2.5 pro 1M+", "gemini-2.5-pro", 1_048_576, true},
		{"grok 2M", "grok-4-1-fast-reasoning", 2_000_000, true},
		// Guard against accidental prefix collisions.
		{"sonnet 4.6 numeric suffix is NOT a dated variant", "claude-sonnet-4-60", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cw, ok := LookupModelContextWindow(tc.model)
			if ok != tc.wantOK {
				t.Fatalf("ok=%v want=%v", ok, tc.wantOK)
			}
			if cw != tc.wantCW {
				t.Fatalf("cw=%d want=%d", cw, tc.wantCW)
			}
		})
	}
}

func TestMaybeAutoAdjustContextWindow(t *testing.T) {
	t.Run("unlocked: known model bumps window", func(t *testing.T) {
		a := &AgentLoop{contextWindow: 200_000}
		a.maybeAutoAdjustContextWindow("claude-sonnet-4-6")
		if a.contextWindow != 1_000_000 {
			t.Fatalf("expected auto-bump to 1M, got %d", a.contextWindow)
		}
	})

	t.Run("locked: known model is ignored", func(t *testing.T) {
		a := &AgentLoop{contextWindow: 200_000}
		a.SetContextWindowExplicit(200_000)
		a.maybeAutoAdjustContextWindow("claude-sonnet-4-6")
		if a.contextWindow != 200_000 {
			t.Fatalf("explicit lock violated: got %d", a.contextWindow)
		}
	})

	t.Run("unlocked: unknown model leaves window untouched", func(t *testing.T) {
		a := &AgentLoop{contextWindow: 200_000}
		a.maybeAutoAdjustContextWindow("totally-made-up-model")
		if a.contextWindow != 200_000 {
			t.Fatalf("unknown should not change window: got %d", a.contextWindow)
		}
	})

	t.Run("unlocked: empty model name is no-op", func(t *testing.T) {
		a := &AgentLoop{contextWindow: 200_000}
		a.maybeAutoAdjustContextWindow("")
		if a.contextWindow != 200_000 {
			t.Fatalf("empty model should be no-op: got %d", a.contextWindow)
		}
	})

	t.Run("unlocked: same value is no-op", func(t *testing.T) {
		a := &AgentLoop{contextWindow: 1_000_000}
		a.maybeAutoAdjustContextWindow("claude-sonnet-4-6")
		if a.contextWindow != 1_000_000 {
			t.Fatalf("same value churn: got %d", a.contextWindow)
		}
	})

	t.Run("unlocked: switch from large to small model shrinks window", func(t *testing.T) {
		a := &AgentLoop{contextWindow: 1_000_000}
		a.maybeAutoAdjustContextWindow("claude-haiku-4-5-20251001")
		if a.contextWindow != 200_000 {
			t.Fatalf("expected shrink to 200K, got %d", a.contextWindow)
		}
	})
}

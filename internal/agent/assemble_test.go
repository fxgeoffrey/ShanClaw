package agent

import (
	"strings"
	"testing"

	"github.com/Kocoro-lab/ShanClaw/internal/prompt"
)

func TestAssembleUserMessage_CacheBreakRegression(t *testing.T) {
	t.Run("empty stable omits marker", func(t *testing.T) {
		result := assembleUserMessage(prompt.PromptParts{
			StableContext:  "",
			VolatileContext: "current date: 2026-04-03",
		}, "hello")
		if strings.Contains(result, "cache_break") {
			t.Error("cache_break should not appear when StableContext is empty")
		}
	})

	t.Run("non-empty stable includes marker", func(t *testing.T) {
		result := assembleUserMessage(prompt.PromptParts{
			StableContext:  "system instructions",
			VolatileContext: "current date: 2026-04-03",
		}, "hello")
		if !strings.Contains(result, "cache_break") {
			t.Error("cache_break should appear when StableContext is non-empty")
		}
	})

	t.Run("marker separates stable from volatile", func(t *testing.T) {
		result := assembleUserMessage(prompt.PromptParts{
			StableContext:  "stable-prefix",
			VolatileContext: "volatile-suffix",
		}, "user-query")

		idx := strings.Index(result, "<!-- cache_break -->")
		if idx < 0 {
			t.Fatal("marker not found")
		}
		if !strings.Contains(result[:idx], "stable-prefix") {
			t.Error("stable content should be before marker")
		}
		if !strings.Contains(result[idx:], "volatile-suffix") {
			t.Error("volatile content should be after marker")
		}
		if !strings.HasSuffix(result, "user-query") {
			t.Error("user message should be at the end")
		}
	})
}

package agent

import "strings"

// modelContextWindow maps known model IDs to their context window in tokens.
//
// Source of truth: Anthropic / OpenAI / Google / xAI official model docs and
// shannon-cloud/config/models.yaml. Update both when bumping a window.
//
// Used by AgentLoop.reportLLMUsage to auto-adjust the compaction threshold
// based on the model that actually served the request (read from
// CompletionResponse.Model). Auto-adjust only applies when the user did not
// explicitly configure agent.context_window — see SetContextWindowExplicit.
//
// Unknown models leave contextWindow untouched (graceful degradation).
var modelContextWindow = map[string]int{
	// --- Anthropic (1M-context: GA, no beta header, standard pricing) ---
	"claude-sonnet-4-6":          1_000_000,
	"claude-opus-4-6":            1_000_000,
	"claude-opus-4-7":            1_000_000,
	"claude-mythos-preview":      1_000_000,

	// --- Anthropic (200K) ---
	"claude-sonnet-4-5-20250929": 200_000,
	"claude-haiku-4-5-20251001":  200_000,
	"claude-opus-4-5-20251101":   200_000,
	"claude-opus-4-1-20250805":   200_000,
	"claude-sonnet-4-20250514":   200_000,
	"claude-opus-4-20250514":     200_000,

	// --- OpenAI ---
	"gpt-5.1":                400_000,
	"gpt-5.1-chat-latest":    400_000,
	"gpt-5-pro-2025-10-06":   400_000,
	"gpt-5-mini-2025-08-07":  400_000,
	"gpt-5-nano-2025-08-07":  400_000,
	"gpt-4.1-2025-04-14":     128_000,

	// --- Google Gemini ---
	"gemini-3-pro-preview":  1_000_000,
	"gemini-2.5-pro":        1_048_576,
	"gemini-2.5-flash":      1_048_576,
	"gemini-2.5-flash-lite": 1_048_576,
	"gemini-2.0-flash":      1_048_576,
	"gemini-2.0-flash-lite": 1_048_576,

	// --- xAI Grok ---
	"grok-4-1-fast-non-reasoning": 2_000_000,
	"grok-4-1-fast-reasoning":     2_000_000,
	"grok-4.20-0309-reasoning":    2_000_000,

	// --- Others routed by Shannon Cloud (medium/large tiers) ---
	"kimi-k2-turbo-preview": 256_000,
	"kimi-k2.5":             256_000,
	"kimi-k2-thinking":      256_000,
}

// modelContextWindowPrefix handles forward-compat for dated variants of
// dateless model IDs (e.g., a future "claude-sonnet-4-6-20260301" snapshot
// of claude-sonnet-4-6). Only prefixes that we are confident represent a
// stable family lineage belong here — exact map above wins on collision.
var modelContextWindowPrefix = map[string]int{
	"claude-sonnet-4-6-": 1_000_000,
	"claude-opus-4-6-":   1_000_000,
	"claude-opus-4-7-":   1_000_000,
}

// LookupModelContextWindow returns the known context window for a model ID
// (including future dated variants of known dateless families). Returns
// (0, false) when the model is unknown — callers should leave existing
// contextWindow untouched in that case.
func LookupModelContextWindow(modelID string) (int, bool) {
	if modelID == "" {
		return 0, false
	}
	if v, ok := modelContextWindow[modelID]; ok {
		return v, true
	}
	for prefix, v := range modelContextWindowPrefix {
		if strings.HasPrefix(modelID, prefix) {
			return v, true
		}
	}
	return 0, false
}

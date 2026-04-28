package client

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestContentBlock_CompressedTier_NotSerialized guards the cache-stability
// invariant: the agent-loop's CompressedTier marker is internal state and
// must never appear on the wire. If it leaks, Anthropic's prompt-cache
// prefix matcher would treat its presence/absence as drift and re-tag every
// turn. See docs/issues/cache-message-prefix-invalidation.md.
func TestContentBlock_CompressedTier_NotSerialized(t *testing.T) {
	cases := []struct {
		name string
		tier int
	}{
		{"uncompressed", 0},
		{"tier2", 2},
		{"tier1", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cb := ContentBlock{
				Type:           "tool_result",
				ToolUseID:      "tc1",
				ToolContent:    "ok",
				CompressedTier: c.tier,
			}
			raw, err := json.Marshal(cb)
			if err != nil {
				t.Fatalf("marshal failed: %v", err)
			}
			s := strings.ToLower(string(raw))
			for _, k := range []string{"compressedtier", "compressed_tier"} {
				if strings.Contains(s, k) {
					t.Errorf("CompressedTier leaked to wire bytes: %s", raw)
				}
			}
		})
	}
}

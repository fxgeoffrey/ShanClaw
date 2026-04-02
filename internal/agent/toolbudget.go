package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
)

const (
	// schemaTokenBudget is the maximum token budget we allow for tool schemas
	// before switching to deferred mode. 8000 tokens is roughly 28K chars of
	// compact schema JSON at the conservative 3.5 chars/token ratio.
	schemaTokenBudget = 8000

	// charsPerTokenSchema mirrors the context estimator's conservative ratio.
	charsPerTokenSchema = 3.5
)

// estimateSchemaTokens returns a heuristic token count for the named tool
// schemas using compact JSON serialization.
func estimateSchemaTokens(reg *ToolRegistry, names []string) int {
	if reg == nil || len(names) == 0 {
		return 0
	}

	total := 0
	for _, name := range names {
		t, ok := reg.Get(name)
		if !ok {
			continue
		}
		data, err := json.Marshal(buildToolSchema(t))
		if err != nil {
			continue
		}
		total += int(math.Ceil(float64(len(data)) / charsPerTokenSchema))
	}
	return total
}

// shouldDefer returns true when sending all named schemas would exceed budget.
func shouldDefer(reg *ToolRegistry, names []string, budget int) bool {
	if budget <= 0 {
		return false
	}
	return estimateSchemaTokens(reg, names) > budget
}

// toolSchemaFingerprint hashes the effective serialized tool schemas in
// deterministic name order. This is used to invalidate warmed deferred
// schemas whenever the real toolset changes.
func toolSchemaFingerprint(reg *ToolRegistry) string {
	if reg == nil {
		return ""
	}

	h := sha256.New()
	for _, name := range reg.SortedNames() {
		t, ok := reg.Get(name)
		if !ok {
			continue
		}
		data, err := json.Marshal(buildToolSchema(t))
		if err != nil {
			continue
		}
		_, _ = h.Write([]byte(name))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write(data)
		_, _ = h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Kocoro-lab/ShanClaw/internal/agent"
	"github.com/Kocoro-lab/ShanClaw/internal/memory"
)

// FallbackQuery is the legacy memory recall path the tool falls back to when
// the structured memory service (Kocoro Cloud memory sidecar) is unavailable.
// Daemon supplies an adapter wired to session.Manager.Search + a MEMORY.md
// grep; CLI/TUI supplies the same against their local resources.
type FallbackQuery interface {
	SessionKeyword(ctx context.Context, query string, limit int) ([]any, error)
	MemoryFileSnippet(ctx context.Context, query string) (string, error)
}

// MemoryTool exposes memory_recall to the agent loop. Service may be nil
// when the daemon's memory.Service failed to start, when provider is
// disabled, or in CLI/TUI attach paths where AttachPolicy returned
// ready=false. Fallback must always be supplied.
//
// Service is left as a typed *memory.Service for now; Task 16 introduces the
// MemoryQuerier interface so test stubs can substitute.
type MemoryTool struct {
	Service  *memory.Service
	Fallback FallbackQuery
}

type memoryArgs struct {
	Mode                string   `json:"mode"`
	AnchorMentions      []string `json:"anchor_mentions"`
	RelationConstraints []string `json:"relation_constraints,omitempty"`
	CandidateType       *string  `json:"candidate_type,omitempty"`
	ScopeFilter         []string `json:"scope_filter,omitempty"`
	TargetSlot          string   `json:"target_slot,omitempty"`
	TimeWindow          *string  `json:"time_window,omitempty"`
	EvidenceBudget      int      `json:"evidence_budget,omitempty"`
	ResultLimit         int      `json:"result_limit,omitempty"`
}

func (t *MemoryTool) Info() agent.ToolInfo {
	return agent.ToolInfo{
		Name:        "memory_recall",
		Description: "Recall facts learned from prior sessions. Use this to look up things like \"what caused X?\", \"what does the user prefer for Y?\", \"what did we decide about Z?\" before asking the user. Returns ranked candidates with evidence labels (observed > derived > text-search). Falls back to keyword search across past sessions when the structured memory is unavailable; results in that mode are flagged as lower-confidence.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"mode":                 map[string]any{"type": "string", "enum": []string{"direct_relation", "path_query", "typed_neighborhood"}},
				"anchor_mentions":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"relation_constraints": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"candidate_type":       map[string]any{"type": "string"},
				"scope_filter":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"target_slot":          map[string]any{"type": "string", "enum": []string{"head", "tail"}},
				"time_window":          map[string]any{"type": "string"},
				"evidence_budget":      map[string]any{"type": "integer"},
				"result_limit":         map[string]any{"type": "integer"},
			},
		},
		Required: []string{"anchor_mentions"},
	}
}

func (t *MemoryTool) RequiresApproval() bool     { return false }
func (t *MemoryTool) IsReadOnlyCall(string) bool { return true }

func (t *MemoryTool) Run(ctx context.Context, argsJSON string) (agent.ToolResult, error) {
	var a memoryArgs
	if err := json.Unmarshal([]byte(argsJSON), &a); err != nil {
		return agent.ToolResult{Content: fmt.Sprintf("invalid input: %v", err), IsError: true}, nil
	}
	if len(a.AnchorMentions) == 0 {
		return agent.ToolResult{Content: "anchor_mentions is required and must be non-empty", IsError: true}, nil
	}
	if a.Mode == "" {
		a.Mode = "direct_relation"
	}
	if a.ResultLimit <= 0 {
		a.ResultLimit = 10
	}
	if a.EvidenceBudget <= 0 {
		a.EvidenceBudget = 5
	}

	intent := memory.QueryIntent{
		Mode:                memory.QueryMode(a.Mode),
		AnchorMentions:      a.AnchorMentions,
		RelationConstraints: a.RelationConstraints,
		CandidateType:       a.CandidateType,
		ScopeFilter:         a.ScopeFilter,
		TargetSlot:          a.TargetSlot,
		TimeWindow:          a.TimeWindow,
		EvidenceBudget:      a.EvidenceBudget,
		ResultLimit:         a.ResultLimit,
	}
	return t.run(ctx, intent)
}

// run is the post-validation path. Task 16 replaces this with the full
// class-branch logic; for now we always fall back so the tool is wired
// end-to-end in advance of class branching.
func (t *MemoryTool) run(ctx context.Context, intent memory.QueryIntent) (agent.ToolResult, error) {
	return t.fallback("service_unavailable", "fallback")
}

// fallback returns the JSON-shaped fallback envelope per spec §5.4. Task 16
// will route ClassRetryable failures to source="fallback_after_retry" via
// this same function with different args.
func (t *MemoryTool) fallback(reason, source string) (agent.ToolResult, error) {
	out := map[string]any{
		"source":           source,
		"evidence_quality": "text_search",
		"bundle_version":   nil,
		"candidates":       []any{},
		"warnings":         []any{},
		"fallback_reason":  reason,
	}
	body, _ := json.Marshal(out)
	return agent.ToolResult{Content: string(body)}, nil
}

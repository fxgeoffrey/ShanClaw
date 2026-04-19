package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Kocoro-lab/ShanClaw/internal/agent"
	"github.com/Kocoro-lab/ShanClaw/internal/memory"
)

// MemoryQuerier is the subset of *memory.Service the tool consumes.
// Pulled out as an interface so tests can substitute a stub. The real
// memory.Service satisfies it via Status() and Query(ctx, intent).
type MemoryQuerier interface {
	Status() memory.ServiceStatus
	Query(ctx context.Context, intent memory.QueryIntent) (*memory.ResponseEnvelope, memory.ErrorClass, error)
}

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
type MemoryTool struct {
	Service  MemoryQuerier
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

// run is the post-validation path. Implements the four class branches
// from spec §5.3: ClassOK shapes structured candidates (with a degraded
// warning prefix when env.Reason == "degraded"), ClassRetryable does one
// inline retry after 500ms before falling back, ClassPermanent surfaces
// the envelope error as warnings with IsError=true, ClassUnavailable and
// any transport error fall back via the legacy keyword path.
func (t *MemoryTool) run(ctx context.Context, intent memory.QueryIntent) (agent.ToolResult, error) {
	if t.Service == nil || t.Service.Status() != memory.StatusReady {
		return t.fallback("service_unavailable", "fallback")
	}
	env, class, err := t.Service.Query(ctx, intent)
	if err != nil || class == memory.ClassUnavailable {
		return t.fallback("service_unavailable", "fallback")
	}
	if class == memory.ClassRetryable {
		// Spec §5.3: one inline retry after 500ms.
		select {
		case <-ctx.Done():
			return t.fallback("service_unavailable", "fallback")
		case <-time.After(500 * time.Millisecond):
		}
		env, class, err = t.Service.Query(ctx, intent)
		if err != nil || class == memory.ClassUnavailable || class == memory.ClassRetryable {
			return t.fallback("retryable_failed", "fallback_after_retry")
		}
	}
	if class == memory.ClassPermanent {
		return t.permanentResult(env), nil
	}
	return t.shapeResult(env), nil
}

func (t *MemoryTool) permanentResult(env *memory.ResponseEnvelope) agent.ToolResult {
	out := map[string]any{
		"source":           "memory_sidecar",
		"evidence_quality": "structured",
		"bundle_version":   env.BundleVersion,
		"candidates":       []any{},
		"warnings":         envelopeWarnings(env),
		"fallback_reason":  nil,
	}
	body, _ := json.Marshal(out)
	return agent.ToolResult{Content: string(body), IsError: true}
}

func (t *MemoryTool) shapeResult(env *memory.ResponseEnvelope) agent.ToolResult {
	quality := "structured"
	warnings := envelopeWarnings(env)
	if env.Reason == "degraded" {
		quality = "structured_degraded"
		warnings = append([]map[string]any{{
			"code":    "bundle_degraded",
			"message": "memory bundle degraded — results may be incomplete",
		}}, warnings...)
	}
	cands := make([]map[string]any, 0, len(env.Candidates))
	for _, c := range env.Candidates {
		m := map[string]any{
			"value":                c.Value,
			"score":                c.Score,
			"evidence":             c.Evidence,
			"supporting_event_ids": c.SupportingEventIDs,
		}
		if c.Scope != nil {
			m["scope"] = *c.Scope
		}
		if c.SupportCount != nil {
			m["support_count"] = *c.SupportCount
		}
		if c.DistinctSessionCount != nil {
			m["distinct_session_count"] = *c.DistinctSessionCount
		}
		cands = append(cands, m)
	}
	out := map[string]any{
		"source":           "memory_sidecar",
		"evidence_quality": quality,
		"bundle_version":   env.BundleVersion,
		"candidates":       cands,
		"warnings":         warnings,
		"fallback_reason":  nil,
	}
	body, _ := json.Marshal(out)
	return agent.ToolResult{Content: string(body)}
}

func envelopeWarnings(env *memory.ResponseEnvelope) []map[string]any {
	out := []map[string]any{}
	for _, w := range env.Warnings {
		out = append(out, map[string]any{"code": w.Code, "message": w.Message})
	}
	if env.Error != nil {
		out = append(out, map[string]any{
			"code":     env.Error.Code,
			"message":  env.Error.Message,
			"sub_code": env.Error.SubCode(),
		})
	}
	return out
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

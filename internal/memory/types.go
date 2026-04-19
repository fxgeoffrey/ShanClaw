// Package memory implements the ShanClaw-side client for the Kocoro Cloud
// memory feature: managed sidecar lifecycle, Cloud bundle pull, agent
// memory_recall tool with fallback. Wire schemas mirror the Kocoro Cloud
// memory sidecar HTTP contract.
package memory

import "time"

type QueryMode string

const (
	ModeDirectRelation    QueryMode = "direct_relation"
	ModePathQuery         QueryMode = "path_query"
	ModeTypedNeighborhood QueryMode = "typed_neighborhood"
)

type QueryIntent struct {
	Mode                QueryMode `json:"mode"`
	AnchorMentions      []string  `json:"anchor_mentions"`
	RelationConstraints []string  `json:"relation_constraints,omitempty"`
	CandidateType       *string   `json:"candidate_type,omitempty"`
	ScopeFilter         []string  `json:"scope_filter,omitempty"`
	TargetSlot          string    `json:"target_slot,omitempty"`
	TimeWindow          *string   `json:"time_window,omitempty"`
	EvidenceBudget      int       `json:"evidence_budget,omitempty"`
	ResultLimit         int       `json:"result_limit,omitempty"`
}

type QueryRequest struct {
	Intent    QueryIntent `json:"intent"`
	UserID    *string     `json:"user_id,omitempty"`
	RequestID *string     `json:"request_id,omitempty"`
}

type QueryCandidate struct {
	Value                string   `json:"value"`
	Score                float64  `json:"score"`
	Evidence             string   `json:"evidence"`
	SupportingEventIDs   []string `json:"supporting_event_ids"`
	SupportCount         *int     `json:"support_count,omitempty"`
	DistinctSessionCount *int     `json:"distinct_session_count,omitempty"`
	EntityID             *string  `json:"entity_id,omitempty"`
	Scope                *string  `json:"scope,omitempty"`
}

type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ErrorObject struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func (e *ErrorObject) SubCode() string {
	if e == nil || e.Details == nil {
		return ""
	}
	if v, ok := e.Details["sub_code"].(string); ok {
		return v
	}
	return ""
}

type ResponseEnvelope struct {
	ProtocolVersion int              `json:"protocol_version"`
	BundleVersion   string           `json:"bundle_version,omitempty"`
	BundleCreatedAt *time.Time       `json:"bundle_created_at,omitempty"`
	BundleDir       string           `json:"bundle_dir,omitempty"`
	RequestID       string           `json:"request_id"`
	Candidates      []QueryCandidate `json:"candidates"`
	Warnings        []Warning        `json:"warnings"`
	Reason          string           `json:"reason"`
	Error           *ErrorObject     `json:"error,omitempty"`
	LatencyMs       float64          `json:"latency_ms"`
}

type ReloadResponse struct {
	ProtocolVersion   int          `json:"protocol_version"`
	RequestID         string       `json:"request_id"`
	Swapped           bool         `json:"swapped"`
	Trigger           string       `json:"trigger"`
	Reason            string       `json:"reason"`
	PreviousBundleDir *string      `json:"previous_bundle_dir,omitempty"`
	CurrentBundleDir  *string      `json:"current_bundle_dir,omitempty"`
	ReloadDurationMs  float64      `json:"reload_duration_ms"`
	Warnings          []Warning    `json:"warnings"`
	Error             *ErrorObject `json:"error,omitempty"`
}

type HealthPayload struct {
	Ready             bool         `json:"ready"`
	Compatibility     string       `json:"compatibility"`
	BundleVersion     string       `json:"bundle_version,omitempty"`
	BundleCreatedAt   *time.Time   `json:"bundle_created_at,omitempty"`
	BundleDir         string       `json:"bundle_dir,omitempty"`
	LastReloadAgeSecs *float64     `json:"last_reload_age_secs,omitempty"`
	LastReloadTrigger *string      `json:"last_reload_trigger,omitempty"`
	ProtocolVersion   int          `json:"protocol_version"`
	UptimeSecs        float64      `json:"uptime_secs"`
	Error             *ErrorObject `json:"error,omitempty"`
	StatusMessage     string       `json:"status_message,omitempty"`
}

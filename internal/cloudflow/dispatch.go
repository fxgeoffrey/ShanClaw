// Package cloudflow runs Shannon Cloud Gateway workflows (research, swarm,
// auto-routing) and bridges Gateway SSE events to a daemon-style EventHandler.
//
// This package was extracted from internal/tools/cloud_delegate.go so the same
// pipeline can be invoked both from the agent loop (as a tool) and from the
// daemon HTTP layer (as a slash-command target).
package cloudflow

import (
	"context"
	"errors"

	"github.com/Kocoro-lab/ShanClaw/internal/agent"
	"github.com/Kocoro-lab/ShanClaw/internal/client"
)

// ErrGatewayNotConfigured is returned when Run is called without a Gateway.
// Callers should surface a user-readable message; the daemon HTTP path turns
// this into a 503-style SSE error event.
var ErrGatewayNotConfigured = errors.New("cloudflow: gateway not configured")

// Request describes a single cloud workflow run.
type Request struct {
	Gateway      *client.GatewayClient
	APIKey       string
	Query        string
	WorkflowType string         // "research", "swarm", "auto", or ""
	Strategy     string         // "quick" | "standard" | "deep" | "academic" — research only
	SessionID    string         // optional; passed to Gateway for correlation
	UserContext  string         // optional free-text context appended to the request
	ExtraContext map[string]any // optional; merged into TaskRequest.Context
}

// Result holds the final assistant message and accumulated cloud usage.
type Result struct {
	FinalText           string
	Usage               agent.TurnUsage
	WorkflowID          string
	TaskID              string
	FullResultConfirmed bool
}

// contextKeyOnWorkflowStarted is the unexported key used by WithOnWorkflowStarted.
type contextKeyOnWorkflowStarted struct{}

// OnWorkflowStartedFunc is invoked exactly once with the resolved workflow ID
// after Gateway accepts the submission. The daemon uses this to forward the
// workflow ID to its EventBus so other subscribers (Slack, LINE, webhook) can
// hand off subsequent stream events.
type OnWorkflowStartedFunc func(workflowID string)

// WithOnWorkflowStarted returns a child context that carries cb. Run calls cb
// after a successful SubmitTaskStream when present.
func WithOnWorkflowStarted(ctx context.Context, cb OnWorkflowStartedFunc) context.Context {
	return context.WithValue(ctx, contextKeyOnWorkflowStarted{}, cb)
}

// Run submits a Gateway task, streams its SSE events into handler via
// the OnCloudAgent / OnCloudProgress / OnCloudPlan / OnStreamDelta / OnUsage
// callbacks, and returns the final assistant text plus the workflow_id /
// task_id and a FullResultConfirmed flag (true when the API fallback
// returned a complete untruncated result, false when the SSE-only payload
// is the best we have).
//
// Callers that need to inject a workflow_started callback (so the daemon
// EventBus can hand a workflow ID to other subscribers) can place an
// OnWorkflowStartedFunc into ctx via WithOnWorkflowStarted.
func Run(ctx context.Context, req Request, handler agent.EventHandler) (Result, error) {
	if req.Gateway == nil {
		return Result{}, ErrGatewayNotConfigured
	}
	// TODO(A2): full implementation — extracted from cloud_delegate.go.
	return Result{}, errors.New("cloudflow.Run not implemented")
}

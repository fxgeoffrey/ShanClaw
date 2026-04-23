package daemon

import (
	"time"

	"github.com/Kocoro-lab/ShanClaw/internal/agent"
)

// multiHandler fans agent.EventHandler callbacks to multiple wrapped handlers.
//
// Propagation rules:
//   - Base methods (OnToolCall, OnText, OnUsage, etc.): call every wrapped handler in order.
//   - OnApprovalNeeded: call every wrapped handler; return the OR of all results.
//     This means any handler returning "approved" approves the call.
//   - SetSessionID (Task 8): propagate only to wrapped handlers that implement it.
//   - OnRunStatus (Task 9): propagate only to wrapped handlers that implement RunStatusHandler.
type multiHandler struct {
	handlers []agent.EventHandler
}

func (m *multiHandler) OnToolCall(name, args string) {
	for _, h := range m.handlers {
		h.OnToolCall(name, args)
	}
}

func (m *multiHandler) OnToolResult(name, args string, result agent.ToolResult, elapsed time.Duration) {
	for _, h := range m.handlers {
		h.OnToolResult(name, args, result, elapsed)
	}
}

func (m *multiHandler) OnText(text string) {
	for _, h := range m.handlers {
		h.OnText(text)
	}
}

func (m *multiHandler) OnStreamDelta(delta string) {
	for _, h := range m.handlers {
		h.OnStreamDelta(delta)
	}
}

func (m *multiHandler) OnApprovalNeeded(tool, args string) bool {
	approved := false
	for _, h := range m.handlers {
		if h.OnApprovalNeeded(tool, args) {
			approved = true
		}
	}
	return approved
}

func (m *multiHandler) OnUsage(u agent.TurnUsage) {
	for _, h := range m.handlers {
		h.OnUsage(u)
	}
}

func (m *multiHandler) OnCloudAgent(agentID, status, message string) {
	for _, h := range m.handlers {
		h.OnCloudAgent(agentID, status, message)
	}
}

func (m *multiHandler) OnCloudProgress(completed, total int) {
	for _, h := range m.handlers {
		h.OnCloudProgress(completed, total)
	}
}

func (m *multiHandler) OnCloudPlan(planType, content string, needsReview bool) {
	for _, h := range m.handlers {
		h.OnCloudPlan(planType, content, needsReview)
	}
}

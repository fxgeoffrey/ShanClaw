package daemon

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
)

// ApprovalDecision represents the user's response to a tool approval request.
type ApprovalDecision string

const (
	DecisionAllow       ApprovalDecision = "allow"
	DecisionDeny        ApprovalDecision = "deny"
	DecisionAlwaysAllow ApprovalDecision = "always_allow"
)

// ApprovalBroker mediates between the agent loop's OnApprovalNeeded and the WS
// client. It sends approval_request messages over WS and blocks until a
// matching approval_response arrives (or context is cancelled).
type ApprovalBroker struct {
	mu              sync.Mutex
	pending         map[string]chan ApprovalDecision
	toolAutoApprove map[string]bool // in-memory only, non-bash "always allow"
	sendFn          func(req ApprovalRequest) error
	onRequest       func(requestID, tool, args string)
	onRegister      func(requestID string) // called when a pending entry is created
	onDeregister    func(requestID string) // called when a pending entry is cleaned up
}

// NewApprovalBroker creates a broker. sendFn sends an approval_request over WS.
// It must be reconnect-safe (e.g., a method on *Client, not a closure over a conn).
func NewApprovalBroker(sendFn func(req ApprovalRequest) error) *ApprovalBroker {
	return &ApprovalBroker{
		pending:         make(map[string]chan ApprovalDecision),
		toolAutoApprove: make(map[string]bool),
		sendFn:          sendFn,
	}
}

// SetOnRequest sets a callback invoked when a new approval request is created,
// before sending it over WS. Used to emit EventApprovalRequest to SSE subscribers.
func (b *ApprovalBroker) SetOnRequest(fn func(requestID, tool, args string)) {
	b.onRequest = fn
}

// Request sends an approval_request and blocks until the response arrives
// or ctx is cancelled. Returns DecisionDeny if send fails or ctx is done.
func (b *ApprovalBroker) Request(ctx context.Context, channel, threadID, agent, tool, args string) ApprovalDecision {
	if b.IsToolAutoApproved(tool) {
		return DecisionAllow
	}

	reqID := generateRequestID()
	ch := make(chan ApprovalDecision, 1)

	b.mu.Lock()
	b.pending[reqID] = ch
	b.mu.Unlock()

	if b.onRegister != nil {
		b.onRegister(reqID)
	}

	defer func() {
		if b.onDeregister != nil {
			b.onDeregister(reqID)
		}
		b.mu.Lock()
		delete(b.pending, reqID)
		b.mu.Unlock()
	}()

	if b.onRequest != nil {
		b.onRequest(reqID, tool, args)
	}

	req := ApprovalRequest{
		Channel:   channel,
		ThreadID:  threadID,
		RequestID: reqID,
		Tool:      tool,
		Args:      args,
		Agent:     agent,
	}
	if err := b.sendFn(req); err != nil {
		return DecisionDeny
	}

	select {
	case decision := <-ch:
		return decision
	case <-ctx.Done():
		return DecisionDeny
	}
}

// Resolve delivers a decision to a pending request. No-op if requestID is
// not found (stale or duplicate response).
func (b *ApprovalBroker) Resolve(requestID string, decision ApprovalDecision) {
	b.mu.Lock()
	ch, ok := b.pending[requestID]
	if ok {
		delete(b.pending, requestID)
	}
	b.mu.Unlock()

	if ok {
		select {
		case ch <- decision:
		default:
		}
	}
}

// CancelAll sends DecisionDeny to all pending requests and clears the map.
// Called on WS disconnect to unblock all waiting goroutines.
func (b *ApprovalBroker) CancelAll() {
	b.mu.Lock()
	for id, ch := range b.pending {
		select {
		case ch <- DecisionDeny:
		default:
		}
		delete(b.pending, id)
	}
	b.mu.Unlock()
}

// SetToolAutoApprove marks a non-bash tool as auto-approved (in-memory only).
func (b *ApprovalBroker) SetToolAutoApprove(tool string) {
	b.mu.Lock()
	b.toolAutoApprove[tool] = true
	b.mu.Unlock()
}

// IsToolAutoApproved checks if a tool has been auto-approved via "Always Allow".
func (b *ApprovalBroker) IsToolAutoApproved(tool string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.toolAutoApprove[tool]
}

func generateRequestID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "apr_" + hex.EncodeToString(b)
}

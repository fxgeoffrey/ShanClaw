package daemon

import (
	"encoding/json"
	"sync"
)

// Event types emitted by the daemon.
const (
	EventMessageReceived  = "message_received"
	EventAgentReply       = "agent_reply"
	EventApprovalRequest  = "approval_request"
	EventApprovalResolved = "approval_resolved"
	EventAgentError       = "agent_error"
	EventHeartbeatAlert   = "heartbeat_alert"
	EventToolStatus       = "tool_status"
	EventCloudAgent       = "cloud_agent"
	EventCloudProgress    = "cloud_progress"
	EventCloudPlan        = "cloud_plan"
	EventNotification     = "notification"
)

// Event is a daemon lifecycle event pushed to SSE subscribers.
type Event struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// EventBus is a simple pub/sub bus for daemon events.
type EventBus struct {
	mu          sync.RWMutex
	subscribers map[<-chan Event]chan Event
}

// NewEventBus creates a new EventBus.
func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[<-chan Event]chan Event),
	}
}

// Subscribe returns a channel that receives all emitted events.
// Caller must call Unsubscribe when done.
func (b *EventBus) Subscribe() <-chan Event {
	ch := make(chan Event, 64)
	b.mu.Lock()
	b.subscribers[ch] = ch
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes a subscriber. No further events will be sent to ch.
// The channel is not closed; callers should stop reading after Unsubscribe.
func (b *EventBus) Unsubscribe(ch <-chan Event) {
	b.mu.Lock()
	delete(b.subscribers, ch)
	b.mu.Unlock()
}

// Emit sends an event to all subscribers. Non-blocking: if a subscriber's
// buffer is full, the event is dropped for that subscriber.
func (b *EventBus) Emit(evt Event) {
	_ = b.EmitTo(evt)
}

// EmitTo sends an event to all subscribers and returns the number of
// subscribers that actually accepted the event (i.e. had buffer space).
// Subscribers whose buffer was full are counted as drops. Callers that need
// to make a real delivery decision — e.g. the notify tool choosing between
// the Desktop path and the osascript fallback — should use this method; a
// zero return value means "nobody got the event, fall back".
//
// Known limitation: EmitTo cannot distinguish a Desktop client from, say, a
// curl session debugging /events. It only reports best-effort delivery to
// any current subscriber. Capability negotiation on the /events endpoint is
// tracked as future work if this becomes a real problem.
func (b *EventBus) EmitTo(evt Event) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	delivered := 0
	for _, ch := range b.subscribers {
		select {
		case ch <- evt:
			delivered++
		default:
			// subscriber too slow, drop
		}
	}
	return delivered
}

// HasSubscribers reports whether at least one subscriber is currently attached.
// Retained for callers that only need a cheap liveness check. New delivery
// decisions should prefer EmitTo's return value instead.
func (b *EventBus) HasSubscribers() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers) > 0
}

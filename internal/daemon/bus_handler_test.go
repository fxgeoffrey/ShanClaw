package daemon

import (
	"testing"
	"time"

	"github.com/Kocoro-lab/ShanClaw/internal/agent"
)

// Sanity check that busEventHandler satisfies both interfaces the agent loop
// cares about. A compile-time assertion would work too, but a runtime check
// plays nicely with the rest of the test file we're about to grow.
func TestBusEventHandlerImplementsInterfaces(t *testing.T) {
	var _ agent.EventHandler = (*busEventHandler)(nil)
	var _ agent.RunStatusHandler = (*busEventHandler)(nil)
}

func TestBusEventHandlerSetSessionID(t *testing.T) {
	h := &busEventHandler{}
	h.SetSessionID("sess_123")
	if h.sessionID != "sess_123" {
		t.Fatalf("sessionID = %q, want %q", h.sessionID, "sess_123")
	}
}

// newTestHandler returns a handler attached to a fresh bus; the caller uses
// bus.Subscribe() to drain events. Shared by the remaining tests in this file.
func newTestHandler(t *testing.T) (*busEventHandler, *EventBus) {
	t.Helper()
	bus := NewEventBus()
	deps := &ServerDeps{EventBus: bus}
	h := &busEventHandler{deps: deps, agent: "coding"}
	h.SetSessionID("sess_test")
	return h, bus
}

// drain reads up to `want` events from the bus subscription within 250ms;
// returns whatever it got. Callers assert length + contents.
func drain(t *testing.T, ch <-chan Event, want int) []Event {
	t.Helper()
	out := make([]Event, 0, want)
	deadline := time.After(250 * time.Millisecond)
	for len(out) < want {
		select {
		case evt := <-ch:
			out = append(out, evt)
		case <-deadline:
			return out
		}
	}
	return out
}

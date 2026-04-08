package daemon

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEventBusSubscribeReceive(t *testing.T) {
	bus := NewEventBus()
	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	bus.Emit(Event{Type: "agent_reply", Payload: json.RawMessage(`{"agent":"test"}`)})

	select {
	case evt := <-ch:
		if evt.Type != "agent_reply" {
			t.Fatalf("expected agent_reply, got %s", evt.Type)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestEventBusUnsubscribe(t *testing.T) {
	bus := NewEventBus()
	ch := bus.Subscribe()
	bus.Unsubscribe(ch)

	bus.Emit(Event{Type: "test", Payload: json.RawMessage(`{}`)})

	select {
	case <-ch:
		t.Fatal("should not receive after unsubscribe")
	case <-time.After(50 * time.Millisecond):
		// expected
	}
}

func TestEventBusFanOut(t *testing.T) {
	bus := NewEventBus()
	ch1 := bus.Subscribe()
	ch2 := bus.Subscribe()
	defer bus.Unsubscribe(ch1)
	defer bus.Unsubscribe(ch2)

	bus.Emit(Event{Type: "test", Payload: json.RawMessage(`{}`)})

	for _, ch := range []<-chan Event{ch1, ch2} {
		select {
		case evt := <-ch:
			if evt.Type != "test" {
				t.Fatalf("expected test, got %s", evt.Type)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout")
		}
	}
}

func TestEventBusSlowSubscriberNonBlocking(t *testing.T) {
	bus := NewEventBus()
	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	// Overflow the channel buffer (size 64)
	for i := 0; i < 128; i++ {
		bus.Emit(Event{Type: "flood", Payload: json.RawMessage(`{}`)})
	}

	// Emit should not block even with a full subscriber channel
	done := make(chan struct{})
	go func() {
		bus.Emit(Event{Type: "final", Payload: json.RawMessage(`{}`)})
		close(done)
	}()

	select {
	case <-done:
		// expected — Emit did not block
	case <-time.After(time.Second):
		t.Fatal("Emit blocked on slow subscriber")
	}
}

// EmitTo must report zero deliveries when the only subscriber's channel is
// full. The notify tool routing relies on this to fall back to osascript —
// if a stalled Desktop client silently dropped notifications, the agent's
// banner would never surface.
func TestEventBusEmitToReportsSlowSubscriberAsZeroDelivered(t *testing.T) {
	bus := NewEventBus()
	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	// Fill the 64-slot buffer exactly.
	for i := 0; i < 64; i++ {
		if delivered := bus.EmitTo(Event{Type: "flood", Payload: json.RawMessage(`{}`)}); delivered != 1 {
			t.Fatalf("fill iter %d: expected 1 delivery, got %d", i, delivered)
		}
	}

	// Next emit must be dropped for this subscriber; delivered count must be 0
	// so a notify-tool caller knows the fallback is needed.
	if delivered := bus.EmitTo(Event{Type: "overflow", Payload: json.RawMessage(`{}`)}); delivered != 0 {
		t.Fatalf("expected 0 deliveries after buffer fill, got %d", delivered)
	}
}

// EmitTo with no subscribers returns zero, so the notify tool falls back to
// osascript in headless mode.
func TestEventBusEmitToNoSubscribers(t *testing.T) {
	bus := NewEventBus()
	if delivered := bus.EmitTo(Event{Type: "orphan", Payload: json.RawMessage(`{}`)}); delivered != 0 {
		t.Errorf("expected 0 deliveries with no subscribers, got %d", delivered)
	}
}

// EmitTo returns the count of subscribers that actually received the event,
// not the total number attached. If one of two subscribers has a full buffer,
// EmitTo must return 1 — the call still counts as "delivered" for notify
// routing purposes because at least one Desktop client got the event.
func TestEventBusEmitToPartialDelivery(t *testing.T) {
	bus := NewEventBus()
	slow := bus.Subscribe()
	fast := bus.Subscribe()
	defer bus.Unsubscribe(slow)
	defer bus.Unsubscribe(fast)

	// Fill only the slow subscriber.
	for i := 0; i < 64; i++ {
		_ = bus.EmitTo(Event{Type: "fill", Payload: json.RawMessage(`{}`)})
	}
	// Drain the fast subscriber so it has room for the next emit.
	for i := 0; i < 64; i++ {
		<-fast
	}

	// Now slow is full, fast is empty. Next emit should deliver to fast only.
	delivered := bus.EmitTo(Event{Type: "partial", Payload: json.RawMessage(`{}`)})
	if delivered != 1 {
		t.Errorf("expected 1 delivery (to fast only), got %d", delivered)
	}
}

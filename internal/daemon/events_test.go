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

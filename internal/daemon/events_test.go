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

// --- Ring buffer tests ---

// Emitted events must receive monotonically increasing IDs starting from 1.
func TestEventBusAssignsIncrementingIDs(t *testing.T) {
	bus := NewEventBus()
	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	for i := 0; i < 5; i++ {
		bus.Emit(Event{Type: "test", Payload: json.RawMessage(`{}`)})
	}

	for i := uint64(1); i <= 5; i++ {
		evt := <-ch
		if evt.ID != i {
			t.Fatalf("expected ID %d, got %d", i, evt.ID)
		}
	}
}

// EventsSince returns all events with ID > lastID from the ring buffer.
func TestEventBusEventsSince(t *testing.T) {
	bus := NewEventBus()

	// Emit 10 events (IDs 1..10)
	for i := 0; i < 10; i++ {
		bus.Emit(Event{Type: "test", Payload: json.RawMessage(`{}`)})
	}

	// Ask for events since ID 7 → should get IDs 8, 9, 10
	events := bus.EventsSince(7)
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	for i, evt := range events {
		expected := uint64(8 + i)
		if evt.ID != expected {
			t.Fatalf("events[%d]: expected ID %d, got %d", i, expected, evt.ID)
		}
	}
}

// EventsSince(0) returns all buffered events.
func TestEventBusEventsSinceZero(t *testing.T) {
	bus := NewEventBus()
	for i := 0; i < 5; i++ {
		bus.Emit(Event{Type: "test", Payload: json.RawMessage(`{}`)})
	}

	events := bus.EventsSince(0)
	if len(events) != 5 {
		t.Fatalf("expected 5 events, got %d", len(events))
	}
}

// EventsSince on empty bus returns nil.
func TestEventBusEventsSinceEmpty(t *testing.T) {
	bus := NewEventBus()
	events := bus.EventsSince(0)
	if events != nil {
		t.Fatalf("expected nil, got %v", events)
	}
}

// EventsSince with lastID >= nextID returns nil (client is up to date).
func TestEventBusEventsSinceUpToDate(t *testing.T) {
	bus := NewEventBus()
	for i := 0; i < 5; i++ {
		bus.Emit(Event{Type: "test", Payload: json.RawMessage(`{}`)})
	}

	events := bus.EventsSince(5)
	if events != nil {
		t.Fatalf("expected nil for up-to-date client, got %d events", len(events))
	}

	events = bus.EventsSince(999)
	if events != nil {
		t.Fatalf("expected nil for future ID, got %d events", len(events))
	}
}

// When ring buffer wraps, old events are overwritten and EventsSince
// only returns events still in the buffer.
func TestEventBusRingBufferWrap(t *testing.T) {
	bus := NewEventBus()

	// Emit ringSize + 100 events → first 100 are overwritten
	total := ringSize + 100
	for i := 0; i < total; i++ {
		bus.Emit(Event{Type: "test", Payload: json.RawMessage(`{}`)})
	}

	// Ask for events since 0 → should only get ringSize events (the newest ones)
	events := bus.EventsSince(0)
	if len(events) != ringSize {
		t.Fatalf("expected %d events after wrap, got %d", ringSize, len(events))
	}

	// First event in buffer should be ID 101 (the 100 oldest were overwritten)
	if events[0].ID != 101 {
		t.Fatalf("expected first buffered ID to be 101, got %d", events[0].ID)
	}

	// Last event should be ID ringSize+100
	if events[len(events)-1].ID != uint64(total) {
		t.Fatalf("expected last ID to be %d, got %d", total, events[len(events)-1].ID)
	}

	// Ask for events since an ID that was already overwritten → should return
	// all buffered events (best effort)
	events = bus.EventsSince(50)
	if len(events) != ringSize {
		t.Fatalf("expected %d events for overwritten lastID, got %d", ringSize, len(events))
	}
}

// SubscribeWithReplay must atomically return missed events AND register the
// subscriber, so no events are lost between replay and live delivery.
func TestEventBusSubscribeWithReplayAtomic(t *testing.T) {
	bus := NewEventBus()

	// Emit 3 events (IDs 1..3) before subscribing.
	for i := 0; i < 3; i++ {
		bus.Emit(Event{Type: "pre", Payload: json.RawMessage(`{}`)})
	}

	// Subscribe with replay from ID 1 → should get IDs 2, 3
	missed, ch := bus.SubscribeWithReplay(1)
	defer bus.Unsubscribe(ch)

	if len(missed) != 2 {
		t.Fatalf("expected 2 missed events, got %d", len(missed))
	}
	if missed[0].ID != 2 || missed[1].ID != 3 {
		t.Fatalf("expected IDs [2, 3], got [%d, %d]", missed[0].ID, missed[1].ID)
	}

	// Emit a live event → must arrive on the channel (no gap)
	bus.Emit(Event{Type: "live", Payload: json.RawMessage(`{}`)})

	select {
	case evt := <-ch:
		if evt.ID != 4 {
			t.Fatalf("expected live event ID 4, got %d", evt.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for live event after SubscribeWithReplay")
	}
}

// SubscribeWithReplay(0) returns all buffered events.
func TestEventBusSubscribeWithReplayFromZero(t *testing.T) {
	bus := NewEventBus()
	for i := 0; i < 5; i++ {
		bus.Emit(Event{Type: "test", Payload: json.RawMessage(`{}`)})
	}

	missed, ch := bus.SubscribeWithReplay(0)
	defer bus.Unsubscribe(ch)

	if len(missed) != 5 {
		t.Fatalf("expected 5 missed events, got %d", len(missed))
	}
}

// Undelivered notification events must NOT be stored in the ring buffer,
// because the caller falls back to osascript and replaying them on reconnect
// would produce a duplicate banner.
func TestEventBusNotificationNotBufferedWhenUndelivered(t *testing.T) {
	bus := NewEventBus()
	// No subscribers → EmitTo returns 0, notification should be excluded from ring.
	bus.EmitTo(Event{Type: EventNotification, Payload: json.RawMessage(`{"title":"test"}`)})

	events := bus.EventsSince(0)
	if len(events) != 0 {
		t.Fatalf("expected 0 buffered events for undelivered notification, got %d", len(events))
	}
}

// Delivered notification events ARE stored in the ring buffer for replay.
func TestEventBusNotificationBufferedWhenDelivered(t *testing.T) {
	bus := NewEventBus()
	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	bus.EmitTo(Event{Type: EventNotification, Payload: json.RawMessage(`{"title":"test"}`)})
	<-ch // drain

	events := bus.EventsSince(0)
	if len(events) != 1 {
		t.Fatalf("expected 1 buffered notification event, got %d", len(events))
	}
}

// Non-notification events are always buffered regardless of delivery.
func TestEventBusNonNotificationAlwaysBuffered(t *testing.T) {
	bus := NewEventBus()
	// No subscribers
	bus.EmitTo(Event{Type: EventAgentReply, Payload: json.RawMessage(`{}`)})

	events := bus.EventsSince(0)
	if len(events) != 1 {
		t.Fatalf("expected 1 buffered event, got %d", len(events))
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

func TestEventUsageConstant(t *testing.T) {
	if EventUsage != "usage" {
		t.Fatalf("EventUsage = %q, want %q", EventUsage, "usage")
	}
}

package daemon

import (
	"encoding/json"
	"strings"
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

func TestBusEventHandlerOnToolCallEmitsRunning(t *testing.T) {
	h, bus := newTestHandler(t)
	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	h.OnToolCall("bash", "ls -la /tmp")

	got := drain(t, ch, 1)
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1", len(got))
	}
	evt := got[0]
	if evt.Type != EventToolStatus {
		t.Fatalf("event type = %q, want %q", evt.Type, EventToolStatus)
	}

	var p struct {
		Tool      string `json:"tool"`
		Status    string `json:"status"`
		Args      string `json:"args"`
		SessionID string `json:"session_id"`
		TS        string `json:"ts"`
	}
	if err := json.Unmarshal(evt.Payload, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if p.Tool != "bash" || p.Status != "running" {
		t.Fatalf("bad tool/status: %+v", p)
	}
	if p.Args != "ls -la /tmp" {
		t.Fatalf("args = %q, want unchanged", p.Args)
	}
	if p.SessionID != "sess_test" {
		t.Fatalf("session_id = %q", p.SessionID)
	}
	if p.TS == "" {
		t.Fatalf("ts missing")
	}
}

func TestBusEventHandlerOnToolCallRedactsAndTruncatesArgs(t *testing.T) {
	h, bus := newTestHandler(t)
	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	long := "curl -H 'Authorization: Bearer sk-secretvalue1234567890' https://api.example.com/" + strings.Repeat("x", 500)
	h.OnToolCall("bash", long)

	got := drain(t, ch, 1)
	if len(got) != 1 {
		t.Fatalf("want 1 event")
	}
	var p struct {
		Args string `json:"args"`
	}
	_ = json.Unmarshal(got[0].Payload, &p)

	if strings.Contains(p.Args, "sk-secretvalue") {
		t.Fatalf("secret leaked: %q", p.Args)
	}
	if !strings.Contains(p.Args, "[REDACTED]") {
		t.Fatalf("expected [REDACTED] marker, got %q", p.Args)
	}
	if len(p.Args) > 200 {
		t.Fatalf("args len = %d, want ≤ 200", len(p.Args))
	}
}

func TestBusEventHandlerOnToolResultEmitsCompleted(t *testing.T) {
	h, bus := newTestHandler(t)
	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	result := agent.ToolResult{
		Content: "total 12\ndrwxr-x...",
		IsError: false,
	}
	h.OnToolResult("bash", "ls", result, 1234*time.Millisecond)

	got := drain(t, ch, 1)
	if len(got) != 1 {
		t.Fatalf("want 1 event")
	}

	var p struct {
		Tool      string `json:"tool"`
		Status    string `json:"status"`
		ElapsedMS int64  `json:"elapsed_ms"`
		IsError   bool   `json:"is_error"`
		Preview   string `json:"preview"`
		SessionID string `json:"session_id"`
	}
	_ = json.Unmarshal(got[0].Payload, &p)
	if p.Status != "completed" {
		t.Fatalf("status = %q, want completed", p.Status)
	}
	if p.ElapsedMS != 1234 {
		t.Fatalf("elapsed_ms = %d", p.ElapsedMS)
	}
	if p.IsError {
		t.Fatalf("is_error = true, want false")
	}
	if !strings.HasPrefix(p.Preview, "total 12") {
		t.Fatalf("preview = %q", p.Preview)
	}
}

func TestBusEventHandlerOnToolResultTruncatesPreview(t *testing.T) {
	h, bus := newTestHandler(t)
	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	longText := strings.Repeat("x", 500)
	result := agent.ToolResult{Content: longText}
	h.OnToolResult("bash", "", result, 0)

	got := drain(t, ch, 1)
	var p struct {
		Preview string `json:"preview"`
	}
	_ = json.Unmarshal(got[0].Payload, &p)
	if len(p.Preview) > 200 {
		t.Fatalf("preview len = %d, want ≤ 200", len(p.Preview))
	}
}

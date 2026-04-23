package daemon

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Kocoro-lab/ShanClaw/internal/agent"
)

// TestWatchdogEmitsRunStatusToBus asserts the end-to-end path used in
// production (runner.go wiring from Task 10): when the watchdog invokes
// OnRunStatus on the handler that RunAgent installed, the signal propagates
// through multiHandler to busEventHandler and lands on the bus with the
// expected code, session_id, and agent fields.
//
// We drive the path directly (without spinning up a full agent loop) by
// invoking OnRunStatus on a multiHandler that wraps a busEventHandler —
// the exact shape produced by `handler = &multiHandler{handlers: []agent.EventHandler{handler, bus}}`
// in RunAgent.
func TestWatchdogEmitsRunStatusToBus(t *testing.T) {
	bus := NewEventBus()
	deps := &ServerDeps{EventBus: bus}
	transport := &spyHandler{} // stand-in for sseEventHandler / daemonEventHandler
	bush := &busEventHandler{deps: deps, agent: "coding"}

	m := &multiHandler{handlers: []agent.EventHandler{transport, bush}}
	m.SetSessionID("sess_wdg")

	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	// Simulate what the watchdog does when the soft threshold fires
	// (runner.go:917-935 invokes OnRunStatus via type assertion; multiHandler
	// satisfies agent.RunStatusHandler so the assertion succeeds).
	m.OnRunStatus("idle_soft", "no LLM activity for 15s (phase=awaiting_llm)")

	select {
	case evt := <-ch:
		if evt.Type != EventRunStatus {
			t.Fatalf("type = %q, want %q", evt.Type, EventRunStatus)
		}
		var p struct {
			Code      string `json:"code"`
			Detail    string `json:"detail"`
			SessionID string `json:"session_id"`
			Agent     string `json:"agent"`
		}
		if err := json.Unmarshal(evt.Payload, &p); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if p.Code != "idle_soft" {
			t.Fatalf("code = %q, want idle_soft", p.Code)
		}
		if p.SessionID != "sess_wdg" {
			t.Fatalf("session_id = %q, want sess_wdg", p.SessionID)
		}
		if p.Agent != "coding" {
			t.Fatalf("agent = %q, want coding", p.Agent)
		}
		if p.Detail == "" {
			t.Fatal("detail missing")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timeout waiting for run_status on bus")
	}

	// Also verify the hard-idle path propagates distinctly — matches the
	// invariant covered by the now-deleted cmd/daemon_runstatus_test.go.
	m.OnRunStatus("idle_hard", "cancelling after 540s idle")
	select {
	case evt := <-ch:
		var p struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(evt.Payload, &p)
		if p.Code != "idle_hard" {
			t.Fatalf("idle_hard code = %q", p.Code)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("idle_hard event did not reach the bus")
	}
}

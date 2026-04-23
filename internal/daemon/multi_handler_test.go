package daemon

import (
	"testing"
	"time"

	"github.com/Kocoro-lab/ShanClaw/internal/agent"
)

// spyHandler counts each callback. Used by all multiHandler tests.
type spyHandler struct {
	toolCalls      int
	toolResults    int
	text           int
	streamDelta    int
	approvalCalls  int
	approvalReturn bool // what OnApprovalNeeded returns
	usage          int
	cloudAgent     int
	cloudProgress  int
	cloudPlan      int
}

func (s *spyHandler) OnToolCall(name, args string) { s.toolCalls++ }
func (s *spyHandler) OnToolResult(name, args string, r agent.ToolResult, e time.Duration) {
	s.toolResults++
}
func (s *spyHandler) OnText(t string)        { s.text++ }
func (s *spyHandler) OnStreamDelta(d string) { s.streamDelta++ }
func (s *spyHandler) OnApprovalNeeded(t, a string) bool {
	s.approvalCalls++
	return s.approvalReturn
}
func (s *spyHandler) OnUsage(u agent.TurnUsage)                        {}
func (s *spyHandler) OnCloudAgent(id, status, msg string)              { s.cloudAgent++ }
func (s *spyHandler) OnCloudProgress(done, total int)                  { s.cloudProgress++ }
func (s *spyHandler) OnCloudPlan(pt, content string, needsReview bool) { s.cloudPlan++ }

// Extra: make sure OnUsage is fanned out too. The counter lives here so the test
// can assert it without making spyHandler carry dead receive-only fields.
func (s *spyHandler) bumpUsage() { s.usage++ }

// usageSpy wraps spyHandler with a real OnUsage that increments the counter.
type usageSpy struct {
	spyHandler
}

func (u *usageSpy) OnUsage(turn agent.TurnUsage) { u.usage++ }

func TestMultiHandlerFansOutBaseMethods(t *testing.T) {
	a, b := &usageSpy{}, &usageSpy{}
	m := &multiHandler{handlers: []agent.EventHandler{a, b}}

	m.OnToolCall("bash", "ls")
	m.OnToolResult("bash", "ls", agent.ToolResult{}, 0)
	m.OnText("hi")
	m.OnStreamDelta("d")
	m.OnUsage(agent.TurnUsage{})
	m.OnCloudAgent("id", "running", "msg")
	m.OnCloudProgress(1, 2)
	m.OnCloudPlan("research", "content", false)

	for _, s := range []*usageSpy{a, b} {
		if s.toolCalls != 1 || s.toolResults != 1 || s.text != 1 || s.streamDelta != 1 ||
			s.usage != 1 || s.cloudAgent != 1 || s.cloudProgress != 1 || s.cloudPlan != 1 {
			t.Fatalf("spy counts off: %+v", s.spyHandler)
		}
	}
}

func TestMultiHandlerApprovalORsResults(t *testing.T) {
	a := &usageSpy{}
	a.approvalReturn = false
	b := &usageSpy{}
	b.approvalReturn = true
	m := &multiHandler{handlers: []agent.EventHandler{a, b}}

	if !m.OnApprovalNeeded("bash", "rm -rf /") {
		t.Fatal("want OR=true when any handler returns true")
	}
	if a.approvalCalls != 1 || b.approvalCalls != 1 {
		t.Fatalf("approval calls: a=%d b=%d, want 1 each", a.approvalCalls, b.approvalCalls)
	}

	// When all return false, result is false.
	c := &usageSpy{}
	c.approvalReturn = false
	d := &usageSpy{}
	d.approvalReturn = false
	m2 := &multiHandler{handlers: []agent.EventHandler{c, d}}
	if m2.OnApprovalNeeded("bash", "ls") {
		t.Fatal("want false when all handlers return false")
	}
}

// Confirms multiHandler can be assigned to agent.EventHandler — both the loop
// and tests pass it via that interface. A compile-time check keeps signature
// drift loud.
func TestMultiHandlerSatisfiesEventHandlerInterface(t *testing.T) {
	var _ agent.EventHandler = (*multiHandler)(nil)
}

package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Kocoro-lab/ShanClaw/internal/client"
)

// makeTestMessages builds a sequence of (assistant tool_use, user tool_result)
// pairs suitable for time-based compact tests. toolNames lets each turn use a
// specific tool — for whitelist tests this is "browser_navigate" etc.; for
// generic tests this is "bash".
func makeTestMessages(toolNames []string) []client.Message {
	msgs := []client.Message{
		{Role: "system", Content: client.NewTextContent("system")},
	}
	for i, name := range toolNames {
		toolID := fmt.Sprintf("tc%02d", i)
		msgs = append(msgs, client.Message{
			Role: "assistant",
			Content: client.NewBlockContent([]client.ContentBlock{
				{Type: "tool_use", ID: toolID, Name: name, Input: json.RawMessage(`{"x":1}`)},
			}),
		})
		msgs = append(msgs, client.Message{
			Role: "user",
			Content: client.NewBlockContent([]client.ContentBlock{
				client.NewToolResultBlock(toolID, fmt.Sprintf("result for tool %d (%s)", i, name), false),
			}),
		})
	}
	return msgs
}

// TestTimeBasedCompact_DisabledIsNoOp confirms the master switch. When
// Enabled = false, timeBasedCompact must not touch any byte regardless of how
// large the gap is or how many results would otherwise be eligible.
func TestTimeBasedCompact_DisabledIsNoOp(t *testing.T) {
	names := make([]string, 20)
	for i := range names {
		names[i] = "bash"
	}
	msgs := makeTestMessages(names)
	before, _ := json.Marshal(msgs)

	cfg := TimeBasedCompactConfig{Enabled: false, GapThresholdMinutes: 60, KeepRecent: 5}
	// Two-hour gap: well above any conceivable threshold.
	last := time.Now().Add(-2 * time.Hour)

	cleared := timeBasedCompact(msgs, last, cfg)
	if cleared != 0 {
		t.Fatalf("disabled config cleared %d results, want 0", cleared)
	}
	after, _ := json.Marshal(msgs)
	if string(before) != string(after) {
		t.Fatalf("disabled config mutated bytes")
	}
}

// TestTimeBasedCompact_BelowThresholdIsNoOp confirms the time-gate. When
// the gap is shorter than the configured threshold, the predicate must
// short-circuit before any clearing happens.
func TestTimeBasedCompact_BelowThresholdIsNoOp(t *testing.T) {
	names := make([]string, 20)
	for i := range names {
		names[i] = "bash"
	}
	msgs := makeTestMessages(names)
	before, _ := json.Marshal(msgs)

	cfg := TimeBasedCompactConfig{Enabled: true, GapThresholdMinutes: 60, KeepRecent: 5}
	// 30-minute gap: below the 60-minute threshold.
	last := time.Now().Add(-30 * time.Minute)

	cleared := timeBasedCompact(msgs, last, cfg)
	if cleared != 0 {
		t.Fatalf("below-threshold gap cleared %d results, want 0", cleared)
	}
	after, _ := json.Marshal(msgs)
	if string(before) != string(after) {
		t.Fatalf("below-threshold gap mutated bytes")
	}
}

// TestTimeBasedCompact_AboveThresholdClearsKeepingRecent is the happy path:
// trigger fires, older compactable results are replaced with the marker, the
// most recent KeepRecent are preserved verbatim.
func TestTimeBasedCompact_AboveThresholdClearsKeepingRecent(t *testing.T) {
	names := make([]string, 20)
	for i := range names {
		names[i] = "bash"
	}
	msgs := makeTestMessages(names)

	cfg := TimeBasedCompactConfig{Enabled: true, GapThresholdMinutes: 60, KeepRecent: 5}
	last := time.Now().Add(-2 * time.Hour)

	cleared := timeBasedCompact(msgs, last, cfg)
	wantCleared := 20 - 5
	if cleared != wantCleared {
		t.Fatalf("cleared %d results, want %d", cleared, wantCleared)
	}

	// Walk the messages: tool_result indices 1..15 (across user msgs at odd
	// indices) should equal the marker; the last 5 should be untouched.
	type observed struct {
		toolID  string
		content string
	}
	var seen []observed
	for _, m := range msgs {
		if m.Role != "user" || !m.Content.HasBlocks() {
			continue
		}
		for _, b := range m.Content.Blocks() {
			if b.Type == "tool_result" {
				s, _ := b.ToolContent.(string)
				seen = append(seen, observed{toolID: b.ToolUseID, content: s})
			}
		}
	}
	if len(seen) != 20 {
		t.Fatalf("expected 20 tool_results, got %d", len(seen))
	}
	for i, o := range seen {
		if i < wantCleared {
			if o.content != timeBasedClearedMarker {
				t.Errorf("tool_result[%d] (%s): content = %q, want marker", i, o.toolID, o.content)
			}
		} else {
			if strings.HasPrefix(o.content, "[") {
				t.Errorf("tool_result[%d] (%s): unexpected marker, want raw content (got %q)", i, o.toolID, o.content)
			}
		}
	}
}

// TestTimeBasedCompact_WhitelistFilters confirms that only the whitelisted
// tools (file_read, bash, grep, glob, http, file_edit, file_write,
// directory_list) get cleared. Non-whitelisted tool_results — browser
// snapshots, computer-use screenshots — must stay verbatim because their
// content IS the task payload.
func TestTimeBasedCompact_WhitelistFilters(t *testing.T) {
	// 10 browser_navigate (NOT compactable) + 10 bash (compactable).
	// Order matters because keepRecent slices from the tail of the
	// compactable IDs, so all 10 browser_navigate are non-compactable
	// throughout, and only the bash ones become eligible.
	var names []string
	for range 10 {
		names = append(names, "browser_navigate")
	}
	for range 10 {
		names = append(names, "bash")
	}
	msgs := makeTestMessages(names)

	cfg := TimeBasedCompactConfig{Enabled: true, GapThresholdMinutes: 60, KeepRecent: 5}
	last := time.Now().Add(-2 * time.Hour)
	cleared := timeBasedCompact(msgs, last, cfg)
	// 10 bash IDs collected → keep last 5 → clear first 5.
	if cleared != 5 {
		t.Fatalf("cleared %d results, want 5", cleared)
	}

	// Verify: all 10 browser_navigate results are untouched, exactly 5 bash
	// results are marker, exactly 5 bash results are raw.
	browserKept, bashCleared, bashKept := 0, 0, 0
	idToName := map[string]string{}
	for _, m := range msgs {
		if m.Role != "assistant" || !m.Content.HasBlocks() {
			continue
		}
		for _, b := range m.Content.Blocks() {
			if b.Type == "tool_use" {
				idToName[b.ID] = b.Name
			}
		}
	}
	for _, m := range msgs {
		if m.Role != "user" || !m.Content.HasBlocks() {
			continue
		}
		for _, b := range m.Content.Blocks() {
			if b.Type != "tool_result" {
				continue
			}
			name := idToName[b.ToolUseID]
			s, _ := b.ToolContent.(string)
			isCleared := s == timeBasedClearedMarker
			switch {
			case name == "browser_navigate":
				if isCleared {
					t.Errorf("non-whitelisted tool %q (id=%s) got cleared — whitelist breach", name, b.ToolUseID)
				}
				browserKept++
			case name == "bash" && isCleared:
				bashCleared++
			case name == "bash":
				bashKept++
			}
		}
	}
	if browserKept != 10 {
		t.Errorf("browser results kept = %d, want 10", browserKept)
	}
	if bashCleared != 5 {
		t.Errorf("bash results cleared = %d, want 5", bashCleared)
	}
	if bashKept != 5 {
		t.Errorf("bash results kept = %d, want 5", bashKept)
	}
}

// TestTimeBasedCompact_ZeroLastAssistantNoOp guards the cold-start case.
// A freshly constructed AgentLoop has lastAssistantAt as the zero time —
// time.Since(zero) is ~1.5B years, which would otherwise trigger every
// threshold. The trigger predicate must explicitly short-circuit.
func TestTimeBasedCompact_ZeroLastAssistantNoOp(t *testing.T) {
	names := make([]string, 20)
	for i := range names {
		names[i] = "bash"
	}
	msgs := makeTestMessages(names)
	before, _ := json.Marshal(msgs)

	cfg := TimeBasedCompactConfig{Enabled: true, GapThresholdMinutes: 60, KeepRecent: 5}
	cleared := timeBasedCompact(msgs, time.Time{}, cfg)
	if cleared != 0 {
		t.Fatalf("zero lastAssistantAt cleared %d results, want 0", cleared)
	}
	after, _ := json.Marshal(msgs)
	if string(before) != string(after) {
		t.Fatalf("zero lastAssistantAt mutated bytes")
	}
}

// TestTimeBasedCompact_IdempotentSecondPass guards the cache-stability
// invariant: re-running timeBasedCompact after a successful clear must NOT
// rewrite the marker bytes. timeBasedCompact checks the existing string
// equals the marker before assignment.
func TestTimeBasedCompact_IdempotentSecondPass(t *testing.T) {
	names := make([]string, 20)
	for i := range names {
		names[i] = "bash"
	}
	msgs := makeTestMessages(names)
	cfg := TimeBasedCompactConfig{Enabled: true, GapThresholdMinutes: 60, KeepRecent: 5}
	last := time.Now().Add(-2 * time.Hour)

	if got := timeBasedCompact(msgs, last, cfg); got != 15 {
		t.Fatalf("pass 1 cleared %d, want 15", got)
	}
	pass1Bytes, _ := json.Marshal(msgs)

	for pass := 2; pass <= 5; pass++ {
		got := timeBasedCompact(msgs, last, cfg)
		if got != 0 {
			t.Errorf("pass %d cleared %d results — must be 0 (idempotent)", pass, got)
		}
		passNBytes, _ := json.Marshal(msgs)
		if string(pass1Bytes) != string(passNBytes) {
			t.Errorf("pass %d mutated bytes from pass 1 — not idempotent", pass)
		}
	}
}

// TestTimeBasedCompact_KeepRecentFloorAt1 confirms the defensive floor: a
// caller who passes KeepRecent <= 0 must NOT see all results cleared (the
// model would have no working context). A misconfigured operator could
// otherwise nuke their session every 60 minutes.
func TestTimeBasedCompact_KeepRecentFloorAt1(t *testing.T) {
	names := make([]string, 5)
	for i := range names {
		names[i] = "bash"
	}
	msgs := makeTestMessages(names)
	cfg := TimeBasedCompactConfig{Enabled: true, GapThresholdMinutes: 60, KeepRecent: 0}
	last := time.Now().Add(-2 * time.Hour)
	cleared := timeBasedCompact(msgs, last, cfg)
	// 5 results, floor keeps last 1 → clear 4.
	if cleared != 4 {
		t.Fatalf("KeepRecent=0 cleared %d, want 4 (floor at 1)", cleared)
	}
}

// TestEvaluateTimeBasedCompactTrigger covers the predicate's branch table
// independently — easier to extend than the action tests above.
func TestEvaluateTimeBasedCompactTrigger(t *testing.T) {
	cfg := TimeBasedCompactConfig{Enabled: true, GapThresholdMinutes: 60, KeepRecent: 5}
	now := time.Now()
	cases := []struct {
		name string
		cfg  TimeBasedCompactConfig
		last time.Time
		want bool
	}{
		{"disabled", TimeBasedCompactConfig{Enabled: false, GapThresholdMinutes: 60, KeepRecent: 5}, now.Add(-2 * time.Hour), false},
		{"zero last", cfg, time.Time{}, false},
		{"under threshold", cfg, now.Add(-30 * time.Minute), false},
		{"at threshold edge", cfg, now.Add(-61 * time.Minute), true},
		{"well over threshold", cfg, now.Add(-2 * time.Hour), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, got := evaluateTimeBasedCompactTrigger(c.last, c.cfg)
			if got != c.want {
				t.Errorf("got fire=%v, want %v", got, c.want)
			}
		})
	}
}

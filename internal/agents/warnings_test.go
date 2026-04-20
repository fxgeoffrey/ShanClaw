package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTestAgent(t *testing.T, agentsDir, name, configYAML string) {
	t.Helper()
	dir := filepath.Join(agentsDir, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte("# "+name+"\n"), 0o600); err != nil {
		t.Fatalf("write AGENT.md: %v", err)
	}
	if configYAML != "" {
		if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(configYAML), 0o600); err != nil {
			t.Fatalf("write config.yaml: %v", err)
		}
	}
}

func TestDetectTriggerConflicts_HeartbeatAndMatchingSchedule(t *testing.T) {
	dir := t.TempDir()
	writeTestAgent(t, dir, "heartbeater", "heartbeat:\n  every: 1h\n")

	ws := DetectTriggerConflicts(dir, "heartbeater", []ScheduleRef{
		{ID: "s1", Agent: "heartbeater", Enabled: true},
	})
	if len(ws) != 1 {
		t.Fatalf("got %d warnings, want 1: %v", len(ws), ws)
	}
	if !strings.Contains(ws[0], "heartbeater") || !strings.Contains(ws[0], "1h") {
		t.Errorf("warning missing agent/every details: %q", ws[0])
	}
}

func TestDetectTriggerConflicts_NoHeartbeat(t *testing.T) {
	dir := t.TempDir()
	writeTestAgent(t, dir, "plain", "")

	ws := DetectTriggerConflicts(dir, "plain", []ScheduleRef{
		{ID: "s1", Agent: "plain", Enabled: true},
	})
	if len(ws) != 0 {
		t.Errorf("expected no warnings, got %v", ws)
	}
}

func TestDetectTriggerConflicts_HeartbeatButNoSchedule(t *testing.T) {
	dir := t.TempDir()
	writeTestAgent(t, dir, "hbonly", "heartbeat:\n  every: 30m\n")

	ws := DetectTriggerConflicts(dir, "hbonly", nil)
	if len(ws) != 0 {
		t.Errorf("expected no warnings when no schedule targets agent, got %v", ws)
	}
}

func TestDetectTriggerConflicts_HeartbeatButScheduleDisabled(t *testing.T) {
	dir := t.TempDir()
	writeTestAgent(t, dir, "hb", "heartbeat:\n  every: 30m\n")

	ws := DetectTriggerConflicts(dir, "hb", []ScheduleRef{
		{ID: "s1", Agent: "hb", Enabled: false},
	})
	if len(ws) != 0 {
		t.Errorf("expected no warnings when schedule disabled, got %v", ws)
	}
}

func TestDetectTriggerConflicts_ScheduleTargetsDifferentAgent(t *testing.T) {
	dir := t.TempDir()
	writeTestAgent(t, dir, "hb", "heartbeat:\n  every: 30m\n")

	ws := DetectTriggerConflicts(dir, "hb", []ScheduleRef{
		{ID: "s1", Agent: "other", Enabled: true},
	})
	if len(ws) != 0 {
		t.Errorf("expected no warnings when schedule targets other agent, got %v", ws)
	}
}

func TestDetectTriggerConflicts_MissingAgent(t *testing.T) {
	dir := t.TempDir()
	// Agent file never written.
	ws := DetectTriggerConflicts(dir, "ghost", []ScheduleRef{
		{ID: "s1", Agent: "ghost", Enabled: true},
	})
	if ws != nil {
		t.Errorf("expected nil warnings for missing agent, got %v", ws)
	}
}

func TestDetectTriggerConflicts_MalformedConfig(t *testing.T) {
	dir := t.TempDir()
	// Valid AGENT.md but broken config.yaml — LoadAgent should fail → no panic, no warnings.
	subdir := filepath.Join(dir, "broken")
	if err := os.MkdirAll(subdir, 0o700); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(subdir, "AGENT.md"), []byte("# broken\n"), 0o600)
	os.WriteFile(filepath.Join(subdir, "config.yaml"), []byte(":::not yaml:::"), 0o600)

	ws := DetectTriggerConflicts(dir, "broken", []ScheduleRef{
		{ID: "s1", Agent: "broken", Enabled: true},
	})
	if ws != nil {
		t.Errorf("expected nil warnings for malformed config, got %v", ws)
	}
}

func TestDetectTriggerConflicts_EmptyAgentName(t *testing.T) {
	dir := t.TempDir()
	ws := DetectTriggerConflicts(dir, "", []ScheduleRef{
		{ID: "s1", Agent: "", Enabled: true},
	})
	if ws != nil {
		t.Errorf("expected nil for empty agent name, got %v", ws)
	}
}

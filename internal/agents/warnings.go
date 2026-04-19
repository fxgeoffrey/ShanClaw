package agents

import (
	"fmt"
)

// ScheduleRef is the minimal view of a schedule needed by DetectTriggerConflicts.
// It intentionally mirrors the subset of internal/schedule.Schedule that the
// warning logic consumes, so this package does not import internal/schedule
// (and so tests can construct a slice without a real schedule store).
type ScheduleRef struct {
	ID      string
	Agent   string
	Enabled bool
}

// DetectTriggerConflicts returns human-readable warnings when an agent has
// BOTH a non-zero heartbeat.every AND at least one enabled schedule targeting
// it. The returned slice is nil when there is no conflict.
//
// Contract:
//   - agentsDir + agentName are the usual LoadAgent inputs.
//   - schedules is the full list (e.g. schedule.Manager.List()) — entries
//     with a non-matching Agent name or Enabled=false are ignored.
//   - Missing/malformed agent files produce no warnings (empty slice, nil err).
//     Callers must not panic. Visibility only, never a hard error.
func DetectTriggerConflicts(agentsDir, agentName string, schedules []ScheduleRef) []string {
	if agentName == "" {
		return nil
	}
	ag, err := LoadAgent(agentsDir, agentName)
	if err != nil || ag == nil || ag.Config == nil || ag.Config.Heartbeat == nil {
		return nil
	}
	every := ag.Config.Heartbeat.Every
	if every == "" {
		return nil
	}

	var matching []ScheduleRef
	for _, s := range schedules {
		if s.Agent == agentName && s.Enabled {
			matching = append(matching, s)
		}
	}
	if len(matching) == 0 {
		return nil
	}

	return []string{
		fmt.Sprintf(
			"agent %q also has heartbeat: every: %s configured. Both triggers will fire independently. Consider disabling one.",
			agentName, every,
		),
	}
}

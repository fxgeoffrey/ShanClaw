package agent

import (
	"fmt"
	"time"
)

// DeltaKind identifies the type of mid-run state change.
type DeltaKind string

const (
	DeltaDateRollover   DeltaKind = "date_rollover"
	DeltaToolCapability DeltaKind = "tool_capability" // v1.1
)

// Delta represents a mid-run state change to inject into the conversation.
type Delta struct {
	Kind    DeltaKind
	Message string
}

// DeltaProvider is polled at each loop iteration boundary. Implementations
// return pending deltas since last call. Only inject a delta when it can
// change the model's very next decision.
type DeltaProvider interface {
	Check() []Delta
}

// TemporalDelta detects calendar day rollover during long-running sessions.
type TemporalDelta struct {
	lastYear    int
	lastYearDay int
}

// NewTemporalDelta creates a TemporalDelta anchored to the current date.
func NewTemporalDelta() *TemporalDelta {
	now := time.Now()
	return &TemporalDelta{
		lastYear:    now.Year(),
		lastYearDay: now.YearDay(),
	}
}

func (t *TemporalDelta) Check() []Delta {
	now := time.Now()
	if now.Year() == t.lastYear && now.YearDay() == t.lastYearDay {
		return nil
	}
	t.lastYear = now.Year()
	t.lastYearDay = now.YearDay()
	return []Delta{{
		Kind:    DeltaDateRollover,
		Message: fmt.Sprintf("The date has changed to %s. References to \"today\", \"tomorrow\", and relative dates should now be interpreted from this new date.", now.Format("2006-01-02")),
	}}
}

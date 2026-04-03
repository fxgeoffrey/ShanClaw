package agent

import (
	"strings"
	"testing"
	"time"
)

func TestTemporalDelta_NoDeltaWithinSameDay(t *testing.T) {
	td := NewTemporalDelta()
	deltas := td.Check()
	if len(deltas) != 0 {
		t.Fatalf("expected no deltas on same day, got %d", len(deltas))
	}
}

func TestTemporalDelta_DeltaOnDayRollover(t *testing.T) {
	td := &TemporalDelta{
		lastYear:    time.Now().Year(),
		lastYearDay: time.Now().YearDay() - 1, // simulate yesterday
	}
	deltas := td.Check()
	if len(deltas) != 1 {
		t.Fatalf("expected 1 delta, got %d", len(deltas))
	}
	if deltas[0].Kind != DeltaDateRollover {
		t.Fatalf("expected DeltaDateRollover, got %s", deltas[0].Kind)
	}
	if !strings.Contains(deltas[0].Message, "date has changed") {
		t.Fatalf("unexpected message: %s", deltas[0].Message)
	}
}

func TestTemporalDelta_DeltaFiresOnlyOnce(t *testing.T) {
	td := &TemporalDelta{
		lastYear:    time.Now().Year(),
		lastYearDay: time.Now().YearDay() - 1,
	}
	deltas := td.Check()
	if len(deltas) != 1 {
		t.Fatalf("first check: expected 1 delta, got %d", len(deltas))
	}
	deltas = td.Check()
	if len(deltas) != 0 {
		t.Fatalf("second check: expected 0 deltas, got %d", len(deltas))
	}
}

func TestTemporalDelta_YearRollover(t *testing.T) {
	td := &TemporalDelta{
		lastYear:    time.Now().Year() - 1,
		lastYearDay: 365,
	}
	deltas := td.Check()
	if len(deltas) != 1 {
		t.Fatalf("expected 1 delta on year rollover, got %d", len(deltas))
	}
}

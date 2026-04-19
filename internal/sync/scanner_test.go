package sync

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/Kocoro-lab/ShanClaw/internal/session"
)

// scannerTestEnv builds a fake ~/.shannon layout and seeds session indexes.
type scannerTestEnv struct {
	HomeDir string
}

func newScannerEnv(t *testing.T) *scannerTestEnv {
	t.Helper()
	return &scannerTestEnv{HomeDir: t.TempDir()}
}

func (e *scannerTestEnv) seedSession(t *testing.T, agent string, s *session.Session) {
	t.Helper()
	var dir string
	if agent == "" {
		dir = filepath.Join(e.HomeDir, "sessions")
	} else {
		dir = filepath.Join(e.HomeDir, "agents", agent, "sessions")
	}
	idx, err := session.OpenIndex(dir)
	if err != nil {
		t.Fatalf("OpenIndex(%s): %v", dir, err)
	}
	defer idx.Close()
	if err := idx.UpsertSession(s); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
}

func TestScanner_MultiDir_WatermarkOnly(t *testing.T) {
	env := newScannerEnv(t)
	now := time.Now().UTC().Truncate(time.Second)
	older := now.Add(-2 * time.Hour)
	newer := now.Add(-10 * time.Minute)

	env.seedSession(t, "", &session.Session{ID: "default-old", CreatedAt: older, UpdatedAt: older})
	env.seedSession(t, "", &session.Session{ID: "default-new", CreatedAt: newer, UpdatedAt: newer})
	env.seedSession(t, "ops-bot", &session.Session{ID: "ops-new", CreatedAt: newer, UpdatedAt: newer})
	env.seedSession(t, "personal", &session.Session{ID: "personal-old", CreatedAt: older, UpdatedAt: older})

	cfg := DefaultConfig()
	cfg.Enabled = true
	marker := emptyMarker()
	marker.LastSyncAt = now.Add(-1 * time.Hour) // cutoff

	cands, _, err := DiscoverCandidates(context.Background(), ScannerDeps{HomeDir: env.HomeDir}, cfg, marker, now)
	if err != nil {
		t.Fatalf("DiscoverCandidates: %v", err)
	}

	gotIDs := []string{}
	for _, c := range cands {
		gotIDs = append(gotIDs, c.SessionID)
	}
	sort.Strings(gotIDs)
	wantIDs := []string{"default-new", "ops-new"}
	if !equalStrings(gotIDs, wantIDs) {
		t.Errorf("got candidate IDs %v, want %v", gotIDs, wantIDs)
	}

	// Verify AgentName tagging.
	for _, c := range cands {
		switch c.SessionID {
		case "default-new":
			if c.AgentName != "" {
				t.Errorf("default-new should have empty AgentName, got %q", c.AgentName)
			}
		case "ops-new":
			if c.AgentName != "ops-bot" {
				t.Errorf("ops-new should have AgentName=ops-bot, got %q", c.AgentName)
			}
		}
	}
}

func TestScanner_RetryUnion_DueOnly(t *testing.T) {
	env := newScannerEnv(t)
	now := time.Now().UTC().Truncate(time.Second)
	older := now.Add(-3 * time.Hour)
	due := now.Add(-1 * time.Minute)
	notDue := now.Add(1 * time.Hour)

	// Seed three failed-retry sessions in the default dir, all OLDER than the watermark.
	env.seedSession(t, "", &session.Session{ID: "due-retry", CreatedAt: older, UpdatedAt: older})
	env.seedSession(t, "", &session.Session{ID: "not-due-retry", CreatedAt: older, UpdatedAt: older})
	env.seedSession(t, "", &session.Session{ID: "permanent-fail", CreatedAt: older, UpdatedAt: older})

	cfg := DefaultConfig()
	cfg.Enabled = true
	marker := emptyMarker()
	marker.LastSyncAt = now.Add(-1 * time.Hour) // newer than `older`, so SQL query returns nothing
	marker.Failed = map[string]FailedEntry{
		"due-retry": {
			Reason: "cloud_rejected_retryable", Category: CategoryTransient,
			Attempts: 1, NextAttemptAt: &due,
		},
		"not-due-retry": {
			Reason: "cloud_rejected_retryable", Category: CategoryTransient,
			Attempts: 1, NextAttemptAt: &notDue,
		},
		"permanent-fail": {
			Reason: "size_limit_exceeded", Category: CategoryPermanent,
			Attempts: 1, NextAttemptAt: nil,
		},
	}

	cands, _, err := DiscoverCandidates(context.Background(), ScannerDeps{HomeDir: env.HomeDir}, cfg, marker, now)
	if err != nil {
		t.Fatalf("DiscoverCandidates: %v", err)
	}

	gotIDs := []string{}
	for _, c := range cands {
		gotIDs = append(gotIDs, c.SessionID)
	}
	sort.Strings(gotIDs)
	wantIDs := []string{"due-retry"}
	if !equalStrings(gotIDs, wantIDs) {
		t.Errorf("retry union: got %v, want %v (only transient with NextAttemptAt<=now)", gotIDs, wantIDs)
	}
}

func TestScanner_DedupeOnIDCollision(t *testing.T) {
	// If a failed-retry ID also matches the SQL watermark query (e.g., user
	// edited a previously-failed session), it must appear exactly once with
	// the freshest UpdatedAt.
	env := newScannerEnv(t)
	now := time.Now().UTC().Truncate(time.Second)
	freshUpdate := now.Add(-5 * time.Minute)

	env.seedSession(t, "", &session.Session{ID: "edited-after-fail", CreatedAt: freshUpdate, UpdatedAt: freshUpdate})

	cfg := DefaultConfig()
	cfg.Enabled = true
	marker := emptyMarker()
	marker.LastSyncAt = now.Add(-1 * time.Hour)
	due := now.Add(-1 * time.Minute)
	marker.Failed = map[string]FailedEntry{
		"edited-after-fail": {
			Reason: "cloud_rejected_retryable", Category: CategoryTransient,
			Attempts: 1, NextAttemptAt: &due,
		},
	}

	cands, _, err := DiscoverCandidates(context.Background(), ScannerDeps{HomeDir: env.HomeDir}, cfg, marker, now)
	if err != nil {
		t.Fatalf("DiscoverCandidates: %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("expected 1 candidate after dedupe, got %d: %+v", len(cands), cands)
	}
	if cands[0].SessionID != "edited-after-fail" {
		t.Errorf("got ID %q, want edited-after-fail", cands[0].SessionID)
	}
	if !cands[0].UpdatedAt.Equal(freshUpdate) {
		t.Errorf("expected freshest UpdatedAt %v, got %v", freshUpdate, cands[0].UpdatedAt)
	}
}

func TestScanner_ExcludeAgents(t *testing.T) {
	env := newScannerEnv(t)
	now := time.Now().UTC().Truncate(time.Second)
	newer := now.Add(-10 * time.Minute)

	env.seedSession(t, "", &session.Session{ID: "in-default", CreatedAt: newer, UpdatedAt: newer})
	env.seedSession(t, "personal", &session.Session{ID: "in-personal", CreatedAt: newer, UpdatedAt: newer})
	env.seedSession(t, "ops-bot", &session.Session{ID: "in-ops", CreatedAt: newer, UpdatedAt: newer})

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.ExcludeAgents = []string{"personal", "default"} // "default" excludes the root sessions dir

	marker := emptyMarker()
	marker.LastSyncAt = now.Add(-1 * time.Hour)

	cands, _, err := DiscoverCandidates(context.Background(), ScannerDeps{HomeDir: env.HomeDir}, cfg, marker, now)
	if err != nil {
		t.Fatalf("DiscoverCandidates: %v", err)
	}
	gotIDs := []string{}
	for _, c := range cands {
		gotIDs = append(gotIDs, c.SessionID)
	}
	sort.Strings(gotIDs)
	want := []string{"in-ops"}
	if !equalStrings(gotIDs, want) {
		t.Errorf("ExcludeAgents: got %v, want %v", gotIDs, want)
	}
}

func TestScanner_ExcludeSources_LegacyEmptyTreatedAsLocal(t *testing.T) {
	env := newScannerEnv(t)
	now := time.Now().UTC().Truncate(time.Second)
	newer := now.Add(-10 * time.Minute)

	env.seedSession(t, "", &session.Session{
		ID: "legacy-no-source", CreatedAt: newer, UpdatedAt: newer, Source: "",
	})
	env.seedSession(t, "", &session.Session{
		ID: "explicit-local", CreatedAt: newer, UpdatedAt: newer, Source: "local",
	})
	env.seedSession(t, "", &session.Session{
		ID: "from-slack", CreatedAt: newer, UpdatedAt: newer, Source: "slack",
	})

	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.ExcludeSources = []string{"local"}

	marker := emptyMarker()
	marker.LastSyncAt = now.Add(-1 * time.Hour)

	cands, _, err := DiscoverCandidates(context.Background(), ScannerDeps{HomeDir: env.HomeDir}, cfg, marker, now)
	if err != nil {
		t.Fatalf("DiscoverCandidates: %v", err)
	}
	gotIDs := []string{}
	for _, c := range cands {
		gotIDs = append(gotIDs, c.SessionID)
	}
	sort.Strings(gotIDs)
	want := []string{"from-slack"}
	if !equalStrings(gotIDs, want) {
		t.Errorf("ExcludeSources (legacy empty == local): got %v, want %v", gotIDs, want)
	}
}

// TestScanner_SkippedDirCount asserts the scanner returns a non-zero skipped
// count when a session dir's index is unreadable, while still producing
// candidates from working dirs (per-dir resilience).
func TestScanner_SkippedDirCount(t *testing.T) {
	env := newScannerEnv(t)
	now := time.Now().UTC().Truncate(time.Second)
	newer := now.Add(-10 * time.Minute)

	// Working agent dir produces a normal candidate.
	env.seedSession(t, "ops-bot", &session.Session{ID: "ops-new", CreatedAt: newer, UpdatedAt: newer})

	// Broken agent dir: create a "sessions.db" that is itself a directory,
	// not a sqlite file. os.Stat succeeds (so the early-skip doesn't fire),
	// but OpenIndex fails to read PRAGMA user_version.
	badAgentDir := filepath.Join(env.HomeDir, "agents", "broken", "sessions")
	if err := os.MkdirAll(filepath.Join(badAgentDir, "sessions.db"), 0o755); err != nil {
		t.Fatalf("seed bad dir: %v", err)
	}

	cfg := DefaultConfig()
	cfg.Enabled = true
	marker := emptyMarker()
	marker.LastSyncAt = now.Add(-1 * time.Hour)

	cands, skipped, err := DiscoverCandidates(context.Background(), ScannerDeps{HomeDir: env.HomeDir}, cfg, marker, now)
	if err != nil {
		t.Fatalf("DiscoverCandidates: %v", err)
	}
	if skipped < 1 {
		t.Errorf("expected skipped >= 1 for unreadable sessions.db, got %d", skipped)
	}

	// Working dir still surfaces its candidate.
	gotIDs := []string{}
	for _, c := range cands {
		gotIDs = append(gotIDs, c.SessionID)
	}
	sort.Strings(gotIDs)
	want := []string{"ops-new"}
	if !equalStrings(gotIDs, want) {
		t.Errorf("working dirs must still produce candidates: got %v, want %v", gotIDs, want)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// DefaultConfig is a zero-value-safe config helper used in scanner+batcher+sync tests.
func DefaultConfig() Config {
	return Config{
		BatchMaxSessions:           25,
		BatchMaxBytes:              5 * 1024 * 1024,
		SingleSessionMaxBytes:      4 * 1024 * 1024,
		DaemonInterval:             24 * time.Hour,
		DaemonStartupDelay:         60 * time.Second,
		FailedMaxAttemptsTransient: 5,
		LockTimeout:                30 * time.Second,
	}
}

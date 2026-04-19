package sync

import (
	"context"
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

	cands, err := DiscoverCandidates(context.Background(), ScannerDeps{HomeDir: env.HomeDir}, cfg, marker, now)
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

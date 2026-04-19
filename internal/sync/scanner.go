package sync

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Kocoro-lab/ShanClaw/internal/session"
)

type Candidate struct {
	Dir       string
	AgentName string
	SessionID string
	UpdatedAt time.Time
}

type ScannerDeps struct {
	HomeDir string
}

// DiscoverCandidates enumerates sessions across all session directories whose
// updated_at strictly exceeds marker.LastSyncAt. Failed-retry union, exclusions,
// and dedupe are added in subsequent tasks.
func DiscoverCandidates(ctx context.Context, deps ScannerDeps, cfg Config, marker Marker, now time.Time) ([]Candidate, error) {
	dirs, err := discoverSessionDirs(deps.HomeDir)
	if err != nil {
		return nil, fmt.Errorf("discover session dirs: %w", err)
	}

	var out []Candidate
	for _, sd := range dirs {
		dbPath := filepath.Join(sd.Dir, "sessions.db")
		if _, err := os.Stat(dbPath); err != nil {
			// No index yet for this dir (or unreadable). Skip silently — the
			// agent dir may not have produced any sessions. Avoids creating an
			// empty sessions.db just to query it.
			continue
		}
		idx, err := session.OpenIndex(sd.Dir)
		if err != nil {
			// Skip this dir; do not fail the whole run.
			// Caller's audit log will reflect skipped dirs separately.
			continue
		}
		rows, err := idx.ListUpdatedSince(ctx, marker.LastSyncAt)
		idx.Close()
		if err != nil {
			continue
		}
		for _, r := range rows {
			out = append(out, Candidate{
				Dir:       sd.Dir,
				AgentName: sd.AgentName,
				SessionID: r.ID,
				UpdatedAt: r.UpdatedAt,
			})
		}
	}
	return out, nil
}

type sessionDir struct {
	Dir       string
	AgentName string // "" for default
}

// discoverSessionDirs returns the default ~/.shannon/sessions/ and every
// ~/.shannon/agents/<name>/sessions/ that exists.
func discoverSessionDirs(home string) ([]sessionDir, error) {
	var out []sessionDir
	defaultDir := filepath.Join(home, "sessions")
	if _, err := os.Stat(defaultDir); err == nil {
		out = append(out, sessionDir{Dir: defaultDir, AgentName: ""})
	}
	agentsRoot := filepath.Join(home, "agents")
	entries, err := os.ReadDir(agentsRoot)
	if err != nil {
		// agents/ may not exist on a fresh install; that's fine.
		if os.IsNotExist(err) {
			return out, nil
		}
		return out, fmt.Errorf("read agents dir: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sd := filepath.Join(agentsRoot, e.Name(), "sessions")
		if _, err := os.Stat(sd); err != nil {
			continue
		}
		out = append(out, sessionDir{Dir: sd, AgentName: e.Name()})
	}
	return out, nil
}

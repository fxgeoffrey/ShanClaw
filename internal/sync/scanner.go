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

// DiscoverCandidates enumerates sessions across all session directories.
// It performs two phases:
//  1. SQL watermark query per dir for sessions whose updated_at strictly
//     exceeds marker.LastSyncAt, with a no-churn permanent-skip filter so
//     known-permanent failures don't re-attempt unless the user edited the
//     session since LastObservedUpdatedAt.
//  2. In-memory union with eligible failed-retry entries (transient only,
//     NextAttemptAt non-nil and now >= NextAttemptAt).
//
// Results are deduped by SessionID; freshest UpdatedAt wins on collision.
func DiscoverCandidates(ctx context.Context, deps ScannerDeps, cfg Config, marker Marker, now time.Time) ([]Candidate, error) {
	dirs, err := discoverSessionDirs(deps.HomeDir)
	if err != nil {
		return nil, fmt.Errorf("discover session dirs: %w", err)
	}

	// Phase 1: SQL watermark query per dir.
	// Index dedupe by ID; freshest UpdatedAt wins on collision.
	byID := map[string]Candidate{}
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
			// No-churn rule: permanent-failed sessions skip unless edited
			// since LastObservedUpdatedAt. Without this guard, attempts
			// would increment on every run because LastSyncAt does not
			// advance for un-accepted IDs (the SQL query keeps surfacing
			// them). Spec: "Permanent failures: no-churn rule".
			if fe, isFailed := marker.Failed[r.ID]; isFailed && fe.Category == CategoryPermanent {
				if !r.UpdatedAt.After(fe.LastObservedUpdatedAt) {
					continue // same data we already know fails; skip
				}
				// Else: session edited; let it through for a fresh attempt.
			}
			c := Candidate{
				Dir:       sd.Dir,
				AgentName: sd.AgentName,
				SessionID: r.ID,
				UpdatedAt: r.UpdatedAt,
			}
			if existing, ok := byID[r.ID]; !ok || r.UpdatedAt.After(existing.UpdatedAt) {
				byID[r.ID] = c
			}
		}
	}

	// Phase 2: in-memory union with eligible failed-retry entries.
	// Eligibility: transient category AND NextAttemptAt non-nil AND now >= NextAttemptAt.
	// Permanent entries (NextAttemptAt == nil) are NEVER added by this path; they
	// re-enter only via the SQL watermark query if the user edited the session.
	for id, fe := range marker.Failed {
		if fe.Category != CategoryTransient {
			continue
		}
		if fe.NextAttemptAt == nil {
			continue
		}
		if now.Before(*fe.NextAttemptAt) {
			continue
		}
		if _, alreadyFromSQL := byID[id]; alreadyFromSQL {
			// SQL query already surfaced a freshly-edited version — let that
			// version drive the retry. Don't double-add.
			continue
		}
		// Locate the session's dir so the batcher can load it. Walk dirs.
		dir, agent, found := locateSession(dirs, id)
		if !found {
			// Session was deleted locally but still in marker.Failed — the
			// batcher's load_error path will drop it on next attempt anyway.
			// Add a placeholder candidate so that path runs.
			dir, agent = "", ""
		}
		byID[id] = Candidate{
			Dir:       dir,
			AgentName: agent,
			SessionID: id,
			UpdatedAt: fe.LastAttemptAt, // synthetic; not used to advance marker
		}
	}

	out := make([]Candidate, 0, len(byID))
	for _, c := range byID {
		out = append(out, c)
	}
	return out, nil
}

// locateSession scans the discovered dirs for a session JSON file.
// Used only for failed-retry entries that don't surface via the SQL query.
func locateSession(dirs []sessionDir, id string) (dir string, agent string, found bool) {
	for _, sd := range dirs {
		path := filepath.Join(sd.Dir, id+".json")
		if _, err := os.Stat(path); err == nil {
			return sd.Dir, sd.AgentName, true
		}
	}
	return "", "", false
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

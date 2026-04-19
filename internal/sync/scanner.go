package sync

import (
	"context"
	"fmt"
	"log"
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
	Source    string // "" treated as "local" for exclusion matching
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
//
// The returned int is the count of session dirs that were skipped due to
// OpenIndex or query failure (not "no index file present" — that is benign
// and not counted). The caller surfaces it in the audit event so daemon-mode
// operators can spot migration/permission failures from audit.log without
// tailing stderr.
func DiscoverCandidates(ctx context.Context, deps ScannerDeps, cfg Config, marker Marker, now time.Time) ([]Candidate, int, error) {
	dirs, err := discoverSessionDirs(deps.HomeDir)
	if err != nil {
		return nil, 0, fmt.Errorf("discover session dirs: %w", err)
	}

	// Phase 1: SQL watermark query per dir.
	// Index dedupe by ID; freshest UpdatedAt wins on collision.
	byID := map[string]Candidate{}
	skipped := 0
	for _, sd := range dirs {
		dbPath := filepath.Join(sd.Dir, "sessions.db")
		if _, err := os.Stat(dbPath); err != nil {
			// No index yet for this dir (or unreadable). Skip silently — the
			// agent dir may not have produced any sessions. Avoids creating an
			// empty sessions.db just to query it.
			continue
		}
		// Skip this dir on OpenIndex/query failure. Marker advances per accepted
		// session, not per-time, so sessions in a skipped dir are picked up on
		// the next run when the dir becomes readable — no data loss.
		idx, err := session.OpenIndex(sd.Dir)
		if err != nil {
			log.Printf("sync: skipping session dir %s: %v", sd.Dir, err)
			skipped++
			continue
		}
		rows, err := idx.ListUpdatedSince(ctx, marker.LastSyncAt)
		idx.Close()
		if err != nil {
			log.Printf("sync: skipping session dir %s: %v", sd.Dir, err)
			skipped++
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
				Source:    r.Source,
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

	// Apply ExcludeAgents / ExcludeSources after dedupe so the keys are
	// canonical: empty AgentName ⇢ "default", empty Source ⇢ "local".
	excludeAgent := makeStringSet(cfg.ExcludeAgents)
	excludeSource := makeStringSet(cfg.ExcludeSources)

	out := make([]Candidate, 0, len(byID))
	for _, c := range byID {
		agentKey := c.AgentName
		if agentKey == "" {
			agentKey = "default"
		}
		if excludeAgent[agentKey] {
			continue
		}
		srcKey := c.Source
		if srcKey == "" {
			srcKey = "local"
		}
		if excludeSource[srcKey] {
			continue
		}
		out = append(out, c)
	}
	return out, skipped, nil
}

func makeStringSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
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

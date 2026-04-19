package sync

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/Kocoro-lab/ShanClaw/internal/client"
)

// AuditLogger is a structured logger sink. Satisfied by *audit.Logger in
// production; tests provide a capturing stub.
type AuditLogger interface {
	Log(event string, fields map[string]any)
}

// Deps groups the runtime collaborators required by Run. Now is injectable so
// tests can pin time without freezing the whole package.
type Deps struct {
	Cfg       Config
	HomeDir   string           // ~/.shannon
	ClientVer string           // for SyncBatchRequest.ClientVersion
	Uploader  Uploader
	Loader    SessionLoader
	Audit     AuditLogger
	Now       func() time.Time
}

// Run executes one sync iteration: read marker, scan candidates, build batches,
// upload, apply per-session ack/reject, write marker, audit.
//
// Flock acquisition is layered on in Task 15. The current happy-path body
// is single-process-safe but does NOT guard against concurrent CLI + daemon
// invocations.
func Run(ctx context.Context, deps Deps) error {
	now := time.Now().UTC()
	if deps.Now != nil {
		now = deps.Now()
	}

	markerPath := filepath.Join(deps.HomeDir, "sync_marker.json")

	marker, err := ReadMarker(markerPath)
	if err != nil {
		// ReadMarker swallows missing/corrupt/version-mismatch into emptyMarker,
		// so any error here is unrecoverable I/O — fail the run.
		return fmt.Errorf("read marker: %w", err)
	}

	if !deps.Cfg.Enabled {
		audit(deps.Audit, "session_sync", map[string]any{
			"outcome": OutcomeNoop,
			"reason":  "sync.enabled=false",
		})
		return nil
	}

	cands, err := DiscoverCandidates(ctx, ScannerDeps{HomeDir: deps.HomeDir}, deps.Cfg, marker, now)
	if err != nil {
		return fmt.Errorf("discover candidates: %w", err)
	}

	if len(cands) == 0 {
		marker.LastSyncOutcome = OutcomeNoop
		marker.LastSyncCount = 0
		if err := WriteMarkerAtomic(markerPath, marker); err != nil {
			return fmt.Errorf("write marker (noop): %w", err)
		}
		audit(deps.Audit, "session_sync", map[string]any{
			"outcome": OutcomeNoop,
			"reason":  "no candidates",
		})
		return nil
	}

	batches, err := BuildBatches(ctx, cands, deps.Loader, deps.Cfg, &marker, now)
	if err != nil {
		return fmt.Errorf("build batches: %w", err)
	}

	totalAccepted := 0
	totalRejectedTransient := 0
	totalRejectedPermanent := 0
	outcome := OutcomeOK
	transportErr := false

	// id → MarshaledSession lookup so per-id ack/reject can pull the candidate's
	// UpdatedAt for marker advance and the no-churn rule.
	byID := map[string]MarshaledSession{}
	for _, b := range batches {
		for _, s := range b.Sessions {
			byID[s.SessionID] = s
		}
	}

	for _, batch := range batches {
		req := client.SyncBatchRequest{
			ClientVersion: deps.ClientVer,
			SyncAt:        now,
			Sessions:      make([]client.SessionEnvelope, 0, len(batch.Sessions)),
		}
		for _, s := range batch.Sessions {
			req.Sessions = append(req.Sessions, client.SessionEnvelope{
				AgentName: s.AgentName,
				Session:   s.JSON,
			})
		}

		resp, err := deps.Uploader.Send(ctx, req)
		if err != nil {
			// Transport error: stop sending more batches, but keep advances from
			// already-accepted batches. Marker writes below still happen so the
			// next run resumes from where we got to.
			transportErr = true
			outcome = OutcomeTransportError
			break
		}

		for _, id := range resp.Accepted {
			totalAccepted++
			if ms, ok := byID[id]; ok {
				if ms.Candidate.UpdatedAt.After(marker.LastSyncAt) {
					marker.LastSyncAt = ms.Candidate.UpdatedAt
				}
			}
			delete(marker.Failed, id)
		}
		for _, r := range resp.Rejected {
			cat := ClassifyReason(r.Reason)
			if cat == CategoryTransient {
				totalRejectedTransient++
			} else {
				totalRejectedPermanent++
			}
			recordRejection(&marker, r.ID, r.Reason, byID, now, deps.Cfg.FailedMaxAttemptsTransient)
			if outcome == OutcomeOK {
				outcome = OutcomePartial
			}
		}
	}

	marker.LastSyncCount = totalAccepted
	marker.LastSyncOutcome = outcome
	if err := WriteMarkerAtomic(markerPath, marker); err != nil {
		return fmt.Errorf("write marker: %w", err)
	}

	audit(deps.Audit, "session_sync", map[string]any{
		"sent":               len(byID),
		"accepted":           totalAccepted,
		"rejected_transient": totalRejectedTransient,
		"rejected_permanent": totalRejectedPermanent,
		"failed_carryover":   len(marker.Failed),
		"outcome":            outcome,
		"transport_error":    transportErr,
	})
	return nil
}

// recordRejection merges a per-session reject into marker.Failed. Permanent
// reasons pin NextAttemptAt = nil (no auto-retry); transient reasons schedule
// the next attempt via NextTransientAttemptAt and drop the entry once the
// transient cap is hit. byID supplies the candidate's UpdatedAt so the
// no-churn rule on the scanner side can detect future local edits.
func recordRejection(m *Marker, id, reason string, byID map[string]MarshaledSession, now time.Time, transientCap int) {
	if m.Failed == nil {
		m.Failed = map[string]FailedEntry{}
	}
	prev, existed := m.Failed[id]
	cat := ClassifyReason(reason)
	if !existed {
		prev = FailedEntry{
			Reason:         reason,
			Category:       cat,
			Attempts:       0,
			FirstAttemptAt: now,
		}
	}
	prev.Reason = reason
	prev.Category = cat
	prev.Attempts++
	prev.LastAttemptAt = now
	if ms, ok := byID[id]; ok {
		prev.SizeBytes = ms.SizeBytes
		prev.LastObservedUpdatedAt = ms.Candidate.UpdatedAt
	}
	if cat == CategoryPermanent {
		prev.NextAttemptAt = nil
	} else {
		// Transient: drop if past cap; else schedule next attempt.
		if transientCap > 0 && prev.Attempts >= transientCap {
			delete(m.Failed, id)
			return
		}
		nxt := NextTransientAttemptAt(prev.Attempts, now)
		prev.NextAttemptAt = &nxt
	}
	m.Failed[id] = prev
}

// audit is a nil-safe wrapper so callers don't have to gate every Log call.
func audit(a AuditLogger, event string, fields map[string]any) {
	if a == nil {
		return
	}
	a.Log(event, fields)
}

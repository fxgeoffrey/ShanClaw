// Package sync implements local→cloud session sync (marker, scanner, batcher,
// uploader, run orchestration). This file provides the on-disk marker
// (sync_marker.json) that records the last sync watermark and per-session
// failure state so the next run can resume idempotently.
package sync

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

const MarkerVersion = 1

const (
	OutcomeOK             = "ok"
	OutcomePartial        = "partial"
	OutcomeTransportError = "transport_error"
	OutcomeNoop           = "noop"
)

const (
	CategoryTransient = "transient"
	CategoryPermanent = "permanent"
)

type Marker struct {
	Version         int                    `json:"version"`
	LastSyncAt      time.Time              `json:"last_sync_at"`
	LastSyncCount   int                    `json:"last_sync_count"`
	LastSyncOutcome string                 `json:"last_sync_outcome"`
	Failed          map[string]FailedEntry `json:"failed"`
}

type FailedEntry struct {
	Reason                string     `json:"reason"`
	Category              string     `json:"category"`
	Attempts              int        `json:"attempts"`
	SizeBytes             uint64     `json:"size_bytes"`
	FirstAttemptAt        time.Time  `json:"first_attempt_at"`
	LastAttemptAt         time.Time  `json:"last_attempt_at"`
	LastObservedUpdatedAt time.Time  `json:"last_observed_updated_at"`
	NextAttemptAt         *time.Time `json:"next_attempt_at"`
}

// emptyMarker returns a fresh epoch-reset marker tagged with the current schema version.
func emptyMarker() Marker {
	return Marker{
		Version: MarkerVersion,
		Failed:  map[string]FailedEntry{},
	}
}

// ReadMarker loads the marker at path. Returns emptyMarker() (no error) for any
// of: missing file, corrupt JSON, unknown/future schema version. In the corrupt
// or unknown-version cases, sidecars the offending file for operator triage:
//   - corrupt: <path>.corrupt.bak
//   - unknown version N: <path>.unknown-v<N>.bak
// The original file is left in place; the next successful WriteMarkerAtomic
// replaces it with a v1 marker.
func ReadMarker(path string) (Marker, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return emptyMarker(), nil
		}
		return Marker{}, fmt.Errorf("read marker: %w", err)
	}

	// Probe just the version field first so we can sidecar before full parse.
	var probe struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(b, &probe); err != nil {
		// Corrupt JSON entirely.
		_ = sidecarMarker(path, b, ".corrupt.bak")
		return emptyMarker(), nil
	}
	if probe.Version <= 0 {
		// Missing or zero version field: treat as corrupt rather than
		// "unknown-v0" — clearer for operators reading the sidecar name.
		_ = sidecarMarker(path, b, ".corrupt.bak")
		return emptyMarker(), nil
	}
	if probe.Version != MarkerVersion {
		_ = sidecarMarker(path, b, fmt.Sprintf(".unknown-v%d.bak", probe.Version))
		return emptyMarker(), nil
	}

	var m Marker
	if err := json.Unmarshal(b, &m); err != nil {
		// Right version, malformed body. Treat same as corrupt.
		_ = sidecarMarker(path, b, ".corrupt.bak")
		return emptyMarker(), nil
	}
	if m.Failed == nil {
		m.Failed = map[string]FailedEntry{}
	}
	return m, nil
}

func sidecarMarker(originalPath string, contents []byte, suffix string) error {
	side := originalPath + suffix
	return os.WriteFile(side, contents, 0o644)
}

// WriteMarkerAtomic writes m to path via write-temp + rename.
func WriteMarkerAtomic(path string, m Marker) error {
	if m.Version == 0 {
		m.Version = MarkerVersion
	}
	if m.Failed == nil {
		m.Failed = map[string]FailedEntry{}
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal marker: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir marker dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "sync_marker-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create tempfile: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write tempfile: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close tempfile: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename tempfile: %w", err)
	}
	return nil
}

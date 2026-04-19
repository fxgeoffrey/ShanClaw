package sync

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestMarker_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sync_marker.json")

	now := time.Date(2026, 4, 19, 3, 0, 0, 0, time.UTC)
	next := now.Add(48 * time.Hour)
	want := Marker{
		Version:         MarkerVersion,
		LastSyncAt:      now,
		LastSyncCount:   42,
		LastSyncOutcome: OutcomeOK,
		Failed: map[string]FailedEntry{
			"sess-xyz": {
				Reason:                "size_limit_exceeded",
				Category:              CategoryPermanent,
				Attempts:              1,
				SizeBytes:             5242881,
				FirstAttemptAt:        now,
				LastAttemptAt:         now,
				LastObservedUpdatedAt: now.Add(-5 * time.Minute),
				NextAttemptAt:         nil,
			},
			"sess-abc": {
				Reason:                "cloud_rejected_retryable",
				Category:              CategoryTransient,
				Attempts:              2,
				SizeBytes:             1234,
				FirstAttemptAt:        now.Add(-24 * time.Hour),
				LastAttemptAt:         now,
				LastObservedUpdatedAt: now.Add(-30 * time.Minute),
				NextAttemptAt:         &next,
			},
		},
	}

	if err := WriteMarkerAtomic(path, want); err != nil {
		t.Fatalf("WriteMarkerAtomic: %v", err)
	}
	got, err := ReadMarker(path)
	if err != nil {
		t.Fatalf("ReadMarker: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round-trip mismatch:\n got: %+v\nwant: %+v", got, want)
	}
}

func TestMarker_MissingFileReturnsEpoch(t *testing.T) {
	dir := t.TempDir()
	m, err := ReadMarker(filepath.Join(dir, "does-not-exist.json"))
	if err != nil {
		t.Fatalf("ReadMarker on missing file: %v", err)
	}
	if !m.LastSyncAt.IsZero() {
		t.Errorf("missing file should produce epoch marker; got LastSyncAt=%v", m.LastSyncAt)
	}
	if m.Version != MarkerVersion {
		t.Errorf("missing file should produce v%d marker; got Version=%d", MarkerVersion, m.Version)
	}
}

func TestMarker_UnknownVersionSidecarsAndResets(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sync_marker.json")

	// Write a marker with a future version directly.
	bad := []byte(`{"version":999,"last_sync_at":"2099-01-01T00:00:00Z","failed":{}}`)
	if err := os.WriteFile(path, bad, 0o644); err != nil {
		t.Fatalf("seed bad marker: %v", err)
	}

	m, err := ReadMarker(path)
	if err != nil {
		t.Fatalf("ReadMarker should not error on unknown version: %v", err)
	}
	if !m.LastSyncAt.IsZero() {
		t.Errorf("expected epoch-reset marker, got LastSyncAt=%v", m.LastSyncAt)
	}
	if m.Version != MarkerVersion {
		t.Errorf("expected current version %d, got %d", MarkerVersion, m.Version)
	}

	// Sidecar must exist and contain the original bytes.
	sidecar := filepath.Join(dir, "sync_marker.json.unknown-v999.bak")
	side, err := os.ReadFile(sidecar)
	if err != nil {
		t.Fatalf("expected sidecar at %s: %v", sidecar, err)
	}
	if string(side) != string(bad) {
		t.Errorf("sidecar contents mismatch:\n got: %s\nwant: %s", side, bad)
	}

	// Original file should remain in place (it will be replaced on next successful write).
	if _, err := os.Stat(path); err != nil {
		t.Errorf("original marker file should still exist after sidecar copy: %v", err)
	}
}

func TestMarker_CorruptJSONResetsAndSidecars(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sync_marker.json")

	if err := os.WriteFile(path, []byte("not json {{{"), 0o644); err != nil {
		t.Fatalf("seed corrupt marker: %v", err)
	}

	m, err := ReadMarker(path)
	if err != nil {
		t.Fatalf("ReadMarker should not error on corrupt file: %v", err)
	}
	if !m.LastSyncAt.IsZero() {
		t.Errorf("expected epoch-reset marker, got LastSyncAt=%v", m.LastSyncAt)
	}
	sidecar := filepath.Join(dir, "sync_marker.json.corrupt.bak")
	if _, err := os.Stat(sidecar); err != nil {
		t.Errorf("expected corrupt sidecar at %s: %v", sidecar, err)
	}
}

// Regression for the no-lockout contract: after an unknown-version file is
// sidecared, the next successful WriteMarkerAtomic must replace the original
// with a fresh v1 file, while the sidecar remains in place for triage.
func TestMarker_UnknownVersionFullRecoveryCycle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sync_marker.json")

	bad := []byte(`{"version":999,"last_sync_at":"2099-01-01T00:00:00Z","failed":{}}`)
	if err := os.WriteFile(path, bad, 0o644); err != nil {
		t.Fatalf("seed bad marker: %v", err)
	}

	// Step 1: ReadMarker returns epoch + sidecars the bad file.
	m, err := ReadMarker(path)
	if err != nil {
		t.Fatalf("ReadMarker: %v", err)
	}
	sidecarPath := filepath.Join(dir, "sync_marker.json.unknown-v999.bak")
	if _, err := os.Stat(sidecarPath); err != nil {
		t.Fatalf("sidecar should exist after read: %v", err)
	}

	// Step 2: a normal Run-style flow writes the recovered marker back.
	m.LastSyncAt = time.Date(2026, 4, 19, 3, 0, 0, 0, time.UTC)
	m.LastSyncCount = 1
	m.LastSyncOutcome = OutcomeOK
	if err := WriteMarkerAtomic(path, m); err != nil {
		t.Fatalf("WriteMarkerAtomic: %v", err)
	}

	// Step 3: original file is now a clean v1 marker.
	got, err := ReadMarker(path)
	if err != nil {
		t.Fatalf("ReadMarker post-write: %v", err)
	}
	if got.Version != MarkerVersion {
		t.Errorf("post-write Version: got %d, want %d", got.Version, MarkerVersion)
	}
	if got.LastSyncOutcome != OutcomeOK {
		t.Errorf("post-write Outcome: got %q, want %q", got.LastSyncOutcome, OutcomeOK)
	}

	// Step 4: sidecar still intact (operator can inspect it).
	sideBytes, err := os.ReadFile(sidecarPath)
	if err != nil {
		t.Fatalf("sidecar should still exist post-write: %v", err)
	}
	if string(sideBytes) != string(bad) {
		t.Errorf("sidecar contents changed unexpectedly")
	}
}

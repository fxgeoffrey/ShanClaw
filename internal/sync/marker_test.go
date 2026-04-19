package sync

import (
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

package sync

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// fakeLoader returns a marshaled session with a body of `bodySize` bytes for the
// given id. Lets tests dial size precisely.
func fakeLoader(payloads map[string]int) SessionLoader {
	return func(dir, id string) ([]byte, error) {
		size, ok := payloads[id]
		if !ok {
			return nil, &os.PathError{Op: "open", Path: id, Err: os.ErrNotExist}
		}
		// Construct a minimal JSON object with a `pad` field of the right size.
		// The total marshaled size will be size + a small constant overhead.
		obj := map[string]string{"id": id, "pad": strings.Repeat("a", size)}
		return json.Marshal(obj)
	}
}

func TestBatcher_PacksUnderCap(t *testing.T) {
	cands := []Candidate{
		{SessionID: "a", AgentName: ""},
		{SessionID: "b", AgentName: ""},
		{SessionID: "c", AgentName: ""},
	}
	loader := fakeLoader(map[string]int{"a": 100, "b": 100, "c": 100})

	cfg := DefaultConfig()
	cfg.BatchMaxSessions = 25
	cfg.BatchMaxBytes = 1024
	cfg.SingleSessionMaxBytes = 1024

	marker := emptyMarker()
	now := time.Now().UTC()

	batches, err := BuildBatches(context.Background(), cands, loader, cfg, &marker, now)
	if err != nil {
		t.Fatalf("BuildBatches: %v", err)
	}
	if len(batches) != 1 {
		t.Fatalf("expected 1 batch, got %d", len(batches))
	}
	if len(batches[0].Sessions) != 3 {
		t.Errorf("expected 3 sessions in batch, got %d", len(batches[0].Sessions))
	}
}

func TestBatcher_SplitsOnSessionCount(t *testing.T) {
	cands := []Candidate{}
	payloads := map[string]int{}
	for i := 0; i < 30; i++ {
		id := "s" + strings.Repeat("0", 0) + string(rune('a'+i%26))
		// Make IDs unique even past 26.
		id = id + string(rune('A'+i/26))
		cands = append(cands, Candidate{SessionID: id})
		payloads[id] = 50
	}
	loader := fakeLoader(payloads)

	cfg := DefaultConfig()
	cfg.BatchMaxSessions = 25
	cfg.BatchMaxBytes = 1024 * 1024
	cfg.SingleSessionMaxBytes = 1024 * 1024

	marker := emptyMarker()
	now := time.Now().UTC()

	batches, err := BuildBatches(context.Background(), cands, loader, cfg, &marker, now)
	if err != nil {
		t.Fatalf("BuildBatches: %v", err)
	}
	if len(batches) != 2 {
		t.Fatalf("expected 2 batches (25 + 5), got %d", len(batches))
	}
	if len(batches[0].Sessions) != 25 || len(batches[1].Sessions) != 5 {
		t.Errorf("unexpected split: batch sizes %d, %d", len(batches[0].Sessions), len(batches[1].Sessions))
	}
}

func TestBatcher_SplitsOnBytes(t *testing.T) {
	cands := []Candidate{
		{SessionID: "big1"}, {SessionID: "big2"}, {SessionID: "big3"},
	}
	loader := fakeLoader(map[string]int{
		"big1": 600, "big2": 600, "big3": 600,
	})

	cfg := DefaultConfig()
	cfg.BatchMaxSessions = 25
	cfg.BatchMaxBytes = 1500           // can fit 2 (each ~620 bytes incl JSON overhead) but not 3
	cfg.SingleSessionMaxBytes = 100000 // permissive single-session cap

	marker := emptyMarker()
	now := time.Now().UTC()

	batches, err := BuildBatches(context.Background(), cands, loader, cfg, &marker, now)
	if err != nil {
		t.Fatalf("BuildBatches: %v", err)
	}
	if len(batches) != 2 {
		t.Fatalf("expected 2 batches due to byte cap, got %d (sizes: %v)", len(batches), batchSizes(batches))
	}
}

func batchSizes(bs []Batch) []int {
	out := make([]int, len(bs))
	for i, b := range bs {
		out[i] = len(b.Sessions)
	}
	return out
}

func TestBatcher_RejectsOversizedSession(t *testing.T) {
	cands := []Candidate{
		{SessionID: "ok", UpdatedAt: time.Date(2026, 4, 19, 1, 0, 0, 0, time.UTC)},
		{SessionID: "huge", UpdatedAt: time.Date(2026, 4, 19, 2, 0, 0, 0, time.UTC)},
	}
	loader := fakeLoader(map[string]int{
		"ok":   100,
		"huge": 5000,
	})

	cfg := DefaultConfig()
	cfg.BatchMaxSessions = 25
	cfg.BatchMaxBytes = 1024 * 1024
	cfg.SingleSessionMaxBytes = 1000 // huge will exceed this

	marker := emptyMarker()
	now := time.Now().UTC()

	batches, err := BuildBatches(context.Background(), cands, loader, cfg, &marker, now)
	if err != nil {
		t.Fatalf("BuildBatches: %v", err)
	}

	// huge must NOT appear in any batch.
	for _, b := range batches {
		for _, s := range b.Sessions {
			if s.SessionID == "huge" {
				t.Errorf("huge session must not appear in any batch")
			}
		}
	}

	fe, ok := marker.Failed["huge"]
	if !ok {
		t.Fatalf("expected marker.Failed[huge] to be recorded")
	}
	if fe.Reason != "size_limit_exceeded" {
		t.Errorf("Reason: got %q, want size_limit_exceeded", fe.Reason)
	}
	if fe.Category != CategoryPermanent {
		t.Errorf("Category: got %q, want permanent", fe.Category)
	}
	if fe.NextAttemptAt != nil {
		t.Errorf("NextAttemptAt: got %v, want nil (permanent)", fe.NextAttemptAt)
	}
	if fe.SizeBytes == 0 {
		t.Errorf("SizeBytes should be populated, got 0")
	}
	if fe.Attempts != 1 {
		t.Errorf("Attempts: got %d, want 1 (first observation)", fe.Attempts)
	}
	if !fe.LastObservedUpdatedAt.Equal(cands[1].UpdatedAt) {
		t.Errorf("LastObservedUpdatedAt: got %v, want %v", fe.LastObservedUpdatedAt, cands[1].UpdatedAt)
	}
}

func TestBatcher_RejectsOnLoadError(t *testing.T) {
	cands := []Candidate{{SessionID: "missing", UpdatedAt: time.Date(2026, 4, 19, 3, 0, 0, 0, time.UTC)}}
	loader := fakeLoader(map[string]int{}) // missing returns ErrNotExist

	cfg := DefaultConfig()
	cfg.BatchMaxSessions = 25
	cfg.BatchMaxBytes = 1024 * 1024
	cfg.SingleSessionMaxBytes = 1024 * 1024

	marker := emptyMarker()
	now := time.Now().UTC()

	batches, err := BuildBatches(context.Background(), cands, loader, cfg, &marker, now)
	if err != nil {
		t.Fatalf("BuildBatches: %v", err)
	}
	if len(batches) != 0 {
		t.Errorf("expected 0 batches when only candidate fails to load, got %d", len(batches))
	}
	fe, ok := marker.Failed["missing"]
	if !ok {
		t.Fatalf("expected marker.Failed[missing] for load_error")
	}
	if fe.Reason != "load_error" || fe.Category != CategoryPermanent {
		t.Errorf("got Reason=%q Category=%q; want load_error/permanent", fe.Reason, fe.Category)
	}
	if !fe.LastObservedUpdatedAt.Equal(cands[0].UpdatedAt) {
		t.Errorf("LastObservedUpdatedAt: got %v, want %v", fe.LastObservedUpdatedAt, cands[0].UpdatedAt)
	}
}

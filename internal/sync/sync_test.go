package sync

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Kocoro-lab/ShanClaw/internal/client"
	"github.com/Kocoro-lab/ShanClaw/internal/session"
)

// stubAudit captures audit events for assertions.
type stubAudit struct {
	events []map[string]any
}

func (s *stubAudit) Log(event string, fields map[string]any) {
	merged := map[string]any{"_event": event}
	for k, v := range fields {
		merged[k] = v
	}
	s.events = append(s.events, merged)
}

// stubUploader returns a canned response without hitting the network.
type stubUploader struct {
	respFn func(client.SyncBatchRequest) (client.SyncBatchResponse, error)
	calls  int
}

func (s *stubUploader) Send(_ context.Context, batch client.SyncBatchRequest) (client.SyncBatchResponse, error) {
	s.calls++
	return s.respFn(batch)
}

func TestSyncRun_HappyPath(t *testing.T) {
	home := t.TempDir()
	now := time.Now().UTC().Truncate(time.Second)

	// Seed one session in the default sessions dir.
	sd := filepath.Join(home, "sessions")
	if err := os.MkdirAll(sd, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	idx, err := session.OpenIndex(sd)
	if err != nil {
		t.Fatalf("OpenIndex: %v", err)
	}
	sess := &session.Session{
		ID:        "s1",
		CreatedAt: now.Add(-1 * time.Minute),
		UpdatedAt: now.Add(-1 * time.Minute),
	}
	if err := idx.UpsertSession(sess); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}
	idx.Close()
	// Also write the JSON file so the loader finds it.
	jsonPath := filepath.Join(sd, "s1.json")
	body, _ := json.Marshal(sess)
	if err := os.WriteFile(jsonPath, body, 0o644); err != nil {
		t.Fatalf("write session json: %v", err)
	}

	cfg := DefaultConfig()
	cfg.Enabled = true

	uploader := &stubUploader{
		respFn: func(b client.SyncBatchRequest) (client.SyncBatchResponse, error) {
			return client.SyncBatchResponse{Accepted: []string{"s1"}}, nil
		},
	}
	audit := &stubAudit{}
	loader := func(dir, id string) ([]byte, error) {
		return os.ReadFile(filepath.Join(dir, id+".json"))
	}

	deps := Deps{
		Cfg:       cfg,
		HomeDir:   home,
		ClientVer: "shanclaw/test",
		Uploader:  uploader,
		Loader:    loader,
		Audit:     audit,
		Now:       func() time.Time { return now },
	}

	if err := Run(context.Background(), deps); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if uploader.calls != 1 {
		t.Errorf("expected 1 upload call, got %d", uploader.calls)
	}

	m, err := ReadMarker(filepath.Join(home, "sync_marker.json"))
	if err != nil {
		t.Fatalf("ReadMarker: %v", err)
	}
	if !m.LastSyncAt.Equal(sess.UpdatedAt) {
		t.Errorf("LastSyncAt: got %v, want %v", m.LastSyncAt, sess.UpdatedAt)
	}
	if m.LastSyncCount != 1 {
		t.Errorf("LastSyncCount: got %d, want 1", m.LastSyncCount)
	}
	if m.LastSyncOutcome != OutcomeOK {
		t.Errorf("LastSyncOutcome: got %q, want %q", m.LastSyncOutcome, OutcomeOK)
	}

	if len(audit.events) == 0 {
		t.Errorf("expected at least one audit event")
	}
}

func TestSyncRun_DisabledIsNoop(t *testing.T) {
	home := t.TempDir()

	cfg := DefaultConfig()
	cfg.Enabled = false

	uploader := &stubUploader{respFn: func(b client.SyncBatchRequest) (client.SyncBatchResponse, error) {
		t.Fatalf("uploader must not be called when disabled")
		return client.SyncBatchResponse{}, nil
	}}
	audit := &stubAudit{}

	deps := Deps{
		Cfg: cfg, HomeDir: home, Uploader: uploader, Audit: audit,
		Now: func() time.Time { return time.Now().UTC() },
	}
	if err := Run(context.Background(), deps); err != nil {
		t.Fatalf("Run (disabled): %v", err)
	}
	if uploader.calls != 0 {
		t.Errorf("expected 0 upload calls, got %d", uploader.calls)
	}
	if len(audit.events) != 1 || audit.events[0]["outcome"] != OutcomeNoop {
		t.Errorf("expected single noop audit event, got %+v", audit.events)
	}
}

func TestSyncRun_FlockSerializes(t *testing.T) {
	// Two concurrent Run calls on the same HomeDir; second should block then
	// either no-op (marker advanced) or run fresh.
	home := t.TempDir()
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.LockTimeout = 5 * time.Second

	uploader := &stubUploader{respFn: func(b client.SyncBatchRequest) (client.SyncBatchResponse, error) {
		return client.SyncBatchResponse{}, nil
	}}
	deps := Deps{
		Cfg: cfg, HomeDir: home, Uploader: uploader, Audit: &stubAudit{},
		Loader: func(dir, id string) ([]byte, error) { return nil, os.ErrNotExist },
		Now:    func() time.Time { return time.Now().UTC() },
	}

	done := make(chan error, 2)
	go func() { done <- Run(context.Background(), deps) }()
	go func() { done <- Run(context.Background(), deps) }()

	for i := 0; i < 2; i++ {
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Run #%d: %v", i, err)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("Run #%d timed out — flock likely deadlocked", i)
		}
	}
}

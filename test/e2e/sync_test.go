// test/e2e/sync_test.go
package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Kocoro-lab/ShanClaw/internal/client"
	"github.com/Kocoro-lab/ShanClaw/internal/session"
	"github.com/Kocoro-lab/ShanClaw/internal/sync"
)

// TestE2ESync_OfflineHappyPath wires the full sync pipeline (scanner → batcher
// → uploader → marker) against a mock Cloud server that accepts everything.
// First run uploads two seeded sessions across two dirs; second run is a noop
// because the marker advanced past every candidate.
func TestE2ESync_OfflineHappyPath(t *testing.T) {
	home := t.TempDir()
	sd := filepath.Join(home, "sessions")
	if err := os.MkdirAll(sd, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)

	// Seed two sessions across two dirs: default + ops-bot agent.
	for _, spec := range []struct{ agent, id string }{
		{"", "default-1"},
		{"ops-bot", "ops-1"},
	} {
		dir := sd
		if spec.agent != "" {
			dir = filepath.Join(home, "agents", spec.agent, "sessions")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatalf("mkdir agent sessions %q: %v", spec.agent, err)
			}
		}
		idx, err := session.OpenIndex(dir)
		if err != nil {
			t.Fatalf("OpenIndex %q: %v", dir, err)
		}
		s := &session.Session{
			ID:        spec.id,
			CreatedAt: now,
			UpdatedAt: now,
		}
		if err := idx.UpsertSession(s); err != nil {
			t.Fatalf("UpsertSession %q: %v", spec.id, err)
		}
		idx.Close()
		body, err := json.Marshal(s)
		if err != nil {
			t.Fatalf("marshal session %q: %v", spec.id, err)
		}
		if err := os.WriteFile(filepath.Join(dir, spec.id+".json"), body, 0o644); err != nil {
			t.Fatalf("write session json %q: %v", spec.id, err)
		}
	}

	// Mock Cloud server: accept every session it receives.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req client.SyncBatchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ids := []string{}
		for _, env := range req.Sessions {
			var probe struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(env.Session, &probe); err == nil && probe.ID != "" {
				ids = append(ids, probe.ID)
			}
		}
		_ = json.NewEncoder(w).Encode(client.SyncBatchResponse{Accepted: ids})
	}))
	defer srv.Close()

	cfg := sync.Config{
		Enabled:                    true,
		BatchMaxSessions:           25,
		BatchMaxBytes:              5 * 1024 * 1024,
		SingleSessionMaxBytes:      4 * 1024 * 1024,
		DaemonInterval:             24 * time.Hour,
		DaemonStartupDelay:         60 * time.Second,
		FailedMaxAttemptsTransient: 5,
		LockTimeout:                30 * time.Second,
	}

	deps := sync.Deps{
		Cfg:       cfg,
		HomeDir:   home,
		ClientVer: "shanclaw/e2e",
		Uploader:  &sync.CloudUploader{Client: client.NewGatewayClient(srv.URL, "test-key")},
		Loader: func(dir, id string) ([]byte, error) {
			return os.ReadFile(filepath.Join(dir, id+".json"))
		},
		Audit: noopAudit{},
		Now:   func() time.Time { return now },
	}

	if err := sync.Run(context.Background(), deps); err != nil {
		t.Fatalf("Run: %v", err)
	}

	m, err := sync.ReadMarker(filepath.Join(home, "sync_marker.json"))
	if err != nil {
		t.Fatalf("ReadMarker: %v", err)
	}
	if m.LastSyncOutcome != sync.OutcomeOK {
		t.Errorf("outcome: got %q, want %q", m.LastSyncOutcome, sync.OutcomeOK)
	}
	if m.LastSyncCount != 2 {
		t.Errorf("count: got %d, want 2", m.LastSyncCount)
	}

	// Second run should be a noop — marker advanced past every candidate.
	if err := sync.Run(context.Background(), deps); err != nil {
		t.Fatalf("Run #2: %v", err)
	}
	m2, err := sync.ReadMarker(filepath.Join(home, "sync_marker.json"))
	if err != nil {
		t.Fatalf("ReadMarker #2: %v", err)
	}
	if m2.LastSyncOutcome != sync.OutcomeNoop {
		t.Errorf("second-run outcome: got %q, want %q", m2.LastSyncOutcome, sync.OutcomeNoop)
	}
}

// noopAudit satisfies sync.AuditLogger for tests that don't care about events.
type noopAudit struct{}

func (noopAudit) Log(string, map[string]any) {}

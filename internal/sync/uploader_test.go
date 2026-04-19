package sync

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Kocoro-lab/ShanClaw/internal/client"
)

func TestDryRunUploader_WritesOutboxAndAcceptsAll(t *testing.T) {
	dir := t.TempDir()
	u := &DryRunUploader{OutboxDir: dir, Now: func() time.Time {
		return time.Date(2026, 4, 19, 3, 0, 0, 0, time.UTC)
	}}

	batch := client.SyncBatchRequest{
		ClientVersion: "shanclaw/test",
		SyncAt:        time.Date(2026, 4, 19, 3, 0, 0, 0, time.UTC),
		Sessions: []client.SessionEnvelope{
			{AgentName: "", Session: json.RawMessage(`{"id":"a"}`)},
			{AgentName: "ops-bot", Session: json.RawMessage(`{"id":"b"}`)},
		},
	}

	resp, err := u.Send(context.Background(), batch)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(resp.Accepted) != 2 {
		t.Errorf("expected all sessions accepted in dry-run, got %d", len(resp.Accepted))
	}
	if len(resp.Rejected) != 0 {
		t.Errorf("expected no rejections, got %d", len(resp.Rejected))
	}

	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("expected exactly one outbox file, got %v err=%v", entries, err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if !json.Valid(body) {
		t.Errorf("outbox file is not valid JSON: %s", body)
	}
}

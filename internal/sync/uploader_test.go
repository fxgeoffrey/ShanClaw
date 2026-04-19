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

func TestNormalizeResponse_UnknownAcceptedIDDropped(t *testing.T) {
	batch := client.SyncBatchRequest{
		Sessions: []client.SessionEnvelope{
			{Session: json.RawMessage(`{"id":"a"}`)},
		},
	}
	raw := client.SyncBatchResponse{
		Accepted: []string{"a", "ghost"},
	}
	out := normalizeResponse(batch, raw)
	if len(out.Accepted) != 1 || out.Accepted[0] != "a" {
		t.Errorf("expected only [a]; got %v", out.Accepted)
	}
}

func TestNormalizeResponse_DuplicatesDeduped(t *testing.T) {
	batch := client.SyncBatchRequest{
		Sessions: []client.SessionEnvelope{
			{Session: json.RawMessage(`{"id":"a"}`)},
			{Session: json.RawMessage(`{"id":"b"}`)},
		},
	}
	raw := client.SyncBatchResponse{
		Accepted: []string{"a", "a", "b"},
		Rejected: []client.RejectedEntry{
			{ID: "b", Reason: "x"}, // b is in BOTH lists
		},
	}
	out := normalizeResponse(batch, raw)
	if len(out.Accepted) != 1 || out.Accepted[0] != "a" {
		t.Errorf("Accepted should dedupe to [a]; got %v", out.Accepted)
	}
	if len(out.Rejected) != 1 || out.Rejected[0].ID != "b" {
		t.Errorf("b should be in Rejected; got %v", out.Rejected)
	}
	if out.Rejected[0].Reason != "cloud_inconsistent_response" {
		t.Errorf("conflict reason: got %q, want cloud_inconsistent_response", out.Rejected[0].Reason)
	}
}

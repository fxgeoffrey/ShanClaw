package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Kocoro-lab/ShanClaw/internal/client"
)

// Uploader sends a batch of sessions to the server. Implementations must:
//   - return all session IDs in either Accepted or Rejected (per-session ACK)
//   - return an error for transport-level failures (network, 5xx, malformed body)
type Uploader interface {
	Send(ctx context.Context, batch client.SyncBatchRequest) (client.SyncBatchResponse, error)
}

// CloudUploader sends batches to Shannon Cloud via GatewayClient.
type CloudUploader struct {
	Client *client.GatewayClient
}

func (u *CloudUploader) Send(ctx context.Context, batch client.SyncBatchRequest) (client.SyncBatchResponse, error) {
	if u.Client == nil {
		return client.SyncBatchResponse{}, fmt.Errorf("CloudUploader: nil client")
	}
	resp, err := u.Client.SyncSessions(ctx, batch)
	if err != nil {
		return client.SyncBatchResponse{}, err
	}
	return normalizeResponse(batch, resp), nil
}

// DryRunUploader writes each batch to a JSON file in OutboxDir and synthesizes
// a 100%-accepted response. Used for local verification before Cloud is wired.
type DryRunUploader struct {
	OutboxDir string
	Now       func() time.Time // defaults to time.Now if nil
}

func (u *DryRunUploader) Send(_ context.Context, batch client.SyncBatchRequest) (client.SyncBatchResponse, error) {
	if u.OutboxDir == "" {
		return client.SyncBatchResponse{}, fmt.Errorf("DryRunUploader: empty OutboxDir")
	}
	if err := os.MkdirAll(u.OutboxDir, 0o755); err != nil {
		return client.SyncBatchResponse{}, fmt.Errorf("mkdir outbox: %w", err)
	}
	now := time.Now()
	if u.Now != nil {
		now = u.Now()
	}
	body, err := json.MarshalIndent(batch, "", "  ")
	if err != nil {
		return client.SyncBatchResponse{}, fmt.Errorf("marshal dry-run batch: %w", err)
	}
	name := fmt.Sprintf("%s-%d.json", now.UTC().Format("20060102T150405Z"), now.UnixNano()%1000)
	path := filepath.Join(u.OutboxDir, name)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return client.SyncBatchResponse{}, fmt.Errorf("write outbox: %w", err)
	}

	ids := make([]string, 0, len(batch.Sessions))
	for _, env := range batch.Sessions {
		// Pull "id" out of the embedded session JSON to populate Accepted.
		var probe struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(env.Session, &probe)
		if probe.ID != "" {
			ids = append(ids, probe.ID)
		}
	}
	return client.SyncBatchResponse{Accepted: ids}, nil
}

// normalizeResponse implements the spec's anomaly handling:
//   - accepted IDs not in the batch: log + drop
//   - duplicate IDs in accepted or rejected: dedupe
//   - IDs in BOTH lists: treated as transient reject (defensive)
func normalizeResponse(batch client.SyncBatchRequest, resp client.SyncBatchResponse) client.SyncBatchResponse {
	sentIDs := map[string]bool{}
	for _, env := range batch.Sessions {
		var probe struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(env.Session, &probe)
		if probe.ID != "" {
			sentIDs[probe.ID] = true
		}
	}

	rejectedIDs := map[string]string{}
	for _, r := range resp.Rejected {
		if _, ok := sentIDs[r.ID]; !ok {
			continue // ID not sent; drop silently (could log.Printf in caller)
		}
		// First reason wins on duplicates.
		if _, dup := rejectedIDs[r.ID]; !dup {
			rejectedIDs[r.ID] = r.Reason
		}
	}

	acceptedIDs := map[string]bool{}
	for _, id := range resp.Accepted {
		if _, ok := sentIDs[id]; !ok {
			continue
		}
		if _, alsoRejected := rejectedIDs[id]; alsoRejected {
			// Conflict: present in both lists. Force into rejected as transient.
			rejectedIDs[id] = "cloud_inconsistent_response"
			continue
		}
		acceptedIDs[id] = true
	}

	out := client.SyncBatchResponse{
		Accepted: make([]string, 0, len(acceptedIDs)),
		Rejected: make([]client.RejectedEntry, 0, len(rejectedIDs)),
	}
	for id := range acceptedIDs {
		out.Accepted = append(out.Accepted, id)
	}
	for id, reason := range rejectedIDs {
		out.Rejected = append(out.Rejected, client.RejectedEntry{ID: id, Reason: reason})
	}
	return out
}

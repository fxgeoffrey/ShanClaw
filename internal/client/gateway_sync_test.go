package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGatewayClient_SyncSessions_HappyPath(t *testing.T) {
	var capturedAuth string
	var capturedBody []byte
	var capturedPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("X-API-Key")
		capturedPath = r.URL.Path
		capturedBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"accepted":["s1"],"rejected":[]}`))
	}))
	defer srv.Close()

	c := NewGatewayClient(srv.URL, "TEST_KEY")
	resp, err := c.SyncSessions(context.Background(), SyncBatchRequest{
		ClientVersion: "shanclaw/test",
		SyncAt:        time.Date(2026, 4, 19, 3, 0, 0, 0, time.UTC),
		Sessions: []SessionEnvelope{
			{AgentName: "ops-bot", Session: json.RawMessage(`{"id":"s1"}`)},
		},
	})
	if err != nil {
		t.Fatalf("SyncSessions: %v", err)
	}
	if len(resp.Accepted) != 1 || resp.Accepted[0] != "s1" {
		t.Errorf("accepted: got %v, want [s1]", resp.Accepted)
	}
	if capturedAuth != "TEST_KEY" {
		t.Errorf("X-API-Key header: got %q, want TEST_KEY", capturedAuth)
	}
	if capturedPath != "/api/v1/sessions/sync" {
		t.Errorf("path: got %q, want /api/v1/sessions/sync", capturedPath)
	}
	if !strings.Contains(string(capturedBody), `"agent_name":"ops-bot"`) {
		t.Errorf("body should include agent_name=ops-bot; got: %s", capturedBody)
	}
	if !strings.Contains(string(capturedBody), `"session":{"id":"s1"}`) {
		t.Errorf("body should embed session JSON verbatim; got: %s", capturedBody)
	}
}

func TestGatewayClient_SyncSessions_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
	}))
	defer srv.Close()

	c := NewGatewayClient(srv.URL, "K")
	_, err := c.SyncSessions(context.Background(), SyncBatchRequest{})
	if err == nil {
		t.Fatalf("expected error on 503")
	}
}

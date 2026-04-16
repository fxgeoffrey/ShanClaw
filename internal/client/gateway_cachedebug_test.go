package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCacheDebug_DefaultOff(t *testing.T) {
	os.Unsetenv("SHANNON_CACHE_DEBUG")
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	req := CompletionRequest{Messages: []Message{{Role: "user", Content: NewTextContent("hi")}}}
	_ = logCacheDebug(req, "complete")

	path := filepath.Join(tmp, ".shannon", "logs", "cache-debug.log")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected no cache-debug.log without env flag, got err=%v", err)
	}
}

func TestCacheDebug_EnabledByEnv(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("SHANNON_CACHE_DEBUG", "1")

	req := CompletionRequest{Messages: []Message{{Role: "user", Content: NewTextContent("hi")}}}
	reqID := logCacheDebug(req, "complete")
	if reqID == "" {
		t.Fatalf("expected non-empty reqID")
	}
	path := filepath.Join(tmp, ".shannon", "logs", "cache-debug.log")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected cache-debug.log to be written, got err=%v", err)
	}
}

func TestCacheDebug_WritesSessionID(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("SHANNON_CACHE_DEBUG", "1")

	req := CompletionRequest{
		Messages:  []Message{{Role: "user", Content: NewTextContent("hi")}},
		SessionID: "sess-abc123",
	}
	reqID := logCacheDebug(req, "complete")
	if reqID == "" {
		t.Fatal("expected non-empty reqID")
	}

	data, err := os.ReadFile(filepath.Join(tmp, ".shannon", "logs", "cache-debug.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"session_id":"sess-abc123"`) {
		t.Fatalf("session_id not written in log entry: %s", data)
	}
}

func TestCacheDebug_ResponseCarriesSessionID(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("SHANNON_CACHE_DEBUG", "1")

	resp := &CompletionResponse{
		RequestID: "gw-req-1",
		Usage:     Usage{InputTokens: 10, CacheReadTokens: 5, CacheCreationTokens: 0},
	}
	logCacheResponse("abc123", "sess-xyz789", resp)

	data, _ := os.ReadFile(filepath.Join(tmp, ".shannon", "logs", "cache-debug.log"))
	if !strings.Contains(string(data), `"session_id":"sess-xyz789"`) {
		t.Fatalf("session_id missing from resp entry: %s", data)
	}
}

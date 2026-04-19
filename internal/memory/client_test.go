package memory

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func startUDSServer(t *testing.T, handler http.Handler) string {
	t.Helper()
	// Use a short path under os.TempDir() to stay under macOS sun_path's
	// 104-byte limit; t.TempDir() embeds the (long) test name.
	dir, err := os.MkdirTemp("", "tlm")
	if err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(dir, "s")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &httptest.Server{Listener: ln, Config: &http.Server{Handler: handler}}
	srv.Start()
	t.Cleanup(func() { srv.Close(); os.Remove(sock); os.RemoveAll(dir) })
	return sock
}

func TestClient_QueryHappy(t *testing.T) {
	sock := startUDSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Request-ID") == "" {
			t.Fatal("missing X-Request-ID")
		}
		w.Header().Set("X-Request-ID", r.Header.Get("X-Request-ID"))
		_ = json.NewEncoder(w).Encode(ResponseEnvelope{
			ProtocolVersion: 1,
			RequestID:       r.Header.Get("X-Request-ID"),
			Reason:          "ok",
			Candidates:      []QueryCandidate{{Value: "v"}},
		})
	}))
	c := NewClient(sock, 5*time.Second)
	ctx := WithRequestID(context.Background(), "req-test123")
	env, class, err := c.Query(ctx, QueryIntent{Mode: ModeDirectRelation, AnchorMentions: []string{"x"}})
	if err != nil {
		t.Fatal(err)
	}
	if class != ClassOK || env.RequestID != "req-test123" || len(env.Candidates) != 1 {
		t.Fatalf("got %+v class=%v", env, class)
	}
}

func TestClient_AutoMintsRequestID(t *testing.T) {
	var seen string
	sock := startUDSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("X-Request-ID")
		_ = json.NewEncoder(w).Encode(ResponseEnvelope{Reason: "ok"})
	}))
	c := NewClient(sock, 5*time.Second)
	_, _, _ = c.Query(context.Background(), QueryIntent{Mode: ModeDirectRelation, AnchorMentions: []string{"x"}})
	if len(seen) < 5 || seen[:4] != "req-" {
		t.Fatalf("auto-minted ID %q does not match req-<hex>", seen)
	}
}

func TestClient_MalformedJSON_Unavailable(t *testing.T) {
	sock := startUDSServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{not json"))
	}))
	c := NewClient(sock, 5*time.Second)
	_, class, err := c.Query(context.Background(), QueryIntent{Mode: ModeDirectRelation, AnchorMentions: []string{"x"}})
	if class != ClassUnavailable || err == nil {
		t.Fatalf("class=%v err=%v", class, err)
	}
}

func TestClient_DialCtxCancel(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "missing.sock")
	c := NewClient(sock, 5*time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	_, class, err := c.Query(ctx, QueryIntent{Mode: ModeDirectRelation, AnchorMentions: []string{"x"}})
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("dial took %v — ctx cancellation not honored", elapsed)
	}
	if class != ClassUnavailable || err == nil {
		t.Fatalf("class=%v err=%v", class, err)
	}
}

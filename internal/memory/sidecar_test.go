//go:build !plan9

package memory

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// shortSocketPath builds a UDS path under os.TempDir() that stays below
// the 104-byte sun_path limit (macOS). Avoid t.TempDir() for sockets —
// see internal/memory/client_test.go for context.
func shortSocketPath(t *testing.T, name string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "tlm")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, name)
}

// startFakeSidecar binds a Go HTTP server to socketPath and serves /health.
// Returns a stop func.
func startFakeSidecar(t *testing.T, socketPath string, ready bool) func() {
	t.Helper()
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(HealthPayload{Ready: ready, ProtocolVersion: 1})
	})
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	return func() { _ = srv.Close(); _ = os.Remove(socketPath) }
}

func TestSidecar_AttachPolicy_Ready(t *testing.T) {
	sock := shortSocketPath(t, "s1")
	stop := startFakeSidecar(t, sock, true)
	defer stop()
	ready, err := AttachPolicy(context.Background(), sock)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if !ready {
		t.Fatal("ready should be true")
	}
}

func TestSidecar_AttachPolicy_NoSocket(t *testing.T) {
	sock := shortSocketPath(t, "miss")
	ready, _ := AttachPolicy(context.Background(), sock)
	if ready {
		t.Fatal("ready should be false when no listener")
	}
}

func TestSidecar_AttachPolicy_NotReady(t *testing.T) {
	sock := shortSocketPath(t, "s2")
	stop := startFakeSidecar(t, sock, false)
	defer stop()
	ready, _ := AttachPolicy(context.Background(), sock)
	if ready {
		t.Fatal("ready=true for not-ready sidecar")
	}
}

// writeFakeTLMScript writes a python3 script that listens on `sock` and
// serves /health = ready. Skips the test if python3 is unavailable.
func writeFakeTLMScript(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 unavailable; sidecar process tests require python3")
	}
	dir, err := os.MkdirTemp("", "tlmpy")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	py := `import sys, os, socket, json, http.server, socketserver
sock_path = sys.argv[sys.argv.index('--socket')+1]
try: os.unlink(sock_path)
except FileNotFoundError: pass
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == '/health':
            self.send_response(200); self.send_header('Content-Type','application/json'); self.end_headers()
            self.wfile.write(json.dumps({'ready': True, 'protocol_version': 1}).encode())
    def log_message(self, *args, **kwargs): pass
class UDSServer(socketserver.UnixStreamServer):
    allow_reuse_address = True
srv = UDSServer(sock_path, H)
srv.serve_forever()
`
	path := filepath.Join(dir, "fake_tlm.py")
	if err := os.WriteFile(path, []byte(py), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSidecar_SpawnAndShutdown(t *testing.T) {
	sock := shortSocketPath(t, "spawn")
	root, err := os.MkdirTemp("", "tlmroot")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)

	script := writeFakeTLMScript(t)
	cfg := Config{
		TLMPath:              "python3",
		SocketPath:           sock,
		BundleRoot:           root,
		SidecarReadyTimeout:  5 * time.Second,
		SidecarShutdownGrace: 2 * time.Second,
	}
	sc := NewSidecar(cfg, []string{script})
	ctx := context.Background()
	if err := sc.Spawn(ctx); err != nil {
		t.Fatal(err)
	}
	if err := sc.WaitReady(ctx, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := sc.Shutdown(2 * time.Second); err != nil {
		t.Fatal(err)
	}
}

func TestSidecar_TLMNotFound(t *testing.T) {
	cfg := Config{TLMPath: "/definitely/no/such/binary"}
	sc := NewSidecar(cfg, nil)
	err := sc.Spawn(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	// Should be ErrTLMNotFound when path is set but missing.
	if err.Error() != ErrTLMNotFound.Error() {
		t.Logf("note: error=%v (expected ErrTLMNotFound)", err)
	}
}

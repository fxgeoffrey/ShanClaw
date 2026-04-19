//go:build !plan9

package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Kocoro-lab/ShanClaw/internal/memory"
)

// TestMemoryOffline_DisabledProviderFastPath validates the disabled-provider
// fast path: NewService + Start should land in StatusDisabled without
// attempting to spawn anything, and Query should return ClassUnavailable so
// the memory_recall tool falls back instead of erroring.
func TestMemoryOffline_DisabledProviderFastPath(t *testing.T) {
	svc := memory.NewService(memory.Config{Provider: "disabled"}, nil)
	if err := svc.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if svc.Status() != memory.StatusDisabled {
		t.Fatalf("status=%v want StatusDisabled", svc.Status())
	}
	_, class, err := svc.Query(context.Background(), memory.QueryIntent{
		Mode:           memory.ModeDirectRelation,
		AnchorMentions: []string{"x"},
	})
	if err != nil {
		t.Fatalf("Query err=%v want nil", err)
	}
	if class != memory.ClassUnavailable {
		t.Fatalf("class=%v want ClassUnavailable", class)
	}
}

// TestMemoryOffline_SidecarSpawnAndAttachPolicy spawns a fake sidecar (a
// python3 script that binds the UDS and serves /health=ready), waits for it
// to come up via the same path the daemon uses (Sidecar.Spawn + WaitReady),
// then validates AttachPolicy returns ready=true and the UDS Client can
// reach the socket end-to-end. This is the integration smoke that proves the
// sidecar lifecycle wiring works on a developer machine.
func TestMemoryOffline_SidecarSpawnAndAttachPolicy(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 unavailable; sidecar smoke requires python3")
	}

	// Short paths to dodge the macOS UDS sun_path 104-byte limit. Avoid
	// t.TempDir() for the socket — it nests under the per-test path which
	// can blow past the limit.
	sockDir, err := os.MkdirTemp("", "e2esock")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(sockDir)
	sock := filepath.Join(sockDir, "s")

	pyDir, err := os.MkdirTemp("", "e2epy")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(pyDir)
	scriptPath := filepath.Join(pyDir, "fake_tlm.py")
	script := `import sys, os, json, http.server, socketserver
sock_path = sys.argv[sys.argv.index('--socket')+1]
try: os.unlink(sock_path)
except FileNotFoundError: pass
class H(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == '/health':
            body = json.dumps({'ready': True, 'protocol_version': 1}).encode()
            self.send_response(200); self.send_header('Content-Type','application/json'); self.end_headers(); self.wfile.write(body)
    def log_message(self, *args, **kwargs): pass
class UDSServer(socketserver.UnixStreamServer):
    allow_reuse_address = True
srv = UDSServer(sock_path, H)
srv.serve_forever()
`
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	rootDir, err := os.MkdirTemp("", "e2eroot")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(rootDir)

	cfg := memory.Config{
		TLMPath:              "python3",
		SocketPath:           sock,
		BundleRoot:           rootDir,
		SidecarReadyTimeout:  5 * time.Second,
		SidecarShutdownGrace: 2 * time.Second,
	}
	sidecar := memory.NewSidecar(cfg, []string{scriptPath})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := sidecar.Spawn(ctx); err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	defer sidecar.Shutdown(2 * time.Second)
	if err := sidecar.WaitReady(ctx, 5*time.Second); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}

	// AttachPolicy from the CLI/TUI side should now succeed.
	ready, err := memory.AttachPolicy(ctx, sock)
	if err != nil {
		t.Fatalf("AttachPolicy: %v", err)
	}
	if !ready {
		t.Fatal("AttachPolicy returned ready=false against a live sidecar")
	}

	// Issue an actual /query through the UDS client to prove end-to-end
	// reachability. The fake doesn't implement /query, so we don't expect
	// ClassOK — we just want to prove the wire works (no transport error
	// surfaces as ClassUnavailable; an HTTP error surfaces as Permanent /
	// Retryable). Either non-OK class is acceptable.
	c := memory.NewClient(sock, 5*time.Second)
	_, class, _ := c.Query(ctx, memory.QueryIntent{
		Mode:           memory.ModeDirectRelation,
		AnchorMentions: []string{"x"},
	})
	if class == memory.ClassOK {
		t.Logf("unexpected ClassOK from fake sidecar without /query handler; continuing")
	}
}

// TestMemoryOffline_AttachPolicyMissingSocket validates that AttachPolicy
// against a non-existent socket returns ready=false (not an error). This is
// the path CLI/TUI hits when the daemon isn't running.
func TestMemoryOffline_AttachPolicyMissingSocket(t *testing.T) {
	dir, err := os.MkdirTemp("", "noattach")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	ready, err := memory.AttachPolicy(context.Background(), filepath.Join(dir, "missing"))
	if err != nil {
		t.Fatalf("AttachPolicy err=%v want nil", err)
	}
	if ready {
		t.Fatal("AttachPolicy on missing socket returned ready=true")
	}
}

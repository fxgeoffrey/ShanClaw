package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestService_ColdStart_BootstrapsBundle is the regression guard for the
// cold-start deadlock. Before the Service.Start bootstrap, a fresh install
// (no ~/.shannon/memory/current symlink) would spawn the sidecar, the
// sidecar would return ready=false from /health because no bundle was
// loaded, Supervisor.WaitReady would time out, restart budget would burn,
// and Service would go Degraded — all while the puller goroutine never
// started because it's gated behind the same onReady callback that
// Supervisor only invokes on ready=true. Verified to fail without the
// bootstrap (2026-04-20 dogfood).
//
// The fake tlm script (writeFakeTLMScriptGated) mimics the real sidecar's
// readiness gate — ready iff <bundle-root>/current exists — which is what
// exposes the bug; the default fake in service_test.go always reports
// ready=true and therefore can't detect this class of deadlock.
func TestService_ColdStart_BootstrapsBundle(t *testing.T) {
	t.Run("fresh_install", func(t *testing.T) { runColdStartCase(t, coldStartCaseFresh) })
	t.Run("dangling_current", func(t *testing.T) { runColdStartCase(t, coldStartCaseDangling) })
}

type coldStartCase int

const (
	coldStartCaseFresh coldStartCase = iota
	coldStartCaseDangling
)

func runColdStartCase(t *testing.T, c coldStartCase) {
	root := t.TempDir()

	switch c {
	case coldStartCaseDangling:
		// Simulate operator-deleted bundles dir with orphan `current`
		// symlink — os.Readlink would succeed here and skip bootstrap,
		// re-creating the deadlock. os.Stat (what the fix uses) errors on
		// dangling links and correctly triggers bootstrap.
		orphan := filepath.Join(root, "bundles", "2026-04-01T00-00-00Z")
		if err := os.Symlink(orphan, filepath.Join(root, "current")); err != nil {
			t.Fatal(err)
		}
	case coldStartCaseFresh:
		if _, err := os.Stat(filepath.Join(root, "current")); !os.IsNotExist(err) {
			t.Fatalf("precondition: current already present; err=%v", err)
		}
	}

	const ts = "2026-04-20T09-00-00Z"
	commit := []byte("")
	commitSha := sha256Hex(commit)
	manifest := Manifest{
		BundleTs:        ts,
		BundleVersion:   "0.4.0",
		SizeBytes:       int64(len(commit)),
		IntegritySha256: commitSha,
		Files:           []ManifestFile{{Path: ".commit", Size: int64(len(commit)), Sha256: commitSha}},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/memory/bundle/manifest":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(manifest)
		case strings.HasPrefix(r.URL.Path, "/api/v1/memory/bundle/"+ts+"/"):
			_, _ = w.Write(commit)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	sock := shortSockForSvc(t, "cs")
	script := writeFakeTLMScriptGated(t)

	var events []string
	audit := AuditFunc(func(ev string, _ map[string]any) { events = append(events, ev) })

	cfg := Config{
		Provider:             "cloud",
		Endpoint:             srv.URL,
		APIKey:               "test-key",
		SocketPath:           sock,
		BundleRoot:           root,
		TLMPath:              "python3",
		SidecarReadyTimeout:  5 * time.Second,
		SidecarShutdownGrace: 2 * time.Second,
		SidecarRestartMax:    3,
		ClientRequestTimeout: 5 * time.Second,
	}
	svc := NewService(cfg, audit)
	svc.testExtraSpawnArgs = []string{script}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = svc.Stop() })

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if svc.Status() == StatusReady {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if svc.Status() != StatusReady {
		t.Fatalf("cold-start never reached ready; status=%s", svc.Status())
	}

	found := false
	for _, ev := range events {
		if ev == "memory_bootstrap_pull_ok" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected memory_bootstrap_pull_ok audit event; got %v", events)
	}

	target, err := os.Readlink(filepath.Join(root, "current"))
	if err != nil {
		t.Fatalf("current symlink after start: %v", err)
	}
	if filepath.Base(target) != ts {
		t.Fatalf("current→%s want basename %s", target, ts)
	}
}

func sha256Hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// writeFakeTLMScriptGated stands up a UDS HTTP server that mirrors the
// real tlm sidecar's readiness contract: /health reports ready iff a
// <bundle-root>/current symlink exists. Also answers /bundle/reload with
// a well-formed no-op response so direct Reload probes succeed.
func writeFakeTLMScriptGated(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 unavailable; sidecar spawn tests require python3")
	}
	dir, err := os.MkdirTemp("", "tlmgate")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	py := `import sys, os, json, http.server, socketserver
sock = sys.argv[sys.argv.index('--socket')+1]
root = sys.argv[sys.argv.index('--bundle-root')+1]
try: os.unlink(sock)
except FileNotFoundError: pass
class H(http.server.BaseHTTPRequestHandler):
    def _json(self, code, body):
        self.send_response(code); self.send_header('Content-Type','application/json'); self.end_headers()
        self.wfile.write(json.dumps(body).encode())
    def do_GET(self):
        if self.path == '/health':
            ok = os.path.islink(os.path.join(root, 'current'))
            self._json(200 if ok else 503, {'ready': ok, 'protocol_version': 1})
        else:
            self._json(404, {'error': 'not found'})
    def do_POST(self):
        if self.path == '/bundle/reload':
            self._json(200, {'swapped': False, 'trigger': 'push', 'reason': 'noop',
                             'reload_duration_ms': 0.1, 'warnings': [],
                             'protocol_version': 1, 'request_id': 'test'})
        else:
            self._json(404, {'error': 'not found'})
    def log_message(self, *a, **k): pass
class UDSServer(socketserver.UnixStreamServer):
    allow_reuse_address = True
srv = UDSServer(sock, H)
srv.serve_forever()
`
	path := filepath.Join(dir, "fake_tlm_gated.py")
	if err := os.WriteFile(path, []byte(py), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

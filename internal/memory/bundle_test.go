package memory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func holdFlock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

func TestPuller_VersionOutOfRange(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(Manifest{
			BundleTs:      "2026-04-19T03-14-00Z",
			BundleVersion: "0.5.0",
			Files:         []ManifestFile{},
		})
	}))
	defer srv.Close()
	root := t.TempDir()
	p := NewPuller(Config{Provider: "cloud", BundleRoot: root, Endpoint: srv.URL, APIKey: "k"}, nil, nil)
	err := p.tick(context.Background())
	if err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("err=%v", err)
	}
}

func TestPuller_VersionInRange(t *testing.T) {
	for _, v := range []string{"0.4.0", "0.4.5", "0.4.99"} {
		if !versionInRange(v) {
			t.Fatalf("%q should be in range", v)
		}
	}
	for _, v := range []string{"0.3.9", "0.5.0", "1.0.0", "garbage"} {
		if versionInRange(v) {
			t.Fatalf("%q should NOT be in range", v)
		}
	}
}

func TestPuller_NoopWhenSameTs(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "bundles", "2026-04-19T03-14-00Z"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "bundles", "2026-04-19T03-14-00Z"), filepath.Join(root, "current")); err != nil {
		t.Fatal(err)
	}
	if err := WriteFingerprint(root, "k"); err != nil {
		t.Fatal(err)
	}
	fetched := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/manifest") {
			_ = json.NewEncoder(w).Encode(Manifest{
				BundleTs:      "2026-04-19T03-14-00Z",
				BundleVersion: "0.4.0",
				Files:         []ManifestFile{},
			})
			return
		}
		fetched = true
		w.WriteHeader(404)
	}))
	defer srv.Close()
	p := NewPuller(Config{Provider: "cloud", BundleRoot: root, Endpoint: srv.URL, APIKey: "k"}, nil, nil)
	if err := p.tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fetched {
		t.Fatal("should not fetch files for same ts")
	}
}

func TestPuller_TenantSwitch(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "bundles", "old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteFingerprint(root, "old-key"); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(Manifest{
			BundleTs:      "2026-04-19T03-14-00Z",
			BundleVersion: "0.4.0",
			Files:         nil,
		})
	}))
	defer srv.Close()
	p := NewPuller(Config{Provider: "cloud", BundleRoot: root, Endpoint: srv.URL, APIKey: "new-key"}, nil, nil)
	_ = p.tick(context.Background())
	if _, err := os.Stat(filepath.Join(root, "bundles", "old")); !os.IsNotExist(err) {
		t.Fatal("old bundles should have been wiped on tenant switch")
	}
	fp, _ := ReadFingerprint(root)
	if fp != Fingerprint("new-key") {
		t.Fatalf("fp=%q", fp)
	}
}

func TestPuller_FlockContentionSkips(t *testing.T) {
	root := t.TempDir()
	WriteFingerprint(root, "k")
	// Acquire the lock externally and never release it during the test.
	lockPath := filepath.Join(root, "bundle.lock")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := holdFlock(f); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("manifest must not be fetched while lock contended")
	}))
	defer srv.Close()
	p := NewPuller(Config{Provider: "cloud", BundleRoot: root, Endpoint: srv.URL, APIKey: "k"}, nil, nil)
	if err := p.tick(context.Background()); err != nil {
		t.Fatalf("contention should be silent: %v", err)
	}
}

package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type ManifestFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	Sha256 string `json:"sha256"`
}

type Manifest struct {
	BundleTs        string         `json:"bundle_ts"`
	BundleVersion   string         `json:"bundle_version"`
	SizeBytes       int64          `json:"size_bytes"`
	IntegritySha256 string         `json:"integrity_sha256"`
	Files           []ManifestFile `json:"files"`
}

// Sidecar placeholder — Task 7 will replace this with the real definition
// (sidecar.go: managed child process + AttachPolicy). The puller only holds
// a pointer to schedule /bundle/reload after install.
type Sidecar struct{}

// Puller drives the periodic bundle download cycle in cloud mode.
//   - cfg supplies BundleRoot, Endpoint, APIKey.
//   - sidecar may be nil; reload notification is wired in a later task.
//   - audit may be nil; events are dropped silently when so.
type Puller struct {
	cfg     Config
	sidecar *Sidecar
	audit   AuditLogger
	httpc   *http.Client
}

func NewPuller(cfg Config, sidecar *Sidecar, audit AuditLogger) *Puller {
	return &Puller{
		cfg:     cfg,
		sidecar: sidecar,
		audit:   audit,
		httpc:   &http.Client{Timeout: 60 * time.Second},
	}
}

// versionInRange enforces [0.4.0, 0.5.0). Hand-rolled (no semver dep) — the
// constraint is fixed and trivially encodable as integer triplets.
func versionInRange(v string) bool {
	parts := strings.SplitN(v, ".", 3)
	if len(parts) != 3 {
		return false
	}
	var maj, min, pat int
	if _, err := fmt.Sscanf(v, "%d.%d.%d", &maj, &min, &pat); err != nil {
		return false
	}
	if maj != 0 {
		return false
	}
	if min != 4 {
		return false
	}
	return pat >= 0
}

// tick is one iteration of the bundle pull cycle. Steps 1-4 implemented here:
//  1. flock the bundle root (silent skip on contention)
//  2. tenant fingerprint check (wipe local bundles on switch)
//  3. fetch manifest + version range gate
//  4. compare bundle_ts against the current symlink target (no-op if same)
//
// Steps 5-8 (sandbox/stage/atomic install/reload/retention) are landed in a
// later task; installBundle is a stub so this path is testable in isolation.
func (p *Puller) tick(ctx context.Context) error {
	// Step 1: flock
	if err := os.MkdirAll(p.cfg.BundleRoot, 0o755); err != nil {
		return err
	}
	lockPath := filepath.Join(p.cfg.BundleRoot, "bundle.lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		// Contention: another caller is mid-pull; we'll get the next tick.
		return nil
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

	// Step 2: tenant check (cloud-only — caller ensures provider==cloud
	// before invoking tick).
	switched, err := DetectTenantSwitch(p.cfg.BundleRoot, p.cfg.APIKey)
	if err != nil {
		return err
	}
	if switched {
		if err := os.RemoveAll(filepath.Join(p.cfg.BundleRoot, "bundles")); err != nil {
			return err
		}
		_ = os.Remove(filepath.Join(p.cfg.BundleRoot, "current"))
		if err := WriteFingerprint(p.cfg.BundleRoot, p.cfg.APIKey); err != nil {
			return err
		}
		if p.audit != nil {
			p.audit.Log("memory_tenant_switch", map[string]any{})
		}
	}

	// Step 3: fetch manifest + version gate
	mf, err := p.fetchManifest(ctx)
	if err != nil {
		return err
	}
	if !versionInRange(mf.BundleVersion) {
		return fmt.Errorf("bundle_version %q outside [0.4.0, 0.5.0)", mf.BundleVersion)
	}

	// Step 4: compare ts
	if cur := p.currentTs(); cur != "" && mf.BundleTs <= cur {
		return nil
	}

	// Steps 5-8 land in a later task.
	return p.installBundle(ctx, mf)
}

func (p *Puller) fetchManifest(ctx context.Context) (*Manifest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.cfg.Endpoint+"/api/v1/memory/bundle/manifest", nil)
	if err != nil {
		return nil, err
	}
	if p.cfg.APIKey != "" {
		req.Header.Set("X-API-Key", p.cfg.APIKey)
	}
	resp, err := p.httpc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("manifest status %d: %s", resp.StatusCode, body)
	}
	var mf Manifest
	if err := json.NewDecoder(resp.Body).Decode(&mf); err != nil {
		return nil, err
	}
	return &mf, nil
}

// currentTs reads the symlink target for <bundleRoot>/current and returns its
// basename (the bundle ts). Empty string if the symlink is absent or unreadable.
func (p *Puller) currentTs() string {
	target, err := os.Readlink(filepath.Join(p.cfg.BundleRoot, "current"))
	if err != nil {
		return ""
	}
	return filepath.Base(target)
}

// installBundle is implemented in a later task (sandbox + stage + atomic
// install + reload + retention). For now it's a no-op so the manifest /
// tenant / version paths are testable in isolation.
func (p *Puller) installBundle(ctx context.Context, mf *Manifest) error {
	return nil
}

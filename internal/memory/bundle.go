package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

func (p *Puller) installBundle(ctx context.Context, mf *Manifest) error {
	staging := filepath.Join(p.cfg.BundleRoot, "staging", mf.BundleTs)
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return err
	}
	cleanup := func() { _ = os.RemoveAll(staging) }

	for _, f := range mf.Files {
		if err := validateManifestPath(f.Path, staging); err != nil {
			sample := f.Path
			if len(sample) > 64 {
				sample = sample[:64]
			}
			if p.audit != nil {
				p.audit.Log("memory_bundle_unsafe_path", map[string]any{
					"path_sample": sample,
					"reason":      err.Error(),
				})
			}
			cleanup()
			return fmt.Errorf("unsafe manifest path %q: %w", sample, err)
		}
		if err := p.downloadFile(ctx, mf.BundleTs, f, staging); err != nil {
			cleanup()
			return err
		}
	}

	// Atomic install lives in Task 11. Until that lands this is a no-op so
	// the sandbox/stage/sha256 paths are testable end-to-end.
	return p.atomicInstall(staging, mf.BundleTs)
}

// validateManifestPath enforces the path-sandboxing rules from spec §4.2:
// reject empty, null bytes, absolute paths, parent traversal, and any path
// that escapes the staging dir after Clean+Join. This MUST run before any
// network I/O so a malicious manifest cannot trigger a download to an
// unauthorized location.
func validateManifestPath(rel string, stagingDir string) error {
	if rel == "" {
		return fmt.Errorf("empty path")
	}
	if strings.ContainsRune(rel, 0) {
		return fmt.Errorf("null byte in path")
	}
	if strings.HasPrefix(rel, "/") {
		return fmt.Errorf("absolute path")
	}
	cleaned := filepath.Clean(rel)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.Contains(cleaned, string(os.PathSeparator)+"..") {
		return fmt.Errorf("contains parent traversal")
	}
	abs := filepath.Join(stagingDir, cleaned)
	cleanedAbs := filepath.Clean(abs)
	prefix := filepath.Clean(stagingDir) + string(os.PathSeparator)
	if !strings.HasPrefix(cleanedAbs+string(os.PathSeparator), prefix) {
		return fmt.Errorf("escapes staging dir")
	}
	return nil
}

// downloadFile streams one manifest file into the (already-sandboxed) staging
// path while computing its SHA256. Mismatch → return error and let the caller
// clean staging.
func (p *Puller) downloadFile(ctx context.Context, ts string, f ManifestFile, staging string) error {
	target := filepath.Join(staging, filepath.Clean(f.Path))
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.cfg.Endpoint+"/api/v1/memory/bundle/"+ts+"/"+f.Path, nil)
	if err != nil {
		return err
	}
	if p.cfg.APIKey != "" {
		req.Header.Set("X-API-Key", p.cfg.APIKey)
	}
	resp, err := p.httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("file %s status %d", f.Path, resp.StatusCode)
	}
	out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(out, h), resp.Body); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != f.Sha256 {
		if p.audit != nil {
			p.audit.Log("memory_bundle_install_failed", map[string]any{
				"reason":      "sha256_mismatch",
				"path_sample": f.Path,
			})
		}
		return fmt.Errorf("sha256 mismatch on %s: got %s want %s", f.Path, got, f.Sha256)
	}
	return nil
}

// atomicInstall is implemented in Task 11 (rename staging→bundles, symlink
// swap). Currently a no-op so Task 10 can ship sandbox+stage in isolation.
func (p *Puller) atomicInstall(stagingDir, ts string) error {
	return nil
}

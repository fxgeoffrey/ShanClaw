package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const tenantFile = ".tenant_fingerprint"

// Fingerprint returns sha256(apiKey)[:16] as hex. Empty input returns "".
// One-way: the API key bytes never need to be persisted to disk.
func Fingerprint(apiKey string) string {
	if apiKey == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(apiKey))
	return hex.EncodeToString(sum[:])[:16]
}

func ReadFingerprint(bundleRoot string) (string, error) {
	b, err := os.ReadFile(filepath.Join(bundleRoot, tenantFile))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func WriteFingerprint(bundleRoot, apiKey string) error {
	if err := os.MkdirAll(bundleRoot, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(bundleRoot, tenantFile), []byte(Fingerprint(apiKey)), 0o600)
}

// DetectTenantSwitch returns true if the on-disk fingerprint is missing or
// differs from the fingerprint of apiKey. Cloud-only — callers in local /
// disabled mode must not invoke this (the design treats tenant logic as a
// cloud-only concern; see spec §2.4).
func DetectTenantSwitch(bundleRoot, apiKey string) (bool, error) {
	cur, err := ReadFingerprint(bundleRoot)
	if err != nil {
		return false, err
	}
	return cur != Fingerprint(apiKey), nil
}

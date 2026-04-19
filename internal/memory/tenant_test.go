package memory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFingerprintDeterministic(t *testing.T) {
	a := Fingerprint("abc")
	b := Fingerprint("abc")
	if a != b || len(a) != 16 {
		t.Fatalf("got %q %q", a, b)
	}
	if Fingerprint("abc") == Fingerprint("abd") {
		t.Fatal("fingerprints should differ for different keys")
	}
	if Fingerprint("") != "" {
		t.Fatal("empty key should produce empty fingerprint")
	}
}

func TestDetectTenantSwitch(t *testing.T) {
	dir := t.TempDir()
	switched, err := DetectTenantSwitch(dir, "key1")
	if err != nil || !switched {
		t.Fatalf("missing fingerprint should switch: switched=%v err=%v", switched, err)
	}
	if err := WriteFingerprint(dir, "key1"); err != nil {
		t.Fatal(err)
	}
	switched, _ = DetectTenantSwitch(dir, "key1")
	if switched {
		t.Fatal("matching should not switch")
	}
	switched, _ = DetectTenantSwitch(dir, "key2")
	if !switched {
		t.Fatal("different key should switch")
	}
	info, _ := os.Stat(filepath.Join(dir, ".tenant_fingerprint"))
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %v want 0600", info.Mode().Perm())
	}
}

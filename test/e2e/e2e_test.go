// Package e2e contains end-to-end tests for ShanClaw.
//
// Offline tests (no LLM API needed) run by default.
// Live tests require SHANNON_E2E_LIVE=1 and a configured endpoint+API key.
//
// TestMain builds a fresh shan binary from the current checkout into a temp
// directory. All tests that need the binary use testBinary() to get its path.
package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

var builtBinary string

func TestMain(m *testing.M) {
	// Build shan from current source into a temp dir.
	tmp, err := os.MkdirTemp("", "shan-e2e-*")
	if err != nil {
		panic("e2e: failed to create temp dir: " + err.Error())
	}

	bin := filepath.Join(tmp, "shan")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = filepath.Join(repoRoot())
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		panic("e2e: failed to build shan: " + err.Error())
	}
	builtBinary = bin

	code := m.Run()
	os.RemoveAll(tmp)
	os.Exit(code)
}

func testBinary(t *testing.T) string {
	t.Helper()
	if builtBinary == "" {
		t.Fatal("shan binary not built — TestMain should have built it")
	}
	return builtBinary
}

func skipUnlessLive(t *testing.T) {
	t.Helper()
	if os.Getenv("SHANNON_E2E_LIVE") != "1" {
		t.Skip("skipping live E2E test (set SHANNON_E2E_LIVE=1 to run)")
	}
}

func repoRoot() string {
	// test/e2e/ is two levels deep from repo root
	dir, _ := os.Getwd()
	return filepath.Join(dir, "..", "..")
}

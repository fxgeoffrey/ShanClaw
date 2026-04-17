package skills

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// skipIfNoKeychain skips tests that require actual Keychain access.
// Keychain tests only run on darwin and when SHANNON_KEYCHAIN_TEST=1
// is set, to avoid polluting the developer's login keychain during
// routine `go test ./...`.
func skipIfNoKeychain(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("Keychain is only available on darwin")
	}
	if os.Getenv("SHANNON_KEYCHAIN_TEST") != "1" {
		t.Skip("set SHANNON_KEYCHAIN_TEST=1 to run Keychain integration tests")
	}
}

// --- Index file tests (no Keychain dependency) ---

func TestSecretsStore_ConfiguredKeysAfterSet(t *testing.T) {
	skipIfNoKeychain(t)
	dir := t.TempDir()
	store := NewSecretsStore(dir)
	t.Cleanup(func() { _ = store.Delete("test-configured") })

	if err := store.Set("test-configured", map[string]string{"KEY_A": "aaa", "KEY_B": "bbb"}); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	keys := store.ConfiguredKeys("test-configured")
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d: %v", len(keys), keys)
	}
	if keys[0] != "KEY_A" || keys[1] != "KEY_B" {
		t.Errorf("expected sorted [KEY_A, KEY_B], got %v", keys)
	}
}

func TestSecretsStore_ConfiguredKeysMissing(t *testing.T) {
	dir := t.TempDir()
	store := NewSecretsStore(dir)

	keys := store.ConfiguredKeys("nonexistent")
	if keys != nil {
		t.Errorf("expected nil for missing skill, got %v", keys)
	}
}

func TestSecretsStore_NilStore(t *testing.T) {
	// Empty shannonDir → nil store. All methods must be safe.
	store := NewSecretsStore("")
	if store != nil {
		t.Fatal("expected nil store for empty shannonDir")
	}
	if got := store.Get("any"); got != nil {
		t.Error("Get on nil store should return nil")
	}
	if keys := store.ConfiguredKeys("any"); keys != nil {
		t.Error("ConfiguredKeys on nil store should return nil")
	}
	if err := store.Set("any", map[string]string{"K": "V"}); err != nil {
		t.Errorf("Set on nil store should be no-op, got %v", err)
	}
	if err := store.Delete("any"); err != nil {
		t.Errorf("Delete on nil store should be no-op, got %v", err)
	}
	if err := store.DeleteKey("any", "K"); err != nil {
		t.Errorf("DeleteKey on nil store should be no-op, got %v", err)
	}
}

func TestSecretsStore_IndexFilePermissions(t *testing.T) {
	skipIfNoKeychain(t)
	dir := t.TempDir()
	store := NewSecretsStore(dir)
	t.Cleanup(func() { _ = store.Delete("test-perm") })

	if err := store.Set("test-perm", map[string]string{"K": "V"}); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, "secrets-index.json"))
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("expected 0600 permissions, got %o", perm)
	}
}

func TestIsValidEnvKey(t *testing.T) {
	tests := []struct {
		key   string
		valid bool
	}{
		{"API_KEY", true},
		{"GEMINI_API_KEY", true},
		{"A", true},
		{"A1_B2", true},
		{"api_key", false},        // lowercase
		{"1_KEY", false},          // leading digit
		{"_KEY", false},           // leading underscore
		{"API KEY", false},        // space
		{"API-KEY", false},        // dash
		{"", false},               // empty
	}
	for _, tt := range tests {
		if got := IsValidEnvKey(tt.key); got != tt.valid {
			t.Errorf("IsValidEnvKey(%q) = %v, want %v", tt.key, got, tt.valid)
		}
	}
}

// --- Keychain integration tests (opt-in) ---

func TestSecretsStore_SetAndGet_Keychain(t *testing.T) {
	skipIfNoKeychain(t)
	dir := t.TempDir()
	store := NewSecretsStore(dir)
	t.Cleanup(func() { _ = store.Delete("test-setget") })

	if err := store.Set("test-setget", map[string]string{"SERPER_API_KEY": "test-key-123"}); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	got := store.Get("test-setget")
	if got["SERPER_API_KEY"] != "test-key-123" {
		t.Errorf("expected test-key-123, got %q", got["SERPER_API_KEY"])
	}
}

func TestSecretsStore_SetMerges_Keychain(t *testing.T) {
	skipIfNoKeychain(t)
	dir := t.TempDir()
	store := NewSecretsStore(dir)
	t.Cleanup(func() { _ = store.Delete("test-merge") })

	store.Set("test-merge", map[string]string{"KEY_A": "aaa"})
	store.Set("test-merge", map[string]string{"KEY_B": "bbb"})

	got := store.Get("test-merge")
	if got["KEY_A"] != "aaa" {
		t.Errorf("KEY_A should be preserved, got %q", got["KEY_A"])
	}
	if got["KEY_B"] != "bbb" {
		t.Errorf("KEY_B should be added, got %q", got["KEY_B"])
	}
}

func TestSecretsStore_SetOverwrites_Keychain(t *testing.T) {
	skipIfNoKeychain(t)
	dir := t.TempDir()
	store := NewSecretsStore(dir)
	t.Cleanup(func() { _ = store.Delete("test-overwrite") })

	store.Set("test-overwrite", map[string]string{"KEY_A": "old"})
	store.Set("test-overwrite", map[string]string{"KEY_A": "new"})

	got := store.Get("test-overwrite")
	if got["KEY_A"] != "new" {
		t.Errorf("KEY_A should be overwritten, got %q", got["KEY_A"])
	}
}

func TestSecretsStore_Delete_Keychain(t *testing.T) {
	skipIfNoKeychain(t)
	dir := t.TempDir()
	store := NewSecretsStore(dir)

	store.Set("test-delete", map[string]string{"KEY": "val"})
	if err := store.Delete("test-delete"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	if keys := store.ConfiguredKeys("test-delete"); keys != nil {
		t.Errorf("expected nil after delete, got %v", keys)
	}
	if got := store.Get("test-delete"); got != nil {
		t.Errorf("expected nil after delete, got %v", got)
	}
}

func TestSecretsStore_DeleteKey_Keychain(t *testing.T) {
	skipIfNoKeychain(t)
	dir := t.TempDir()
	store := NewSecretsStore(dir)
	t.Cleanup(func() { _ = store.Delete("test-deletekey") })

	store.Set("test-deletekey", map[string]string{"KEY_A": "aaa", "KEY_B": "bbb"})
	if err := store.DeleteKey("test-deletekey", "KEY_A"); err != nil {
		t.Fatalf("DeleteKey failed: %v", err)
	}

	keys := store.ConfiguredKeys("test-deletekey")
	if len(keys) != 1 || keys[0] != "KEY_B" {
		t.Errorf("expected [KEY_B], got %v", keys)
	}
	got := store.Get("test-deletekey")
	if got["KEY_B"] != "bbb" {
		t.Errorf("KEY_B should be preserved, got %q", got["KEY_B"])
	}
}

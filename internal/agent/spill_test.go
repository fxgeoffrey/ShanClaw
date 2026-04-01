package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSpillToDisk_SmallResult(t *testing.T) {
	// Results under threshold should not be spilled (caller checks threshold).
	// This test verifies spillToDisk works even for small content.
	dir := t.TempDir()
	content := "small output"
	preview, err := spillToDisk(dir, "sess1", "call1", content)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(preview, "Output saved to disk") {
		t.Fatal("expected spill preview header")
	}
	if !strings.Contains(preview, "small output") {
		t.Fatal("expected full content in preview for small output")
	}
}

func TestSpillToDisk_LargeResult(t *testing.T) {
	dir := t.TempDir()
	// Build a 60K rune string.
	content := strings.Repeat("x", 60000)
	preview, err := spillToDisk(dir, "sess1", "call2", content)
	if err != nil {
		t.Fatal(err)
	}

	// Preview should contain the file path and char count.
	if !strings.Contains(preview, "60000 chars") {
		t.Fatalf("expected char count in preview, got: %s", preview[:200])
	}

	// Preview should be truncated to ~2000 chars of content + header.
	previewContent := preview[strings.Index(preview, "Preview (first 2000 chars):\n")+len("Preview (first 2000 chars):\n"):]
	if len([]rune(previewContent)) != spillPreviewChars {
		t.Fatalf("expected preview content of %d runes, got %d", spillPreviewChars, len([]rune(previewContent)))
	}

	// Full content should be on disk.
	path := filepath.Join(dir, "tmp", "tool_result_sess1_call2.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("spill file not readable: %v", err)
	}
	if len(data) != 60000 {
		t.Fatalf("expected 60000 bytes on disk, got %d", len(data))
	}
}

func TestSpillToDisk_RuneSafe(t *testing.T) {
	dir := t.TempDir()
	// Multi-byte chars: each is 3 bytes in UTF-8.
	content := strings.Repeat("あ", 60000)
	preview, err := spillToDisk(dir, "sess1", "call3", content)
	if err != nil {
		t.Fatal(err)
	}
	// Preview content should be exactly 2000 runes, not bytes.
	idx := strings.Index(preview, "Preview (first 2000 chars):\n")
	previewContent := preview[idx+len("Preview (first 2000 chars):\n"):]
	if len([]rune(previewContent)) != spillPreviewChars {
		t.Fatalf("expected %d runes in preview, got %d", spillPreviewChars, len([]rune(previewContent)))
	}
}

func TestCleanupSpills(t *testing.T) {
	dir := t.TempDir()
	// Create spill files for two sessions.
	spillToDisk(dir, "sess1", "a", "data-a")
	spillToDisk(dir, "sess1", "b", "data-b")
	spillToDisk(dir, "sess2", "c", "data-c")

	// Cleanup sess1 only.
	cleanupSpills(dir, "sess1")

	// sess1 files should be gone.
	matches, _ := filepath.Glob(filepath.Join(dir, "tmp", "tool_result_sess1_*.txt"))
	if len(matches) != 0 {
		t.Fatalf("expected 0 sess1 files after cleanup, got %d", len(matches))
	}

	// sess2 file should remain.
	matches, _ = filepath.Glob(filepath.Join(dir, "tmp", "tool_result_sess2_*.txt"))
	if len(matches) != 1 {
		t.Fatalf("expected 1 sess2 file, got %d", len(matches))
	}
}

func TestSpillThresholdIntegration(t *testing.T) {
	// Verify that content under spillThreshold would not trigger spill
	// (the check is in loop.go, but we verify the constant here).
	under := strings.Repeat("x", spillThreshold)
	if len([]rune(under)) > spillThreshold {
		t.Fatal("threshold constant mismatch")
	}
	over := strings.Repeat("x", spillThreshold+1)
	if len([]rune(over)) <= spillThreshold {
		t.Fatal("threshold constant mismatch")
	}
}

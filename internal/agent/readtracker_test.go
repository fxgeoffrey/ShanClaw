package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Kocoro-lab/ShanClaw/internal/cwdctx"
)

func TestReadTracker_MarkAndHasRead(t *testing.T) {
	rt := NewReadTracker()

	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hello"), 0644)

	if rt.HasRead(path) {
		t.Error("expected HasRead to return false before MarkRead")
	}

	rt.MarkRead(path)

	if !rt.HasRead(path) {
		t.Error("expected HasRead to return true after MarkRead")
	}
}

func TestReadTracker_RelativePath(t *testing.T) {
	rt := NewReadTracker()

	// Create a file in a temp dir. The tracker should treat absolute and
	// relative paths as the same file when SetCWD is called — this mirrors
	// the explicit-CWD contract the rest of the filesystem tools enforce
	// after the CWD-hardening work; process cwd is NOT consulted.
	dir := t.TempDir()
	path := filepath.Join(dir, "rel.txt")
	os.WriteFile(path, []byte("data"), 0644)

	rt.SetCWD(dir)

	// Mark with absolute, check with relative
	rt.MarkRead(path)
	if !rt.HasRead("rel.txt") {
		t.Error("expected HasRead to match relative path against absolute after SetCWD")
	}
}

// TestReadTracker_RelativePathWithoutCWD verifies that a relative path with
// no SetCWD is treated as a distinct key from its absolute form — the
// tracker must NOT silently resolve against the process cwd.
func TestReadTracker_RelativePathWithoutCWD(t *testing.T) {
	rt := NewReadTracker()

	dir := t.TempDir()
	abs := filepath.Join(dir, "rel.txt")
	os.WriteFile(abs, []byte("data"), 0644)

	rt.MarkRead(abs)
	if rt.HasRead("rel.txt") {
		t.Error("expected relative path to not match absolute when no CWD is set")
	}
}

func TestReadTracker_Symlink(t *testing.T) {
	rt := NewReadTracker()

	dir := t.TempDir()
	real := filepath.Join(dir, "real.txt")
	link := filepath.Join(dir, "link.txt")
	os.WriteFile(real, []byte("data"), 0644)
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlinks not supported")
	}

	// Mark the symlink as read, check via real path
	rt.MarkRead(link)
	if !rt.HasRead(real) {
		t.Error("expected HasRead to resolve symlinks")
	}
}

func TestReadTracker_EmptyPath(t *testing.T) {
	rt := NewReadTracker()
	rt.MarkRead("")
	if rt.HasRead("") {
		t.Error("expected empty path to not be tracked")
	}
}

func TestReadTracker_NonexistentFile(t *testing.T) {
	rt := NewReadTracker()

	dir := t.TempDir()
	path := filepath.Join(dir, "noexist.txt")

	// Should still work (normalization falls back to clean path)
	rt.MarkRead(path)
	if !rt.HasRead(path) {
		t.Error("expected HasRead to work for nonexistent files")
	}
}

func TestReadTracker_NormalizesWithSessionCWD(t *testing.T) {
	rt := NewReadTracker()
	rt.SetCWD("/projects/foo")

	rt.MarkRead("src/main.go")
	if !rt.HasRead("src/main.go") {
		t.Error("should find relative path after MarkRead")
	}
	if !rt.HasRead("/projects/foo/src/main.go") {
		t.Error("should find absolute path equivalent")
	}
	if rt.HasRead("/other/src/main.go") {
		t.Error("should not match different absolute path")
	}
}

func TestReadTracker_ResetTurnReadsKeepsDedupHistory(t *testing.T) {
	rt := NewReadTracker()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(path, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.WithValue(context.Background(), ReadTrackerKey(), rt)
	rt.MarkRead(path)
	RecordFileRead(ctx, path, 0, 0, info.ModTime(), info.Size())

	rt.ResetTurnReads()

	if rt.HasRead(path) {
		t.Fatal("ResetTurnReads must clear read-before-write state for the next user turn")
	}
	if hit, _ := CheckFileReadDedup(ctx, path, 0, 0, info.ModTime(), info.Size()); !hit {
		t.Fatal("ResetTurnReads must preserve file_read dedup history across turns")
	}
}

func TestReadTracker_FileReadDedupKeepsMultipleRangesPerPath(t *testing.T) {
	rt := NewReadTracker()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(path, []byte("line1\nline2\nline3\nline4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.WithValue(context.Background(), ReadTrackerKey(), rt)
	mtime := info.ModTime()
	size := info.Size()
	RecordFileRead(ctx, path, 0, 2, mtime, size)
	RecordFileRead(ctx, path, 0, 4, mtime, size)

	if hit, _ := CheckFileReadDedup(ctx, path, 0, 2, mtime, size); !hit {
		t.Fatal("dedup history should retain the earlier limit=2 range after reading limit=4")
	}
	if hit, _ := CheckFileReadDedup(ctx, path, 0, 4, mtime, size); !hit {
		t.Fatal("dedup history should retain the later limit=4 range")
	}
	if hit, _ := CheckFileReadDedup(ctx, path, 2, 2, mtime, size); hit {
		t.Fatal("different offset+limit range must not dedup")
	}
	if hit, _ := CheckFileReadDedup(ctx, path, 0, 2, mtime.Add(time.Second), size); hit {
		t.Fatal("changed mtime must not dedup")
	}
}

func TestIsMemoryFile_UsesSessionCWD(t *testing.T) {
	memDir := t.TempDir()
	ctx := context.Background()
	ctx = WithMemoryDir(ctx, memDir)
	ctx = cwdctx.WithSessionCWD(ctx, "/projects/foo")

	// Absolute path to memory file
	if !IsMemoryFile(ctx, filepath.Join(memDir, "MEMORY.md")) {
		t.Error("absolute path to MEMORY.md should match")
	}
}

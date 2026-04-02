package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Kocoro-lab/ShanClaw/internal/cwdctx"
)

// readTrackerKey is the context key for ReadTracker.
type readTrackerKey struct{}

// memoryDirKey is the context key for the agent's memory directory path.
type memoryDirKey struct{}

// WithMemoryDir returns a new context with the memory directory set.
func WithMemoryDir(ctx context.Context, dir string) context.Context {
	return context.WithValue(ctx, memoryDirKey{}, dir)
}

// MemoryDirFromContext returns the memory directory from context, or "".
func MemoryDirFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(memoryDirKey{}).(string); ok {
		return v
	}
	return ""
}

// IsMemoryFile returns true if path resolves to the MEMORY.md inside the
// agent's configured memory directory. Returns false when no memory dir
// is set in context (e.g. tool called outside agent loop).
func IsMemoryFile(ctx context.Context, path string) bool {
	dir, ok := ctx.Value(memoryDirKey{}).(string)
	if !ok || dir == "" {
		return false
	}
	resolvedPath := cwdctx.ResolvePath(ctx, path)
	memPath := filepath.Clean(filepath.Join(dir, "MEMORY.md"))
	return strings.EqualFold(resolvedPath, memPath)
}

// ReadTrackerKey returns the context key used to store a ReadTracker.
// Exported for use in tests that need to inject a tracker into context.
func ReadTrackerKey() any { return readTrackerKey{} }

// ReadTracker tracks which files have been read during the current agent turn.
// Used to enforce read-before-edit: file_edit and file_write on existing files
// must be preceded by a file_read of that file.
type ReadTracker struct {
	read map[string]bool
	cwd  string
}

// NewReadTracker creates a new ReadTracker.
func NewReadTracker() *ReadTracker {
	return &ReadTracker{read: make(map[string]bool)}
}

// SetCWD sets the session CWD used for relative path resolution.
func (rt *ReadTracker) SetCWD(cwd string) {
	rt.cwd = cwd
}

// MarkRead records that a file has been read.
func (rt *ReadTracker) MarkRead(path string) {
	norm := normalizePathWithCWD(path, rt.cwd)
	if norm != "" {
		rt.read[norm] = true
	}
}

// HasRead returns true if the file has been read in this turn.
func (rt *ReadTracker) HasRead(path string) bool {
	norm := normalizePathWithCWD(path, rt.cwd)
	if norm == "" {
		return false
	}
	return rt.read[norm]
}

// CheckReadBeforeWrite extracts the ReadTracker from context and returns an error
// if the given path has not been read. Returns nil if the tracker is absent (e.g.,
// tool called outside the agent loop) or the file has been read.
func CheckReadBeforeWrite(ctx context.Context, path string) error {
	rt, ok := ctx.Value(readTrackerKey{}).(*ReadTracker)
	if !ok || rt == nil {
		return nil
	}
	if !rt.HasRead(path) {
		return fmt.Errorf("You must read this file with file_read before editing it. Path: %s", path)
	}
	return nil
}

// normalizePathWithCWD resolves a path to an absolute, clean, symlink-resolved form
// using the given cwd for relative path resolution.
func normalizePathWithCWD(path, cwd string) string {
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) {
		if cwd != "" {
			path = filepath.Join(cwd, path)
		} else {
			wd, err := os.Getwd()
			if err != nil {
				return filepath.Clean(path)
			}
			path = filepath.Join(wd, path)
		}
	}
	path = filepath.Clean(path)
	// Try to resolve symlinks; if it fails (file doesn't exist yet), use the clean path.
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	return path
}

// normalizePath resolves a path using process CWD (backward compatibility).
func normalizePath(path string) string {
	return normalizePathWithCWD(path, "")
}

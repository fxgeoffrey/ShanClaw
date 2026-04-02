package cwdctx

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type contextKey struct{}

// WithSessionCWD stores the session CWD in the context.
func WithSessionCWD(ctx context.Context, cwd string) context.Context {
	return context.WithValue(ctx, contextKey{}, cwd)
}

// FromContext retrieves the session CWD from context, returns "" if unset.
func FromContext(ctx context.Context) string {
	v, _ := ctx.Value(contextKey{}).(string)
	return v
}

// expandHome expands a leading ~ to the user's home directory.
func expandHome(path string) string {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return home
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return filepath.Join(home, path[2:])
	}
	return path
}

// ResolvePath resolves a path against the session CWD in ctx.
// Absolute paths and ~ paths are returned as-is (after ~ expansion).
// Empty or "." returns the session CWD.
// Falls back to os.Getwd() if no session CWD is set.
func ResolvePath(ctx context.Context, path string) string {
	sessionCWD := FromContext(ctx)

	base := sessionCWD
	if base == "" {
		cwd, err := os.Getwd()
		if err == nil {
			base = cwd
		}
	}

	if path == "" || path == "." {
		return base
	}

	if strings.HasPrefix(path, "~") {
		return filepath.Clean(expandHome(path))
	}

	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}

	return filepath.Clean(filepath.Join(base, path))
}

// IsUnderSessionCWD checks if the resolved path is under the session CWD.
// Both paths are symlink-resolved to prevent escapes via symlinks pointing
// outside the CWD.
func IsUnderSessionCWD(ctx context.Context, path string) bool {
	sessionCWD := FromContext(ctx)
	if sessionCWD == "" {
		return false
	}
	resolved := ResolvePath(ctx, path)
	// Resolve symlinks on both sides to prevent escape via symlink targets.
	// If EvalSymlinks fails (path doesn't exist yet), try resolving the
	// parent directory and rejoin the filename. This handles the common case
	// of referencing a file that doesn't exist yet in a symlinked dir.
	resolved = evalSymlinksBestEffort(resolved)
	cwdClean := evalSymlinksBestEffort(filepath.Clean(sessionCWD))
	if resolved == cwdClean {
		return true
	}
	return strings.HasPrefix(resolved, cwdClean+string(filepath.Separator))
}

// ResolveEffectiveCWD returns the first non-empty value among requestCWD,
// sessionCWD, agentCWD, falling back to os.Getwd().
func ResolveEffectiveCWD(requestCWD, sessionCWD, agentCWD string) string {
	for _, cwd := range []string{requestCWD, sessionCWD, agentCWD} {
		if cwd != "" {
			return cwd
		}
	}
	cwd, _ := os.Getwd()
	return cwd
}

// ValidateCWD validates that cwd is an absolute path to an existing directory.
// Empty string returns nil (means "use fallback").
func ValidateCWD(cwd string) error {
	if cwd == "" {
		return nil
	}
	if !filepath.IsAbs(cwd) {
		return fmt.Errorf("cwd must be an absolute path, got %q", cwd)
	}
	info, err := os.Stat(cwd)
	if err != nil {
		return fmt.Errorf("cwd %q: %w", cwd, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("cwd %q is not a directory", cwd)
	}
	return nil
}

// evalSymlinksBestEffort resolves symlinks as far up the path as possible.
// If the full path doesn't exist, it walks up the directory tree until it
// finds an existing ancestor, resolves that, and rejoins the tail.
func evalSymlinksBestEffort(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	// Walk up until we find an existing ancestor to resolve.
	clean := filepath.Clean(path)
	for dir := filepath.Dir(clean); dir != clean; clean, dir = dir, filepath.Dir(dir) {
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			tail, _ := filepath.Rel(dir, filepath.Clean(path))
			return filepath.Join(real, tail)
		}
	}
	return path
}

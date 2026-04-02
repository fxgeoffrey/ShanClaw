package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/Kocoro-lab/ShanClaw/internal/cwdctx"
)

// ExpandHome expands a leading ~ in a path to the user's home directory.
// Returns the path unchanged if it doesn't start with ~.
func ExpandHome(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}

// isPathUnderSessionCWD returns true if the given path resolves to a location
// under the session CWD from context. Falls back to isPathUnderCWD if no
// session CWD is set.
func isPathUnderSessionCWD(ctx context.Context, path string) bool {
	if cwdctx.FromContext(ctx) != "" {
		return cwdctx.IsUnderSessionCWD(ctx, path)
	}
	return isPathUnderCWD(path)
}

// isPathUnderCWD returns true if the given path resolves to a location
// under the current working directory. Used by read-only tools to
// auto-approve safe paths.
func isPathUnderCWD(path string) bool {
	if path == "" || path == "." {
		return true
	}

	cwd, err := os.Getwd()
	if err != nil {
		return false
	}

	// Expand ~ before resolving
	path = ExpandHome(path)

	// Resolve the path to absolute
	absPath := path
	if !filepath.IsAbs(path) {
		absPath = filepath.Join(cwd, path)
	}
	absPath = filepath.Clean(absPath)

	cwdClean := filepath.Clean(cwd)
	if absPath == cwdClean {
		return true
	}
	return strings.HasPrefix(absPath, cwdClean+string(filepath.Separator))
}

package tools

import (
	"os"
	"path/filepath"
	"strings"
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

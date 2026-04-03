package agents

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:embed builtin
var builtinFS embed.FS

// BuiltinNames lists the names of all bundled specialist agents.
var BuiltinNames = []string{"explorer", "reviewer"}

// EnsureBuiltins syncs embedded agent definitions to agentsDir/_builtin/.
// Skips if the on-disk version matches currentVersion (idempotent).
// Uses write-to-temp-then-rename for atomicity: .version is written last.
func EnsureBuiltins(agentsDir, currentVersion string) error {
	builtinDir := filepath.Join(agentsDir, "_builtin")
	versionFile := filepath.Join(builtinDir, ".version")

	// Check existing version
	if data, err := os.ReadFile(versionFile); err == nil {
		if strings.TrimSpace(string(data)) == currentVersion {
			return nil // already synced
		}
	}

	// Ensure _builtin dir exists
	if err := os.MkdirAll(builtinDir, 0700); err != nil {
		return err
	}

	// Walk embedded FS and write each file
	err := fs.WalkDir(builtinFS, "builtin", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Strip "builtin/" prefix to get relative path under _builtin/
		rel := strings.TrimPrefix(path, "builtin/")
		if rel == "" || rel == "builtin" {
			return nil
		}
		target := filepath.Join(builtinDir, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		data, err := builtinFS.ReadFile(path)
		if err != nil {
			return err
		}
		// Write to temp, then rename for atomicity
		tmp := target + ".tmp"
		if err := os.WriteFile(tmp, data, 0600); err != nil {
			return err
		}
		return os.Rename(tmp, target)
	})
	if err != nil {
		return err
	}

	// Write version file last
	tmp := versionFile + ".tmp"
	if err := os.WriteFile(tmp, []byte(currentVersion), 0600); err != nil {
		return err
	}
	return os.Rename(tmp, versionFile)
}

// IsBuiltinAgent returns true if the given name matches a bundled agent.
func IsBuiltinAgent(name string) bool {
	for _, n := range BuiltinNames {
		if n == name {
			return true
		}
	}
	return false
}

// MaterializeBuiltin copies all definition files from _builtin/<name>/ to
// <name>/ in agentsDir. Used before CRUD writes to ensure the user-override
// directory is self-contained. MEMORY.md is NOT copied (it already lives at
// the top-level runtime dir). Returns nil if the builtin dir doesn't exist.
func MaterializeBuiltin(agentsDir, name string) error {
	src := filepath.Join(agentsDir, "_builtin", name)
	dst := filepath.Join(agentsDir, name)

	if _, err := os.Stat(filepath.Join(src, "AGENT.md")); err != nil {
		return nil // no builtin to materialize
	}

	if err := os.MkdirAll(dst, 0700); err != nil {
		return err
	}

	return fs.WalkDir(os.DirFS(src), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == "." {
			return nil
		}
		// Skip MEMORY.md — runtime state lives at top-level
		if path == "MEMORY.md" {
			return nil
		}
		target := filepath.Join(dst, path)
		if d.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		data, err := os.ReadFile(filepath.Join(src, path))
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0600)
	})
}

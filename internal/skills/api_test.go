package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestEnsureBuiltinSkills_InstallsAllOnFirstRun guards against a typo in the
// builtinSkills slice — every name must end up on disk after a fresh run, with
// real frontmatter content (not a stub).
func TestEnsureBuiltinSkills_InstallsAllOnFirstRun(t *testing.T) {
	shannonDir := t.TempDir()

	if err := EnsureBuiltinSkills(shannonDir); err != nil {
		t.Fatalf("EnsureBuiltinSkills: %v", err)
	}

	for _, name := range builtinSkills {
		skillMD := filepath.Join(shannonDir, "skills", name, "SKILL.md")
		data, err := os.ReadFile(skillMD)
		if err != nil {
			t.Fatalf("builtin %q SKILL.md missing after install: %v", name, err)
		}
		if !strings.Contains(string(data), "name: "+name) {
			t.Fatalf("builtin %q SKILL.md content wrong: %s", name, data)
		}
	}
}

// TestEnsureBuiltinSkills_RestoresDeleted is the headline self-heal test:
// `rm -rf ~/.shannon/skills/<builtin>` must be undone on the next startup.
// This is the user-reported bug the new design fixes.
func TestEnsureBuiltinSkills_RestoresDeleted(t *testing.T) {
	shannonDir := t.TempDir()
	if err := EnsureBuiltinSkills(shannonDir); err != nil {
		t.Fatalf("first EnsureBuiltinSkills: %v", err)
	}

	target := filepath.Join(shannonDir, "skills", "kocoro-generative-ui")
	if err := os.RemoveAll(target); err != nil {
		t.Fatalf("delete builtin: %v", err)
	}

	if err := EnsureBuiltinSkills(shannonDir); err != nil {
		t.Fatalf("restore EnsureBuiltinSkills: %v", err)
	}

	if _, err := os.Stat(filepath.Join(target, "SKILL.md")); err != nil {
		t.Fatalf("kocoro-generative-ui not restored: %v", err)
	}
}

// TestEnsureBuiltinSkills_OverwritesUserEdits locks in the new "builtins are
// daemon-managed" semantic: any local edit to a builtin SKILL.md is wiped on
// next startup. Users wanting customization should fork under a different
// skill name.
func TestEnsureBuiltinSkills_OverwritesUserEdits(t *testing.T) {
	shannonDir := t.TempDir()
	if err := EnsureBuiltinSkills(shannonDir); err != nil {
		t.Fatalf("first EnsureBuiltinSkills: %v", err)
	}

	skillMD := filepath.Join(shannonDir, "skills", "kocoro", "SKILL.md")
	if err := os.WriteFile(skillMD, []byte("user-edit"), 0600); err != nil {
		t.Fatalf("edit SKILL.md: %v", err)
	}

	if err := EnsureBuiltinSkills(shannonDir); err != nil {
		t.Fatalf("second EnsureBuiltinSkills: %v", err)
	}

	data, err := os.ReadFile(skillMD)
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}
	// `user-edit` cannot contain `name: kocoro`, so this single positive
	// assertion proves the edit was wiped AND the embed content was written.
	if !strings.Contains(string(data), "name: kocoro") {
		t.Fatalf("user edit survived overlay; got %q, want frontmatter from embed.FS", data)
	}
}

// TestEnsureBuiltinSkills_RemovesStaleFiles confirms orphan files (e.g. a
// reference file the user dropped in, or a leftover from a previous bundled
// version) are removed when overlay fires.
func TestEnsureBuiltinSkills_RemovesStaleFiles(t *testing.T) {
	shannonDir := t.TempDir()
	if err := EnsureBuiltinSkills(shannonDir); err != nil {
		t.Fatalf("first EnsureBuiltinSkills: %v", err)
	}

	stale := filepath.Join(shannonDir, "skills", "kocoro", "references", "removed.md")
	if err := os.MkdirAll(filepath.Dir(stale), 0700); err != nil {
		t.Fatalf("mkdir references: %v", err)
	}
	if err := os.WriteFile(stale, []byte("stale"), 0600); err != nil {
		t.Fatalf("write stale: %v", err)
	}

	if err := EnsureBuiltinSkills(shannonDir); err != nil {
		t.Fatalf("second EnsureBuiltinSkills: %v", err)
	}

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("stale file survived overlay: err=%v", err)
	}
}

// TestEnsureBuiltinSkills_NoOpWhenContentMatches is a performance + safety
// guarantee: when on-disk content already matches embed.FS, the overlay path
// must NOT fire (otherwise we churn mtimes and inotify watchers on every
// startup). Detected by stuffing an artificial old mtime and asserting it
// survives.
func TestEnsureBuiltinSkills_NoOpWhenContentMatches(t *testing.T) {
	shannonDir := t.TempDir()
	if err := EnsureBuiltinSkills(shannonDir); err != nil {
		t.Fatalf("first EnsureBuiltinSkills: %v", err)
	}

	skillMD := filepath.Join(shannonDir, "skills", "kocoro", "SKILL.md")
	pastTime := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(skillMD, pastTime, pastTime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	if err := EnsureBuiltinSkills(shannonDir); err != nil {
		t.Fatalf("second EnsureBuiltinSkills: %v", err)
	}

	info, err := os.Stat(skillMD)
	if err != nil {
		t.Fatalf("stat after second run: %v", err)
	}
	if !info.ModTime().Equal(pastTime) {
		t.Fatalf("SKILL.md was rewritten when content matched; mtime moved %v -> %v", pastTime, info.ModTime())
	}
}

// TestEnsureBuiltinSkills_RemovesLegacyVersionSidecar confirms the previous
// design's `_builtin.version` file is cleaned up on next startup so it
// doesn't accumulate as a stale curiosity.
func TestEnsureBuiltinSkills_RemovesLegacyVersionSidecar(t *testing.T) {
	shannonDir := t.TempDir()
	skillsDir := filepath.Join(shannonDir, "skills")
	if err := os.MkdirAll(skillsDir, 0700); err != nil {
		t.Fatalf("mkdir skills: %v", err)
	}
	legacy := filepath.Join(skillsDir, "_builtin.version")
	if err := os.WriteFile(legacy, []byte("0.0.99"), 0600); err != nil {
		t.Fatalf("write legacy sidecar: %v", err)
	}

	if err := EnsureBuiltinSkills(shannonDir); err != nil {
		t.Fatalf("EnsureBuiltinSkills: %v", err)
	}

	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy _builtin.version survived: err=%v", err)
	}
}

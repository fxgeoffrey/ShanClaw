package skills

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRegistryIndexParse(t *testing.T) {
	raw := `{
		"version": 1,
		"updated_at": "2026-04-06T12:00:00Z",
		"skills": [
			{
				"slug": "self-improving-agent",
				"name": "self-improving-agent",
				"description": "Captures learnings",
				"author": "pskoett",
				"license": "MIT-0",
				"repo": "https://github.com/peterskoett/self-improving-agent",
				"repo_path": "",
				"ref": "main",
				"homepage": "https://clawhub.ai/pskoett/self-improving-agent",
				"downloads": 354000,
				"stars": 3000,
				"version": "3.0.13",
				"security": {
					"virustotal": "benign",
					"openclaw": "benign",
					"scanned_at": "2026-04-01T00:00:00Z"
				},
				"tags": ["productivity", "meta"]
			}
		]
	}`

	var idx RegistryIndex
	if err := json.Unmarshal([]byte(raw), &idx); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if idx.Version != 1 {
		t.Errorf("Version = %d, want 1", idx.Version)
	}
	if len(idx.Skills) != 1 {
		t.Fatalf("len(Skills) = %d, want 1", len(idx.Skills))
	}
	s := idx.Skills[0]
	if s.Slug != "self-improving-agent" {
		t.Errorf("Slug = %q", s.Slug)
	}
	if s.Downloads != 354000 {
		t.Errorf("Downloads = %d, want 354000", s.Downloads)
	}
	if s.Security.VirusTotal != "benign" {
		t.Errorf("Security.VirusTotal = %q, want benign", s.Security.VirusTotal)
	}
	if s.Ref != "main" {
		t.Errorf("Ref = %q, want main", s.Ref)
	}
}

func TestMarketplaceClientFetchAndCache(t *testing.T) {
	var hits int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"version":1,"updated_at":"2026-04-06T00:00:00Z","skills":[{"slug":"demo","name":"demo","description":"d","author":"a","repo":"https://x/y"}]}`))
	}))
	defer ts.Close()

	client := NewMarketplaceClient(ts.URL, 1*time.Hour)
	idx, err := client.Load(context.Background())
	if err != nil {
		t.Fatalf("first Load: %v", err)
	}
	if len(idx.Skills) != 1 || idx.Skills[0].Slug != "demo" {
		t.Fatalf("unexpected index: %+v", idx)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("expected 1 hit, got %d", got)
	}

	// Second call within TTL should not hit the server.
	if _, err := client.Load(context.Background()); err != nil {
		t.Fatalf("second Load: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("expected still 1 hit after cached load, got %d", got)
	}
}

func TestMarketplaceClientStaleOnError(t *testing.T) {
	var fail int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.LoadInt32(&fail) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write([]byte(`{"version":1,"skills":[{"slug":"demo","name":"demo","description":"d","author":"a","repo":"r"}]}`))
	}))
	defer ts.Close()

	// Zero TTL so every call tries to refetch.
	client := NewMarketplaceClient(ts.URL, 0)
	if _, err := client.Load(context.Background()); err != nil {
		t.Fatalf("priming Load: %v", err)
	}

	atomic.StoreInt32(&fail, 1)
	idx, err := client.Load(context.Background())
	if err != nil {
		t.Fatalf("stale Load should succeed, got: %v", err)
	}
	if len(idx.Skills) != 1 {
		t.Errorf("expected 1 skill from stale cache, got %d", len(idx.Skills))
	}
	if !client.IsStale() {
		t.Errorf("expected IsStale() true after serving stale")
	}
}

// TestMarketplaceClientStaleCooldown verifies that once we fall into
// stale mode during a registry outage, subsequent Loads within the
// cooldown window do NOT re-hit the upstream. Otherwise heavy UI
// traffic during an outage would turn into a retry storm.
func TestMarketplaceClientStaleCooldown(t *testing.T) {
	var hits int32
	var fail int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if atomic.LoadInt32(&fail) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write([]byte(`{"version":1,"skills":[{"slug":"demo","name":"demo","description":"d","author":"a","repo":"r"}]}`))
	}))
	defer ts.Close()

	// Zero TTL so every Load would refetch without the cooldown guard.
	// Generous cooldown so it definitely covers the test window.
	client := NewMarketplaceClient(ts.URL, 0)
	client.staleCooldown = 10 * time.Second

	// Prime the cache.
	if _, err := client.Load(context.Background()); err != nil {
		t.Fatalf("priming Load: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("priming hits = %d, want 1", got)
	}

	// Enter outage; first Load after the outage triggers one fetch
	// attempt, then serves stale.
	atomic.StoreInt32(&fail, 1)
	if _, err := client.Load(context.Background()); err != nil {
		t.Fatalf("first stale Load: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Errorf("after first stale Load hits = %d, want 2", got)
	}
	if !client.IsStale() {
		t.Errorf("expected IsStale() true after first stale Load")
	}

	// Three more Loads during cooldown: must NOT retry.
	for i := 0; i < 3; i++ {
		if _, err := client.Load(context.Background()); err != nil {
			t.Fatalf("cooldown Load %d: %v", i, err)
		}
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Errorf("hits after cooldown Loads = %d, want 2 (no retries during cooldown)", got)
	}
}

func TestMarketplaceClientNoCacheNoServer(t *testing.T) {
	// Unreachable URL, no prior cache → must return error.
	client := NewMarketplaceClient("http://127.0.0.1:1/no-such", 1*time.Hour)
	_, err := client.Load(context.Background())
	if err == nil {
		t.Fatal("expected error with no cache and unreachable URL")
	}
}

func TestFilterSortPaginate(t *testing.T) {
	entries := []MarketplaceEntry{
		{Slug: "alpha", Name: "alpha", Description: "The first thing", Author: "alice", Downloads: 10, Stars: 5},
		{Slug: "bravo", Name: "bravo", Description: "Second thing", Author: "bob", Downloads: 100, Stars: 20},
		{Slug: "charlie", Name: "charlie", Description: "Third thing", Author: "alice", Downloads: 50, Stars: 15},
		{Slug: "delta", Name: "delta", Description: "Malicious", Author: "mallory", Downloads: 999,
			Security: SecurityScan{VirusTotal: "malicious"}},
	}

	// Default sort = downloads desc, malicious excluded.
	out, total := FilterSortPaginate(entries, "", "downloads", 1, 10)
	if total != 3 {
		t.Errorf("total = %d, want 3 (malicious excluded)", total)
	}
	if len(out) != 3 {
		t.Fatalf("len(out) = %d, want 3", len(out))
	}
	if out[0].Slug != "bravo" || out[1].Slug != "charlie" || out[2].Slug != "alpha" {
		t.Errorf("downloads sort order wrong: %v %v %v", out[0].Slug, out[1].Slug, out[2].Slug)
	}

	// Sort by name asc.
	out, _ = FilterSortPaginate(entries, "", "name", 1, 10)
	if out[0].Slug != "alpha" || out[2].Slug != "charlie" {
		t.Errorf("name sort order wrong: %v", sluggs(out))
	}

	// Search: matches name, description, author (case-insensitive).
	out, total = FilterSortPaginate(entries, "ALICE", "downloads", 1, 10)
	if total != 2 {
		t.Errorf("alice search total = %d, want 2", total)
	}
	out, total = FilterSortPaginate(entries, "third", "downloads", 1, 10)
	if total != 1 || out[0].Slug != "charlie" {
		t.Errorf("third search wrong: total=%d, %v", total, sluggs(out))
	}

	// Pagination: page 2 of size 2, downloads desc.
	out, total = FilterSortPaginate(entries, "", "downloads", 2, 2)
	if total != 3 {
		t.Errorf("page2 total = %d, want 3", total)
	}
	if len(out) != 1 || out[0].Slug != "alpha" {
		t.Errorf("page2 contents: %v", sluggs(out))
	}

	// Out-of-range page → empty slice, total still correct.
	out, total = FilterSortPaginate(entries, "", "downloads", 99, 10)
	if total != 3 {
		t.Errorf("OOR total = %d, want 3", total)
	}
	if len(out) != 0 {
		t.Errorf("OOR expected empty slice, got %v", sluggs(out))
	}
}

func sluggs(es []MarketplaceEntry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Slug
	}
	return out
}

func TestSlugLockSerializesSameSlug(t *testing.T) {
	locks := NewSlugLocks()
	var order []string
	var mu sync.Mutex

	done := make(chan struct{}, 2)
	start := make(chan struct{})

	go func() {
		<-start
		unlock := locks.Lock("alpha")
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		order = append(order, "A")
		mu.Unlock()
		unlock()
		done <- struct{}{}
	}()
	go func() {
		<-start
		time.Sleep(5 * time.Millisecond) // start after A has the lock
		unlock := locks.Lock("alpha")
		mu.Lock()
		order = append(order, "B")
		mu.Unlock()
		unlock()
		done <- struct{}{}
	}()

	close(start)
	<-done
	<-done
	if len(order) != 2 || order[0] != "A" || order[1] != "B" {
		t.Errorf("expected serialized order [A, B], got %v", order)
	}
}

func TestSlugLockDifferentSlugsConcurrent(t *testing.T) {
	locks := NewSlugLocks()

	unlockA := locks.Lock("alpha")
	defer unlockA()

	// Locking a different slug must not block.
	ch := make(chan struct{})
	go func() {
		unlock := locks.Lock("bravo")
		unlock()
		close(ch)
	}()
	select {
	case <-ch:
		// good
	case <-time.After(200 * time.Millisecond):
		t.Fatal("locking a different slug should not block")
	}
}

func TestStageCleanPayloadExcludesGit(t *testing.T) {
	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "SKILL.md"), "---\nname: demo\ndescription: d\n---\nbody")
	mustWriteExec(t, filepath.Join(src, "scripts/run.sh"), "#!/bin/sh\necho hi")
	mustWrite(t, filepath.Join(src, ".git/config"), "[core]")
	mustWrite(t, filepath.Join(src, ".github/workflows/ci.yml"), "name: ci")
	mustWrite(t, filepath.Join(src, ".gitignore"), "node_modules")

	dst := filepath.Join(t.TempDir(), "stage")
	if err := stageCleanPayload(src, dst); err != nil {
		t.Fatalf("stageCleanPayload: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dst, "SKILL.md")); err != nil {
		t.Errorf("SKILL.md missing: %v", err)
	}
	// Scripts directory must survive AND keep its executable bit. Community
	// skills like self-improving-agent ship scripts/activator.sh that break
	// silently if we strip mode during the copy.
	scriptInfo, err := os.Stat(filepath.Join(dst, "scripts/run.sh"))
	if err != nil {
		t.Errorf("scripts/run.sh missing: %v", err)
	} else if scriptInfo.Mode().Perm()&0100 == 0 {
		t.Errorf("scripts/run.sh lost its executable bit: mode = %v", scriptInfo.Mode().Perm())
	}
	for _, excluded := range []string{".git", ".github", ".gitignore"} {
		if _, err := os.Stat(filepath.Join(dst, excluded)); !os.IsNotExist(err) {
			t.Errorf("%s should have been excluded, got err = %v", excluded, err)
		}
	}
}

func TestStageCleanPayloadRejectsSymlinks(t *testing.T) {
	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "SKILL.md"), "---\nname: demo\ndescription: d\n---\n")
	// Create a symlink pointing outside the src tree.
	if err := os.Symlink("/etc/passwd", filepath.Join(src, "evil")); err != nil {
		t.Skipf("symlink not supported on this filesystem: %v", err)
	}

	dst := filepath.Join(t.TempDir(), "stage")
	err := stageCleanPayload(src, dst)
	if err == nil {
		t.Fatal("expected error on symlink, got nil")
	}
	if !strings.Contains(err.Error(), "symlink") {
		t.Errorf("error should mention symlink, got: %v", err)
	}
	// Stage directory must not contain a half-copied payload.
	if _, statErr := os.Stat(filepath.Join(dst, "SKILL.md")); statErr == nil {
		t.Error("stage dir should be cleaned up on symlink rejection")
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func mustWriteExec(t *testing.T, path, content string) {
	t.Helper()
	mustWrite(t, path, content)
	if err := os.Chmod(path, 0755); err != nil {
		t.Fatalf("chmod: %v", err)
	}
}

// zipFileSpec describes one entry in a test zip fixture.
type zipFileSpec struct {
	name string      // path inside the zip
	body string      // file contents
	mode os.FileMode // file mode (0 means default 0644)
	link string      // non-empty → emit as a symlink to this target
}

// makeZipFixture builds an in-memory zip archive from the given entries.
func makeZipFixture(t *testing.T, specs []zipFileSpec) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, sp := range specs {
		mode := sp.mode
		if mode == 0 {
			mode = 0644
		}
		if sp.link != "" {
			mode |= os.ModeSymlink
		}
		hdr := &zip.FileHeader{
			Name:   sp.name,
			Method: zip.Deflate,
		}
		hdr.SetMode(mode)
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatalf("zip CreateHeader %q: %v", sp.name, err)
		}
		body := sp.body
		if sp.link != "" {
			body = sp.link
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatalf("zip Write %q: %v", sp.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip Close: %v", err)
	}
	return buf.Bytes()
}

func TestExtractZipToSkillSuccess(t *testing.T) {
	zipBytes := makeZipFixture(t, []zipFileSpec{
		{name: "SKILL.md", body: "---\nname: demo\ndescription: d\n---\nbody"},
		{name: "scripts/run.sh", body: "#!/bin/sh\necho hi", mode: 0755},
		{name: "references/schema.md", body: "schema content"},
	})

	destDir := filepath.Join(t.TempDir(), "stage")
	if err := extractZipToSkill(bytes.NewReader(zipBytes), destDir); err != nil {
		t.Fatalf("extractZipToSkill: %v", err)
	}

	// Core files present.
	if _, err := os.Stat(filepath.Join(destDir, "SKILL.md")); err != nil {
		t.Errorf("SKILL.md missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "references", "schema.md")); err != nil {
		t.Errorf("references/schema.md missing: %v", err)
	}
	// Executable bit preserved on scripts.
	info, err := os.Stat(filepath.Join(destDir, "scripts", "run.sh"))
	if err != nil {
		t.Errorf("scripts/run.sh missing: %v", err)
	} else if info.Mode().Perm()&0100 == 0 {
		t.Errorf("scripts/run.sh lost executable bit: %v", info.Mode().Perm())
	}
}

func TestExtractZipToSkillExcludesGitMetadata(t *testing.T) {
	zipBytes := makeZipFixture(t, []zipFileSpec{
		{name: "SKILL.md", body: "---\nname: demo\ndescription: d\n---\n"},
		{name: ".git/config", body: "[core]"},
		{name: ".github/workflows/ci.yml", body: "name: ci"},
		{name: ".gitignore", body: "node_modules"},
		{name: ".gitattributes", body: "* text"},
	})

	destDir := filepath.Join(t.TempDir(), "stage")
	if err := extractZipToSkill(bytes.NewReader(zipBytes), destDir); err != nil {
		t.Fatalf("extractZipToSkill: %v", err)
	}
	for _, excluded := range []string{".git", ".github", ".gitignore", ".gitattributes"} {
		if _, err := os.Stat(filepath.Join(destDir, excluded)); !os.IsNotExist(err) {
			t.Errorf("%s should have been excluded, got err = %v", excluded, err)
		}
	}
}

func TestExtractZipToSkillRejectsSymlink(t *testing.T) {
	zipBytes := makeZipFixture(t, []zipFileSpec{
		{name: "SKILL.md", body: "---\nname: demo\ndescription: d\n---\n"},
		{name: "evil", link: "/etc/passwd"},
	})

	destDir := filepath.Join(t.TempDir(), "stage")
	err := extractZipToSkill(bytes.NewReader(zipBytes), destDir)
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Errorf("expected symlink rejection error, got: %v", err)
	}
	// Stage dir must be cleaned up on failure.
	if _, statErr := os.Stat(filepath.Join(destDir, "SKILL.md")); statErr == nil {
		t.Error("stage dir should be cleaned up after symlink rejection")
	}
}

func TestExtractZipToSkillRejectsZipSlip(t *testing.T) {
	// Classic zip-slip: entry name escapes the destination via ../
	zipBytes := makeZipFixture(t, []zipFileSpec{
		{name: "SKILL.md", body: "---\nname: demo\ndescription: d\n---\n"},
		{name: "../outside.txt", body: "malicious"},
	})

	destDir := filepath.Join(t.TempDir(), "stage")
	err := extractZipToSkill(bytes.NewReader(zipBytes), destDir)
	if err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Errorf("expected zip-slip rejection error, got: %v", err)
	}
}

func TestExtractZipToSkillRejectsSizeCap(t *testing.T) {
	// Build a tiny zip but feed it through a reader capped tinier than the
	// compressed size. Simulates a server returning more bytes than the cap.
	zipBytes := makeZipFixture(t, []zipFileSpec{
		{name: "SKILL.md", body: strings.Repeat("x", 1024)},
	})

	destDir := filepath.Join(t.TempDir(), "stage")
	// Feed exactly 10 bytes so the zip reader can't even parse the header.
	err := extractZipToSkill(io.LimitReader(bytes.NewReader(zipBytes), 10), destDir)
	if err == nil {
		t.Errorf("expected error from truncated zip, got nil")
	}
}

// makeFixtureRepo creates a minimal git repository on disk that can be
// cloned via file:// URLs. Uses the runGit helper from api.go.
func makeFixtureRepo(t *testing.T, skillContent string) string {
	t.Helper()
	dir := t.TempDir()
	if err := runGit(dir, "init", "-q", "-b", "main"); err != nil {
		t.Fatalf("git init: %v", err)
	}
	mustWrite(t, filepath.Join(dir, "SKILL.md"), skillContent)
	mustWrite(t, filepath.Join(dir, "README.md"), "# demo")
	if err := runGit(dir, "config", "user.email", "test@example.com"); err != nil {
		t.Fatalf("git config email: %v", err)
	}
	if err := runGit(dir, "config", "user.name", "Test"); err != nil {
		t.Fatalf("git config name: %v", err)
	}
	if err := runGit(dir, "config", "commit.gpgsign", "false"); err != nil {
		t.Fatalf("git config gpgsign: %v", err)
	}
	if err := runGit(dir, "add", "."); err != nil {
		t.Fatalf("git add: %v", err)
	}
	if err := runGit(dir, "commit", "-q", "-m", "init"); err != nil {
		t.Fatalf("git commit: %v", err)
	}
	return dir
}

func TestInstallFromMarketplaceSuccess(t *testing.T) {
	repo := makeFixtureRepo(t, "---\nname: demo\ndescription: d\n---\nbody")
	shannonDir := t.TempDir()

	entry := MarketplaceEntry{
		Slug: "demo",
		Name: "demo",
		Repo: "file://" + repo,
		Ref:  "main",
	}
	locks := NewSlugLocks()

	if err := InstallFromMarketplace(shannonDir, entry, locks); err != nil {
		t.Fatalf("InstallFromMarketplace: %v", err)
	}

	installed := filepath.Join(shannonDir, "skills", "demo", "SKILL.md")
	if _, err := os.Stat(installed); err != nil {
		t.Errorf("installed SKILL.md missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(shannonDir, "skills", "demo", ".git")); !os.IsNotExist(err) {
		t.Error(".git should have been excluded from installed skill")
	}
}

func TestInstallFromMarketplaceNameMismatch(t *testing.T) {
	repo := makeFixtureRepo(t, "---\nname: different\ndescription: d\n---\n")
	shannonDir := t.TempDir()

	entry := MarketplaceEntry{Slug: "demo", Name: "demo", Repo: "file://" + repo, Ref: "main"}
	err := InstallFromMarketplace(shannonDir, entry, NewSlugLocks())
	if err == nil || !errors.Is(err, ErrInvalidSkillPayload) {
		t.Errorf("expected ErrInvalidSkillPayload, got: %v", err)
	}
	if _, err := os.Stat(filepath.Join(shannonDir, "skills", "demo")); !os.IsNotExist(err) {
		t.Error("no skill dir should exist after rejected install")
	}
}

func TestInstallFromMarketplaceBlocksMalicious(t *testing.T) {
	shannonDir := t.TempDir()
	entry := MarketplaceEntry{
		Slug:     "demo",
		Name:     "demo",
		Repo:     "file:///does/not/matter",
		Security: SecurityScan{VirusTotal: "malicious"},
	}
	err := InstallFromMarketplace(shannonDir, entry, NewSlugLocks())
	if !errors.Is(err, ErrMaliciousSkill) {
		t.Errorf("expected ErrMaliciousSkill, got: %v", err)
	}
}

func TestInstallFromMarketplaceAlreadyInstalled(t *testing.T) {
	repo := makeFixtureRepo(t, "---\nname: demo\ndescription: d\n---\n")
	shannonDir := t.TempDir()
	entry := MarketplaceEntry{Slug: "demo", Name: "demo", Repo: "file://" + repo, Ref: "main"}
	locks := NewSlugLocks()
	if err := InstallFromMarketplace(shannonDir, entry, locks); err != nil {
		t.Fatalf("first install: %v", err)
	}
	err := InstallFromMarketplace(shannonDir, entry, locks)
	if !errors.Is(err, ErrSkillAlreadyInstalled) {
		t.Errorf("expected ErrSkillAlreadyInstalled, got: %v", err)
	}
}

// makeFixtureRepoSubdir creates a fixture repo where SKILL.md lives under
// skills/<slug>/ rather than at the repo root, plus an unrelated sibling
// directory that must NOT end up in the installed skill. Used to exercise
// the sparse-checkout branch of InstallFromMarketplace.
func makeFixtureRepoSubdir(t *testing.T, slug, skillContent string) string {
	t.Helper()
	dir := t.TempDir()
	if err := runGit(dir, "init", "-q", "-b", "main"); err != nil {
		t.Fatalf("git init: %v", err)
	}
	mustWrite(t, filepath.Join(dir, "skills", slug, "SKILL.md"), skillContent)
	mustWriteExec(t, filepath.Join(dir, "skills", slug, "scripts", "hello.sh"), "#!/bin/sh\necho hi")
	// Sibling skill that must not bleed into the install.
	mustWrite(t, filepath.Join(dir, "skills", "other", "SKILL.md"), "---\nname: other\ndescription: x\n---\n")
	// Unrelated top-level file.
	mustWrite(t, filepath.Join(dir, "README.md"), "# monorepo")

	for _, args := range [][]string{
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"config", "commit.gpgsign", "false"},
		{"add", "."},
		{"commit", "-q", "-m", "init"},
	} {
		if err := runGit(dir, args...); err != nil {
			t.Fatalf("git %v: %v", args, err)
		}
	}
	return dir
}

func TestInstallFromMarketplaceSubdirectory(t *testing.T) {
	repo := makeFixtureRepoSubdir(t, "demo", "---\nname: demo\ndescription: d\n---\nbody")
	shannonDir := t.TempDir()

	entry := MarketplaceEntry{
		Slug:     "demo",
		Name:     "demo",
		Repo:     "file://" + repo,
		RepoPath: "skills/demo",
		Ref:      "main",
	}

	if err := InstallFromMarketplace(shannonDir, entry, NewSlugLocks()); err != nil {
		t.Fatalf("InstallFromMarketplace: %v", err)
	}

	installedRoot := filepath.Join(shannonDir, "skills", "demo")

	// SKILL.md lands at the top of the installed dir, not nested under skills/demo.
	if _, err := os.Stat(filepath.Join(installedRoot, "SKILL.md")); err != nil {
		t.Errorf("installed SKILL.md missing: %v", err)
	}

	// Helper script survives and keeps its executable bit.
	scriptInfo, err := os.Stat(filepath.Join(installedRoot, "scripts", "hello.sh"))
	if err != nil {
		t.Errorf("scripts/hello.sh missing: %v", err)
	} else if scriptInfo.Mode().Perm()&0100 == 0 {
		t.Errorf("scripts/hello.sh lost its executable bit: mode = %v", scriptInfo.Mode().Perm())
	}

	// Unrelated siblings and top-level files must NOT be copied in.
	mustNotExist := []string{
		filepath.Join(installedRoot, "README.md"),
		filepath.Join(installedRoot, "skills"), // no nested skills/ dir
		filepath.Join(installedRoot, "other"),
		filepath.Join(installedRoot, ".git"),
	}
	for _, p := range mustNotExist {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s should not exist in subdirectory install, got err = %v", p, err)
		}
	}
}

// TestInstallFromMarketplaceConcurrentSameSlug drives two goroutines at the
// same slug. With the per-slug lock in place, exactly one must succeed and
// the other must see ErrSkillAlreadyInstalled — no filesystem corruption,
// no ENOTEMPTY from a half-finished rename.
func TestInstallFromMarketplaceConcurrentSameSlug(t *testing.T) {
	repo := makeFixtureRepo(t, "---\nname: demo\ndescription: d\n---\n")
	shannonDir := t.TempDir()
	entry := MarketplaceEntry{Slug: "demo", Name: "demo", Repo: "file://" + repo, Ref: "main"}
	locks := NewSlugLocks()

	const N = 5
	results := make(chan error, N)
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- InstallFromMarketplace(shannonDir, entry, locks)
		}()
	}
	wg.Wait()
	close(results)

	var successes, alreadyInstalled, other int
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrSkillAlreadyInstalled):
			alreadyInstalled++
		default:
			other++
			t.Errorf("unexpected concurrent install error: %v", err)
		}
	}
	if successes != 1 {
		t.Errorf("expected exactly 1 successful install, got %d", successes)
	}
	if alreadyInstalled != N-1 {
		t.Errorf("expected %d already-installed results, got %d", N-1, alreadyInstalled)
	}

	// Final state must be a single clean install.
	if _, err := os.Stat(filepath.Join(shannonDir, "skills", "demo", "SKILL.md")); err != nil {
		t.Errorf("installed SKILL.md missing after concurrent installs: %v", err)
	}
}

func TestMarketplaceEntryIsMalicious(t *testing.T) {
	cases := []struct {
		name string
		e    MarketplaceEntry
		want bool
	}{
		{"clean", MarketplaceEntry{}, false},
		{"benign", MarketplaceEntry{Security: SecurityScan{VirusTotal: "benign", OpenClaw: "benign"}}, false},
		{"vt-malicious", MarketplaceEntry{Security: SecurityScan{VirusTotal: "malicious"}}, true},
		{"oc-malicious", MarketplaceEntry{Security: SecurityScan{OpenClaw: "malicious"}}, true},
		{"suspicious-only", MarketplaceEntry{Security: SecurityScan{VirusTotal: "suspicious"}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.e.IsMalicious(); got != tc.want {
				t.Errorf("IsMalicious = %v, want %v", got, tc.want)
			}
		})
	}
}

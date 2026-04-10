package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Kocoro-lab/ShanClaw/internal/cwdctx"
)

func TestGlob_BasicPattern(t *testing.T) {
	tmp := t.TempDir()
	for _, name := range []string{"a.go", "b.go", "c.md"} {
		if err := os.WriteFile(filepath.Join(tmp, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	tool := &GlobTool{}
	result, err := tool.Run(context.Background(), fmt.Sprintf(`{"pattern":"*.go","path":%q}`, tmp))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", result.Content)
	}
	lines := strings.Split(strings.TrimSpace(result.Content), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 matches, got %d: %s", len(lines), result.Content)
	}
}

func TestGlob_RecursivePattern(t *testing.T) {
	tmp := t.TempDir()
	sub := filepath.Join(tmp, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(tmp, "top.go"), filepath.Join(sub, "nested.go")} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	tool := &GlobTool{}
	result, err := tool.Run(context.Background(), fmt.Sprintf(`{"pattern":"**/*.go","path":%q}`, tmp))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", result.Content)
	}
	lines := strings.Split(strings.TrimSpace(result.Content), "\n")
	if len(lines) != 2 {
		t.Errorf("expected 2 matches, got %d: %s", len(lines), result.Content)
	}
}

func TestGlob_NoMatches(t *testing.T) {
	tmp := t.TempDir()
	tool := &GlobTool{}
	result, err := tool.Run(context.Background(), fmt.Sprintf(`{"pattern":"*.xyz","path":%q}`, tmp))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", result.Content)
	}
	if !strings.Contains(result.Content, "no files matched") {
		t.Errorf("expected 'no files matched', got: %s", result.Content)
	}
}

func TestGlob_MaxResults(t *testing.T) {
	tmp := t.TempDir()
	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("file%02d.txt", i)
		if err := os.WriteFile(filepath.Join(tmp, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	tool := &GlobTool{}
	result, err := tool.Run(context.Background(), fmt.Sprintf(`{"pattern":"*.txt","path":%q,"max_results":3}`, tmp))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", result.Content)
	}
	lines := strings.Split(strings.TrimSpace(result.Content), "\n")
	fileLines := 0
	hasTruncation := false
	for _, l := range lines {
		if strings.Contains(l, "truncated") {
			hasTruncation = true
		} else if l != "" {
			fileLines++
		}
	}
	if fileLines != 3 {
		t.Errorf("expected 3 file lines, got %d: %s", fileLines, result.Content)
	}
	if !hasTruncation {
		t.Errorf("expected truncation notice, got: %s", result.Content)
	}
}

func TestGlob_ContextCancellation(t *testing.T) {
	tmp := t.TempDir()
	for i := 0; i < 5; i++ {
		if err := os.WriteFile(filepath.Join(tmp, fmt.Sprintf("f%d.go", i)), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tool := &GlobTool{}
	// Must not hang; either returns error result or an error
	_, _ = tool.Run(ctx, fmt.Sprintf(`{"pattern":"**/*.go","path":%q}`, tmp))
}

// TestGlob_RelativePatternRefusedWithoutSessionCWD documents the guard:
// a relative path with no session CWD must fail loudly rather than silently
// falling back to os.Getwd() and scanning whatever directory the daemon was
// started from.
func TestGlob_RelativePatternRefusedWithoutSessionCWD(t *testing.T) {
	tool := &GlobTool{}
	result, err := tool.Run(context.Background(), `{"pattern":"*.go"}`)
	if err != nil {
		t.Fatalf("Run should not return a transport error, got %v", err)
	}
	if !result.IsError {
		t.Fatalf("expected error result when session CWD unset and pattern is relative, got: %s", result.Content)
	}
	if !strings.Contains(strings.ToLower(result.Content), "session working directory") &&
		!strings.Contains(strings.ToLower(result.Content), "absolute path") {
		t.Errorf("expected guard message, got: %s", result.Content)
	}
}

// TestGlob_RelativePatternWorksWithSessionCWD ensures the new guard does not
// break legitimate use: when session CWD is set, relative patterns resolve.
func TestGlob_RelativePatternWorksWithSessionCWD(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "x.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := cwdctx.WithSessionCWD(context.Background(), tmp)
	tool := &GlobTool{}
	result, err := tool.Run(ctx, `{"pattern":"*.go"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", result.Content)
	}
	if !strings.Contains(result.Content, "x.go") {
		t.Errorf("expected x.go in results, got: %s", result.Content)
	}
}

func TestGlob_GitignoreRespected(t *testing.T) {
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("rg not available; gitignore test requires rg")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	tmp := t.TempDir()
	if err := exec.Command("git", "init", tmp).Run(); err != nil {
		t.Fatalf("git init failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, ".gitignore"), []byte("ignored/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "visible.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	ignoredDir := filepath.Join(tmp, "ignored")
	if err := os.MkdirAll(ignoredDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ignoredDir, "secret.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	tool := &GlobTool{}
	result, err := tool.Run(context.Background(), fmt.Sprintf(`{"pattern":"**/*.go","path":%q}`, tmp))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatalf("unexpected error result: %s", result.Content)
	}
	if strings.Contains(result.Content, "secret.go") {
		t.Errorf("gitignored file should not appear in results, got: %s", result.Content)
	}
	if !strings.Contains(result.Content, "visible.go") {
		t.Errorf("visible.go should appear in results, got: %s", result.Content)
	}
}

func TestSplitAbsPattern(t *testing.T) {
	tests := []struct {
		pattern  string
		wantRoot string
		wantRel  string
	}{
		{"/a/b/c/{*.md,*.go}", "/a/b/c", "{*.md,*.go}"},
		{"/a/b/*/README.md", "/a/b", "*/README.md"},
		{"/a/b/**/*.go", "/a/b", "**/*.go"},
		{"/a/b/c/file.txt", "/a/b/c", "file.txt"},
	}
	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			gotRoot, gotRel := splitAbsPattern(tt.pattern)
			if gotRoot != tt.wantRoot || gotRel != tt.wantRel {
				t.Errorf("splitAbsPattern(%q) = (%q, %q), want (%q, %q)",
					tt.pattern, gotRoot, gotRel, tt.wantRoot, tt.wantRel)
			}
		})
	}
}

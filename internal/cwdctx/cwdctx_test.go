package cwdctx

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// suppress unused import warning — fmt is used in table tests
var _ = fmt.Sprintf

func TestSessionCWD_RoundTrip(t *testing.T) {
	ctx := context.Background()
	if got := FromContext(ctx); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
	ctx = WithSessionCWD(ctx, "/projects/foo")
	if got := FromContext(ctx); got != "/projects/foo" {
		t.Fatalf("expected /projects/foo, got %q", got)
	}
}

func TestResolvePath_AbsoluteUnchanged(t *testing.T) {
	ctx := WithSessionCWD(context.Background(), "/projects/foo")
	got := ResolvePath(ctx, "/etc/hosts")
	if got != "/etc/hosts" {
		t.Fatalf("absolute path should be unchanged, got %q", got)
	}
}

func TestResolvePath_TildeExpanded(t *testing.T) {
	home, _ := os.UserHomeDir()
	ctx := WithSessionCWD(context.Background(), "/projects/foo")
	got := ResolvePath(ctx, "~/Desktop")
	if got != filepath.Join(home, "Desktop") {
		t.Fatalf("expected %s, got %q", filepath.Join(home, "Desktop"), got)
	}
}

func TestResolvePath_RelativeUsesSessionCWD(t *testing.T) {
	ctx := WithSessionCWD(context.Background(), "/projects/foo")
	got := ResolvePath(ctx, "src/main.go")
	if got != "/projects/foo/src/main.go" {
		t.Fatalf("expected /projects/foo/src/main.go, got %q", got)
	}
}

func TestResolvePath_DotUsesSessionCWD(t *testing.T) {
	ctx := WithSessionCWD(context.Background(), "/projects/foo")
	got := ResolvePath(ctx, ".")
	if got != "/projects/foo" {
		t.Fatalf("expected /projects/foo, got %q", got)
	}
}

func TestResolvePath_EmptyPathReturnsSessionCWD(t *testing.T) {
	ctx := WithSessionCWD(context.Background(), "/projects/foo")
	got := ResolvePath(ctx, "")
	if got != "/projects/foo" {
		t.Fatalf("expected /projects/foo, got %q", got)
	}
}

func TestResolvePath_NoContextFallsBackToGetwd(t *testing.T) {
	cwd, _ := os.Getwd()
	ctx := context.Background()
	got := ResolvePath(ctx, "relative.txt")
	expected := filepath.Join(cwd, "relative.txt")
	if got != expected {
		t.Fatalf("expected %s, got %q", expected, got)
	}
}

func TestIsUnderSessionCWD(t *testing.T) {
	ctx := WithSessionCWD(context.Background(), "/projects/foo")
	tests := []struct {
		path string
		want bool
	}{
		{".", true},
		{"", true},
		{"src/main.go", true},
		{"/projects/foo/bar", true},
		{"/projects/foo", true},
		{"/etc/passwd", false},
		{"../../etc/passwd", false},
		{"/projects/foobar", false},
	}
	for _, tt := range tests {
		if got := IsUnderSessionCWD(ctx, tt.path); got != tt.want {
			t.Errorf("IsUnderSessionCWD(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestResolvePath_CleansTraversalInAbsolutePath(t *testing.T) {
	ctx := WithSessionCWD(context.Background(), "/projects/foo")
	got := ResolvePath(ctx, "/projects/foo/../secret")
	if got != "/projects/secret" {
		t.Fatalf("expected /projects/secret, got %q", got)
	}
}

func TestIsUnderSessionCWD_RejectsAbsoluteTraversal(t *testing.T) {
	ctx := WithSessionCWD(context.Background(), "/projects/foo")
	// Absolute path that traverses out of CWD
	if IsUnderSessionCWD(ctx, "/projects/foo/../secret") {
		t.Error("/projects/foo/../secret should NOT be under /projects/foo after cleaning")
	}
	if IsUnderSessionCWD(ctx, "/projects/foo/../../etc/passwd") {
		t.Error("traversal via absolute path should be rejected")
	}
	// But legitimate subpath should still work
	if !IsUnderSessionCWD(ctx, "/projects/foo/sub/../sub/file.txt") {
		t.Error("/projects/foo/sub/../sub/file.txt should be under CWD after cleaning")
	}
}

func TestResolveEffectiveCWD_Priority(t *testing.T) {
	tests := []struct {
		name    string
		request string
		session string
		agent   string
		want    string
	}{
		{"request wins", "/req", "/sess", "/agent", "/req"},
		{"session wins when no request", "", "/sess", "/agent", "/sess"},
		{"agent wins when no request or session", "", "", "/agent", "/agent"},
		{"fallback to process cwd", "", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveEffectiveCWD(tt.request, tt.session, tt.agent)
			if tt.want != "" && got != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got)
			}
			if tt.want == "" && got == "" {
				t.Error("fallback should return non-empty os.Getwd()")
			}
		})
	}
}

func TestValidateCWD_EmptyIsOK(t *testing.T) {
	if err := ValidateCWD(""); err != nil {
		t.Fatalf("empty cwd should be valid (means fallback), got %v", err)
	}
}

func TestValidateCWD_RejectsRelative(t *testing.T) {
	if err := ValidateCWD("relative/path"); err == nil {
		t.Fatal("expected error for relative path")
	}
}

func TestValidateCWD_RejectsNonexistent(t *testing.T) {
	if err := ValidateCWD("/nonexistent/path/12345"); err == nil {
		t.Fatal("expected error for nonexistent path")
	}
}

func TestValidateCWD_AcceptsRealDir(t *testing.T) {
	dir := t.TempDir()
	if err := ValidateCWD(dir); err != nil {
		t.Fatalf("expected valid, got %v", err)
	}
}

func TestValidateCWD_RejectsFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "notadir.txt")
	os.WriteFile(file, []byte("x"), 0644)
	if err := ValidateCWD(file); err == nil {
		t.Fatal("expected error for file path")
	}
}

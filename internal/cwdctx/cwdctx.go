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
		return expandHome(path)
	}

	if filepath.IsAbs(path) {
		return path
	}

	return filepath.Join(base, path)
}

// IsUnderSessionCWD checks if the resolved path is under the session CWD.
func IsUnderSessionCWD(ctx context.Context, path string) bool {
	sessionCWD := FromContext(ctx)
	if sessionCWD == "" {
		return false
	}
	resolved := ResolvePath(ctx, path)
	// Ensure trailing separator to avoid /projects/foobar matching /projects/foo
	base := sessionCWD
	if !strings.HasSuffix(base, string(filepath.Separator)) {
		base += string(filepath.Separator)
	}
	return resolved == sessionCWD || strings.HasPrefix(resolved, base)
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

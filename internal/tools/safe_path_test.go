package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("cannot get home dir: %v", err)
	}

	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
		{"~", home},
		{"~/Desktop/file.txt", filepath.Join(home, "Desktop/file.txt")},
		{"~/", filepath.Join(home, "")},
		{"~user/foo", "~user/foo"}, // ~user expansion not supported, returned as-is
	}

	for _, tt := range tests {
		got := ExpandHome(tt.input)
		if got != tt.want {
			t.Errorf("ExpandHome(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsPathUnderCWD_WithTilde(t *testing.T) {
	// ~ paths should not be considered under CWD (unless CWD is home)
	home, _ := os.UserHomeDir()
	cwd, _ := os.Getwd()

	// ~/Desktop is only under CWD if CWD is home and Desktop is a subdir
	result := isPathUnderCWD("~/Desktop")
	if cwd == home {
		if !result {
			t.Error("~/Desktop should be under CWD when CWD is home")
		}
	} else {
		if result {
			t.Error("~/Desktop should NOT be under CWD when CWD is not home")
		}
	}
}

package daemon

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setupInstructionsServer(t *testing.T) (*Server, string, context.CancelFunc) {
	t.Helper()
	shannonDir := t.TempDir()
	sessDir := t.TempDir()
	deps := &ServerDeps{
		ShannonDir:   shannonDir,
		SessionCache: NewSessionCache(sessDir),
	}
	c := NewClient("ws://localhost:1/x", "", func(msg MessagePayload) string { return "" }, nil)
	srv := NewServer(0, c, deps, "test")
	ctx, cancel := context.WithCancel(context.Background())
	go srv.Start(ctx)
	time.Sleep(100 * time.Millisecond)
	return srv, shannonDir, cancel
}

// TestServer_PutInstructions_JSON exercises the existing JSON-body path
// (back-compat). The "content" field is taken verbatim and written.
func TestServer_PutInstructions_JSON(t *testing.T) {
	srv, shannonDir, cancel := setupInstructionsServer(t)
	defer cancel()

	req, err := http.NewRequest(
		http.MethodPut,
		fmt.Sprintf("http://127.0.0.1:%d/instructions", srv.Port()),
		strings.NewReader(`{"content":"# Hello\nworld"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	data, err := os.ReadFile(filepath.Join(shannonDir, "instructions.md"))
	if err != nil {
		t.Fatalf("file not written: %v", err)
	}
	// JSON-decoded \n is a real newline
	if string(data) != "# Hello\nworld" {
		t.Errorf("got %q, want %q", string(data), "# Hello\nworld")
	}
}

// TestServer_PutInstructions_RawMarkdown is the headline new behavior:
// Content-Type: text/markdown lets the client send raw bytes that contain
// every character that's painful to JSON-string-escape (quotes, backslashes,
// backticks, newlines, unicode). The body lands on disk byte-for-byte.
func TestServer_PutInstructions_RawMarkdown(t *testing.T) {
	srv, shannonDir, cancel := setupInstructionsServer(t)
	defer cancel()

	raw := "# Global Instructions\n\n" +
		"Use \"double quotes\" and `backticks` and \\backslashes\\ freely.\n" +
		"Multi-line\ncontent\twith tabs.\n" +
		"中文 — 日本語 — emoji 🚀 all pass through unchanged.\n"

	req, err := http.NewRequest(
		http.MethodPut,
		fmt.Sprintf("http://127.0.0.1:%d/instructions", srv.Port()),
		strings.NewReader(raw),
	)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "text/markdown")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	data, err := os.ReadFile(filepath.Join(shannonDir, "instructions.md"))
	if err != nil {
		t.Fatalf("file not written: %v", err)
	}
	if string(data) != raw {
		t.Errorf("file content mismatch.\n got: %q\nwant: %q", string(data), raw)
	}
}

// TestServer_PutInstructions_TextPlain mirrors the markdown path —
// text/plain is also accepted as raw bytes. Charset parameters are ignored.
func TestServer_PutInstructions_TextPlain(t *testing.T) {
	srv, shannonDir, cancel := setupInstructionsServer(t)
	defer cancel()

	raw := `not "json" — just text`

	req, err := http.NewRequest(
		http.MethodPut,
		fmt.Sprintf("http://127.0.0.1:%d/instructions", srv.Port()),
		strings.NewReader(raw),
	)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "text/plain; charset=utf-8")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	data, err := os.ReadFile(filepath.Join(shannonDir, "instructions.md"))
	if err != nil {
		t.Fatalf("file not written: %v", err)
	}
	if string(data) != raw {
		t.Errorf("got %q, want %q", string(data), raw)
	}
}

// TestServer_PutInstructions_DefaultContentTypeStillJSON confirms the
// default (no Content-Type, or application/json) goes through the JSON
// path. Sending raw markdown without the new header should fail decode —
// this is what protects the legacy contract.
func TestServer_PutInstructions_DefaultContentTypeStillJSON(t *testing.T) {
	srv, _, cancel := setupInstructionsServer(t)
	defer cancel()

	req, err := http.NewRequest(
		http.MethodPut,
		fmt.Sprintf("http://127.0.0.1:%d/instructions", srv.Port()),
		strings.NewReader(`# raw markdown without content-type header`),
	)
	if err != nil {
		t.Fatal(err)
	}
	// Intentionally no Content-Type set — falls into the JSON path.

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 (raw text rejected by JSON path), got %d", resp.StatusCode)
	}
}

// TestServer_PutInstructions_NullContentJSONDeletes preserves the existing
// "{\"content\": null}" → delete-file behavior on the JSON path.
func TestServer_PutInstructions_NullContentJSONDeletes(t *testing.T) {
	srv, shannonDir, cancel := setupInstructionsServer(t)
	defer cancel()

	// Seed an instructions file.
	path := filepath.Join(shannonDir, "instructions.md")
	if err := os.WriteFile(path, []byte("seed content"), 0600); err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest(
		http.MethodPut,
		fmt.Sprintf("http://127.0.0.1:%d/instructions", srv.Port()),
		strings.NewReader(`{"content":null}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected file removed, stat err = %v", err)
	}
}

func TestIsTextContentType(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"text/markdown", true},
		{"text/plain", true},
		{"text/markdown; charset=utf-8", true},
		{"TEXT/MARKDOWN", true},
		{"  text/plain  ", true},
		{"application/json", false},
		{"", false},
		{"text/html", false},
		{"application/x-markdown", false},
	}
	for _, tc := range cases {
		got := isTextContentType(tc.in)
		if got != tc.want {
			t.Errorf("isTextContentType(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

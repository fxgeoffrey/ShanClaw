package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"context"
)

func TestServer_ProjectInit_BasicInit(t *testing.T) {
	shannonDir := t.TempDir()
	projectDir := t.TempDir()
	deps := &ServerDeps{
		ShannonDir:   shannonDir,
		SessionCache: NewSessionCache(shannonDir),
	}
	c := NewClient("ws://localhost:1/x", "", func(msg MessagePayload) string { return "" }, nil)
	srv := NewServer(0, c, deps, "test")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Start(ctx)
	time.Sleep(100 * time.Millisecond)

	body := fmt.Sprintf(`{"cwd":%q}`, projectDir)
	resp, err := http.Post(
		fmt.Sprintf("http://127.0.0.1:%d/project/init", srv.Port()),
		"application/json",
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, raw)
	}

	var result struct {
		Created []string `json:"created"`
		Existed []string `json:"existed"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(result.Created) != 1 || result.Created[0] != ".shannon/" {
		t.Errorf("created = %v, want [.shannon/]", result.Created)
	}
	if len(result.Existed) != 0 {
		t.Errorf("existed = %v, want []", result.Existed)
	}

	if _, err := os.Stat(filepath.Join(projectDir, ".shannon")); err != nil {
		t.Errorf(".shannon dir not created: %v", err)
	}
}

func TestServer_ProjectInit_WithInstructions(t *testing.T) {
	shannonDir := t.TempDir()
	projectDir := t.TempDir()
	deps := &ServerDeps{
		ShannonDir:   shannonDir,
		SessionCache: NewSessionCache(shannonDir),
	}
	c := NewClient("ws://localhost:1/x", "", func(msg MessagePayload) string { return "" }, nil)
	srv := NewServer(0, c, deps, "test")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Start(ctx)
	time.Sleep(100 * time.Millisecond)

	body := fmt.Sprintf(`{"cwd":%q,"instructions":"# My Project\n\nDo good stuff."}`, projectDir)
	resp, err := http.Post(
		fmt.Sprintf("http://127.0.0.1:%d/project/init", srv.Port()),
		"application/json",
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, raw)
	}

	var result struct {
		Created []string `json:"created"`
		Existed []string `json:"existed"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	wantCreated := map[string]bool{".shannon/": true, ".shannon/instructions.md": true}
	for _, c := range result.Created {
		if !wantCreated[c] {
			t.Errorf("unexpected created entry: %q", c)
		}
		delete(wantCreated, c)
	}
	if len(wantCreated) > 0 {
		t.Errorf("missing created entries: %v", wantCreated)
	}
	if len(result.Existed) != 0 {
		t.Errorf("existed = %v, want []", result.Existed)
	}

	instPath := filepath.Join(projectDir, ".shannon", "instructions.md")
	data, err := os.ReadFile(instPath)
	if err != nil {
		t.Fatalf("instructions.md not created: %v", err)
	}
	if string(data) != "# My Project\n\nDo good stuff." {
		t.Errorf("instructions content = %q, unexpected", string(data))
	}
}

func TestServer_ProjectInit_RelativePath(t *testing.T) {
	shannonDir := t.TempDir()
	deps := &ServerDeps{
		ShannonDir:   shannonDir,
		SessionCache: NewSessionCache(shannonDir),
	}
	c := NewClient("ws://localhost:1/x", "", func(msg MessagePayload) string { return "" }, nil)
	srv := NewServer(0, c, deps, "test")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Start(ctx)
	time.Sleep(100 * time.Millisecond)

	resp, err := http.Post(
		fmt.Sprintf("http://127.0.0.1:%d/project/init", srv.Port()),
		"application/json",
		strings.NewReader(`{"cwd":"relative/path"}`),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestServer_ProjectInit_InsideShannonDir(t *testing.T) {
	shannonDir := t.TempDir()
	// Try to init inside the global shannon dir itself
	subDir := filepath.Join(shannonDir, "subproject")
	if err := os.MkdirAll(subDir, 0700); err != nil {
		t.Fatalf("mkdir subDir: %v", err)
	}
	deps := &ServerDeps{
		ShannonDir:   shannonDir,
		SessionCache: NewSessionCache(shannonDir),
	}
	c := NewClient("ws://localhost:1/x", "", func(msg MessagePayload) string { return "" }, nil)
	srv := NewServer(0, c, deps, "test")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Start(ctx)
	time.Sleep(100 * time.Millisecond)

	// Test 1: exact shannon dir
	body := fmt.Sprintf(`{"cwd":%q}`, shannonDir)
	resp, err := http.Post(
		fmt.Sprintf("http://127.0.0.1:%d/project/init", srv.Port()),
		"application/json",
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("exact shannonDir: expected 400, got %d", resp.StatusCode)
	}

	// Test 2: subdir inside shannon dir
	body2 := fmt.Sprintf(`{"cwd":%q}`, subDir)
	resp2, err := http.Post(
		fmt.Sprintf("http://127.0.0.1:%d/project/init", srv.Port()),
		"application/json",
		strings.NewReader(body2),
	)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusBadRequest {
		t.Errorf("subDir of shannonDir: expected 400, got %d", resp2.StatusCode)
	}
}

func TestServer_ProjectInit_Idempotent(t *testing.T) {
	shannonDir := t.TempDir()
	projectDir := t.TempDir()
	deps := &ServerDeps{
		ShannonDir:   shannonDir,
		SessionCache: NewSessionCache(shannonDir),
	}
	c := NewClient("ws://localhost:1/x", "", func(msg MessagePayload) string { return "" }, nil)
	srv := NewServer(0, c, deps, "test")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go srv.Start(ctx)
	time.Sleep(100 * time.Millisecond)

	// Pre-create .shannon dir and instructions.md with existing content
	dotShannon := filepath.Join(projectDir, ".shannon")
	if err := os.MkdirAll(dotShannon, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	existingContent := "# Existing Instructions"
	if err := os.WriteFile(filepath.Join(dotShannon, "instructions.md"), []byte(existingContent), 0600); err != nil {
		t.Fatalf("write existing instructions: %v", err)
	}

	body := fmt.Sprintf(`{"cwd":%q,"instructions":"# New Instructions"}`, projectDir)
	resp, err := http.Post(
		fmt.Sprintf("http://127.0.0.1:%d/project/init", srv.Port()),
		"application/json",
		strings.NewReader(body),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d: %s", resp.StatusCode, raw)
	}

	var result struct {
		Created []string `json:"created"`
		Existed []string `json:"existed"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if len(result.Created) != 0 {
		t.Errorf("created = %v, want [] (both already existed)", result.Created)
	}
	wantExisted := map[string]bool{".shannon/": true, ".shannon/instructions.md": true}
	for _, e := range result.Existed {
		if !wantExisted[e] {
			t.Errorf("unexpected existed entry: %q", e)
		}
		delete(wantExisted, e)
	}
	if len(wantExisted) > 0 {
		t.Errorf("missing existed entries: %v", wantExisted)
	}

	// Verify existing file was NOT overwritten
	data, _ := os.ReadFile(filepath.Join(dotShannon, "instructions.md"))
	if string(data) != existingContent {
		t.Errorf("instructions.md was overwritten: got %q, want %q", string(data), existingContent)
	}
}

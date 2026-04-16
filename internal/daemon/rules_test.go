package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setupRulesServer(t *testing.T) (*Server, string, context.CancelFunc) {
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

func TestServer_ListRules_Empty(t *testing.T) {
	srv, _, cancel := setupRulesServer(t)
	defer cancel()

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/rules", srv.Port()))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	var parsed map[string]json.RawMessage
	json.Unmarshal(body, &parsed)
	if string(parsed["rules"]) != "[]" {
		t.Errorf("expected empty rules array, got %s", string(body))
	}
}

func TestServer_ListRules_WithEntries(t *testing.T) {
	srv, shannonDir, cancel := setupRulesServer(t)
	defer cancel()

	rulesDir := filepath.Join(shannonDir, "rules")
	if err := os.MkdirAll(rulesDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rulesDir, "zebra.md"), []byte("z content"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rulesDir, "alpha.md"), []byte("a content"), 0600); err != nil {
		t.Fatal(err)
	}
	// non-.md file should be ignored
	if err := os.WriteFile(filepath.Join(rulesDir, "ignore.txt"), []byte("ignored"), 0600); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/rules", srv.Port()))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var body struct {
		Rules []struct {
			Name    string `json:"name"`
			Content string `json:"content"`
		} `json:"rules"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	if len(body.Rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(body.Rules))
	}
	// sorted alphabetically
	if body.Rules[0].Name != "alpha" {
		t.Errorf("expected first rule to be 'alpha', got %q", body.Rules[0].Name)
	}
	if body.Rules[0].Content != "a content" {
		t.Errorf("expected content 'a content', got %q", body.Rules[0].Content)
	}
	if body.Rules[1].Name != "zebra" {
		t.Errorf("expected second rule to be 'zebra', got %q", body.Rules[1].Name)
	}
}

func TestServer_GetRule(t *testing.T) {
	srv, shannonDir, cancel := setupRulesServer(t)
	defer cancel()

	rulesDir := filepath.Join(shannonDir, "rules")
	if err := os.MkdirAll(rulesDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rulesDir, "my-rule.md"), []byte("rule body"), 0600); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/rules/my-rule", srv.Port()))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var entry struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	json.NewDecoder(resp.Body).Decode(&entry)
	if entry.Name != "my-rule" {
		t.Errorf("expected name 'my-rule', got %q", entry.Name)
	}
	if entry.Content != "rule body" {
		t.Errorf("expected content 'rule body', got %q", entry.Content)
	}
}

func TestServer_GetRule_NotFound(t *testing.T) {
	srv, _, cancel := setupRulesServer(t)
	defer cancel()

	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/rules/nonexistent", srv.Port()))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

func TestServer_PutRule_Create(t *testing.T) {
	srv, shannonDir, cancel := setupRulesServer(t)
	defer cancel()

	req, err := http.NewRequest(
		http.MethodPut,
		fmt.Sprintf("http://127.0.0.1:%d/rules/new-rule", srv.Port()),
		strings.NewReader(`{"content":"hello world"}`),
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

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.StatusCode)
	}

	// verify file on disk
	data, err := os.ReadFile(filepath.Join(shannonDir, "rules", "new-rule.md"))
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("expected 'hello world', got %q", string(data))
	}
}

func TestServer_PutRule_Update(t *testing.T) {
	srv, shannonDir, cancel := setupRulesServer(t)
	defer cancel()

	rulesDir := filepath.Join(shannonDir, "rules")
	if err := os.MkdirAll(rulesDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rulesDir, "existing.md"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest(
		http.MethodPut,
		fmt.Sprintf("http://127.0.0.1:%d/rules/existing", srv.Port()),
		strings.NewReader(`{"content":"updated"}`),
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

	data, err := os.ReadFile(filepath.Join(rulesDir, "existing.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "updated" {
		t.Errorf("expected 'updated', got %q", string(data))
	}
}

func TestServer_PutRule_EmptyContent(t *testing.T) {
	srv, _, cancel := setupRulesServer(t)
	defer cancel()

	req, err := http.NewRequest(
		http.MethodPut,
		fmt.Sprintf("http://127.0.0.1:%d/rules/my-rule", srv.Port()),
		strings.NewReader(`{"content":"   "}`),
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

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestServer_DeleteRule_NoConfirm(t *testing.T) {
	srv, _, cancel := setupRulesServer(t)
	defer cancel()

	req, err := http.NewRequest(
		http.MethodDelete,
		fmt.Sprintf("http://127.0.0.1:%d/rules/any-rule", srv.Port()),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	var errBody map[string]string
	json.Unmarshal(body, &errBody)
	if errBody["error"] != "confirmation_required" {
		t.Errorf("expected error 'confirmation_required', got %q", errBody["error"])
	}
}

func TestServer_DeleteRule_WithConfirm(t *testing.T) {
	srv, shannonDir, cancel := setupRulesServer(t)
	defer cancel()

	rulesDir := filepath.Join(shannonDir, "rules")
	if err := os.MkdirAll(rulesDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rulesDir, "to-delete.md"), []byte("bye"), 0600); err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest(
		http.MethodDelete,
		fmt.Sprintf("http://127.0.0.1:%d/rules/to-delete?confirm=true", srv.Port()),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	if _, err := os.Stat(filepath.Join(rulesDir, "to-delete.md")); !os.IsNotExist(err) {
		t.Errorf("expected file to be deleted, got err=%v", err)
	}
}

func TestServer_DeleteRule_NotFound(t *testing.T) {
	srv, _, cancel := setupRulesServer(t)
	defer cancel()

	req, err := http.NewRequest(
		http.MethodDelete,
		fmt.Sprintf("http://127.0.0.1:%d/rules/ghost?confirm=true", srv.Port()),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
}

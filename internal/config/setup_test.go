package config

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunSetup_OllamaProvider(t *testing.T) {
	ollama := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Write([]byte("Ollama is running"))
		case "/api/tags":
			json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]any{
					{"name": "qwen3:4b", "size": 2500000000, "details": map[string]string{"parameter_size": "4B"}},
					{"name": "llama3.1:8b", "size": 5200000000, "details": map[string]string{"parameter_size": "8B"}},
				},
			})
		default:
			w.WriteHeader(404)
		}
	}))
	defer ollama.Close()

	cfg := &Config{}
	// Simulate: choose "2" (Ollama) → enter endpoint → choose model "1"
	input := strings.NewReader("2\n" + ollama.URL + "\n1\n")
	var output bytes.Buffer

	err := RunSetup(cfg, input, &output)
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	if cfg.Provider != "ollama" {
		t.Errorf("expected provider=ollama, got %q", cfg.Provider)
	}
	if cfg.Ollama.Model != "qwen3:4b" {
		t.Errorf("expected model=qwen3:4b, got %q", cfg.Ollama.Model)
	}
	if cfg.Ollama.Endpoint != ollama.URL {
		t.Errorf("expected endpoint=%s, got %q", ollama.URL, cfg.Ollama.Endpoint)
	}
}

func TestRunSetup_GatewayProvider(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer gw.Close()

	cfg := &Config{}
	// Simulate: choose "1" (Cloud) → enter endpoint → enter API key
	input := strings.NewReader("1\n" + gw.URL + "\ntest-key\n")
	var output bytes.Buffer

	err := RunSetup(cfg, input, &output)
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	if cfg.Provider != "" && cfg.Provider != "gateway" {
		t.Errorf("expected provider=gateway or empty, got %q", cfg.Provider)
	}
	if cfg.APIKey != "test-key" {
		t.Errorf("expected api_key=test-key, got %q", cfg.APIKey)
	}
}

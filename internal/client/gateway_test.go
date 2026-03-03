package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCompleteUsesCompletionsEndpoint(t *testing.T) {
	got := struct {
		Messages []Message `json:"messages"`
		Tools    []Tool    `json:"tools"`
	}{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CompletionResponse{
			OutputText:   "hello",
			FinishReason: "end_turn",
			Usage: Usage{
				InputTokens:  3,
				OutputTokens: 4,
				TotalTokens:  7,
			},
		})
	}))
	defer server.Close()

	gw := NewGatewayClient(server.URL, "key")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := gw.Complete(ctx, CompletionRequest{
		Messages: []Message{{Role: "user", Content: NewTextContent("ping")}},
		Tools:    []Tool{{Type: "function"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.OutputText != "hello" {
		t.Fatalf("expected output hello, got %s", resp.OutputText)
	}
	if len(got.Messages) != 1 || got.Messages[0].Content.Text() != "ping" {
		t.Errorf("request body messages not preserved")
	}
	if len(got.Tools) != 1 || got.Tools[0].Type != "function" {
		t.Errorf("expected tool payload to include tools")
	}
}

func TestListTools(t *testing.T) {
	tools := []ServerToolSchema{
		{Name: "web_search", Description: "Search the web", Parameters: map[string]any{"type": "object"}},
		{Name: "getStockBars", Description: "Get stock bars"},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tools" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.Header.Get("X-API-Key") != "test-key" {
			t.Errorf("expected X-API-Key=test-key, got %s", r.Header.Get("X-API-Key"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(tools)
	}))
	defer server.Close()

	gw := NewGatewayClient(server.URL, "test-key")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	got, err := gw.ListTools(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(got))
	}
	if got[0].Name != "web_search" {
		t.Errorf("expected web_search, got %s", got[0].Name)
	}
	if got[1].Name != "getStockBars" {
		t.Errorf("expected getStockBars, got %s", got[1].Name)
	}
}

func TestListTools_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	gw := NewGatewayClient(server.URL, "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := gw.ListTools(ctx)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestExecuteTool(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tools/web_search/execute" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		var req ToolExecuteRequest
		json.NewDecoder(r.Body).Decode(&req)
		if req.Arguments["query"] != "golang testing" {
			t.Errorf("expected query=golang testing, got %v", req.Arguments["query"])
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ToolExecuteResponse{
			Success: true,
			Output:  json.RawMessage(`{"results":["found 10 results"]}`),
		})
	}))
	defer server.Close()

	gw := NewGatewayClient(server.URL, "key")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := gw.ExecuteTool(ctx, "web_search", map[string]any{"query": "golang testing"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Success {
		t.Error("expected success=true")
	}
	if string(resp.Output) != `{"results":["found 10 results"]}` {
		t.Errorf("unexpected output: %s", string(resp.Output))
	}
}

func TestExecuteTool_UrlEscapesName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// r.URL.RawPath preserves the percent-encoding; r.URL.Path is decoded
		want := "/api/v1/tools/my%2Ftool/execute"
		if r.URL.RawPath != want {
			t.Errorf("expected raw path %s, got %s", want, r.URL.RawPath)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ToolExecuteResponse{Success: true, Output: json.RawMessage(`"ok"`)})
	}))
	defer server.Close()

	gw := NewGatewayClient(server.URL, "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := gw.ExecuteTool(ctx, "my/tool", map[string]any{}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExecuteTool_403(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("tool not allowed"))
	}))
	defer server.Close()

	gw := NewGatewayClient(server.URL, "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := gw.ExecuteTool(ctx, "dangerous_tool", map[string]any{}, "")
	if err == nil {
		t.Fatal("expected error for 403")
	}
}

func TestMessageContent_MarshalString(t *testing.T) {
	msg := Message{Role: "user", Content: NewTextContent("hello")}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	var raw map[string]json.RawMessage
	json.Unmarshal(data, &raw)
	var content string
	if err := json.Unmarshal(raw["content"], &content); err != nil {
		t.Fatalf("content should be a string, got: %s", string(raw["content"]))
	}
	if content != "hello" {
		t.Errorf("expected 'hello', got %q", content)
	}
}

func TestMessageContent_MarshalBlocks(t *testing.T) {
	msg := Message{
		Role: "user",
		Content: NewBlockContent([]ContentBlock{
			{Type: "text", Text: "describe this"},
			{Type: "image", Source: &ImageSource{Type: "base64", MediaType: "image/png", Data: "abc123"}},
		}),
	}
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	var raw map[string]json.RawMessage
	json.Unmarshal(data, &raw)
	var blocks []ContentBlock
	if err := json.Unmarshal(raw["content"], &blocks); err != nil {
		t.Fatalf("content should be an array, got: %s", string(raw["content"]))
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
}

func TestMessageContent_UnmarshalString(t *testing.T) {
	raw := `{"role":"user","content":"hello"}`
	var msg Message
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if msg.Content.Text() != "hello" {
		t.Errorf("expected 'hello', got %q", msg.Content.Text())
	}
}

func TestMessageContent_UnmarshalBlocks(t *testing.T) {
	raw := `{"role":"user","content":[{"type":"text","text":"hi"},{"type":"image","source":{"type":"base64","media_type":"image/png","data":"xyz"}}]}`
	var msg Message
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if !msg.Content.HasBlocks() {
		t.Fatal("expected blocks")
	}
	blocks := msg.Content.Blocks()
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(blocks))
	}
}

func TestContentBlock_ToolUse_MarshalJSON(t *testing.T) {
	block := NewToolUseBlock("toolu_abc123", "bash", json.RawMessage(`{"command":"ls"}`))
	data, err := json.Marshal(block)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	var m map[string]any
	json.Unmarshal(data, &m)
	if m["type"] != "tool_use" {
		t.Errorf("expected type=tool_use, got %v", m["type"])
	}
	if m["id"] != "toolu_abc123" {
		t.Errorf("expected id=toolu_abc123, got %v", m["id"])
	}
	if m["name"] != "bash" {
		t.Errorf("expected name=bash, got %v", m["name"])
	}
}

func TestContentBlock_ToolResult_MarshalJSON_StringContent(t *testing.T) {
	block := NewToolResultBlock("toolu_abc123", "file1.txt\nfile2.txt", false)
	data, err := json.Marshal(block)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	var m map[string]any
	json.Unmarshal(data, &m)
	if m["type"] != "tool_result" {
		t.Errorf("expected type=tool_result, got %v", m["type"])
	}
	if m["tool_use_id"] != "toolu_abc123" {
		t.Errorf("expected tool_use_id=toolu_abc123, got %v", m["tool_use_id"])
	}
	if m["content"] != "file1.txt\nfile2.txt" {
		t.Errorf("unexpected content: %v", m["content"])
	}
	if _, ok := m["is_error"]; ok {
		t.Error("is_error should be omitted when false")
	}
}

func TestContentBlock_ToolResult_MarshalJSON_ArrayContent(t *testing.T) {
	block := NewToolResultBlockWithImages("toolu_xyz", "Screenshot captured", []ContentBlock{
		{Type: "image", Source: &ImageSource{Type: "base64", MediaType: "image/png", Data: "fakedata"}},
	}, false)
	data, err := json.Marshal(block)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	var m map[string]any
	json.Unmarshal(data, &m)
	contentArr, ok := m["content"].([]any)
	if !ok {
		t.Fatalf("expected content to be array, got %T: %v", m["content"], m["content"])
	}
	if len(contentArr) != 2 {
		t.Fatalf("expected 2 content blocks (text+image), got %d", len(contentArr))
	}
}

func TestContentBlock_ToolResult_RoundTrip(t *testing.T) {
	// String content round-trip
	original := NewToolResultBlock("toolu_abc", "result text", true)
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	var decoded ContentBlock
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if decoded.Type != "tool_result" {
		t.Errorf("type mismatch: %s", decoded.Type)
	}
	if decoded.ToolUseID != "toolu_abc" {
		t.Errorf("tool_use_id mismatch: %s", decoded.ToolUseID)
	}
	if !decoded.IsError {
		t.Error("is_error should be true")
	}
	text, ok := decoded.ToolContent.(string)
	if !ok {
		t.Fatalf("expected string content, got %T", decoded.ToolContent)
	}
	if text != "result text" {
		t.Errorf("content mismatch: %s", text)
	}

	// Array content round-trip
	original2 := NewToolResultBlockWithImages("toolu_xyz", "Screenshot", []ContentBlock{
		{Type: "image", Source: &ImageSource{Type: "base64", MediaType: "image/png", Data: "abc"}},
	}, false)
	data2, _ := json.Marshal(original2)
	var decoded2 ContentBlock
	if err := json.Unmarshal(data2, &decoded2); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	blocks, ok := decoded2.ToolContent.([]ContentBlock)
	if !ok {
		t.Fatalf("expected []ContentBlock, got %T", decoded2.ToolContent)
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 nested blocks, got %d", len(blocks))
	}
}

func TestFunctionCall_ID(t *testing.T) {
	raw := `{"id":"toolu_abc","name":"bash","arguments":{"command":"ls"}}`
	var fc FunctionCall
	if err := json.Unmarshal([]byte(raw), &fc); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if fc.ID != "toolu_abc" {
		t.Errorf("expected ID=toolu_abc, got %q", fc.ID)
	}
	if fc.Name != "bash" {
		t.Errorf("expected Name=bash, got %q", fc.Name)
	}
}

func TestToolResultText_Extraction(t *testing.T) {
	// String content
	b1 := NewToolResultBlock("id1", "hello world", false)
	if got := ToolResultText(b1); got != "hello world" {
		t.Errorf("expected 'hello world', got %q", got)
	}
	// Array content
	b2 := NewToolResultBlockWithImages("id2", "screenshot taken", nil, false)
	if got := ToolResultText(b2); got != "screenshot taken" {
		t.Errorf("expected 'screenshot taken', got %q", got)
	}
	// Non-tool_result
	b3 := ContentBlock{Type: "text", Text: "plain"}
	if got := ToolResultText(b3); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

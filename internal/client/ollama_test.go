package client

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// 1. Message format conversion: ShanClaw (Anthropic) → OpenAI
// ---------------------------------------------------------------------------

func TestConvertMessages_SimpleText(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: NewTextContent("hello")},
		{Role: "assistant", Content: NewTextContent("hi there")},
	}
	got := convertMessagesToOpenAI(msgs)
	if len(got) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(got))
	}
	if got[0].Role != "user" || got[0].Content != "hello" {
		t.Errorf("msg[0] mismatch: %+v", got[0])
	}
	if got[1].Role != "assistant" || got[1].Content != "hi there" {
		t.Errorf("msg[1] mismatch: %+v", got[1])
	}
}

func TestConvertMessages_AssistantWithToolUseBlocks(t *testing.T) {
	// ShanClaw stores assistant tool calls as ContentBlocks with type "tool_use"
	blocks := []ContentBlock{
		{Type: "text", Text: "Let me check."},
		NewToolUseBlock("call_abc", "bash", json.RawMessage(`{"command":"ls"}`)),
	}
	msgs := []Message{
		{Role: "assistant", Content: NewBlockContent(blocks)},
	}
	got := convertMessagesToOpenAI(msgs)
	if len(got) != 1 {
		t.Fatalf("expected 1 message, got %d", len(got))
	}
	m := got[0]
	if m.Role != "assistant" {
		t.Errorf("expected role=assistant, got %q", m.Role)
	}
	if m.Content != "Let me check." {
		t.Errorf("expected text content, got %q", m.Content)
	}
	if len(m.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(m.ToolCalls))
	}
	tc := m.ToolCalls[0]
	if tc.ID != "call_abc" {
		t.Errorf("tool call ID: got %q, want %q", tc.ID, "call_abc")
	}
	if tc.Function.Name != "bash" {
		t.Errorf("tool call name: got %q, want %q", tc.Function.Name, "bash")
	}
	if tc.Function.Arguments != `{"command":"ls"}` {
		t.Errorf("tool call args: got %q", tc.Function.Arguments)
	}
}

func TestConvertMessages_ToolResultToToolRole(t *testing.T) {
	// ShanClaw stores tool results as user message with tool_result blocks
	blocks := []ContentBlock{
		NewToolResultBlock("call_abc", "file1.txt\nfile2.go", false),
	}
	msgs := []Message{
		{Role: "user", Content: NewBlockContent(blocks)},
	}
	got := convertMessagesToOpenAI(msgs)
	if len(got) != 1 {
		t.Fatalf("expected 1 message, got %d", len(got))
	}
	m := got[0]
	if m.Role != "tool" {
		t.Errorf("expected role=tool, got %q", m.Role)
	}
	if m.ToolCallID != "call_abc" {
		t.Errorf("tool_call_id: got %q, want %q", m.ToolCallID, "call_abc")
	}
	if m.Content != "file1.txt\nfile2.go" {
		t.Errorf("content mismatch: %q", m.Content)
	}
}

func TestConvertMessages_MultipleToolResults(t *testing.T) {
	// Multiple tool results in one user message → multiple role:tool messages
	blocks := []ContentBlock{
		NewToolResultBlock("call_1", "result one", false),
		NewToolResultBlock("call_2", "result two", true),
	}
	msgs := []Message{
		{Role: "user", Content: NewBlockContent(blocks)},
	}
	got := convertMessagesToOpenAI(msgs)
	if len(got) != 2 {
		t.Fatalf("expected 2 messages (one per tool result), got %d", len(got))
	}
	if got[0].Role != "tool" || got[0].ToolCallID != "call_1" {
		t.Errorf("msg[0] mismatch: %+v", got[0])
	}
	if got[1].Role != "tool" || got[1].ToolCallID != "call_2" {
		t.Errorf("msg[1] mismatch: %+v", got[1])
	}
}

func TestConvertMessages_MultipleToolCallsInOneAssistant(t *testing.T) {
	blocks := []ContentBlock{
		NewToolUseBlock("call_a", "glob", json.RawMessage(`{"pattern":"*.go"}`)),
		NewToolUseBlock("call_b", "grep", json.RawMessage(`{"pattern":"func"}`)),
	}
	msgs := []Message{
		{Role: "assistant", Content: NewBlockContent(blocks)},
	}
	got := convertMessagesToOpenAI(msgs)
	if len(got) != 1 {
		t.Fatalf("expected 1 assistant message, got %d", len(got))
	}
	if len(got[0].ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(got[0].ToolCalls))
	}
	if got[0].ToolCalls[0].Function.Name != "glob" {
		t.Errorf("first tool call name: %q", got[0].ToolCalls[0].Function.Name)
	}
	if got[0].ToolCalls[1].Function.Name != "grep" {
		t.Errorf("second tool call name: %q", got[0].ToolCalls[1].Function.Name)
	}
}

func TestConvertMessages_SkipsSystemRole(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: NewTextContent("You are helpful.")},
		{Role: "user", Content: NewTextContent("hi")},
	}
	got := convertMessagesToOpenAI(msgs)
	// system messages should be converted to system role or filtered — depends on design
	// At minimum, user message must survive
	hasUser := false
	for _, m := range got {
		if m.Role == "user" {
			hasUser = true
		}
	}
	if !hasUser {
		t.Error("user message missing from converted output")
	}
}

func TestConvertMessages_XMLFallbackTextPassthrough(t *testing.T) {
	// When tool results are stored as XML text (non-native path), they're
	// plain user text messages. These should pass through as-is.
	xml := `<tool_exec tool="bash" call_id="a1b2c3">
<input>{"command":"ls"}</input>
<output status="ok">file1.txt</output>
</tool_exec>`
	msgs := []Message{
		{Role: "user", Content: NewTextContent(xml)},
	}
	got := convertMessagesToOpenAI(msgs)
	if len(got) != 1 {
		t.Fatalf("expected 1 message, got %d", len(got))
	}
	if got[0].Role != "user" {
		t.Errorf("expected role=user, got %q", got[0].Role)
	}
	if got[0].Content != xml {
		t.Errorf("XML text should pass through unchanged")
	}
}

// ---------------------------------------------------------------------------
// 2. Response conversion: OpenAI → ShanClaw
// ---------------------------------------------------------------------------

func TestConvertResponse_SimpleText(t *testing.T) {
	raw := `{
		"id": "chatcmpl-123",
		"model": "qwen3",
		"choices": [{
			"index": 0,
			"message": {"role": "assistant", "content": "Hello!"},
			"finish_reason": "stop"
		}],
		"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
	}`
	resp, err := convertOpenAIResponse([]byte(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.OutputText != "Hello!" {
		t.Errorf("OutputText: got %q, want %q", resp.OutputText, "Hello!")
	}
	if resp.Model != "qwen3" {
		t.Errorf("Model: got %q", resp.Model)
	}
	if resp.Provider != "ollama" {
		t.Errorf("Provider: got %q, want %q", resp.Provider, "ollama")
	}
	if resp.FinishReason != "end_turn" {
		t.Errorf("FinishReason: got %q, want %q (mapped from stop)", resp.FinishReason, "end_turn")
	}
	if resp.Usage.InputTokens != 10 {
		t.Errorf("InputTokens: got %d", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 5 {
		t.Errorf("OutputTokens: got %d", resp.Usage.OutputTokens)
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("TotalTokens: got %d", resp.Usage.TotalTokens)
	}
}

func TestConvertResponse_WithToolCalls(t *testing.T) {
	raw := `{
		"id": "chatcmpl-456",
		"model": "llama3.1",
		"choices": [{
			"index": 0,
			"message": {
				"role": "assistant",
				"content": "",
				"tool_calls": [
					{
						"id": "call_xyz",
						"type": "function",
						"function": {
							"name": "bash",
							"arguments": "{\"command\":\"pwd\"}"
						}
					}
				]
			},
			"finish_reason": "tool_calls"
		}],
		"usage": {"prompt_tokens": 20, "completion_tokens": 10, "total_tokens": 30}
	}`
	resp, err := convertOpenAIResponse([]byte(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_xyz" {
		t.Errorf("ID: got %q", tc.ID)
	}
	if tc.Name != "bash" {
		t.Errorf("Name: got %q", tc.Name)
	}
	if string(tc.Arguments) != `{"command":"pwd"}` {
		t.Errorf("Arguments: got %s", string(tc.Arguments))
	}
	// FinishReason should map tool_calls → tool_use
	if resp.FinishReason != "tool_use" {
		t.Errorf("FinishReason: got %q, want %q", resp.FinishReason, "tool_use")
	}
}

func TestConvertResponse_MultipleToolCalls(t *testing.T) {
	raw := `{
		"model": "qwen3",
		"choices": [{
			"message": {
				"role": "assistant",
				"content": "Checking both.",
				"tool_calls": [
					{"id": "c1", "type": "function", "function": {"name": "glob", "arguments": "{\"pattern\":\"*.go\"}"}},
					{"id": "c2", "type": "function", "function": {"name": "grep", "arguments": "{\"pattern\":\"main\"}"}}
				]
			},
			"finish_reason": "tool_calls"
		}],
		"usage": {"prompt_tokens": 5, "completion_tokens": 5, "total_tokens": 10}
	}`
	resp, err := convertOpenAIResponse([]byte(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.OutputText != "Checking both." {
		t.Errorf("OutputText should preserve text alongside tool calls: %q", resp.OutputText)
	}
	if len(resp.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].Name != "glob" || resp.ToolCalls[1].Name != "grep" {
		t.Errorf("tool call names: %q, %q", resp.ToolCalls[0].Name, resp.ToolCalls[1].Name)
	}
}

func TestConvertResponse_EmptyChoices(t *testing.T) {
	raw := `{"model": "qwen3", "choices": [], "usage": {}}`
	_, err := convertOpenAIResponse([]byte(raw))
	if err == nil {
		t.Fatal("expected error for empty choices")
	}
}

func TestConvertResponse_NullContent(t *testing.T) {
	// Some models return null content when only tool calls are present
	raw := `{
		"model": "llama3.2",
		"choices": [{
			"message": {
				"role": "assistant",
				"content": null,
				"tool_calls": [{"id": "c1", "type": "function", "function": {"name": "bash", "arguments": "{}"}}]
			},
			"finish_reason": "tool_calls"
		}],
		"usage": {"prompt_tokens": 5, "completion_tokens": 5, "total_tokens": 10}
	}`
	resp, err := convertOpenAIResponse([]byte(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.OutputText != "" {
		t.Errorf("expected empty OutputText for null content, got %q", resp.OutputText)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
}

func TestConvertResponse_ReasoningFieldFallback(t *testing.T) {
	// When content is empty but reasoning is present (Qwen3 thinking mode
	// truncated by max_tokens), reasoning should surface as OutputText
	// so the user sees something rather than empty output.
	raw := `{
		"model": "qwen3:4b",
		"choices": [{
			"message": {
				"role": "assistant",
				"content": "",
				"reasoning": "Let me think about quantum computing step by step..."
			},
			"finish_reason": "length"
		}],
		"usage": {"prompt_tokens": 10, "completion_tokens": 100, "total_tokens": 110}
	}`
	resp, err := convertOpenAIResponse([]byte(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.OutputText == "" {
		t.Error("expected non-empty OutputText when reasoning is present but content is empty")
	}
	if !strings.Contains(resp.OutputText, "think") {
		t.Errorf("OutputText should contain reasoning content, got %q", resp.OutputText)
	}
}

func TestConvertResponse_ReasoningNotUsedWhenContentPresent(t *testing.T) {
	// When both content and reasoning are present, content wins — don't
	// pollute the output with thinking text.
	raw := `{
		"model": "qwen3:4b",
		"choices": [{
			"message": {
				"role": "assistant",
				"content": "The answer is 42.",
				"reasoning": "Long internal thinking process..."
			},
			"finish_reason": "stop"
		}],
		"usage": {"prompt_tokens": 10, "completion_tokens": 50, "total_tokens": 60}
	}`
	resp, err := convertOpenAIResponse([]byte(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.OutputText != "The answer is 42." {
		t.Errorf("expected content to win over reasoning, got %q", resp.OutputText)
	}
}

// ---------------------------------------------------------------------------
// 3. FinishReason mapping
// ---------------------------------------------------------------------------

func TestMapFinishReason(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"stop", "end_turn"},
		{"length", "max_tokens"},
		{"tool_calls", "tool_use"},
		{"content_filter", "content_filter"},
		{"", "end_turn"},         // empty → safe default
		{"unknown", "end_turn"},  // unknown → safe default
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%q→%q", tc.input, tc.want), func(t *testing.T) {
			got := mapFinishReason(tc.input)
			if got != tc.want {
				t.Errorf("mapFinishReason(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 4. Tool schema filtering (NativeToolDef must be excluded)
// ---------------------------------------------------------------------------

func TestFilterToolsForOpenAI_StandardFunctionPassthrough(t *testing.T) {
	tools := []Tool{
		{Type: "function", Function: FunctionDef{Name: "bash", Description: "Run shell commands"}},
		{Type: "function", Function: FunctionDef{Name: "file_read", Description: "Read files"}},
	}
	got := filterToolsForOpenAI(tools)
	if len(got) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(got))
	}
	if got[0].Function.Name != "bash" || got[1].Function.Name != "file_read" {
		t.Errorf("tool names mismatch")
	}
}

func TestFilterToolsForOpenAI_ExcludesNativeTools(t *testing.T) {
	tools := []Tool{
		{Type: "function", Function: FunctionDef{Name: "bash", Description: "Run shell"}},
		{Type: "computer_20251124", Name: "computer", DisplayWidthPx: 1280, DisplayHeightPx: 800},
		{Type: "function", Function: FunctionDef{Name: "file_read", Description: "Read"}},
	}
	got := filterToolsForOpenAI(tools)
	if len(got) != 2 {
		t.Fatalf("expected 2 tools (native filtered), got %d", len(got))
	}
	for _, tool := range got {
		if tool.Type != "function" {
			t.Errorf("non-function tool leaked through: type=%q", tool.Type)
		}
	}
}

func TestFilterToolsForOpenAI_EmptyList(t *testing.T) {
	got := filterToolsForOpenAI(nil)
	if got == nil {
		// nil is acceptable
	} else if len(got) != 0 {
		t.Errorf("expected empty, got %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// 5. OllamaClient.Complete() — HTTP integration with mock server
// ---------------------------------------------------------------------------

func TestOllamaClient_Complete_CorrectEndpointAndFormat(t *testing.T) {
	var gotPath string
	var gotBody map[string]json.RawMessage

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"model": "llama3.1",
			"choices": [{
				"message": {"role": "assistant", "content": "Hi!"},
				"finish_reason": "stop"
			}],
			"usage": {"prompt_tokens": 5, "completion_tokens": 3, "total_tokens": 8}
		}`)
	}))
	defer server.Close()

	oc := NewOllamaClient(server.URL, "llama3.1")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := oc.Complete(ctx, CompletionRequest{
		Messages:    []Message{{Role: "user", Content: NewTextContent("hello")}},
		Temperature: 0.7,
		MaxTokens:   100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify endpoint
	if gotPath != "/v1/chat/completions" {
		t.Errorf("expected path /v1/chat/completions, got %q", gotPath)
	}

	// Verify model is set in request body
	var model string
	json.Unmarshal(gotBody["model"], &model)
	if model != "llama3.1" {
		t.Errorf("expected model=llama3.1 in body, got %q", model)
	}

	// Verify response mapping
	if resp.OutputText != "Hi!" {
		t.Errorf("OutputText: got %q", resp.OutputText)
	}
	if resp.FinishReason != "end_turn" {
		t.Errorf("FinishReason: got %q", resp.FinishReason)
	}
}

func TestOllamaClient_Complete_WithTools(t *testing.T) {
	var gotBody map[string]json.RawMessage

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"model": "qwen3",
			"choices": [{
				"message": {
					"role": "assistant",
					"content": "",
					"tool_calls": [{
						"id": "call_001",
						"type": "function",
						"function": {"name": "bash", "arguments": "{\"command\":\"ls\"}"}
					}]
				},
				"finish_reason": "tool_calls"
			}],
			"usage": {"prompt_tokens": 10, "completion_tokens": 8, "total_tokens": 18}
		}`)
	}))
	defer server.Close()

	oc := NewOllamaClient(server.URL, "qwen3")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := oc.Complete(ctx, CompletionRequest{
		Messages: []Message{{Role: "user", Content: NewTextContent("list files")}},
		Tools: []Tool{
			{Type: "function", Function: FunctionDef{Name: "bash", Description: "Run commands"}},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify tools were sent in OpenAI format
	var tools []json.RawMessage
	json.Unmarshal(gotBody["tools"], &tools)
	if len(tools) == 0 {
		t.Error("tools not included in request body")
	}

	// Verify tool call response
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	if resp.ToolCalls[0].ID != "call_001" {
		t.Errorf("tool call ID: %q", resp.ToolCalls[0].ID)
	}
	if resp.ToolCalls[0].Name != "bash" {
		t.Errorf("tool call name: %q", resp.ToolCalls[0].Name)
	}
}

func TestOllamaClient_Complete_SkipsThinkingAndReasoningEffort(t *testing.T) {
	var gotBody map[string]json.RawMessage

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"model":"m","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{}}`)
	}))
	defer server.Close()

	oc := NewOllamaClient(server.URL, "m")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	oc.Complete(ctx, CompletionRequest{
		Messages: []Message{{Role: "user", Content: NewTextContent("test")}},
		Thinking: &ThinkingConfig{Type: "adaptive", BudgetTokens: 5000},
		ReasoningEffort: "high",
	})

	// Anthropic-specific fields must NOT appear in the request
	if _, ok := gotBody["thinking"]; ok {
		t.Error("thinking field should not be sent to Ollama")
	}
	if _, ok := gotBody["reasoning_effort"]; ok {
		t.Error("reasoning_effort field should not be sent to Ollama")
	}
}

func TestOllamaClient_Complete_UsesSpecificModelOverride(t *testing.T) {
	var gotBody map[string]json.RawMessage

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"model":"qwen3","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{}}`)
	}))
	defer server.Close()

	// Default model is llama3.1, but SpecificModel overrides it
	oc := NewOllamaClient(server.URL, "llama3.1")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	oc.Complete(ctx, CompletionRequest{
		Messages:      []Message{{Role: "user", Content: NewTextContent("test")}},
		SpecificModel: "qwen3",
	})

	var model string
	json.Unmarshal(gotBody["model"], &model)
	if model != "qwen3" {
		t.Errorf("SpecificModel should override default: got %q, want %q", model, "qwen3")
	}
}

func TestOllamaClient_Complete_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("model not found"))
	}))
	defer server.Close()

	oc := NewOllamaClient(server.URL, "nonexistent")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := oc.Complete(ctx, CompletionRequest{
		Messages: []Message{{Role: "user", Content: NewTextContent("hi")}},
	})
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	// Should be an APIError with status code
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T: %v", err, err)
	}
	if apiErr.StatusCode != 500 {
		t.Errorf("expected status 500, got %d", apiErr.StatusCode)
	}
}

func TestOllamaClient_Complete_ConnectionRefused(t *testing.T) {
	// Use a port that's definitely not listening
	oc := NewOllamaClient("http://127.0.0.1:1", "llama3.1")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := oc.Complete(ctx, CompletionRequest{
		Messages: []Message{{Role: "user", Content: NewTextContent("hi")}},
	})
	if err == nil {
		t.Fatal("expected error for connection refused")
	}
}

// ---------------------------------------------------------------------------
// 6. OllamaClient.CompleteStream() — streaming text output
// ---------------------------------------------------------------------------

func TestOllamaClient_CompleteStream_TextDeltas(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("server doesn't support flushing")
		}

		chunks := []string{
			`{"id":"1","object":"chat.completion.chunk","choices":[{"delta":{"role":"assistant","content":"Hello"},"finish_reason":null}]}`,
			`{"id":"1","object":"chat.completion.chunk","choices":[{"delta":{"content":" world"},"finish_reason":null}]}`,
			`{"id":"1","object":"chat.completion.chunk","choices":[{"delta":{},"finish_reason":"stop"}],"model":"llama3.1","usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`,
		}
		for _, chunk := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", chunk)
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	defer server.Close()

	oc := NewOllamaClient(server.URL, "llama3.1")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var deltas []string
	resp, err := oc.CompleteStream(ctx, CompletionRequest{
		Messages: []Message{{Role: "user", Content: NewTextContent("hi")}},
	}, func(d StreamDelta) {
		deltas = append(deltas, d.Text)
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify deltas received
	if len(deltas) < 2 {
		t.Fatalf("expected at least 2 deltas, got %d: %v", len(deltas), deltas)
	}
	joined := strings.Join(deltas, "")
	if joined != "Hello world" {
		t.Errorf("concatenated deltas: got %q, want %q", joined, "Hello world")
	}

	// Verify final response
	if resp.OutputText != "Hello world" {
		t.Errorf("OutputText: got %q, want %q", resp.OutputText, "Hello world")
	}
	if resp.FinishReason != "end_turn" {
		t.Errorf("FinishReason: got %q", resp.FinishReason)
	}
}

func TestOllamaClient_CompleteStream_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("overloaded"))
	}))
	defer server.Close()

	oc := NewOllamaClient(server.URL, "llama3.1")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := oc.CompleteStream(ctx, CompletionRequest{
		Messages: []Message{{Role: "user", Content: NewTextContent("hi")}},
	}, nil)
	if err == nil {
		t.Fatal("expected error for 503")
	}
}

// ---------------------------------------------------------------------------
// 7. Full conversation round-trip (interleaved tool calls)
// ---------------------------------------------------------------------------

func TestConvertMessages_FullConversationRoundTrip(t *testing.T) {
	// Simulate a full ShanClaw conversation with tool calls:
	// 1. user asks a question
	// 2. assistant calls a tool
	// 3. user provides tool result
	// 4. assistant responds with final text
	msgs := []Message{
		{Role: "user", Content: NewTextContent("What files are in this dir?")},
		{Role: "assistant", Content: NewBlockContent([]ContentBlock{
			{Type: "text", Text: "Let me check."},
			NewToolUseBlock("call_001", "bash", json.RawMessage(`{"command":"ls"}`)),
		})},
		{Role: "user", Content: NewBlockContent([]ContentBlock{
			NewToolResultBlock("call_001", "main.go\ngo.mod\nREADME.md", false),
		})},
		{Role: "assistant", Content: NewTextContent("There are 3 files: main.go, go.mod, and README.md.")},
	}

	got := convertMessagesToOpenAI(msgs)

	// Expected: user, assistant(text+tool_calls), tool, assistant
	if len(got) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(got))
	}

	// msg 0: user text
	if got[0].Role != "user" || got[0].Content != "What files are in this dir?" {
		t.Errorf("msg[0]: %+v", got[0])
	}

	// msg 1: assistant with text + tool call
	if got[1].Role != "assistant" {
		t.Errorf("msg[1] role: %q", got[1].Role)
	}
	if got[1].Content != "Let me check." {
		t.Errorf("msg[1] content: %q", got[1].Content)
	}
	if len(got[1].ToolCalls) != 1 || got[1].ToolCalls[0].ID != "call_001" {
		t.Errorf("msg[1] tool_calls: %+v", got[1].ToolCalls)
	}

	// msg 2: tool result
	if got[2].Role != "tool" || got[2].ToolCallID != "call_001" {
		t.Errorf("msg[2]: %+v", got[2])
	}

	// msg 3: final assistant text
	if got[3].Role != "assistant" || got[3].Content != "There are 3 files: main.go, go.mod, and README.md." {
		t.Errorf("msg[3]: %+v", got[3])
	}
}

// ---------------------------------------------------------------------------
// 8. Edge cases
// ---------------------------------------------------------------------------

func TestConvertMessages_EmptySlice(t *testing.T) {
	got := convertMessagesToOpenAI(nil)
	if len(got) != 0 {
		t.Errorf("expected empty output, got %d", len(got))
	}
}

func TestConvertMessages_ToolUseWithEmptyArguments(t *testing.T) {
	blocks := []ContentBlock{
		NewToolUseBlock("call_x", "think", json.RawMessage(`{}`)),
	}
	msgs := []Message{
		{Role: "assistant", Content: NewBlockContent(blocks)},
	}
	got := convertMessagesToOpenAI(msgs)
	if len(got) != 1 {
		t.Fatalf("expected 1, got %d", len(got))
	}
	if got[0].ToolCalls[0].Function.Arguments != "{}" {
		t.Errorf("empty args should stay as {}: got %q", got[0].ToolCalls[0].Function.Arguments)
	}
}

func TestConvertResponse_ArgumentsAsObject(t *testing.T) {
	// Ollama native API returns arguments as JSON object, but the OpenAI-compat
	// layer should return them as string. Test both cases for robustness.
	raw := `{
		"model": "qwen3",
		"choices": [{
			"message": {
				"role": "assistant",
				"tool_calls": [{
					"id": "c1",
					"type": "function",
					"function": {"name": "bash", "arguments": {"command": "ls"}}
				}]
			},
			"finish_reason": "tool_calls"
		}],
		"usage": {}
	}`
	resp, err := convertOpenAIResponse([]byte(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(resp.ToolCalls))
	}
	// Arguments should be valid JSON regardless of input format
	var args map[string]any
	if err := json.Unmarshal(resp.ToolCalls[0].Arguments, &args); err != nil {
		t.Errorf("arguments should be valid JSON: %v (raw: %s)", err, string(resp.ToolCalls[0].Arguments))
	}
}

func TestConvertResponse_CostAndCacheFieldsDefaultZero(t *testing.T) {
	raw := `{
		"model": "llama3.1",
		"choices": [{
			"message": {"role": "assistant", "content": "ok"},
			"finish_reason": "stop"
		}],
		"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
	}`
	resp, err := convertOpenAIResponse([]byte(raw))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Usage.CostUSD != 0 {
		t.Errorf("CostUSD should be 0 for local models, got %f", resp.Usage.CostUSD)
	}
	if resp.Usage.CacheReadTokens != 0 {
		t.Errorf("CacheReadTokens should be 0, got %d", resp.Usage.CacheReadTokens)
	}
	if resp.Usage.CacheCreationTokens != 0 {
		t.Errorf("CacheCreationTokens should be 0, got %d", resp.Usage.CacheCreationTokens)
	}
}

func TestOllamaClient_Complete_NativeToolsFiltered(t *testing.T) {
	var gotBody map[string]json.RawMessage

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"model":"m","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{}}`)
	}))
	defer server.Close()

	oc := NewOllamaClient(server.URL, "m")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	oc.Complete(ctx, CompletionRequest{
		Messages: []Message{{Role: "user", Content: NewTextContent("test")}},
		Tools: []Tool{
			{Type: "function", Function: FunctionDef{Name: "bash", Description: "shell"}},
			{Type: "computer_20251124", Name: "computer", DisplayWidthPx: 1280, DisplayHeightPx: 800},
		},
	})

	// Only function tools should be sent, native tools filtered
	var tools []map[string]any
	json.Unmarshal(gotBody["tools"], &tools)
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool (native filtered), got %d", len(tools))
	}
}

func TestOllamaClient_Complete_NoToolsSendsNoToolsField(t *testing.T) {
	var gotBody map[string]json.RawMessage

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"model":"m","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{}}`)
	}))
	defer server.Close()

	oc := NewOllamaClient(server.URL, "m")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	oc.Complete(ctx, CompletionRequest{
		Messages: []Message{{Role: "user", Content: NewTextContent("test")}},
		// No tools
	})

	// tools field should be absent or null to avoid confusing Ollama
	if raw, ok := gotBody["tools"]; ok {
		var tools []any
		json.Unmarshal(raw, &tools)
		if len(tools) > 0 {
			t.Error("tools should not be sent when empty")
		}
	}
}

// ---------------------------------------------------------------------------
// 9. OllamaClient.ListModels() and CheckHealth()
// ---------------------------------------------------------------------------

func TestOllamaClient_ListModels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			t.Errorf("expected /api/tags, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"models":[
			{"name":"qwen3:4b","size":2500000000,"details":{"parameter_size":"4B"}},
			{"name":"llama3.1:8b","size":5200000000,"details":{"parameter_size":"8B"}}
		]}`)
	}))
	defer server.Close()

	oc := NewOllamaClient(server.URL, "qwen3:4b")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	models, err := oc.ListModels(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
	if models[0].Name != "qwen3:4b" {
		t.Errorf("first model name: %q", models[0].Name)
	}
	if models[1].Name != "llama3.1:8b" {
		t.Errorf("second model name: %q", models[1].Name)
	}
	if models[0].Size != 2500000000 {
		t.Errorf("first model size: %d", models[0].Size)
	}
}

func TestOllamaClient_ListModels_ConnectionRefused(t *testing.T) {
	oc := NewOllamaClient("http://127.0.0.1:1", "m")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := oc.ListModels(ctx)
	if err == nil {
		t.Fatal("expected error for connection refused")
	}
}

func TestOllamaClient_CheckHealth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			t.Errorf("expected /, got %s", r.URL.Path)
		}
		fmt.Fprint(w, "Ollama is running")
	}))
	defer server.Close()

	oc := NewOllamaClient(server.URL, "m")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := oc.CheckHealth(ctx); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOllamaClient_CheckHealth_NotRunning(t *testing.T) {
	oc := NewOllamaClient("http://127.0.0.1:1", "m")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := oc.CheckHealth(ctx); err == nil {
		t.Fatal("expected error when Ollama is not running")
	}
}

// ---------------------------------------------------------------------------
// 10. LLMClient interface satisfaction
// ---------------------------------------------------------------------------

func TestOllamaClient_ImplementsLLMClient(t *testing.T) {
	// Compile-time check: OllamaClient must satisfy LLMClient interface
	var _ LLMClient = (*OllamaClient)(nil)
}

func TestGatewayClient_ImplementsLLMClient(t *testing.T) {
	// Compile-time check: GatewayClient must also satisfy LLMClient interface
	var _ LLMClient = (*GatewayClient)(nil)
}

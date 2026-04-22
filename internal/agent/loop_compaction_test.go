package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Kocoro-lab/ShanClaw/internal/client"
)

// TestAgentLoop_CompactionAndMemoryPersist verifies the full compaction chain:
//
//  1. Agent loop runs multiple tool-call iterations within a single Run()
//  2. Mock server reports growing input tokens each iteration
//  3. When tokens exceed 85% of context_window → compaction triggers
//  4. PersistLearnings fires (small tier) → writes to MEMORY.md
//  5. GenerateSummary fires (small tier) → creates summary
//  6. ShapeHistory reduces messages
//
// Uses context_window=2000 so 85% threshold = 1700 tokens.
// Needs ≥5 tool iterations so messages > MinShapeable (9).
func TestAgentLoop_CompactionAndMemoryPersist(t *testing.T) {
	memoryDir := t.TempDir()

	var mu sync.Mutex
	var calls []string // ordered log of all calls

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := readBody(r.Body)
		defer r.Body.Close()

		var req struct {
			ModelTier string `json:"model_tier"`
			Messages  []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		json.Unmarshal(raw, &req)

		mu.Lock()
		callNum := len(calls) + 1

		// Identify small-tier calls
		if req.ModelTier == "small" {
			isPersist := false
			isSummary := false
			for _, m := range req.Messages {
				var text string
				json.Unmarshal(m.Content, &text)
				if strings.Contains(text, "extracting durable knowledge") {
					isPersist = true
				}
				if strings.Contains(text, "Compress the following conversation") {
					isSummary = true
				}
			}

			if isPersist {
				calls = append(calls, fmt.Sprintf("call %d: PERSIST", callNum))
				mu.Unlock()
				t.Logf("Call %d: [small] PersistLearnings (messages: %d)", callNum, len(req.Messages))
				json.NewEncoder(w).Encode(nativeResponse(
					"- Agent discussed system architecture\n- Testing compaction flow",
					"end_turn", nil, 50, 30))
				return
			}
			if isSummary {
				calls = append(calls, fmt.Sprintf("call %d: SUMMARY", callNum))
				mu.Unlock()
				t.Logf("Call %d: [small] GenerateSummary", callNum)
				json.NewEncoder(w).Encode(nativeResponse(
					"User asked about architecture. Agent reasoned through multiple steps.",
					"end_turn", nil, 50, 30))
				return
			}

			calls = append(calls, fmt.Sprintf("call %d: small-other", callNum))
			mu.Unlock()
			t.Logf("Call %d: [small] other", callNum)
			json.NewEncoder(w).Encode(nativeResponse("ok", "end_turn", nil, 50, 30))
			return
		}

		// Main-tier calls: use message count to decide behavior.
		// We need the loop to iterate 6+ times so messages exceed MinShapeable (9).
		// Report input tokens that grow to exceed the 1700 threshold.
		msgCount := len(req.Messages)
		// Scale input tokens based on message count to simulate realistic growth
		inputTokens := msgCount * 200

		if msgCount < 12 {
			// Keep looping with tool calls until we have enough messages
			calls = append(calls, fmt.Sprintf("call %d: TOOL (msgs=%d, input=%d)", callNum, msgCount, inputTokens))
			mu.Unlock()
			t.Logf("Call %d: [main] tool_use (msgs=%d, input_tokens=%d)", callNum, msgCount, inputTokens)
			json.NewEncoder(w).Encode(nativeResponse(
				"", "tool_use",
				toolCall("think", fmt.Sprintf(`{"thought":"Analyzing step with %d messages in context"}`, msgCount)),
				inputTokens, 100))
		} else {
			calls = append(calls, fmt.Sprintf("call %d: END_TURN (msgs=%d, input=%d)", callNum, msgCount, inputTokens))
			mu.Unlock()
			t.Logf("Call %d: [main] end_turn (msgs=%d, input_tokens=%d)", callNum, msgCount, inputTokens)
			json.NewEncoder(w).Encode(nativeResponse(
				"Here is the complete analysis based on my reasoning through all the steps.",
				"end_turn", nil, inputTokens, 100))
		}
	}))
	defer server.Close()

	gw := client.NewGatewayClient(server.URL, "")
	reg := NewToolRegistry()

	// Register think tool — no approval needed, keeps loop iterating
	reg.Register(&thinkTool{})

	handler := &mockHandler{approveResult: true}

	loop := NewAgentLoop(gw, reg, "medium", "", 20, 2000, 200, nil, nil, nil)
	loop.SetContextWindow(2000) // 85% = 1700 triggers compaction
	loop.SetMemoryDir(memoryDir)
	loop.SetHandler(handler)

	// Run with a big message
	result, usage, err := loop.Run(context.Background(),
		"Explain the complete system architecture. Think through each component step by step. Be thorough.",
		nil, nil)
	if err != nil {
		t.Logf("Run error (may be iteration limit): %v", err)
	}

	mu.Lock()
	t.Logf("\n=== Call sequence (%d total) ===", len(calls))
	for _, c := range calls {
		t.Logf("  %s", c)
	}

	hasPersist := false
	hasSummary := false
	for _, c := range calls {
		if strings.Contains(c, "PERSIST") {
			hasPersist = true
		}
		if strings.Contains(c, "SUMMARY") {
			hasSummary = true
		}
	}
	mu.Unlock()

	t.Logf("Result: %d chars", len(result))
	t.Logf("Usage: %d LLM calls, %d input+output tokens",
		usage.LLMCalls, usage.InputTokens+usage.OutputTokens)

	// Check compaction fired
	if !hasPersist {
		t.Error("PersistLearnings should have fired during compaction")
	}
	if !hasSummary {
		t.Error("GenerateSummary should have fired during compaction")
	}

	// Check MEMORY.md
	memPath := filepath.Join(memoryDir, "MEMORY.md")
	memData, err := os.ReadFile(memPath)
	if err != nil {
		if hasPersist {
			t.Fatalf("MEMORY.md should exist since PersistLearnings fired: %v", err)
		}
		t.Logf("MEMORY.md not created — compaction didn't trigger")
		return
	}

	memContent := string(memData)
	t.Logf("\n=== MEMORY.md ===\n%s", memContent)

	if !strings.Contains(memContent, "Auto-persisted") {
		t.Error("MEMORY.md should contain Auto-persisted section")
	}
}

// thinkTool is a minimal think tool for the compaction test.
type thinkTool struct{}

func (t *thinkTool) Info() ToolInfo {
	return ToolInfo{
		Name:        "think",
		Description: "Plan or reason through tasks",
		Parameters:  map[string]any{"type": "object", "properties": map[string]any{"thought": map[string]any{"type": "string"}}},
		Required:    []string{"thought"},
	}
}

func (t *thinkTool) Run(ctx context.Context, args string) (ToolResult, error) {
	return ToolResult{Content: "Thought recorded."}, nil
}

func (t *thinkTool) RequiresApproval() bool { return false }

func readBody(body interface{ Read([]byte) (int, error) }) ([]byte, error) {
	var buf []byte
	tmp := make([]byte, 4096)
	for {
		n, err := body.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			break
		}
	}
	return buf, nil
}

func TestTruncateHeadTail(t *testing.T) {
	t.Run("short string unchanged", func(t *testing.T) {
		s := "hello world"
		got := truncateHeadTail(s, 100)
		if got != s {
			t.Errorf("expected unchanged, got %q", got)
		}
	})

	t.Run("exact limit unchanged", func(t *testing.T) {
		s := "abcdefghij" // 10 runes
		got := truncateHeadTail(s, 10)
		if got != s {
			t.Errorf("expected unchanged, got %q", got)
		}
	})

	t.Run("long string gets head+tail", func(t *testing.T) {
		// 100 chars, truncate to 40
		s := strings.Repeat("a", 50) + strings.Repeat("z", 50)
		got := truncateHeadTail(s, 40)
		// keepHead=30, keepTail=10
		if !strings.HasPrefix(got, strings.Repeat("a", 30)) {
			t.Errorf("expected head of 30 'a's, got prefix: %q", got[:40])
		}
		if !strings.HasSuffix(got, strings.Repeat("z", 10)) {
			t.Errorf("expected tail of 10 'z's, got suffix: %q", got[len(got)-20:])
		}
		if !strings.Contains(got, "[... truncated 60 chars ...]") {
			t.Errorf("expected truncation marker with 60 dropped chars, got: %q", got)
		}
	})

	t.Run("rune-safe with multibyte", func(t *testing.T) {
		// 20 runes of 3 bytes each
		s := strings.Repeat("日", 20)
		got := truncateHeadTail(s, 10)
		// keepHead=7, keepTail=2
		runes := []rune(got)
		// Should start with 7 日 and end with 2 日
		if runes[0] != '日' || runes[len(runes)-1] != '日' {
			t.Errorf("expected rune-safe truncation, got: %q", got)
		}
		if !strings.Contains(got, "[... truncated 10 chars ...]") {
			t.Errorf("expected truncation marker, got: %q", got)
		}
	})
}

func TestBuildToolCallMap(t *testing.T) {
	messages := []client.Message{
		{
			Role: "assistant",
			Content: client.NewBlockContent([]client.ContentBlock{
				client.NewToolUseBlock("tu-1", "file_read", json.RawMessage(`{"path":"/tmp/foo.txt"}`)),
				client.NewToolUseBlock("tu-2", "bash", json.RawMessage(`{"command":"echo hello"}`)),
			}),
		},
		{
			Role: "user",
			Content: client.NewBlockContent([]client.ContentBlock{
				client.NewToolResultBlock("tu-1", "file contents here", false),
			}),
		},
	}

	m := buildToolCallMap(messages)
	if len(m) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(m))
	}
	if m["tu-1"].Name != "file_read" {
		t.Errorf("expected file_read, got %q", m["tu-1"].Name)
	}
	if m["tu-2"].Name != "bash" {
		t.Errorf("expected bash, got %q", m["tu-2"].Name)
	}
	if !strings.Contains(m["tu-1"].Args, "/tmp/foo.txt") {
		t.Errorf("expected args to contain path, got %q", m["tu-1"].Args)
	}
}

func TestBuildToolCallMap_LongArgsTruncated(t *testing.T) {
	longArgs := `{"content":"` + strings.Repeat("x", 200) + `"}`
	messages := []client.Message{
		{
			Role: "assistant",
			Content: client.NewBlockContent([]client.ContentBlock{
				client.NewToolUseBlock("tu-1", "file_write", json.RawMessage(longArgs)),
			}),
		},
	}

	m := buildToolCallMap(messages)
	if len(m["tu-1"].Args) > 104 { // 100 + "..."
		t.Errorf("expected args truncated to ~103 chars, got %d", len(m["tu-1"].Args))
	}
}

func TestCompressOldToolResults_TieredBehavior(t *testing.T) {
	// Create 25 tool result pairs to exercise all three tiers with current constants:
	// tier1Threshold=20, keepRecent passed as 8 to match compressAfter.
	const numTools = 25
	const keepRecent = 8

	var messages []client.Message
	messages = append(messages, client.Message{
		Role:    "user",
		Content: client.NewTextContent("Do some work"),
	})

	for i := 0; i < numTools; i++ {
		id := fmt.Sprintf("tu-%d", i)
		name := fmt.Sprintf("tool_%d", i)
		args := json.RawMessage(fmt.Sprintf(`{"arg":"value_%d"}`, i))
		content := fmt.Sprintf("Result content for tool %d: %s", i, strings.Repeat("x", 500))

		messages = append(messages, client.Message{
			Role: "assistant",
			Content: client.NewBlockContent([]client.ContentBlock{
				client.NewToolUseBlock(id, name, args),
			}),
		})
		messages = append(messages, client.Message{
			Role: "user",
			Content: client.NewBlockContent([]client.ContentBlock{
				client.NewToolResultBlock(id, content, false),
			}),
		})
	}

	compressOldToolResults(context.Background(), messages, keepRecent, 300, nil)

	for i := 0; i < numTools; i++ {
		msgIdx := 2 + i*2
		msg := messages[msgIdx]
		blocks := msg.Content.Blocks()
		if len(blocks) == 0 {
			t.Fatalf("tool result %d: no blocks", i)
		}
		resultContent := ""
		for _, b := range blocks {
			if b.Type == "tool_result" {
				if s, ok := b.ToolContent.(string); ok {
					resultContent = s
				}
			}
		}

		distFromEnd := (numTools - 1) - i

		if distFromEnd < keepRecent {
			// Tier 3: should be full (500+ chars)
			if len(resultContent) < 500 {
				t.Errorf("tool %d (dist=%d): expected tier 3 full content (%d chars), got %d chars",
					i, distFromEnd, 500, len(resultContent))
			}
		} else if distFromEnd >= 20 {
			// Tier 1: should contain "snipped"
			if !strings.Contains(resultContent, "snipped") {
				t.Errorf("tool %d (dist=%d): expected tier 1 metadata with 'snipped', got: %q",
					i, distFromEnd, resultContent)
			}
		} else {
			// Tier 2: should be truncated but not snipped (head+tail)
			if strings.Contains(resultContent, "snipped") {
				t.Errorf("tool %d (dist=%d): tier 2 should not contain 'snipped', got: %q",
					i, distFromEnd, resultContent)
			}
			if len(resultContent) > 400 {
				t.Errorf("tool %d (dist=%d): expected tier 2 truncated to ~300 chars, got %d",
					i, distFromEnd, len(resultContent))
			}
			if !strings.Contains(resultContent, "[... truncated") {
				t.Errorf("tool %d (dist=%d): expected head+tail truncation marker, got: %q",
					i, distFromEnd, resultContent)
			}
		}
	}
}

func TestCompressOldToolResults_Tier2FloorForReadTools(t *testing.T) {
	// Verify that file_read and grep results never degrade to Tier 1 metadata stubs,
	// even when they would normally be old enough for Tier 1.
	const numTools = 26
	var messages []client.Message
	messages = append(messages, client.Message{
		Role:    "user",
		Content: client.NewTextContent("Start"),
	})

	// Tools 0-4: floor tools, 5-25: normal tools.
	// With 26 total results, tool 5 sits exactly at distFromEnd=20, so it should
	// hit Tier 1 and serve as the non-floor control case.
	for i := 0; i < numTools; i++ {
		id := fmt.Sprintf("tu-%d", i)
		name := "tool_other"
		if i < 3 {
			name = "file_read"
		} else if i < 5 {
			name = "grep"
		}
		args := json.RawMessage(fmt.Sprintf(`{"arg":"value_%d"}`, i))
		content := fmt.Sprintf("Result %d: %s", i, strings.Repeat("x", 500))

		messages = append(messages, client.Message{
			Role: "assistant",
			Content: client.NewBlockContent([]client.ContentBlock{
				client.NewToolUseBlock(id, name, args),
			}),
		})
		messages = append(messages, client.Message{
			Role: "user",
			Content: client.NewBlockContent([]client.ContentBlock{
				client.NewToolResultBlock(id, content, false),
			}),
		})
	}

	compressOldToolResults(context.Background(), messages, 8, 300, nil)

	// Check the oldest file_read/grep results (tools 0-4, dist 25-21 from end)
	// These should be Tier 2 (truncated with head+tail), NOT Tier 1 (snipped).
	for i := 0; i < 5; i++ {
		msgIdx := 2 + i*2
		blocks := messages[msgIdx].Content.Blocks()
		resultContent := ""
		for _, b := range blocks {
			if b.Type == "tool_result" {
				if s, ok := b.ToolContent.(string); ok {
					resultContent = s
				}
			}
		}
		if strings.Contains(resultContent, "snipped") {
			t.Errorf("floor tool %d: should not be Tier 1 (snipped), got: %q", i, resultContent[:80])
		}
		if !strings.Contains(resultContent, "[... truncated") {
			t.Errorf("floor tool %d: should be Tier 2 (truncated), got: %q", i, resultContent[:80])
		}
	}

	// Non-floor control: tool 5 is old enough for Tier 1 and should become metadata-only.
	normalIdx := 2 + 5*2
	blocks := messages[normalIdx].Content.Blocks()
	resultContent := ""
	for _, b := range blocks {
		if b.Type == "tool_result" {
			if s, ok := b.ToolContent.(string); ok {
				resultContent = s
			}
		}
	}
	if !strings.Contains(resultContent, "snipped") {
		t.Fatalf("non-floor tool should be Tier 1 (snipped), got: %q", resultContent[:80])
	}
	if strings.Contains(resultContent, "[... truncated") {
		t.Fatalf("non-floor tool should not stay in Tier 2, got: %q", resultContent[:80])
	}
}

func TestCompressOldToolResults_EmergencyMode(t *testing.T) {
	// Simulate emergency compaction: keepRecent=1, maxChars=100
	var messages []client.Message
	messages = append(messages, client.Message{
		Role:    "user",
		Content: client.NewTextContent("Start"),
	})

	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("tu-%d", i)
		content := strings.Repeat("y", 300)
		messages = append(messages, client.Message{
			Role: "assistant",
			Content: client.NewBlockContent([]client.ContentBlock{
				client.NewToolUseBlock(id, "bash", json.RawMessage(`{"command":"ls"}`)),
			}),
		})
		messages = append(messages, client.Message{
			Role: "user",
			Content: client.NewBlockContent([]client.ContentBlock{
				client.NewToolResultBlock(id, content, false),
			}),
		})
	}

	compressOldToolResults(context.Background(), messages, 1, 100, nil)

	// Only the last tool result should be full
	for i := 0; i < 5; i++ {
		msgIdx := 2 + i*2
		blocks := messages[msgIdx].Content.Blocks()
		for _, b := range blocks {
			if b.Type == "tool_result" {
				s, ok := b.ToolContent.(string)
				if !ok {
					continue
				}
				if i == 4 {
					// Last one: tier 3, full
					if len(s) < 300 {
						t.Errorf("last tool result should be full, got %d chars", len(s))
					}
				} else {
					// All others should be compressed
					if len(s) >= 300 {
						t.Errorf("tool %d should be compressed, got %d chars", i, len(s))
					}
				}
			}
		}
	}
}

// TestAgentLoop_ReactiveCompaction verifies the reactive compaction safety net:
//
//  1. Agent loop has enough messages to build context (6+ tool iterations)
//  2. Mock server returns HTTP 400 "prompt is too long" after sufficient iterations
//  3. Reactive compaction fires: PersistLearnings → compress → summary → ShapeHistory
//  4. Retry succeeds with compacted messages
//  5. compactionApplied flag prevents infinite retry loops
//
// The proactive compaction is bypassed by reporting low input tokens until the
// server triggers the 400 error, simulating the case where token counting
// underestimates and the API rejects the request.
func TestAgentLoop_ReactiveCompaction(t *testing.T) {
	memoryDir := t.TempDir()

	var mu sync.Mutex
	var calls []string

	// After 6 tool iterations (13+ messages), return a 400 context-length error
	// on the next main-tier call, then succeed on retry.
	contextErrorReturned := false
	retrySucceeded := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := readBody(r.Body)
		defer r.Body.Close()

		var req struct {
			ModelTier string `json:"model_tier"`
			Messages  []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		json.Unmarshal(raw, &req)

		mu.Lock()
		callNum := len(calls) + 1

		// Small-tier calls (PersistLearnings, GenerateSummary)
		if req.ModelTier == "small" {
			isPersist := false
			isSummary := false
			for _, m := range req.Messages {
				var text string
				json.Unmarshal(m.Content, &text)
				if strings.Contains(text, "extracting durable knowledge") {
					isPersist = true
				}
				if strings.Contains(text, "Compress the following conversation") {
					isSummary = true
				}
			}

			if isPersist {
				calls = append(calls, fmt.Sprintf("call %d: PERSIST", callNum))
				mu.Unlock()
				t.Logf("Call %d: [small] PersistLearnings (messages: %d)", callNum, len(req.Messages))
				json.NewEncoder(w).Encode(nativeResponse(
					"- Agent was analyzing system architecture\n- Reactive compaction triggered",
					"end_turn", nil, 50, 30))
				return
			}
			if isSummary {
				calls = append(calls, fmt.Sprintf("call %d: SUMMARY", callNum))
				mu.Unlock()
				t.Logf("Call %d: [small] GenerateSummary", callNum)
				json.NewEncoder(w).Encode(nativeResponse(
					"User asked about architecture. Agent analyzed multiple components before context overflow.",
					"end_turn", nil, 50, 30))
				return
			}

			calls = append(calls, fmt.Sprintf("call %d: small-other", callNum))
			mu.Unlock()
			json.NewEncoder(w).Encode(nativeResponse("ok", "end_turn", nil, 50, 30))
			return
		}

		// Main-tier calls
		msgCount := len(req.Messages)

		if msgCount < 12 {
			// Keep looping with tool calls, report LOW tokens so proactive
			// compaction does NOT trigger (under 85% of 128000).
			calls = append(calls, fmt.Sprintf("call %d: TOOL (msgs=%d)", callNum, msgCount))
			mu.Unlock()
			t.Logf("Call %d: [main] tool_use (msgs=%d)", callNum, msgCount)
			json.NewEncoder(w).Encode(nativeResponse(
				"", "tool_use",
				toolCall("think", fmt.Sprintf(`{"thought":"Step %d analysis"}`, msgCount)),
				500, 100)) // Low tokens — proactive compaction won't trigger
			return
		}

		// At 12+ messages: return 400 context-length error (once)
		if !contextErrorReturned {
			contextErrorReturned = true
			calls = append(calls, fmt.Sprintf("call %d: CONTEXT_ERROR (msgs=%d)", callNum, msgCount))
			mu.Unlock()
			t.Logf("Call %d: [main] → 400 prompt is too long (msgs=%d)", callNum, msgCount)
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":{"type":"invalid_request_error","message":"prompt is too long"}}`))
			return
		}

		// After reactive compaction retries: succeed
		retrySucceeded = true
		calls = append(calls, fmt.Sprintf("call %d: RETRY_SUCCESS (msgs=%d)", callNum, msgCount))
		mu.Unlock()
		t.Logf("Call %d: [main] end_turn after reactive compaction (msgs=%d)", callNum, msgCount)
		json.NewEncoder(w).Encode(nativeResponse(
			"Analysis complete after reactive compaction.",
			"end_turn", nil, 800, 100))
	}))
	defer server.Close()

	gw := client.NewGatewayClient(server.URL, "")
	reg := NewToolRegistry()
	reg.Register(&thinkTool{})

	handler := &mockHandler{approveResult: true}

	loop := NewAgentLoop(gw, reg, "medium", "", 20, 2000, 200, nil, nil, nil)
	loop.SetContextWindow(128000) // High window so proactive compaction doesn't trigger
	loop.SetMemoryDir(memoryDir)
	loop.SetHandler(handler)

	result, usage, err := loop.Run(context.Background(),
		"Analyze each component of the system. Think through every step carefully.",
		nil, nil)
	if err != nil {
		t.Logf("Run error: %v", err)
	}

	mu.Lock()
	t.Logf("\n=== Call sequence (%d total) ===", len(calls))
	for _, c := range calls {
		t.Logf("  %s", c)
	}

	hasPersist := false
	hasSummary := false
	hasContextError := false
	hasRetrySuccess := false
	for _, c := range calls {
		if strings.Contains(c, "PERSIST") {
			hasPersist = true
		}
		if strings.Contains(c, "SUMMARY") {
			hasSummary = true
		}
		if strings.Contains(c, "CONTEXT_ERROR") {
			hasContextError = true
		}
		if strings.Contains(c, "RETRY_SUCCESS") {
			hasRetrySuccess = true
		}
	}
	mu.Unlock()

	t.Logf("Result: %d chars", len(result))
	t.Logf("Usage: %d LLM calls", usage.LLMCalls)

	// Verify reactive compaction chain
	if !hasContextError {
		t.Error("expected context-length 400 error to be returned by mock server")
	}
	if !hasPersist {
		t.Error("PersistLearnings should fire during reactive compaction")
	}
	if !hasSummary {
		t.Error("GenerateSummary should fire during reactive compaction")
	}
	if !hasRetrySuccess {
		t.Error("retry after reactive compaction should succeed")
	}
	if !retrySucceeded {
		t.Error("retrySucceeded flag should be true")
	}

	// Verify MEMORY.md was written
	memPath := filepath.Join(memoryDir, "MEMORY.md")
	memData, err := os.ReadFile(memPath)
	if err != nil {
		t.Fatalf("MEMORY.md should exist after reactive PersistLearnings: %v", err)
	}
	memContent := string(memData)
	t.Logf("\n=== MEMORY.md ===\n%s", memContent)
	if !strings.Contains(memContent, "Auto-persisted") {
		t.Error("MEMORY.md should contain Auto-persisted section")
	}

	// Verify result came through
	if result == "" {
		t.Error("expected non-empty result after successful retry")
	}
}

// TestAgentLoop_ReactiveCompactionNoDoubleRetry verifies the compactionApplied
// guard prevents infinite loops: if reactive compaction fires but the retry
// ALSO returns a context-length error, the loop should fail instead of retrying.
func TestAgentLoop_ReactiveCompactionNoDoubleRetry(t *testing.T) {
	contextErrors := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := readBody(r.Body)
		defer r.Body.Close()

		var req struct {
			ModelTier string `json:"model_tier"`
			Messages  []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		json.Unmarshal(raw, &req)

		// Small-tier: always succeed
		if req.ModelTier == "small" {
			for _, m := range req.Messages {
				var text string
				json.Unmarshal(m.Content, &text)
				if strings.Contains(text, "extracting durable knowledge") {
					json.NewEncoder(w).Encode(nativeResponse("learnings", "end_turn", nil, 50, 30))
					return
				}
				if strings.Contains(text, "Compress the following conversation") {
					json.NewEncoder(w).Encode(nativeResponse("summary", "end_turn", nil, 50, 30))
					return
				}
			}
			json.NewEncoder(w).Encode(nativeResponse("ok", "end_turn", nil, 50, 30))
			return
		}

		msgCount := len(req.Messages)
		t.Logf("Main-tier call: msgs=%d, contextErrors=%d", msgCount, contextErrors)

		if msgCount < 6 && contextErrors == 0 {
			// Build up messages with tool calls until we first trigger overflow.
			json.NewEncoder(w).Encode(nativeResponse(
				"", "tool_use",
				toolCall("think", `{"thought":"building context"}`),
				500, 100))
			return
		}

		// Always return context-length error once we've started — even after
		// compaction reduces message count. This forces the double-retry guard.
		contextErrors++
		t.Logf("Returning context-length error #%d (msgs=%d)", contextErrors, msgCount)
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"type":"invalid_request_error","message":"context_length_exceeded"}}`))
	}))
	defer server.Close()

	gw := client.NewGatewayClient(server.URL, "")
	reg := NewToolRegistry()
	reg.Register(&thinkTool{})

	handler := &mockHandler{approveResult: true}
	loop := NewAgentLoop(gw, reg, "medium", "", 20, 2000, 200, nil, nil, nil)
	loop.SetContextWindow(128000)
	loop.SetMemoryDir(t.TempDir())
	loop.SetHandler(handler)

	_, _, err := loop.Run(context.Background(), "Trigger reactive compaction that fails on retry too.", nil, nil)

	// Should get an error — NOT an infinite loop
	if err == nil {
		t.Fatal("expected error when retry after reactive compaction also fails")
	}
	t.Logf("Got expected error: %v", err)

	// Should have seen at most 2 context-length errors (original + one retry)
	if contextErrors > 2 {
		t.Errorf("expected at most 2 context-length errors (original + retry), got %d — infinite loop guard may be broken", contextErrors)
	}
}

func TestReactiveSummaryInput_InsertsPriorSummaryOnce(t *testing.T) {
	messages := []client.Message{
		{Role: "system", Content: client.NewTextContent("system")},
		{Role: "user", Content: client.NewTextContent("first user")},
		{Role: "assistant", Content: client.NewTextContent("recent reply")},
	}

	withSummary := reactiveSummaryInput(messages, "Earlier work happened")
	if len(withSummary) != len(messages)+1 {
		t.Fatalf("expected injected summary message, got %d messages", len(withSummary))
	}
	if got := withSummary[2].Content.Text(); got != "Previous context summary: Earlier work happened" {
		t.Fatalf("unexpected injected summary message: %q", got)
	}

	again := reactiveSummaryInput(withSummary, "Earlier work happened")
	if len(again) != len(withSummary) {
		t.Fatal("summary should not be injected twice")
	}
}

func TestAgentLoop_ReactiveCompaction_UsesEmergencyFallbackWhenSoftStillOverBudget(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	mainCalls := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := readBody(r.Body)
		defer r.Body.Close()

		var req struct {
			ModelTier string `json:"model_tier"`
			Messages  []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		json.Unmarshal(raw, &req)

		mu.Lock()
		defer mu.Unlock()

		if req.ModelTier == "small" {
			calls = append(calls, "summary")
			json.NewEncoder(w).Encode(nativeResponse(
				"condensed summary",
				"end_turn", nil, 50, 30))
			return
		}

		mainCalls++
		if mainCalls == 1 {
			calls = append(calls, "context_error")
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"error":{"type":"invalid_request_error","message":"prompt is too long"}}`))
			return
		}

		calls = append(calls, "retry_success")
		json.NewEncoder(w).Encode(nativeResponse(
			"Recovered after emergency fallback.",
			"end_turn", nil, 500, 100))
	}))
	defer server.Close()

	gw := client.NewGatewayClient(server.URL, "")
	reg := NewToolRegistry()
	reg.Register(&thinkTool{})

	loop := NewAgentLoop(gw, reg, "medium", "", 10, 2000, 200, nil, nil, nil)
	loop.SetContextWindow(100000)

	huge := strings.Repeat("x", 450000)
	history := []client.Message{
		{Role: "user", Content: client.NewTextContent(huge)},
		{Role: "assistant", Content: client.NewTextContent("ack")},
		{Role: "user", Content: client.NewTextContent("second turn")},
		{Role: "assistant", Content: client.NewTextContent("second reply")},
		{Role: "user", Content: client.NewTextContent("third turn")},
		{Role: "assistant", Content: client.NewTextContent("third reply")},
	}

	result, _, err := loop.Run(context.Background(), "trigger reactive overflow", nil, history)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Recovered after emergency fallback." {
		t.Fatalf("unexpected result: %q", result)
	}

	mu.Lock()
	gotCalls := append([]string(nil), calls...)
	mu.Unlock()

	summaryCalls := 0
	for _, call := range gotCalls {
		if call == "summary" {
			summaryCalls++
		}
	}
	if summaryCalls != 2 {
		t.Fatalf("expected soft + emergency summary calls, got %d (%v)", summaryCalls, gotCalls)
	}
	if len(gotCalls) != 4 || gotCalls[0] != "context_error" || gotCalls[1] != "summary" || gotCalls[2] != "summary" || gotCalls[3] != "retry_success" {
		t.Fatalf("unexpected call order: %v", gotCalls)
	}
}

// TestAgentLoop_CompactionTriggersOnWarmCache is a regression test for the
// compaction-gate fix that sums cached tokens into the gate's input.
//
// Before the fix, lastInputTokens was assigned normalizedUsage.InputTokens —
// which Anthropic defines as *excluding* cached tokens. A long warm-cache
// session would report input_tokens of a few hundred while cache_read_tokens
// carried the real 90K+ prompt, so ShouldCompact never tripped and compaction
// never fired until the cache went cold.
//
// After the fix, totalPromptTokens(u) = input + cache_read + cache_creation,
// which reflects the real context-window consumption.
//
// This test drives the loop against a mock that always reports a small
// InputTokens but a large CacheReadTokens. Once messages grow past
// MinShapeable (9), the gate must trigger — PersistLearnings + GenerateSummary
// must both fire. If the test fails, the gate has regressed to the pre-fix
// behaviour.
func TestAgentLoop_CompactionTriggersOnWarmCache(t *testing.T) {
	memoryDir := t.TempDir()

	var mu sync.Mutex
	var calls []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := readBody(r.Body)
		defer r.Body.Close()

		var req struct {
			ModelTier string `json:"model_tier"`
			Messages  []struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		json.Unmarshal(raw, &req)

		mu.Lock()
		callNum := len(calls) + 1

		if req.ModelTier == "small" {
			isPersist := false
			isSummary := false
			for _, m := range req.Messages {
				var text string
				json.Unmarshal(m.Content, &text)
				if strings.Contains(text, "extracting durable knowledge") {
					isPersist = true
				}
				if strings.Contains(text, "Compress the following conversation") {
					isSummary = true
				}
			}
			if isPersist {
				calls = append(calls, fmt.Sprintf("call %d: PERSIST", callNum))
				mu.Unlock()
				json.NewEncoder(w).Encode(nativeResponse(
					"- Warm-cache compaction fired correctly",
					"end_turn", nil, 50, 30))
				return
			}
			if isSummary {
				calls = append(calls, fmt.Sprintf("call %d: SUMMARY", callNum))
				mu.Unlock()
				json.NewEncoder(w).Encode(nativeResponse(
					"Agent summarised cached history.", "end_turn", nil, 50, 30))
				return
			}
			calls = append(calls, fmt.Sprintf("call %d: small-other", callNum))
			mu.Unlock()
			json.NewEncoder(w).Encode(nativeResponse("ok", "end_turn", nil, 50, 30))
			return
		}

		// Main-tier: simulate a warm cache — small InputTokens, large CacheReadTokens.
		// context_window=2000 so threshold = 1700. InputTokens alone (200) is below
		// threshold; total prompt (200 + 1800 cache_read = 2000) is above. Pre-fix
		// code reads only InputTokens and would NOT compact; post-fix reads
		// totalPromptTokens and SHOULD compact once msgCount > MinShapeable (9).
		msgCount := len(req.Messages)
		resp := client.CompletionResponse{
			Model:        "test-model",
			FinishReason: "tool_use",
			FunctionCall: nil,
			ToolCalls: []client.FunctionCall{{
				Name:      "think",
				Arguments: json.RawMessage(fmt.Sprintf(`{"thought":"step with %d msgs"}`, msgCount)),
			}},
			Usage: client.Usage{
				InputTokens:     200,
				OutputTokens:    50,
				TotalTokens:     250,
				CacheReadTokens: 1800,
			},
			RequestID: "req-test",
		}
		if msgCount >= 12 {
			// Emit end_turn so the run can terminate after compaction fires.
			resp.FinishReason = "end_turn"
			resp.ToolCalls = nil
			resp.OutputText = "Analysis complete after warm-cache compaction."
		}
		calls = append(calls, fmt.Sprintf("call %d: MAIN (msgs=%d, input=200, cache_read=1800)", callNum, msgCount))
		mu.Unlock()
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	gw := client.NewGatewayClient(server.URL, "")
	reg := NewToolRegistry()
	reg.Register(&thinkTool{})

	handler := &mockHandler{approveResult: true}

	loop := NewAgentLoop(gw, reg, "medium", "", 20, 2000, 200, nil, nil, nil)
	loop.SetContextWindow(2000)
	loop.SetMemoryDir(memoryDir)
	loop.SetHandler(handler)

	_, _, err := loop.Run(context.Background(),
		"Run through several reasoning steps so message count grows past MinShapeable.",
		nil, nil)
	if err != nil {
		t.Logf("Run error (iteration limit is acceptable): %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	t.Logf("\n=== Call sequence (%d total) ===", len(calls))
	for _, c := range calls {
		t.Logf("  %s", c)
	}

	hasPersist := false
	hasSummary := false
	for _, c := range calls {
		if strings.Contains(c, "PERSIST") {
			hasPersist = true
		}
		if strings.Contains(c, "SUMMARY") {
			hasSummary = true
		}
	}

	if !hasPersist {
		t.Error("PersistLearnings must fire once warm-cache total prompt exceeds 85% — gate regressed to pre-fix behavior")
	}
	if !hasSummary {
		t.Error("GenerateSummary must fire once warm-cache total prompt exceeds 85% — gate regressed to pre-fix behavior")
	}
}

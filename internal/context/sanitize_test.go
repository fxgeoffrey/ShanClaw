package context

import (
	"testing"

	"github.com/Kocoro-lab/ShanClaw/internal/client"
)

func TestSanitizeHistory_Empty(t *testing.T) {
	result := SanitizeHistory(nil)
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
	result = SanitizeHistory([]client.Message{})
	if len(result) != 0 {
		t.Errorf("expected empty, got %d", len(result))
	}
}

func TestSanitizeHistory_CleanPassthrough(t *testing.T) {
	msgs := []client.Message{
		{Role: "user", Content: client.NewTextContent("hello")},
		{Role: "assistant", Content: client.NewTextContent("hi there")},
		{Role: "user", Content: client.NewTextContent("how are you")},
		{Role: "assistant", Content: client.NewTextContent("doing well")},
	}
	result := SanitizeHistory(msgs)
	if len(result) != 4 {
		t.Fatalf("expected 4, got %d", len(result))
	}
	for i, m := range result {
		if m.Role != msgs[i].Role || m.Content.Text() != msgs[i].Content.Text() {
			t.Errorf("msg %d mismatch", i)
		}
	}
}

func TestSanitizeHistory_DropsToolCallPlaceholders(t *testing.T) {
	msgs := []client.Message{
		{Role: "user", Content: client.NewTextContent("search for X")},
		{Role: "assistant", Content: client.NewTextContent("[tool_call: web_search]")},
		{Role: "assistant", Content: client.NewTextContent("[tool_call: web_search]")},
		{Role: "assistant", Content: client.NewTextContent("here are the results")},
	}
	result := SanitizeHistory(msgs)
	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
	if result[0].Role != "user" {
		t.Errorf("expected user, got %s", result[0].Role)
	}
	if result[1].Content.Text() != "here are the results" {
		t.Errorf("expected final assistant text, got %q", result[1].Content.Text())
	}
}

func TestSanitizeHistory_DropsPlainTextToolMessages(t *testing.T) {
	msgs := []client.Message{
		{Role: "user", Content: client.NewTextContent("hello")},
		{Role: "assistant", Content: client.NewTextContent("let me search")},
		{Role: "tool", Content: client.NewTextContent("Search results for: shoes")},
		{Role: "assistant", Content: client.NewTextContent("found some shoes")},
	}
	result := SanitizeHistory(msgs)
	// tool msg dropped, consecutive assistants merged → keep last
	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
	if result[1].Content.Text() != "found some shoes" {
		t.Errorf("expected merged assistant, got %q", result[1].Content.Text())
	}
}

func TestSanitizeHistory_DropsErrorMessages(t *testing.T) {
	msgs := []client.Message{
		{Role: "user", Content: client.NewTextContent("hello")},
		{Role: "assistant", Content: client.NewTextContent("Sorry, the AI service encountered a temporary error. Please try again.")},
		{Role: "user", Content: client.NewTextContent("try again")},
		{Role: "assistant", Content: client.NewTextContent("Sorry, the AI service encountered a temporary error. Please try again.")},
	}
	result := SanitizeHistory(msgs)
	// Both error assistants dropped, consecutive users merged → keep last
	if len(result) != 1 {
		t.Fatalf("expected 1 message, got %d", len(result))
	}
	if result[0].Content.Text() != "try again" {
		t.Errorf("expected last user, got %q", result[0].Content.Text())
	}
}

func TestSanitizeHistory_DropsAgentFailedError(t *testing.T) {
	msgs := []client.Message{
		{Role: "user", Content: client.NewTextContent("Say hi")},
		{Role: "assistant", Content: client.NewTextContent("[error: agent failed to respond]")},
		{Role: "user", Content: client.NewTextContent("hello")},
		{Role: "assistant", Content: client.NewTextContent("hi!")},
	}
	result := SanitizeHistory(msgs)
	// error dropped, consecutive users merged → keep last
	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
	if result[0].Content.Text() != "hello" {
		t.Errorf("expected second user msg, got %q", result[0].Content.Text())
	}
}

func TestSanitizeHistory_MergesConsecutiveAssistant(t *testing.T) {
	msgs := []client.Message{
		{Role: "user", Content: client.NewTextContent("hello")},
		{Role: "assistant", Content: client.NewTextContent("first response")},
		{Role: "assistant", Content: client.NewTextContent("second response")},
		{Role: "assistant", Content: client.NewTextContent("third response")},
	}
	result := SanitizeHistory(msgs)
	if len(result) != 2 {
		t.Fatalf("expected 2, got %d", len(result))
	}
	if result[1].Content.Text() != "third response" {
		t.Errorf("expected last assistant kept, got %q", result[1].Content.Text())
	}
}

func TestSanitizeHistory_MergesConsecutiveUser(t *testing.T) {
	msgs := []client.Message{
		{Role: "user", Content: client.NewTextContent("first")},
		{Role: "user", Content: client.NewTextContent("second")},
		{Role: "user", Content: client.NewTextContent("third")},
		{Role: "assistant", Content: client.NewTextContent("got it")},
	}
	result := SanitizeHistory(msgs)
	if len(result) != 2 {
		t.Fatalf("expected 2, got %d", len(result))
	}
	if result[0].Content.Text() != "third" {
		t.Errorf("expected last user kept, got %q", result[0].Content.Text())
	}
}

func TestSanitizeHistory_FullCorruptionScenario(t *testing.T) {
	// Reproduce the exact little-v corruption pattern
	msgs := []client.Message{
		{Role: "user", Content: client.NewTextContent("check rankings")},
		{Role: "assistant", Content: client.NewTextContent("The request was cancelled or timed out.")},
		{Role: "user", Content: client.NewTextContent("heartbeat prompt")},
		{Role: "assistant", Content: client.NewTextContent("I noticed the timeout")},
		{Role: "assistant", Content: client.NewTextContent("urgent confirmation")},
		{Role: "assistant", Content: client.NewTextContent("[tool_call: web_search]")},
		{Role: "assistant", Content: client.NewTextContent("[tool_call: web_search]")},
		{Role: "assistant", Content: client.NewTextContent("[tool_call: web_search]")},
		{Role: "tool", Content: client.NewTextContent("Search results 1")},
		{Role: "tool", Content: client.NewTextContent("Search results 2")},
		{Role: "tool", Content: client.NewTextContent("Search results 3")},
		{Role: "assistant", Content: client.NewTextContent("Here are the rankings")},
		{Role: "user", Content: client.NewTextContent("你好呀！")},
		{Role: "assistant", Content: client.NewTextContent("Sorry, the AI service encountered a temporary error. Please try again.")},
		{Role: "user", Content: client.NewTextContent("今天有没有什么更新")},
		{Role: "assistant", Content: client.NewTextContent("Sorry, the AI service encountered a temporary error. Please try again.")},
	}

	result := SanitizeHistory(msgs)

	// Verify alternation
	for i := 1; i < len(result); i++ {
		if result[i].Role == result[i-1].Role {
			t.Errorf("consecutive same role at %d: %s", i, result[i].Role)
		}
	}

	// Should not contain any error or tool_call messages
	for i, m := range result {
		text := m.Content.Text()
		if m.Role == "tool" {
			t.Errorf("tool message at %d should be dropped", i)
		}
		if text == "Sorry, the AI service encountered a temporary error. Please try again." {
			t.Errorf("error message at %d should be dropped", i)
		}
		if text == "[tool_call: web_search]" {
			t.Errorf("tool_call placeholder at %d should be dropped", i)
		}
	}

	// Verify we kept meaningful content
	found := false
	for _, m := range result {
		if m.Content.Text() == "Here are the rankings" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'Here are the rankings' to survive")
	}

	t.Logf("sanitized %d → %d messages", len(msgs), len(result))
	for i, m := range result {
		t.Logf("  [%d] %s: %s", i, m.Role, truncStr(m.Content.Text(), 50))
	}
}

func TestSanitizeHistory_PreservesSystemMessages(t *testing.T) {
	msgs := []client.Message{
		{Role: "system", Content: client.NewTextContent("you are helpful")},
		{Role: "user", Content: client.NewTextContent("hello")},
		{Role: "assistant", Content: client.NewTextContent("hi")},
	}
	result := SanitizeHistory(msgs)
	if len(result) != 3 {
		t.Fatalf("expected 3, got %d", len(result))
	}
	if result[0].Role != "system" {
		t.Errorf("system message should be preserved")
	}
}

func TestSanitizeHistory_DropsOrphanedToolUse(t *testing.T) {
	// Assistant has a tool_use block but the matching tool_result was
	// stripped (e.g. it was an error marker). The tool_use must be removed
	// to prevent API rejection.
	msgs := []client.Message{
		{Role: "user", Content: client.NewTextContent("fetch the page")},
		{Role: "assistant", Content: client.NewBlockContent([]client.ContentBlock{
			{Type: "text", Text: "Let me fetch that."},
			client.NewToolUseBlock("toolu_orphan1", "web_fetch", nil),
		})},
		// No tool_result for toolu_orphan1 follows (it was dropped).
		{Role: "user", Content: client.NewTextContent("try again")},
		{Role: "assistant", Content: client.NewTextContent("OK")},
	}
	result := SanitizeHistory(msgs)

	// The orphaned tool_use should be stripped; the text should survive.
	for _, m := range result {
		if m.Content.HasBlocks() {
			for _, b := range m.Content.Blocks() {
				if b.Type == "tool_use" && b.ID == "toolu_orphan1" {
					t.Error("orphaned tool_use should be stripped")
				}
			}
		}
	}

	// The text "Let me fetch that." should survive.
	found := false
	for _, m := range result {
		if m.Content.HasBlocks() {
			for _, b := range m.Content.Blocks() {
				if b.Type == "text" && b.Text == "Let me fetch that." {
					found = true
				}
			}
		}
	}
	if !found {
		t.Error("text content should survive after stripping orphaned tool_use")
	}
}

func TestSanitizeHistory_DropsAssistantWithOnlyOrphanedToolUse(t *testing.T) {
	// Assistant message has ONLY a tool_use block (no text).
	// After stripping the orphan, the message is empty → dropped entirely.
	msgs := []client.Message{
		{Role: "user", Content: client.NewTextContent("do something")},
		{Role: "assistant", Content: client.NewBlockContent([]client.ContentBlock{
			client.NewToolUseBlock("toolu_orphan2", "bash", nil),
		})},
		{Role: "user", Content: client.NewTextContent("hello")},
		{Role: "assistant", Content: client.NewTextContent("hi")},
	}
	result := SanitizeHistory(msgs)

	// Should be: user("hello") → assistant("hi")
	// The first user + empty assistant are dropped/merged.
	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
	if result[0].Role != "user" {
		t.Errorf("expected user, got %s", result[0].Role)
	}
	if result[1].Content.Text() != "hi" {
		t.Errorf("expected 'hi', got %q", result[1].Content.Text())
	}
}

func TestSanitizeHistory_PreservesMatchedToolUse(t *testing.T) {
	// Normal case: tool_use has a matching tool_result → preserved.
	msgs := []client.Message{
		{Role: "user", Content: client.NewTextContent("search")},
		{Role: "assistant", Content: client.NewBlockContent([]client.ContentBlock{
			{Type: "text", Text: "Searching..."},
			client.NewToolUseBlock("toolu_matched", "web_search", nil),
		})},
		{Role: "user", Content: client.NewBlockContent([]client.ContentBlock{
			client.NewToolResultBlock("toolu_matched", "results here", false),
		})},
		{Role: "assistant", Content: client.NewTextContent("found it")},
	}
	result := SanitizeHistory(msgs)

	if len(result) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(result))
	}

	// Verify the tool_use block is preserved.
	assistantBlocks := result[1].Content.Blocks()
	hasToolUse := false
	for _, b := range assistantBlocks {
		if b.Type == "tool_use" && b.ID == "toolu_matched" {
			hasToolUse = true
		}
	}
	if !hasToolUse {
		t.Error("matched tool_use should be preserved")
	}
}

func TestSanitizeHistory_NonAdjacentToolResultDoesNotMatch(t *testing.T) {
	// tool_result exists later in conversation but not adjacent to the tool_use.
	// The tool_use should still be stripped (adjacency required).
	msgs := []client.Message{
		{Role: "user", Content: client.NewTextContent("fetch page")},
		{Role: "assistant", Content: client.NewBlockContent([]client.ContentBlock{
			{Type: "text", Text: "Fetching..."},
			client.NewToolUseBlock("toolu_nonadj", "web_fetch", nil),
		})},
		{Role: "user", Content: client.NewTextContent("actually never mind")},
		{Role: "assistant", Content: client.NewTextContent("OK, cancelled")},
		// tool_result appears later but not adjacent to the tool_use
		{Role: "user", Content: client.NewBlockContent([]client.ContentBlock{
			client.NewToolResultBlock("toolu_nonadj", "late result", false),
		})},
		{Role: "assistant", Content: client.NewTextContent("done")},
	}
	result := SanitizeHistory(msgs)

	// The tool_use in the first assistant should be stripped.
	for _, m := range result {
		if m.Content.HasBlocks() {
			for _, b := range m.Content.Blocks() {
				if b.Type == "tool_use" && b.ID == "toolu_nonadj" {
					t.Error("non-adjacent tool_use should be stripped")
				}
			}
		}
	}

	// The non-adjacent tool_result should also be stripped (its tool_use is gone).
	for _, m := range result {
		if m.Content.HasBlocks() {
			for _, b := range m.Content.Blocks() {
				if b.Type == "tool_result" && b.ToolUseID == "toolu_nonadj" {
					t.Error("orphaned tool_result should be stripped")
				}
			}
		}
	}
}

func TestSanitizeHistory_MergeDeletesToolUseLeavingOrphanResult(t *testing.T) {
	// Consecutive assistant merge deletes the one with tool_use,
	// orphaning the tool_result in the following user message.
	msgs := []client.Message{
		{Role: "user", Content: client.NewTextContent("do it")},
		{Role: "assistant", Content: client.NewBlockContent([]client.ContentBlock{
			client.NewToolUseBlock("toolu_merged", "bash", nil),
		})},
		{Role: "assistant", Content: client.NewTextContent("noise")},
		{Role: "user", Content: client.NewBlockContent([]client.ContentBlock{
			client.NewToolResultBlock("toolu_merged", "output", false),
		})},
		{Role: "assistant", Content: client.NewTextContent("done")},
	}
	result := SanitizeHistory(msgs)

	// After merge: assistant("noise") replaces assistant(tool_use).
	// The tool_result should then be stripped as orphaned.
	for _, m := range result {
		if m.Content.HasBlocks() {
			for _, b := range m.Content.Blocks() {
				if b.Type == "tool_result" && b.ToolUseID == "toolu_merged" {
					t.Error("tool_result orphaned by merge should be stripped")
				}
			}
		}
	}

	// Verify alternation is maintained.
	for i := 1; i < len(result); i++ {
		if result[i].Role == result[i-1].Role {
			t.Errorf("consecutive same role at %d: %s", i, result[i].Role)
		}
	}
}

func TestSanitizeHistory_DuplicateIDNonAdjacentStripped(t *testing.T) {
	// A valid pair uses ID "toolu_dup", then a stale tool_result reuses
	// the same ID later. The stale one must be stripped.
	msgs := []client.Message{
		{Role: "user", Content: client.NewTextContent("search")},
		{Role: "assistant", Content: client.NewBlockContent([]client.ContentBlock{
			client.NewToolUseBlock("toolu_dup", "web_search", nil),
		})},
		{Role: "user", Content: client.NewBlockContent([]client.ContentBlock{
			client.NewToolResultBlock("toolu_dup", "valid result", false),
		})},
		{Role: "assistant", Content: client.NewTextContent("got it")},
		// Stale duplicate — same ID but not adjacent to any tool_use.
		{Role: "user", Content: client.NewBlockContent([]client.ContentBlock{
			client.NewToolResultBlock("toolu_dup", "stale duplicate", false),
		})},
		{Role: "assistant", Content: client.NewTextContent("done")},
	}
	result := SanitizeHistory(msgs)

	// The valid pair should survive.
	if !result[1].Content.HasBlocks() {
		t.Fatal("valid assistant with tool_use should be preserved")
	}
	foundValidUse := false
	for _, b := range result[1].Content.Blocks() {
		if b.Type == "tool_use" && b.ID == "toolu_dup" {
			foundValidUse = true
		}
	}
	if !foundValidUse {
		t.Error("adjacent tool_use should be preserved")
	}

	// The stale duplicate tool_result should be stripped.
	for i, m := range result {
		if i <= 2 {
			continue // skip the valid pair
		}
		if m.Content.HasBlocks() {
			for _, b := range m.Content.Blocks() {
				if b.Type == "tool_result" && b.ToolUseID == "toolu_dup" {
					t.Errorf("stale duplicate tool_result at position %d should be stripped", i)
				}
			}
		}
	}

	// Verify alternation.
	for i := 1; i < len(result); i++ {
		if result[i].Role == result[i-1].Role {
			t.Errorf("consecutive same role at %d: %s", i, result[i].Role)
		}
	}
}

func TestSanitizeHistory_LegacyToolRoleDropped(t *testing.T) {
	// Legacy tool-role messages (even with proper tool_result blocks) should
	// be dropped since the pairing pass only recognizes user-role tool_results.
	msgs := []client.Message{
		{Role: "user", Content: client.NewTextContent("hello")},
		{Role: "assistant", Content: client.NewBlockContent([]client.ContentBlock{
			client.NewToolUseBlock("toolu_legacy", "bash", nil),
		})},
		{Role: "tool", Content: client.NewBlockContent([]client.ContentBlock{
			client.NewToolResultBlock("toolu_legacy", "output", false),
		})},
		{Role: "assistant", Content: client.NewTextContent("done")},
	}
	result := SanitizeHistory(msgs)

	for _, m := range result {
		if m.Role == "tool" {
			t.Error("legacy tool-role message should be dropped")
		}
	}

	// Verify alternation.
	for i := 1; i < len(result); i++ {
		if result[i].Role == result[i-1].Role {
			t.Errorf("consecutive same role at %d: %s", i, result[i].Role)
		}
	}
}

func truncStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

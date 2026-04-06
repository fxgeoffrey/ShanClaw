package tools

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Kocoro-lab/ShanClaw/internal/agent"
	"github.com/Kocoro-lab/ShanClaw/internal/client"
)

// snapshotCtx builds a context with a fake conversation snapshot provider.
func snapshotCtx(msgs []client.Message) context.Context {
	return agent.WithConversationSnapshot(context.Background(), func() []client.Message {
		return msgs
	})
}

func TestExtractConversationContext_FiltersSystemAndEmpty(t *testing.T) {
	msgs := []client.Message{
		{Role: "system", Content: client.NewTextContent("you are helpful")},
		{Role: "user", Content: client.NewTextContent("hello")},
		{Role: "assistant", Content: client.NewTextContent("")}, // empty — skip
		{Role: "assistant", Content: client.NewTextContent("hi there")},
	}
	got := extractConversationContext(snapshotCtx(msgs))
	if len(got) != 2 {
		t.Fatalf("got %d msgs, want 2: %+v", len(got), got)
	}
	if got[0].Role != "user" || got[0].Content != "hello" {
		t.Errorf("msg[0] = %+v", got[0])
	}
	if got[1].Role != "assistant" || got[1].Content != "hi there" {
		t.Errorf("msg[1] = %+v", got[1])
	}
}

func TestExtractConversationContext_NoSnapshotProvider(t *testing.T) {
	got := extractConversationContext(context.Background())
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestExtractConversationContext_EmptySnapshot(t *testing.T) {
	got := extractConversationContext(snapshotCtx(nil))
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestExtractConversationContext_Max20Messages(t *testing.T) {
	var msgs []client.Message
	for i := 0; i < 25; i++ {
		msgs = append(msgs, client.Message{
			Role:    "user",
			Content: client.NewTextContent(string(rune('a' + i%26))),
		})
	}
	got := extractConversationContext(snapshotCtx(msgs))
	if len(got) != 20 {
		t.Fatalf("got %d msgs, want 20", len(got))
	}
	// Must keep the most recent 20 (indices 5..24).
	if got[0].Content != string(rune('a'+5%26)) {
		t.Errorf("expected first kept msg to be index 5, got %q", got[0].Content)
	}
}

func TestExtractConversationContext_RuneCountedBudget(t *testing.T) {
	// Each Chinese char is 3 bytes, 1 rune. Budget is 8000 runes (not bytes).
	// Build two messages of 5000 runes each → 10000 runes total → must drop one.
	// Prior implementation counted bytes, so 5000 runes ≈ 15000 bytes would
	// overflow on the first message alone and (incorrectly) drop everything.
	const runesPerMsg = 5000
	cn := strings.Repeat("中", runesPerMsg)
	if utf8.RuneCountInString(cn) != runesPerMsg {
		t.Fatalf("setup: rune count = %d, want %d", utf8.RuneCountInString(cn), runesPerMsg)
	}
	msgs := []client.Message{
		{Role: "user", Content: client.NewTextContent(cn)},
		{Role: "assistant", Content: client.NewTextContent(cn)},
	}
	got := extractConversationContext(snapshotCtx(msgs))
	if len(got) != 1 {
		t.Fatalf("got %d msgs, want 1 (8000-rune budget should drop exactly one)", len(got))
	}
	// The most recent one should survive — oldest is dropped first.
	if got[0].Role != "assistant" {
		t.Errorf("expected assistant msg to survive, got role=%q", got[0].Role)
	}
}

func TestExtractConversationContext_SkipsBlockMessagesWithoutText(t *testing.T) {
	// A message that is purely tool_use / tool_result blocks (no text block)
	// should be skipped, because we only want human-readable conversation.
	blockContent := client.NewBlockContent([]client.ContentBlock{
		{Type: "tool_use", ID: "tu1", Name: "some_tool"},
	})
	msgs := []client.Message{
		{Role: "user", Content: client.NewTextContent("real user message")},
		{Role: "assistant", Content: blockContent},
	}
	got := extractConversationContext(snapshotCtx(msgs))
	if len(got) != 1 {
		t.Fatalf("got %d msgs, want 1: %+v", len(got), got)
	}
	if got[0].Content != "real user message" {
		t.Errorf("msg[0] content = %q", got[0].Content)
	}
}

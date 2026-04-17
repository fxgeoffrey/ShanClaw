package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Kocoro-lab/ShanClaw/internal/client"
	"github.com/Kocoro-lab/ShanClaw/internal/skills"
)

// mockFnCompleter implements ctxwin.Completer via a callback function.
type mockFnCompleter struct {
	fn func(ctx context.Context, req client.CompletionRequest) (*client.CompletionResponse, error)
}

func (m *mockFnCompleter) Complete(ctx context.Context, req client.CompletionRequest) (*client.CompletionResponse, error) {
	return m.fn(ctx, req)
}

func TestParseDiscoveryOutput(t *testing.T) {
	loaded := []*skills.Skill{
		{Name: "kocoro", Description: "platform management"},
		{Name: "pdf-reader", Description: "analyze PDFs"},
		{Name: "mcp-builder", Description: "build MCP servers"},
	}

	tests := []struct {
		name     string
		output   string
		expected []string
	}{
		{"single match", "kocoro", []string{"kocoro"}},
		{"multiple matches", "kocoro\npdf-reader", []string{"kocoro", "pdf-reader"}},
		{"none", "none", nil},
		{"unknown ignored", "kocoro\nunknown-skill\npdf-reader", []string{"kocoro", "pdf-reader"}},
		{"blank lines ignored", "\nkocoro\n\n", []string{"kocoro"}},
		{"all unknown", "foo\nbar", nil},
		{"empty output", "", nil},
		{"with whitespace", "  kocoro  \n  pdf-reader  ", []string{"kocoro", "pdf-reader"}},
		{"duplicates deduped", "kocoro\nkocoro", []string{"kocoro"}},
		{"NONE case insensitive", "None", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDiscoveryOutput(tt.output, loaded)
			if len(got) != len(tt.expected) {
				t.Errorf("got %d results, want %d", len(got), len(tt.expected))
				return
			}
			for i := range got {
				if got[i].Name != tt.expected[i] {
					t.Errorf("got[%d].Name = %q, want %q", i, got[i].Name, tt.expected[i])
				}
			}
		})
	}
}

func TestFormatDiscoveryHint(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		if hint := formatDiscoveryHint(nil); hint != "" {
			t.Error("nil skills should produce empty hint")
		}
	})

	t.Run("single skill", func(t *testing.T) {
		matched := []*skills.Skill{
			{Name: "kocoro", Description: "Set up agents, skills, MCP servers"},
		}
		hint := formatDiscoveryHint(matched)
		if !strings.Contains(hint, "<system-reminder>") {
			t.Error("hint should contain system-reminder tags")
		}
		if !strings.Contains(hint, "kocoro") {
			t.Error("hint should contain matched skill name")
		}
		if !strings.Contains(hint, "use_skill") {
			t.Error("hint should mention use_skill")
		}
	})

	t.Run("long description truncated", func(t *testing.T) {
		long := strings.Repeat("a", 200)
		matched := []*skills.Skill{
			{Name: "test", Description: long},
		}
		hint := formatDiscoveryHint(matched)
		if strings.Contains(hint, long) {
			t.Error("long description should be truncated")
		}
		if !strings.Contains(hint, "...") {
			t.Error("truncated description should end with ...")
		}
	})
}

func TestDiscoverRelevantSkills_EmptyInputs(t *testing.T) {
	mock := &mockFnCompleter{
		fn: func(ctx context.Context, req client.CompletionRequest) (*client.CompletionResponse, error) {
			t.Fatal("should not call LLM with empty inputs")
			return nil, nil
		},
	}

	loaded := []*skills.Skill{{Name: "kocoro", Description: "test"}}

	// Empty user text
	result, _ := discoverRelevantSkills(context.Background(), mock, "", loaded)
	if len(result) != 0 {
		t.Error("empty user text should return nil")
	}

	// Empty skills
	result, _ = discoverRelevantSkills(context.Background(), mock, "hello", nil)
	if len(result) != 0 {
		t.Error("empty skills should return nil")
	}
}

func TestDiscoverRelevantSkills_Timeout(t *testing.T) {
	mock := &mockFnCompleter{
		fn: func(ctx context.Context, req client.CompletionRequest) (*client.CompletionResponse, error) {
			select {
			case <-time.After(10 * time.Second):
				return &client.CompletionResponse{OutputText: "kocoro"}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}

	loaded := []*skills.Skill{{Name: "kocoro", Description: "test"}}
	start := time.Now()
	result, _ := discoverRelevantSkills(context.Background(), mock, "create agent", loaded)
	elapsed := time.Since(start)

	if len(result) != 0 {
		t.Error("expected no results on timeout")
	}
	if elapsed > 7*time.Second {
		t.Errorf("should timeout in ~5s, took %v", elapsed)
	}
}

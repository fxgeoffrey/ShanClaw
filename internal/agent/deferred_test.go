package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/Kocoro-lab/ShanClaw/internal/client"
)

func TestParseLoadedHeader(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []string
	}{
		{"two tools", "LOADED:a,b\nrest of content", []string{"a", "b"}},
		{"one tool", "LOADED:playwright_click\nschema here", []string{"playwright_click"}},
		{"empty header", "LOADED:\nNo matching", nil},
		{"no header", "some random text", nil},
		{"no newline", "LOADED:a,b", []string{"a", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseLoadedHeader(tt.input)
			if len(got) != len(tt.expected) {
				t.Fatalf("expected %v, got %v", tt.expected, got)
			}
			for i := range got {
				if got[i] != tt.expected[i] {
					t.Errorf("index %d: expected %q, got %q", i, tt.expected[i], got[i])
				}
			}
		})
	}
}

// mockMCPTool implements ToolSourcer to classify as MCP.
type mockMCPTool struct{ name string }

func (m *mockMCPTool) Info() ToolInfo {
	return ToolInfo{Name: m.name, Description: "mock mcp tool", Parameters: map[string]any{"type": "object", "properties": map[string]any{}}}
}
func (m *mockMCPTool) Run(context.Context, string) (ToolResult, error) { return ToolResult{}, nil }
func (m *mockMCPTool) RequiresApproval() bool                          { return false }
func (m *mockMCPTool) ToolSource() ToolSource                          { return SourceMCP }
func (m *mockMCPTool) IsReadOnlyCall(string) bool                      { return false }

func TestRebuildSchemas_Deterministic(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(&mockTool{name: "grep"})
	reg.Register(&mockTool{name: "bash"})
	reg.Register(&mockMCPTool{name: "mcp_z"})
	reg.Register(&mockMCPTool{name: "mcp_a"})

	baseSchemas := buildLocalOnlySchemas(reg)

	loaded := map[string]client.Tool{
		"mcp_z": {Type: "function", Function: client.FunctionDef{Name: "mcp_z"}},
	}

	result := rebuildSchemas(reg, baseSchemas, loaded)

	// Canonical order: [bash, grep, mcp_z]
	if len(result) != 3 {
		t.Fatalf("expected 3 schemas, got %d", len(result))
	}
	expected := []string{"bash", "grep", "mcp_z"}
	for i, exp := range expected {
		got := schemaName(result[i])
		if got != exp {
			t.Errorf("index %d: expected %q, got %q", i, exp, got)
		}
	}

	// Determinism: same result on second call.
	result2 := rebuildSchemas(reg, baseSchemas, loaded)
	for i := range result {
		if schemaName(result[i]) != schemaName(result2[i]) {
			t.Errorf("index %d: non-deterministic", i)
		}
	}
}

func TestRebuildSchemas_NoDuplicates(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(&mockTool{name: "bash"})

	baseSchemas := reg.SortedSchemas()
	loaded := map[string]client.Tool{
		"bash": baseSchemas[0],
	}

	result := rebuildSchemas(reg, baseSchemas, loaded)
	if len(result) != 1 {
		t.Fatalf("expected 1 schema (no duplicate), got %d", len(result))
	}
}

func schemaName(t client.Tool) string {
	if t.Function.Name != "" {
		return t.Function.Name
	}
	return t.Name
}

// --- toolSearchTool tests (runtime implementation in deferred.go) ---

func newTestToolSearchAgent() *toolSearchTool {
	reg := NewToolRegistry()
	reg.Register(&mockMCPTool{name: "mock_mcp_a"})
	reg.Register(&mockMCPTool{name: "mock_mcp_b"})
	reg.Register(&mockTool{name: "bash"}) // local tool, not deferred

	deferred := map[string]bool{
		"mock_mcp_a": true,
		"mock_mcp_b": true,
	}
	return newToolSearchTool(reg, deferred)
}

func TestToolSearchTool_SelectExact(t *testing.T) {
	ts := newTestToolSearchAgent()
	result, err := ts.Run(context.Background(), `{"query":"select:mock_mcp_a,mock_mcp_b"}`)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("unexpected error: %s", result.Content)
	}
	header := strings.SplitN(result.Content, "\n", 2)[0]
	if !strings.Contains(header, "mock_mcp_a") || !strings.Contains(header, "mock_mcp_b") {
		t.Errorf("LOADED header should contain both tools, got: %s", header)
	}
}

func TestToolSearchTool_KeywordSearch(t *testing.T) {
	ts := newTestToolSearchAgent()
	// mockMCPTool has description "mock mcp tool"
	result, err := ts.Run(context.Background(), `{"query":"mcp"}`)
	if err != nil {
		t.Fatal(err)
	}
	header := strings.SplitN(result.Content, "\n", 2)[0]
	if !strings.Contains(header, "mock_mcp_a") {
		t.Errorf("keyword 'mcp' should match mock_mcp_a, got header: %s", header)
	}
}

func TestToolSearchTool_NoMatches(t *testing.T) {
	ts := newTestToolSearchAgent()
	result, err := ts.Run(context.Background(), `{"query":"nonexistent_xyz"}`)
	if err != nil {
		t.Fatal(err)
	}
	header := strings.SplitN(result.Content, "\n", 2)[0]
	if header != "LOADED:" {
		t.Errorf("empty LOADED header expected, got: %s", header)
	}
}

func TestToolSearchTool_OnlySearchesDeferred(t *testing.T) {
	ts := newTestToolSearchAgent()
	result, err := ts.Run(context.Background(), `{"query":"select:bash"}`)
	if err != nil {
		t.Fatal(err)
	}
	header := strings.SplitN(result.Content, "\n", 2)[0]
	if strings.Contains(header, "bash") {
		t.Error("tool_search should not find local tool 'bash'")
	}
}

func TestToolSearchTool_IsReadOnly(t *testing.T) {
	ts := newTestToolSearchAgent()
	if !ts.IsReadOnlyCall("{}") {
		t.Error("tool_search should be read-only")
	}
}

func TestToolSearchTool_RequiresApproval(t *testing.T) {
	ts := newTestToolSearchAgent()
	if ts.RequiresApproval() {
		t.Error("tool_search should not require approval")
	}
}

func TestPreseedDeferredSchemas_FiltersToCurrentDeferredSet(t *testing.T) {
	ws := NewWorkingSet()
	ws.Add("mcp_a", client.Tool{Type: "function", Function: client.FunctionDef{Name: "mcp_a"}})
	ws.Add("mcp_b", client.Tool{Type: "function", Function: client.FunctionDef{Name: "mcp_b"}})

	loaded := preseedDeferredSchemas(ws, map[string]bool{
		"mcp_a": true,
	})

	if len(loaded) != 1 {
		t.Fatalf("expected 1 pre-seeded schema, got %d", len(loaded))
	}
	if _, ok := loaded["mcp_a"]; !ok {
		t.Fatal("expected mcp_a to be pre-seeded")
	}
	if _, ok := loaded["mcp_b"]; ok {
		t.Fatal("mcp_b should not be pre-seeded when no longer deferred")
	}
}

func TestRemainingDeferredNames_RemovesWarmedTools(t *testing.T) {
	remaining := remainingDeferredNames(
		map[string]bool{"mcp_a": true, "mcp_b": true},
		map[string]client.Tool{"mcp_a": {Type: "function", Function: client.FunctionDef{Name: "mcp_a"}}},
	)

	if remaining["mcp_a"] {
		t.Fatal("mcp_a should have been removed from deferred list once warmed")
	}
	if !remaining["mcp_b"] {
		t.Fatal("mcp_b should remain deferred")
	}
}

func TestDeferredToolSummariesForNames_Sorted(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(&mockMCPTool{name: "mcp_b"})
	reg.Register(&mockMCPTool{name: "mcp_a"})

	summaries := deferredToolSummariesForNames(reg, map[string]bool{
		"mcp_b": true,
		"mcp_a": true,
	})

	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(summaries))
	}
	if summaries[0].Name != "mcp_a" || summaries[1].Name != "mcp_b" {
		t.Fatalf("expected sorted summaries, got %+v", summaries)
	}
}

func TestDeferredPromptSync_WarmedToolsBecomeLive(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(&mockTool{name: "bash"})
	reg.Register(&mockMCPTool{name: "mcp_a"})
	reg.Register(&mockMCPTool{name: "mcp_b"})

	loaded := map[string]client.Tool{
		"mcp_a": buildToolSchema(&mockMCPTool{name: "mcp_a"}),
	}
	remaining := remainingDeferredNames(deferredToolNames(reg), loaded)

	effTools := reg.Clone()
	effTools.Register(newToolSearchTool(reg, remaining))

	baseSchemas := buildLocalOnlySchemas(effTools)
	liveSchemas := rebuildSchemas(effTools, baseSchemas, loaded)
	liveNames := liveToolNames(liveSchemas)

	if !containsString(liveNames, "mcp_a") {
		t.Fatal("warmed tool should be promoted into live tool names")
	}
	if !containsString(liveNames, "tool_search") {
		t.Fatal("tool_search should remain available while cold deferred tools remain")
	}

	summaries := deferredToolSummariesForNames(reg, remaining)
	if len(summaries) != 1 || summaries[0].Name != "mcp_b" {
		t.Fatalf("expected only cold deferred tool in summaries, got %+v", summaries)
	}
}

func containsString(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

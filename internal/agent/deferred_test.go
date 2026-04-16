package agent

import (
	"context"
	"sort"
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

func TestExpandDeferredFamilyCore_LoadsBrowserCore(t *testing.T) {
	reg := NewToolRegistry()
	deferred := make(map[string]bool)
	for _, name := range FamilyRegistry["browser"].Core {
		reg.Register(&mockMCPTool{name: name})
		deferred[name] = true
	}
	reg.Register(&mockMCPTool{name: "mock_extra"})
	deferred["mock_extra"] = true

	expanded := expandDeferredFamilyCore(reg, deferred, []string{"browser_navigate"})

	if len(expanded) != len(FamilyRegistry["browser"].Core) {
		t.Fatalf("expected browser family core only, got %v", expanded)
	}
	expected := append([]string(nil), FamilyRegistry["browser"].Core...)
	sort.Strings(expected)
	for i, name := range expected {
		if expanded[i] != name {
			t.Fatalf("index %d: expected %q, got %q", i, name, expanded[i])
		}
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

func TestToolSearch_ReturnsToolReferenceBlocksAlongsideLegacyString(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(&mockMCPTool{name: "x_search"})
	reg.Register(&mockMCPTool{name: "web_fetch"})
	deferred := map[string]bool{"x_search": true, "web_fetch": true}

	ts := newToolSearchTool(reg, deferred)
	res, err := ts.Run(context.Background(), `{"query":"select:x_search,web_fetch"}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Legacy Content must still have the LOADED: header (non-breaking fallback)
	if !strings.HasPrefix(res.Content, "LOADED:") {
		t.Fatalf("expected LOADED: prefix in legacy Content, got %q", res.Content)
	}
	// New path: ContentBlocks populated with tool_reference blocks
	if len(res.ContentBlocks) != 2 {
		t.Fatalf("expected 2 tool_reference blocks, got %d: %+v", len(res.ContentBlocks), res.ContentBlocks)
	}
	names := map[string]bool{}
	for _, b := range res.ContentBlocks {
		if b.Type != "tool_reference" {
			t.Fatalf("wrong block type: %q", b.Type)
		}
		names[b.ToolName] = true
	}
	if !names["x_search"] || !names["web_fetch"] {
		t.Fatalf("missing expected tool_reference names: %v", names)
	}
}

func TestToolSearch_EmptyMatchNoBlocks(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(&mockMCPTool{name: "a"})
	deferred := map[string]bool{"a": true}
	ts := newToolSearchTool(reg, deferred)

	res, err := ts.Run(context.Background(), `{"query":"select:does_not_exist"}`)
	if err != nil {
		t.Fatal(err)
	}
	// Zero matches → zero blocks, legacy Content says no match
	if len(res.ContentBlocks) != 0 {
		t.Fatalf("expected no blocks on empty match, got %d", len(res.ContentBlocks))
	}
	if !strings.Contains(res.Content, "No matching") {
		t.Fatalf("expected 'No matching' in Content, got %q", res.Content)
	}
}

func TestModelSupportsToolRef(t *testing.T) {
	cases := []struct {
		model string
		want  bool
	}{
		{"claude-sonnet-4-5-20250929", true},
		{"claude-sonnet-4-20250514", true},
		{"claude-opus-4-6", true},
		{"claude-opus-4-5", true},
		{"claude-haiku-4-5-20251001", false}, // Haiku excluded per Anthropic docs
		{"claude-3-5-sonnet-20241022", false}, // Pre-4 excluded
		{"gpt-4o", false},
		{"llama3", false},
		{"", false},
	}
	for _, c := range cases {
		if got := modelSupportsToolRef(c.model); got != c.want {
			t.Errorf("modelSupportsToolRef(%q)=%v, want %v", c.model, got, c.want)
		}
	}
}

func TestHasAnyNonDeferred(t *testing.T) {
	all := []client.Tool{
		{Name: "a", DeferLoading: true},
		{Name: "b", DeferLoading: true},
	}
	if hasAnyNonDeferred(all) {
		t.Fatal("expected false when every tool is deferred")
	}
	mixed := []client.Tool{
		{Name: "a", DeferLoading: true},
		{Name: "b"},
	}
	if !hasAnyNonDeferred(mixed) {
		t.Fatal("expected true when at least one tool is non-deferred")
	}
}

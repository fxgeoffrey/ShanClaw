package agent

import (
	"testing"

	"github.com/Kocoro-lab/ShanClaw/internal/client"
)

func TestWorkingSet_AddAndContains(t *testing.T) {
	ws := NewWorkingSet()
	schema := client.Tool{Type: "function", Function: client.FunctionDef{Name: "browser_navigate"}}

	ws.Add("browser_navigate", schema)
	if !ws.Contains("browser_navigate") {
		t.Error("working set should contain added tool")
	}
	if ws.Contains("browser_click") {
		t.Error("working set should not contain unadded tool")
	}
}

func TestWorkingSet_Schemas(t *testing.T) {
	ws := NewWorkingSet()
	ws.Add("b_tool", client.Tool{Type: "function", Function: client.FunctionDef{Name: "b_tool"}})
	ws.Add("a_tool", client.Tool{Type: "function", Function: client.FunctionDef{Name: "a_tool"}})

	schemas := ws.Schemas()
	if len(schemas) != 2 {
		t.Fatalf("expected 2 schemas, got %d", len(schemas))
	}
	if _, ok := schemas["a_tool"]; !ok {
		t.Error("schemas copy should contain a_tool")
	}
	if _, ok := schemas["b_tool"]; !ok {
		t.Error("schemas copy should contain b_tool")
	}
}

func TestWorkingSet_Len(t *testing.T) {
	ws := NewWorkingSet()
	if ws.Len() != 0 {
		t.Error("empty working set should have length 0")
	}
	ws.Add("x", client.Tool{Type: "function", Function: client.FunctionDef{Name: "x"}})
	if ws.Len() != 1 {
		t.Errorf("expected length 1, got %d", ws.Len())
	}
	ws.Add("x", client.Tool{Type: "function", Function: client.FunctionDef{Name: "x"}})
	if ws.Len() != 1 {
		t.Errorf("expected length 1 after duplicate add, got %d", ws.Len())
	}
}

func TestWorkingSet_Invalidate(t *testing.T) {
	ws := NewWorkingSet()
	ws.Add("x", client.Tool{Type: "function", Function: client.FunctionDef{Name: "x"}})
	ws.Invalidate()
	if ws.Contains("x") {
		t.Error("invalidated working set should be empty")
	}
	if ws.Len() != 0 {
		t.Error("invalidated working set should have length 0")
	}
	if ws.Fingerprint() != "" {
		t.Error("invalidated working set should clear fingerprint")
	}
}

func TestWorkingSet_SyncToolsetInvalidatesOnFingerprintChange(t *testing.T) {
	reg1 := NewToolRegistry()
	reg1.Register(&mockTool{name: "bash"})
	reg1.Register(&mockMCPTool{name: "browser_click"})

	reg2 := NewToolRegistry()
	reg2.Register(&mockTool{name: "bash"})
	reg2.Register(&mockMCPTool{name: "browser_type"})

	ws := NewWorkingSet()
	if !ws.SyncToolset(reg1) {
		t.Error("first sync should establish fingerprint")
	}

	ws.Add("browser_click", buildToolSchema(&mockMCPTool{name: "browser_click"}))
	if !ws.Contains("browser_click") {
		t.Fatal("working set should contain warmed schema before fingerprint change")
	}

	if ws.SyncToolset(reg1) {
		t.Error("same toolset fingerprint should not invalidate working set")
	}
	if !ws.Contains("browser_click") {
		t.Fatal("working set should survive same fingerprint")
	}

	if !ws.SyncToolset(reg2) {
		t.Error("changed toolset fingerprint should invalidate working set")
	}
	if ws.Contains("browser_click") {
		t.Error("working set should clear stale warmed schemas on fingerprint change")
	}
}

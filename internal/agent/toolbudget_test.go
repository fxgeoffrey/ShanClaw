package agent

import (
	"fmt"
	"testing"
)

func TestEstimateSchemaTokens_Simple(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(&mockTool{name: "bash"})
	reg.Register(&mockTool{name: "grep"})

	tokens := estimateSchemaTokens(reg, reg.Names())
	if tokens <= 0 {
		t.Fatalf("expected positive token count, got %d", tokens)
	}
}

func TestEstimateSchemaTokens_Empty(t *testing.T) {
	reg := NewToolRegistry()
	tokens := estimateSchemaTokens(reg, nil)
	if tokens != 0 {
		t.Fatalf("expected 0 tokens for empty list, got %d", tokens)
	}
}

func TestShouldDefer_UnderBudget(t *testing.T) {
	reg := NewToolRegistry()
	for _, name := range []string{"a", "b", "c"} {
		reg.Register(&mockTool{name: name})
	}

	if shouldDefer(reg, reg.Names(), 50000) {
		t.Error("3 small tools should not trigger deferral under a large budget")
	}
}

func TestShouldDefer_OverBudget(t *testing.T) {
	reg := NewToolRegistry()
	for i := 0; i < 80; i++ {
		reg.Register(&mockMCPTool{name: fmt.Sprintf("tool_%03d", i)})
	}

	if !shouldDefer(reg, reg.Names(), 1000) {
		t.Error("80 verbose tools should trigger deferral under a small budget")
	}
}

func TestToolSchemaFingerprint_Deterministic(t *testing.T) {
	reg1 := NewToolRegistry()
	reg1.Register(&mockTool{name: "bash"})
	reg1.Register(&mockMCPTool{name: "browser_click"})

	reg2 := NewToolRegistry()
	reg2.Register(&mockMCPTool{name: "browser_click"})
	reg2.Register(&mockTool{name: "bash"})

	fp1 := toolSchemaFingerprint(reg1)
	fp2 := toolSchemaFingerprint(reg2)
	if fp1 == "" || fp2 == "" {
		t.Fatal("fingerprints should not be empty")
	}
	if fp1 != fp2 {
		t.Fatalf("fingerprints should be deterministic, got %q vs %q", fp1, fp2)
	}
}

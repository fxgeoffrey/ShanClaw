package cmd

import (
	"os"
	"testing"

	"github.com/Kocoro-lab/ShanClaw/internal/agents"
)

func TestResolveOneShotCWD_DefaultsToProcessCWD(t *testing.T) {
	got, err := resolveOneShotCWD(nil)
	if err != nil {
		t.Fatalf("resolveOneShotCWD error: %v", err)
	}
	want, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd error: %v", err)
	}
	if got != want {
		t.Fatalf("expected process cwd %q, got %q", want, got)
	}
}

func TestResolveOneShotCWD_UsesAgentConfig(t *testing.T) {
	want := t.TempDir()
	got, err := resolveOneShotCWD(&agents.Agent{
		Config: &agents.AgentConfig{CWD: want},
	})
	if err != nil {
		t.Fatalf("resolveOneShotCWD error: %v", err)
	}
	if got != want {
		t.Fatalf("expected agent CWD %q, got %q", want, got)
	}
}

func TestResolveOneShotCWD_RejectsInvalidAgentCWD(t *testing.T) {
	_, err := resolveOneShotCWD(&agents.Agent{
		Config: &agents.AgentConfig{CWD: "/nonexistent/path/for/one-shot"},
	})
	if err == nil {
		t.Fatal("expected invalid cwd error")
	}
}

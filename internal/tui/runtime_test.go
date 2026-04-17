package tui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/charmbracelet/bubbles/textarea"

	"github.com/Kocoro-lab/ShanClaw/internal/config"
	"github.com/Kocoro-lab/ShanClaw/internal/permissions"
	"github.com/Kocoro-lab/ShanClaw/internal/session"
	"github.com/Kocoro-lab/ShanClaw/internal/tools"
)

func TestBuildRuntimeCommands_ProjectOverridesGlobal(t *testing.T) {
	shannonDir := t.TempDir()
	projectDir := t.TempDir()

	if err := os.MkdirAll(filepath.Join(shannonDir, "commands"), 0755); err != nil {
		t.Fatalf("mkdir global commands: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(projectDir, "commands"), 0755); err != nil {
		t.Fatalf("mkdir project commands: %v", err)
	}
	if err := os.WriteFile(filepath.Join(shannonDir, "commands", "deploy.md"), []byte("global deploy"), 0644); err != nil {
		t.Fatalf("write global command: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "commands", "deploy.md"), []byte("project deploy"), 0644); err != nil {
		t.Fatalf("write project command: %v", err)
	}

	commands, slashCmds := buildRuntimeCommands(shannonDir, projectDir, nil)
	if commands["deploy"] != "project deploy" {
		t.Fatalf("expected project deploy command, got %q", commands["deploy"])
	}

	found := false
	for _, cmd := range slashCmds {
		if cmd.cmd == "/deploy" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected /deploy slash command")
	}
}

func TestApplyRuntimeContext_RefreshesConfigAndCommands(t *testing.T) {
	shannonDir := t.TempDir()
	projectDir := t.TempDir()

	if err := os.MkdirAll(filepath.Join(shannonDir, "commands"), 0755); err != nil {
		t.Fatalf("mkdir global commands: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(projectDir, "commands"), 0755); err != nil {
		t.Fatalf("mkdir project commands: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(projectDir, ".shannon"), 0755); err != nil {
		t.Fatalf("mkdir project config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "commands", "deploy.md"), []byte("project deploy"), 0644); err != nil {
		t.Fatalf("write project command: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, ".shannon", "config.yaml"), []byte("model_tier: low\ntools:\n  bash_max_output: 4096\npermissions:\n  allowed_commands:\n    - make test\n"), 0644); err != nil {
		t.Fatalf("write project config: %v", err)
	}

	baseCfg := &config.Config{
		Endpoint:  "https://global.example",
		ModelTier: "medium",
		Agent: config.AgentConfig{
			MaxIterations:  5,
			MaxTokens:      1000,
			ContextWindow:  8000,
			Thinking:       true,
			ThinkingMode:   "adaptive",
			ThinkingBudget: 1000,
		},
		Tools: config.ToolsConfig{
			BashMaxOutput:    30000,
			ResultTruncation: 1000,
			ArgsTruncation:   200,
		},
		Permissions: permissions.PermissionsConfig{
			AllowedCommands: []string{"git status"},
		},
		Sources: map[string]config.ConfigSource{},
	}

	reg, skillsPtr, cleanup := tools.RegisterLocalTools(baseCfg, nil)
	defer cleanup()

	sessions := session.NewManager(t.TempDir())
	sess := sessions.NewSession()
	sess.CWD = projectDir

	m := &Model{
		baseCfg:        baseCfg,
		cfg:            config.Clone(baseCfg),
		sessions:       sessions,
		toolRegistry:   reg,
		textarea:       textarea.New(),
		shannonDir:     shannonDir,
		skillsPtr:      skillsPtr,
		resumedSession: true,
		markdownCache:  map[string]string{},
	}

	gotCWD := m.applyRuntimeContext(sess)
	if gotCWD != projectDir {
		t.Fatalf("expected project cwd %q, got %q", projectDir, gotCWD)
	}
	if m.cfg.ModelTier != "low" {
		t.Fatalf("expected project model tier, got %q", m.cfg.ModelTier)
	}
	if m.customCommands["deploy"] != "project deploy" {
		t.Fatalf("expected project deploy command, got %q", m.customCommands["deploy"])
	}
	if m.agentLoop == nil {
		t.Fatal("expected agent loop to be rebuilt")
	}

	bashTool, ok := m.toolRegistry.Get("bash")
	if !ok {
		t.Fatal("expected bash tool")
	}
	bash, ok := bashTool.(*tools.BashTool)
	if !ok {
		t.Fatal("expected bash tool type")
	}
	if bash.MaxOutput != 4096 {
		t.Fatalf("expected bash max output 4096, got %d", bash.MaxOutput)
	}
	if len(bash.ExtraSafeCommands) != 2 || bash.ExtraSafeCommands[0] != "git status" || bash.ExtraSafeCommands[1] != "make test" {
		t.Fatalf("unexpected bash safe commands: %#v", bash.ExtraSafeCommands)
	}
}

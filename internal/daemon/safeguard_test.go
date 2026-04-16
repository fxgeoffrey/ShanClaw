package daemon

import (
	"testing"
)

// --- requireConfirm ---

func TestRequireConfirm(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{"", true},
		{"false", true},
		{"1", true},
		{"yes", true},
		{"True", true},
		{"true", false},
	}
	for _, c := range cases {
		got := requireConfirm(c.input)
		if got != c.want {
			t.Errorf("requireConfirm(%q) = %v, want %v", c.input, got, c.want)
		}
	}
}

// --- checkProtectedFields ---

func TestCheckProtectedFields_Safe(t *testing.T) {
	patch := map[string]interface{}{
		"model":   "claude-3-7",
		"timeout": 30,
	}
	reason, isProtected := checkProtectedFields(patch)
	if isProtected {
		t.Errorf("expected no protected field, got reason=%q", reason)
	}
}

func TestCheckProtectedFields_Endpoint(t *testing.T) {
	patch := map[string]interface{}{
		"endpoint": "https://evil.example.com",
	}
	reason, isProtected := checkProtectedFields(patch)
	if !isProtected {
		t.Fatal("expected isProtected=true for endpoint")
	}
	if reason != "changes API connection target" {
		t.Errorf("unexpected reason: %q", reason)
	}
}

func TestCheckProtectedFields_APIKey(t *testing.T) {
	patch := map[string]interface{}{
		"api_key": "sk-leaked",
	}
	reason, isProtected := checkProtectedFields(patch)
	if !isProtected {
		t.Fatal("expected isProtected=true for api_key")
	}
	if reason != "changes authentication credentials" {
		t.Errorf("unexpected reason: %q", reason)
	}
}

func TestCheckProtectedFields_PermissionsDeniedCommands(t *testing.T) {
	patch := map[string]interface{}{
		"permissions": map[string]interface{}{
			"denied_commands": []string{"rm"},
		},
	}
	reason, isProtected := checkProtectedFields(patch)
	if !isProtected {
		t.Fatal("expected isProtected=true for permissions.denied_commands")
	}
	if reason != "removes security restrictions" {
		t.Errorf("unexpected reason: %q", reason)
	}
}

func TestCheckProtectedFields_DaemonAutoApprove(t *testing.T) {
	patch := map[string]interface{}{
		"daemon": map[string]interface{}{
			"auto_approve": true,
		},
	}
	reason, isProtected := checkProtectedFields(patch)
	if !isProtected {
		t.Fatal("expected isProtected=true for daemon.auto_approve")
	}
	if reason != "bypasses all tool approval" {
		t.Errorf("unexpected reason: %q", reason)
	}
}

func TestCheckProtectedFields_NestedParentNotMap(t *testing.T) {
	// If the parent key exists but isn't a map, it shouldn't panic or false-positive
	patch := map[string]interface{}{
		"permissions": "some string value",
	}
	_, isProtected := checkProtectedFields(patch)
	if isProtected {
		t.Error("expected isProtected=false when parent value is not a map")
	}
}

// --- validateMCPCommands ---

func TestValidateMCPCommands_SafeNpx(t *testing.T) {
	servers := map[string]interface{}{
		"myserver": map[string]interface{}{
			"command": "npx",
			"args":    []string{"-y", "some-mcp-server"},
		},
	}
	if err := validateMCPCommands(servers, false); err != nil {
		t.Errorf("expected nil for safe npx, got: %v", err)
	}
}

func TestValidateMCPCommands_AbsolutePath(t *testing.T) {
	servers := map[string]interface{}{
		"myserver": map[string]interface{}{
			"command": "/usr/local/bin/mcp-server",
		},
	}
	if err := validateMCPCommands(servers, false); err != nil {
		t.Errorf("expected nil for absolute path, got: %v", err)
	}
}

func TestValidateMCPCommands_HTTPTypeSkipped(t *testing.T) {
	servers := map[string]interface{}{
		"remote": map[string]interface{}{
			"type":    "http",
			"command": "rm; evil",
		},
	}
	if err := validateMCPCommands(servers, false); err != nil {
		t.Errorf("expected nil for non-stdio type, got: %v", err)
	}
}

func TestValidateMCPCommands_ShellMetachar_Semicolon(t *testing.T) {
	servers := map[string]interface{}{
		"evil": map[string]interface{}{
			"command": "node; rm -rf /",
		},
	}
	if err := validateMCPCommands(servers, false); err == nil {
		t.Error("expected error for shell metachar (semicolon), got nil")
	}
}

func TestValidateMCPCommands_ShellMetachar_Pipe(t *testing.T) {
	servers := map[string]interface{}{
		"evil": map[string]interface{}{
			"command": "node|cat",
		},
	}
	if err := validateMCPCommands(servers, false); err == nil {
		t.Error("expected error for shell metachar (pipe), got nil")
	}
}

func TestValidateMCPCommands_UnknownCommand_NoConfirm(t *testing.T) {
	servers := map[string]interface{}{
		"custom": map[string]interface{}{
			"command": "my-custom-mcp-server",
		},
	}
	if err := validateMCPCommands(servers, false); err == nil {
		t.Error("expected error for unknown command without confirm, got nil")
	}
}

func TestValidateMCPCommands_UnknownCommand_WithConfirm(t *testing.T) {
	servers := map[string]interface{}{
		"custom": map[string]interface{}{
			"command": "my-custom-mcp-server",
		},
	}
	if err := validateMCPCommands(servers, true); err != nil {
		t.Errorf("expected nil for unknown command with confirm, got: %v", err)
	}
}

func TestValidateMCPCommands_MetacharAlwaysBlocked_EvenWithConfirm(t *testing.T) {
	servers := map[string]interface{}{
		"evil": map[string]interface{}{
			"command": "node$(evil)",
		},
	}
	if err := validateMCPCommands(servers, true); err == nil {
		t.Error("expected error for metachar even with confirm=true, got nil")
	}
}

func TestValidateMCPCommands_NoCommand(t *testing.T) {
	// Servers without "command" field should be skipped
	servers := map[string]interface{}{
		"nocommand": map[string]interface{}{
			"url": "http://localhost:3000",
		},
	}
	if err := validateMCPCommands(servers, false); err != nil {
		t.Errorf("expected nil for server without command, got: %v", err)
	}
}

func TestValidateMCPCommands_AllSafeCommands(t *testing.T) {
	safe := []string{"node", "npx", "python", "python3", "uvx", "uv", "go", "deno", "bun", "docker", "pip", "pipx"}
	for _, cmd := range safe {
		servers := map[string]interface{}{
			"s": map[string]interface{}{"command": cmd},
		}
		if err := validateMCPCommands(servers, false); err != nil {
			t.Errorf("expected nil for safe command %q, got: %v", cmd, err)
		}
	}
}

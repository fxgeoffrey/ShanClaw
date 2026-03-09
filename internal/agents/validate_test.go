package agents

import "testing"

func TestValidateCommandName(t *testing.T) {
	// Valid names
	for _, name := range []string{"review", "deploy", "my-cmd"} {
		if err := ValidateCommandName(name); err != nil {
			t.Errorf("expected valid: %q, got %v", name, err)
		}
	}
	// Built-in collision
	for _, name := range []string{"help", "quit", "copy", "search"} {
		if err := ValidateCommandName(name); err == nil {
			t.Errorf("expected error for built-in %q", name)
		}
	}
	// Invalid charset
	if err := ValidateCommandName("UPPER"); err == nil {
		t.Error("expected error for uppercase")
	}
}

func TestValidateToolsFilter(t *testing.T) {
	// nil is ok
	if err := ValidateToolsFilter(nil); err != nil {
		t.Errorf("nil should be valid: %v", err)
	}
	// allow only is ok
	if err := ValidateToolsFilter(&AgentToolsFilter{Allow: []string{"bash"}}); err != nil {
		t.Errorf("allow-only should be valid: %v", err)
	}
	// both set is error
	if err := ValidateToolsFilter(&AgentToolsFilter{Allow: []string{"a"}, Deny: []string{"b"}}); err == nil {
		t.Error("expected error when both allow and deny set")
	}
}

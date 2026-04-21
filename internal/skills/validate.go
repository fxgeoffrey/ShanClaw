package skills

import (
	"fmt"
	"regexp"
	"strings"
)

var skillNameRegex = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?$`)

// builtinCommands mirrors agents.BuiltinCommands to avoid an import cycle.
// Keep in sync with internal/agents/validate.go.
var builtinCommands = map[string]bool{
	"quit": true, "exit": true, "help": true, "clear": true,
	"sessions": true, "session": true, "model": true, "config": true,
	"setup": true, "update": true, "copy": true, "research": true,
	"swarm": true, "search": true,
}

func ValidateSkillName(name string) error {
	if name == "" {
		return fmt.Errorf("skill name is required")
	}
	if len(name) > 64 {
		return fmt.Errorf("skill name %q exceeds 64 characters", name)
	}
	if !skillNameRegex.MatchString(name) {
		return fmt.Errorf("skill name %q must contain only lowercase letters, numbers, and hyphens (no underscores), and must not start or end with a hyphen", name)
	}
	if strings.Contains(name, "--") {
		return fmt.Errorf("skill name %q must not contain consecutive hyphens", name)
	}
	if builtinCommands[name] {
		return fmt.Errorf("skill name %q conflicts with built-in slash command", name)
	}
	return nil
}

// validateFrontmatterName bounds the frontmatter.name field so it can't
// smuggle newlines or control chars into LLM-visible contexts (skill
// catalog, use_skill output, sticky reinjection). Unlike slugs, frontmatter
// name is a free-form display label and may contain uppercase / mixed
// case / CJK / spaces; we only reject hostile formatting.
func validateFrontmatterName(name string) error {
	if name == "" {
		return fmt.Errorf("skill name is required in frontmatter")
	}
	if len(name) > 100 {
		return fmt.Errorf("skill frontmatter name exceeds 100 characters")
	}
	for _, r := range name {
		// Reject ASCII control chars and the DEL character. U+0000–U+001F
		// and U+007F can break the "Available Skills" list formatting or
		// inject markers into <system-reminder> content.
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("skill frontmatter name contains a control character")
		}
	}
	return nil
}

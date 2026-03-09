package skills

// SkillType defines how a skill is executed.
type SkillType string

const (
	// SkillTypePrompt injects a prompt template into the conversation.
	SkillTypePrompt SkillType = "prompt"
)

// Skill is a composable capability loaded from an agent's skills/ directory.
// Currently only prompt-type skills are executed (as TUI slash commands).
type Skill struct {
	Name        string    `yaml:"name" json:"name"`
	Description string    `yaml:"description" json:"description"`
	Type        SkillType `yaml:"type" json:"type"`

	// For SkillTypePrompt
	Prompt string `yaml:"prompt,omitempty" json:"prompt,omitempty"`

	// Metadata
	Source string `yaml:"-" json:"-"` // which agent defined this
}

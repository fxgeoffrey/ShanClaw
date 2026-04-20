package skills

// SecretSpec describes a single secret (API key) that a skill requires.
type SecretSpec struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Required bool   `json:"required"`
}

// Skill is a composable capability loaded from a SKILL.md file.
// Follows the Anthropic Agent Skills spec (agentskills.io/specification).
type Skill struct {
	Name            string         `json:"name"`
	Description     string         `json:"description"`
	Prompt          string         `json:"prompt,omitempty"`
	License         string         `json:"license,omitempty"`
	Compatibility   string         `json:"compatibility,omitempty"`
	// Metadata uses `map[string]any` to preserve nested YAML structures
	// (ClawHub uses a structured `clawdbot` object). See skillFrontmatter
	// in loader.go for rationale.
	Metadata        map[string]any `json:"metadata,omitempty"`
	AllowedTools    []string       `json:"allowed_tools,omitempty"`
	// StickyInstructions, when true, opts the skill into a short
	// <system-reminder> reinjection on activation and on skill-filter
	// drift. Set via frontmatter `sticky-instructions: true`. Intended for
	// policy skills (e.g. kocoro) whose guidance must survive compaction.
	StickyInstructions bool   `json:"sticky_instructions,omitempty"`
	// StickySnippet is the RESOLVED reinjection body used at runtime. It
	// comes from StickySnippetOverride when set, else from the heuristic
	// body extractor, else from Description. Capped to 400 chars. Not
	// persisted — recomputed on every load.
	StickySnippet   string         `json:"-"`
	// StickySnippetOverride is the author-pinned frontmatter value
	// (`sticky-snippet:`). Separate from the resolved StickySnippet so the
	// save path can round-trip author intent without accidentally freezing
	// a heuristic choice into the SKILL.md file. Empty means "let the
	// heuristic pick".
	StickySnippetOverride string `json:"-"`
	Source          string         `json:"-"`
	InstallSource   string         `json:"-"`
	MarketplaceSlug string         `json:"-"`
	Dir             string         `json:"-"`
}

// SkillMeta is the lightweight representation for API responses (no body/prompt).
type SkillMeta struct {
	Name             string       `json:"name"`
	Description      string       `json:"description"`
	Source           string       `json:"source,omitempty"`
	InstallSource    string       `json:"install_source"`
	MarketplaceSlug  string       `json:"marketplace_slug,omitempty"`
	RequiredSecrets  []SecretSpec `json:"required_secrets,omitempty"`
	ConfiguredSecrets []string    `json:"configured_secrets,omitempty"`
}

// ToMeta returns API-safe metadata without the full prompt body.
func (s *Skill) ToMeta() SkillMeta {
	return SkillMeta{
		Name:            s.Name,
		Description:     s.Description,
		Source:          s.Source,
		InstallSource:   s.InstallSource,
		MarketplaceSlug: s.MarketplaceSlug,
	}
}

// RequiredSecrets parses requires.env from ClawHub metadata
// (metadata.openclaw.requires.env and metadata.clawdbot.requires.env).
// Returns nil if no secrets are declared or metadata is malformed.
func (s *Skill) RequiredSecrets() []SecretSpec {
	if len(s.Metadata) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var result []SecretSpec
	for _, parentKey := range []string{"openclaw", "clawdbot"} {
		envKeys := extractRequiresEnv(s.Metadata, parentKey)
		for _, key := range envKeys {
			if seen[key] {
				continue
			}
			seen[key] = true
			result = append(result, SecretSpec{
				Key:      key,
				Label:    key,
				Required: true,
			})
		}
	}
	return result
}

// extractRequiresEnv safely navigates metadata[parentKey].requires.env
// and returns the string slice. Returns nil on any type mismatch.
func extractRequiresEnv(metadata map[string]any, parentKey string) []string {
	parent, ok := metadata[parentKey].(map[string]any)
	if !ok {
		return nil
	}
	requires, ok := parent["requires"].(map[string]any)
	if !ok {
		return nil
	}
	envList, ok := requires["env"].([]any)
	if !ok {
		return nil
	}
	var result []string
	for _, v := range envList {
		if s, ok := v.(string); ok && s != "" {
			result = append(result, s)
		}
	}
	return result
}

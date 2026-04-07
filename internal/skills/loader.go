package skills

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Kocoro-lab/ShanClaw/internal/skills/bundled"
	"github.com/adrg/frontmatter"
	"gopkg.in/yaml.v3"
)

type SkillSource struct {
	Dir    string
	Source string
}

const (
	SourceGlobal  = "global"
	SourceBundled = "bundled"
)

func BundledSkillSource(shannonDir string) (SkillSource, error) {
	dir, err := bundled.ExtractBundledSkills(shannonDir)
	if err != nil {
		return SkillSource{}, err
	}
	return SkillSource{Dir: dir, Source: SourceBundled}, nil
}

type skillFrontmatter struct {
	Name string `yaml:"name"`
	// Slug, when present, is the canonical identifier (ClawHub convention:
	// `name` is a display label like "Docker", `slug` is the URL identifier
	// like "docker"). If empty, Name is used as the identity — that covers
	// all bundled/Anthropic skills where the two are equal.
	Slug          string         `yaml:"slug,omitempty"`
	Description   string         `yaml:"description"`
	License       string         `yaml:"license"`
	Compatibility string         `yaml:"compatibility"`
	// Metadata is intentionally `map[string]any` so nested YAML values
	// (ClawHub skills embed a structured `clawdbot` object with emoji,
	// required bins, etc.) round-trip through loadSkillMD without blowing
	// up unmarshal. A flat `map[string]string` would reject any non-string
	// value and surface as ErrInvalidSkillPayload / HTTP 422 "malformed"
	// — see the regression test in marketplace_test.go.
	Metadata     map[string]any `yaml:"metadata"`
	AllowedTools string         `yaml:"allowed-tools"`
}

// canonicalName returns the authoritative identity for a skill:
// frontmatter.slug if set, otherwise frontmatter.name. The two are equal for
// every bundled skill; ClawHub skills separate them.
func (fm *skillFrontmatter) canonicalName() string {
	if fm.Slug != "" {
		return fm.Slug
	}
	return fm.Name
}

func LoadSkills(sources ...SkillSource) ([]*Skill, error) {
	seen := make(map[string]bool)
	var result []*Skill

	for _, src := range sources {
		if _, err := os.Stat(src.Dir); os.IsNotExist(err) {
			continue
		}
		warnLegacyYAML(src.Dir)

		entries, err := os.ReadDir(src.Dir)
		if err != nil {
			continue
		}
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			if e.IsDir() {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)

		for _, name := range names {
			if seen[name] {
				continue
			}
			skillDir := filepath.Join(src.Dir, name)
			skillFile := filepath.Join(skillDir, "SKILL.md")
			if _, err := os.Stat(skillFile); os.IsNotExist(err) {
				continue
			}
			s, err := loadSkillMD(skillFile, name, src.Source)
			if err != nil {
				// Fail open per skill: a malformed SKILL.md must not block
				// every other skill in the same (or any other) source. Log a
				// warning that names the file path so the user can find and
				// fix it, then move on without marking `seen[name]` — that
				// way a valid lower-priority version of the same skill name
				// (e.g. bundled vs broken global) can still take over.
				log.Printf("WARNING: skipping skill %q (%s): %v", name, skillFile, err)
				continue
			}
			s.Dir = skillDir
			s.InstallSource, s.MarketplaceSlug = installProvenanceForSkill(src.Source, skillDir)
			seen[name] = true
			result = append(result, s)
		}
	}
	return result, nil
}

func loadSkillMD(path, dirName, source string) (*Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var fm skillFrontmatter
	body, err := frontmatter.Parse(bytes.NewReader(data), &fm, frontmatter.NewFormat("---", "---", yaml.Unmarshal))
	if err != nil {
		return nil, fmt.Errorf("parse frontmatter: %w", err)
	}
	if fm.Name == "" {
		return nil, fmt.Errorf("skill name is required in frontmatter")
	}
	// Use the canonical identity (slug when present, else name). This lets
	// ClawHub-style frontmatter — display `name: Docker`, URL `slug: docker`
	// — satisfy the directory-name check without forcing authors to lowercase
	// the display label.
	canonical := fm.canonicalName()
	if canonical != dirName {
		return nil, fmt.Errorf("skill %q must match directory name %q", canonical, dirName)
	}
	if err := ValidateSkillName(canonical); err != nil {
		return nil, err
	}
	if fm.Description == "" {
		return nil, fmt.Errorf("skill description is required")
	}
	var allowedTools []string
	if fm.AllowedTools != "" {
		allowedTools = strings.Fields(fm.AllowedTools)
	}
	return &Skill{
		Name:          canonical,
		Description:   fm.Description,
		Prompt:        strings.TrimSpace(string(body)),
		License:       fm.License,
		Compatibility: fm.Compatibility,
		Metadata:      fm.Metadata,
		AllowedTools:  allowedTools,
		Source:        source,
	}, nil
}

func warnLegacyYAML(dir string) {
	matches, _ := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if len(matches) > 0 {
		log.Printf("WARNING: Found legacy skills/*.yaml files in %s — migrate to SKILL.md format", dir)
	}
	matches, _ = filepath.Glob(filepath.Join(dir, "*.yml"))
	if len(matches) > 0 {
		log.Printf("WARNING: Found legacy skills/*.yml files in %s — migrate to SKILL.md format", dir)
	}
}

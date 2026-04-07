package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func createSkillDir(t *testing.T, base, name, content string) {
	t.Helper()
	dir := filepath.Join(base, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadSkills_BasicParsing(t *testing.T) {
	tmp := t.TempDir()
	createSkillDir(t, tmp, "pdf", "---\nname: pdf\ndescription: Extract text from PDFs\nlicense: MIT\n---\n\n# PDF Processing\n\nUse pypdf to extract text.\n")

	skills, err := LoadSkills(SkillSource{Dir: tmp, Source: "global"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	s := skills[0]
	if s.Name != "pdf" {
		t.Errorf("name = %q, want pdf", s.Name)
	}
	if s.Description != "Extract text from PDFs" {
		t.Errorf("description = %q", s.Description)
	}
	if s.License != "MIT" {
		t.Errorf("license = %q", s.License)
	}
	if !strings.Contains(s.Prompt, "# PDF Processing") {
		t.Errorf("prompt missing body")
	}
	if s.Source != "global" {
		t.Errorf("source = %q", s.Source)
	}
	if s.Dir != filepath.Join(tmp, "pdf") {
		t.Errorf("dir = %q", s.Dir)
	}
}

func TestLoadSkills_PriorityDedup(t *testing.T) {
	agentDir := t.TempDir()
	globalDir := t.TempDir()
	createSkillDir(t, agentDir, "pdf", "---\nname: pdf\ndescription: Agent PDF\n---\nAgent version.")
	createSkillDir(t, globalDir, "pdf", "---\nname: pdf\ndescription: Global PDF\n---\nGlobal version.")
	createSkillDir(t, globalDir, "xlsx", "---\nname: xlsx\ndescription: Spreadsheet\n---\nXLSX.")

	skills, err := LoadSkills(
		SkillSource{Dir: agentDir, Source: "agent:mybot"},
		SkillSource{Dir: globalDir, Source: "global"},
	)
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("expected 2, got %d", len(skills))
	}
	var pdf *Skill
	for _, s := range skills {
		if s.Name == "pdf" {
			pdf = s
		}
	}
	if pdf == nil {
		t.Fatal("pdf not found")
	}
	if pdf.Source != "agent:mybot" {
		t.Errorf("pdf source = %q", pdf.Source)
	}
	if !strings.Contains(pdf.Prompt, "Agent version") {
		t.Error("agent pdf should win")
	}
}

// TestLoadSkills_NameMismatch_IsSkippedNotErrored locks in fail-open
// behavior: a single broken skill must not abort the entire LoadSkills call.
// The skill is logged and dropped; the function returns no error and an empty
// (or partial) result list.
func TestLoadSkills_NameMismatch_IsSkippedNotErrored(t *testing.T) {
	tmp := t.TempDir()
	createSkillDir(t, tmp, "pdf", "---\nname: wrong-name\ndescription: Mismatch\n---\nBody.")
	skills, err := LoadSkills(SkillSource{Dir: tmp, Source: "global"})
	if err != nil {
		t.Fatalf("LoadSkills must not error on a single bad skill: %v", err)
	}
	if len(skills) != 0 {
		t.Errorf("expected the bad skill to be skipped, got %d skills", len(skills))
	}
}

// TestLoadSkills_FailOpenWithGoodAndBad guards the central fail-open contract:
// when one skill in a source is malformed (e.g. broken YAML frontmatter), the
// loader must skip it and still return every other valid skill in the same
// source. Regression target: a real user environment where one skill with a
// nested-map metadata block silently broke ALL global skill loading.
func TestLoadSkills_FailOpenWithGoodAndBad(t *testing.T) {
	tmp := t.TempDir()

	// Valid skill — must survive.
	createSkillDir(t, tmp, "pdf", "---\nname: pdf\ndescription: Read PDFs\n---\n# PDF body")

	// Broken skill — frontmatter parses fine but the description field is a
	// map instead of a string, mirroring the real-world failure observed in
	// the user's environment ("cannot unmarshal !!map into string"). This
	// MUST be skipped, not fatal.
	createSkillDir(t, tmp, "broken", "---\nname: broken\ndescription:\n  nested: oops\n---\n# body")

	// Another valid skill loaded after the broken one in alphabetical order
	// (sorted by directory name): "pdf" comes before "broken"? No — "broken"
	// sorts before "pdf". So this confirms the loader recovers and continues
	// past a mid-stream failure rather than aborting.
	createSkillDir(t, tmp, "zebra", "---\nname: zebra\ndescription: Stripes\n---\n# zebra body")

	skills, err := LoadSkills(SkillSource{Dir: tmp, Source: "global"})
	if err != nil {
		t.Fatalf("LoadSkills must not error when only some skills are bad: %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("expected 2 valid skills (pdf, zebra) — broken should be skipped — got %d", len(skills))
	}
	got := map[string]bool{}
	for _, s := range skills {
		got[s.Name] = true
	}
	if !got["pdf"] || !got["zebra"] {
		t.Errorf("expected both pdf and zebra to load, got %v", got)
	}
	if got["broken"] {
		t.Error("broken skill should have been skipped, but it was loaded")
	}
}

// TestLoadSkills_BrokenSkillDoesNotShadowLowerSource locks in the part of the
// fail-open fix that intentionally does NOT mark a broken skill as "seen".
// If a broken global skill shadowed a working bundled skill of the same name,
// users would be silently downgraded; instead, the bundled version must take
// over when the higher-priority source is malformed.
func TestLoadSkills_BrokenSkillDoesNotShadowLowerSource(t *testing.T) {
	highPrio := t.TempDir()
	lowPrio := t.TempDir()

	// Higher-priority source has a broken `pdf` skill (bad name mismatch).
	createSkillDir(t, highPrio, "pdf", "---\nname: not-pdf\ndescription: x\n---\n# x")

	// Lower-priority source has a valid `pdf` skill.
	createSkillDir(t, lowPrio, "pdf", "---\nname: pdf\ndescription: Real PDF\n---\n# Real PDF")

	skills, err := LoadSkills(
		SkillSource{Dir: highPrio, Source: "global"},
		SkillSource{Dir: lowPrio, Source: "bundled"},
	)
	if err != nil {
		t.Fatalf("LoadSkills returned error: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected the bundled pdf to take over, got %d skills", len(skills))
	}
	if skills[0].Name != "pdf" {
		t.Errorf("expected name=pdf, got %q", skills[0].Name)
	}
	if skills[0].Description != "Real PDF" {
		t.Errorf("expected the bundled (working) skill to win, got description=%q", skills[0].Description)
	}
	if skills[0].Source != "bundled" {
		t.Errorf("expected source=bundled (broken global was skipped), got %q", skills[0].Source)
	}
}

func TestLoadSkills_LegacyYAML(t *testing.T) {
	tmp := t.TempDir()
	os.WriteFile(filepath.Join(tmp, "old.yaml"), []byte("name: old"), 0o644)
	skills, err := LoadSkills(SkillSource{Dir: tmp, Source: "global"})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(skills) != 0 {
		t.Errorf("expected 0, got %d", len(skills))
	}
}

func TestLoadSkills_EmptyDir(t *testing.T) {
	tmp := t.TempDir()
	skills, err := LoadSkills(SkillSource{Dir: tmp, Source: "global"})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(skills) != 0 {
		t.Errorf("expected 0, got %d", len(skills))
	}
}

func TestLoadSkills_NonexistentDir(t *testing.T) {
	skills, err := LoadSkills(SkillSource{Dir: "/nonexistent", Source: "global"})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if skills != nil {
		t.Errorf("expected nil")
	}
}

func TestLoadSkills_Integration(t *testing.T) {
	agentDir := t.TempDir()
	globalDir := t.TempDir()

	// Agent skill shadows global
	createSkillDir(t, agentDir, "pdf", "---\nname: pdf\ndescription: Agent PDF\n---\n# Agent PDF Guide")
	// Global skills
	createSkillDir(t, globalDir, "pdf", "---\nname: pdf\ndescription: Global PDF\n---\n# Global PDF Guide")
	createSkillDir(t, globalDir, "xlsx", "---\nname: xlsx\ndescription: Spreadsheet processing\n---\n# XLSX Guide")

	loaded, err := LoadSkills(
		SkillSource{Dir: agentDir, Source: "agent:test"},
		SkillSource{Dir: globalDir, Source: "global"},
	)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 skills (deduped), got %d", len(loaded))
	}

	var pdf, xlsx *Skill
	for _, s := range loaded {
		switch s.Name {
		case "pdf":
			pdf = s
		case "xlsx":
			xlsx = s
		}
	}

	// Agent pdf shadows global
	if pdf == nil {
		t.Fatal("pdf not found")
	}
	if pdf.Source != "agent:test" {
		t.Errorf("pdf source = %q, want agent:test", pdf.Source)
	}
	if !strings.Contains(pdf.Prompt, "Agent PDF Guide") {
		t.Error("agent pdf should shadow global")
	}

	// Global xlsx loaded
	if xlsx == nil {
		t.Fatal("xlsx not found")
	}
	if xlsx.Source != "global" {
		t.Errorf("xlsx source = %q, want global", xlsx.Source)
	}

	// Sorted order
	if loaded[0].Name != "pdf" || loaded[1].Name != "xlsx" {
		t.Errorf("expected [pdf, xlsx], got [%s, %s]", loaded[0].Name, loaded[1].Name)
	}
}

func TestLoadSkills_Sorted(t *testing.T) {
	tmp := t.TempDir()
	createSkillDir(t, tmp, "zebra", "---\nname: zebra\ndescription: Z\n---\nZ")
	createSkillDir(t, tmp, "alpha", "---\nname: alpha\ndescription: A\n---\nA")
	skills, err := LoadSkills(SkillSource{Dir: tmp, Source: "global"})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	if len(skills) != 2 {
		t.Fatalf("expected 2, got %d", len(skills))
	}
	if skills[0].Name != "alpha" {
		t.Errorf("expected alpha first, got %s", skills[0].Name)
	}
}

func TestLoadSkills_InstallProvenance(t *testing.T) {
	globalDir := t.TempDir()
	bundledDir := t.TempDir()

	createSkillDir(t, globalDir, "local-skill", "---\nname: local-skill\ndescription: Local\n---\nlocal")
	createSkillDir(t, globalDir, "market-skill", "---\nname: market-skill\ndescription: Market\n---\nmarket")
	createSkillDir(t, bundledDir, "bundled-skill", "---\nname: bundled-skill\ndescription: Bundled\n---\nbundled")

	if err := writeMarketplaceProvenance(filepath.Join(globalDir, "market-skill"), "market-skill"); err != nil {
		t.Fatalf("write provenance: %v", err)
	}

	loaded, err := LoadSkills(
		SkillSource{Dir: globalDir, Source: SourceGlobal},
		SkillSource{Dir: bundledDir, Source: SourceBundled},
	)
	if err != nil {
		t.Fatalf("load error: %v", err)
	}

	got := make(map[string]*Skill, len(loaded))
	for _, skill := range loaded {
		got[skill.Name] = skill
	}

	if got["local-skill"].InstallSource != InstallSourceLocal {
		t.Errorf("local-skill install source = %q, want %q", got["local-skill"].InstallSource, InstallSourceLocal)
	}
	if got["local-skill"].MarketplaceSlug != "" {
		t.Errorf("local-skill marketplace slug = %q, want empty", got["local-skill"].MarketplaceSlug)
	}

	if got["market-skill"].InstallSource != InstallSourceMarketplace {
		t.Errorf("market-skill install source = %q, want %q", got["market-skill"].InstallSource, InstallSourceMarketplace)
	}
	if got["market-skill"].MarketplaceSlug != "market-skill" {
		t.Errorf("market-skill marketplace slug = %q, want market-skill", got["market-skill"].MarketplaceSlug)
	}

	if got["bundled-skill"].InstallSource != InstallSourceBundled {
		t.Errorf("bundled-skill install source = %q, want %q", got["bundled-skill"].InstallSource, InstallSourceBundled)
	}
}

func TestWriteGlobalSkillClearsMarketplaceProvenance(t *testing.T) {
	shannonDir := t.TempDir()
	skillDir := filepath.Join(shannonDir, "skills", "ontology")

	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := writeMarketplaceProvenance(skillDir, "ontology"); err != nil {
		t.Fatalf("write provenance: %v", err)
	}

	err := WriteGlobalSkill(shannonDir, &Skill{
		Name:        "ontology",
		Description: "Local replacement",
		Prompt:      "# local body",
	})
	if err != nil {
		t.Fatalf("WriteGlobalSkill: %v", err)
	}

	if _, err := os.Stat(filepath.Join(skillDir, marketplaceProvenanceFile)); !os.IsNotExist(err) {
		t.Fatalf("provenance marker should be removed, stat err = %v", err)
	}

	loaded, err := LoadSkills(SkillSource{Dir: filepath.Join(shannonDir, "skills"), Source: SourceGlobal})
	if err != nil {
		t.Fatalf("LoadSkills: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(loaded))
	}
	if loaded[0].InstallSource != InstallSourceLocal {
		t.Errorf("install source = %q, want %q", loaded[0].InstallSource, InstallSourceLocal)
	}
	if loaded[0].MarketplaceSlug != "" {
		t.Errorf("marketplace slug = %q, want empty", loaded[0].MarketplaceSlug)
	}
}

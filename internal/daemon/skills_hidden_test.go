package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/Kocoro-lab/ShanClaw/internal/skills"
)

// writeSkillFile is a tiny helper to drop a SKILL.md into <shannonDir>/skills/<slug>/.
func writeSkillFile(t *testing.T, shannonDir, slug, body string) {
	t.Helper()
	dir := filepath.Join(shannonDir, "skills", slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
}

// TestHandleListSkills_HiddenFiltering verifies that skills with
// frontmatter `hidden: true` are omitted from the default GET /skills
// response, but re-appear when `?include_hidden=true` is passed.
func TestHandleListSkills_HiddenFiltering(t *testing.T) {
	shannonDir := t.TempDir()
	writeSkillFile(t, shannonDir, "visible", "---\nname: visible\ndescription: Visible skill\n---\n# Body")
	writeSkillFile(t, shannonDir, "kocoro", "---\nname: kocoro\ndescription: Hidden policy skill\nhidden: true\n---\n# Body")

	s := &Server{deps: &ServerDeps{ShannonDir: shannonDir, AgentsDir: t.TempDir()}}

	// Default call — hidden skill should be filtered out.
	req := httptest.NewRequest("GET", "/skills", nil)
	rr := httptest.NewRecorder()
	s.handleListSkills(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("default list status = %d, body = %s", rr.Code, rr.Body.String())
	}
	slugs := decodeSkillSlugs(t, rr.Body.Bytes())
	if slices.Contains(slugs, "kocoro") {
		t.Errorf("default list unexpectedly contains hidden skill: %v", slugs)
	}
	if !slices.Contains(slugs, "visible") {
		t.Errorf("default list missing non-hidden skill: %v", slugs)
	}

	// With ?include_hidden=true — hidden skill should re-appear.
	req = httptest.NewRequest("GET", "/skills?include_hidden=true", nil)
	rr = httptest.NewRecorder()
	s.handleListSkills(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("include_hidden list status = %d, body = %s", rr.Code, rr.Body.String())
	}
	slugs = decodeSkillSlugs(t, rr.Body.Bytes())
	if !slices.Contains(slugs, "kocoro") {
		t.Errorf("include_hidden=true list missing hidden skill: %v", slugs)
	}
	if !slices.Contains(slugs, "visible") {
		t.Errorf("include_hidden=true list missing non-hidden skill: %v", slugs)
	}
}

// TestHandleGetSkill_HiddenStillFetchable guards against a future refactor
// accidentally filtering single-skill lookup by `hidden`. Hidden is a
// browse-list display filter only — callers that know the slug must still
// be able to read the skill detail (admin UIs, kocoro secrets management).
func TestHandleGetSkill_HiddenStillFetchable(t *testing.T) {
	shannonDir := t.TempDir()
	writeSkillFile(t, shannonDir, "kocoro", "---\nname: kocoro\ndescription: Hidden policy skill\nhidden: true\n---\n# Body")

	s := &Server{deps: &ServerDeps{ShannonDir: shannonDir, AgentsDir: t.TempDir()}}

	req := httptest.NewRequest("GET", "/skills/kocoro", nil)
	req.SetPathValue("name", "kocoro")
	rr := httptest.NewRecorder()
	s.handleGetSkill(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("hidden skill lookup status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var detail skills.SkillDetail
	if err := json.Unmarshal(rr.Body.Bytes(), &detail); err != nil {
		t.Fatalf("unmarshal detail: %v", err)
	}
	if detail.Slug != "kocoro" {
		t.Errorf("slug = %q, want kocoro", detail.Slug)
	}
	if !detail.Hidden {
		t.Error("detail.Hidden = false, want true — single-skill lookup must expose the hidden flag so admin/management UIs can see it")
	}
}

// decodeSkillSlugs returns the Slug of every skill in the list response.
// Decoding by Slug (not Name) keeps checks precise — `name` is a free-form
// display label that can diverge from the slug (e.g. CJK or uppercase
// display names), while Slug is the URL-safe on-disk identifier.
func decodeSkillSlugs(t *testing.T, raw []byte) []string {
	t.Helper()
	var body struct {
		Skills []skills.SkillMeta `json:"skills"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("unmarshal skills list: %v", err)
	}
	slugs := make([]string, 0, len(body.Skills))
	for _, s := range body.Skills {
		slugs = append(slugs, s.Slug)
	}
	return slugs
}


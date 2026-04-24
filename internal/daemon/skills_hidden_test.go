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
	names := decodeSkillNames(t, rr.Body.Bytes())
	if slices.Contains(names, "kocoro") {
		t.Errorf("default list unexpectedly contains hidden skill: %v", names)
	}
	if !slices.Contains(names, "visible") {
		t.Errorf("default list missing non-hidden skill: %v", names)
	}

	// With ?include_hidden=true — hidden skill should re-appear.
	req = httptest.NewRequest("GET", "/skills?include_hidden=true", nil)
	rr = httptest.NewRecorder()
	s.handleListSkills(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("include_hidden list status = %d, body = %s", rr.Code, rr.Body.String())
	}
	names = decodeSkillNames(t, rr.Body.Bytes())
	if !slices.Contains(names, "kocoro") {
		t.Errorf("include_hidden=true list missing hidden skill: %v", names)
	}
	if !slices.Contains(names, "visible") {
		t.Errorf("include_hidden=true list missing non-hidden skill: %v", names)
	}
}

func decodeSkillNames(t *testing.T, raw []byte) []string {
	t.Helper()
	var body struct {
		Skills []skills.SkillMeta `json:"skills"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("unmarshal skills list: %v", err)
	}
	names := make([]string, 0, len(body.Skills))
	for _, s := range body.Skills {
		names = append(names, s.Name)
	}
	return names
}


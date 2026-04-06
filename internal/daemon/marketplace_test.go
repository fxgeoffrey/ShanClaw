package daemon

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Kocoro-lab/ShanClaw/internal/agents"
	"github.com/Kocoro-lab/ShanClaw/internal/skills"
)

// daemonTestRunGit runs git inside dir for marketplace handler tests.
// Mirrors the unexported runGit helper in internal/skills/api.go.
func daemonTestRunGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd.Run()
}

// daemonTestWriteFile writes a file, creating parent dirs. Mirrors
// mustWrite from the skills package but without needing t *testing.T
// everywhere.
func daemonTestWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// newTestServerWithMarketplace constructs a minimal *Server suitable for
// exercising the marketplace handlers. It sets only the fields those
// handlers read (marketplace client, deps.ShannonDir, deps.AgentsDir).
func newTestServerWithMarketplace(t *testing.T, registryJSON string) (*Server, *httptest.Server) {
	t.Helper()
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(registryJSON))
	}))
	t.Cleanup(registry.Close)

	shannonDir := t.TempDir()
	agentsDir := t.TempDir()

	s := &Server{
		deps: &ServerDeps{
			ShannonDir: shannonDir,
			AgentsDir:  agentsDir,
		},
		marketplace: skills.NewMarketplaceClient(registry.URL, 1*time.Hour),
		slugLocks:   skills.NewSlugLocks(),
	}
	return s, registry
}

func TestHandleMarketplaceList(t *testing.T) {
	registryJSON := `{
		"version": 1,
		"skills": [
			{"slug":"alpha","name":"alpha","description":"first","author":"a","repo":"r","downloads":10},
			{"slug":"bravo","name":"bravo","description":"second","author":"b","repo":"r","downloads":100}
		]
	}`
	s, _ := newTestServerWithMarketplace(t, registryJSON)

	req := httptest.NewRequest("GET", "/skills/marketplace?page=1&size=20&sort=downloads", nil)
	rr := httptest.NewRecorder()
	s.handleMarketplaceList(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var body struct {
		Total  int                       `json:"total"`
		Page   int                       `json:"page"`
		Size   int                       `json:"size"`
		Skills []skills.MarketplaceEntry `json:"skills"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.Total != 2 {
		t.Errorf("Total = %d, want 2", body.Total)
	}
	if len(body.Skills) != 2 || body.Skills[0].Slug != "bravo" {
		t.Errorf("sort order wrong: %+v", body.Skills)
	}
}

func TestHandleMarketplaceDetail(t *testing.T) {
	registryJSON := `{
		"version":1,
		"skills":[{"slug":"demo","name":"demo","description":"d","author":"a","repo":"r","homepage":"https://example.com"}]
	}`
	s, _ := newTestServerWithMarketplace(t, registryJSON)

	req := httptest.NewRequest("GET", "/skills/marketplace/entry/demo", nil)
	req.SetPathValue("slug", "demo")
	rr := httptest.NewRecorder()
	s.handleMarketplaceDetail(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var body struct {
		Slug      string `json:"slug"`
		Homepage  string `json:"homepage"`
		Installed bool   `json:"installed"`
		Preview   string `json:"preview"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.Slug != "demo" || body.Homepage != "https://example.com" {
		t.Errorf("unexpected body: %+v", body)
	}
	if body.Installed {
		t.Errorf("expected Installed=false for uninstalled skill, got true")
	}
	if body.Preview != "" {
		t.Errorf("expected empty preview for uninstalled skill, got %q", body.Preview)
	}
}

func TestHandleMarketplaceDetailInstalledPreview(t *testing.T) {
	registryJSON := `{
		"version":1,
		"skills":[{"slug":"demo","name":"demo","description":"d","author":"a","repo":"r"}]
	}`
	s, _ := newTestServerWithMarketplace(t, registryJSON)

	// Simulate the skill already present on disk.
	skillDir := filepath.Join(s.deps.ShannonDir, "skills", "demo")
	if err := os.MkdirAll(skillDir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	const skillBody = "---\nname: demo\ndescription: d\n---\nOn-disk body text."
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillBody), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	req := httptest.NewRequest("GET", "/skills/marketplace/entry/demo", nil)
	req.SetPathValue("slug", "demo")
	rr := httptest.NewRecorder()
	s.handleMarketplaceDetail(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var body struct {
		Installed bool   `json:"installed"`
		Preview   string `json:"preview"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !body.Installed {
		t.Errorf("expected Installed=true, got false")
	}
	if body.Preview != skillBody {
		t.Errorf("preview mismatch:\n got %q\nwant %q", body.Preview, skillBody)
	}
}

func TestHandleMarketplaceDetailBlocksMalicious(t *testing.T) {
	registryJSON := `{
		"version":1,
		"skills":[{"slug":"evil","name":"evil","description":"d","author":"a","repo":"r",
			"security":{"virustotal":"malicious"}}]
	}`
	s, _ := newTestServerWithMarketplace(t, registryJSON)

	req := httptest.NewRequest("GET", "/skills/marketplace/entry/evil", nil)
	req.SetPathValue("slug", "evil")
	rr := httptest.NewRecorder()
	s.handleMarketplaceDetail(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rr.Code)
	}
}

func TestHandleMarketplaceDetailNotFound(t *testing.T) {
	s, _ := newTestServerWithMarketplace(t, `{"version":1,"skills":[]}`)

	req := httptest.NewRequest("GET", "/skills/marketplace/entry/nope", nil)
	req.SetPathValue("slug", "nope")
	rr := httptest.NewRecorder()
	s.handleMarketplaceDetail(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
	}
}

func TestHandleMarketplaceInstallSuccess(t *testing.T) {
	// Fixture git repo for file:// clone. Deliberately use a description
	// in the frontmatter that differs from the registry entry so we can
	// prove the response reflects on-disk truth, not synthesized registry
	// metadata.
	repo := t.TempDir()
	if err := daemonTestRunGit(repo, "init", "-q", "-b", "main"); err != nil {
		t.Fatalf("git init: %v", err)
	}
	const onDiskDesc = "On-disk authoritative description"
	daemonTestWriteFile(t, filepath.Join(repo, "SKILL.md"),
		"---\nname: demo\ndescription: "+onDiskDesc+"\n---\nbody")
	daemonTestRunGit(repo, "config", "user.email", "t@e.com")
	daemonTestRunGit(repo, "config", "user.name", "t")
	daemonTestRunGit(repo, "config", "commit.gpgsign", "false")
	daemonTestRunGit(repo, "add", ".")
	daemonTestRunGit(repo, "commit", "-q", "-m", "init")

	// Registry entry intentionally carries a different description so we
	// can detect drift between response body and on-disk SKILL.md.
	registryJSON := fmt.Sprintf(`{
		"version":1,
		"skills":[{"slug":"demo","name":"demo","description":"Registry description (should NOT appear in response)","author":"a","repo":"file://%s","ref":"main"}]
	}`, repo)
	s, _ := newTestServerWithMarketplace(t, registryJSON)

	req := httptest.NewRequest("POST", "/skills/marketplace/install/demo", nil)
	req.SetPathValue("slug", "demo")
	rr := httptest.NewRecorder()
	s.handleMarketplaceInstall(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	if _, err := os.Stat(filepath.Join(s.deps.ShannonDir, "skills", "demo", "SKILL.md")); err != nil {
		t.Errorf("installed file missing: %v", err)
	}

	// Response body must reflect on-disk truth, not the registry entry.
	var meta skills.SkillMeta
	if err := json.Unmarshal(rr.Body.Bytes(), &meta); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if meta.Name != "demo" {
		t.Errorf("meta.Name = %q, want demo", meta.Name)
	}
	if meta.Description != onDiskDesc {
		t.Errorf("meta.Description = %q, want %q (registry description leaked into response)",
			meta.Description, onDiskDesc)
	}
}

func TestHandleMarketplaceInstallMaliciousBlocked(t *testing.T) {
	registryJSON := `{
		"version":1,
		"skills":[{"slug":"evil","name":"evil","description":"d","author":"a","repo":"file:///x","security":{"virustotal":"malicious"}}]
	}`
	s, _ := newTestServerWithMarketplace(t, registryJSON)

	req := httptest.NewRequest("POST", "/skills/marketplace/install/evil", nil)
	req.SetPathValue("slug", "evil")
	rr := httptest.NewRecorder()
	s.handleMarketplaceInstall(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rr.Code)
	}
}

// TestHandleMarketplaceInstallUpstreamFailureMaps502 verifies that a
// failed git clone surfaces as 502 Bad Gateway (upstream problem), not
// 500 Internal Server Error (local problem). Uses a registry entry with
// a bogus file:// path that definitely does not exist.
func TestHandleMarketplaceInstallUpstreamFailureMaps502(t *testing.T) {
	registryJSON := `{
		"version":1,
		"skills":[{"slug":"demo","name":"demo","description":"d","author":"a","repo":"file:///nonexistent/path/to/repo","ref":"main"}]
	}`
	s, _ := newTestServerWithMarketplace(t, registryJSON)

	req := httptest.NewRequest("POST", "/skills/marketplace/install/demo", nil)
	req.SetPathValue("slug", "demo")
	rr := httptest.NewRecorder()
	s.handleMarketplaceInstall(rr, req)

	if rr.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (upstream clone failure)", rr.Code)
	}
}

func TestHandleMarketplaceInstallNotFound(t *testing.T) {
	s, _ := newTestServerWithMarketplace(t, `{"version":1,"skills":[]}`)

	req := httptest.NewRequest("POST", "/skills/marketplace/install/nope", nil)
	req.SetPathValue("slug", "nope")
	rr := httptest.NewRecorder()
	s.handleMarketplaceInstall(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
	}
}

func TestHandleSkillUsage(t *testing.T) {
	s, _ := newTestServerWithMarketplace(t, `{"version":1,"skills":[]}`)

	// Wire up two agents attaching the skill.
	if err := agents.SetAttachedSkills(s.deps.AgentsDir, "coder", []string{"demo"}); err != nil {
		t.Fatalf("SetAttachedSkills coder: %v", err)
	}
	if err := agents.SetAttachedSkills(s.deps.AgentsDir, "researcher", []string{"demo", "other"}); err != nil {
		t.Fatalf("SetAttachedSkills researcher: %v", err)
	}

	req := httptest.NewRequest("GET", "/skills/demo/usage", nil)
	req.SetPathValue("name", "demo")
	rr := httptest.NewRecorder()
	s.handleSkillUsage(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var body struct {
		Skill  string   `json:"skill"`
		Agents []string `json:"agents"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body.Skill != "demo" {
		t.Errorf("Skill = %q", body.Skill)
	}
	want := []string{"coder", "researcher"}
	if len(body.Agents) != 2 || body.Agents[0] != want[0] || body.Agents[1] != want[1] {
		t.Errorf("Agents = %v, want %v", body.Agents, want)
	}
}

func TestHandleSkillUsageEmpty(t *testing.T) {
	s, _ := newTestServerWithMarketplace(t, `{"version":1,"skills":[]}`)

	req := httptest.NewRequest("GET", "/skills/unused/usage", nil)
	req.SetPathValue("name", "unused")
	rr := httptest.NewRecorder()
	s.handleSkillUsage(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("status = %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `"agents":[]`) {
		t.Errorf("expected empty agents array in body, got: %s", rr.Body.String())
	}
}

func TestHandleMarketplaceListFiltersMalicious(t *testing.T) {
	registryJSON := `{
		"version":1,
		"skills":[
			{"slug":"good","name":"good","description":"d","author":"a","repo":"r"},
			{"slug":"evil","name":"evil","description":"d","author":"a","repo":"r",
			 "security":{"virustotal":"malicious"}}
		]
	}`
	s, _ := newTestServerWithMarketplace(t, registryJSON)

	req := httptest.NewRequest("GET", "/skills/marketplace", nil)
	rr := httptest.NewRecorder()
	s.handleMarketplaceList(rr, req)

	if !strings.Contains(rr.Body.String(), `"good"`) || strings.Contains(rr.Body.String(), `"evil"`) {
		t.Errorf("malicious entry should be excluded: %s", rr.Body.String())
	}
}

package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Kocoro-lab/ShanClaw/internal/skills"
)

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

	req := httptest.NewRequest("GET", "/skills/marketplace/demo", nil)
	req.SetPathValue("slug", "demo")
	rr := httptest.NewRecorder()
	s.handleMarketplaceDetail(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var got skills.MarketplaceEntry
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Slug != "demo" || got.Homepage != "https://example.com" {
		t.Errorf("unexpected body: %+v", got)
	}
}

func TestHandleMarketplaceDetailNotFound(t *testing.T) {
	s, _ := newTestServerWithMarketplace(t, `{"version":1,"skills":[]}`)

	req := httptest.NewRequest("GET", "/skills/marketplace/nope", nil)
	req.SetPathValue("slug", "nope")
	rr := httptest.NewRecorder()
	s.handleMarketplaceDetail(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rr.Code)
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

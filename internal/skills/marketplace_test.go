package skills

import (
	"encoding/json"
	"testing"
)

func TestRegistryIndexParse(t *testing.T) {
	raw := `{
		"version": 1,
		"updated_at": "2026-04-06T12:00:00Z",
		"skills": [
			{
				"slug": "self-improving-agent",
				"name": "self-improving-agent",
				"description": "Captures learnings",
				"author": "pskoett",
				"license": "MIT-0",
				"repo": "https://github.com/peterskoett/self-improving-agent",
				"repo_path": "",
				"ref": "main",
				"homepage": "https://clawhub.ai/pskoett/self-improving-agent",
				"downloads": 354000,
				"stars": 3000,
				"version": "3.0.13",
				"security": {
					"virustotal": "benign",
					"openclaw": "benign",
					"scanned_at": "2026-04-01T00:00:00Z"
				},
				"tags": ["productivity", "meta"]
			}
		]
	}`

	var idx RegistryIndex
	if err := json.Unmarshal([]byte(raw), &idx); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if idx.Version != 1 {
		t.Errorf("Version = %d, want 1", idx.Version)
	}
	if len(idx.Skills) != 1 {
		t.Fatalf("len(Skills) = %d, want 1", len(idx.Skills))
	}
	s := idx.Skills[0]
	if s.Slug != "self-improving-agent" {
		t.Errorf("Slug = %q", s.Slug)
	}
	if s.Downloads != 354000 {
		t.Errorf("Downloads = %d, want 354000", s.Downloads)
	}
	if s.Security.VirusTotal != "benign" {
		t.Errorf("Security.VirusTotal = %q, want benign", s.Security.VirusTotal)
	}
	if s.Ref != "main" {
		t.Errorf("Ref = %q, want main", s.Ref)
	}
}

func TestMarketplaceEntryIsMalicious(t *testing.T) {
	cases := []struct {
		name string
		e    MarketplaceEntry
		want bool
	}{
		{"clean", MarketplaceEntry{}, false},
		{"benign", MarketplaceEntry{Security: SecurityScan{VirusTotal: "benign", OpenClaw: "benign"}}, false},
		{"vt-malicious", MarketplaceEntry{Security: SecurityScan{VirusTotal: "malicious"}}, true},
		{"oc-malicious", MarketplaceEntry{Security: SecurityScan{OpenClaw: "malicious"}}, true},
		{"suspicious-only", MarketplaceEntry{Security: SecurityScan{VirusTotal: "suspicious"}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.e.IsMalicious(); got != tc.want {
				t.Errorf("IsMalicious = %v, want %v", got, tc.want)
			}
		})
	}
}

package skills

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
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

func TestMarketplaceClientFetchAndCache(t *testing.T) {
	var hits int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"version":1,"updated_at":"2026-04-06T00:00:00Z","skills":[{"slug":"demo","name":"demo","description":"d","author":"a","repo":"https://x/y"}]}`))
	}))
	defer ts.Close()

	client := NewMarketplaceClient(ts.URL, 1*time.Hour)
	idx, err := client.Load(context.Background())
	if err != nil {
		t.Fatalf("first Load: %v", err)
	}
	if len(idx.Skills) != 1 || idx.Skills[0].Slug != "demo" {
		t.Fatalf("unexpected index: %+v", idx)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("expected 1 hit, got %d", got)
	}

	// Second call within TTL should not hit the server.
	if _, err := client.Load(context.Background()); err != nil {
		t.Fatalf("second Load: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("expected still 1 hit after cached load, got %d", got)
	}
}

func TestMarketplaceClientStaleOnError(t *testing.T) {
	var fail int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.LoadInt32(&fail) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write([]byte(`{"version":1,"skills":[{"slug":"demo","name":"demo","description":"d","author":"a","repo":"r"}]}`))
	}))
	defer ts.Close()

	// Zero TTL so every call tries to refetch.
	client := NewMarketplaceClient(ts.URL, 0)
	if _, err := client.Load(context.Background()); err != nil {
		t.Fatalf("priming Load: %v", err)
	}

	atomic.StoreInt32(&fail, 1)
	idx, err := client.Load(context.Background())
	if err != nil {
		t.Fatalf("stale Load should succeed, got: %v", err)
	}
	if len(idx.Skills) != 1 {
		t.Errorf("expected 1 skill from stale cache, got %d", len(idx.Skills))
	}
	if !client.IsStale() {
		t.Errorf("expected IsStale() true after serving stale")
	}
}

func TestMarketplaceClientNoCacheNoServer(t *testing.T) {
	// Unreachable URL, no prior cache → must return error.
	client := NewMarketplaceClient("http://127.0.0.1:1/no-such", 1*time.Hour)
	_, err := client.Load(context.Background())
	if err == nil {
		t.Fatal("expected error with no cache and unreachable URL")
	}
}

func TestFilterSortPaginate(t *testing.T) {
	entries := []MarketplaceEntry{
		{Slug: "alpha", Name: "alpha", Description: "The first thing", Author: "alice", Downloads: 10, Stars: 5},
		{Slug: "bravo", Name: "bravo", Description: "Second thing", Author: "bob", Downloads: 100, Stars: 20},
		{Slug: "charlie", Name: "charlie", Description: "Third thing", Author: "alice", Downloads: 50, Stars: 15},
		{Slug: "delta", Name: "delta", Description: "Malicious", Author: "mallory", Downloads: 999,
			Security: SecurityScan{VirusTotal: "malicious"}},
	}

	// Default sort = downloads desc, malicious excluded.
	out, total := FilterSortPaginate(entries, "", "downloads", 1, 10)
	if total != 3 {
		t.Errorf("total = %d, want 3 (malicious excluded)", total)
	}
	if len(out) != 3 {
		t.Fatalf("len(out) = %d, want 3", len(out))
	}
	if out[0].Slug != "bravo" || out[1].Slug != "charlie" || out[2].Slug != "alpha" {
		t.Errorf("downloads sort order wrong: %v %v %v", out[0].Slug, out[1].Slug, out[2].Slug)
	}

	// Sort by name asc.
	out, _ = FilterSortPaginate(entries, "", "name", 1, 10)
	if out[0].Slug != "alpha" || out[2].Slug != "charlie" {
		t.Errorf("name sort order wrong: %v", sluggs(out))
	}

	// Search: matches name, description, author (case-insensitive).
	out, total = FilterSortPaginate(entries, "ALICE", "downloads", 1, 10)
	if total != 2 {
		t.Errorf("alice search total = %d, want 2", total)
	}
	out, total = FilterSortPaginate(entries, "third", "downloads", 1, 10)
	if total != 1 || out[0].Slug != "charlie" {
		t.Errorf("third search wrong: total=%d, %v", total, sluggs(out))
	}

	// Pagination: page 2 of size 2, downloads desc.
	out, total = FilterSortPaginate(entries, "", "downloads", 2, 2)
	if total != 3 {
		t.Errorf("page2 total = %d, want 3", total)
	}
	if len(out) != 1 || out[0].Slug != "alpha" {
		t.Errorf("page2 contents: %v", sluggs(out))
	}

	// Out-of-range page → empty slice, total still correct.
	out, total = FilterSortPaginate(entries, "", "downloads", 99, 10)
	if total != 3 {
		t.Errorf("OOR total = %d, want 3", total)
	}
	if len(out) != 0 {
		t.Errorf("OOR expected empty slice, got %v", sluggs(out))
	}
}

func sluggs(es []MarketplaceEntry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Slug
	}
	return out
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

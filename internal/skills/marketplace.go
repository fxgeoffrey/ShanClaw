package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// RegistryIndex is the top-level JSON document served by the marketplace
// registry repo. Field names match the schema in
// docs/superpowers/specs/2026-04-06-skill-marketplace-design.md.
type RegistryIndex struct {
	Version   int                `json:"version"`
	UpdatedAt string             `json:"updated_at"`
	Skills    []MarketplaceEntry `json:"skills"`
}

// MarketplaceEntry is one skill listing in the registry.
type MarketplaceEntry struct {
	Slug        string       `json:"slug"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Author      string       `json:"author"`
	License     string       `json:"license,omitempty"`
	Repo        string       `json:"repo"`
	RepoPath    string       `json:"repo_path,omitempty"`
	Ref         string       `json:"ref,omitempty"`
	Homepage    string       `json:"homepage,omitempty"`
	Downloads   int          `json:"downloads,omitempty"`
	Stars       int          `json:"stars,omitempty"`
	Version     string       `json:"version,omitempty"`
	Security    SecurityScan `json:"security,omitempty"`
	Tags        []string     `json:"tags,omitempty"`
}

// SecurityScan mirrors the scan results published by ClawHub.
// Empty strings mean "not scanned" and render as a neutral badge.
type SecurityScan struct {
	VirusTotal string `json:"virustotal,omitempty"`
	OpenClaw   string `json:"openclaw,omitempty"`
	ScannedAt  string `json:"scanned_at,omitempty"`
}

// IsMalicious returns true when any scanner flagged the entry as malicious.
// Used as a server-side gate in both the list and install endpoints.
func (e MarketplaceEntry) IsMalicious() bool {
	return e.Security.VirusTotal == "malicious" || e.Security.OpenClaw == "malicious"
}

// MarketplaceClient fetches and caches the registry index.
//
// Caching rules (see design doc §Registry Cache):
//   - First fetch populates the in-memory cache.
//   - Subsequent calls within TTL return the cached copy.
//   - After TTL expires, the next call refetches; on fetch failure the
//     previous cache is served as stale (IsStale() returns true).
//   - If no cache exists and fetch fails, Load returns the error.
type MarketplaceClient struct {
	url  string
	ttl  time.Duration
	http *http.Client

	mu      sync.Mutex
	cache   *RegistryIndex
	fetched time.Time
	stale   bool
}

// NewMarketplaceClient constructs a client with the given registry URL and
// cache TTL. A TTL of 0 forces every call to refetch (used by stale-on-error
// tests and by operators who explicitly disable caching).
func NewMarketplaceClient(url string, ttl time.Duration) *MarketplaceClient {
	return &MarketplaceClient{
		url:  url,
		ttl:  ttl,
		http: &http.Client{Timeout: 15 * time.Second},
	}
}

// Load returns the current registry index, refetching when the cache is
// empty or past TTL. See the type doc for stale-on-error semantics.
func (c *MarketplaceClient) Load(ctx context.Context) (*RegistryIndex, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cache != nil && time.Since(c.fetched) < c.ttl {
		c.stale = false
		return c.cache, nil
	}

	idx, err := c.fetch(ctx)
	if err != nil {
		if c.cache != nil {
			c.stale = true
			return c.cache, nil
		}
		return nil, err
	}

	c.cache = idx
	c.fetched = time.Now()
	c.stale = false
	return c.cache, nil
}

// IsStale reports whether the most recent Load served a stale cache because
// the upstream fetch failed. Handlers use this to set an X-Cache-Stale header.
func (c *MarketplaceClient) IsStale() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stale
}

// SlugLocks is a map of per-slug mutexes. The outer mutex protects map access;
// each per-slug lock serializes install/uninstall/usage-check operations for
// that slug only. Different slugs never block each other.
//
// Usage:
//
//	unlock := locks.Lock("my-skill")
//	defer unlock()
type SlugLocks struct {
	outer   sync.Mutex
	perSlug map[string]*sync.Mutex
}

// NewSlugLocks creates an empty SlugLocks.
func NewSlugLocks() *SlugLocks {
	return &SlugLocks{perSlug: make(map[string]*sync.Mutex)}
}

// Lock acquires the per-slug mutex and returns a function that releases it.
// The returned function is safe to call exactly once (typically via defer).
func (l *SlugLocks) Lock(slug string) func() {
	l.outer.Lock()
	m, ok := l.perSlug[slug]
	if !ok {
		m = &sync.Mutex{}
		l.perSlug[slug] = m
	}
	l.outer.Unlock()

	m.Lock()
	return m.Unlock
}

// FilterSortPaginate applies the marketplace list pipeline to a raw index slice.
// Returns the page slice plus the total count of entries that matched the
// filter (used by the API response for client-side pagination controls).
//
// Pipeline:
//  1. Drop malicious entries (server-side security gate).
//  2. Apply case-insensitive substring search against name+description+author.
//  3. Sort by the requested key (downloads|stars|name); unknown keys fall
//     back to downloads desc.
//  4. Slice to the requested page. Out-of-range pages return an empty slice.
//
// Sort keys:
//   - "downloads" (default): descending by Downloads, ties broken by name asc
//   - "stars":               descending by Stars, ties broken by name asc
//   - "name":                ascending by Name
func FilterSortPaginate(entries []MarketplaceEntry, query, sortKey string, page, size int) ([]MarketplaceEntry, int) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 100 {
		size = 100
	}

	// Step 1+2: filter.
	q := strings.ToLower(strings.TrimSpace(query))
	filtered := make([]MarketplaceEntry, 0, len(entries))
	for _, e := range entries {
		if e.IsMalicious() {
			continue
		}
		if q != "" {
			hay := strings.ToLower(e.Name + " " + e.Description + " " + e.Author)
			if !strings.Contains(hay, q) {
				continue
			}
		}
		filtered = append(filtered, e)
	}

	// Step 3: sort.
	switch sortKey {
	case "name":
		sort.SliceStable(filtered, func(i, j int) bool {
			return filtered[i].Name < filtered[j].Name
		})
	case "stars":
		sort.SliceStable(filtered, func(i, j int) bool {
			if filtered[i].Stars != filtered[j].Stars {
				return filtered[i].Stars > filtered[j].Stars
			}
			return filtered[i].Name < filtered[j].Name
		})
	default: // "downloads" and unknown
		sort.SliceStable(filtered, func(i, j int) bool {
			if filtered[i].Downloads != filtered[j].Downloads {
				return filtered[i].Downloads > filtered[j].Downloads
			}
			return filtered[i].Name < filtered[j].Name
		})
	}

	total := len(filtered)

	// Step 4: paginate.
	start := (page - 1) * size
	if start >= total {
		return []MarketplaceEntry{}, total
	}
	end := start + size
	if end > total {
		end = total
	}
	return filtered[start:end], total
}

func (c *MarketplaceClient) fetch(ctx context.Context) (*RegistryIndex, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch registry: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch registry: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024)) // 10 MB cap
	if err != nil {
		return nil, fmt.Errorf("read registry body: %w", err)
	}
	var idx RegistryIndex
	if err := json.Unmarshal(body, &idx); err != nil {
		return nil, fmt.Errorf("parse registry: %w", err)
	}
	return &idx, nil
}

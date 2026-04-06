package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
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

// stageCleanPayload walks src and copies every regular file into dst,
// excluding git metadata (.git/, .github/, .gitignore, .gitattributes) at
// any depth. Symlinks are rejected unconditionally: if the walk encounters
// one, the function removes dst (cleaning up any partial copy) and returns
// a 422-worthy error. See design doc §Install flow step 9.
//
// Exclusions match on the base name of any path segment, so nested .git dirs
// are also skipped.
//
// File modes are preserved from the source (masked to 0755 to strip any
// setuid/setgid/sticky bits), so shipped helper scripts keep their
// executable bit — this matters for community skills like
// self-improving-agent that ship scripts/activator.sh.
func stageCleanPayload(src, dst string) error {
	excluded := map[string]bool{
		".git":           true,
		".github":        true,
		".gitignore":     true,
		".gitattributes": true,
	}

	if err := os.MkdirAll(dst, 0700); err != nil {
		return fmt.Errorf("create stage dir: %w", err)
	}

	walkErr := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == src {
			return nil
		}

		// Reject symlinks outright. WalkDir gives us the lstat'd entry via
		// d.Type(), which preserves the Symlink bit.
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("unsupported symlink in skill payload: %s", path)
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		// Exclude if any path segment matches.
		for _, seg := range strings.Split(rel, string(filepath.Separator)) {
			if excluded[seg] {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		destPath := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(destPath, 0700)
		}

		// Preserve source file mode so shipped helper scripts keep their
		// executable bit. Mask to 0755 so no file can become setuid/setgid/
		// sticky via install, and ensure owner-read is always set.
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		srcMode := info.Mode().Perm() & 0755
		if srcMode&0400 == 0 {
			srcMode |= 0400
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(destPath, content, srcMode); err != nil {
			return err
		}
		// os.WriteFile respects the umask on some platforms; chmod to
		// guarantee the requested mode lands on disk.
		return os.Chmod(destPath, srcMode)
	})

	if walkErr != nil {
		os.RemoveAll(dst)
		return walkErr
	}
	return nil
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

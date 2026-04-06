package skills

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

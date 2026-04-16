package daemon

import (
	"fmt"
	"path/filepath"
	"regexp"
)

// requireConfirm returns true if the ?confirm=true query parameter is missing.
func requireConfirm(confirmParam string) bool {
	return confirmParam != "true"
}

// protectedFields maps a top-level config key to a human-readable reason.
var protectedFields = map[string]string{
	"endpoint": "changes API connection target",
	"api_key":  "changes authentication credentials",
}

// protectedNestedFields maps [parent, child] to a reason.
var protectedNestedFields = map[[2]string]string{
	{"permissions", "denied_commands"}: "removes security restrictions",
	{"daemon", "auto_approve"}:        "bypasses all tool approval",
}

// checkProtectedFields inspects a config patch for protected fields.
// Returns (reason, true) if a protected field is being modified.
func checkProtectedFields(patch map[string]interface{}) (string, bool) {
	for key, reason := range protectedFields {
		if _, ok := patch[key]; ok {
			return reason, true
		}
	}
	for pair, reason := range protectedNestedFields {
		parent, child := pair[0], pair[1]
		parentVal, ok := patch[parent]
		if !ok {
			continue
		}
		parentMap, ok := parentVal.(map[string]interface{})
		if !ok {
			continue
		}
		if _, ok := parentMap[child]; ok {
			return reason, true
		}
	}
	return "", false
}

// safeCommands is the allowlist of known-safe MCP server commands.
var safeCommands = map[string]bool{
	"node": true, "npx": true, "python": true, "python3": true,
	"uvx": true, "uv": true, "go": true, "deno": true, "bun": true,
	"docker": true, "pip": true, "pipx": true,
}

// shellMetachars matches characters that indicate shell injection.
var shellMetachars = regexp.MustCompile(`[;|&><$()\x60]`) // backtick = \x60

// validateMCPCommands checks that MCP server commands are safe.
// confirmed=true relaxes the unknown-command check but NOT the metachar check.
func validateMCPCommands(servers map[string]interface{}, confirmed bool) error {
	for name, srvRaw := range servers {
		srvMap, ok := srvRaw.(map[string]interface{})
		if !ok {
			continue
		}
		// Skip non-stdio servers
		if t, ok := srvMap["type"].(string); ok && t != "stdio" && t != "" {
			continue
		}
		cmdRaw, ok := srvMap["command"]
		if !ok {
			continue
		}
		cmd, ok := cmdRaw.(string)
		if !ok {
			continue
		}

		// Always block shell metacharacters
		if shellMetachars.MatchString(cmd) {
			return fmt.Errorf("mcp_servers.%s: command %q contains shell metacharacters", name, cmd)
		}

		// Allow absolute paths and known-safe commands
		if filepath.IsAbs(cmd) {
			continue
		}
		base := filepath.Base(cmd)
		if safeCommands[base] {
			continue
		}

		// Unknown command — block unless confirmed
		if !confirmed {
			return fmt.Errorf("mcp_servers.%s: unknown command %q — add X-Confirm header to proceed", name, cmd)
		}
	}
	return nil
}

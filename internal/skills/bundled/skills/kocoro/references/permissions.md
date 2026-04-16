# Permissions

## What is this?

Permissions control what commands and tools agents are allowed to run. Before executing anything, Shannon checks a chain of rules: hard-blocked commands are always rejected, then denied commands, then allowed commands, then a set of safe defaults, and finally anything unrecognized requires explicit approval from the user. This protects against agents accidentally running destructive or sensitive commands.

**Evaluation order:**
1. **Hard-blocks** — always denied, cannot be overridden (e.g., `rm -rf /`)
2. **Denied commands** — configured blocklist, never run
3. **Allowed commands** — configured allowlist, run without asking
4. **Default safe** — commands Shannon considers low-risk by default
5. **Ask user** — anything else requires approval before running

## API Endpoints

### View current permission config
- Method: GET
- Path: /config
- Response includes: `{"permissions": {"allowed_commands": ["git *", "npm test"], "denied_commands": ["rm -rf *"]}}`
- Notes: Use this to see the full permission configuration.

### Add allowed commands
- Method: PATCH
- Path: /config
- Body: `{"permissions": {"allowed_commands": ["git *", "npm run *"]}}`
- Response: `{"status": "updated"}`
- Notes: List is merged (deduplicated), not replaced. Patterns support `*` as a wildcard.

### Check macOS system permissions (TCC)
- Method: GET
- Path: /permissions
- Response: `{"screen_recording": "granted", "accessibility": "granted", "camera": "denied", "microphone": "not_determined"}`
- Notes: Shows macOS system-level permissions for Shannon (screen recording, accessibility, etc.).

### Request macOS system permissions
- Method: POST
- Path: /permissions/request
- Body: `{"permission": "screen_recording"}`
- Response: `{"permission": "string", "status": "requested"}`
- Notes: Triggers the macOS system permission dialog for the user to approve.

## Common Scenarios

### "Allow git commands"
1. PATCH /config with `{"permissions": {"allowed_commands": ["git *"]}}`
2. Agents can now run any `git` command without asking for approval.
3. To be more specific: `"git status"`, `"git log *"`, `"git diff *"` (only those exact patterns)

### "Allow npm scripts"
1. PATCH /config with `{"permissions": {"allowed_commands": ["npm run *", "npm test"]}}`

### "Check if screen recording is enabled (for screenshot tool)"
1. GET /permissions → look at `screen_recording` field
2. If `"denied"` or `"not_determined"`: POST /permissions/request with `{"permission": "screen_recording"}`
3. A macOS dialog will appear — click Allow.

### "See what commands are currently blocked"
1. GET /config → look at `permissions.denied_commands`

### "Check why an agent can't run a command"
1. GET /config → review `permissions.allowed_commands` and `permissions.denied_commands`
2. The command may need to be added to `allowed_commands`, or it may be in `denied_commands` (which cannot be removed via API).

## Safety Notes

- **NEVER modify `denied_commands`**: The `denied_commands` list is a security boundary. It is intentionally not modifiable through the standard API. If you need a command that's in the denied list, reconsider the approach — these blocks exist for good reason.
- **Allowed commands widen the attack surface**: Every command you add to `allowed_commands` is one the agent can run without confirmation. Use specific patterns (`git status`) rather than broad ones (`*`) where possible.
- **Wildcard patterns**: `*` matches any text. `git *` allows all git subcommands. `npm *` allows all npm commands including `npm publish` — be specific.
- **Reload needed**: After changing permissions via PATCH /config, call POST /config/reload to ensure the daemon picks up the new settings.
- **macOS permissions are system-level**: Screen recording, accessibility, and microphone permissions are managed by macOS and can only be granted by the user via system dialogs. Shannon cannot grant these programmatically.

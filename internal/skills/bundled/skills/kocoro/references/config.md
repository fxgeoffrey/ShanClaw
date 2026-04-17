# Config

## What is this?

Global settings control how Shannon behaves across all agents — which AI model to use, how to connect to the AI service, how long tools are allowed to run, and whether tools need approval before running. Settings are layered: global config, project config, and local config, with later layers overriding earlier ones.

## API Endpoints

### Get current config
- Method: GET
- Path: /config
- Response: `{"global": {...}, "effective": {...}, "sources": {"provider": "global", "endpoint": "global"}}`
- Notes: `effective` is the merged result. `sources` shows which config file each setting came from.

### Update config (deep merge)
- Method: PATCH
- Path: /config
- Body: `{"agent": {"model": "claude-opus-4-5"}}`
- Response: `{"status": "updated"}`
- Notes: PATCH merges deeply — you only need to include the fields you want to change. Protected fields (`endpoint`, `api_key`, `daemon.auto_approve`, `permissions.denied_commands`) return HTTP 409 and cannot be changed through this API.

### Reload config from disk
- Method: POST
- Path: /config/reload
- Response: `{"status": "reloaded"}`
- Notes: Picks up changes made directly to config files on disk. Also reconnects MCP servers.

### Get config status
- Method: GET
- Path: /config/status
- Response: `{"mcp_servers": {"slack": "connected"|"enabled"|"disabled"}}`
- Notes: Shows live connection status for MCP servers and provider health.

## Key Config Fields

| Field | Description | Protected |
|-------|-------------|-----------|
| `provider` | LLM backend: `""` (Shannon Cloud/Gateway) or `"ollama"` | No |
| `endpoint` | Shannon Cloud or custom gateway URL | YES |
| `api_key` | API key for the configured provider | YES |
| `agent.model` | Default model for all agents (e.g., `claude-sonnet-4-5`) | No |
| `agent.temperature` | Creativity level 0.0–1.0. Lower = more predictable. | No |
| `agent.max_iterations` | Max tool-use rounds per conversation turn | No |
| `agent.skill_discovery` | Enable small-model skill matching on first turn (default: true) | No |
| `tools.bash_timeout` | Max seconds a bash command can run (default: 120) | No |
| `daemon.auto_approve` | Skip approval prompts for all tool calls | YES |
| `mcp_servers` | External service integrations (see mcp reference) | No |

## Common Scenarios

### "Change the AI model"
1. PATCH /config with `{"agent": {"model": "claude-opus-4-5"}}`
2. POST /config/reload (optional — model is picked up on next conversation)
3. Verify: GET /config → check `effective.agent.model`

### "Increase bash command timeout"
1. PATCH /config with `{"tools": {"bash_timeout": 300}}`
2. Bash commands can now run up to 5 minutes before timing out.

### "Check which model is being used"
1. GET /config → look at `effective.agent.model`
2. `sources.agent.model` shows whether it came from global, project, or local config.

## Safety Notes

- **Protected fields**: `endpoint`, `api_key`, and `daemon.auto_approve` are protected. Attempting to modify them returns HTTP 409. These fields cannot be changed through this skill — the user must edit `~/.shannon/config.yaml` directly.
- **Three config levels**: Changes via PATCH /config write to the global config (`~/.shannon/config.yaml`). Project-level settings (`.shannon/config.yaml`) override global settings for that project. Local settings (`.shannon/config.local.yaml`) override both.
- **Reload after file edits**: If you edit config files directly on disk, call POST /config/reload so the daemon picks up the changes.
- **Model names**: Use exact model IDs from your provider. Invalid model names will cause conversations to fail at the start.

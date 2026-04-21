# Skills

## What is this?

Skills are knowledge packages that teach agents specific abilities — like reading PDFs, writing presentations, or analyzing spreadsheets. A skill contains instructions, example workflows, and sometimes custom tools that the agent can use. You install a skill once, then attach it to any agent that needs it.

## API Endpoints

### List installed skills
- Method: GET
- Path: /skills
- Response: `{"skills": [{"name": "...", "slug": "...", "description": "...", "source": "global", "install_source": "...", "required_secrets": [...], "configured_secrets": [...]}]}`
- Notes: Shows all skills currently installed in your Shannon instance. Use `slug` (the on-disk / URL-safe identifier) for all subsequent CRUD calls; `name` is a free-form display label that may contain uppercase letters or CJK characters and is not guaranteed to match the slug.

### List downloadable skills (bundled)
- Method: GET
- Path: /skills/downloadable
- Response: `{"skills": [{"name": "...", "description": "...", "installed": false}]}`
- Notes: Skills that ship with Shannon and can be installed with one call.

### List marketplace skills
- Method: GET
- Path: /skills/marketplace
- Response: `[{"slug": "string", "name": "string", "description": "string", "author": "string"}]`
- Notes: Community-contributed skills from the Shannon marketplace.

### Install a bundled skill
- Method: POST
- Path: /skills/install/{name}
- Response: `{"name": "...", "slug": "...", "description": "...", "install_source": "..."}`
- Notes: Installs from bundled (downloadable) skills. The `{name}` path segment is the skill's slug (always lowercase + hyphens).

### Install a marketplace skill
- Method: POST
- Path: /skills/marketplace/install/{slug}
- Response: `{"slug": "string", "name": "string", "description": "string", "install_source": "marketplace"}`
- Notes: Downloads and installs from the marketplace. Use the slug from GET /skills/marketplace. The response `name` is the frontmatter display label (may differ from the slug, e.g. slug `xiaohongshu-mcp-skills` with name `xiaohongshu`).

### Update a custom skill
- Method: PUT
- Path: /skills/{slug}
- Body: `{"description": "...", "prompt": "# My Skill\n\n..."}`
- Response: `{"status": "updated"}`
- Notes: For skills you have created or customized. The `{slug}` path segment is the directory identifier (from GET /skills). Bundled skills should be reinstalled rather than edited. The existing frontmatter `name` (display label) is preserved; supply a fresh name via the payload only when renaming intentionally.

### Delete a skill
- Method: DELETE
- Path: /skills/{slug}?confirm=true
- Response: `{"status": "deleted"}`
- Notes: DESTRUCTIVE. The `{slug}` path segment is the directory identifier. Automatically detaches from all agents that use it and clears any stored API keys from the OS keychain.

## Common Scenarios

### "What skills are available to install?"
1. GET /skills/downloadable — see bundled skills included with Shannon
2. GET /skills/marketplace — see community skills

### "Install the PDF skill"
1. GET /skills/downloadable — find the PDF skill name (e.g., `pdf`)
2. POST /skills/install/pdf
3. PUT /agents/{agent-name}/skills/pdf — attach to the agent that needs it

### "Give an agent presentation ability"
1. GET /skills/downloadable — find presentation skill (e.g., `pptx` or `slidev`)
2. POST /skills/install/pptx
3. PUT /agents/my-agent/skills/pptx
4. Verify: GET /agents/my-agent — check `skills` array includes `pptx`

### "Remove a skill from one agent but keep it for others"
1. DELETE /agents/{agent-name}/skills/{skill-name} — detaches from that agent only
2. Skill remains installed and attached to other agents.

### "Completely remove a skill"
1. DELETE /skills/{skill-name}?confirm=true — removes skill and detaches from ALL agents

## Safety Notes

- **Deletion detaches from all agents**: If you delete a skill, every agent that uses it immediately loses that ability. Prefer detaching from a specific agent (DELETE /agents/{name}/skills/{skill}) if you only want to remove it from one.
- **Marketplace skills**: Review the skill description before installing — marketplace skills are community-contributed and not officially vetted.
- **Custom skill edits**: Use PUT /skills/{name} only for skills you own. Editing bundled skills may cause issues on updates. If you need to customize a bundled skill, copy its content first and create a new skill with a different name.

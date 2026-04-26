# Changelog

All notable changes to ShanClaw are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/).

## Unreleased

## v0.0.101 — 2026-04-27

### Added
- **Event bus enrichment** — `tool_status` (running/completed), `run_status`, and `usage` snapshot events emitted to the EventBus ring buffer; `multiHandler` fan-out wires `busEventHandler` into all RunAgent paths so SSE subscribers and Desktop get a unified real-time event stream.
- **Per-request SSE tool events enriched** — elapsed time, `is_error`, and redaction-boundary semantics aligned between per-request SSE and bus emissions.
- **Hidden skills flag** — `hidden: true` in skill frontmatter excludes internal skills (e.g. `kocoro-generative-ui`) from `GET /skills` listing while keeping them loadable via `use_skill`; flag preserved across `WriteGlobalSkill` round-trips; `GET /skills/{name}` exposes it on `SkillDetail`.
- **kocoro-generative-ui bundled skill** — inline visualization assistant teaching the agent to emit `html-artifact` fenced blocks rendered in Kocoro Desktop's sandboxed WKWebView; reference files cover charts, diagrams, maps, SVG, and UI components.
- **Kocoro identity + language anti-drift policy** — persona rebrand to Kocoro; language policy added to prevent identity drift across long sessions.
- Skill secrets API endpoints: `PUT/DELETE /skills/{name}/secrets` and `GET /skills` returns `required_secrets` + `configured_secrets` (values never exposed).
- `metadata.clawdis` accepted as third ClawHub spec alias alongside `openclaw` and `clawdbot`.
- heatmap-analyze skill: API-key acquisition walkthrough; EN+JA official copy with reply-language rule.

### Fixed
- **Agent reliability triad**: loop-detector args-uniqueness gate prevents batch-tolerant tool thrash; force-stop now synthesizes a structured partial report; empty-result rule narrowed to distinguish retry vs diversify (user-named scope wins, `http` excluded).
- `writeVerbs` blacklist expanded; compound-verb MCP tool names rejected from batch-tolerance.
- Benchmark analyzer unifies synthesis detection and handles `force_stop` audit events.
- Skills: frontmatter `name` decoupled from marketplace slug — `Slug` used everywhere directory/URL/manifest identity is needed; secrets lookup uses `Slug`.
- Daemon: `daemon.auto_approve` settable via `PATCH /config`.
- Kocoro skill: drop sticky-instructions after opt-in revert; post-create hint steers to ShanClaw Desktop.

## v0.0.98 — 2026-04-20

### Added
- **Phase 2.3 memory client** — sidecar lifecycle (spawn / health / restart / shutdown), 24h bundle puller with tenant fingerprint, `memory_recall` tool with `session_search` + `MEMORY.md` fallback, CLI/TUI attach-only path via `NewServiceAttached`, full daemon wire-up.
- **Daily session sync** — opt-in upload of `~/.shannon/sessions/` to Shannon Cloud with flock + atomic marker, per-session ACK, persistent failed-entry bookkeeping, oversized + load-error permanent rejection.
- **Three-layer skill discovery** — skill descriptions embedded in scaffolded first user message (4000-char budget, rune-safe), semantic prefetch on iteration 0 (`model_tier: small`, 5s timeout, gated by `agent.skill_discovery`), fallback catalog in `use_skill` tool description.
- **Skill secrets management** — per-skill API keys stored in the macOS Keychain via `zalando/go-keyring` (pure Go, no CGo; password passed via stdin not argv). Plaintext index at `~/.shannon/secrets-index.json` tracks configured key names; values are env-var-injected into `bash` only for skills activated via `use_skill` within the current run.
- **heatmap-analyze bundled skill** — Ptengine heatmap analysis with `install.sh`.
- **kocoro setup skill** — platform-configuration assistant teaching the agent to manage ShanClaw via the daemon HTTP API.
- **Cache-source TTL routing** — `cache_source` tags every LLM call; 1h cache for channel/TUI, 5m for one-shot/subagent; `SHANNON_FORCE_TTL` override.

### Fixed
- Runtime hardening: skill-discovery guards, sticky policy routing, tool error semantics.
- MaxIter graceful finalize synthesizes a partial report; `Partial` flag corrected.
- Sync CLI path: `config.Load()` runs before sync; `cloud.*` aliases canonicalized.
- Memory cold-start bootstrap via `os.Stat`.
- Usage accounting pipeline and cache breakdown corrections.

## v0.0.96 — 2026-04-14

### Added
- Inline base64 image blocks materialized to `~/.shannon/tmp/attachments/<nonce>/` with model-visible path hints, so agents use real attachment tools instead of hallucinating replicas (#62).
- MCP workspace roots advertised to servers honoring the roots capability — `browser_file_upload` accepts staged attachment paths (#63).
- CJK-aware FTS5 session search via trigram + short-query fallback (#60).
- Family-aware no-progress nudges; `[system]` prefix on harness-injected messages.

### Fixed
- Session-edit API preserves multimodal content on resend (#61).
- Reanchor message preserves current-turn text blocks across deferred-tool / post-compaction / retry boundaries.
- Browser upload recovery hints and loop-detector scoping prevent retries into closed file choosers.

## v0.0.95 — 2026-04-13

### Added
- Remote file attachment download pipeline for Slack and Feishu (#54).

### Fixed
- `bash` NoProgress threshold raised to prevent premature force-stop.
- Double-encoded `tool_use` input unwrapped for OpenAI-shaped providers.
- Request config preserved and partial state surfaced on force-stop.

## v0.0.94 — 2026-04-11

### Fixed
- Playwright Chrome profile clone lifecycle: update ordering and sync, state kept consistent during reset (#52).
- Closed remaining process-cwd leaks in readtracker and session manager (#51).

## v0.0.93 — 2026-04-11

### Fixed
- `readtracker` no longer falls back to daemon process CWD when no session CWD is set — scopeless relative paths stay distinct from their absolute form.
- Removed dead `getCWD()` helper from session manager.
- Regression test locks in the new contract.

## v0.0.92 — 2026-04-06

### Added
- **Delta injection** — `DeltaProvider` interface polled at loop iteration boundary. Ships `TemporalDelta` (date rollover detection). Delta messages visible to model mid-run but excluded from session persistence.
- **Contrast examples** — 5 GOOD/BAD behavioral pairs targeting cowork failure modes (over-engineering, coding-default bias, premature completion, narrating instead of acting, wrong cloud/local boundary). Cloud/local pair conditional on `cloud_delegate` availability.
- **Bundled specialist agents** — `@explorer` (read-only orientation) and `@reviewer` (critical evaluation) embedded via `embed.FS`, synced to `_builtin/` on startup. Two-step `LoadAgent` resolution (user > builtin). CRUD protection with full-snapshot materialization before writes.
- **Session-scoped CWD** — each run carries its own project directory, resolving the daemon CWD gap. Priority cascade: request `cwd` → resumed session → agent config `cwd` → process fallback.
- **Structured inject payload** — follow-up injection uses `InjectedMessage` instead of raw text. Active-run CWD is immutable (different-CWD follow-ups return `cwd_conflict` 409).
- **Project config overlay** — project-local config loaded at runtime from session CWD, scoped to session-safe fields (`model_tier`, `agent.*`, `tools.*`, `permissions.*`). Process-global settings (`endpoint`, `api_key`, `mcp_servers`, `daemon.*`) no longer overridden.

### Fixed
- `listAgentNames` returns `([]string, error)` — propagates I/O errors, only swallows `os.IsNotExist`.
- `EnsureBuiltins` uses `os.CreateTemp` for race-safe temp files.
- `GET /agents/{name}` matches `ListAgents` semantics: `Builtin=true` only when no user override exists.
- Path traversal canonicalization and symlink escape prevention in `IsUnderSessionCWD`.
- Cold-start resume treats empty resumed session as fresh.
- Heartbeat CWD carryover and one-shot validation.
- `cloud_delegate` deep-copied per-run to prevent concurrent daemon route races.

## v0.0.91

### Added
- **Context quality Phase 1–3** — compaction floor, session-scoped tool warming, reactive compaction recovery

### Fixed
- Agent skill CRUD aligned with manifest-based attachment model
- Spill cleanup lifetime scoped to session, spurious `OnToolCall` suppressed
- TUI rendering: header duplication, resize, response positioning

## v0.0.9

### Added
- **Prompt cache stability** — `PromptParts` (static/stable/volatile) split, `ToolSourcer` sorted ordering, cache telemetry
- **Context management** — tiered compaction with head+tail truncation, reactive compaction on overflow, two-phase compression with analysis scratchpad, micro-compact LLM summary, memory staleness annotation
- **Tool safety** — partitioned batch execution (read-only parallel, writes serialized), disk spill for large results (>50K), deferred tool loading (`tool_search` meta-tool)
- **Output format profiles** — channel-aware formatting (`markdown` for TUI/web, `plain` for Slack/LINE/Telegram/webhook)
- **Self-awareness and system reminders** — reinforcement hints in long sessions
- `OnToolCall` fires at actual execution start (post-semaphore)
- `ax_server` bundled mode with Unix socket transport
- `cloud_delegate` terminal param for loop continuation control

### Fixed
- Deferred `tool_search` continuation (model proceeds after schema load)
- Cache ratio formula corrected for Anthropic token semantics
- Volatile context stripped from persisted session history
- API key whitespace trimmed in all config load/save paths
- Per-message timestamps in session persistence

## v0.0.8

### Added
- **Manifest-based skill attachment** for agents (name-only attachment, replace semantics)
- Bundled skills moved to installable, hidden from default skills list

### Fixed
- Playwright CDP lifecycle: lazy-launch, race conditions, Chrome cleanup
- CDP Chrome launched offscreen to prevent window flash
- Orphaned CDP Chrome cleanup after daemon hard kill
- Bundled skills removed from runtime loading (global-only resolution)

# Brand & Intent Discovery

Before collecting technical parameters (Phase 0d) and fetching heatmap data (Phase 1), spend a
short pre-analysis beat understanding **what this page is trying to do for what business**. This
is Phase 0b's job — optionally preceded by Phase 0c auto-research when the user's message lacks
business context.

The product of this phase is a compact `brand_context` card that subsequent phases consume when
writing narrative. The card **never overrides metric evidence** — it only shapes phrasing and the
priority of findings.

---

## Purpose

Human CRO analysts open every engagement with two moves:
1. "Let me look at what this company does" (research)
2. "What are you trying to get out of this analysis?" (intent clarification)

This phase reproduces those moves. Skipping them forces Phase 5 to write generic conclusions;
doing them turns the same heatmap numbers into findings the operator can act on.

---

## Decision table — when to run, skip, or short-circuit

Check the user's first message against this table:

| Condition | Action |
|---|---|
| URL + analysis type + ≥ 2 business signals (brand / product / audience / primary_goal) | Skip Phase 0c and 0b. Synthesize `brand_context` from the message with `source: user_stated`. Go to Phase 0d. |
| URL only, no business context | Run **Phase 0c.1** auto-research, then Phase 0b asking only the remaining gaps. |
| User gives a brand-doc path (e.g. "my brand doc is at /path/brand.md") | `file_read` that path, extract the card with `source: brand_doc`, Phase 0b only fills gaps. Phase 0c.1 usually unnecessary. |
| User explicitly says "skip questions" / "just analyze" / "不要调研" / "直接分析" | `brand_context` = `{ source: none }`, skip Phase 0b and Phase 0c, go straight to Phase 0d. |
| URL missing | Ask for URL first; re-enter this table once received. |

Hard caps:
- Phase 0b may ask at most **5 questions**; after Phase 0c.1 you should usually need only 1-2.
- Never re-ask a field the user has already stated.
- Use `think` to rank candidate questions by expected narrative-quality lift before asking; the
  highest-lift 1-3 are almost always enough.

---

## Phase 0c.1 — Auto Website / Brand Research

**Purpose**: gather the minimum brand, product, and audience signal needed to turn blind
clarifying questions into targeted ones. This is NOT heatmap data.

### Boundaries (re-read before fetching)

- `ptengine-cli` is the only authoritative source for the analyzed URL's **block structure and
  historical behavior metrics**. Do NOT use `http` / `browser_*` / `screenshot` to fetch the
  analyzed URL and use its current DOM to fill `content_summary`, `block_name`, or any block-level
  field in Phase 3.
- You MAY use `http` / `browser_*` for brand-context pages (company homepage, about, pricing,
  product pages). If the analyzed URL IS the homepage, you may fetch it — but only to read brand
  voice / what the company does, not to infer block structure.

### What to fetch (priority order)

1. Site root (e.g. strip path from the analyzed URL). Skip if the analyzed URL already is the root.
2. One of: `/about`, `/company`, `/team`.
3. One of: `/pricing`, `/products`, `/features` — whichever best reveals business model.

### Budget

- At most **3 URLs**.
- Tool choice (prefer higher-level tools when available):
  - `web_fetch` or `web_subpage_fetch` (gateway) — first choice; handles JS-rendered pages
  - `http` GET + text extraction — fallback when gateway tools are unavailable
  - `browser_navigate` + `browser_snapshot` — escalate only when the above return empty / JS-shell
  - `web_search` — useful when only a URL is given and the brand name isn't in the URL/page title
- Each page contributes **at most ~1500 characters** of extracted text into `think`; do not push
  full HTML into context.
- Total time budget **< 20 seconds**. On timeout or network error, fall back to Phase 0b pure Q&A
  and record `source: user_stated` (or `mixed` if partial research completed).
- Record every fetched URL in `brand_context.research_refs[]` with a one-line note; this enables
  audit / user review.

### What to extract

From the fetched text, infer:
- `brand_name` (company name, product name)
- `product_or_service` (one line)
- `business_model` (saas | ecommerce | content | leadgen | other)
- `target_audience` (if clearly stated on site; otherwise leave null)
- `brand_voice` (professional / playful / technical / editorial / etc.; optional)

Leave `primary_goal` and `known_problems` to Phase 0b — those are intent, not site facts.

---

## Phase 0c.2 — Heatmap Preview (optional, ptengine-cli only)

Run a lightweight `block_metrics` probe (last 7 days, MOBILE, no filters) ONLY when BOTH are true:

- Page-type classification is still ambiguous after Phase 0c.1 (e.g. URL + brand both compatible
  with multiple of the 7 types), AND
- A quick block-names sample would materially disambiguate it (e.g. telling `sales_lp` from
  `article_lp` by the presence of hero+offer+cta vs narrative+softCTA).

Default posture is to SKIP this probe. It costs one extra API call and rarely changes the final
Phase 2 classification (Phase 1 will fetch the full range anyway). Only run it when skipping it
would leave Phase 0b asking a worse clarifying question about analysis framing.

The 7-day probe is an **early preview**, not a Phase 1 substitute. Phase 1 always fetches with
the user's requested date range (collected in Phase 0d). Reuse the 0c.2 response only for
`preview_signals` in `brand_context`; do NOT use it for final `base_metric` / `block_data`.

---

## `brand_context` schema

```yaml
brand_context:
  brand_name: string | null
  product_or_service: string | null     # one line
  business_model: saas | ecommerce | content | leadgen | other | null
  target_audience: string | null        # one line, e.g. "indie developers, US"
  primary_goal: string | null           # one line, e.g. "free-trial signup"
  known_problems: [string]              # 0-3 user-stated pain points (may be empty)
  brand_voice: string | null            # optional one line
  source: user_stated | auto_research | brand_doc | inferred_from_preview | mixed | none
  research_refs:                        # 0c.1 sources; omit or [] when 0c.1 didn't run
    - url: string
      note: string                      # one-line summary of what this page contributed
preview_signals:                        # present only when Phase 0c.2 ran
  block_names_sample: [string]          # up to ~15 names
  proposed_page_type: string            # one of the 7 page-classification keys
  confidence: high | medium | low
```

Nullable fields exist because Phase 0b can be skipped (user says "just analyze") or Phase 0c.1
can fail (network error, bot-protected site). Any field can be `null`; downstream phases must
tolerate missing values without erroring.

Target size: **≤ ~300 tokens** serialized. Trim long strings before writing the card; the card
travels through every subsequent phase and bloats prompt cache if it grows.

---

## Phase 0b — Question bank

Use `think` to pick at most 5 (usually 1-3) from the categories below, prioritizing the gaps that
survived Phase 0c.1 / brand-doc ingestion.

### A. Business & audience (ask only if unclear from research)
- "What does this page sell or offer, and who is the primary audience?"
- "Is this a B2B, B2C, or internal page?"

### B. Primary goal (almost always worth asking)
- "What's the main action you want visitors to take on this page — a purchase, a signup, a lead
  form, reading to completion, or something else?"
- "If this page performed perfectly, what would the user do differently?"

### C. Known problems / hypotheses (worth asking — sharpens Phase 5)
- "Is there a specific problem you already suspect — drop-off before the CTA, low ad-traffic
  quality, mobile layout issues — or should I look broadly?"
- "Have there been recent changes (redesign, copy update, pricing change) that the analysis
  window should account for?"

### D. Analysis framing (ask only if analysis_type wasn't stated and isn't obvious)
- "Do you want a single-page deep dive, a comparison across segments (new vs returning,
  organic vs ads), or A/B test validation?"
- "Any segment you care about most — device, traffic source, campaign?"

### E. Skip signals
If the user shows impatience ("just analyze", "skip", "直接开始"), stop asking, record the partial
card, and proceed.

---

## Localization

Questions are asked in the **user's question language** (same rule as `ptengine-cli.md` §
"Reply language" — the Phase 0 setup conversation is not governed by the `language` parameter).
Phase 0c.1 research summaries in `think` may be English or the user's language; the final report
language is still governed by the `language` parameter collected in Phase 0d.

---

## Handoff — how later phases consume `brand_context`

- **Phase 2 (page classification)**: `preview_signals.proposed_page_type` and
  `brand_context.business_model` are **weak priors**. Full-data signals (actual block names from
  Phase 1, full page_insight) still override the prior if they disagree.
- **Phase 3 (data enrichment)**: `brand_context.product_or_service` and `brand_voice` inform the
  phrasing of `content_summary` and `marketing_intent`. Never invent block copy; only use brand
  context to choose between equally-grounded paraphrasings.
- **Phase 5 (analysis execution)**: `brand_context.primary_goal` shapes which
  barriers/opportunities surface first. `brand_context.known_problems` becomes a hypothesis the
  Phase 5 narrative can confirm or complicate (again, hedged — cite metric evidence).
- **Phase 6 (report)**: When `source != none`, open the report with a one-sentence framing
  sentence that ties the analysis to `primary_goal`. If `research_refs` is non-empty, you may
  append a small "Sources consulted" footnote at the end of the report listing those URLs.

---

## Hard rules (read before writing any narrative)

1. `brand_context` **does not substitute metric evidence**. Any conclusion driven partly by brand
   context must also cite at least one dwell / exit / impression data point.
2. Block-level text (`block_name`, `content_summary`) comes from `ptengine-cli` responses —
   never from Phase 0c.1 research. If a block's name is missing from ptengine-cli, ASK the user;
   do not scrape the page to fill it in.
3. The card is optional. When `source: none`, every downstream phase behaves exactly as in the
   pre-discovery workflow.
4. Never echo `brand_context` as raw YAML in user-facing output. It is internal state.

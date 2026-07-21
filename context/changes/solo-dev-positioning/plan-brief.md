# Mapping Card Provenance + README Rewrite — Plan Brief

> Full plan: `context/changes/solo-dev-positioning/plan.md`
> Frame brief: `context/changes/solo-dev-positioning/frame.md`

## What & Why

The mapping-graph visualization renders data shape (model → provider → upstream) but not **decision provenance** — when each mapping was added, whether its env var is present, what family of client models it resolves — leaving the maintainer unable to reconstruct why a mapping exists and whether it will forward successfully. The README doesn't anchor any of this either: 218 lines of "what it connects" with zero maintainer-legibility content.

 enriched cards + a rewritten README that answers "what is this for / how do I read its current state" in 30 seconds.

## Starting Point

The breadcrumb-chain card renderer (`buildMappingRows` at `proxy/web/handlers.go:267`) already iterates the full providers map and could surface env status inline — but the existing `checkRequiredEnvVars` logic has its result discarded at `main.go:134`. The `extractFamily()` function (`proxy/families.go:18`) exists as dead code from an archived plan. The README (218 lines) opens with a 16-row provider table and never tells the maintainer how to read system state.

## Desired End State

Each mapping card shows: (a) an "added" date from config annotation, (b) a green/amber env-var-present dot, (c) a family badge. A maintainer returning after a gap reads the rewritten README in 30 seconds and understands the tool, its audience, and how to read system state from the dashboard.

## Key Decisions Made

| Decision | Choice | Why | Source |
| --- | --- | --- | --- |
| Added-at timestamp source | Config-file annotation | Zero build complexity, no git coupling, honest provenance ("when did I add this to config?") | Plan |
| README restructure | Full rewrite from scratch | Directly addresses frame's critique; surgical edits leave the structural problem intact | Plan |
| Mapping-key resolution display | Family badge via `extractFamily()` | Leverages already-written dead code, answers "what models resolve here" at near-zero cost | Plan |
| Env status check | Inline `os.Getenv` in `buildMappingRows` | Reuses existing `DefaultAPIKeyEnv` field, zero new deps, reads live process env | Plan |
| "Why it exists" free-text rationale | Cut | No source of truth exists; manual prose per mapping doesn't pay for itself at current counts | Frame + Plan |

## Scope

**In scope:**

- 3 new signals on mapping cards (added-at, env-present dot, family badge)
- `mappingRow` struct extension
- Optional `added_at` YAML field on mappings
- README rewrite (purpose-first, maintainer-legibility, ~90-140 lines)

**Out of scope:**

- Git blame integration, runtime first-seen timestamps
- Full mapping-key ↔ client-model resolution preview
- Env presence precomputed at startup / stored on Handlers
- Provider struct changes
- Edit dialog / fallback chain JS modifications

## Architecture / Approach

Extend the read path only: add fields to `mappingRow`, populate in `buildMappingRows` using data already in scope (config annotation, inline `os.Getenv`, `extractFamily` call), render in the template, add minimal CSS reusing existing badge/dot tokens. README is an independent full-replacement deliverable. No new types, no new dependencies, no new endpoints.

## Phases at a Glance

| Phase | What it delivers | Key risk |
| --- | --- | --- |
| 1. Data & Types | `mappingRow` extended, populated; `added_at` in config schema; `extractFamily` wired up | Populating cleanly within the existing loop without breaking iteration |
| 2. Card Rendering | Template shows all three signals; minimal CSS reusing tokens | Mobile-viewport overflow of new header elements |
| 3. README Rewrite | Purpose-first README at half the current length | Scope drift into copywriting; must be bounded to maintainer-legibility |

**Prerequisites:** None — `status: framed`, all upstream context in `frame.md`.
**Estimated effort:** ~1-2 sessions across 3 phases.

## Open Risks & Assumptions

- `buildMappingRows` existing tests (if any) may need updating to satisfy assertions on new fields — assumed there's a test harness in `handlers_test.go`.
- The env-dot reflects the **process** env (read at render time). If the maintainer edits env vars without restarting the server, the dot won't update until next restart. Acceptable — the server reads env at startup anyway.
- `extractFamily()` is regex-based on model name substrings (opus/sonnet/haiku). Custom model names with no family keyword will show no badge — that's correct behavior, not a gap.
- README claims about card signals must be implemented (Phase 1-2) before or alongside Phase 3, so the README doesn't describe features that don't yet exist.

## Success Criteria (Summary)

- Each mapping card shows added-date, env-var presence, and family badge; `mage test` + `mage lint` pass.
- README is ≤140 lines, opens with purpose/audience/state-reading, contains zero stale claims, and passes spellcheck.
- `extractFamily()` has at least one production caller (no longer dead code).

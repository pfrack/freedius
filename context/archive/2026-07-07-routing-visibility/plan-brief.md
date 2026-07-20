# Routing Visibility — Plan 1 Brief

> Full plan: `context/changes/routing-visibility/plan.md`
> Frame brief: `context/changes/routing-visibility/frame.md`

## What & Why

The frame brief identified that routing logic is invisible across the entire
web UI — scattered across pages, absent from the Dashboard, and unlinked
between Providers and Mappings. This plan closes the foundational slice: make
the Dashboard a routing overview and add cross-links between Providers and
Mappings. Plans 2 and 3 follow with richer per-step metadata and runtime stats.

## Starting Point

The web-ui-friendliness plan already shipped route-chain cards on `/mappings`
(Phase 2: protocol badges, aria-labels, last-responder chevron, click-through
to filtered logs) and cross-page UX (Phase 3: error feedback, empty states,
loading indicators). The Dashboard (`/`) shows four non-interactive stat cards
with zero routing context. Providers shows `MappingCount` as a plain number.
No `?provider=` filter exists on `/mappings`.

## Desired End State

A user landing on `/` sees: mapping cards (reusing `mappings-table.html`) and
a compact provider list with mapping-count links. Clicking a count navigates
to `/mappings?provider=<name>`, filtered to that provider. The existing stats
(Uptime, Listening On) are preserved in a compact strip above.

## Key Decisions Made

| Decision | Choice | Why | Source |
|---|---|---|---|
| Plan scope | 3 sequential plans: this = cross-links + Dashboard | Frame identified 4 dimensions; splitting avoids one massive PR | Frame |
| Dashboard shape | Repurpose `/` as routing overview | Landing page should orient users toward routing, not generic stats | Plan |
| Dashboard viz | Reuse `mappings-table.html` fragment | Zero new template code; all Phase 2 polish carries over | Plan |
| Provider↔Mapping link | MappingCount → link to `/mappings?provider=` | Minimal template change; `MappingCount` already exists | Plan |
| Filter matching | Substring case-insensitive on provider name | Matches `handleLogs` pattern; catches both primary and fallback refs | Plan |
| Plan phases | 3 phases: filter, cross-links, dashboard | Each independently reviewable; backend testable before UI | Plan |

## Scope

**In scope:**
- `?provider=` filter on `/mappings` (substring, case-insensitive)
- `MappingCount` as a link in providers table
- Dashboard shows mapping cards + provider list
- Shared `buildMappingRows` helper (eliminates duplication)

**Out of scope:**
- Runtime stats (error rates, fallback triggers) — Plan 3
- Richer per-step metadata (Behavior, effective URL, family matching) — Plan 2
- New sidebar links, a11y audit, client-side search
- Config schema changes

## Architecture / Approach

Extract `buildMappingRows(cfg, providers, lastResponder, providerFilter)` from
the duplicate logic in `handleMappings` and `renderMappingsTable`. Add
`Mappings` and `Providers` fields to `indexData` so `index.html` can include
`{{template "mappings-table" .}}`. Change `{{.MappingCount}}` in
`providers-table.html` to a link. All changes in `proxy/web/` only.

## Phases at a Glance

| Phase | What it delivers | Key risk |
|---|---|---|
| 1. Filter contract | `?provider=` on `/mappings` + shared helper | Filter logic correctness (substring vs exact) |
| 2. Cross-links | MappingCount → link in providers table | URL encoding of provider names |
| 3. Dashboard | Mapping cards + provider list on `/` | Template data shape (`indexData` must match `mappings-table` expectations) |

**Prerequisites:** web-ui-friendliness Phases 1–3 shipped (breadcrumb polish +
cross-page UX). No blockers.
**Estimated effort:** ~1 session across 3 phases.

## Open Risks & Assumptions

- `MappingCount` currently counts primary-provider references only (not
  fallback references). The link target `/mappings?provider=` uses substring
  match which catches both primary and fallback. This asymmetry is intentional
  (count = "how many mappings use this as primary"; filter = "show all
  mappings touching this provider") but could surprise users.
- The Dashboard reuses `mappings-table.html` which renders CRUD buttons (Edit,
  Delete). These are functional on the Dashboard — same as on `/mappings`. If
  the user wants read-only Dashboard cards, the fragment needs a conditional
  flag (out of scope for this plan).

## Success Criteria (Summary)

- Landing on `/` shows routing context (mapping cards + provider list)
- Clicking a provider mapping-count navigates to filtered `/mappings`
- `/mappings?provider=<name>` correctly filters by substring match
- All existing tests pass; new tests cover filter + cross-link + dashboard

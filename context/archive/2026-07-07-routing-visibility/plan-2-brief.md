# Routing Visibility — Plan 2 Brief

> Full plan: `context/changes/routing-visibility/plan-2.md`
> Frame brief: `context/changes/routing-visibility/frame.md`
> Depends on: Plan 1 (`context/changes/routing-visibility/plan.md`)

## What & Why

The breadcrumb chain shows only Provider/Model + Protocol (when set),
collapsing ~7 routing decision points into 2 visible dimensions. This plan
surfaces the missing static metadata: Behavior class (openai/anthropic/mix),
effective BaseURL (post-normalization), API Key Env var, and family matching
visibility. Together with Plan 1's cross-links and Dashboard, this makes the
chain a complete picture of the static routing config.

## Starting Point

Plan 1 (Dashboard + cross-links) ships `buildMappingRows` helper, `?provider=`
filter, and MappingCount links. The chain cards show Protocol badge, aria-
labels, last-responder chevron, and click-through to logs. Missing: Behavior,
effective URL, API Key Env, family matching.

## Desired End State

Each step pill shows Behavior label + API Key Env (muted). Tooltip shows
effective URL (not configured). Family-named mappings ("opus", "sonnet") show
a badge in the card header.

## Key Decisions Made

| Decision | Choice | Why | Source |
|---|---|---|---|
| Effective URL computation | Handler-layer helper, not MixAdapter import | MixAdapter is a runtime struct; handler needs a pure function | Plan |
| Family matching source | Reimplement regexes in handler, don't import `proxy` package | Family list is a UI concern; avoids coupling | Plan |
| Behavior display | Inline label on each step pill | Matches existing Protocol badge pattern | Plan |
| Family badge placement | Card header (mapping-level), not step-level | Family matching is about the mapping name, not individual steps | Plan |
| API Key Env display | Muted monospace text on each step | Informational; shouldn't compete with Provider/Model for attention | Plan |

## Scope

**In scope:**
- `Behavior`, `APIKeyEnv`, `EffectiveURL` fields on `fallbackEntry` + `mappingRow`
- `Family` field on `mappingRow`
- `computeEffectiveURL` helper function
- Family matching detection (reimplemented regexes)
- Template rendering of new fields
- CSS for Behavior label, API Key Env, family badge

**Out of scope:**
- Runtime stats (error rates, fallback triggers) — Plan 3
- Dashboard changes — Plan 1
- Cross-page links — Plan 1
- Config schema changes
- Changes to `mix.go` or `proxy/` package

## Architecture / Approach

Extend `fallbackEntry`/`mappingRow` types. Add `computeEffectiveURL` helper
that mirrors `mix.go:84-101` suffix logic. Add family matching via local
`knownFamilies` regex map. Update `buildMappingRows` (from Plan 1) to
populate all new fields. Update `mappings-table.html` to render them.

## Phases at a Glance

| Phase | What it delivers | Key risk |
|---|---|---|
| 1. Data model + handlers | New fields populated in buildMappingRows | Effective URL edge cases (trailing slashes, path-only URLs) |
| 2. Template + CSS | New fields rendered in chain cards | Template data shape must match types |

**Prerequisites:** Plan 1 (`buildMappingRows` helper must exist).
**Estimated effort:** ~1 session across 2 phases.

## Open Risks & Assumptions

- `computeEffectiveURL` reimplements logic from `mix.go:84-101`. If the
  normalization logic changes in `mix.go`, the handler helper must be updated
  in sync. Risk is low (the logic is stable and simple).
- Family matching regexes are reimplemented in the handler. If `families.go`
  adds new families, the handler must be updated. Mitigate by documenting the
  coupling in a code comment.

## Success Criteria (Summary)

- Each step pill shows Behavior + API Key Env
- Tooltip shows effective URL
- Family-named mappings show a badge
- All tests pass; no visual regressions

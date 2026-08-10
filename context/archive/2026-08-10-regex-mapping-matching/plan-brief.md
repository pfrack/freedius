# Regex model→mapping matching + protected default — Plan Brief

> Full plan: `context/changes/regex-mapping-matching/plan.md`
> Frame brief: `context/changes/regex-mapping-matching/frame.md`

## What & Why

We generalize how a requested model name is routed to a mapping. Today routing
only works on an exact key match or one of four hardcoded family keywords
(`opus|sonnet|haiku|auto`); anything else 404s. The reframed problem: matching is
hardcoded to those four families, so user-defined mapping keys only match
exactly — generalize loose matching to all keys, keep `default` as a
specially-exempt always-last catch-all, and protect it from deletion.

## Starting Point

`resolveMapping` (`proxy/proxy.go:132`) does exact-key lookup then
`ExtractFamily` (`proxy/families.go:22`) for four keywords, then `default`. A
`default` catch-all already exists via the family layer, and
`handleDeleteMapping` (`proxy/web/handlers.go:1270`) deletes any mapping
unconditionally. `Mapping` has no regex field.

## Desired End State

Every model name routes to the **most specific** matching mapping key (exact
`model==key` wins; else the longest key among regex matches, ties by key name);
`default` catches everything else and can never be deleted. Matching is a single
precompiled, case-insensitive regex scan per request. The `auto` mapping and the
cosmetic family badge are gone.

## Key Decisions Made

| Decision | Choice | Why | Source |
| --- | --- | --- | --- |
| Precedence on collision | Most-specific wins (exact > longest key > sorted tiebreak > default last) | Intuitive "most specific mapping wins"; deterministic | Plan |
| Pattern source | Auto-wrap key as `(?i).*key.*` only | Matches "all mappings auto"; ReDoS-safe; no new config field | Frame + Plan |
| `default` guarantee | Inject if absent (in-memory) + protect from deletion | Catch-all always exists, even for legacy configs | Frame + Plan |
| Matcher architecture | Unified compiled matcher list on `Config` | Single source of truth, deterministic order | Plan |
| `ExtractFamily` / family badge | Drop entirely | Routing-irrelevant for UI; less dead code | Plan (user) |
| TUI for `default` | Protected badge + disabled delete | Clear, prevents accidental removal | Plan |
| Performance | Exact short-circuit + precompiled matchers | Keeps current fast path; no per-request compile | Plan |
| Injected default target | Mirror first mapping (sorted key) | Routes to a real, configured provider | Plan |
| `auto` mapping | Remove from starter | `default` is now the only catch-all | Plan |
| Case sensitivity | Case-insensitive `(?i)` | Consistent with prior family behavior | Plan |
| Migration | In-memory inject, no disk rewrite | Non-destructive to user files | Plan |
| Tests | Comprehensive | Regression-safe across changed matcher | Plan |

## Scope

**In scope:** compiled matcher on `Config`; `resolveMapping` rewrite; in-memory
`default` injection; delete guard (API + UI); starter `auto` removal; family
badge/`ExtractFamily` removal; comprehensive tests.

**Out of scope:** user-facing `regex` config field; persisting injected default;
schema changes to `Mapping`; provider/fallback changes.

## Architecture / Approach

At load (and after each runtime mutation) `Config` builds an ordered list of
compiled matchers: one `(?i).*<key>.*` per non-`default` mapping key, sorted by
key length descending (ties by key ascending), plus a catch-all `default`
matcher last. `resolveMapping` checks exact `model==key` then scans the list.
`ensureDefaultMapping` (load pipeline only) injects a missing `default` mirroring
the first mapping; `buildMatchers` (reused by mutations) just compiles. This
separation keeps hand-built test `Config`s free of an injected `default`.

## Phases at a Glance

| Phase | What it delivers | Key risk |
| --- | --- | --- |
| 1. Compiled matcher core | `Config` matcher list + `resolveMapping` rewrite; retire `ExtractFamily` | Splitting `ensureDefault` vs `buildMatchers` so negative tests stay valid |
| 2. Default protection + rebuild | Delete guard (409) + `buildMatchers` after add/delete | Forgetting a mutation path → stale matchers |
| 3. Starter cleanup + TUI | Drop `auto`; protected badge + disabled delete | Missing the drawer delete control |
| 4. Comprehensive tests | Precedence/case/inject/guard coverage | Updating hand-built `Config` tests to call `buildMatchers` |

**Prerequisites:** none beyond the existing build (`mage`).
**Estimated effort:** ~2-3 sessions across 4 phases.

## Open Risks & Assumptions

- "Most-specific = longest key" is the agreed specificity proxy (auto-wrapped
  patterns are equal-length wildcards, so key length is the only differentiator).
- Injected `default` mirrors the first mapping's provider/model; if that
  provider is unkeyed, unmatched traffic still 404s at request time (same as
  today's `default`).

## Success Criteria (Summary)

- A model routes to the most specific matching mapping key; unmatched → `default`.
- `default` always exists and cannot be deleted via API or UI.
- `mage test`, `mage lint`, `mage govulncheck` all pass; prior family-priority
  tests still hold under the new matcher.

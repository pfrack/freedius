# Routing Visibility — Plan 3 Brief

> Full plan: `context/changes/routing-visibility/plan-3.md`
> Frame brief: `context/changes/routing-visibility/frame.md`
> Depends on: Plan 1 (`plan.md`), Plan 2 (`plan-2.md`)

## What & Why

Plans 1 and 2 make the static routing config visible (Dashboard, cross-links,
richer per-step metadata). This plan closes the final dimension: runtime
visibility. Users cannot currently see error rates per step, how often
fallback triggers, or what the timeout budget is. The frame investigation
found this is the last gap preventing full routing comprehension.

## Starting Point

Plan 1 ships `buildMappingRows` helper and Dashboard overview. Plan 2 adds
Behavior, effective URL, API Key Env, and family matching to chain cards.
LastResponder tracks the most recent successful responder (60s TTL). EventBus
publishes `RequestEvent` with status/error data. No error rate or fallback
trigger tracking exists.

## Desired End State

Each step pill shows an error rate badge (color-coded %) and last failure
timestamp. Each mapping card header shows fallback trigger count ("fallback
used 3x") and timeout budget ("10m0s budget"). A `/v1/stats/routing` endpoint
exposes raw stats as JSON.

## Key Decisions Made

| Decision | Choice | Why | Source |
|---|---|---|---|
| Aggregation primitive | New `StatsAggregator` in `proxy/` | Parallel to LastResponder; same sync.Mutex + TTL pattern | Plan |
| Wiring point | Direct calls from dispatch path | O(1) per request; avoids EventBus ring buffer scan | Plan |
| TTL window | 5 minutes (vs LastResponder's 60s) | Error rates over 5 min are more useful than 60s | Plan |
| Error rate display | Color-coded badge: green/amber/red | Immediate visual signal; matches existing badge pattern | Plan |
| Fallback trigger display | Card header text ("fallback used Nx") | Mapping-level concept, not step-level | Plan |
| Timeout budget display | Card header muted text | Informational; doesn't compete with error badges | Plan |
| Stats endpoint | `/v1/stats/routing` JSON | Programmatic access; complements the UI rendering | Plan |

## Scope

**In scope:**
- `StatsAggregator` primitive (`proxy/stats.go`)
- Dispatch-path wiring (success, failure, fallback trigger)
- `/v1/stats/routing` JSON endpoint
- Error rate + last failure on step pills
- Fallback trigger count + timeout budget on card headers
- CSS for error rate badges

**Out of scope:**
- Real-time SSE updates (stats update on page reload)
- Per-request latency tracking
- Dashboard changes (Plans 1 and 2)
- Config schema changes

## Architecture / Approach

Create `StatsAggregator` (sync.Mutex map, TTL eviction) tracking per-provider
{errors, total, lastError} and per-mapping {fallbackTriggers}. Wire into
Dispatcher dispatch path at success/failure points. Expose via Handlers.
`buildMappingRows` reads stats snapshots. Template renders badges.

## Phases at a Glance

| Phase | What it delivers | Key risk |
|---|---|---|
| 1. StatsAggregator + dispatch wiring | New primitive + Dispatcher integration | Nil-safety; TTL correctness |
| 2. Endpoint + template rendering | JSON endpoint + chain card badges | Template data shape; error rate edge cases |

**Prerequisites:** Plans 1 and 2 (`buildMappingRows` with all metadata fields).
**Estimated effort:** ~1 session across 2 phases.

## Open Risks & Assumptions

- Stats start empty on every restart. No persistence. Users see data only
  after requests flow through. This is acceptable for a local single-user
  tool.
- Error rate percentage computation: `errors/total` when total > 0, else 0.
  Steps with 0 requests show no badge (not "0%") to avoid clutter.
- `StatsAggregator` adds one mutex lock per request. Same overhead as
  LastResponder — negligible for single-user QPS.

## Success Criteria (Summary)

- Error rate badges appear on steps after failures
- Fallback trigger count visible in card header after fallback fires
- Timeout budget displayed in card header
- `/v1/stats/routing` returns well-formed JSON
- All tests pass; no performance regression

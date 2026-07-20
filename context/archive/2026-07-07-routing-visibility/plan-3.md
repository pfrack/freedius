# Routing Visibility — Plan 3: Runtime Stats

## Overview

Surface runtime routing behavior in the chain cards: error rates per step,
fallback trigger history, and timeout budget visibility. This is the third
and final plan identified by the routing-visibility frame brief.

The frame investigation found that the UI shows static routing config (Plans 1
and 2) but nothing about what actually happens at runtime. Users cannot answer
"which step handled recent requests?" (partially addressed by LastResponder),
"how often does fallback fire?", or "what's the timeout budget for this chain?".

## Current State Analysis

- **LastResponder** (`proxy/lastresponder.go`): Tracks the most recent
  successful responder index per mapping. 60s TTL, lazy eviction. Already
  wired into the dispatch path (`proxy.go:343-344`) and rendered as a chevron
  pulse on the chain card. No error tracking.
- **EventBus** (`proxy/eventbus.go`): Publishes `RequestEvent` after each
  request. Contains `Provider`, `Status`, `Latency`, `ErrorMessage`,
  `ErrorType`. Ring buffer (10k entries). Consumed by the TUI dashboard. The
  web UI does not subscribe.
- **Fallback dispatch** (`proxy.go:275-406`): Iterates the chain. On success,
  records to LastResponder (line 343-344). On failure, appends to `attempts`
  slice and logs (line 383-391). All entries exhausted → aggregated error
  response (line 394-406).
- **`fallbackAttempt`** (`proxy.go:408-416`): Internal struct recording per-
  attempt error. Not exposed outside the dispatch function.
- **Timeout budget** (`proxy.go:281`): `chainTimeout =
  fallbackTimeoutMultiplier * streamTimeout`. Default: `2 * 5min = 10min`.
  Not exposed to the web UI.
- **`eventstream.Handlers`** (`internal/eventstream/handlers.go:23-35`):
  Carries `Bus`, `LogSink`, `Cfg`, `Registry`, `LastResponder`. No stats
  aggregator.

### Key Discoveries:

- The EventBus ring buffer has all the data needed for error rate computation
  (per-provider status codes). But scanning it per-request is O(N) over up to
  10k entries — not acceptable for the render path. Pre-aggregation is
  required.
- The LastResponder pattern (sync.Mutex map + TTL eviction) is the right
  primitive for per-provider stats. Extending it with error counts is
  straightforward.
- The dispatch path already distinguishes success (line 333-347) from failure
  (line 350-391). Adding aggregator calls at these two points is the minimal
  wiring.
- Timeout budget is a **mapping-level** property (depends on fallback chain
  length and configured multiplier). It should appear in the card header, not
  on individual steps.

## Desired End State

Each `.route-step` pill shows:
- **Error rate** (new): small badge showing error percentage (e.g., "12%") or
  "no errors" when zero. Color-coded: green for 0%, amber for 1-25%, red for
  >25%.
- **Last failure** (new): relative timestamp (e.g., "2m ago") when the step
  last failed. Hidden when no failures recorded.

Each mapping card header shows:
- **Fallback trigger count** (new): how many times the fallback chain was
  triggered for this mapping (e.g., "fallback used 3 times"). Hidden when 0.
- **Timeout budget** (new): the chain timeout value (e.g., "10m budget").
  Shown as muted text.

A new JSON endpoint `GET /v1/stats/routing` exposes the raw stats for
programmatic consumption.

Verification: load `/mappings` — steps show error rate badges. A mapping with
fallback history shows "fallback used N times" in the header. After a request
that triggers fallback, the fallback count increments on next page load.

## What We're NOT Doing

- **No real-time SSE updates** — stats update on page reload (server-
  rendered). SSE-driven live updates are a future enhancement.
- **No per-request latency tracking** — the EventBus already tracks latency
  but surfacing it per-step is out of scope.
- **No dashboard changes** — Plans 1 and 2 handle that.
- **No changes to the config schema** — all changes in `proxy/` and
  `proxy/web/`.
- **No EventBus subscription from the web UI** — pre-aggregation via direct
  dispatch-path calls is simpler and O(1) per read.

## Implementation Approach

Create a `StatsAggregator` in `proxy/` (parallel to `LastResponder`). Wire it
into the dispatch path at success and failure points. Expose via a new
`/v1/stats/routing` endpoint. Pass stats to `buildMappingRows` for template
rendering. Add CSS for error rate badges and fallback count.

## Critical Implementation Details

- **`StatsAggregator` must be injected into the Dispatcher** — the dispatch
  path (`proxy.go:333-391`) needs to call `agg.RecordSuccess(provider)` and
  `agg.RecordFailure(provider)`. The `Dispatcher` struct (line 44-59) already
  carries `LastResponder`; add `Stats *StatsAggregator` alongside it.
- **TTL for stats entries** — use 5 minutes (vs LastResponder's 60s). Error
  rates over a 5-minute window are more useful than over 60s. Background
  eviction is not needed; lazy eviction on read (same as LastResponder) is
  sufficient.
- **Fallback trigger counting** — track per-mapping (not per-provider). When
  the dispatch path enters the fallback loop and `i > 0` on success (line 335),
  increment the mapping's fallback-trigger count. Separate from per-provider
  error stats.
- **`buildMappingRows` dependency** — Plan 1's `buildMappingRows` takes
  `(cfg, providers, lastResponder, providerFilter)`. This plan adds a `stats
  *StatsAggregator` parameter. All callers must pass it.
- **`/v1/stats/routing` response shape** — return a JSON object with two
  maps: `providers: {name: {errors, total, lastError}}` and `mappings: {name:
  {fallbackTriggers}}`. Keep it simple; no nested structure.
- **Timeout budget computation** — the `Dispatcher` exposes
  `FallbackTimeout()` method returning `time.Duration(fallbackTimeoutMultiplier)
  * streamTimeout`. The handler calls it and passes to the template as a
  formatted string.

---

## Phase 1: StatsAggregator + dispatch wiring

### Overview

Create the `StatsAggregator` primitive in `proxy/`. Wire it into the
Dispatcher's dispatch path. Expose via `eventstream.Handlers`.

### Changes Required:

#### 1. Create `StatsAggregator`

**File**: `proxy/stats.go` (new)

**Intent**: A per-provider and per-mapping stats aggregator, parallel to
`LastResponder`. Tracks error count, total request count, and last error time
per provider. Tracks fallback trigger count per mapping. TTL-based lazy
eviction.

**Contract**:

```go
type StatsAggregator struct {
    mu       sync.Mutex
    providers map[string]providerStats
    mappings  map[string]mappingStats
    now       func() time.Time
    ttl       time.Duration
}

type providerStats struct {
    errors    int
    total     int
    lastError time.Time
}

type mappingStats struct {
    fallbackTriggers int
    lastTrigger      time.Time
}
```

Methods:
- `RecordSuccess(provider string)` — increments `total` for the provider.
- `RecordFailure(provider string)` — increments `errors` and `total`, updates
  `lastError`.
- `RecordFallbackTrigger(mappingName string)` — increments
  `fallbackTriggers`, updates `lastTrigger`.
- `ProviderSnapshot() map[string]ProviderStatView` — returns a copy with
  expired entries filtered out.
- `MappingSnapshot() map[string]MappingStatView` — returns a copy with
  expired entries filtered out.

`ProviderStatView` and `MappingStatView` are exported structs for JSON
serialization.

#### 2. Wire StatsAggregator into Dispatcher

**File**: `proxy/proxy.go`

**Intent**: Add `Stats *StatsAggregator` field to `Dispatcher` struct (line
44-59). Call `Stats.RecordSuccess` on success (line 333-347) and
`Stats.RecordFailure` on failure (line 350-391). Call
`Stats.RecordFallbackTrigger` when fallback fires (line 335).

**Contract**: In the dispatch loop:
- After `adapter.Handle` returns with `err == nil || ww.wroteHeader` (line 333):
  call `d.Stats.RecordSuccess(target.ProviderName)`. If `i > 0`, also call
  `d.Stats.RecordFallbackTrigger(mappingName)`.
- After error classification (line 374-381): call
  `d.Stats.RecordFailure(target.ProviderName)`.

Guard all calls with `if d.Stats != nil` (same nil-safe pattern as
LastResponder).

#### 3. Add FallbackTimeout method to Dispatcher

**File**: `proxy/proxy.go`

**Intent**: Expose the chain timeout budget as a method so the web handler can
read it.

**Contract**:

```go
func (d *Dispatcher) FallbackTimeout() time.Duration {
    return time.Duration(float64(d.fallbackTimeoutMultiplier)) * d.streamTimeout
}
```

#### 4. Expose StatsAggregator via eventstream.Handlers

**File**: `internal/eventstream/handlers.go`

**Intent**: Add `Stats *proxy.StatsAggregator` field to `Handlers` struct (line
23-35). Wire it from `main.go` (or wherever the Dispatcher is constructed).

**Contract**: New field `Stats *proxy.StatsAggregator`. The web handler layer
accesses it via `h.Stats`.

### Success Criteria:

#### Automated Verification:

- [ ] 1.1 `go build ./...` succeeds
- [ ] 1.2 `mage test` passes — all new + existing tests
- [ ] 1.3 `mage lint` clean
- [ ] 1.4 `TestStatsAggregator_RecordSuccess` — record 3 successes for
  provider "nim"; snapshot shows `total: 3, errors: 0`
- [ ] 1.5 `TestStatsAggregator_RecordFailure` — record 2 failures + 1 success;
  snapshot shows `errors: 2, total: 3`
- [ ] 1.6 `TestStatsAggregator_TTLEviction` — record entries, advance clock
  past TTL, snapshot returns empty
- [ ] 1.7 `TestStatsAggregator_FallbackTrigger` — record 2 fallback triggers
  for mapping "opus"; mapping snapshot shows `fallbackTriggers: 2`
- [ ] 1.8 `TestDispatcher_StatsWiring` — dispatch a request that fails on
  primary and succeeds on fallback; assert Stats shows 1 failure for primary
  provider and 1 fallback trigger for the mapping

#### Manual Verification:

- [ ] 1.9 Send a request that triggers fallback; check `/v1/stats/routing`
  endpoint shows the failure and fallback trigger

**Implementation Note**: After completing this phase and all automated
verification passes, pause for manual confirmation before proceeding to Phase 2.

---

## Phase 2: Endpoint + template rendering

### Overview

Add a `/v1/stats/routing` JSON endpoint. Pass stats to `buildMappingRows`.
Update the template to render error rate badges, last failure timestamps,
fallback trigger counts, and timeout budget.

### Changes Required:

#### 1. Add `/v1/stats/routing` endpoint

**File**: `proxy/web/handlers.go`

**Intent**: Register `GET /v1/stats/routing` in `SetupMux` (near line 85-87).
Returns JSON with provider stats and mapping stats.

**Contract**: Handler reads `h.Stats.ProviderSnapshot()` and
`h.Stats.MappingSnapshot()`, merges into a single JSON response:

```json
{
  "providers": {
    "nim": {"errors": 2, "total": 10, "lastError": "2026-07-07T12:05:00Z"},
    "anthropic": {"errors": 0, "total": 5, "lastError": null}
  },
  "mappings": {
    "opus": {"fallbackTriggers": 3, "lastTrigger": "2026-07-07T12:04:00Z"}
  },
  "chainTimeout": "10m0s"
}
```

When `h.Stats` is nil, return empty maps (graceful degradation).

#### 2. Extend `mappingRow` and `fallbackEntry` with stats fields

**File**: `proxy/web/types.go`

**Intent**: Add display-ready stats fields to the data types.

**Contract**:

Add to `mappingRow`:
- `ErrorRate float64` — 0.0 to 1.0
- `LastErrorAgo string` — relative time (e.g., "2m ago") or ""
- `FallbackTriggers int` — count
- `ChainTimeout string` — formatted duration (e.g., "10m0s")

Add to `fallbackEntry`:
- `ErrorRate float64`
- `LastErrorAgo string`

#### 3. Update `buildMappingRows` to populate stats

**File**: `proxy/web/handlers.go`

**Intent**: Accept a `*proxy.StatsAggregator` parameter. Populate the new
stats fields from the aggregator's snapshots.

**Contract**: `buildMappingRows` signature gains `stats *proxy.StatsAggregator`.
When stats is non-nil:
- Read `stats.ProviderSnapshot()` and `stats.MappingSnapshot()`.
- For each step, compute `ErrorRate = float64(errors) / float64(total)` (0
  when total is 0).
- Format `LastErrorAgo` as relative time (e.g., `"2m ago"`) using
  `time.Since(lastError).Truncate(time.Second).String()`. Empty when no
  errors.
- Set `FallbackTriggers` from the mapping snapshot.
- Set `ChainTimeout` from `dispatcher.FallbackTimeout().String()` (passed as
  parameter or computed).

#### 4. Template: render stats on chain cards

**File**: `proxy/web/templates/mappings-table.html`

**Intent**: Add error rate badge and last failure timestamp to each step pill.
Add fallback trigger count and timeout budget to the card header.

**Contract**:

For each step pill, after existing metadata (Behavior, API Key Env):
```html
{{if gt .ErrorRate 0.0}}
  <span class="route-step__error-rate" title="{{.LastErrorAgo}}">
    {{printf "%.0f" (mul .ErrorRate 100)}}%
  </span>
{{end}}
```

In the card header, after the fallback count badge:
```html
{{if gt .FallbackTriggers 0}}
  <span class="route-card__fallback-triggers">
    fallback used {{.FallbackTriggers}}x
  </span>
{{end}}
<span class="route-card__timeout text-muted">{{.ChainTimeout}} budget</span>
```

Note: `mul` is not a built-in template function. Either register it in
`templateFuncs` (`embed.go:22-31`) or pre-compute the percentage string in
`buildMappingRows` as `ErrorRatePct string` (e.g., "12%"). The latter avoids
template function changes.

#### 5. CSS: error rate badge, fallback triggers, timeout budget

**File**: `proxy/web/static/app.css`

**Intent**: Add styles for the new runtime indicators.

**Contract**:

- `.route-step__error-rate` — small badge, color-coded: green (0%), amber
  (1-25%), red (>25%). Use `--color-success`, `--color-warning`,
  `--color-error` tokens.
- `.route-card__fallback-triggers` — small text in card header, muted color.
- `.route-card__timeout` — small muted text, right-aligned in header.

### Success Criteria:

#### Automated Verification:

- [ ] 2.1 `go build ./...` succeeds
- [ ] 2.2 `mage test` passes — all new + existing tests
- [ ] 2.3 `mage lint` clean
- [ ] 2.4 `TestStatsEndpoint_ReturnsJSON` — GET `/v1/stats/routing`; assert
  JSON contains `providers` and `mappings` keys
- [ ] 2.5 `TestStatsEndpoint_EmptyWhenNoStats` — no requests; endpoint returns
  `{"providers":{},"mappings":{},"chainTimeout":"..."}`
- [ ] 2.6 `TestMappingRow_PopulatesErrorRate` — dispatch 3 requests (2 fail);
  assert `mappingRow.ErrorRatePct` contains "67%"
- [ ] 2.7 `TestMappingRow_PopulatesFallbackTriggers` — dispatch a request that
  triggers fallback; assert `mappingRow.FallbackTriggers == 1`
- [ ] 2.8 Regex test on rendered HTML: step with errors contains
  `class="route-step__error-rate"`

#### Manual Verification:

- [ ] 2.9 Send requests that cause failures; reload `/mappings`; error rate
  badges appear on affected steps
- [ ] 2.10 Send a request that triggers fallback; reload; "fallback used 1x"
  appears in card header
- [ ] 2.11 Hover error rate badge — tooltip shows last failure time
- [ ] 2.12 Card header shows timeout budget (e.g., "10m0s budget")
- [ ] 2.13 Visit `/v1/stats/routing` — JSON response is well-formed

**Implementation Note**: After completing this phase and all automated
verification passes, the plan is ready for `/10x-impl-review`.

---

## Testing Strategy

### Unit Tests:

- Phase 1: Pure Go tests for `StatsAggregator` (Record, Snapshot, TTL
  eviction). Integration test for Dispatcher wiring (dispatch → stats
  updated).
- Phase 2: Handler tests for `/v1/stats/routing` endpoint. Template-rendering
  tests for error rate badges and fallback counts.

### Integration Tests:

- End-to-end: configure a mapping with a failing primary → dispatch → check
  stats endpoint → check rendered card.

### Manual Testing Steps:

Send requests that trigger failures and fallbacks. Reload `/mappings`. Verify
error rate badges, fallback trigger counts, and timeout budget display. Check
`/v1/stats/routing` JSON.

## Performance Considerations

- `StatsAggregator` adds one mutex lock per request (success or failure path).
  Same pattern as LastResponder — negligible overhead.
- `ProviderSnapshot()` and `MappingSnapshot()` iterate the maps — O(P + M)
  where P = providers, M = mappings. Called once per page render. Acceptable
  for typical configs.
- No EventBus ring buffer scanning — all data is pre-aggregated.

## Migration Notes

- None. No config schema changes. Stats start empty on startup and populate
  as requests flow through.

## References

- Frame brief: `context/changes/routing-visibility/frame.md`
- Prior plans: `context/changes/routing-visibility/plan.md` (Plan 1),
  `context/changes/routing-visibility/plan-2.md` (Plan 2)
- Prior research: `context/changes/web-ui-friendliness/research.md`
- Foundation lessons: `context/foundation/lessons.md`

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a
> step lands. Do not rename step titles. See `references/progress-format.md`.

### Phase 1: StatsAggregator + dispatch wiring

#### Automated

- [ ] 1.1 `go build ./...` succeeds
- [ ] 1.2 `mage test` passes — all new + existing tests
- [ ] 1.3 `mage lint` clean
- [ ] 1.4 `TestStatsAggregator_RecordSuccess`
- [ ] 1.5 `TestStatsAggregator_RecordFailure`
- [ ] 1.6 `TestStatsAggregator_TTLEviction`
- [ ] 1.7 `TestStatsAggregator_FallbackTrigger`
- [ ] 1.8 `TestDispatcher_StatsWiring`

#### Manual

- [ ] 1.9 Send request that triggers fallback; check `/v1/stats/routing`

### Phase 2: Endpoint + template rendering

#### Automated

- [ ] 2.1 `go build ./...` succeeds
- [ ] 2.2 `mage test` passes — all new + existing tests
- [ ] 2.3 `mage lint` clean
- [ ] 2.4 `TestStatsEndpoint_ReturnsJSON`
- [ ] 2.5 `TestStatsEndpoint_EmptyWhenNoStats`
- [ ] 2.6 `TestMappingRow_PopulatesErrorRate`
- [ ] 2.7 `TestMappingRow_PopulatesFallbackTriggers`
- [ ] 2.8 Regex: step with errors contains `route-step__error-rate`

#### Manual

- [ ] 2.9 Error rate badges appear after failures
- [ ] 2.10 Fallback trigger count appears after fallback
- [ ] 2.11 Error rate tooltip shows last failure time
- [ ] 2.12 Card header shows timeout budget
- [ ] 2.13 `/v1/stats/routing` returns well-formed JSON

<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: Dashboard GUI Redesign

- **Plan**: context/changes/dashboard-redesign/plan.md
- **Scope**: All 8 phases (full plan review)
- **Date**: 2026-08-05
- **Verdict**: REJECTED
- **Findings**: 3 critical, 7 warnings, 0 observations

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | FAIL |
| Scope Discipline | WARNING |
| Safety & Quality | FAIL |
| Architecture | PASS |
| Pattern Consistency | WARNING |
| Success Criteria | FAIL |

## Findings

### F1 — Stats attributed to client model string, not resolved mapping; maps unbounded

- **Severity**: ❌ CRITICAL
- **Impact**: 🔬 HIGH — architectural stakes; think carefully before deciding
- **Dimension**: Safety & Quality / Plan Adherence
- **Location**: proxy/stats_collector.go:116-193 (keyed on `ev.Model`); consumed at proxy/web/handlers.go:119
- **Detail**: `record()` keys `sc.mappings` on the raw request `model` field, which the dispatcher resolved to a mapping by exact name *or by family* (`proxy/proxy.go:131-140`) — the collector never applies that resolution. A request for `claude-sonnet-4-...` through mapping `sonnet` lands under the key `"claude-sonnet-4-..."`, so `mappingStats["sonnet"]` misses and the dashboard renders `Requests: — / No traffic` for a mapping that is actively serving. The same miss hits the drawer, health strip, row status, and attention Rule 5. Compounding: the key is client-controlled and unbounded (no cap/TTL) while every sibling aggregate bounds memory (`LastResponder` 60s TTL, `EventBus`/`LogSink` 10k ring).
- **Fix**: Resolve the mapping name at record time (share `resolveMapping`, or add the resolved name to `RequestEvent` in the middleware) and drop unresolvable names — this also bounds the map; or add lazy TTL/LRU eviction like `LastResponder`.
  - Strength: Fixes both misattribution and unbounded growth in one change; aligns with the lessons.md rule on bounded aggregates.
  - Tradeoff: Requires touching the event-emit path (middleware) — a few files, but low risk.
  - Confidence: HIGH — resolution logic already exists in the dispatcher.
  - Blind spot: Need to confirm `RequestEvent` is the right place vs. resolving inside `record()`.
- **Decision**: FIXED — Added `MappingName` to `RequestEvent` (set via new `X-Freedius-Matched-Mapping` header in the dispatcher at proxy.go:275, read in `EventBusMiddleware`); `record()` keys on `MappingName` and only falls back to `Model` when unmatched. Bounds the map to configured mappings and fixes misattribution. Added `TestStatsCollector_MappingNameResolution`.

### F2 — `RecordFallback` has no production caller; all fallback telemetry is permanently zero

- **Severity**: ❌ CRITICAL
- **Impact**: 🔬 HIGH — architectural stakes; think carefully before deciding
- **Dimension**: Plan Adherence
- **Location**: proxy/stats_collector.go:202-216 (only called by tests); proxy/web/handlers.go:87,146,227
- **Detail**: `grep -rn RecordFallback --include=*.go` returns only the definition and test callers. `FallbackCount` is therefore always 0, which dead-ends the dashboard "Fallbacks" column (index.html:99), health-strip `FallbacksLast24h` (handlers.go:87), drawer "Fallback Events" (mapping-drawer.html:40), and attention Rule 5 (`attention.go:93`, gated on `ms.FallbackCount > 0`). The plan's headline goal #3 — "What fallback will be used if the primary fails?" — is unanswerable from the UI, and `activityRow.FallbackUsed` is hardcoded `false // TODO: enrich from LastResponder` (handlers.go:224).
- **Fix**: Call `RecordFallback(name)` where `LastResponder.Record` is invoked with a non-zero responder index (the fallback signal already exists), or drop the columns until the signal exists.
  - Strength: Reuses the existing `LastResponder` signal — no new instrumentation needed.
  - Tradeoff: Requires locating the responder-record call site in the proxy path.
  - Confidence: HIGH — `LastResponder.Record` is the fallback indicator.
  - Blind spot: None significant.
- **Decision**: FIXED — Added `Stats *StatsCollector` to `Dispatcher` (nil-safe, like `LastResponder`), wired in main.go:161, and call `d.Stats.RecordFallback(mappingName)` at the fallback-success site (proxy.go:357) alongside `LastResponder.Record`. Fallback telemetry now increments in production.

### F3 — Live SSE activity feed is dead code; plan's core "live activity" goal unmet

- **Severity**: ❌ CRITICAL
- **Impact**: 🔬 HIGH — architectural stakes; think carefully before deciding
- **Dimension**: Plan Adherence / Success Criteria
- **Location**: proxy/web/templates/index.html:140,181-211; ext behavior in proxy/web/static/htmx.min.js
- **Detail**: `#activity-feed` carries only `hx-ext="sse" sse-connect="/v1/events"` — no `sse-swap` attribute and no `hx-trigger="sse:event"`. The bundled htmx SSE extension only attaches an `EventSource` listener (and dispatches `htmx:sseMessage`) when one of those attributes is present, so the ~30-line handler at index.html:181 never fires. The `EventSource` *is* opened and `handleEvents` replays the full 10k ring on connect — every dashboard tab streams 10k events that are then discarded. Plan criterion 2.9 ("Activity feed updates via SSE without page refresh") and goal #5 are unmet. The E2E spec `e2e/tests/activity-feed.spec.ts` only asserts the container is *visible*, so it cannot catch this.
- **Fix**: Add `hx-trigger="sse:event"` (or `sse-swap="message"`) to `#activity-feed`, and pass `?since=` so connects don't replay the ring; update E2E to assert a row appears after a proxied request.
  - Strength: One-attribute fix restores the feature; E2E tightening prevents regression.
  - Tradeoff: Minor — attribute + a small E2E change.
  - Confidence: HIGH — confirmed the extension dispatch condition by reading htmx.min.js.
  - Blind spot: Verify the SSE payload field names match what the handler reads (`data.Model`, `data.MatchedProvider`, etc.).
- **Decision**: FIXED — Added `hx-trigger="sse:event"` to `#activity-feed` (extension now attaches the listener) and passed `?since={{.CurrentSeq}}` (new `EventBus.CurrentSeq()` + `dashboardData.CurrentSeq`) so the connect skips the buffered ring replay. Removed the broken `e.detail.type !== 'event'` guard in the handler (`e.detail` is a `MessageEvent`, so that guard killed every event). Added an E2E regression guard for the SSE wiring attributes.

### F4 — `hx-*` attributes allow URL-path traversal / destructive-action retargeting

- **Severity**: ⚠️ WARNING
- **Impact**: 🔬 HIGH — architectural stakes; think carefully before deciding
- **Dimension**: Safety & Quality
- **Location**: proxy/web/templates/providers-table.html:72,87; index.html:86; mappings-routing-table.html (hx-* paths)
- **Detail**: `html/template` context-aware-escapes only recognized URL attributes (`href`, `src`); custom attributes like `hx-get`/`hx-post`/`hx-delete` are emitted as plain text with HTML-escaping only. Provider/mapping names only forbid CR/LF/`:`/`%` (`forms.go`), so `/`, `..`, `?`, `#` are legal. A provider named `x/../mappings/prod` renders `hx-delete="/v1/providers/x/../mappings/prod"`, which the browser normalizes to `DELETE /v1/mappings/prod` — the confirm dialog says "Delete provider", the server deletes a *mapping*. No XSS (values are HTML-escaped), but destructive-action retargeting. Combined with F5, an external page can drive this.
- **Fix**: Add a `urlPath` template func (`url.PathEscape`) and use `{{.Name | urlPath}}` in every `hx-*` path (the JS side already does `encodeURIComponent` in `confirmDeleteMapping`).
  - Strength: Mirrors the existing JS escaping; removes the traversal class entirely.
  - Tradeoff: Touches several templates.
  - Confidence: HIGH — html/template does not URL-escape unknown attrs (documented behavior).
  - Blind spot: Verify no template already relies on literal `/` in names for routing.
- **Decision**: FIXED — Added a `urlPath` template func (`url.PathEscape`) to `templateFuncs` (embed.go) and applied `{{.Name | urlPath}}` to the three `hx-*` URL paths (`index.html:86`, `providers-table.html:72,87`). Also escaped `ds.name` with `encodeURIComponent` in the JS `editMapping` (`mappings.html:121`) to close the same traversal class on the JS side.

### F5 — No CSRF/Origin check on mutating `POST /v1/*` endpoints (incl. outbound Test Connection)

- **Severity**: ⚠️ WARNING
- **Impact**: 🔬 HIGH — architectural stakes; think carefully before deciding
- **Dimension**: Safety & Quality
- **Location**: proxy/web/handlers.go:291-293 (POST /v1/providers/{name}/test); SetupMux routing
- **Detail**: `AuthToken` is empty by default and the UI binds 127.0.0.1. A cross-origin `<form method=post>` is a *simple* request (no preflight, no token). Any page the operator visits can make freedius issue an outbound GET to a configured base URL via the new Test Connection endpoint, and (with the unprotected CRUD endpoints) even create a provider and probe it. This change adds the first mutating endpoint that performs an **outbound network call** on POST.
- **Fix**: Reject mutating `/v1/*` requests whose `Sec-Fetch-Site` ≠ `same-origin` (or whose `Origin` doesn't match the listener) via one `SetupMux` middleware.
  - Strength: One middleware closes the whole class of CSRF gaps.
  - Tradeoff: Needs care so HTMX same-origin requests still pass (they send `Sec-Fetch-Site: same-origin`).
  - Confidence: HIGH — HTMX requests are same-origin by default.
  - Blind spot: Confirm the SSE/GET endpoints remain open as intended.
- **Decision**: FIXED — Added `csrfGuard` middleware (handlers.go) that rejects mutating `/v1/*` requests whose `Sec-Fetch-Site` is not `same-origin` (falling back to an `Origin`-vs-listener host check). Wrapped the mux in `NewServer` so it applies even with an empty `AuthToken`. Added `TestCSRFGuard` (8 cases).

### F6 — Keyboard navigation and focus-return for the routing table/drawer are broken

- **Severity**: ⚠️ WARNING
- **Impact**: 🔬 HIGH — architectural stakes; think carefully before deciding
- **Dimension**: Plan Adherence / Accessibility
- **Location**: proxy/web/static/app.js:71-75,126; proxy/web/templates/index.html:71,85
- **Detail**: (a) The keyboard-nav handler is gated on `evt.target.closest('table[role="grid"]')`, but the routing table is `<table class="routing-table">` with **no `role="grid"`** — so arrow-key nav and Enter/Space-to-open are entirely no-ops (`<tr role="button">` has no native Enter activation). (b) Focus capture listens on `htmx:beforeRequest` and checks `evt.target.id === 'mapping-drawer'`, but `beforeRequest` fires on the *requesting* element (the `<tr>`), never the target, so `drawerOpener` stays null and `closeDrawer` silently skips focus restore. Plan criteria 3.6/3.7/7.5 unmet; `e2e/tests/drawer.spec.ts` never asserts focus return.
- **Fix**: Add `role="grid"` (and `role="row"`/`gridcell`) to the table, or put a real `<button>`/`<a>` in the first cell and drop `role="button"` from `<tr>`; fix focus capture to inspect `evt.detail.target.id` (or capture on the row's click).
  - Strength: Restores the documented keyboard/focus contract.
  - Tradeoff: Minor — markup + a few lines of JS.
  - Confidence: HIGH — verified both selectors against the templates and event semantics.
  - Blind spot: Test with a screen reader to confirm `grid` role announcement.
- **Decision**: FIXED — Added `role="grid"` to the dashboard routing table (`index.html:71`) so the keyboard-nav handler now matches and arrow keys / Enter / Space operate on rows. Fixed focus capture in `app.js` to read `evt.detail.target.id === 'mapping-drawer'` (htmx fires `beforeRequest` on the *requesting* element, not the swap target), so focus now returns to the opening row on close.

### F7 — "Key Missing" status yields a broken CSS class and fires for keyless providers

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Correctness / Plan Adherence
- **Location**: proxy/web/handlers.go:125-133,857-858; proxy/web/templates/index.html:97; mapping-drawer.html:11
- **Detail**: The badge class is built as `badge--status-{{.StatusLabel | lower}}`. With `StatusLabel = "Key Missing"` this emits `class="badge badge--status-key missing"` — two bogus classes; `app.css` defines only `badge--status-{healthy,degraded,error,unknown,...}`, so the badge renders with **no styling**. Separately, `envPresent` starts `false` and is only set when `p.DefaultAPIKeyEnv != ""`, so a keyless local provider (ollama, llama.cpp) is *permanently* flagged "Key Missing"/"Inactive" and is excluded by the new `?status=active` filter — while `computeAlerts` Rule 1 correctly stays silent. Dashboard row and attention panel disagree.
- **Fix**: Derive the class from a slug field (`StatusSlug string`) instead of `lower`-ing a display label, and treat "no env var declared" as not-a-problem (match `attention.go`).
  - Strength: Removes both the broken-class bug and the false-positive for keyless providers.
  - Tradeoff: Adds a slug field to the row types.
  - Confidence: HIGH — confirmed the class string and the envPresent logic.
  - Blind spot: None significant.
- **Decision**: FIXED — Added `StatusSlug` to `routingTableRow` and `drawerData`; templates now use `badge--status-{{.StatusSlug}}`. New `mappingStatus(ms, envDeclared, envPresent)` helper only flags "Key Missing" when the provider *declares* an API-key env var that is missing, so keyless providers (ollama/llama.cpp) are no longer false-flagged and stay in the `?status=active` filter. Added `badge--status-key-missing` CSS (error styling). This now matches `attention.go` Rule 1.

### F8 — Two divergent provider-status derivations with inverted thresholds

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Correctness / Pattern Consistency
- **Location**: proxy/web/handlers.go:167-176 (dashboard) vs :470-481 (providers page)
- **Detail**: Dashboard badge: `RecentErrorRate > 0.5 → "error"`, `> 0.2 → "degraded"`. Providers page: `> 0.5 → "degraded"`, else `LastError.After(LastSuccess) → "error"`. The same provider renders **Error** on `/` and **Degraded** on `/providers`. The doc comment on `deriveProviderStatus` claims "last 3 consecutive errors → error", which the code never computes, and `renderProvidersTable` duplicates `handleProviders`'s ~30-line enrichment block.
- **Fix**: Extract one exported `deriveProviderStatus` helper used by both call sites; reconcile the thresholds to a single documented rule.
  - Strength: Single source of truth; removes the contradiction operators see across pages.
  - Tradeoff: Minor refactor of two handler sites.
  - Confidence: HIGH — both derivations read directly from the same snapshot.
  - Blind spot: Decide the canonical threshold (error-rate vs. last-error-after-success).
- **Decision**: FIXED — Replaced the dashboard health-summary inline switch with a call to the existing `deriveProviderStatus` helper (handlers.go), so both `/` and `/providers` now use one rule: `>0.5 → degraded`, `LastError.After(LastSuccess) → error`, else healthy/unknown. Removed the misleading "last 3 consecutive errors" implication from the doc comment. Existing `TestProviderStatus_*` tests still pass.

### F9 — `handleTestConnection` HTTP client leaks/diverges; dashboard copies full event ring per load

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Safety & Quality / Performance
- **Location**: proxy/web/handlers.go:1320-1346 (Test Connection); :203-207 (dashboard activity)
- **Detail**: (a) `handleTestConnection` builds a fresh `http.Transport` **per request** with no `IdleConnTimeout` (custom transports default to *no* idle timeout) and never `CloseIdleConnections()` — each "Test" click parks an idle connection until the upstream drops it. It also omits `Proxy: http.ProxyFromEnvironment` (so it fails behind a corporate proxy where real traffic works) and follows up to 10 redirects (a provider URL can bounce the reachability probe to an unrelated host). Sibling `proxy.FetchModels` does none of these. (b) The dashboard handler does `h.Bus.Since(0)`, copying the entire 10k-event ring (~1.6 MB) and holding `ringMu.RLock` on every page load, briefly stalling event emission on the request path.
- **Fix**: Use a package-level `testClient` reusing one transport with `Proxy: http.ProxyFromEnvironment` and `CheckRedirect: return http.ErrUseLastResponse`; drain with `io.CopyN`. For (b), add a `Recent(n int)` accessor (or `Since(currentSeq-20)`) that copies only the tail.
  - Strength: Matches the existing `FetchModels` client pattern; removes per-load 1.6 MB copy.
  - Tradeoff: Minor — two small, localized changes.
  - Confidence: HIGH — both patterns are present in the codebase already.
  - Blind spot: None significant.
- **Decision**: FIXED — `handleTestConnection` now uses a package-level `testClient` that reuses one `http.Transport` with `ProxyFromEnvironment`, an `IdleConnTimeout`, and `CheckRedirect: ErrUseLastResponse` (no more leaked idle connections or proxy-blindness or redirect-chasing). Added `EventBus.Recent(n)` and switched the dashboard activity loop from `Since(0)` (full 10k-ring copy under `ringMu.RLock` per load) to `Recent(20)` (tail-only copy). All 423 proxy+web tests pass.

### F10 — Phase 7 CSS/accessibility polish defects (reduced-motion, backdrop, responsive columns)

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Plan Adherence
- **Location**: proxy/web/static/app.css:2352-2360,2108-2125,2341-2348
- **Detail**: (a) The `prefers-reduced-motion` block forces `transform: translateX(0)` on the **base** `.drawer` (which is `position: fixed; right: 0; width: 400px` with no visibility/display fallback), so with reduced-motion the empty drawer panel is **permanently on-screen**, covering the right 400px of every page. (b) `.drawer-overlay` / `.drawer-overlay--visible` are defined but no markup/JS uses them — the drawer opens with no backdrop. (c) The ≤480px rule hides `nth-child(5)` and `nth-child(7)` (Requests, Last Activity); the plan specified hiding **fallback count** (col 6) and Last Activity. (d) Status badges carry inline SVG icons only on the Providers page; dashboard/drawer badges are text-only (a11y intent survives since text is present, but the stated contract differs).
- **Fix**: In the reduced-motion block, keep `.drawer` off-screen (`transform: translateX(100%)` or `visibility:hidden`) and only neutralize the transition; add the overlay element + show class; correct the responsive `nth-child` targets.
  - Strength: Restores the documented reduced-motion and responsive behavior.
  - Tradeoff: CSS-only, low risk.
  - Confidence: HIGH — selectors confirmed against the templates.
  - Blind spot: Verify overlay z-index vs. drawer.
- **Decision**: FIXED — (a) Reduced-motion block no longer forces `transform: translateX(0)` on the base `.drawer`, so the panel stays off-screen by default and only the slide transition is disabled. (b) Added `<div class="drawer-overlay" id="drawer-overlay">` to the dashboard and wired `openDrawer`/`closeDrawer` (and an overlay click) to toggle `drawer-overlay--visible`, so the drawer now has a working backdrop. (c) Mobile (≤480px) rule now hides columns 6 (Fallback count) and 7 (Last Activity) per the plan, instead of 5 and 7. (d) Dashboard/drawer badges remain text-only (Providers page has icons); acceptable since text satisfies the no-color-only requirement.

═══════════════════════════════════════════════════════════
  TRIAGE COMPLETE
═══════════════════════════════════════════════════════════

  Fixed:     F1, F2, F3, F4, F5, F6, F7, F8, F9, F10   (10)

═══════════════════════════════════════════════════════════

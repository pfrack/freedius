# Web UI Friendliness Improvements — Plan Brief

> Full plan: `context/changes/web-ui-friendliness/plan.md`
> Research: `context/changes/web-ui-friendliness/research.md`

## What & Why

The breadcrumb-chain mapping cards shipped in V-02d (`d6f1930`) are functional but minimal — color-only role signal (WCAG 1.4.1 violation), no protocol disclosure per step, and four PENDING findings from the impl-review still live in the code. A separate codebase scan surfaced 30+ cross-page UX gaps: silent CRUD errors, broken `hx-confirm` copy (`Delete mapping 'foo?`), zero loading indicators, zero empty-state copy, no toast feedback. This plan ships three independently-mergeable phases that close all of the above without breaking the existing HTMX swap contract or the single-binary constraint.

## Starting Point

- `/mappings` renders cards with primary (green) + N fallbacks (amber) connected by `▶` chevrons. Render path: [`handleMappings`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/handlers.go#L193) → [`renderPage`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/embed.go#L96) → [`mappings.html`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings.html) → [`mappings-table.html`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings-table.html).
- HTMX swap contract: `hx-target="#mappings" hx-swap="outerHTML"` (must not change).
- Design system: zinc dark palette + indigo accent; BEM-ish classes; `--color-*`/`--accent*`/`--space-*`/`--radius-*`/`--shadow-*` tokens at [`app.css:7-70`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/static/app.css#L7) — no new hex values introduced.
- All 4 PENDING impl-review findings (F1–F4) verified still present in code at commit `d6f1930`; `FallbacksString()` and `fallbackEntry.String()` are dead code (zero callers).
- Bonus regression surfaced by research: Edit → Cancel → Add → Save silently fails because `editMapping` rewrites `hx-put` and there is no reset path.
- Validation already returns per-field JSON via [`forms.go:13-30`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/forms.go#L13) (`ValidationError{Fields: map[string]string}`); the UI drops those errors today.

## Desired End State

A user opening the freedius dashboard at `:8083` sees:

- **`/mappings`**: each card has accessible role labels (Primary / Fallback N — color-independent), per-step protocol badges, a fallback depth pill, hover tooltips with the underlying base URL, and clickable steps that jump to filtered logs. A sustained-pulse chevron marks the step that answered the most recent successful request (auto-fades after 60s).
- **All pages**: every CRUD operation shows visible feedback — in-form error messages on validation failures (using the existing scaffolded `#*-form-error-dialog` slots), a global toast for non-form failures, a spinner during slow saves. Empty lists show a CTA to add the first item. Delete-confirm dialogs read grammatically.
- **`/logs?min=error`**: filter survives reload; SSE events below the threshold do not render.

Verification: load `/mappings` and exercise every CRUD path; every failure is now visible somewhere on the page.

## Key Decisions Made

| Decision | Choice | Why (1 sentence) | Source |
| --- | --- | --- | --- |
| PR slicing | Bugfix → Polish → Cross-page UX | Production bugs ship fastest; each phase is independently reviewable | Plan |
| F1 fix path | Fix A — drop `\| js` + rewrite `addFallbackRow` to `createElement`+`.value` | Matches Go `html/template` context-aware escaping; closes adjacent XSS-shaped concat | Research §D.10 / Round 2 |
| Last-responder highlight | Ship in Phase 2 (minimal aggregator) | Existing "fallback succeeded" log path supplies the data; avoids per-request O(N) ring-buffer scan | Research §G.1 / Round 2 |
| Silent-error feedback | Global toast + per-dialog inline error (read existing `fields` JSON) | Closes the worst UX bug and reuses scaffolded divs | Round 2 |
| Tests | Handler tests + 1 JS smoke test | "Embrace Extra Tests" lesson; ~5 focused tests; matches existing handler-test surface | Round 2 |
| Stale `hx-put` bug | Bundle with Phase 1 bugfix PR | Same surface area (dialog JS); ~15 LOC; 5 bugs vs 4 | Round 1 |
| Breadcrumb MVP | All 6 visual items (D.1, D.2, D.3, D.5, D.12) | Avoids leaving visual richness for later half-ships | Round 1 |
| Per-step click → filtered logs | In scope as Phase 2 polish item | Closes research §D.4 (breadcrumb as navigation); makes `/logs` the routing-evidence single source | Round 1 |
| What stays out | SVG graph view, drag-and-drop, view toggle, a11y audit, client-side search, dashboard drill-downs, theme toggle | All explicitly out-of-scope across V-02b/V-02d; research §F prevents re-investigation | Research §F |

## Scope

**In scope:**

- Close 4 PENDING impl-review findings (F1–F4) + bonus stale-`hx-put` bug + dead-code cleanup.
- Breadcrumb visual polish: protocol badge, a11y aria-label, fallback depth pill, chevron `:first-child` fix, hover tooltip, click-through to filtered logs.
- Last-responder chevron (subscriber + endpoint + render + CSS).
- Cross-page silent-error wiring (global `htmx:afterRequest` listener + toast region + populate existing inline error slots).
- Empty-state copy on Providers, Mappings, Logs.
- Loading indicators on all write buttons (`htmx-indicator` + `hx-disabled-elt`).
- ~13 focused handler tests covering the regressions above.

**Out of scope:**

- SVG graph visualization of mappings (V-02d deferred).
- Drag-and-drop reordering of fallbacks (V-02d).
- View toggle (table vs card).
- Full accessibility audit (V-02b out-of-scope; only the WCAG 1.4.1 closure rides along).
- Client-side search/filter on any list page.
- Dashboard drill-downs (stat cards linking to `/logs` etc.) — parked under "Monitoring dashboard".
- Live SSE-driven dashboard refresh.
- JS framework, CSS preprocessor, or build pipeline.
- New design tokens / new hex values.
- Changes to `config.Mapping` YAML schema.
- Static asset cache invalidation — accept `Cache-Control: max-age=300` during dev.

## Architecture / Approach

Three phases, each one Go-side touches only `proxy/web/{types.go,handlers.go,templates/,static/app.css}`. No new packages, no new dependencies. Phase 2's last-responder work adds a small aggregator type (could live inline in `proxy/logtee.go` as a `sync.Map`-backed struct) that the dispatch path calls directly — never via O(N) log-sink scan. Phase 3's silent-error wiring uses one `htmx:afterRequest` listener on `document.body` that introspects `event.target` to decide between inline form-error vs toast.

The data-model delta is contained: `fallbackEntry` and `mappingRow` gain `Protocol string` + `BaseURL string`; `modelsData` gains `Truncated bool` + `FetchInProgress bool`. No config YAML change. No `config.Mapping` change.

## Phases at a Glance

| Phase | What it delivers | Key risk |
| --- | --- | --- |
| 1. Bug fixes | F1–F4 + stale-`hx-put` reset + dead-helper cleanup + ~5 regression tests | F1's `addFallbackRow` rewrite is the largest single JS edit (~30 lines), but it's mechanical |
| 2. Breadcrumb polish | Protocol badge + a11y aria + depth pill + tooltip + click-to-filtered-logs + last-responder | Last-responder needs the aggregator subscribed from the dispatch path's log emit — easy to forget the call site |
| 3. Cross-page UX | Global `htmx:afterRequest` listener wiring inline errors + toast region + empty-state copy on 3 pages + loading indicators | Existing inline-error divs must be populated correctly; listener must not double-fire on already-empty responses |

**Prerequisites:** None — all work happens in `proxy/web/`, builds on `d6f1930` (already on `main`). The `eventstream.Handlers` instance already exists with a `Bus` + `LogSink`.
**Estimated effort:** ~2-3 sessions for one engineer across 3 PRs. Phase 1 ~half day; Phase 2 ~1.5 sessions; Phase 3 ~1 session. ~13 new handler tests, ~80 LOC of CSS, ~250 LOC of Go + JS.

## Open Risks & Assumptions

- **Last-responder accuracy**: depends on the dispatch path's existing `"fallback succeeded"` log line being emitted on every successful fallback. If a future code path bypasses this log line (e.g. caching), the chevron will be wrong/missing. Tests cover the happy path only.
- **`addFallbackRow` rewrite scope**: the rewrite touches a function called only from `editMapping`. No other call site — verified by grep. Low risk of cascading test fallout.
- **Toast region collisions**: the existing `.z-index` of `100` (sidebar) / `200` (hamburger) is below the new toast `9999`. If anything else ever wants higher z-index, this could pile up.
- **Phase 2's `handleLogs` filter is substring-match on raw log lines** (case-insensitive). Provider names containing regex special chars are unlikely but worth flagging; the match is plain `strings.Contains`, not regex.
- **`server.go:39-41` enables an optional `FREEDIUS_UI_TOKEN`**. The Phase 3 global listener MUST skip 401s when the token is unset (no-op case) and surface them in toast when auth IS required.

## Success Criteria (Summary)

- All 4 PENDING impl-review findings verified fixed via new handler tests.
- New `TestMappingsTable_F1_RoundTrip` passes for provider names with `'` and `"`.
- Edit → Cancel → Add Mapping → Save cycle submits as POST (Phase 1.13 manual).
- Each `.route-step` has `aria-label` and `role="listitem"` (regex-asserted in test).
- Clicking a step navigates to `/logs?provider=&mapping=` with the right subset visible.
- A 1500-model provider renders exactly 1000 `<li>` + the truncation notice.
- Reloading `/logs?min=error` preserves the filter chip and URL.
- A submission with an invalid field shows the per-field error inside the dialog.

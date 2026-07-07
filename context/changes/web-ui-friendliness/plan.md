# Web UI Friendliness Improvements — Implementation Plan

## Overview

Polish the breadcrumb-chain `/mappings` cards shipped in V-02d (`d6f1930`) and close the cross-page UX gaps surfaced by [`context/changes/web-ui-friendliness/research.md`](../web-ui-friendliness/research.md). Three independently-shippable phases:

- **Phase 1**: Bug fixes — close the 4 PENDING impl-review findings (F1–F4) and the bonus stale-`hx-put` dialog bug; delete dead helpers.
- **Phase 2**: Breadcrumb polish — protocol badge, aria-label, fallback depth, chevron cleanup, hover tooltip, click-through to filtered `/logs`, last-responder highlight.
- **Phase 3**: Cross-page UX — wire silent validation errors to existing form-error slots + global toast region; empty-state copy on all 3 list pages; loading indicators on write buttons.

The change inherits every constraint documented in research §E/F (HTMX-only; design tokens only; BEM-ish classes; no SVG graph view; no JS framework; no a11y audit), and respects the prior lessons in `context/foundation/lessons.md` (notably "Embrace Extra Tests" — keep tests beyond the plan's enumerated set, and "json.Marshal over json.NewEncoder" — applies to the F1 fix and `addFallbackRow` rewrite).

## Current State Analysis

- The breadcrumb-chain cards are implemented in [`proxy/web/templates/mappings-table.html`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings-table.html) and rendered by [`renderMappingsTable`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/handlers.go#L305) / [`handleMappings`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/handlers.go#L193). HTMX swap contract: `hx-target="#mappings" hx-swap="outerHTML"` at [`mappings.html:16-17`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings.html#L16) and [`mappings-table.html:22-23`](https://github.com/pfrack/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings-table.html#L22) (corrected permalink) — must keep working.
- CSS for the chain lives at [`proxy/web/static/app.css:750-828`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/static/app.css#L750). Reusable tokens already declared at `:root` (lines 7-70): `--color-success`, `--color-warning`, `--color-error`, `--accent`, `--accent-subtle`, `--bg-card`, `--bg-surface`, `--text-muted`, `--space-1..8`, `--radius-sm..xl`, `--shadow-sm/md/lg`, `--sidebar-width`, `--font-sans`, `--font-mono`.
- 4 PENDING findings from [`reviews/impl-review.md`](https://github.com/pfrack/freedius/blob/d6f1930a/context/changes/mapping-graph-visualization/reviews/impl-review.md) all verified still present in the code at commit `d6f1930`. Plus 1 bonus bug (stale `hx-put` after Edit→Add cycle, surfaces in `editMapping` at [`mappings.html:65-89`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings.html#L65)).
- Validation already returns per-field JSON ([`forms.go:13-30`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/forms.go#L13) — `ValidationError{Fields: map[string]string}`), but the UI drops those errors today.
- `models-fragment.html:1-19` is the only template with both empty-state copy AND a user-visible error block; everything else is silent.
- `mappingRow.FallbacksString()` and `fallbackEntry.String()` at [`types.go:62-82`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/types.go#L62) have zero callers outside their definitions — safe to delete.

### Key Discoveries:

- **Edit dialog reuses the same `<form>` element for both Add and Edit by JS-rewriting `hx-post`/`hx-put`.** No reset path exists, so Edit → Cancel → Add silently submits to the wrong endpoint ([`mappings.html:65-89`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings.html#L65)). Fix lives next to `editMapping`.
- **HTMX 2.0's default `responseHandling` ([htmx.min.js:8](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/static/htmx.min.js)) drops 4xx/5xx JSON bodies entirely** — so `writeValidationError`/`writeJSONError` responses are never surfaced today. The empty `id="mapping-form-error-dialog"` and `id="provider-form-error-dialog"` divs were scaffolded for exactly this.
- **`handleLogs` currently only filters by `?min=`** ([`handlers.go:96-155`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/handlers.go#L96)). Extending it for `?provider=` and `?mapping=` is straightforward (substring match on the rendered line); the existing ring buffer [`logtee.go`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/logtee.go) is the data source.
- **Last-responder data already exists** in the dispatch path at [`proxy/proxy.go:326-335`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/proxy.go#L326) — `"fallback succeeded", "attempt", i, "provider", …` slog line. Aggregating from the LogSink is one consumer (in-memory `sync.Map` + 60s TTL). O(N) scan over the ring buffer is NOT acceptable per research §G.1 — must subscribe to events, not re-scan.
- **`serveStatic` sets `Cache-Control: public, max-age=300`** ([`embed.go:90-93`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/embed.go#L90)). CSS changes can be masked 5 min — DX sharp edge during dev; not changing this but flagging for testing.

## Desired End State

A user opening the freedius dashboard at `:8083` sees:

- A `/mappings` page where each mapping's pipeline is a card with **color-independent role labels** ("Primary" / "Fallback N"), the underlying **protocol** for each step, a depth indicator (e.g. "primary + 2 backups"), per-step **click-through to filtered logs**, and a **sustained-pulse chevron** on the step that actually answered the most recent successful request.
- A `/mappings` page where **destructive actions are confirmed with grammatical copy**, **dialog errors are always shown in-form**, **empty mappings show a clear "Add your first mapping" CTA**, and **submit buttons show a spinner during the (occasionally slow) `cfg.SaveData`** round-trip.
- A `/providers` page with matching affordances: grammatical confirm, empty CTA, in-form errors, loading spinner.
- A `/logs` page where the level dropdown has a visible "Showing X+" chip with a × clear control, the URL records the active filter, the empty state says "Waiting for log events…", and incoming SSE messages respect the level filter.
- A shared toast region at the page level for any HTMX 4xx/5xx from non-form swaps (deletes, fetches), driven by one global `htmx:afterRequest` listener.

Verification: load `/mappings` in a browser, see the polished cards. Try every CRUD operation; every failure is now visible somewhere. Reload on `/logs?min=error` — filter survives. Open a mapping with a single quote in a provider name → edit dialog pre-populates correctly (closes F1).

## What We're NOT Doing

- **No SVG graph view** of the mappings (deferred from V-02d, per [`mapping-graph-visualization/plan.md:38`](https://github.com/pfrack/freedius/blob/d6f1930a/context/changes/mapping-graph-visualization/plan.md#L38)). The last-responder chevron is the closest visual affordance we'll add.
- **No drag-and-drop reordering** of fallback chains (V-02d §What We're NOT Doing).
- **No view toggle** (table vs graph) — full replacement of the historical `<table>` is final.
- **No accessibility audit** — explicit out-of-scope across V-02b and V-02d plans. The Phase 2 aria-label fix is a *well-scoped* WCAG 1.4.1 closure that ships inside Phase 2 without re-opening the audit.
- **No client-side filter/search** on `/providers`, `/mappings`, or `/logs` tables. Research §D explicitly deferred these; a separate slice can tackle them.
- **No dashboard drill-downs** (Total Logs stat card linking to `/logs`, etc.) — parked under "Monitoring dashboard" in [`roadmap.md:233-238`](https://github.com/pfrack/freedius/blob/d6f1930a/context/foundation/roadmap.md#L233).
- **No live SSE-driven dashboard refresh** — same parked item.
- **No JS framework, no CSS preprocessor, no build pipeline** — HTMX + inline JS only; pure CSS with custom properties.
- **No new design tokens** (no new color hex values); reuse `--color-*`, `--accent*`, `--bg-*`, `--text-*`, `--border-*`, `--space-*`, `--radius-*`, `--shadow-*` only.
- **No config.Mapping data-model changes** — all Go changes are local to `proxy/web/types.go` and `proxy/web/handlers.go`; YAML schema unchanged.

## Implementation Approach

One Go-side slice per bug or per data-model addition; one HTML/template slice per visual item or JS wiring. Tests ride alongside each item — handler-level coverage for Go changes, byte-level string assertions for template/HTML regressions where static analysis suffices, and DOM smoke tests via `jsonschema`-style assertions on `event.detail.xhr.response` for the silent-error wiring.

Both problem and solution sit inside the existing `proxy/web/` package. No new packages, no new dependencies. Phase boundaries are chosen so each phase is independently reviewable and revertable.

Static asset caching note: CSS additions in Phase 2/3 ride the existing `Cache-Control: public, max-age=300`. Manual testers should hard-reload (Cmd-Shift-R / Ctrl-Shift-R) or temporarily append `?v=<sha>` to `/static/app.css` during dev.

## Critical Implementation Details

- **F1 + `addFallbackRow` are one fix.** The `data-fallbacks` `| js` problem and the `innerHTML` string-concat for building fallback rows are the same anti-pattern (applying the wrong encoding at the wrong layer). Drop `| js` AND rewrite `addFallbackRow` to `createElement` + `.value` assignments in the same Phase 1.1 patch. Verified in research §D.10.
- **Last-responder MUST pre-aggregate, never scan.** Subscribe to `"fallback succeeded"` log entries in `eventstream/handlers.go` and update a `sync.Map[string]int64{providerName: ts-of-last-success}`. `renderMappingsTable` looks up the map (O(1)) to determine the responder index for a mapping. TTL eviction via a periodic goroutine or piggybacked update timestamps.
- **`<dialog>` + inline `<script>` live OUTSIDE `#mappings`**, so HTMX swaps of `.mappings-grid` never reset dialog state. Phase 3 wiring of `htmx:afterRequest` MUST use event delegation on `document.body`, not direct listeners on the (recreated) grid.
- **The existing `id="mapping-form-error-dialog"` and `id="provider-form-error-dialog"` divs are the canonical home for inline errors.** Phase 3 must populate these (read from `event.detail.xhr.response`, parse JSON for `fields` map, render key/value pairs). Do not invent new IDs.
- **The dialog stale-`hx-put` bug fix must land with Phase 1** because it's a regression from the same session (Edit → Cancel → Add cycle). Not allowed to slip into Phase 2 or 3.
- **`server.go:39-41` enables an optional auth token via `FREEDIUS_UI_TOKEN`.** Phase 3's global `htmx:afterRequest` listener MUST NOT intercept auth challenges (401) when the token is unset; in that case there is no auth, so all 4xx/5xx are genuine validation/server errors and should be surfaced.

---

## Phase 1: Bug fixes

### Overview

Close the 4 PENDING impl-review findings (F1–F4) plus the bonus stale-`hx-put` dialog bug and remove the dead `FallbacksString`/`fallbackEntry.String()` helpers. All Go tests must stay green; existing handler tests should remain unchanged unless they touch the affected fragments.

### Changes Required:

#### 1. F1: `data-fallbacks` `| js` removal + `addFallbackRow` rewrite

**File**: `proxy/web/templates/mappings-table.html` and `proxy/web/templates/mappings.html`

**Intent**: Stop double-escaping the JSON `data-fallbacks` attribute (F1) and stop building fallback-row markup via `innerHTML` string-concat (XSS-shaped adjacency).

**Contract**: The rendered Edit button on each card carries `data-fallbacks="..."` (double quotes) with raw JSON from `json.Marshal` (PascalCase keys: `ProviderName`, `Model`). `editMapping(btn)` reads `btn.dataset.fallbacks`, `JSON.parse`s it, and for each entry creates one fallback row via `document.createElement` + `.value` assignment (no string concatenation into HTML). The dataset round-trip must work for provider names containing `'` and `"`.

#### 2. F2: Server-side truncation of upstream models list

**File**: `proxy/web/handlers.go` and `proxy/web/templates/models-fragment.html` and `proxy/web/types.go`

**Intent**: Cap the model-list DOM at 1000 entries server-side; replace the misleading "Truncated at 1000 models" message that the current template appends unconditionally.

**Contract**: After `proxy.FetchModels(...)` returns in `handleRefreshModels`, if `len(models) > 1000`, truncate to the first 1000 and set `modelsData.Truncated = true`. `models-fragment.html` renders `{{if .Truncated}}` (only when the flag is set) the truncation notice. No template change in `modelsData` lookup sites.

#### 3. F3: Unbalanced `hx-confirm` quotes on both delete buttons

**File**: `proxy/web/templates/mappings-table.html` and `proxy/web/templates/providers-table.html`

**Intent**: Fix copy `Delete mapping '{{.Name}}?` → `Delete mapping '{{.Name}}'?` (and same on providers).

**Contract**: Templates emit grammatically correct quoted copy in `hx-confirm`. No JS change.

#### 4. F4: Refresh-in-progress banner on `/v1/providers/{name}/models/refresh`

**File**: `proxy/web/types.go` and `proxy/web/handlers.go` and `proxy/web/templates/models-fragment.html`

**Intent**: When the user clicks "Fetch models" while another fetch is in flight for the same provider, surface a "Refresh in progress…" hint instead of silently returning stale data.

**Contract**: Add `FetchInProgress bool` to `modelsData`. Set true on the `TryLock()`-failed branch of `handleRefreshModels`. Render `<small class="text-muted">Refresh already in progress…</small>` when set.

#### 5. Bonus: Stale `hx-put` after Edit → Cancel → Add cycle

**File**: `proxy/web/templates/mappings.html` and `proxy/web/templates/providers.html`

**Intent**: Close the regression where Edit → Cancel → Add → Save silently submits to `PUT /v1/mappings/<last-edited-name>` instead of `POST /v1/mappings`.

**Contract**: Introduce `openAddMapping()` and `openAddProvider()` helpers. The Add buttons at [`mappings.html:5`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings.html#L5) and [`providers.html:5`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/providers.html#L5) call these. Helpers: reset `hx-post`/`hx-put` attrs on the form (force `hx-post`), clear `name.readOnly`, reset `name/model_string` to empty, clear the fallback-rows container, set the dialog title text back to "Add", then `showModal()`. The Cancel buttons at [`mappings.html:57`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/mappings.html#L57) and [`providers.html:52`](https://github.com/pfrack/freedius/blob/d6f1930a/proxy/web/templates/providers.html#L52) gain a `form.reset()` call before `close()`.

#### 6. Cleanup: delete dead `FallbacksString` / `fallbackEntry.String`

**File**: `proxy/web/types.go`

**Intent**: Remove the unused `String()` method on `fallbackEntry` and the `FallbacksString()` method on `mappingRow`. Both have zero callers (verified by `rg FallbacksString` — only definitions).

**Contract**: Delete the two methods and the now-unused `"fmt"` and `"strings"` imports in `types.go`. No behavior change.

### Success Criteria:

#### Automated Verification:

- 1.1 `go build ./...` succeeds.
- 1.2 `mage test` (or `go test ./proxy/web/...`) passes with all new and existing tests.
- 1.3 `mage lint` clean.
- 1.4 New `TestMappingsTable_F1_RoundTrip`: load `mappings-table.html` with a fallback entry whose model string contains `'` and `"`; assert the rendered HTML contains a parseable `data-fallbacks="..."` attribute (no `\x27`/`\x22` escape sequences from `| js`).
- 1.5 New `TestHandleRefreshModels_Truncation`: mock `proxy.FetchModels` to return 1500 models; assert the rendered fragment HTML contains exactly 1000 `<li data-model-id=...>` and the truncation notice.
- 1.6 New `TestHandleRefreshModels_InProgress`: hold the inflight lock for provider `nim`; send a second concurrent POST to `/v1/providers/nim/models/refresh`; assert the second response contains `Fetch in progress`.
- 1.7 New `TestHandleRefreshModels_NoTruncationMessage_WhenExactly1000`: assert 1000 models do NOT append the truncation notice.
- 1.8 New `TestStaleHxPutAfterEditCancel`: render the Add Mapping button's onclick, evaluate it, then render Edit's onclick, evaluate it, then re-evaluate Add's — assert the form's `hx-put` attribute is gone.

#### Manual Verification:

- 1.9 Open `/mappings`; create a mapping with provider name containing a single quote (e.g. `O'Reilly`); click Edit; confirm the dialog pre-populates correctly with all fallback rows.
- 1.10 Open the same page; create a mapping; click Delete in two successive cards; both `hx-confirm` dialogs show grammatically correct copy.
- 1.11 Open `/providers`; trigger Fetch models on a provider that has 1500+ upstream models; confirm only 1000 are rendered and the truncation notice shows.
- 1.12 Click Fetch models twice quickly on the same provider; second click shows the "Refresh in progress" hint.
- 1.13 Edit a mapping, close the dialog, open Add Mapping — title is "Add Mapping", `name` field is empty and editable, Save submits as POST.

**Implementation Note**: After completing this phase and all automated verification passes, pause for manual confirmation from the human that the manual tests succeeded before proceeding to Phase 2.

---

## Phase 2: Breadcrumb polish

### Overview

Upgrade each `.route-step` in the breadcrumb chain to be information-rich (protocol badge, base-URL tooltip, fallback depth count), accessible (aria-label + visually-hidden role text), and well-formed (no leading chevron on the primary step). Add click-through navigation to filtered logs and a last-responder chevron animation.

### Changes Required:

#### 1. Data model: extend `mappingRow`/`fallbackEntry` with Protocol + BaseURL

**File**: `proxy/web/types.go` and `proxy/web/handlers.go`

**Intent**: Expose the underlying provider's `Protocol` (openai/anthropic/auto) and `DefaultBaseURL` so the template can render a protocol badge and a hover tooltip on each step pill.

**Contract**: Add `Protocol string` and `BaseURL string` to both `mappingRow` (lines 67-73) and `fallbackEntry` (lines 56-60). Populate these in `handleMappings` (lines 193-242) and `renderMappingsTable` (lines 305-355) by looking up `cfg.ProvidersSnapshot()` per provider name. No changes to `config.Mapping`.

#### 2. HTML: per-step protocol badge + accessible role text + chevron behaviour

**File**: `proxy/web/templates/mappings-table.html`

**Intent**: Render each `.route-step` with a protocol badge, an `aria-label`, and visually-hidden "Primary:" / "Fallback N:" prefix text. Hide the leading chevron on the primary step.

**Contract**: The `{{range .Fallbacks}}` block adds `$index` so the aria-label reads `Fallback {{add 1 $index}}`. Inside each step, prepend `<span class="visually-hidden">Primary:</span>` (or `Fallback N:` for fallbacks) and render `{{if .Protocol}}<span class="badge badge--protocol route-step__protocol">{{.Protocol}}</span>{{end}}` next to the name. Add `title="{{.BaseURL}} ({{.Protocol}})"` for the hover tooltip.

#### 3. HTML: fallback-depth pill in the card header

**File**: `proxy/web/templates/mappings-table.html`

**Intent**: Surface the chain depth at a glance in `.route-card__header`.

**Contract**: After the mapping-name `<h3>`, conditionally render `<span class="route-card__depth">{{len .Fallbacks}} fallback{{if ne (len .Fallbacks) 1}}s{{end}}</span>` when `len .Fallbacks > 0`.

#### 4. CSS: `.visually-hidden`, `.route-step__protocol`, `.route-card__depth`, chevron `:first-child` fix

**File**: `proxy/web/static/app.css`

**Intent**: Add the three new utility classes inline with the existing `.route-*` block (lines 750-828) and fix the leading chevron bug.

**Contract**: Append:
- `.visually-hidden` utility near `.text-muted` (line 690) — standard sr-only shape.
- `.route-step__protocol` — small badge inline with provider/model text; uses `--space-2`, `--badge--protocol` styles.
- `.route-card__depth` — small bordered pill in the header; uses `--text-muted`, `--border-subtle`, `--radius-sm`, `--space-1/2`.
- `.route-step:first-child::after { display: none; }` next to `.route-step:last-child::after` (line 814).

All new CSS reuses existing tokens only — no new hex values.

#### 5. HandleLogs: extend `?provider=` and `?mapping=` filters

**File**: `proxy/web/handlers.go`

**Intent**: Make each `.route-step` a navigation affordance to the log page filtered by the step's provider and the mapping's name.

**Contract**: After `parseMinLevel` (line 99), read `r.URL.Query().Get("provider")` and `r.URL.Query().Get("mapping")`. When non-empty, post-filter the `filtered` slice (line 108) by substring match on the log line (case-insensitive). Document the new query params as the canonical URL surface for "show me what happened on this step". Set `hx-push-url="true"` on the level `<select>` (line 6 in `logs.html`).

#### 6. HTML: per-step clickable `<a>` wrappers

**File**: `proxy/web/templates/mappings-table.html`

**Intent**: Convert `.route-step` `<div>` elements into `<a>` elements pointing to `/logs?provider=...&mapping=...` so the breadcrumb becomes a navigation surface.

**Contract**: Each step opens with `<a class="route-step route-step--primary" href="/logs?provider={{.ProviderName}}&mapping={{$.Name}}">` and closes with `</a>`. Browser default focus ring satisfies the a11y actionability gap. No CSS changes required.

#### 7. Last-responder: pre-aggregating subscriber + read endpoint + render chevron

**Files**: `internal/eventstream/handlers.go` (new), `proxy/logtee.go` (new subscriber), `proxy/web/handlers.go` (read endpoint), `proxy/web/templates/mappings-table.html` (render)

**Intent**: Subscribe to `"fallback succeeded"` log entries to maintain a per-mapping responder index; expose it via `GET /v1/mappings/last-responders`; render a sustained-pulse chevron on the matching step.

**Contract**:
- New `LastResponder` struct (in `proxy/logtee.go` or a small `proxy/lastresponder.go`) holds `sync.Map[providerName]time.Time` of last-success-timestamp; subscribes to the LogSink (or has a method `Record(provider)` called from the dispatch path).
- Eviction: an entry older than 60s is treated as "no responder" (lookup returns false). Pure TTL — no background goroutine needed if we always check `time.Since(t)` at read.
- `GET /v1/mappings/last-responders` (registered in `SetupMux`): returns `map[string]int{ mappingName: responderIndex }` (0 = primary, 1+ = fallback). Reads from a second `sync.Map[mappingName]int64` keyed by epoch second — populated by the same subscriber which knows the current fallback chain length.
- `renderMappingsTable` accepts an optional `lastResponders` map; for each mapping row, finds the index and renders `<span class="route-step--responder">` on the matching step. CSS at `app.css:830+` adds:
  ```css
  .route-step--responder::before { /* sustained pulse via ::before */ }
  @keyframes responder-pulse { /* fade in over 30s */ }
  @media (prefers-reduced-motion: reduce) { .route-step--responder::before { animation: none; } }
  ```
- For correctness: the subscriber MUST be invoked from the existing `"fallback succeeded", "attempt", i, "provider", name` log emission site at `proxy/proxy.go:326-335` — a direct call, NOT a re-scan of the LogSink.

### Success Criteria:

#### Automated Verification:

- 2.1 `go build ./...` succeeds.
- 2.2 `mage test` passes; new and existing tests for the data-model + handlers + endpoint all green.
- 2.3 `mage lint` clean.
- 2.4 New `TestMappingRow_PopulatesProtocol`: when `cfg.ProvidersSnapshot()` carries a provider with `Protocol: "openai"`, `renderMappingsTable` for a mapping that uses it produces HTML containing `badge--protocol` and the text `openai`.
- 2.5 New `TestHandleLogs_ProviderFilter`: POST fixture data into LogSink with lines `proxy=alpha` and `proxy=beta`; GET `/logs?provider=alpha`; assert only `alpha` lines in response.
- 2.6 New `TestLastResponder_AggregationAndLookup`: directly invoke `LastResponder.Record("nim", 0)` then `.Record("zen", 1)` on a synthetic mapping; assert the read endpoint returns the correct indices.
- 2.7 New `TestLastResponder_TTLEvicts`: record `Record("nim", 0)`; advance a mocked clock past 60s; assert `Lookup` returns `(0, false)`.
- 2.8 New regex test on rendered mappings-table.html output: every `.route-step` carries an `aria-label="..."` attribute and `role="listitem"`.
- 2.9 New test: with no `LastResponder` entries, no `.route-step--responder` class appears.

#### Manual Verification:

- 2.10 Load `/mappings`; confirm each step shows a protocol badge (when set), the depth pill in the card header, and per-step `aria-label` text in a screen reader.
- 2.11 Hover each step — tooltip shows the base URL + protocol.
- 2.12 Click a fallback step — navigates to `/logs?provider=<name>&mapping=<mappingName>` filtered to that subset.
- 2.13 Open DevTools, reduce motion preference; the chevron pulse animation stops.
- 2.14 After a successful request that falls back to step 2, reload `/mappings` — the chevron on step 2 is visibly distinct (pulse animation).
- 2.15 Wait >60s with no traffic; chevron animation fades to no-op state.

**Implementation Note**: After completing this phase and all automated verification passes, pause for manual confirmation before proceeding to Phase 3.

---

## Phase 3: Cross-page UX

### Overview

Eliminate silent errors across all 4 pages, add empty-state copy to the 3 list pages, and add loading indicators to every write flow. Wires the existing `ValidationError{Fields}` JSON to actual rendered text via a global `htmx:afterRequest` listener.

### Changes Required:

#### 1. Layout: toast region + utility class

**File**: `proxy/web/templates/layout.html` and `proxy/web/static/app.css`

**Intent**: Add a top-level `<div id="toast-region" aria-live="polite" role="status">` region so non-form HTMX 4xx/5xx (delete failures, fetch failures) become visible.

**Contract**: One element inside `<body>` after `<main>` in `layout.html:40-44`. CSS at `app.css:830+`:
```css
#toast-region { position: fixed; right: var(--space-4); bottom: var(--space-4); z-index: 9999; display: flex; flex-direction: column; gap: var(--space-2); }
.toast { padding: var(--space-3) var(--space-4); border-radius: var(--radius-md); background: var(--color-error); color: #fff; box-shadow: var(--shadow-lg); }
.toast--success { background: var(--color-success); }
```

#### 2. Global: silent-error JS listener

**File**: `proxy/web/templates/layout.html` (new inline `<script>` block)

**Intent**: One `htmx:afterRequest` listener wired on `document.body`; on `!event.detail.successful` it (a) populates `#*-form-error-dialog` if the request came from a form, (b) parses `event.detail.xhr.response` JSON for `fields` and renders key/value pairs, or (c) enqueues a `#toast-region` toast with the error message. Also adds a success toast on `successful` mutations (POST/PUT/DELETE that aren't mere filter GETs).

**Contract**: Include `<script src="/static/app.js" defer>` OR inline the snippet in `layout.html` (in `embed.go`'s spirit: anything user-facing sits in `templates/`). Listens use `event.target` introspection: if it's a `<form>`, populate the matching inline-error slot; otherwise toast. 4-second auto-dismiss for toasts via `setTimeout`. Identical behaviour for providers and mappings pages.

#### 3. HTML: per-form inline-error slots (already exist; verify they get populated)

**File**: `proxy/web/templates/providers.html:21` and `proxy/web/templates/mappings.html:21`

**Intent**: Confirm that the existing `<div id="*-form-error-dialog" class="form-error">` divs are populated by Phase 3.2's JS — no template change needed if already in place.

**Contract**: `htmx:afterRequest` on the dialog's `<form>` parses the response and writes `error.message` + each `fields[key]` as `<li>` items into the appropriate div. The div is reset to empty on subsequent opens via the dialog's `close` event listener (`event.target.querySelector('#*-form-error-dialog').textContent = ''`).

#### 4. Empty-state copy on Providers, Mappings, Logs

**Files**: `proxy/web/templates/providers-table.html`, `proxy/web/templates/mappings-table.html`, `proxy/web/templates/logs.html`, `proxy/web/static/app.css`

**Intent**: Show a useful hint with a CTA when a list is empty instead of an invisible `<div>`.

**Contract**: Replace the empty `<tbody>` / `<div>` with explicit copy + a CTA button to open the Add dialog. The Providers empty state links to the provider dialog open via `onclick="document.getElementById('provider-dialog').showModal()"`. Mappings same. Logs: a `<small class="text-muted">Waiting for log events…</small>` paragraph (no CTA — live SSE eventually populates it). New `.empty-state` class in `app.css` for the centered layout — reuses `--bg-card`, `--border-subtle`, `--radius-lg`, `--space-*`.

#### 5. Loading indicators on submit buttons

**File**: `proxy/web/templates/providers.html`, `proxy/web/templates/mappings.html`, `proxy/web/static/app.css`

**Intent**: Show a spinner during the (occasionally slow) `cfg.SaveData` round-trip; prevent double-clicks.

**Contract**: Each Save button gains a sibling `<span class="htmx-indicator" aria-hidden="true">⟳</span>` (or SVG) and `hx-disabled-elt="this"`. CSS:
```css
.htmx-indicator { display: none; }
.htmx-request .htmx-indicator { display: inline; }
```
The `.htmx-request` class is added by HTMX automatically during the request lifecycle.

### Success Criteria:

#### Automated Verification:

- 3.1 `go build ./...` succeeds.
- 3.2 `mage test` passes; no regression in handler tests; new empty-state / error-wiring tests pass.
- 3.3 `mage lint` clean.
- 3.4 New `TestEmptyState_Providers`: render `providers-table.html` with an empty `Providers` slice; assert the HTML contains `class="empty-state"` and the CTA copy.
- 3.5 New `TestEmptyState_Mappings`: same as 3.4 for mappings; assert CTA references the `mapping-dialog` ID.
- 3.6 New `TestErrorMessageInResponse`: POST a malformed provider form (e.g. `name=&behavior=invalid`); assert the response body JSON contains `fields.name` and `fields.behavior` (this confirms the JS listener has data to display — it doesn't render the JS, but proves the contract).
- 3.7 New regex test on `providers.html` / `mappings.html`: each `<button type="submit">` carries an `hx-disabled-elt="this"` attribute and a sibling `.htmx-indicator` element.

#### Manual Verification:

- 3.8 Open `/providers`; click Add with an invalid base URL (e.g. `not-a-url`); the dialog stays open and the `form-error` div shows the message; no console error.
- 3.9 Open `/mappings`; click Add with empty name; same affordance.
- 3.10 Delete a provider while a slow operation is in flight (or just observe); the submit button shows a spinner for the duration.
- 3.11 With no providers, open `/providers` — empty state visible, click CTA, dialog opens.
- 3.12 Reload `/logs?min=error` — the URL persists, the dropdown stays on "error", and incoming SSE messages respect the level filter (info-level SSE messages are NOT appended).
- 3.13 Trigger a global toast by stubbing a 500 from `/v1/providers/foo` — toast bottom-right, dismisses after 4s.

**Implementation Note**: After completing this phase and the final commit, the change is ready for `/10x-impl-review`.

---

## Testing Strategy

### Unit Tests (alongside each phase's changes):

- Each Phase's `Changes Required` section enumerates specific tests to add. All tests live in `proxy/web/` next to the file they exercise.
- For Phase 1: regex/byte-level assertions on rendered templates catch the F1 double-escape and F3 unbalanced-quote regressions immediately, without spawning a browser.
- For Phase 2: handler-level tests for `Protocol`/`BaseURL` population; HTTP-fixture test for `handleLogs` with `?provider=&mapping=`; pure Go tests for the `LastResponder` aggregator (no time-skip via `time.Sleep` — use injectable clock or testable `record()` API).
- For Phase 3: handler-fixture tests proving error JSON shape matches what the global `htmx:afterRequest` listener expects.

### Integration Tests:

- Existing `proxy/web/handlers_write_test.go` covers happy-path CRUD. Add Path-Sad HTTP-level tests for the regressions this PR closes (no browser, no E2E).
- No new E2E/browser tests — visual verification is manual per Phase §Manual Verification.

### Manual Testing Steps:

End-to-end manual run per Phase (the per-Phase §Manual Verification blocks enumerate the steps). Visual regression is captured in the per-PR screenshots run by the human before merge.

## Performance Considerations

- **Template rendering**: same number of template executions as before; one new optional `LastResponder` lookup (`sync.Map.Load`) per row — O(1).
- **CSS**: ~80 lines of new rules; trivially fast for <50 cards.
- **JSON encoding**: `data-fallbacks` no longer goes through `JSEscapeString`; the JSON comes directly from `json.Marshal` and is HTML-escaped only once by `html/template`. Slight net speedup vs current double-escape.
- **`handleLogs` substring filter**: O(N × M) over up to 200 entries × variable filter length. Acceptable; matches the existing pattern.
- **Toast lifecycle**: bounded by `setTimeout`; no memory leak.

## Migration Notes

- None. No config schema changes (`config.Mapping` unchanged). No data migration. Existing users get the upgrade automatically the next time they reload.
- `mappingRow.FallbacksString()` / `fallbackEntry.String()` are unexported methods on unexported structs — no external API surface; safe to delete (matches the historical cleanup pattern in the codebase).
- HTMX swap selectors (`#mappings`, `#providers`) stay identical — existing browser state survives the upgrade.

## References

- Related research: [`context/changes/web-ui-friendliness/research.md`](../web-ui-friendliness/research.md)
- Prior change plan: [`context/changes/mapping-graph-visualization/plan.md`](https://github.com/pfrack/freedius/blob/d6f1930a/context/changes/mapping-graph-visualization/plan.md)
- Prior impl-review (PENDING findings source): [`context/changes/mapping-graph-visualization/reviews/impl-review.md`](https://github.com/pfrack/freedius/blob/d6f1930a/context/changes/mapping-graph-visualization/reviews/impl-review.md)
- Foundation lessons: [`context/foundation/lessons.md`](https://github.com/pfrack/freedius/blob/d6f1930a/context/foundation/lessons.md)
- Roadmap: [`context/foundation/roadmap.md`](https://github.com/pfrack/freedius/blob/d6f1930a/context/foundation/roadmap.md)

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles. See `references/progress-format.md`.

### Phase 1: Bug fixes (PENDING findings F1–F4 + bonus + cleanup)

#### Automated

- [ ] 1.1 `go build ./...` succeeds
- [ ] 1.2 `mage test` (or `go test ./proxy/web/...`) passes — all new + existing tests
- [ ] 1.3 `mage lint` clean
- [ ] 1.4 `TestMappingsTable_F1_RoundTrip` — single/double-quote round-trips through `data-fallbacks` attribute
- [ ] 1.5 `TestHandleRefreshModels_Truncation` — 1500-model response capped at 1000 + truncation notice
- [ ] 1.6 `TestHandleRefreshModels_InProgress` — second concurrent refresh returns `Fetch in progress`
- [ ] 1.7 `TestHandleRefreshModels_NoTruncationMessage_WhenExactly1000` — 1000 models do not show "Truncated"
- [ ] 1.8 `TestStaleHxPutAfterEditCancel` — Add Mapping after Edit/Cancel cycle resets form to POST mode

#### Manual

- [ ] 1.9 Mapping with single-quote provider name Edit-pre-populates correctly
- [ ] 1.10 Two delete confirm dialogs show grammatical copy
- [ ] 1.11 1500+ model provider renders only 1000 with truncation notice
- [ ] 1.12 Double-click Fetch models shows "Refresh in progress"
- [ ] 1.13 Edit → Cancel → Add cycle resets form state and submits as POST

### Phase 2: Breadcrumb polish (protocol, aria, depth, click, last-responder)

#### Automated

- [ ] 2.1 `go build ./...` succeeds
- [ ] 2.2 `mage test` passes — new + existing tests
- [ ] 2.3 `mage lint` clean
- [ ] 2.4 `TestMappingRow_PopulatesProtocol` — provider Protocol copied into mappingRow & rendered as badge
- [ ] 2.5 `TestHandleLogs_ProviderFilter` — `?provider=` substring filter applies
- [ ] 2.6 `TestLastResponder_AggregationAndLookup` — Record + Read round-trip
- [ ] 2.7 `TestLastResponder_TTLEvicts` — old entries drop out
- [ ] 2.8 Regex assertion: every `.route-step` has `aria-label` + `role="listitem"`
- [ ] 2.9 No `.route-step--responder` rendered when aggregator is empty

#### Manual

- [ ] 2.10 Cards show protocol badge, depth pill, accessible role text per step
- [ ] 2.11 Hover tooltip shows BaseURL + Protocol
- [ ] 2.12 Step click navigates to `/logs?provider=&mapping=` with correct filter
- [ ] 2.13 `prefers-reduced-motion: reduce` stops the pulse animation
- [ ] 2.14 Successful fallback fires the chevron pulse on the responder step
- [ ] 2.15 After >60s idle, chevron pulse fades out

### Phase 3: Cross-page UX (silent errors + empty states + loading indicators)

#### Automated

- [ ] 3.1 `go build ./...` succeeds
- [ ] 3.2 `mage test` passes — no regression in handler tests
- [ ] 3.3 `mage lint` clean
- [ ] 3.4 `TestEmptyState_Providers` — empty provider list shows CTA
- [ ] 3.5 `TestEmptyState_Mappings` — empty mapping list shows CTA referencing mapping dialog
- [ ] 3.6 `TestErrorMessageInResponse` — malformed POST returns ValidationError JSON containing `fields.name` + `fields.behavior`
- [ ] 3.7 Regex assertion: each Save button carries `hx-disabled-elt="this"` + sibling `.htmx-indicator`

#### Manual

- [ ] 3.8 Invalid base URL → form-error dialog shows per-field message
- [ ] 3.9 Empty mapping name → same affordance on mappings page
- [ ] 3.10 Submit button shows spinner during slow save
- [ ] 3.11 Empty providers/mappings → empty-state CTA opens dialog
- [ ] 3.12 `/logs?min=error` reload preserves filter; SSE respects level
- [ ] 3.13 Triggered 500 → toast visible bottom-right, auto-dismiss in 4s

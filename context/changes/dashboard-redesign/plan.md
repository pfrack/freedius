# Dashboard GUI Redesign Implementation Plan

## Overview

Redesign the Freedius web dashboard from a configuration-heavy card UI into an operator-friendly monitoring dashboard. The redesign adds telemetry aggregation, health visibility, attention alerts, compact routing tables, and live activity — enabling an operator to answer 5 key questions in under 10 seconds:

1. Is the router healthy and receiving requests?
2. Which mapping routes to which primary model/provider?
3. What fallback will be used if the primary fails?
4. Which mappings or providers need attention?
5. Where can the user safely change configuration?

## Current State Analysis

The web UI (`proxy/web/`) uses Go `html/template` + HTMX + vanilla JS, served from `embed.FS`. Styling is vanilla CSS (`app.css`, 40KB), font is Geist via CDN.

### Key Discoveries:

- `proxy/web/templates/index.html` — Dashboard shows stat cards, uptime strip, the same `mappings-table.html` route-cards used on Mappings page, and a provider chip list. Inline Edit/Delete on mapping cards.
- `proxy/web/templates/mappings-table.html` — Shared template rendering large route-cards with Edit/Delete actions. Used by both dashboard and Mappings page.
- `proxy/eventbus.go` — `RequestEvent` struct has: Seq, RequestID, Method, Path, Model, Provider, Status, Latency, MatchedProvider, MatchedModel, Timestamp, ErrorMessage, ErrorType. Ring buffer capacity 10,000.
- `proxy/lastresponder.go` — `LastResponder` tracks most recent successful responder index per mapping (60s TTL). Provides `Lookup` and `Snapshot`.
- `proxy/logtee.go` — `LogSink` stores pre-rendered slog text lines with Level/Seq. Ring buffer 10,000 capacity.
- `internal/eventstream/handlers.go` — SSE endpoints: `/v1/events` (request events), `/v1/logs` (log stream), `/v1/stats` (JSON stats), `/v1/config` (JSON config dump).
- `proxy/web/handlers.go:SetupMux` — Routes: `/` (dashboard), `/mappings`, `/providers`, `/logs`, plus CRUD `/v1/mappings/*`, `/v1/providers/*`.
- `proxy/web/types.go` — `indexData`, `mappingRow`, `providerRow`, `logsData` structs for template rendering.
- No per-mapping/per-provider aggregation exists. No provider health check. No "attention" logic.

## Desired End State

After this plan is complete:

- The dashboard renders a health strip (router state, uptime, endpoint, last request, errors, fallback events), a conditional attention panel, a compact searchable routing table, provider-health badges, and a live SSE activity feed.
- The Mappings page uses a compact `<table>` with row actions (Edit via dialog, Delete via confirmation modal), search, and filters.
- The Providers page shows connection state, last-checked/last-error info, and a "Test Connection" button.
- The Logs page supports `?outcome=` and `?fallback=` deep-link filters in addition to existing `?min=`, `?provider=`, `?mapping=`.
- A `StatsCollector` in `proxy/` subscribes to `EventBus` and maintains per-mapping and per-provider counters accessible to the web layer.
- Clicking a mapping row on the dashboard opens a read-only slide-in drawer with full route details.
- All status indicators use icon + text (never color alone). Keyboard navigation and focus management are implemented.

### Verification:

- `mage test` passes (Go handler tests + StatsCollector unit tests)
- Template render assertions verify expected DOM fragments
- Playwright E2E tests (3-5) pass: SSE feed, drawer, attention panel, deep-link filters
- Manual: dashboard answers the 5 key questions within 10 seconds of page load

## What We're NOT Doing

- Background active health checks (polling providers on a timer)
- Persistent telemetry across restarts (SQLite, files)
- Multi-user auth or RBAC
- Mobile-first layout (desktop-first, responsive for tablet)
- TUI changes (web-only)
- New npm/JS dependencies (staying with vanilla JS + HTMX)
- Provider CRUD redesign (only adding health columns + test button)

## Implementation Approach

8 phases, each independently testable:

1. Backend telemetry (StatsCollector) — foundational data layer
2. Dashboard templates + handler — largest UI change, consumes StatsCollector
3. Mapping details drawer — progressive disclosure component
4. Mappings page table refactor — config page modernization
5. Providers page enhancement — operational visibility
6. Logs page deep-link filters — cross-page navigation glue
7. CSS & accessibility polish — visual refinement pass
8. Playwright E2E tests — browser-level verification

Each phase has automated verification (Go tests, lint, build) and manual verification steps.

## Phase 1: StatsCollector & Telemetry Backend

### Overview

Create a `StatsCollector` that subscribes to `EventBus`, maintains per-mapping and per-provider counters (request count, error count, fallback count, last activity timestamp, recent latencies), and exposes a snapshot API for the web handlers.

### Changes Required:

#### 1. StatsCollector struct and logic

**File**: `proxy/stats_collector.go`

**Intent**: New file. Defines `StatsCollector` with `MappingStats` and `ProviderStats` structs. Subscribes to EventBus on construction. Updates counters on each `RequestEvent`. Exposes `MappingSnapshot()` and `ProviderSnapshot()` methods returning copies (same pattern as `Config.MappingsSnapshot()`).

**Contract**: 
- `NewStatsCollector(bus *EventBus) *StatsCollector` — constructor, starts subscriber goroutine.
- `MappingStats` — struct with: `RequestCount int64`, `ErrorCount int64`, `FallbackCount int64`, `LastActivity time.Time`, `LastLatency time.Duration`, `RecentErrorRate float64` (last 10 requests).
- `ProviderStats` — struct with: `RequestCount int64`, `ErrorCount int64`, `LastSuccess time.Time`, `LastError time.Time`, `LastErrorMessage string`, `RecentErrorRate float64`.
- `MappingSnapshot() map[string]MappingStats`
- `ProviderSnapshot() map[string]ProviderStats`
- Thread-safe: internal `sync.RWMutex` guarding maps.

#### 2. StatsCollector unit tests

**File**: `proxy/stats_collector_test.go`

**Intent**: Test counter accuracy: emit events via EventBus, verify MappingSnapshot/ProviderSnapshot reflect correct counts, error rates, timestamps. Test zero-state (no events → empty maps). Test concurrent access safety.

**Contract**: Table-driven tests covering: single event, multiple events, error events, fallback events (where `MatchedProvider != Provider` or status indicates fallback), concurrent emit+read.

#### 3. Wire StatsCollector in main.go

**File**: `cmd/freedius/main.go`

**Intent**: Create `StatsCollector` after `EventBus` is created, pass it to `eventstream.Handlers` (or directly to web layer) so dashboard handler can access it.

**Contract**: `StatsCollector` field added to `eventstream.Handlers` struct (or passed separately to `web.SetupMux`). Initialized before server starts.

#### 4. Expose StatsCollector to eventstream.Handlers

**File**: `internal/eventstream/handlers.go`

**Intent**: Add `Stats *proxy.StatsCollector` field to `Handlers` struct. No new routes needed — the web handler reads stats on render.

**Contract**: `Handlers.Stats *proxy.StatsCollector` — nil-safe (dashboard degrades gracefully if nil).

### Success Criteria:

#### Automated Verification:

- Unit tests pass: `go test ./proxy/ -run TestStatsCollector`
- Build succeeds: `mage build`
- Lint passes: `mage lint`
- Existing tests unbroken: `mage test`

#### Manual Verification:

- Start freedius, route a few requests, inspect `/v1/stats` endpoint (or add a temporary debug endpoint) to confirm counters increment correctly.

**Implementation Note**: After completing this phase and all automated verification passes, pause here for manual confirmation from the human that the manual testing was successful before proceeding to the next phase.

---

## Phase 2: Dashboard Redesign (Templates + Handler)

### Overview

Replace the dashboard `index.html` template with: health strip, conditional attention panel, compact routing table, provider-health summary badges, and SSE activity feed. Rewrite the `/` handler to compute attention rules and pass enriched data. Delete old shared `mappings-table.html`.

### Changes Required:

#### 1. New dashboard template

**File**: `proxy/web/templates/index.html`

**Intent**: Complete rewrite. Layout top-to-bottom: (1) Health strip — router state, uptime, listening endpoint, last routed timestamp, errors in last hour, fallback events in 24h. (2) Attention panel — conditional, only renders when issues exist. Each alert links to relevant page/logs. (3) Routing table — `<table>` with columns: Mapping, Primary Route, Fallback Route, Status, Requests, Fallbacks, Last Activity. Rows are clickable (open drawer). (4) Provider health — counts strip + badges by status. (5) Activity feed — last 20 events server-rendered, SSE-appended.

**Contract**: Template defines `{{define "content"}}` block. Consumes new `dashboardData` struct. Uses `hx-get` for drawer loading, `sse-connect="/v1/events"` for live feed. Includes empty/zero states per section.

#### 2. Delete old shared mappings-table template

**File**: `proxy/web/templates/mappings-table.html`

**Intent**: Delete this file. It rendered the large route-cards shared between dashboard and mappings. Replaced by separate templates for each page.

**Contract**: File removed. All references in `loadPageTemplate` and `loadFragmentTemplate` calls updated.

#### 3. New dashboard data types

**File**: `proxy/web/types.go`

**Intent**: Add `dashboardData` struct replacing `indexData`. Add `attentionAlert`, `activityRow`, `routingTableRow`, `providerHealthBadge` types.

**Contract**:
- `dashboardData` — embeds `pageData`; fields: `Health healthStrip`, `Alerts []attentionAlert`, `Rows []routingTableRow`, `ProviderHealth providerHealthSummary`, `RecentActivity []activityRow`.
- `healthStrip` — `State string` (Healthy/Degraded/Down), `Uptime string`, `Endpoint string`, `LastRequest *time.Time`, `ErrorsLastHour int64`, `FallbacksLast24h int64`.
- `attentionAlert` — `Severity string`, `Message string`, `Link string`, `Icon string`.
- `routingTableRow` — extends `mappingRow` with: `RequestCount int64`, `ErrorCount int64`, `FallbackCount int64`, `LastActivity string`, `StatusLabel string`, `StatusIcon string`.
- `providerHealthSummary` — `Total int`, `Healthy int`, `Degraded int`, `Error int`, `Unknown int`, `Badges []providerHealthBadge`.
- `providerHealthBadge` — `Name string`, `Status string`, `LastChecked string`, `MappingCount int`.
- `activityRow` — `Timestamp string`, `Mapping string`, `Route string`, `FallbackUsed bool`, `Latency string`, `Status int`, `LogsLink string`.

#### 4. Rewrite dashboard handler

**File**: `proxy/web/handlers.go`

**Intent**: Rewrite the `GET /` handler to: compute health strip from StatsCollector, evaluate attention rules, build routing table rows enriched with stats, compute provider health summary, fetch recent activity from EventBus ring. Pass `dashboardData` to new template. Remove `mappings-table.html` from `renderPage` extra files.

**Contract**: Handler function signature unchanged (closure in `SetupMux`). Reads `h.Stats.MappingSnapshot()` and `h.Stats.ProviderSnapshot()`. Attention rules: missing env var, provider error rate > 50% in last 10, no success in 5 min, mapping references missing provider.

#### 5. Attention rules helper

**File**: `proxy/web/attention.go`

**Intent**: New file. `computeAlerts(cfg, mappingStats, providerStats, providers) []attentionAlert` — evaluates all rules, returns sorted alerts (errors first, then warnings).

**Contract**: Pure function, no side effects. Testable in isolation.

#### 6. Dashboard handler tests

**File**: `proxy/web/handlers_dashboard_test.go`

**Intent**: Extend existing test file. Add tests for: health strip rendering, attention panel presence/absence, routing table row content, provider badges, activity feed section. Test zero-traffic state renders gracefully.

**Contract**: Table-driven tests using `httptest.NewRecorder`. Assert on HTML fragments (string contains checks for key DOM elements).

### Success Criteria:

#### Automated Verification:

- Build succeeds: `mage build`
- Handler tests pass: `go test ./proxy/web/ -run TestDashboard`
- Attention rules tests pass: `go test ./proxy/web/ -run TestComputeAlerts`
- Lint passes: `mage lint`
- Full test suite: `mage test`

#### Manual Verification:

- Dashboard loads with health strip showing correct uptime and endpoint
- With no traffic: routing table shows mappings with "—" counts and "No traffic"
- After routing requests: counts update on page refresh, activity feed shows events
- Attention panel appears when a provider env var is missing
- Attention panel hidden when all providers are configured correctly

**Implementation Note**: After completing this phase and all automated verification passes, pause here for manual confirmation from the human that the manual testing was successful before proceeding to the next phase.

---

## Phase 3: Mapping Details Drawer

### Overview

Add a slide-in drawer component that opens when a routing table row is clicked. The drawer shows full mapping details (route chain, stats, config info) in a read-only view with an "Edit" link to the Mappings page.

### Changes Required:

#### 1. Drawer fragment template

**File**: `proxy/web/templates/mapping-drawer.html`

**Intent**: New template. Defines a `{{define "mapping-drawer"}}` block rendering: mapping name, status badge, full route chain (primary + all fallbacks as a vertical step list), request/error/fallback counts, last activity, added-at date, and an "Edit on Mappings page" link.

**Contract**: Fragment loaded via HTMX (`hx-get="/v1/mappings/{name}/detail"`). Self-contained HTML (no layout wrapper). Includes a close button. Uses existing `.route-step` CSS classes for the chain visualization.

#### 2. Drawer endpoint handler

**File**: `proxy/web/handlers.go`

**Intent**: Add `GET /v1/mappings/{name}/detail` route in `SetupMux`. Returns the drawer fragment HTML for the named mapping. Reads config + stats for that mapping.

**Contract**: Returns 200 + HTML fragment on success. Returns 404 JSON error if mapping not found. HTMX-only endpoint (renders fragment, not full page).

#### 3. Drawer container in dashboard template

**File**: `proxy/web/templates/index.html`

**Intent**: Add an empty `<aside id="mapping-drawer" class="drawer">` container at the bottom of the content block. HTMX swaps the drawer fragment into this container on row click. Add `hx-get` and `hx-target="#mapping-drawer"` attributes to routing table rows.

**Contract**: Drawer positioned fixed right, slides in via CSS transition. Close button removes content from container (or sets `aria-hidden`).

#### 4. Drawer open/close JS

**File**: `proxy/web/static/app.js`

**Intent**: Add minimal JS for drawer focus management: trap focus inside drawer when open, return focus to triggering row on close, close on Escape key. HTMX handles the content swap; JS handles accessibility.

**Contract**: Event listeners on `htmx:afterSwap` (for drawer target) and `keydown` (Escape). No new dependencies.

#### 5. Drawer handler test

**File**: `proxy/web/handlers_dashboard_test.go`

**Intent**: Add test for `/v1/mappings/{name}/detail` endpoint: returns HTML fragment with mapping details, returns 404 for unknown mapping.

**Contract**: `httptest` request with `HX-Request: true` header. Assert response contains mapping name, provider, model, route chain elements.

### Success Criteria:

#### Automated Verification:

- Build succeeds: `mage build`
- Drawer handler test passes: `go test ./proxy/web/ -run TestMappingDrawer`
- Lint passes: `mage lint`
- Full test suite: `mage test`

#### Manual Verification:

- Click a mapping row on dashboard → drawer slides in from right with full details
- Drawer shows correct route chain (primary + fallbacks)
- Press Escape → drawer closes, focus returns to clicked row
- Click "Edit on Mappings page" → navigates to /mappings with that mapping's dialog open
- Keyboard: Tab through table rows, Enter opens drawer, Escape closes

**Implementation Note**: After completing this phase and all automated verification passes, pause here for manual confirmation from the human that the manual testing was successful before proceeding to the next phase.

---

## Phase 4: Mappings Page Table Refactor

### Overview

Replace the card grid on the Mappings page with a compact `<table>` layout using row action menus. Add search, status filter, provider filter, and "has fallback" filter. Replace the inline `hx-confirm` delete with a proper confirmation modal that includes the mapping name.

### Changes Required:

#### 1. New mappings routing table template

**File**: `proxy/web/templates/mappings-routing-table.html`

**Intent**: New fragment template. Compact `<table>` with columns: Mapping (alias), Primary (provider/model inline), Fallback (first fallback + "+N more"), Status badge, Actions (⋯ menu with Edit/Delete). Includes a filter bar above the table: search input, provider `<select>`, status `<select>`, "has fallback" checkbox. Empty state when no mappings match filters.

**Contract**: Template `{{define "mappings-routing-table"}}`. HTMX-powered filters (`hx-get="/mappings"` with `hx-include` for filter inputs, `hx-target="#mappings-table-container"`). Primary route shown as `provider / model` with truncation. Fallback shows first entry + "+N" badge if more exist.

#### 2. Delete confirmation modal

**File**: `proxy/web/templates/mappings.html`

**Intent**: Replace `hx-confirm="Delete mapping '...'?"` (browser native confirm) with a `<dialog>` modal that shows the mapping name prominently and requires explicit "Delete" button click. The dialog is shown via JS when the Delete action is triggered.

**Contract**: `<dialog id="delete-confirm-dialog">` with mapping name interpolated. "Cancel" closes dialog. "Delete" fires `hx-delete` to the endpoint. Prevents accidental deletion better than native confirm.

#### 3. Update mappings page template

**File**: `proxy/web/templates/mappings.html`

**Intent**: Update to use `mappings-routing-table.html` instead of `mappings-table.html`. Keep "Add Mapping" button and the existing `<dialog id="mapping-dialog">` form (it works well). Add filter state from query params.

**Contract**: Template includes `{{template "mappings-routing-table" .}}`. Filter inputs pre-filled from handler-provided data.

#### 4. Update mappings handler for filters

**File**: `proxy/web/handlers.go`

**Intent**: Extend `handleMappings` to parse additional query params: `?search=` (name substring), `?status=` (active/inactive), `?has_fallback=` (true/false). Pass filter state to template data for pre-filling inputs. HTMX requests return only the table fragment.

**Contract**: Existing `?provider=` filter preserved. New filters added with AND logic. `mappingsData` struct extended with filter fields: `Search string`, `StatusFilter string`, `HasFallbackFilter string`.

#### 5. Update renderMappingsTable for new fragment

**File**: `proxy/web/handlers.go`

**Intent**: `renderMappingsTable` loads `mappings-routing-table.html` fragment instead of deleted `mappings-table.html`.

**Contract**: Fragment template name updated. Same `mappingsData` struct input.

#### 6. Mappings page handler tests

**File**: `proxy/web/handlers_phase1_test.go` (or new file)

**Intent**: Add/update tests for: new filter params work correctly, HTMX request returns table fragment, delete confirmation flow, search narrows results.

**Contract**: Table-driven tests asserting filter behavior and rendered output.

### Success Criteria:

#### Automated Verification:

- Build succeeds: `mage build`
- Handler tests pass: `go test ./proxy/web/ -run TestMappings`
- Lint passes: `mage lint`
- Full test suite: `mage test`

#### Manual Verification:

- Mappings page shows compact table (not cards)
- Search input filters mappings by name in real-time
- Provider dropdown filters by provider
- "Has fallback" checkbox shows only mappings with fallbacks
- Edit action opens the existing dialog pre-filled
- Delete action opens confirmation modal with mapping name; cancel dismisses
- "Add Mapping" still works with the existing dialog

**Implementation Note**: After completing this phase and all automated verification passes, pause here for manual confirmation from the human that the manual testing was successful before proceeding to the next phase.

---

## Phase 5: Providers Page Enhancement

### Overview

Add operational visibility to the Providers page: connection state indicator (passive health derived from StatsCollector), last-checked timestamp, last-error info, and a "Test Connection" button (lightweight reachability check — does NOT fetch models). Move low-frequency technical fields (base URL, API key env) into an expandable detail row.

### Changes Required:

#### 1. Extend providerRow type

**File**: `proxy/web/types.go`

**Intent**: Add fields to `providerRow`: `Status string` (healthy/degraded/error/unknown), `LastSuccess string`, `LastError string`, `LastErrorMessage string`, `RequestCount int64`.

**Contract**: New fields populated from `StatsCollector.ProviderSnapshot()`. Status derived: error rate > 50% in last 10 → "degraded", last 3 consecutive errors → "error", no traffic → "unknown", otherwise "healthy".

#### 2. Update providers template

**File**: `proxy/web/templates/providers.html`

**Intent**: Redesign table columns. Primary columns: Name, Status (badge), Mappings, Last Checked, Last Error, Actions. Technical fields (Base URL, API Key Env, Protocol, Behavior) shown in expandable `<details>` row or a toggle. Add "Test Connection" button in actions column.

**Contract**: Table uses `<details>` element for technical fields. "Test Connection" button POSTs to `POST /v1/providers/{name}/test` (new lightweight reachability endpoint) and opens a `<dialog id="test-dialog">` modal showing the result. Status badges use icon + text.

#### 3. Update providers-table fragment

**File**: `proxy/web/templates/providers-table.html`

**Intent**: Match the new table structure for HTMX fragment swaps.

**Contract**: Fragment template updated to render new columns. Same `{{define "providers-table"}}` block name.

#### 4. Update providers handler

**File**: `proxy/web/handlers.go`

**Intent**: In `handleProviders`, enrich `providerRow` with stats from `h.Stats.ProviderSnapshot()`. Derive status label per provider. Add `handleTestConnection` for lightweight reachability check.

**Contract**: Handler reads `h.Stats` (nil-safe — if nil, all providers show "unknown" status). New `POST /v1/providers/{name}/test` endpoint performs a 5s-timeout HTTP GET to the provider's base URL; any response (even 401/403) = reachable, connection error = unreachable. Renders `test-result.html` fragment.

#### 5. Test result fragment

**File**: `proxy/web/templates/test-result.html`

**Intent**: New fragment showing reachable/unreachable status with icon + message + latency. No model list.

**Contract**: `{{if .Reachable}}` shows green checkmark + "Reachable (HTTP {status}, {latency} ms)". `{{else}}` shows red X + error message.

#### 6. Provider page tests

**File**: `proxy/web/handlers_provider_filter_test.go` (or extend existing)

**Intent**: Test that provider table renders status badges, that expandable details are present.

**Contract**: Assert rendered HTML contains status badges, last-error text when errors exist, and "unknown" when no traffic data.

### Success Criteria:

#### Automated Verification:

- Build succeeds: `mage build`
- Provider tests pass: `go test ./proxy/web/ -run TestProvider`
- Lint passes: `mage lint`
- Full test suite: `mage test`

#### Manual Verification:

- Providers page shows status badges for each provider
- Provider with recent errors shows "Degraded" or "Error" badge
- Provider with no traffic shows "Unknown"
- "Test Connection" button opens a modal showing reachability (✓ Reachable with HTTP status + latency, or ✗ Unreachable with error); table stays intact; Close dismisses modal
- Technical details (base URL, API key env) are hidden by default, expandable

**Implementation Note**: After completing this phase and all automated verification passes, pause here for manual confirmation from the human that the manual testing was successful before proceeding to the next phase.

---

## Phase 6: Logs Page Deep-Link Filters

### Overview

Extend the Logs page to support `?outcome=` (success/error) and `?fallback=` (true/false) query parameters. Ensure all dashboard attention-panel links and activity-feed links construct correct filter URLs. Add visual distinction between system/startup logs and routing request logs.

### Changes Required:

#### 1. Extend log filtering logic

**File**: `proxy/web/handlers.go`

**Intent**: In `handleLogs`, parse `?outcome=` (values: "success", "error") and `?fallback=` (values: "true", "false") from query params. Apply as additional AND filters on the log entries. For outcome: match on log level (error level = error outcome) or on presence of error-related substrings. For fallback: match on "fallback" substring in log line.

**Contract**: `parseOutcomeFilter` and `parseFallbackFilter` helper functions. Return `nil` for empty/invalid values (no filter). Extend the existing filter loop in `handleLogs`.

#### 2. Update logs template with new filter controls

**File**: `proxy/web/templates/logs.html`

**Intent**: Add two new filter controls in `.log-filters`: an "Outcome" dropdown (All/Success/Error) and a "Fallback" dropdown (All/Yes/No). Wire them with `hx-get="/logs"` and `hx-include` like existing filters.

**Contract**: New `<select>` elements with `name="outcome"` and `name="fallback"`. Pre-filled from handler-provided data (`logsData` extended with `Outcome string`, `Fallback string`).

#### 3. Extend logsData type

**File**: `proxy/web/types.go`

**Intent**: Add `Outcome string` and `Fallback string` fields to `logsData` for template pre-fill.

**Contract**: Fields set from parsed query params in handler.

#### 4. Visual distinction for system logs

**File**: `proxy/web/templates/logs.html` + `proxy/web/static/app.css`

**Intent**: Add a CSS class (`.log-system`) for startup/system log entries (those without routing metadata). In the SSE handler JS, detect system vs routing logs by absence of provider/mapping references and apply the class.

**Contract**: System logs rendered with a subtle left-border or muted background to visually separate from routing request logs. Detection heuristic: log lines containing "listening", "loaded config", "starting" get `.log-system` class.

#### 5. Log filter tests

**File**: `proxy/web/log_filter_test.go`

**Intent**: Extend existing filter tests with new `?outcome=` and `?fallback=` params. Test that invalid values are ignored (no filter applied). Test combined filters.

**Contract**: Table-driven tests asserting correct filtering behavior.

### Success Criteria:

#### Automated Verification:

- Build succeeds: `mage build`
- Log filter tests pass: `go test ./proxy/web/ -run TestLog`
- Lint passes: `mage lint`
- Full test suite: `mage test`

#### Manual Verification:

- Navigate to `/logs?outcome=error` — only error-level logs shown
- Navigate to `/logs?fallback=true` — only logs mentioning fallback shown
- Dashboard attention panel link opens logs with correct pre-applied filters
- Activity feed "View in logs" link opens filtered logs view
- System startup logs visually distinct from routing logs

**Implementation Note**: After completing this phase and all automated verification passes, pause here for manual confirmation from the human that the manual testing was successful before proceeding to the next phase.

---

## Phase 7: CSS & Accessibility Polish

### Overview

Responsive layout adjustments, drawer slide animation, status badge styles, keyboard navigation for table rows and drawer, focus management, truncation/copy tooltip for long model IDs, and no-color-only status indicators.

### Changes Required:

#### 1. Status badge styles

**File**: `proxy/web/static/app.css`

**Intent**: Define badge variants: `.badge--healthy` (green icon + "Healthy" text), `.badge--degraded` (amber icon + "Degraded"), `.badge--error` (red icon + "Error"), `.badge--unknown` (gray icon + "Unknown"), `.badge--disabled` (muted icon + "Disabled"). Each badge includes an inline SVG icon so status is never color-only.

**Contract**: Badges use `display: inline-flex; align-items: center; gap: 0.25rem`. Colors meet WCAG 2.1 AA contrast (4.5:1 minimum against white background). Icon is a small circle or status-specific shape.

#### 2. Drawer animation and positioning

**File**: `proxy/web/static/app.css`

**Intent**: `.drawer` positioned fixed right, full height, width 400px (desktop) / 100% (mobile). Slide-in via `transform: translateX(100%)` → `translateX(0)` transition. Overlay backdrop. z-index above main content.

**Contract**: `transition: transform 0.2s ease-out`. `.drawer--open` class toggles visibility. Respects `prefers-reduced-motion` (instant transition when motion disabled).

#### 3. Table row keyboard navigation

**File**: `proxy/web/static/app.js`

**Intent**: Make routing table rows keyboard-navigable: `role="button"` or `tabindex="0"` on `<tr>` elements. Arrow keys move focus between rows. Enter/Space opens drawer.

**Contract**: Event listener on table `keydown`. Focus ring visible (`:focus-visible` outline). Does not interfere with HTMX event handling.

#### 4. Model ID truncation + copy tooltip

**File**: `proxy/web/static/app.css` + `proxy/web/static/app.js`

**Intent**: Long model IDs (e.g., `stepfun-ai/step-3.5-flash`) truncated with ellipsis in table cells. On hover: show full ID in a tooltip. Click-to-copy with brief "Copied!" feedback.

**Contract**: CSS: `.model-id { max-width: 180px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }`. JS: click handler copies `title` attribute value to clipboard, shows toast.

#### 5. Responsive layout

**File**: `proxy/web/static/app.css`

**Intent**: Desktop-first. At `max-width: 768px`: routing table scrolls horizontally, drawer goes full-width, health strip stacks vertically, provider badges wrap. At `max-width: 480px`: hide low-priority table columns (fallback count, last activity).

**Contract**: Media queries. No layout shifts on desktop. Table uses `.table-wrap` (already exists in codebase).

#### 6. Focus management for drawer

**File**: `proxy/web/static/app.js`

**Intent**: When drawer opens: move focus to drawer heading or close button. When drawer closes: return focus to the row that triggered it. Trap tab inside drawer while open.

**Contract**: Uses `MutationObserver` or `htmx:afterSwap` event to detect drawer content insertion. Stores `document.activeElement` before swap for focus return.

### Success Criteria:

#### Automated Verification:

- Build succeeds: `mage build`
- Lint passes: `mage lint`
- CSS has no syntax errors (build embeds it — if embed fails, build fails)

#### Manual Verification:

- Status badges show icon + text, readable at all sizes
- Drawer slides in smoothly, respects reduced-motion preference
- Tab through routing table rows with keyboard, Enter opens drawer
- Long model IDs truncated with tooltip on hover, click copies
- Mobile viewport: table scrolls, drawer full-width, no horizontal overflow on page
- All interactive elements have visible focus indicators
- Screen reader: table announced as data table, drawer announced as dialog

**Implementation Note**: After completing this phase and all automated verification passes, pause here for manual confirmation from the human that the manual testing was successful before proceeding to the next phase.

---

## Phase 8: Playwright E2E Tests

### Overview

Add 3-5 targeted Playwright browser tests covering the JS/SSE/HTMX interactions that Go handler tests cannot verify: SSE activity feed updates, drawer open/close, attention panel reactivity, and filter deep-links.

### Changes Required:

#### 1. Playwright project setup

**File**: `e2e/playwright.config.ts` (or `.js`)

**Intent**: Initialize a minimal Playwright project in `e2e/`. Configure base URL to `http://localhost:8083` (default web port). Single browser (Chromium). No parallel to avoid port conflicts with the running freedius instance.

**Contract**: `npx playwright init` equivalent. `package.json` in `e2e/` with `@playwright/test` as dev dependency. `.gitignore` excludes `e2e/node_modules/` and test results.

#### 2. Test helper: start freedius instance

**File**: `e2e/helpers/server.ts`

**Intent**: Helper that starts a freedius binary (built via `mage build`) with a test config, waits for the health endpoint to respond, and provides cleanup (kill process). Used in `beforeAll`/`afterAll`.

**Contract**: Exports `startServer(configPath): Promise<{ port: number, cleanup: () => void }>`. Reads port from config or defaults to 8083.

#### 3. E2E Test: SSE activity feed

**File**: `e2e/tests/activity-feed.spec.ts`

**Intent**: Start server, load dashboard, send a proxy request via HTTP (to trigger an event), verify that the activity feed section updates in the browser without page refresh.

**Contract**: Assert that after sending a request through the proxy, a new row appears in `#activity-feed` within 5 seconds. Verify row contains the mapping name and status.

#### 4. E2E Test: Drawer open/close

**File**: `e2e/tests/drawer.spec.ts`

**Intent**: Load dashboard (with at least one mapping configured), click a routing table row, verify drawer appears with mapping details, press Escape, verify drawer closes.

**Contract**: Assert drawer element visible after click, contains mapping name and route chain. Assert drawer hidden after Escape. Verify focus returns to the clicked row.

#### 5. E2E Test: Attention panel reactivity

**File**: `e2e/tests/attention-panel.spec.ts`

**Intent**: Start server with a config that has a missing env var (provider configured but env var not set). Load dashboard, verify attention panel is visible with the expected alert message. Verify the alert links to the correct page.

**Contract**: Assert `.attention-panel` exists and contains text about missing API key. Click the alert link and verify navigation.

#### 6. E2E Test: Deep-link filters

**File**: `e2e/tests/deep-link-filters.spec.ts`

**Intent**: Navigate directly to `/logs?min=error&provider=nim`. Verify that filter controls are pre-populated and that displayed logs match the filters.

**Contract**: Assert level dropdown shows "Error" selected, provider input contains "nim". Assert all visible log entries match the filter criteria (or empty state if no matching logs).

### Success Criteria:

#### Automated Verification:

- Playwright tests pass: `cd e2e && npx playwright test`
- CI integration: tests run in GitHub Actions workflow (add step to `.github/workflows/ci.yml`)

#### Manual Verification:

- Run `npx playwright test --headed` and observe tests executing in browser
- All tests green on a clean build

**Implementation Note**: After completing this phase and all automated verification passes, pause here for manual confirmation from the human that the manual testing was successful before proceeding to the next phase.

---

## Testing Strategy

### Unit Tests:

- `proxy/stats_collector_test.go` — counter accuracy, concurrent access, zero-state, error rate calculation
- `proxy/web/attention_test.go` — rule evaluation: missing env var, error rate threshold, no-success timeout, missing provider reference
- `proxy/web/handlers_dashboard_test.go` — template render assertions for all dashboard sections

### Integration Tests:

- Handler tests with full StatsCollector wired: emit events, render dashboard, verify counts appear in HTML
- Filter combination tests: multiple query params applied simultaneously
- Drawer endpoint returns correct fragment for each mapping

### E2E Tests (Playwright):

- SSE activity feed live update
- Drawer open/close with focus management
- Attention panel presence/absence based on config
- Deep-link filter pre-population
- (Optional 5th) Delete confirmation modal flow on Mappings page

### Manual Testing Steps:

1. Start freedius with test config, load dashboard — verify health strip and empty states
2. Route 5-10 requests through the proxy — verify counts update on refresh, activity feed populates via SSE
3. Remove a provider API key env var, restart — verify attention panel appears with correct alert
4. Click a mapping row — verify drawer opens with correct details
5. Navigate to Mappings page — verify table layout, filters, Edit/Delete actions
6. Navigate to Providers page — verify status badges, Test Connection
7. Click an attention alert link — verify it deep-links to Logs with correct filters
8. Test keyboard navigation: Tab through table, Enter to open drawer, Escape to close

## Performance Considerations

- StatsCollector runs as a goroutine processing events from a buffered channel (100 buffer) — no impact on request latency.
- Dashboard handler reads from StatsCollector snapshots (copy under RLock) — same pattern as existing `Config.MappingsSnapshot()`.
- Template parsing: cached via existing `sync.Map` pattern — no per-request parsing.
- SSE activity feed: single EventBus subscriber per open dashboard tab. EventBus already handles 10k+ events without issue.
- CSS: no new external dependencies. Status badge SVG icons are inline (no network requests).
- Attention rules computed on render (not cached) — evaluating ~10 rules across ~10 providers/mappings is sub-millisecond.

## Migration Notes

- **Breaking template change**: `mappings-table.html` deleted. Both dashboard and Mappings page get new independent templates. No backward compatibility needed (solo dev, no external consumers).
- **New Go files**: `proxy/stats_collector.go`, `proxy/web/attention.go`. No changes to `go.mod` (no new dependencies).
- **New templates**: `mapping-drawer.html`, `mappings-routing-table.html`. Deleted: `mappings-table.html`.
- **Handler data types**: `indexData` replaced by `dashboardData`. `mappingsData` and `providersData` extended with new fields.
- **Existing tests**: Some `handlers_phase*_test.go` tests reference `indexData` or `mappings-table` template — these need updating to match new types/templates.
- **E2E infrastructure**: New `e2e/` directory with Node.js Playwright setup. Added to `.gitignore` (node_modules). CI workflow extended.

## References

- Prior UI refactor: `context/archive/2026-07-30-mapping-first-ui-refactor/plan.md`
- EventBus pattern: `proxy/eventbus.go`
- LogSink pattern: `proxy/logtee.go`
- LastResponder pattern: `proxy/lastresponder.go`
- Existing handler tests: `proxy/web/handlers_phase1_test.go`, `handlers_phase2_test.go`, `handlers_phase3_test.go`
- Lessons: `context/foundation/lessons.md` (json.Marshal for SSE, sync.Mutex+map for iteration, bundled struct injection)

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles. See `references/progress-format.md`.

### Phase 1: StatsCollector & Telemetry Backend

#### Automated

- [x] 1.1 Unit tests pass: `go test ./proxy/ -run TestStatsCollector` — 23507dd
- [x] 1.2 Build succeeds: `mage build` — 23507dd
- [x] 1.3 Lint passes: `mage lint` — 23507dd
- [x] 1.4 Full test suite: `mage test` — 23507dd

#### Manual

- [ ] 1.5 StatsCollector counters increment correctly after routing requests

### Phase 2: Dashboard Redesign (Templates + Handler)

#### Automated

- [x] 2.1 Build succeeds: `mage build` — d486689
- [x] 2.2 Handler tests pass: `go test ./proxy/web/ -run TestDashboard` — d486689
- [x] 2.3 Attention rules tests pass: `go test ./proxy/web/ -run TestComputeAlerts` — d486689
- [x] 2.4 Lint passes: `mage lint` — d486689
- [x] 2.5 Full test suite: `mage test` — d486689

#### Manual

- [ ] 2.6 Dashboard loads with health strip, routing table, and provider badges
- [ ] 2.7 Zero-traffic state renders gracefully with placeholder indicators
- [ ] 2.8 Attention panel appears/disappears based on config issues
- [ ] 2.9 Activity feed updates via SSE without page refresh

### Phase 3: Mapping Details Drawer

#### Automated

- [x] 3.1 Build succeeds: `mage build` — d06110b
- [x] 3.2 Drawer handler test passes: `go test ./proxy/web/ -run TestMappingDrawer` — d06110b
- [x] 3.3 Lint passes: `mage lint` — d06110b
- [x] 3.4 Full test suite: `mage test` — d06110b

#### Manual

- [ ] 3.5 Click row opens drawer with correct mapping details
- [ ] 3.6 Escape closes drawer, focus returns to triggering row
- [ ] 3.7 "Edit on Mappings page" link navigates correctly

### Phase 4: Mappings Page Table Refactor

#### Automated

- [x] 4.1 Build succeeds: `mage build` — e81d9c9
- [x] 4.2 Handler tests pass: `go test ./proxy/web/ -run TestMappings` — e81d9c9
- [x] 4.3 Lint passes: `mage lint` — e81d9c9
- [x] 4.4 Full test suite: `mage test` — e81d9c9

#### Manual

- [ ] 4.5 Mappings page shows compact table with filters
- [ ] 4.6 Search, provider filter, and "has fallback" filter work
- [ ] 4.7 Edit/Delete actions work via row menu and confirmation modal

### Phase 5: Providers Page Enhancement

#### Automated

- [x] 5.1 Build succeeds: `mage build` — 58d7c47
- [x] 5.2 Provider tests pass: `go test ./proxy/web/ -run TestProvider` — 58d7c47
- [x] 5.3 Lint passes: `mage lint` — 58d7c47
- [x] 5.4 Full test suite: `mage test` — 58d7c47

#### Manual

- [ ] 5.5 Providers page shows status badges and last-error info
- [ ] 5.6 "Test Connection" button works and updates row
- [ ] 5.7 Technical details expandable/collapsible

### Phase 6: Logs Page Deep-Link Filters

#### Automated

- [x] 6.1 Build succeeds: `mage build` — deba9b8
- [x] 6.2 Log filter tests pass: `go test ./proxy/web/ -run TestLog` — deba9b8
- [x] 6.3 Lint passes: `mage lint` — deba9b8
- [x] 6.4 Full test suite: `mage test` — deba9b8

#### Manual

- [ ] 6.5 `?outcome=error` and `?fallback=true` filters work
- [ ] 6.6 Dashboard links open Logs with correct pre-applied filters
- [ ] 6.7 System logs visually distinct from routing logs

### Phase 7: CSS & Accessibility Polish

#### Automated

- [x] 7.1 Build succeeds: `mage build` — 44ba8c2
- [x] 7.2 Lint passes: `mage lint` — 44ba8c2

#### Manual

- [ ] 7.3 Status badges show icon + text, sufficient contrast
- [ ] 7.4 Drawer animation smooth, respects prefers-reduced-motion
- [ ] 7.5 Keyboard navigation through table and drawer works
- [ ] 7.6 Model ID truncation + copy-to-clipboard works
- [ ] 7.7 Mobile viewport: no overflow, drawer full-width

### Phase 8: Playwright E2E Tests

#### Automated

- [x] 8.1 Playwright tests pass: `cd e2e && npx playwright test` — 7c66675
- [x] 8.2 CI workflow includes E2E step — 7c66675

#### Manual

- [ ] 8.3 Tests run headed and pass visually

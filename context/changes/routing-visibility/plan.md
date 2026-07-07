# Routing Visibility — Plan 1: Dashboard + Cross-Links

## Overview

Make routing a first-class concept on the Dashboard and connect Providers to
Mappings via cross-links. This is the first of three plans identified by the
routing-visibility frame brief: the remaining two cover richer per-step
metadata (Plan 2) and runtime stats / error rates (Plan 3).

The existing web-ui-friendliness plan (Phases 1–3) already shipped route-chain
cards with protocol badges, aria-labels, last-responder highlighting, click-
through to filtered logs, and silent-error feedback. This plan builds on that
foundation — reusing the `mappings-table.html` fragment on the Dashboard and
adding a provider filter to the `/mappings` route.

## Current State Analysis

- **Dashboard (`/`, `index.html:5-42`)**: Four non-interactive stat cards
  (Uptime, Total Events, Total Logs, Listening On). Zero routing context.
  Links to nothing. `indexData` struct (`types.go:13-20`) carries only these
  fields.
- **Providers page (`/providers`, `providers-table.html:33`)**: `MappingCount`
  is a plain integer — not a link. Users cannot click through to see which
  mappings reference a provider.
- **Mappings page (`/mappings`, `mappings-table.html`)**: Route-chain cards
  with protocol badges, aria-labels, last-responder chevron, and click-through
  to `/logs?provider=…&mapping=…`. Fully polished. No `?provider=` filter on
  the `/mappings` route itself.
- **`handleMappings` (`handlers.go:211-281`)** and **`renderMappingsTable`
  (`handlers.go:343-413`)**: Both build `[]mappingRow` independently with
  identical logic. Neither accepts query-param filters.
- **`handleLogs` (`handlers.go:97-173`)**: Already supports `?provider=` and
  `?mapping=` substring case-insensitive filters — pattern to follow.

### Key Discoveries:

- `handleMappings` and `renderMappingsTable` share identical mapping-building
  logic (lines 217-251 ≈ 349-383). Extracting a shared helper eliminates
  duplication and centralizes filter logic.
- `renderMappingsTable` is called from two places: `handleMappings` (line 272,
  HTMX path) and `handleMappings` (line 275, full-page path via `renderPage`
  with `"mappings-table.html"` extra file). Both need the filter.
- `index.html` already includes a template fragment via `{{template "X" .}}`
  — the `mappings-table` fragment can be included the same way.
- The `providers-table.html` template is used by `renderProvidersTable`
  (`handlers.go:307-341`) and by `handleProviders` (`handlers.go:199-208`).
  Both pass `providersData` — the template has access to `.Providers`.

## Desired End State

A user landing on the Dashboard at `:8083` sees:

- A **Mappings** section showing all mappings as route-chain cards (reusing
  `mappings-table.html` fragment). Each card shows the primary + fallback
  chain with protocol badges, last-responder chevron, and click-through to
  filtered logs.
- A **Providers** section showing each provider with its name and a
  `MappingCount` that is a clickable link to `/mappings?provider=<name>`.
- The current stat strip (Uptime, Events, Logs, Port) preserved in a compact
  row above the routing sections.
- Clicking `MappingCount` on `/providers` navigates to `/mappings?provider=<name>`,
  which shows only mappings that reference that provider (in primary or any
  fallback step).

Verification: load `/` — see mapping cards and provider list. Click a provider's
mapping count → `/mappings?provider=nim` filtered. Visit `/providers` — click
the count → same filtered view. Reload `/mappings?provider=nim` — filter
persists.

## What We're NOT Doing

- **No new /routing page** — the Dashboard replaces that role.
- **No runtime stats** (error rates, fallback trigger history) — Plan 3.
- **No richer per-step metadata** (Behavior, effective BaseURL, API Key Env,
  family matching visibility) — Plan 2.
- **No new sidebar links** — Dashboard is already the first link.
- **No changes to the route-chain card design** — reuses `mappings-table.html`
  as-is.
- **No client-side search/filter** — deferred to a future slice.
- **No a11y audit** — WCAG fixes ride along only where touched; no standalone
  audit.
- **No changes to `config.Mapping` or `config.Provider` data models** — all
  changes are in `proxy/web/` types and handlers.

## Implementation Approach

Extract shared mapping-building + filtering logic into a helper function.
Reuse the `mappings-table.html` fragment on the Dashboard by adding `Mappings`
and `Providers` fields to `indexData`. Change `MappingCount` in the providers
table from a plain integer to a link. All changes stay within `proxy/web/`.

## Critical Implementation Details

- **Template data shape**: `index.html` includes `{{template "mappings-table" .}}`.
  When the layout executes this, `.` is the `indexData` value. The
  `mappings-table` template accesses `.Mappings` and `.Providers` — so
  `indexData` must have exported fields with those exact names and types.
  Adding `Mappings []mappingRow` and `Providers []providerRow` to `indexData`
  satisfies this without template changes.
- **HTMX filter propagation**: When `/mappings?provider=nim` is visited via
  HTMX (e.g., from the providers page link), `handleMappings` receives the
  query param. The `renderMappingsTable` call must pass the filter through.
  Currently `renderMappingsTable` takes `(w, r, h)` — the `r` already carries
  the query. The helper extracts the filter from `r.URL.Query().Get("provider")`.
- **`MappingCount` includes primary-only**: `handleProviders` counts
  `m.ProviderName` matches only (line 183-185). This is correct for the link
  — clicking shows all mappings where this provider is the primary. Fallback
  references are not counted. This matches the user's "exact provider name
  match" expectation for the link target.

---

## Phase 1: Provider filter on `/mappings`

### Overview

Add `?provider=<name>` query parameter to the `/mappings` route. Filter is a
case-insensitive substring match against provider names in primary and fallback
steps. Extract shared mapping-building logic into a helper to eliminate
duplication between `handleMappings` and `renderMappingsTable`.

### Changes Required:

#### 1. Extract `buildMappingRows` helper

**File**: `proxy/web/handlers.go`

**Intent**: Extract the shared mapping-building logic from `handleMappings`
(lines 217-251) and `renderMappingsTable` (lines 349-383) into a single
`buildMappingRows` function. Both callers pass `(cfg, providers, lastResponder,
providerFilter)` and get back `[]mappingRow`.

**Contract**:

```go
func buildMappingRows(
    cfg *config.Config,
    providers map[string]config.Provider,
    lastResponder *proxy.LastResponder,
    providerFilter string,
) []mappingRow
```

The function:
1. Calls `cfg.MappingsSnapshot()`.
2. Iterates mappings; for each, checks if the provider filter matches (case-
   insensitive substring on primary `ProviderName` or any fallback
   `ProviderName`). Skips non-matching rows when filter is non-empty.
3. Builds `[]fallbackEntry` for each matching mapping (same logic as current).
4. Looks up `lastResponder.Lookup(name)` for the responder index.
5. Returns the filtered `[]mappingRow`.

When `providerFilter` is empty, all mappings pass (no filtering).

#### 2. Update `handleMappings` to use helper and read `?provider=`

**File**: `proxy/web/handlers.go`

**Intent**: Replace the inline mapping-building loop with a call to
`buildMappingRows`. Read `r.URL.Query().Get("provider")` and pass as filter.

**Contract**: `handleMappings` reads the provider filter from the query string
(case-insensitive, trimmed). Calls `buildMappingRows(cfg, providers,
h.LastResponder, providerFilter)`. The HTMX path (line 271-272) passes the
filter to `renderMappingsTable` via `r`. The full-page path (line 274-279)
passes the filtered rows to `renderPage`.

#### 3. Update `renderMappingsTable` to use helper and read `?provider=`

**File**: `proxy/web/handlers.go`

**Intent**: Replace the inline mapping-building loop with a call to
`buildMappingRows`. Read the provider filter from the request.

**Contract**: `renderMappingsTable` reads `r.URL.Query().Get("provider")`
(note: `renderMappingsTable` currently takes `_ *http.Request` — change the
blank identifier to `r`). Calls `buildMappingRows(cfg, providers,
h.LastResponder, providerFilter)`.

### Success Criteria:

#### Automated Verification:

- [ ] 1.1 `go build ./...` succeeds
- [ ] 1.2 `mage test` passes — all new + existing tests
- [ ] 1.3 `mage lint` clean
- [ ] 1.4 `TestMappingsProviderFilter_SubstringMatch` — create config with
  mappings using providers `nim` and `openai-nim`; GET `/mappings?provider=nim`;
  assert both mappings appear (substring match)
- [ ] 1.5 `TestMappingsProviderFilter_FallbackMatch` — create mapping where
  primary is `alpha` and fallback is `beta`; GET `/mappings?provider=beta`;
  assert the mapping appears (fallback match)
- [ ] 1.6 `TestMappingsProviderFilter_CaseInsensitive` — GET
  `/mappings?provider=NIM`; assert match against provider `nim`
- [ ] 1.7 `TestMappingsProviderFilter_EmptyShowsAll` — GET `/mappings` (no
  filter); assert all mappings appear
- [ ] 1.8 `TestMappingsProviderFilter_NoMatchShowsEmpty` — GET
  `/mappings?provider=nonexistent`; assert empty grid or empty-state CTA

#### Manual Verification:

- [ ] 1.9 Load `/mappings` with no filter — all mappings visible
- [ ] 1.10 Load `/mappings?provider=nim` — only mappings using `nim` (primary
  or fallback) appear
- [ ] 1.11 Reload `/mappings?provider=nim` — filter persists in URL

**Implementation Note**: After completing this phase and all automated
verification passes, pause for manual confirmation before proceeding to Phase 2.

---

## Phase 2: Provider cross-links

### Overview

Change `MappingCount` in the providers table from a plain integer to a
clickable link to `/mappings?provider=<name>`. Applies to both the full-page
render and the HTMX fragment render.

### Changes Required:

#### 1. Template: `MappingCount` as link

**File**: `proxy/web/templates/providers-table.html`

**Intent**: Replace the plain `{{.MappingCount}}` text (line 33) with a link
when count > 0.

**Contract**: Change the `MappingCount` cell to:

```html
<td>
  {{if gt .MappingCount 0}}
    <a href="/mappings?provider={{.Name}}">{{.MappingCount}}</a>
  {{else}}
    <span class="text-muted">0</span>
  {{end}}
</td>
```

When `MappingCount` is 0, render as muted "0" (no link). When > 0, render as
a link to `/mappings?provider=<name>`.

#### 2. Handler: pass provider name to template

**File**: `proxy/web/handlers.go`

**Intent**: The `providerRow` struct already carries `Name` (line 39). No Go
change needed — the template accesses `.Name` for the link URL. Verify that
both `handleProviders` (line 187-197) and `renderProvidersTable` (line 318-327)
populate `Name` correctly.

**Contract**: No Go change. Template-only change.

### Success Criteria:

#### Automated Verification:

- [ ] 2.1 `go build ./...` succeeds
- [ ] 2.2 `mage test` passes
- [ ] 2.3 `mage lint` clean
- [ ] 2.4 `TestProvidersTable_MappingCountLink` — create config with provider
  `nim` referenced by 2 mappings; GET `/providers`; assert the response
  contains `href="/mappings?provider=nim"` and the text `2`
- [ ] 2.5 `TestProvidersTable_ZeroMappingCount` — create provider with 0
  mappings; assert the response contains `text-muted">0</span>` and no link

#### Manual Verification:

- [ ] 2.6 Load `/providers` — MappingCount column shows links for providers
  with mappings, muted "0" for those without
- [ ] 2.7 Click a MappingCount link — navigates to `/mappings?provider=<name>`
  with correct filter applied

**Implementation Note**: After completing this phase and all automated
verification passes, pause for manual confirmation before proceeding to Phase 3.

---

## Phase 3: Dashboard as routing overview

### Overview

Replace the four stat cards on the Dashboard with a routing overview: a
Mappings section (using the `mappings-table` fragment) and a Providers section
(showing each provider with name, protocol badge, and mapping-count link).
Preserve the uptime/listening stats in a compact strip above the routing
sections.

### Changes Required:

#### 1. Data model: extend `indexData` with routing fields

**File**: `proxy/web/types.go`

**Intent**: Add `Mappings []mappingRow` and `Providers []providerRow` to
`indexData` so the Dashboard template can include the `mappings-table` fragment
and render the providers section.

**Contract**: The `indexData` struct (lines 13-20) gains two fields:

```go
type indexData struct {
    pageData
    Uptime      string
    TotalEvents int64
    TotalLogs   int64
    Port        string
    Host        string
    Mappings    []mappingRow
    Providers   []providerRow
}
```

The `Mappings` and `Providers` field names and types match what
`mappings-table.html` expects (`.Mappings` and `.Providers`).

#### 2. Handler: populate routing data in `handleIndex`

**File**: `proxy/web/handlers.go`

**Intent**: The `GET /` handler (lines 43-53) currently builds `indexData` with
only stats. Add logic to build `[]mappingRow` (via `buildMappingRows` from
Phase 1) and `[]providerRow` (same logic as `handleProviders`).

**Contract**: After building the stats fields, call `buildMappingRows(cfg,
providers, h.LastResponder, "")` (empty filter — show all). Build
`[]providerRow` from `cfg.ProvidersSnapshot()` with mapping counts. Pass both
to `indexData`.

#### 3. Template: Dashboard with routing sections

**File**: `proxy/web/templates/index.html`

**Intent**: Replace the four stat cards with a routing overview. Preserve
uptime/listening in a compact strip.

**Contract**: The new template structure:

1. **Stats strip**: A compact row showing Uptime and Listening On (preserve
   the two most useful stats; drop Total Events and Total Logs — they are
   available on the Logs page).
2. **Mappings section**: `{{template "mappings-table" .}}` — reuses the
   existing fragment. The `.` context is `indexData` which has `.Mappings` and
   `.Providers`.
3. **Providers section**: A compact list of providers. Each entry shows the
   provider name, protocol badge (if set), and mapping count as a link
   (reusing the same link pattern from Phase 2). No full table — just a
   scannable list.

The providers section is rendered inline in `index.html` (not via a fragment
template) since it is a compact list, not a full CRUD table.

#### 4. CSS: compact stats strip and providers list

**File**: `proxy/web/static/app.css`

**Intent**: Add styles for the compact stats strip and the providers list on
the Dashboard. Reuse existing design tokens only.

**Contract**: New CSS rules near the existing `.stats-grid` block:

- `.stats-strip` — horizontal row of compact stat pills; uses `--bg-card`,
  `--border-subtle`, `--space-2/3`, `--radius-md`.
- `.providers-overview` — compact list layout; flex column with gap.
- `.providers-overview__item` — single provider entry; flex row with name,
  protocol badge, and mapping-count link. Uses existing `.badge--protocol`
  for the protocol indicator.

### Success Criteria:

#### Automated Verification:

- [ ] 3.1 `go build ./...` succeeds
- [ ] 3.2 `mage test` passes — no regression in handler tests
- [ ] 3.3 `mage lint` clean
- [ ] 3.4 `TestIndexHandler_ReturnsMappings` — create config with 2 mappings
  and 1 provider; GET `/`; assert response body contains route-chain card
  markup (`class="route-card"`)
- [ ] 3.5 `TestIndexHandler_ReturnsProviders` — same config; assert response
  body contains provider name and mapping-count link (`href="/mappings?provider="`)
- [ ] 3.6 `TestIndexHandler_EmptyState` — config with no mappings; assert
  response contains the empty-state CTA from `mappings-table.html`
- [ ] 3.7 `TestIndexHandler_StatsPreserved` — assert response still contains
  Uptime and Listening On text (stats strip)

#### Manual Verification:

- [ ] 3.8 Load `/` — see mapping cards and provider list; stats strip at top
- [ ] 3.9 Click a mapping step pill — navigates to `/logs?provider=…&mapping=…`
  (existing Phase 2 behavior preserved)
- [ ] 3.10 Click a provider mapping-count link — navigates to
  `/mappings?provider=<name>` (Phase 2 link behavior)
- [ ] 3.11 Load `/` with no providers/mappings — empty-state CTA visible for
  mappings; providers section shows nothing or "No providers configured"
- [ ] 3.12 Last-responder chevron appears on the correct step after a
  successful fallback request

**Implementation Note**: After completing this phase and all automated
verification passes, the plan is ready for `/10x-impl-review`.

---

## Testing Strategy

### Unit Tests:

- Phase 1: Handler-level tests for `handleMappings` with `?provider=` filter.
  Table-driven tests covering substring match, fallback match, case
  insensitivity, empty filter, no match.
- Phase 2: Template-rendering tests for `providers-table.html` — assert link
  presence/absence based on `MappingCount`.
- Phase 3: Handler-level tests for `GET /` — assert `indexData` carries
  `Mappings` and `Providers`; assert template rendering includes route-chain
  card markup and provider links.

### Integration Tests:

- Existing `handlers_test.go` `TestPageHandlers` covers page status codes.
  New tests extend this pattern with content assertions.
- Existing `handlers_write_test.go` covers CRUD. No new write tests needed —
  this plan is read-only (no mutations).

### Manual Testing Steps:

End-to-end flow: load Dashboard → see routing overview → click provider link →
filtered /mappings → click mapping step → filtered /logs. Then visit
/providers → click MappingCount → same filtered /mappings.

## Performance Considerations

- `buildMappingRows` is called on every `/` and `/mappings` request. It
  snapshots config and iterates mappings — O(M × F) where M = mapping count
  and F = avg fallbacks. For typical configs (< 100 mappings, < 5 fallbacks
  each), this is negligible.
- The Dashboard now renders the same `mappings-table.html` fragment as
  `/mappings`. Template is cached after first parse (`pageTemplates` sync.Map).
  No per-request parsing overhead.
- No new SSE channels or polling endpoints in this plan.

## Migration Notes

- None. No config schema changes. No data migration. Existing users get the
  Dashboard upgrade automatically on reload.
- The `MappingCount` link changes the providers table visually (number becomes
  a link) but the data is identical — no behavioral change.

## References

- Frame brief: `context/changes/routing-visibility/frame.md`
- Prior research: `context/changes/web-ui-friendliness/research.md`
- Prior plan: `context/changes/web-ui-friendliness/plan.md`
- Foundation lessons: `context/foundation/lessons.md`
- Roadmap: `context/foundation/roadmap.md:236` (parked "Monitoring dashboard")

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a
> step lands. Do not rename step titles. See `references/progress-format.md`.

### Phase 1: Provider filter on `/mappings`

#### Automated

- [x] 1.1 `go build ./...` succeeds — eb9b659
- [x] 1.2 `mage test` passes — all new + existing tests — eb9b659
- [x] 1.3 `mage lint` clean — eb9b659
- [x] 1.4 `TestMappingsProviderFilter_SubstringMatch` — eb9b659
- [x] 1.5 `TestMappingsProviderFilter_FallbackMatch` — eb9b659
- [x] 1.6 `TestMappingsProviderFilter_CaseInsensitive` — eb9b659
- [x] 1.7 `TestMappingsProviderFilter_EmptyShowsAll` — eb9b659
- [x] 1.8 `TestMappingsProviderFilter_NoMatchShowsEmpty` — eb9b659

#### Manual

- [ ] 1.9 Load `/mappings` with no filter — all mappings visible
- [ ] 1.10 Load `/mappings?provider=nim` — only mappings using `nim` appear
- [ ] 1.11 Reload `/mappings?provider=nim` — filter persists in URL

### Phase 2: Provider cross-links

#### Automated

- [x] 2.1 `go build ./...` succeeds — 0cf8127
- [x] 2.2 `mage test` passes — 0cf8127
- [x] 2.3 `mage lint` clean — 0cf8127
- [x] 2.4 `TestProvidersTable_MappingCountLink` — 0cf8127
- [x] 2.5 `TestProvidersTable_ZeroMappingCount` — 0cf8127

#### Manual

- [ ] 2.6 Load `/providers` — MappingCount shows links for providers with
  mappings, muted "0" for those without
- [ ] 2.7 Click a MappingCount link — navigates to `/mappings?provider=<name>`

### Phase 3: Dashboard as routing overview

#### Automated

- [x] 3.1 `go build ./...` succeeds — 71097c0
- [x] 3.2 `mage test` passes — no regression — 71097c0
- [x] 3.3 `mage lint` clean — 71097c0
- [x] 3.4 `TestIndexHandler_ReturnsMappings` — 71097c0
- [x] 3.5 `TestIndexHandler_ReturnsProviders` — 71097c0
- [x] 3.6 `TestIndexHandler_EmptyState` — 71097c0
- [x] 3.7 `TestIndexHandler_StatsPreserved` — 71097c0

#### Manual

- [ ] 3.8 Load `/` — mapping cards and provider list visible; stats strip at
  top
- [ ] 3.9 Click a mapping step pill → `/logs?provider=…&mapping=…`
- [ ] 3.10 Click a provider mapping-count link → `/mappings?provider=<name>`
- [ ] 3.11 Empty state visible when no providers/mappings configured
- [ ] 3.12 Last-responder chevron appears on correct step after fallback

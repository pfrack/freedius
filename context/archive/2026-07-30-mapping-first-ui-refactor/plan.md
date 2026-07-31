# Mapping-First UI Refactor — Implementation Plan

## Overview

Refactor the Freedius Web UI so mappings are the primary operational entity. Redesign all four screens (Dashboard, Mappings, Providers, Logs) with mapping-first information hierarchy. Provider becomes subordinate metadata.

## Current State Analysis

- Sidebar: Dashboard → Logs → Providers → Mappings (Mappings is last)
- Dashboard: Two equal sections (Mappings + Providers) with minimal stats (Uptime + Host)
- Mapping cards: "ProviderName / Model" as visual anchor — provider dominates
- Logs: Single level-filter dropdown; backend supports provider/mapping filters but UI doesn't expose them
- All data needed for the redesign is already in template context (no backend changes needed)

### Key Discoveries:

- `proxy/web/templates/layout.html:30-55` — sidebar navigation order
- `proxy/web/templates/index.html` — dashboard with dual equal sections
- `proxy/web/templates/mappings-table.html` — shared fragment for mapping cards
- `proxy/web/types.go` — indexData already has Mappings + Providers + TotalEvents/TotalLogs
- HTMX swap targets `#mappings` and `#providers` must be preserved
- CSS design system (`app.css`) has established patterns (stats-grid, card, badge, route-card)

## Desired End State

- Mappings are the primary entity across all screens
- Sidebar: Dashboard → Mappings → Providers → Logs
- Dashboard: Stats grid (4 mapping-centric cards) + recent mappings section + demoted provider summary
- Mapping cards: name dominant (h3 bold), model as second line, provider as "via ..." metadata
- Logs: Exposed mapping + provider filter dropdowns alongside level filter
- Providers page: functional infrastructure page, no longer feels central

## What We're NOT Doing

- No backend/handler changes (data is already available)
- No JavaScript framework additions
- No new pages or routes
- No changes to HTMX CRUD behavior
- No changes to config/proxy logic
- No changes to test assertions beyond what template changes require

## Implementation Approach

Template-first, CSS-support. Change templates to render the new hierarchy, add CSS to style it, ensure existing HTMX swap targets (`#mappings`, `#providers`) remain intact. Work in phases matching each screen.

## Phase 1: Global Layout & Sidebar

### Overview

Reorder sidebar navigation and add any missing CSS primitives.

### Changes Required:

#### 1. Sidebar reorder

**File**: `proxy/web/templates/layout.html`

**Intent**: Reorder sidebar links to Dashboard → Mappings → Providers → Logs. Mappings moves from position 4 to position 2.

**Contract**: Same `<a>` elements, same `href` values, same icons — only DOM order changes.

#### 2. Stats grid CSS

**File**: `proxy/web/static/app.css`

**Intent**: Ensure `.stats-grid` and `.card--stat` primitives are ready (they already exist in the CSS; verify no additions needed). Add `.page-section` and `.section-header` classes if not present.

**Contract**: `.page-section { margin-bottom: var(--space-6) }`, `.section-header { display: flex; align-items: center; justify-content: space-between; margin-bottom: var(--space-3) }`.

### Success Criteria:

#### Automated Verification:

- `go build ./...` passes
- `go test ./proxy/web/...` passes

#### Manual Verification:

- Sidebar shows Dashboard → Mappings → Providers → Logs
- Active states work on all four pages

---

## Phase 2: Dashboard Redesign

### Overview

Replace the current dashboard with mapping-centric stats grid + recent mappings section + demoted providers row.

### Changes Required:

#### 1. Dashboard stats computation

**File**: `proxy/web/types.go`

**Intent**: Add computed stats fields to `indexData`: TotalMappings, ActiveMappings (env present), FallbackMappings (len(fallbacks)>0), TotalProviders.

**Contract**: Four new `int` fields on `indexData`.

#### 2. Dashboard handler stats computation

**File**: `proxy/web/handlers.go`

**Intent**: Compute the four new stats from existing `mappings` and `providers` data in the index handler.

**Contract**: Set `TotalMappings = len(mappings)`, count `ActiveMappings` where `.EnvPresent`, count `FallbackMappings` where `len(.Fallbacks) > 0`, `TotalProviders = len(providerRows)`.

#### 3. Dashboard template redesign

**File**: `proxy/web/templates/index.html`

**Intent**: Replace dual-section layout with: (a) stats grid (4 cards: Total Mappings, Active, With Fallbacks, Providers), (b) "Recent Mappings" section reusing mappings-table, (c) demoted providers summary as compact inline list.

**Contract**: Stats grid uses `.stats-grid > .card.card--stat` pattern already in CSS. Provider section becomes a compact `.providers-summary` row (name badges only, no full `providers-overview`).

#### 4. Dashboard stats CSS

**File**: `proxy/web/static/app.css`

**Intent**: Add `.providers-summary` compact inline styles. Remove or deprecate `.stats-strip` (replaced by stats-grid).

**Contract**: `.providers-summary` — flex row with wrapped badges.

### Success Criteria:

#### Automated Verification:

- `go build ./...` passes
- `go test ./proxy/web/...` passes

#### Manual Verification:

- Dashboard shows 4 stat cards at top
- Mapping list dominates the page
- Providers section is visually subordinate (small inline badges)
- Responsive: stats grid wraps to 2×2 on tablet, stacks on mobile

---

## Phase 3: Mappings Page Redesign

### Overview

Redesign mapping cards so mapping name is the dominant visual, model is second, and provider appears as subordinate "via" metadata.

### Changes Required:

#### 1. Mapping card template redesign

**File**: `proxy/web/templates/mappings-table.html`

**Intent**: Restructure route-card internals: (a) Header: name (h3 bold), status badge with label, family badge, fallback count, actions. (b) Body: model as primary text, "via provider" as muted metadata. (c) Route chain: show model names with small "via provider" beneath each step.

**Contract**: Preserve `id="mappings"` on outer div. Keep `hx-delete` / `hx-target` / `hx-swap` attributes on delete buttons. Keep `editMapping(this)` onclick with same data attributes.

#### 2. Route chain redesign CSS

**File**: `proxy/web/static/app.css`

**Intent**: Restyle `.route-step` so model name is the primary text and provider is secondary. Add `.route-step__model` (font-weight: 500, normal size) and `.route-step__provider` (font-size: 0.75rem, text-muted, "via" prefix). Change status-dot to a labeled badge.

**Contract**: `.route-step__model { font-weight: 500 }`, `.route-step__provider { font-size: 0.75rem; color: var(--text-muted) }`. Status badge: `.badge--status-ok`, `.badge--status-warn`.

#### 3. Delete button de-emphasis

**File**: `proxy/web/static/app.css`

**Intent**: Change mapping delete buttons from solid red `.btn--danger` to ghost style with error color text.

**Contract**: On route-card actions: use `.btn--ghost btn--sm btn--danger-subtle` class. Add `.btn--danger-subtle { color: var(--color-error); background: transparent; } .btn--danger-subtle:hover { background: rgba(239,68,68,0.1); }`.

### Success Criteria:

#### Automated Verification:

- `go build ./...` passes
- `go test ./proxy/web/...` passes

#### Manual Verification:

- Mapping name is the largest/boldest text in each card
- Model name is clearly readable as the primary target
- Provider appears as small "via ProviderName" text
- Status has a text label (not just a dot)
- Delete button is subtle (ghost style with red text, not solid red)
- Edit/Delete buttons work (HTMX swaps function)
- Add Mapping dialog still works

---

## Phase 4: Providers Page Polish

### Overview

Minor improvements to the Providers page — keep it functional but ensure it doesn't feel like the app's center.

### Changes Required:

#### 1. Provider table improvements

**File**: `proxy/web/templates/providers-table.html`

**Intent**: Truncate long Base URLs with `title` for full value. Make Mappings column link to `/mappings?provider=Name`. Keep table structure intact.

**Contract**: Base URL cell gets `class="url-truncate"` with `max-width: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap`. Preserve `id="providers"` on table.

#### 2. URL truncation CSS

**File**: `proxy/web/static/app.css`

**Intent**: Add `.url-truncate` utility class.

**Contract**: `.url-truncate { max-width: 200px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; display: inline-block; vertical-align: middle; }`.

### Success Criteria:

#### Automated Verification:

- `go build ./...` passes
- `go test ./proxy/web/...` passes

#### Manual Verification:

- Long URLs truncate with ellipsis
- Hovering shows full URL in tooltip
- Mapping count links work
- Add/Edit/Delete still work

---

## Phase 5: Logs Page Improvements

### Overview

Expose mapping and provider filter dropdowns alongside the existing level filter.

### Changes Required:

#### 1. Logs filter UI

**File**: `proxy/web/templates/logs.html`

**Intent**: Add provider and mapping text inputs (or small text fields) alongside the level dropdown. Wire them via HTMX `hx-get="/logs"` with `hx-include` to send all filter values together.

**Contract**: Add `<input name="provider" placeholder="Filter provider…">` and `<input name="mapping" placeholder="Filter mapping…">`. Both included in the HTMX request alongside the level select. The handler already supports `?provider=` and `?mapping=` query params.

#### 2. Logs page template data

**File**: `proxy/web/types.go`

**Intent**: Add `Provider` and `Mapping` string fields to `logsData` to preserve filter state on page load.

**Contract**: Two new string fields on `logsData`.

#### 3. Handler filter state

**File**: `proxy/web/handlers.go`

**Intent**: Pass provider/mapping query params through to `logsData` so the template can pre-fill the filter inputs.

**Contract**: `logsData.Provider = q.Get("provider")`, `logsData.Mapping = q.Get("mapping")`.

#### 4. Logs styling

**File**: `proxy/web/static/app.css`

**Intent**: Add `.log-filters` flex row with gap for the filter controls.

**Contract**: `.log-filters { display: flex; gap: var(--space-3); align-items: center; margin-bottom: var(--space-4); flex-wrap: wrap; }`.

### Success Criteria:

#### Automated Verification:

- `go build ./...` passes
- `go test ./proxy/web/...` passes

#### Manual Verification:

- Provider filter narrows log entries
- Mapping filter narrows log entries
- Filters combine with level filter
- Filters persist on page reload via URL params
- SSE stream still works

---

## Testing Strategy

### Unit Tests:

- Existing `proxy/web/*_test.go` tests validate handler behavior and template rendering
- No new test files needed — existing tests cover CRUD + rendering

### Manual Testing Steps:

1. Visit Dashboard — verify stats grid shows correct counts, mappings section dominates
2. Visit Mappings — verify cards show name > model > via provider hierarchy
3. Visit Providers — verify table is functional, URLs truncate
4. Visit Logs — verify new filters work with existing SSE stream
5. Test mobile responsive (resize to <768px) — sidebar collapses, cards stack
6. Test HTMX mutations (add/edit/delete mapping and provider) — confirm swap targets work

## Performance Considerations

No performance concerns — all changes are template rendering and CSS. No additional network requests or JavaScript.

## References

- Frame brief: `context/changes/mapping-first-ui-refactor/frame.md`
- Research: `context/changes/mapping-first-ui-refactor/research.md`
- Design system: `proxy/web/static/app.css`
- Handler types: `proxy/web/types.go`

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands.

### Phase 1: Global Layout & Sidebar

#### Automated

- [ ] 1.1 `go build ./...` passes
- [ ] 1.2 `go test ./proxy/web/...` passes

#### Manual

- [ ] 1.3 Sidebar shows Dashboard → Mappings → Providers → Logs
- [ ] 1.4 Active states work on all four pages

### Phase 2: Dashboard Redesign

#### Automated

- [ ] 2.1 `go build ./...` passes
- [ ] 2.2 `go test ./proxy/web/...` passes

#### Manual

- [ ] 2.3 Dashboard shows 4 stat cards at top
- [ ] 2.4 Mapping list dominates the page
- [ ] 2.5 Providers section is visually subordinate

### Phase 3: Mappings Page Redesign

#### Automated

- [ ] 3.1 `go build ./...` passes
- [ ] 3.2 `go test ./proxy/web/...` passes

#### Manual

- [ ] 3.3 Mapping name is dominant visual in each card
- [ ] 3.4 Provider appears as "via" metadata
- [ ] 3.5 HTMX mutations still work (add/edit/delete)

### Phase 4: Providers Page Polish

#### Automated

- [ ] 4.1 `go build ./...` passes
- [ ] 4.2 `go test ./proxy/web/...` passes

#### Manual

- [ ] 4.3 Long URLs truncate
- [ ] 4.4 Provider CRUD still works

### Phase 5: Logs Page Improvements

#### Automated

- [ ] 5.1 `go build ./...` passes
- [ ] 5.2 `go test ./proxy/web/...` passes

#### Manual

- [ ] 5.3 Provider and mapping filters work
- [ ] 5.4 SSE stream still functions

# Web UI Modernization Implementation Plan

## Overview

Replace the entire CSS layer and update all 8 HTML templates to transform freedius from a minimal white/blue dashboard into a modern dark-mode-first admin panel with sidebar navigation, inline SVG icons, subtle animations, and mobile responsiveness. Refactor the SSE log handler to emit JSON instead of pre-rendered HTML, decoupling Go code from CSS class names.

## Current State Analysis

The current UI is a 226-line CSS file with light-mode-first custom properties, a horizontal `<nav>` bar, plain tables, and minimal styling. The dashboard page is empty. All 15 HTML IDs are consumed by HTMX targets and inline JS — they must be preserved exactly. The SSE handler in `internal/eventstream/handlers.go:160-163,182-187` hardcodes `<pre class="log-{level}">` HTML, creating a tight coupling between Go code and CSS class names.

Key constraints:
- No custom `FuncMap` in Go templates — only built-in `| js` escaper
- Template caching via `sync.Map` — binary rebuild required for any template change
- Static assets embedded via `//go:embed` — no build tools needed
- All HTMX attributes (`hx-target`, `hx-swap`, `hx-post/put/delete`, `sse-connect`, `sse-swap`) must remain functional
- `{{define "..."}}` block names, `{{block "content" .}}`, `{{block "title" .}}`, `{{block "scripts" .}}` must not be renamed

## Desired End State

A dark-mode-first admin panel with: fixed 240px sidebar navigation (hamburger on mobile), card-based layout with zinc-toned backgrounds, indigo accent color, inline SVG icons on nav links and action buttons, badge system for provider protocols, subtle CSS transitions on hover/focus, and a dashboard page showing proxy status cards (uptime, events, logs, port). The SSE handler emits JSON; client-side JS renders log lines. All existing CRUD and HTMX functionality works unchanged.

### Key Discoveries:

- `proxy/web/templates/layout.html:10-15` — Current nav is a simple `<nav>` with `<a>` tags; must restructure to sidebar with SVG icons
- `internal/eventstream/handlers.go:160-163` — SSE log handler hardcodes `<pre class="log-%s">` in two places (replay + live stream); refactoring to JSON emission requires updating both locations
- `proxy/web/templates/logs.html:21-26` — Current `hx-ext="sse"` / `sse-swap="log"` expects HTML in the SSE data field; after refactoring, client JS must intercept and render JSON
- `proxy/web/handlers.go:114-137` — `handleLogs` renders server-side log entries into the template; these also need new CSS classes after the redesign
- `proxy/web/embed.go:51-65` — `loadPageTemplate` caches templates by page file; new template structure must work within this caching model
- All 15 HTML IDs (`providers`, `mappings`, `provider-dialog`, `provider-form`, `provider-dialog-title`, `provider-form-error`, `provider-form-error-dialog`, `mapping-dialog`, `mapping-form`, `mapping-dialog-title`, `mapping-form-error`, `mapping-form-error-dialog`, `model-suggestions`, `level-filter`, `log`) must be preserved exactly

## What We're NOT Doing

- No new backend features or API endpoints (dashboard stats come from existing `/v1/stats`)
- No JavaScript framework — vanilla JS + HTMX only
- No CSS preprocessor (Sass/Less) — pure CSS with custom properties
- No build step or bundler — everything stays in `//go:embed`
- No new Go `FuncMap` entries or template helpers
- No changes to config, proxy, or provider logic
- No accessibility audit (out of scope — future work)
- No i18n or localization

## Implementation Approach

**Phase 1** establishes the design system: a complete CSS rewrite with dark-mode-first custom properties, sidebar layout, base component styles (buttons, forms, tables, badges, dialogs, animations). This phase modifies only `app.css` and `layout.html` — no page templates change yet, so all existing functionality continues to work.

**Phase 2** updates all 8 page/fragment templates to use the new CSS classes, refactors the SSE handler to emit JSON, and adds a client-side log renderer. This is the highest-risk phase because it touches both Go code and templates simultaneously.

**Phase 3** adds dashboard content (status cards), subtle CSS transitions/animations, and final visual polish across all pages.

## Critical Implementation Details

- **SSE JSON refactoring**: The SSE handler at `internal/eventstream/handlers.go:160-163,182-187` must emit `{"level":"info","line":"..."}` JSON instead of `<pre class="log-info">...</pre>` HTML. The client-side JS in `logs.html` must listen for the `log` SSE event, parse JSON, and append rendered `<pre>` elements to `#log`. The `hx-swap="beforeend scroll:#log:bottom"` attribute on the `#log` div must be preserved — the JS appends to the same target.

- **HTMX fragment contract**: When HTMX swaps in a new `providers-table` or `mappings-table` fragment, the returned HTML must contain `<table id="providers">` or `<table id="mappings">` exactly — these are the `hx-target` values. Do not wrap tables in additional containers during the redesign.

- **Dashboard stats**: The `indexData` struct in `proxy/web/types.go:11-13` must be extended with uptime, event count, log count, port, and host fields. The handler at `proxy/web/handlers.go:38-40` must fetch stats from `h.Bus` and `h.LogSink` and pass them to the template. This requires access to the `eventstream.Handlers` struct's fields.

- **Sidebar vs horizontal nav**: The current `layout.html` nav element must become a sidebar `<aside class="sidebar">` with the `<nav>` inside it. The `<main>` element wraps content. CSS must handle the sidebar offset on desktop and the hamburger overlay on mobile. The `{{if eq .Active "..."}}` conditionals for nav highlighting must be preserved.

## Phase 1: CSS Design System + Layout Shell

### Overview

Write the complete new CSS design system and restructure `layout.html` to use sidebar navigation. All page templates continue using old CSS classes — they still work because we add new classes alongside old ones.

### Changes Required:

#### 1. Complete CSS Design System

**File**: `proxy/web/static/app.css`

**Intent**: Full rewrite of the CSS file with a dark-mode-first design system: CSS custom properties for all colors, typography, spacing; sidebar layout styles; base component classes (buttons, forms, tables, badges, dialogs, cards); utility classes; mobile responsive breakpoints; subtle transition animations.

**Contract**: Must define CSS custom properties matching the research color palette (zinc-based dark mode as default, light mode via `@media (prefers-color-scheme: light)`). Must provide `.sidebar`, `.sidebar-overlay`, `.hamburger` classes for mobile nav. Must define `.btn` base with `--primary`, `--secondary`, `--danger`, `--ghost` modifiers. Must define `.badge` with `.badge--openai`, `.badge--anthropic`, `.badge--protocol` modifiers. Must define `.table-wrap` card container. Must define `.form-group`, `.form-label`, `.form-input`, `.form-select` with focus ring. Must define `.card` for dashboard stats. Must define animation keyframes: `fade-in`, `slide-up`. Must preserve `.log-debug`, `.log-info`, `.log-warn`, `.log-error` classes (used by server-rendered logs and client-side renderer).

### Success Criteria:

#### Automated

- 1.1 CSS file compiles without errors (no build step, but validate syntax): `grep -c 'var(--' proxy/web/static/app.css` returns 15+ custom property usages
- 1.2 All original CSS classes still exist (for backward compat during phase 2): `grep -E '\.log-(debug|info|warn|error)|\.btn-primary|\.btn-danger|\.btn-sm|\.btn-cancel|\.form-error|\.form-actions|\.model-list|\.text-muted' proxy/web/static/app.css` returns matches
- 1.3 Dark mode variables are default (not inside media query): `:root { --bg-root:` appears before any `@media`

#### Manual

- 1.4 Open the app in browser — sidebar renders correctly on desktop (240px fixed left, content offset)
- 1.5 Resize to mobile width — hamburger icon appears, sidebar collapses
- 1.6 Click hamburger — sidebar slides in as overlay
- 1.7 Existing pages (providers, mappings, logs) still look functional with old template classes

**Implementation Note**: After completing this phase and all automated verification passes, pause here for manual confirmation from the human that the manual testing was successful before proceeding to the next phase.

---

## Phase 2: Page Templates + SSE Refactor

### Overview

Update all 8 HTML templates to use new CSS classes, restructure layout.html for sidebar nav with SVG icons, refactor SSE handler to emit JSON, add client-side log renderer, and update dashboard handler to pass stats data.

### Changes Required:

#### 1. Sidebar Layout with SVG Icons

**File**: `proxy/web/templates/layout.html`

**Intent**: Replace horizontal `<nav>` with a fixed sidebar containing SVG icon + text nav links. Add hamburger button for mobile. Add `<main>` wrapper with content offset. Preserve all `{{if eq .Active "..."}}` conditionals and `{{block "content" .}}`, `{{block "title" .}}`, `{{block "scripts" .}}`.

**Contract**: The `<nav>` element becomes `<aside class="sidebar"><nav>...</nav></aside>`. Each `<a>` gets an inline SVG icon before the text. The hamburger `<button class="hamburger">` toggles `.sidebar--open` on the aside. The `{{define "layout"}}` block name is unchanged.

#### 2. Dashboard Page with Status Cards

**File**: `proxy/web/templates/index.html`

**Intent**: Replace empty dashboard with a page header and 4 stat cards showing uptime, total events, total logs, and port/host. Each card uses the `.card` class with an icon, label, and value.

**Contract**: Template must access `.Uptime`, `.TotalEvents`, `.TotalLogs`, `.Port`, `.Host` from the data struct. Cards use `.card` class. No HTMX attributes needed — static content.

#### 3. Providers Page Update

**File**: `proxy/web/templates/providers.html`

**Intent**: Update button classes (`btn-primary` → `btn btn--primary`), form classes (`form-error` → `form-error`, keep), dialog styling. Preserve all HTML IDs, HTMX attributes, and JS `editProvider` function exactly.

**Contract**: `#provider-dialog`, `#provider-form`, `#provider-dialog-title`, `#provider-form-error`, `#provider-form-error-dialog` IDs preserved. `hx-post`, `hx-target="#providers"`, `hx-swap="outerHTML"`, `hx-on::after-request` preserved. Add SVG icon to "+ Add Provider" button.

#### 4. Providers Table Fragment

**File**: `proxy/web/templates/providers-table.html`

**Intent**: Wrap table in `.table-wrap` card. Add badge for protocol column. Update button classes. Preserve `<table id="providers">` exactly.

**Contract**: `<table id="providers">` preserved as root element (HTMX swap target). Protocol column gets `.badge .badge--protocol` span. Edit/Delete buttons use `.btn .btn--ghost .btn-sm` and `.btn .btn-danger .btn-sm`.

#### 5. Mappings Page Update

**File**: `proxy/web/templates/mappings.html`

**Intent**: Same as providers — update button/form classes. Preserve all IDs, HTMX attributes, and JS `editMapping` + model suggestions click handler.

**Contract**: `#mapping-dialog`, `#mapping-form`, `#mapping-dialog-title`, `#mapping-form-error`, `#mapping-form-error-dialog`, `#model-suggestions` IDs preserved. All HTMX and JS behavior unchanged.

#### 6. Mappings Table Fragment

**File**: `proxy/web/templates/mappings-table.html`

**Intent**: Wrap table in `.table-wrap` card. Update button classes. Preserve `<table id="mappings">`.

**Contract**: `<table id="mappings">` preserved. Buttons use `.btn` modifier classes.

#### 7. Logs Page with Client-Side Renderer

**File**: `proxy/web/templates/logs.html`

**Intent**: Add inline `<script>` that listens for SSE `log` events, parses JSON, and appends rendered `<pre class="log-{level}">` elements to `#log`. Keep server-rendered entries in the initial HTML (for non-JS fallback and HTMX filter). Add SVG icon to filter label. Preserve all IDs and HTMX/SSE attributes.

**Contract**: `#level-filter` and `#log` IDs preserved. `hx-ext="sse"`, `sse-connect="/v1/logs"`, `sse-swap="log"`, `hx-swap="beforeend scroll:#log:bottom"` preserved. Client JS: `document.getElementById('log').addEventListener('log-event', ...)` or use HTMX SSE extension's event handling. The JS must parse JSON `{level, line}` and create `<pre class="log-{level}">{escaped line}</pre>`.

#### 8. Models Fragment Update

**File**: `proxy/web/templates/models-fragment.html`

**Intent**: Update `.model-list` and `.text-muted` classes to match new design system. No structural changes needed.

**Contract**: Template logic unchanged. `.model-list li` items keep `data-model-id` attribute.

#### 9. SSE Handler JSON Refactoring

**File**: `internal/eventstream/handlers.go`

**Intent**: Change `handleLogs` to emit JSON objects `{"level":"debug","line":"..."}` instead of pre-rendered HTML `<pre class="log-debug">...</pre>`. Update both the replay loop (lines 158-167) and the live stream loop (lines 174-192).

**Contract**: SSE event type stays `"log"`. Data changes from HTML string to JSON object. The `html.EscapeString` calls are removed from Go code (escaping moves to client-side JS). The `levelLabel` function stays unchanged.

#### 10. Dashboard Handler + Types

**File**: `proxy/web/handlers.go` and `proxy/web/types.go`

**Intent**: Extend `indexData` struct with stats fields. Update the `GET /` handler to fetch uptime, event count, log count, port, host from `h.Bus` and `h.LogSink` and pass to template.

**Contract**: `indexData` gets fields: `Uptime string`, `TotalEvents int64`, `TotalLogs int64`, `Port string`, `Host string`. Handler accesses `h.StartTime`, `h.Bus.EventCount()`, `h.LogSink.EventCount()`, `h.Port`, `h.Host`.

### Success Criteria:

#### Automated

- 2.1 `mage build` succeeds — binary compiles with all template and Go changes
- 2.2 `mage test` passes — no regressions in existing tests
- 2.3 `mage lint` passes — no vet/staticcheck issues
- 2.4 All 15 HTML IDs present in templates: `grep -rn 'id="providers"\|id="mappings"\|id="provider-dialog"\|id="provider-form"\|id="provider-dialog-title"\|id="provider-form-error"\|id="provider-form-error-dialog"\|id="mapping-dialog"\|id="mapping-form"\|id="mapping-dialog-title"\|id="mapping-form-error"\|id="mapping-form-error-dialog"\|id="model-suggestions"\|id="level-filter"\|id="log"' proxy/web/templates/`
- 2.5 SSE handler emits JSON: `grep -c 'json.Marshal' internal/eventstream/handlers.go` returns expected count
- 2.6 No hardcoded `<pre class="log-` in Go code: `grep -c '<pre class="log-' internal/eventstream/handlers.go` returns 0

#### Manual

- 2.7 Open providers page — table renders with new styling, Edit/Delete buttons work
- 2.8 Open mappings page — table renders, Add/Edit/Delete work, model suggestions load
- 2.9 Open logs page — server-rendered entries display, live SSE entries append with correct colors
- 2.10 Filter logs by level — HTMX filter works, entries update correctly
- 2.11 Dashboard shows uptime, event count, log count, port/host cards
- 2.12 Mobile: hamburger toggles sidebar, all pages are usable

**Implementation Note**: After completing this phase and all automated verification passes, pause here for manual confirmation from the human that the manual testing was successful before proceeding to the next phase.

---

## Phase 3: Dashboard + Polish

### Overview

Add subtle CSS transitions/animations, final visual polish across all pages, and verify mobile responsiveness end-to-end.

### Changes Required:

#### 1. CSS Transitions and Animations

**File**: `proxy/web/static/app.css`

**Intent**: Add `transition` properties to interactive elements (buttons, nav links, table rows). Add `@keyframes fade-in` for page load. Add `@keyframes slide-up` for cards. Ensure `prefers-reduced-motion` media query disables animations.

**Contract**: All `transition` properties use `150ms ease` or faster. `prefers-reduced-motion: reduce` sets `animation: none` and `transition: none`.

#### 2. Final Visual Polish

**File**: `proxy/web/static/app.css` and select templates

**Intent**: Fine-tune spacing, border-radius, shadow depths, and color contrast across all components. Ensure consistent visual hierarchy. Add focus-visible outlines for keyboard navigation.

**Contract**: No structural HTML changes. CSS-only refinements.

### Success Criteria:

#### Automated

- 3.1 `mage build` succeeds
- 3.2 `mage test` passes
- 3.3 `mage lint` passes

#### Manual

- 3.4 Hover states on buttons and nav links show subtle transition
- 3.5 Page load has fade-in animation
- 3.6 Dashboard cards have slide-up entrance
- 3.7 All pages look polished and consistent in dark mode
- 3.8 Light mode (via OS preference) renders correctly
- 3.9 Mobile responsive at 375px, 768px, 1024px widths
- 3.10 Keyboard navigation (Tab) shows focus outlines

---

## Testing Strategy

### Unit Tests

- No new Go unit tests needed — this is a CSS/HTML/JS change
- Existing tests in `proxy/web/*_test.go` must continue passing

### Integration Tests

- Full page render: navigate to each page, verify HTML structure
- HTMX CRUD: create/edit/delete provider and mapping via UI
- SSE log streaming: verify live logs append with correct JSON parsing
- Log level filter: verify HTMX filter updates log entries

### Manual Testing Steps

1. Run `mage run` and open browser to `http://localhost:PORT`
2. Verify sidebar navigation works on desktop (240px fixed)
3. Resize to mobile width — hamburger appears, sidebar collapses
4. Click hamburger — sidebar slides in as overlay
5. Navigate to each page — all render with new design
6. Dashboard shows 4 stat cards with live data
7. Providers: Add provider via dialog, Edit existing, Delete with confirmation
8. Mappings: Add mapping, Fetch models, Edit, Delete
9. Logs: Verify server-rendered entries display, live SSE entries append
10. Logs: Filter by level — entries update correctly
11. Toggle OS dark/light mode — both themes render correctly
12. Tab through all interactive elements — focus outlines visible

## Performance Considerations

- CSS custom properties are resolved at paint time — no performance impact
- Inline SVG icons are small (~200 bytes each) — negligible payload increase
- Client-side log JSON parsing adds ~0.1ms per log line — negligible
- No new network requests beyond existing ones
- Template caching unchanged — no cold-start regression

## Migration Notes

- No data migration needed — this is purely visual
- The SSE contract change (HTML → JSON) is backward-incompatible for any external SSE clients — but the only consumer is the web UI's HTMX SSE extension, which is updated in the same phase
- Static asset cache headers (`max-age=300`) may cause stale CSS for users who don't hard-refresh — acceptable for development use

## References

- Related research: `context/changes/web-ui-redesign/research.md`
- SSE encoding lesson: `context/foundation/lessons.md` (json.Marshal over json.NewEncoder)
- CSS custom properties: `proxy/web/static/app.css:1-27`
- SSE log handler: `internal/eventstream/handlers.go:135-193`
- Template loading: `proxy/web/embed.go:51-65`
- All HTML IDs: `context/changes/web-ui-redesign/research.md:98-116`

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles.

### Phase 1: CSS Design System + Layout Shell

#### Automated

- [ ] 1.1 CSS file has 15+ custom property usages
- [ ] 1.2 All original CSS classes preserved
- [ ] 1.3 Dark mode variables are default

#### Manual

- [ ] 1.4 Sidebar renders correctly on desktop
- [ ] 1.5 Hamburger appears on mobile width
- [ ] 1.6 Hamburger toggles sidebar overlay
- [ ] 1.7 Existing pages still functional

### Phase 2: Page Templates + SSE Refactor

#### Automated

- [ ] 2.1 `mage build` succeeds
- [ ] 2.2 `mage test` passes
- [ ] 2.3 `mage lint` passes
- [ ] 2.4 All 15 HTML IDs present
- [ ] 2.5 SSE handler emits JSON
- [ ] 2.6 No hardcoded `<pre class="log-` in Go

#### Manual

- [ ] 2.7 Providers table renders with new styling
- [ ] 2.8 Mappings CRUD works end-to-end
- [ ] 2.9 Logs: server-rendered + live SSE entries display
- [ ] 2.10 Log level filter works
- [ ] 2.11 Dashboard shows stat cards
- [ ] 2.12 Mobile sidebar works

### Phase 3: Dashboard + Polish

#### Automated

- [ ] 3.1 `mage build` succeeds
- [ ] 3.2 `mage test` passes
- [ ] 3.3 `mage lint` passes

#### Manual

- [ ] 3.4 Hover transitions work
- [ ] 3.5 Page load fade-in animation
- [ ] 3.6 Dashboard card entrance animation
- [ ] 3.7 All pages polished in dark mode
- [ ] 3.8 Light mode renders correctly
- [ ] 3.9 Mobile responsive at all breakpoints
- [ ] 3.10 Keyboard focus outlines visible

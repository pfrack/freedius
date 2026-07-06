# Breadcrumb-Chain Mapping Visualization — Implementation Plan

## Overview

Replace the flat HTML `<table>` on the mappings page with a **breadcrumb-chain card layout**. Each mapping becomes a self-contained card displaying a left-to-right pipeline: the mapping name (request entry) → primary provider/model → fallback 1 → fallback 2 → ... Each step is a colored pill connected by CSS chevron arrows. Role-based colors distinguish primary (green) from fallback (amber) steps. On mobile (<768px), the chain stacks vertically with downward arrows.

## Current State Analysis

The mappings page (`proxy/web/templates/mappings.html` + `mappings-table.html`) renders a standard `<table>` with columns: Name, Provider, Model, Fallback, Actions. The "Fallback" column contains a pre-formatted string (e.g., `→ zen/claude, → nim/step`) generated in `handleMappings` / `renderMappingsTable` by joining fallback entries.

The fragment is HTMX-swappable via `hx-target="#mappings" hx-swap="outerHTML"` — all CRUD mutations (create, update, delete) replace the `<table id="mappings">` element.

The edit dialog (`editMapping()` JS function) currently reads data from `onclick` attributes on the Edit button and reconstructs fallback rows by parsing the formatted string.

### Key Discoveries:

- `proxy/web/types.go:48-55` — `mappingRow` has `Fallbacks string` (pre-formatted). Must restructure to carry `[]fallbackEntry` for per-step template rendering.
- `proxy/web/embed.go:53-60` — `loadPageTemplate` accepts `extraFiles`. The mappings page uses `extraFiles=["mappings-table.html"]`. Renaming the fragment file requires updating the handler call.
- `proxy/web/handlers.go:306+` — `renderMappingsTable` loads the fragment via `loadFragmentTemplate("mappings-table.html")`. HTMX swap path must stay stable.
- `proxy/web/static/app.css` — Design system provides `--color-success` (#22c55e), `--color-warning` (#f59e0b), `--accent` (#6366f1), `--radius-md`, `--space-*` variables. New CSS extends these — no new color definitions needed.
- Template cache uses `sync.Map` keyed by filename — changing the fragment filename means a new cache entry (no invalidation issues).

## Desired End State

The `/mappings` page displays a vertical stack of cards. Each card shows:
- The mapping name as a heading
- A left-to-right pipeline of steps: `[mapping-name] ▶ [provider / model] ▶ [fallback-provider / model] ▶ ...`
- Primary step has green left-border/accent, fallback steps have amber
- Edit/Delete buttons at top-right of each card
- On mobile, the pipeline steps stack vertically with downward chevrons
- All existing HTMX mutations (create, update, delete) continue working — they swap `#mappings` outerHTML
- The edit dialog pre-populates correctly from the new card structure

Verification: load `/mappings` in a browser, see card-based chain view. Create/edit/delete a mapping — cards update via HTMX. Resize to mobile width — steps stack vertically. Existing Go tests pass unchanged.

## What We're NOT Doing

- No SVG graph view (future enhancement — can layer on top later)
- No drag-and-drop reordering of fallbacks
- No new JavaScript libraries or external dependencies
- No changes to the data model (`config.Mapping` struct stays the same)
- No changes to the providers page
- No view toggle (table/graph) — full replacement

## Implementation Approach

1. Restructure `mappingRow` to carry typed fallback data (not a string) so templates can iterate individual steps.
2. Replace the `mappings-table.html` fragment with a card-based layout using CSS flexbox for the chain.
3. Add CSS classes for the pipeline steps, chevron connectors, and responsive breakpoints.
4. Update the `editMapping()` JS to read data-attributes from the new cards instead of parsing the formatted string.

## Phase 1: Data Layer Restructure

### Overview

Change `mappingRow` to expose structured fallback entries so templates can render each chain step independently.

### Changes Required:

#### 1. Types — add `fallbackEntry` struct

**File**: `proxy/web/types.go`

**Intent**: Add a `fallbackEntry` struct (ProviderName, Model string) and change `mappingRow.Fallbacks` from `string` to `[]fallbackEntry`. This lets the template iterate over individual fallback steps rather than rendering a pre-formatted string.

**Contract**:
```go
type fallbackEntry struct {
    ProviderName string
    Model        string
}

// String returns a formatted fallback string (e.g., "→ provider/model").
func (f fallbackEntry) String() string {
    return fmt.Sprintf("→ %s/%s", f.ProviderName, f.Model)
}

type mappingRow struct {
    Name         string
    ProviderName string
    Model        string
    Fallbacks    []fallbackEntry
}

// FallbacksString returns a comma-separated string of all fallbacks (e.g., "→ zen/claude, → nim/step").
func (m mappingRow) FallbacksString() string {
    var parts []string
    for _, fb := range m.Fallbacks {
        parts = append(parts, fb.String())
    }
    return strings.Join(parts, ", ")
}
```

#### 2. Handler — populate structured fallbacks

**File**: `proxy/web/handlers.go`

**Intent**: In `handleMappings` and `renderMappingsTable`, populate `Fallbacks` as `[]fallbackEntry` instead of joining into a string. Remove the string-formatting logic.

**Contract**: The loop over `m.Fallback` appends `fallbackEntry{ProviderName: fb.ProviderName, Model: fb.ModelString}` to the row's Fallbacks slice. Both `handleMappings` (full page render) and `renderMappingsTable` (fragment render) must use the same construction logic.

### Success Criteria:

#### Automated Verification:

- Existing tests compile: `go build ./...`
- Existing handler tests pass: `go test ./proxy/web/...`
- `FallbacksString()` method compiles and matches old string format.

#### Manual Verification:

- Page still loads (may look broken until Phase 2 lands the new template)
- Rollback plan documented: If Phase 1 breaks tests or UI, revert `types.go` and `handlers.go` to restore the `Fallbacks string` format.

**Implementation Note**: After completing this phase and all automated verification passes, pause here for manual confirmation from the human that the manual testing was successful before proceeding to the next phase.

---

## Phase 2: Template + CSS

### Overview

Replace the table-based `mappings-table.html` with a card-based breadcrumb chain layout and add supporting CSS for the pipeline steps, chevrons, role colors, and mobile responsiveness.

### Changes Required:

#### 1. Replace mappings-table.html with card-based chain

**File**: `proxy/web/templates/mappings-table.html`

**Intent**: Replace the `<table>` markup with a `<div id="mappings">` containing a card per mapping. Each card has a header (mapping name + action buttons) and a `.route-chain` flex container with `.route-step` pill elements connected by CSS chevrons.

**Contract**: The template defines `{{define "mappings-table"}}` (same define name for backward compatibility). Structure:

```html
{{define "mappings-table"}}
<div id="mappings" class="mappings-grid">
  {{range .Mappings}}
  <div class="route-card">
    <div class="route-card__header">
      <h3 class="route-card__name">{{.Name}}</h3>
      <div class="route-card__actions">
        <!-- Edit/Delete buttons with data-* attributes -->
      </div>
    </div>
    <div class="route-chain">
      <div class="route-step route-step--primary">...</div>
      {{range .Fallbacks}}
      <div class="route-step route-step--fallback">...</div>
      {{end}}
    </div>
  </div>
  {{end}}
</div>
{{end}}
```

The Edit button carries `data-name`, `data-provider`, `data-model`, and `data-fallbacks` (JSON-encoded array) attributes for JS consumption. The `data-fallbacks` attribute will contain a JSON array with the following schema:
```json
[
  {"provider_name": "zen", "model": "claude"},
  {"provider_name": "nim", "model": "step"}
]
```

#### 2. Update mappings.html page — remove table references

**File**: `proxy/web/templates/mappings.html`

**Intent**: The `{{template "mappings-table" .}}` call stays the same (template name unchanged). Remove the old `{{define "mappings-table"}}` block that was inlined at the bottom of this file (it was colocated with the page template). Ensure the page still references the fragment correctly.

**Contract**: The file keeps `{{template "mappings-table" .}}` in the `content` block. The `editMapping()` JS function signature changes to accept structured data — see Phase 3.

#### 3. CSS — route chain styles

**File**: `proxy/web/static/app.css`

**Intent**: Add CSS classes for `.mappings-grid`, `.route-card`, `.route-chain`, `.route-step`, chevron connectors (via `::after` pseudo-elements), role-based colors, and responsive vertical stacking.

**Contract**: New classes appended to end of `app.css`:

- `.mappings-grid` — vertical flex/grid with gap for card spacing
- `.route-card` — card container using existing `--bg-card`, `--border-subtle`, `--radius-lg`
- `.route-card__header` — flex row with space-between for name and actions
- `.route-card__name` — mapping name as a small heading
- `.route-card__actions` — flex row of action buttons
- `.route-chain` — horizontal flex with gap and `align-items: center`
- `.route-step` — pill element with border-left color accent, provider name + model
- `.route-step--primary` — `border-left-color: var(--color-success)`
- `.route-step--fallback` — `border-left-color: var(--color-warning)`
- `.route-step::after` — chevron arrow (`▶` character or CSS triangle) between steps
- `.route-step:last-child::after` — hidden (no trailing arrow)
- `@media (max-width: 768px)` — `.route-chain` becomes `flex-direction: column`, chevron rotates to point down

### Success Criteria:

#### Automated Verification:

- Go build succeeds: `go build ./...`
- All tests pass: `go test ./proxy/web/...`
- No lint errors: `mage lint` or `golangci-lint run`

#### Manual Verification:

- `/mappings` page shows cards with breadcrumb chains
- Each mapping displays: name, primary provider/model (green accent), fallback steps (amber accent)
- Chevron arrows visible between steps
- Cards have consistent spacing and follow design system
- Mobile: resize browser to <768px — chain stacks vertically
- Empty state (no mappings) shows gracefully

**Implementation Note**: After completing this phase and all automated verification passes, pause here for manual confirmation from the human that the manual testing was successful before proceeding to the next phase.

---

## Phase 3: Edit Dialog Integration

### Overview

Update the `editMapping()` JavaScript function and Delete button to work with the new card structure. The edit function reads structured data from data-attributes on the card's Edit button instead of parsing a formatted fallback string.

### Changes Required:

#### 1. Update editMapping() in mappings.html

**File**: `proxy/web/templates/mappings.html`

**Intent**: Change `editMapping()` to accept a single DOM element reference (the button itself) and read `data-name`, `data-provider`, `data-model`, `data-fallbacks` attributes. Parse `data-fallbacks` as JSON to pre-populate the fallback rows in the dialog.

**Contract**: The function signature changes from `editMapping(name, provider, model, fallbacksStr)` to reading from `this` (the clicked button's dataset). The fallbacks data attribute is a JSON array with the following schema:
```json
[
  {"provider_name": "zen", "model": "claude"},
  {"provider_name": "nim", "model": "step"}
]
```
The `addFallbackRow(provider, model)` helper must accept `provider_name` and `model` as arguments (or be updated to do so). The existing helper stays unchanged — it's called once per parsed fallback entry with error handling:
```javascript
try {
  var fallbacks = JSON.parse(this.dataset.fallbacks);
  fallbacks.forEach(function(fb) {
    addFallbackRow(fb.provider_name, fb.model);
  });
} catch (e) {
  console.error("Failed to parse fallbacks:", e);
}
```

#### 2. Update Delete button HTMX attributes

**File**: `proxy/web/templates/mappings-table.html`

**Intent**: The Delete button in each card uses the same HTMX attributes as before (`hx-delete`, `hx-confirm`, `hx-target="#mappings"`, `hx-swap="outerHTML"`). Just ensure the button is inside the new card structure at the correct position.

**Contract**: Delete button attributes are unchanged — `hx-delete="/v1/mappings/{{.Name}}"` with `hx-target="#mappings" hx-swap="outerHTML"`.

#### 3. Form hx-target update

**File**: `proxy/web/templates/mappings.html`

**Intent**: The mapping dialog form's `hx-target` is currently `#mappings`. Since the new view uses `<div id="mappings">` instead of `<table id="mappings">`, the `hx-target` and `hx-swap` values remain the same — just verify no mismatch.

**Contract**: Form keeps `hx-target="#mappings" hx-swap="outerHTML"`. The `hx-on::after-request` handler closing the dialog stays unchanged.

### Success Criteria:

#### Automated Verification:

- Go build succeeds: `go build ./...`
- All tests pass: `go test ./proxy/web/...`

#### Manual Verification:

- Click "Add Mapping" → dialog opens, fill form, save → new card appears via HTMX swap
- Click "Edit" on a card → dialog opens pre-populated with correct name, provider, model, and fallback rows
- Edit a mapping's fallback chain → save → card updates with new chain
- Click "Delete" → confirm → card disappears, remaining cards re-render
- Create a mapping with multiple fallbacks → verify all appear as chain steps

**Implementation Note**: After completing this phase and all automated verification passes, pause here for manual confirmation from the human that the manual testing was successful before proceeding to the next phase.

---

## Phase 4: Testing & Verification

### Overview

Run the full test suite, verify edge cases, and confirm no regressions. This is a verification-only phase — no new code unless tests reveal issues.

### Changes Required:

#### 1. Run full test suite

**Intent**: Verify all existing tests pass after the UI changes. The handler tests in `proxy/web/handlers_test.go` and `proxy/web/handlers_write_test.go` may check response content — verify they don't assert on table-specific HTML.

**Contract**: `go test ./...` passes. If any handler tests assert on `<table>` or `<th>` markup, update assertions to match new card structure.

#### 2. Cross-browser quick check

**Intent**: Load the page in Firefox and Chrome to verify CSS rendering (flexbox, pseudo-elements, media queries).

**Contract**: Visual parity across browsers for the card layout, chevrons, and mobile view.

### Success Criteria:

#### Automated Verification:

- Full test suite passes: `go test ./...`
- Lint passes: `mage lint`
- Build produces working binary: `mage build`

#### Manual Verification:

- Page loads in Chrome and Firefox
- Mobile layout verified (browser devtools responsive mode)
- Mobile responsiveness verified in Chrome DevTools (iPhone SE, iPhone 12, Pixel 5).
- Edge case: mapping with 0 fallbacks shows single primary step (no chevron trailing)
- Edge case: mapping with 3+ fallbacks wraps gracefully
- Edge case: long model names don't break layout (text truncation with ellipsis)
- No console errors in browser dev tools

**Implementation Note**: After completing this phase, the change is ready for commit.

---

## Testing Strategy

### Unit Tests:

- Existing `proxy/web/handlers_test.go` — verify page loads return 200
- Existing `proxy/web/handlers_write_test.go` — verify CRUD operations return correct HTMX fragments
- If tests assert on HTML content containing `<table>` or `<th>`, update to match new `<div class="route-card">` structure

### Integration Tests:

- No new integration tests needed — existing tests cover the HTMX flow

### Manual Testing Steps:

1. Start server, navigate to `/mappings`
2. Verify cards display with correct chain visualization
3. Create a new mapping with 2 fallbacks — verify card appears
4. Edit mapping — verify dialog pre-populates correctly
5. Delete mapping — verify card disappears
6. Resize to mobile — verify vertical stacking
7. Test with 0 mappings — verify empty state

## Performance Considerations

- Template rendering: no performance change (same number of template executions)
- CSS: flexbox layout is trivially fast for <50 cards
- No new JS computation — removed the string-parsing logic in favor of direct data-attribute reads

## References

- Related research: `context/changes/mapping-graph-visualization/research.md`
- Current mapping template: `proxy/web/templates/mappings.html`
- Current mapping fragment: `proxy/web/templates/mappings-table.html`
- Handler: `proxy/web/handlers.go:193` (`handleMappings`)
- Types: `proxy/web/types.go:48-55` (`mappingRow`)
- CSS: `proxy/web/static/app.css`

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles. See `references/progress-format.md`.

### Phase 1: Data Layer Restructure

#### Automated

- [x] 1.1 Go build succeeds (`go build ./...`) — 2026-07-06 — 5ca86fa
- [x] 1.2 Handler tests pass (`go test ./proxy/web/...`) — 2026-07-06 — 5ca86fa
- [x] 1.5 `FallbacksString()` method compiles and matches old string format. — 2026-07-06 — 5ca86fa

#### Manual

- [ ] 1.3 Page still loads in browser
- [ ] 1.4 Rollback plan documented: If Phase 1 breaks tests or UI, revert `types.go` and `handlers.go` to restore the `Fallbacks string` format.

### Phase 2: Template + CSS

#### Automated

- [x] 2.1 Go build succeeds (`go build ./...`) — 26ebebb
- [x] 2.2 All web tests pass (`go test ./proxy/web/...`) — 26ebebb
- [x] 2.3 Lint passes (`mage lint`) — 26ebebb

#### Manual

- [ ] 2.4 Cards display with breadcrumb chains
- [ ] 2.5 Role-based colors applied (green primary, amber fallback)
- [ ] 2.6 Mobile vertical stacking works (<768px)
- [ ] 2.7 Empty state renders gracefully

### Phase 3: Edit Dialog Integration

#### Automated

- [ ] 3.1 Go build succeeds (`go build ./...`)
- [ ] 3.2 All web tests pass (`go test ./proxy/web/...`)

#### Manual

- [ ] 3.3 Add Mapping creates card via HTMX swap
- [ ] 3.4 Edit pre-populates dialog correctly (name, provider, model, fallbacks)
- [ ] 3.5 Delete removes card via HTMX swap
- [ ] 3.6 Multi-fallback edit round-trip works

### Phase 4: Testing & Verification

#### Automated

- [ ] 4.1 Full test suite passes (`go test ./...`)
- [ ] 4.2 Lint passes (`mage lint`)
- [ ] 4.3 Build produces working binary (`mage build`)

#### Manual

- [ ] 4.4 Cross-browser check (Chrome + Firefox)
- [ ] 4.5 Long model names truncate with ellipsis
- [ ] 4.6 3+ fallbacks wrap gracefully
- [ ] 4.7 No console errors

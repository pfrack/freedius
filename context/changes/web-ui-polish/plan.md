# Web UI Polish — Remainder & Dead-Code Cleanup

## Overview

Complete the 3 features left over from the superseded `web-ui-polish` audit
(skeleton loaders, body-text max-width, back-to-top button) and clean up dead
code discovered by a fresh audit. CSS/template/JS only. No Go changes, no test
changes.

The original 17-finding audit was fully delivered by the archived
`2026-08-01-ui-design-polish` change (PR #40, commit `5770690`). This plan
covers only what remains, plus newly discovered dead code.

## Current State Analysis

Web UI is Go `html/template` + HTMX + vanilla CSS (1988 lines) + vanilla JS
(236 lines). Geist font via CDN. Dark-mode-first with light-mode `@media`
block. Design system is mature: tokens, glassmorphism, spring physics, noise
overlay.

The original plan's "Current State Analysis" referenced pre-#40 code
(2287-line CSS, `{{add1 (add1 -1)}}` bug, etc.) — all of that is now fixed.
This plan's findings are verified against the live code at commit `main`
(post-#40, post-`49cb1ef`).

### Remaining work from the original audit (3 items)

1. **No skeleton loaders** — The old `⟳` spinner was removed by
   ui-design-polish but nothing replaced it. HTMX swaps (mapping drawer open,
   table refreshes) have no pending-state affordance. No `.skeleton` class
   exists in `app.css`; `app.js` has no `htmx:configRequest` wiring.

2. **No body text max-width** — No `p { max-width: ... }` rule exists. Only
   scoped rules: `.empty-state p` (`42ch`, app.css:1103) and `.not-found p`
   (`44ch`, app.css:1157). Long paragraphs stretch wide on large screens.

3. **No back-to-top button** — No scroll-to-top affordance. Logs and mappings
   pages can get long.

### Dead code discovered by fresh audit (not in original plan)

**Dead CSS selectors (4):**
- `.visually-hidden` (app.css:1011) — never used; file's own comment admits
  "has no current consumer" but retains it as "infrastructure". Genuine dead
  code — no template or JS references it.
- `.badge--disabled` (app.css:1322) — never emitted by any template or Go
  code.
- `.health-strip__state--down` (app.css:1380) — Go (`handlers.go:192,219`)
  only ever sets `Health.State` to `"Healthy"` or `"Degraded"`; `down` is
  unreachable.
- `.health-strip__state--unknown` (app.css:1381) — same reasoning.

**Effectively dead CSS rule (1):**
- `body::before` ambient orb (app.css:196-208) — fully clobbered by the grain
  overlay `body::before` at app.css:1223-1232. The first block's `width: 40vw`,
  `height: 40vw`, `border-radius: 50%` leak into the grain overlay, producing
  a likely-unintended 40vw rounded grain patch instead of a full-viewport
  overlay. The radial-gradient `background`, `bottom/left` positioning,
  `opacity: 0.5`, and `z-index: -1` are all overridden.

**Unstyled footer (NEW bug):**
- `layout.html:54-61` renders `<footer class="footer">` with `.footer__content`,
  `.footer__copyright`, `.footer__nav` — **zero CSS rules exist** in app.css.
  Footer inherits body text size/color with no muted treatment. Introduced by
  ui-design-polish but styling was never written.

**Unstyled provider detail classes (4):**
- `.provider-details`, `.provider-details__summary`, `.provider-details__list`
  (providers-table.html:29-31) — no CSS rules.
- `.provider-error` (providers-table.html:65) — no CSS rules.

**Dead element IDs (4):**
- `#mapping-form-error` (mappings.html:9) — app.js only populates `.form-error`
  slots *inside* a `dialog` (app.js:191, 211-213); this top-level slot is
  never written to or cleared.
- `#provider-form-error` (providers.html:9) — same reasoning.
- `#test-dialog-title` (providers.html:63) — never referenced by CSS/JS.
- `#fallback-fieldset` (mappings.html:72) — the `.fallback-fieldset` *class*
  is styled, but the ID is never referenced.

**Dead template blocks (5):**
- `{{define "index"}}`, `{{define "mappings"}}`, `{{define "providers"}}`,
  `{{define "logs"}}`, `{{define "404"}}` — `embed.go:137` executes `"layout"`
  directly; these per-page wrapper defines are never invoked by name.

**Clean bill (verified):**
- 0 dead `@keyframes` (all 6 are referenced).
- 0 dead JS (every function called, every listener binds to existing elements).
- 0 orphaned asset references.
- 6 CSS custom properties intentionally retained (documented at app.css:1973-1978).

## Desired End State

After this plan:
- All dead CSS selectors removed.
- `body::before` orb removed; grain overlay is a clean full-viewport overlay.
- Footer has proper CSS styling (0.75rem, muted, subtle border-top).
- Provider detail classes styled or removed.
- Dead element IDs removed.
- Dead template `{{define}}` wrappers removed.
- Skeleton loaders appear during HTMX requests.
- Back-to-top button appears after scrolling and scrolls smoothly to top.
- Body text has `max-width: 65ch` for readability.
- `mage lint` and `mage test` pass.
- All 5 pages render correctly at 3 viewports.

## What We're NOT Doing

- Adding new Go code, new template logic, new tests
- Replacing the design system (no new framework, no Tailwind)
- Changing the teal accent color or brand identity
- Re-doing any of the 14 items already fixed by ui-design-polish
- Raising `body::after` ambient gradient opacity (current 0.06/0.04 is accepted)
- Removing the 6 intentionally-retained CSS custom properties
- Mobile-first redesign (current responsive breakpoints preserved)
- Playwright visual regression tests

## Implementation Approach

6 phases, ordered by risk (cleanup first, features after, audit last):

1. **Dead-code cleanup** — Remove dead CSS selectors, fix `body::before` orb +
   grain leak, remove dead template wrappers + IDs, style or remove provider-*
   classes, remove `.visually-hidden`
2. **Footer styling** — Add CSS rules for the footer element and its children
3. **Skeleton loaders** — `.skeleton` CSS class + global HTMX event wiring
4. **Back-to-top button** — Fixed-position button with scroll listener
5. **Body text max-width** — `p { max-width: 65ch }` readability rule
6. **Final audit** — Grep checks for dead code, verify all pages render

## Critical Implementation Details

- **`body::before` orb removal**: Delete the first `body::before` block
  (app.css:196-208) entirely. Then strip `width: 40vw`, `height: 40vw`,
  `border-radius: 50%` from the grain overlay `body::before` (app.css:1223-1232)
  so it's a clean `inset: 0` full-viewport overlay. The grain overlay's own
  properties (`opacity: 0.02`, `background-image`, `mix-blend-mode`,
  `z-index: var(--z-grain)`) are correct and should be preserved.
- **Skeleton loaders**: Add `.skeleton` CSS class with shimmer animation
  (gradient sweep via `background-position`). Wire into HTMX globally: on
  `htmx:configRequest`, add `.skeleton` to `event.detail.target`; on
  `htmx:afterSwap`, remove it. Use `transform` and `opacity` for animation
  (GPU-accelerated, no `top`/`left`). Test carefully around SSE handlers —
  the original plan flagged this as a risk.
- **Back-to-top**: Fixed position, `bottom: var(--space-6)`,
  `right: var(--space-6)`. Show/hide via `opacity` + `transform: translateY`.
  Listen to `scroll` with throttling (requestAnimationFrame or 150ms debounce).
  Use `scrollIntoView({ behavior: 'smooth' })` or `window.scrollTo({ top: 0,
  behavior: 'smooth' })`.
- **Footer CSS**: Follow the original plan's contract: `font-size: 0.75rem`,
  `color: var(--text-muted)`, subtle `border-top: 1px solid var(--border-subtle)`,
  centered content with `max-width` matching `main`.
- **Provider detail classes**: Style `.provider-details` as a collapsed
  `<details>` disclosure widget (summary cursor, list indentation).
  `.provider-error` gets error-color text. These are small additions (~15 lines).
- **Template wrapper removal**: The 5 `{{define}}` wrappers are dead but
  harmless. Removing them simplifies the template files. Verify with
  `mage test` after removal — `embed.go:137` executes `"layout"` directly, so
  no runtime impact expected.

## Phase 1: Dead-Code Cleanup

### Overview

Remove all dead CSS selectors, fix the `body::before` orb / grain overlay
conflict, remove dead template wrappers and IDs, style or remove unstyled
provider classes.

### Changes Required:

#### 1. Remove dead CSS selectors

**File**: `proxy/web/static/app.css`

**Intent**: Delete 4 selectors that are defined but never referenced anywhere.

**Contract**:
- `.visually-hidden` (app.css:1011) — delete the entire rule block.
- `.badge--disabled` (app.css:1322) — delete the entire rule block.
- `.health-strip__state--down` (app.css:1380) — delete the entire rule block.
- `.health-strip__state--unknown` (app.css:1381) — delete the entire rule block.

Also update the design-decisions comment at app.css:1980-1981 to remove the
`.visually-hidden` retention note.

#### 2. Remove `body::before` orb + fix grain overlay

**File**: `proxy/web/static/app.css:196-208` and `proxy/web/static/app.css:1223-1232`

**Intent**: Delete the first `body::before` block (ambient orb) entirely.
Strip the leaked `width`, `height`, `border-radius` from the grain overlay
`body::before` so it renders as a clean full-viewport overlay.

**Contract**: Delete lines 196-208. In the grain overlay block (lines
1223-1232), remove `width: 40vw`, `height: 40vw`, `border-radius: 50%` if
present (they leaked from the first block). The grain overlay should have only:
`content`, `position: fixed`, `inset: 0`, `pointer-events: none`,
`z-index: var(--z-grain)`, `opacity: 0.02`, `background-image`,
`mix-blend-mode: overlay`.

#### 3. Remove dead element IDs from templates

**File**: `proxy/web/templates/mappings.html:9`, `proxy/web/templates/providers.html:9`,
`proxy/web/templates/providers.html:63`, `proxy/web/templates/mappings.html:72`

**Intent**: Remove ID attributes that are never referenced by CSS, JS, or
handlers. Keep the elements themselves — only the `id` attribute is dead.

**Contract**:
- `mappings.html:9` — remove `id="mapping-form-error"` from the `<div>`.
- `providers.html:9` — remove `id="provider-form-error"` from the `<div>`.
- `providers.html:63` — remove `id="test-dialog-title"` from the `<h2>`.
- `mappings.html:72` — remove `id="fallback-fieldset"` from the `<fieldset>`.

#### 4. Remove dead template `{{define}}` wrappers

**File**: `proxy/web/templates/index.html:1`, `proxy/web/templates/mappings.html:1`,
`proxy/web/templates/providers.html:1`, `proxy/web/templates/logs.html:1`,
`proxy/web/templates/404.html:1`

**Intent**: Remove the per-page `{{define "name"}}...{{end}}` wrapper blocks
that are never invoked by name. `embed.go:137` executes `"layout"` directly.

**Contract**: Each file currently wraps its entire content in
`{{define "pagename"}}...{{end}}`. Remove the outer `{{define}}` and `{{end}}`
lines, keeping the inner `{{template "layout" .}}` invocation intact.

#### 5. Style provider detail classes

**File**: `proxy/web/static/app.css` (add new rules near providers section)

**Intent**: Add CSS rules for `.provider-details`, `.provider-details__summary`,
`.provider-details__list`, `.provider-error` — these are referenced in
`providers-table.html` but have no styling.

**Contract**:
- `.provider-details` — `<details>` disclosure widget: `cursor: pointer` on
  summary, `margin-top: var(--space-2)`.
- `.provider-details__summary` — `font-size: 0.8rem`, `color: var(--text-secondary)`,
  `cursor: pointer`, `user-select: none`.
- `.provider-details__list` — `margin: var(--space-2) 0 0 var(--space-4)`,
  `font-size: 0.8rem`, `color: var(--text-muted)`.
- `.provider-error` — `font-size: 0.8rem`, `color: var(--badge-error-text)`,
  `margin-top: var(--space-1)`.

### Success Criteria:

#### Automated:

- `mage test` passes
- `mage lint` passes
- `grep -r "visually-hidden\|badge--disabled\|health-strip__state--down\|health-strip__state--unknown" proxy/web/` returns nothing
- `grep -n "body::before" proxy/web/static/app.css` returns exactly 1 match (the grain overlay)
- `grep "width:\|height:\|border-radius" proxy/web/static/app.css` on the grain overlay block returns nothing
- `grep -r 'id="mapping-form-error"\|id="provider-form-error"\|id="test-dialog-title"\|id="fallback-fieldset"' proxy/web/templates/` returns nothing
- `grep -r '{{define "index"}}\|{{define "mappings"}}\|{{define "providers"}}\|{{define "logs"}}\|{{define "404"}}' proxy/web/templates/` returns nothing

#### Manual:

- Grain overlay renders as full-viewport (no rounded patch visible)
- Provider detail sections in providers table render with proper styling
- All pages render identically to before (no visual regressions from dead-code removal)

---

## Phase 2: Footer Styling

### Overview

Add CSS rules for the footer element introduced by ui-design-polish. The
element exists in `layout.html:54-61` but has zero styling.

### Changes Required:

#### 1. Footer CSS rules

**File**: `proxy/web/static/app.css` (add new section near layout rules)

**Intent**: Style the footer with a subtle, muted treatment that doesn't
compete with main content.

**Contract**:
- `.footer` — `border-top: 1px solid var(--border-subtle)`,
  `padding: var(--space-4) var(--space-6)`, `margin-top: var(--space-8)`.
- `.footer__content` — `max-width: 1200px`, `margin: 0 auto`, `display: flex`,
  `justify-content: space-between`, `align-items: center`.
- `.footer__copyright` — `font-size: 0.75rem`, `color: var(--text-muted)`.
- `.footer__nav` — `font-size: 0.75rem`. Links get `color: var(--text-muted)`,
  `text-decoration: none`, `hover: color: var(--text-secondary)`.

### Success Criteria:

#### Automated:

- `mage test` passes
- `mage lint` passes
- `grep -c "\.footer" proxy/web/static/app.css` returns at least 4 (footer, footer__content, footer__copyright, footer__nav)

#### Manual:

- Footer is visually distinct from main content (subtle top border)
- Footer text is small and muted
- Footer link is readable and has hover state
- Footer is centered and aligned with main content max-width

---

## Phase 3: Skeleton Loaders

### Overview

Add skeleton loaders for HTMX requests. Replace the current "no indicator"
state with shimmer-effect skeletons that match the layout shape.

### Changes Required:

#### 1. Skeleton CSS

**File**: `proxy/web/static/app.css` (add new rules)

**Intent**: Add a `.skeleton` class with a shimmer animation that can be
applied to any container during HTMX requests.

**Contract**: `.skeleton` sets `position: relative; overflow: hidden` and
uses a `::after` pseudo-element with a linear-gradient sweep animation.
Animation uses `background-position` (GPU-friendly). Colors:
`var(--bg-surface)` base with `var(--bg-hover)` shimmer highlight.
Add `@keyframes skeleton-shimmer` for the sweep.

#### 2. Skeleton JS wiring

**File**: `proxy/web/static/app.js`

**Intent**: Wire HTMX events to show/hide skeleton loaders globally.

**Contract**: On `htmx:configRequest`, add `.skeleton` class to
`event.detail.target`. On `htmx:afterSwap`, remove `.skeleton` from
`event.detail.target`. Also remove on `htmx:sendError` and
`htmx:responseError` to prevent stuck skeletons.

### Success Criteria:

#### Automated:

- `mage test` passes
- `mage lint` passes
- `grep -c "\.skeleton" proxy/web/static/app.css` returns at least 1
- `grep -c "skeleton" proxy/web/static/app.js` returns at least 2 (add + remove)

#### Manual:

- Skeleton loader appears when opening the mapping drawer
- Skeleton loader appears during table refreshes (mappings, providers)
- Skeleton loader disappears after content loads
- No stuck skeletons on error
- SSE handlers (logs page) are not affected

---

## Phase 4: Back-to-Top Button

### Overview

Add a fixed-position back-to-top button that appears after scrolling down
and scrolls smoothly to top on click.

### Changes Required:

#### 1. Back-to-top CSS

**File**: `proxy/web/static/app.css` (add new rules)

**Intent**: Style a fixed-position button that fades in/out based on scroll
position.

**Contract**: `.back-to-top` with `position: fixed`, `bottom: var(--space-6)`,
`right: var(--space-6)`, `z-index: var(--z-toast)` (or appropriate layer).
Hidden state: `opacity: 0`, `transform: translateY(8px)`, `pointer-events: none`.
Visible state: `opacity: 1`, `transform: translateY(0)`, `pointer-events: auto`.
Transition on `opacity` and `transform` (GPU-accelerated). Style as a subtle
circle with an upward arrow (SVG or `↑` character).

#### 2. Back-to-top JS

**File**: `proxy/web/static/app.js`

**Intent**: Add scroll listener that toggles the button's visible class.

**Contract**: Create the button element dynamically (or add to layout.html).
Listen to `window` `scroll` event with throttling (150ms debounce or
requestAnimationFrame). Show when `window.scrollY > 300`. On click, call
`window.scrollTo({ top: 0, behavior: 'smooth' })`.

### Success Criteria:

#### Automated:

- `mage test` passes
- `mage lint` passes
- `grep -c "back-to-top" proxy/web/static/app.css` returns at least 1
- `grep -c "back-to-top\|scrollTo\|scrollY" proxy/web/static/app.js` returns at least 2

#### Manual:

- Button appears after scrolling down 300px on logs page
- Button appears after scrolling down 300px on mappings page
- Button hides when scrolled to top
- Clicking button scrolls smoothly to top
- Button does not overlap content on narrow viewports (480px)

---

## Phase 5: Body Text Max-Width

### Overview

Add a global `max-width` on paragraph text for readability. Currently only
`.empty-state p` and `.not-found p` have max-width constraints.

### Changes Required:

#### 1. Paragraph max-width rule

**File**: `proxy/web/static/app.css` (add near base/typography rules)

**Intent**: Constrain paragraph width to ~65 characters for readability.

**Contract**: Add `p { max-width: 65ch; }` in the base typography section.
This is a global rule — existing scoped rules (`.empty-state p` at 42ch,
`.not-found p` at 44ch) will naturally override it via specificity.

### Success Criteria:

#### Automated:

- `mage test` passes
- `mage lint` passes
- `grep -c "max-width: 65ch" proxy/web/static/app.css` returns at least 1

#### Manual:

- Long paragraphs on dashboard are readable (don't stretch full width)
- `.empty-state p` still renders at 42ch (not broken by the new global rule)
- `.not-found p` still renders at 44ch

---

## Phase 6: Final Audit

### Overview

Verify all changes are complete, run grep checks for dead code, and do a
final visual pass on all pages.

### Changes Required:

#### 1. Dead code verification

**Intent**: Confirm all dead code identified in this plan has been removed.

**Contract**: Run grep checks:
- `grep -r "visually-hidden\|badge--disabled\|health-strip__state--down\|health-strip__state--unknown" proxy/web/` → empty
- `grep -c "body::before" proxy/web/static/app.css` → exactly 1
- `grep -r 'id="mapping-form-error"\|id="provider-form-error"\|id="test-dialog-title"\|id="fallback-fieldset"' proxy/web/templates/` → empty
- `grep -r '{{define "index"}}\|{{define "mappings"}}\|{{define "providers"}}\|{{define "logs"}}\|{{define "404"}}' proxy/web/templates/` → empty

#### 2. New code verification

**Intent**: Confirm all new features are in place.

**Contract**:
- `grep -c "\.footer" proxy/web/static/app.css` → ≥ 4
- `grep -c "\.skeleton" proxy/web/static/app.css` → ≥ 1
- `grep -c "back-to-top" proxy/web/static/app.css` → ≥ 1
- `grep -c "max-width: 65ch" proxy/web/static/app.css` → ≥ 1

### Success Criteria:

#### Automated:

- `mage test` passes
- `mage lint` passes
- All grep checks pass
- No new inline styles introduced
- No new dead code introduced

#### Manual:

- All 5 pages (dashboard, mappings, providers, logs, 404) render correctly
- All pages render correctly at 3 viewports (1280, 768, 480)
- Light/dark mode toggle works without regressions
- Skeleton loaders work on all HTMX interactions
- Back-to-top button works on long pages
- Footer is styled and readable
- No visual regressions from dead-code removal

---

## Testing Strategy

### Unit Tests:

- Existing test suite (`mage test`) must pass unchanged
- No new tests added (CSS/template/JS-only changes)

### Integration Tests:

- All 5 pages render without errors
- HTMX swaps complete without stuck skeletons
- SSE log streaming unaffected by skeleton wiring

### Manual Testing Steps:

1. Start proxy: `mage run`
2. Visit `http://localhost:8080/` — verify dashboard renders, check footer styling
3. Visit `http://localhost:8080/mappings` — verify table renders, check provider details styling
4. Visit `http://localhost:8080/providers` — verify table renders
5. Visit `http://localhost:8080/logs` — verify log container renders, scroll down for back-to-top
6. Visit `http://localhost:8080/nonexistent` — verify 404 renders
7. Toggle system theme (dark/light) — verify no contrast regressions
8. Resize viewport to 480px — verify responsive layout, back-to-top doesn't overlap
9. Open mapping drawer — verify skeleton loader appears
10. Scroll down on logs page — verify back-to-top button appears and works

## Performance Considerations

- CSS additions are minimal (~60 lines: footer ~15, skeleton ~20, back-to-top ~15, max-width ~1, provider-details ~15)
- CSS removals reduce file size slightly (~30 lines of dead code)
- Skeleton loader animation uses `background-position` (GPU-accelerated)
- Back-to-top scroll listener is throttled
- No new HTTP requests

## Migration Notes

No data migration needed. All changes are CSS/template/JS-only.

## References

- Superseded plan: this file's previous version (commit `49cb1ef` deleted it from origin/main)
- Implemented polish: `context/archive/2026-08-01-ui-design-polish/` (PR #40, commit `5770690`)
- Dashboard redesign: `context/archive/2026-07-31-dashboard-redesign/` (orthogonal, archived)
- Template functions: `proxy/web/embed.go:24-40`
- Layout template: `proxy/web/templates/layout.html`
- Main stylesheet: `proxy/web/static/app.css` (1988 lines)
- Main script: `proxy/web/static/app.js` (236 lines)

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles. See `references/progress-format.md`.

### Phase 1: Dead-Code Cleanup

#### Automated

- [x] 1.1 Dead CSS selectors removed (4 selectors) — 29491ec
- [x] 1.2 `body::before` orb removed, grain overlay fixed — 29491ec
- [x] 1.3 Dead element IDs removed (4 IDs) — 29491ec
- [x] 1.4 Dead template `{{define}}` wrappers removed (5 wrappers) — 29491ec
- [x] 1.5 Provider detail classes styled — 29491ec
- [x] 1.6 `mage test` passes — 29491ec
- [x] 1.7 `mage lint` passes — 29491ec

#### Manual

- [ ] 1.8 Grain overlay renders full-viewport (no rounded patch)
- [ ] 1.9 Provider details render with proper styling
- [ ] 1.10 No visual regressions from dead-code removal

### Phase 2: Footer Styling

#### Automated

- [x] 2.1 Footer CSS rules added (4 selectors) — 4fad3ca
- [x] 2.2 `mage test` passes — 4fad3ca
- [x] 2.3 `mage lint` passes — 4fad3ca

#### Manual

- [ ] 2.4 Footer visually distinct with subtle border-top
- [ ] 2.5 Footer text is small and muted
- [ ] 2.6 Footer link has hover state

### Phase 3: Skeleton Loaders

#### Automated

- [x] 3.1 `.skeleton` CSS class with shimmer animation added — 197c752
- [x] 3.2 HTMX event wiring added to app.js — 197c752
- [x] 3.3 `mage test` passes — 197c752
- [x] 3.4 `mage lint` passes — 197c752

#### Manual

- [ ] 3.5 Skeleton appears during mapping drawer open
- [ ] 3.6 Skeleton appears during table refreshes
- [ ] 3.7 No stuck skeletons on error
- [ ] 3.8 SSE handlers unaffected

### Phase 4: Back-to-Top Button

#### Automated

- [x] 4.1 Back-to-top CSS added — 3296dc8
- [x] 4.2 Back-to-top JS scroll listener added — 3296dc8
- [x] 4.3 `mage test` passes — 3296dc8
- [x] 4.4 `mage lint` passes — 3296dc8

#### Manual

- [ ] 4.5 Button appears after scrolling 300px
- [ ] 4.6 Button scrolls smoothly to top
- [ ] 4.7 Button doesn't overlap content at 480px

### Phase 5: Body Text Max-Width

#### Automated

- [x] 5.1 `p { max-width: 65ch }` rule added
- [x] 5.2 `mage test` passes
- [x] 5.3 `mage lint` passes

#### Manual

- [ ] 5.4 Long paragraphs are readable on wide screens
- [ ] 5.5 `.empty-state p` still renders at 42ch
- [ ] 5.6 `.not-found p` still renders at 44ch

### Phase 6: Final Audit

#### Automated

- [ ] 6.1 All dead-code grep checks pass
- [ ] 6.2 All new-code grep checks pass
- [ ] 6.3 `mage test` passes
- [ ] 6.4 `mage lint` passes

#### Manual

- [ ] 6.5 All 5 pages render correctly at 3 viewports
- [ ] 6.6 Light/dark mode toggle works without regressions
- [ ] 6.7 All features work as expected

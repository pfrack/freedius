# Web UI Polish — Implementation Plan

## Overview

Comprehensive redesign audit pass on the freedius web UI. Applies the
redesign skill's audit checklist to `proxy/web/templates/*.html` +
`proxy/web/static/app.css` + `proxy/web/static/app.js`. CSS-first changes
following the skill's Fix Priority order (color → typography → layout →
interactivity → components → polish). No Go changes, no test changes.

## Current State Analysis

Web UI is Go `html/template` + HTMX + vanilla CSS (2287 lines) + vanilla JS.
Geist font loaded via CDN. Dark-mode-first with `@media (prefers-color-scheme: light)`.
Existing `dashboard-redesign` change (status: implementing) covered operator
monitoring concerns — orthogonal scope, do not modify.

### Audit Findings (12 items, ranked by Fix Priority)

1. **BUG: Fallback count badge** (`index.html:92`) — `{{add1 (add1 -1)}}` always
   evaluates to `1`, so "+1 more" is shown regardless of actual fallback count.
   `sub1` is already registered in `embed.go:32`.

2. **Light-mode status badge contrast** (`app.css:967-980, 1633-1688`) —
   Translucent tints (`rgba(245,158,11,0.06)` for warning) drop below WCAG AA
   against light surfaces. Solid low-saturation backgrounds needed for light mode.

3. **Dead keyframes** (`app.css:1139-1152`) — `fade-in`, `slide-up` defined but
   never referenced anywhere in templates or JS.

4. **`.route-step__label` CSS bug** (`app.css:1289-1325`) — Referenced in
   `mapping-drawer.html` but no CSS rule exists. Silent — elements render unstyled.

5. **Drawer missing inner-highlight** (`app.css:2081-2096`) — `.drawer` uses
   `box-shadow: var(--shadow-lg)` but lacks `var(--inner-highlight)` that cards
   and `.table-wrap` have — visual inconsistency.

6. **Generic hamburger** (`layout.html:16`) — Three horizontal lines. Skill flags
   as generic. Should use a distinctive mark matching the hexagon brand.

7. **Brand mark mismatch** (`layout.html:8` vs `layout.html:23`) — Favicon is
   `⬡` (hexagon) but sidebar header icon is a stacked-layers SVG. Should unify.

8. **ALL-CAPS labels overused** (`app.css:954-980, 1379-1382, 1633-1688`) —
   `.badge`, `.section-count`, `.health-strip__label`, `.drawer__label`, all `<th>`,
   `.stats-strip__label`. Skill says reduce in low-density contexts.

9. **Empty state uses dashed border** (`app.css:1445-1466`) — Generic "get
   started" pattern. Should be a composed treatment.

10. **404 monster number** (`app.css:1494-1505`) — `clamp(6rem, 18vw, 10rem)`
    accent-colored digits. Skill flags as cliché.

11. **No footer / legal links** — No footer element anywhere. Skill requires
    privacy policy and terms of service links.

12. **No skeleton loaders** (`app.css:1550-1558`, `app.js`) — HTMX requests show
    only a spinning `⟳` character. Skill recommends skeleton loaders matching
    layout shape.

### Additional findings from fresh audit:

- **Missing body text max-width** — Long paragraphs and content stretch wide
  without a readability constraint (~65 chars). `empty-state p` has `max-width: 42ch`
  but general body text doesn't.
- **No back-to-top button** — Long pages (logs, mappings) have no scroll-to-top.
- **Ambient background gradient too subtle** (`app.css:176-187`) — Radial gradient
  at `rgba(13,148,136,0.06)` opacity is nearly invisible.
- **Inline onclick handlers** in templates (`layout.html:16-18`) — Skill says move
  all styling to stylesheet. Inline event handlers are acceptable for HTMX-style
  apps but the hamburger toggle could be cleaner.
- **Missing social meta tags** (`layout.html:5-7`) — No `og:*`, `twitter:card`.

## Desired End State

After this plan:
- All 12 audit findings addressed or explicitly documented as out-of-scope.
- All status badges meet WCAG AA contrast in both dark and light modes.
- The `.route-step__label` CSS bug is fixed.
- Dead keyframes removed.
- Hamburger uses a custom hexagonal mark matching the brand.
- Brand mark unified between favicon and sidebar header (both hexagons).
- Footer with legal links added.
- Skeleton loaders for HTMX requests.
- Body text readability improved with max-width.
- Back-to-top button for long pages.
- Ambient background gradient slightly enhanced.
- Social meta tags added.
- `mage lint` and `mage test` pass.

## What We're NOT Doing

- Adding new Go code, new template logic, new tests
- Replacing the design system (no new framework, no Tailwind)
- Changing the teal accent color or brand identity
- ALL-CAPS removal on table headers / status badges (data-density justified)
- Mobile-first redesign (current responsive breakpoints preserved)
- Playwright visual regression tests

## Implementation Approach

6 phases following redesign skill Fix Priority order:

1. **Bug fixes** — Fallback count badge, `.route-step__label` CSS bug
2. **Color & surface** — Light-mode badge contrast, ambient gradient, drawer inner-highlight
3. **Typography & layout** — Label case reduction, empty state, dead code removal, body text max-width
4. **Interactivity** — Skeleton loaders, back-to-top button
5. **Components** — Hamburger, brand mark, 404, footer
6. **Meta & polish** — Social meta tags, final pass

## Critical Implementation Details

- **Status badge contrast in light mode**: Translucent tints drop below WCAG AA.
  Use solid low-saturation backgrounds for light mode only. Do NOT change dark
  mode tokens (they're already correct).
- **Fallback count fix**: `sub1` is already registered in `embed.go:32`.
  `{{sub1 .FallbackCount}}` gives the count of fallbacks beyond the first.
- **Skeleton loaders**: Add `.skeleton` CSS class with shimmer animation.
  Wire into HTMX `htmx:configRequest` / `htmx:afterSwap` events to show/hide
  on the target container.
- **Back-to-top button**: Fixed position, appears on scroll-down, hides on
  scroll-up. Use `transform` + `opacity` for smooth transition (no `top`/`left`).
- **Ambient gradient**: Raise opacity from `0.06` to `0.08` in dark mode,
  `0.04` to `0.06` in light mode. Add a second subtle orb in bottom-left.
- **Hamburger**: Replace 3-line SVG with hexagon `⬡` matching favicon.
- **Drawer inner-highlight**: Add `var(--inner-highlight)` to `.drawer` box-shadow.

## Phase 1: Bug Fixes

### Overview

Fix the two silent bugs: fallback count badge always showing "+1 more" and
`.route-step__label` having no CSS rule.

### Changes Required:

#### 1. Fallback count badge

**File**: `proxy/web/templates/index.html:92`

**Intent**: Replace `{{add1 (add1 -1)}}` (always evaluates to 1) with `{{sub1 .FallbackCount}}`
to show the correct number of additional fallbacks beyond the first.

**Contract**: `sub1` is already registered in `embed.go:32` as `func(i int) int { return i - 1 }`.
Template uses Go `html/template` FuncMap.

#### 2. `.route-step__label` CSS rule

**File**: `proxy/web/static/app.css` (near `.route-step__provider` at line 1272)

**Intent**: Add a CSS rule for `.route-step__label` that styles the label text
in route steps. Should match the visual hierarchy of `.route-step__provider`
but slightly more prominent.

**Contract**: The class is referenced in `mapping-drawer.html` for route step
labels. Style with `font-size: 0.8rem`, `color: var(--text-secondary)`,
`font-weight: 500`.

### Success Criteria:

#### Automated:

- `mage test` passes
- `mage lint` passes

#### Manual:

- Fallback count badge shows correct number when >1 fallback exists
- `.route-step__label` text is visible and styled in mapping drawer

---

## Phase 2: Color & Surface

### Overview

Fix light-mode status badge contrast, enhance ambient background gradient,
add inner-highlight to drawer for visual consistency.

### Changes Required:

#### 1. Light-mode status badge contrast

**File**: `proxy/web/static/app.css` (lines 967-980, 1633-1688)

**Intent**: Status badges use translucent tints that are nearly invisible in
light mode. Replace with solid low-saturation backgrounds for light mode only.

**Contract**: Add light-mode overrides inside the existing `@media (prefers-color-scheme: light)`
block. Use solid colors like `rgba(34,197,94,0.12)` for success,
`rgba(245,158,11,0.12)` for warning, `rgba(239,68,68,0.12)` for error.

#### 2. Ambient background gradient

**File**: `proxy/web/static/app.css:176-187`

**Intent**: Raise opacity from `0.06` to `0.08` in dark mode and add a second
subtle orb in bottom-left corner for more depth.

**Contract**: Modify `body::after` and add `body::before` (or a second gradient
stop). Keep `pointer-events: none` and `z-index: -1`.

#### 3. Drawer inner-highlight

**File**: `proxy/web/static/app.css:2081-2096`

**Intent**: Add `var(--inner-highlight)` to `.drawer` box-shadow to match
cards and `.table-wrap` visual treatment.

**Contract**: Change `box-shadow: var(--shadow-lg)` to
`box-shadow: var(--shadow-lg), var(--inner-highlight)`.

### Success Criteria:

#### Automated:

- `mage test` passes
- `mage lint` passes

#### Manual:

- Status badges are clearly visible in light mode
- Ambient gradient is perceptible but not distracting
- Drawer has consistent inner-highlight with cards

---

## Phase 3: Typography & Layout

### Overview

Reduce ALL-CAPS labels in low-density contexts, improve empty state,
remove dead keyframes, add body text max-width.

### Changes Required:

#### 1. Label case reduction

**File**: `proxy/web/static/app.css`

**Intent**: Convert ALL-CAPS labels to sentence case in low-density contexts:
`.section-count`, `.health-strip__label`, `.drawer__label`, `.stats-strip__label`.
Keep ALL-CAPS on table headers and status badges (data-density justified).

**Contract**: Remove `text-transform: uppercase` from the listed selectors.
Adjust `letter-spacing` to `0` (uppercase tracking no longer needed).

#### 2. Empty state composition

**File**: `proxy/web/static/app.css:1445-1466`

**Intent**: Replace dashed border with a composed treatment — solid border
with subtle background, more padding, and a decorative element.

**Contract**: Change `border: 1px dashed var(--border-default)` to
`border: 1px solid var(--border-subtle)`. Add `background: var(--bg-surface)`.

#### 3. Dead keyframes removal

**File**: `proxy/web/static/app.css:1139-1152`

**Intent**: Remove `@keyframes fade-in` and `@keyframes slide-up` — defined
but never referenced.

**Contract**: Delete the two `@keyframes` blocks. Verify no references remain
with grep.

#### 4. Body text max-width

**File**: `proxy/web/static/app.css` (add new rule)

**Intent**: Add max-width to paragraph text for readability (~65 chars).
The `main` element already has `max-width: 1200px` but paragraphs can still
stretch too wide on large screens.

**Contract**: Add `p { max-width: 65ch; }` or apply to specific content
containers. Keep existing `empty-state p` rule (already has `max-width: 42ch`).

### Success Criteria:

#### Automated:

- `mage test` passes
- `mage lint` passes

#### Manual:

- Labels are sentence case in low-density contexts
- Empty state has composed treatment
- No dead keyframes remain
- Paragraph text is readable on wide screens

---

## Phase 4: Interactivity

### Overview

Add skeleton loaders for HTMX requests and a back-to-top button for long pages.

### Changes Required:

#### 1. Skeleton loaders

**File**: `proxy/web/static/app.css` (add new rules), `proxy/web/static/app.js`

**Intent**: Replace the simple `⟳` spinner with skeleton loaders that match
the layout shape. Show skeletons while HTMX requests are in flight.

**Contract**: Add `.skeleton` CSS class with shimmer animation (gradient
sweep). Wire into HTMX events: on `htmx:configRequest`, add `.skeleton` to
the target container; on `htmx:afterSwap`, remove it. Use `transform` and
`opacity` for animation (no `top`/`left`).

#### 2. Back-to-top button

**File**: `proxy/web/static/app.css` (add new rules), `proxy/web/static/app.js`

**Intent**: Add a fixed-position button that appears after scrolling down 300px
and scrolls smoothly to top on click.

**Contract**: Button with `position: fixed`, `bottom` / `right` positioning.
Show/hide via `opacity` + `transform` (translateY). Listen to `scroll` event
with throttling. Use `scrollIntoView({ behavior: 'smooth' })` for scroll.

### Success Criteria:

#### Automated:

- `mage test` passes
- `mage lint` passes

#### Manual:

- Skeleton loaders appear during HTMX requests
- Back-to-top button appears after scrolling and scrolls to top

---

## Phase 5: Components

### Overview

Replace generic hamburger with hexagonal mark, unify brand mark, improve
404 treatment, add footer with legal links.

### Changes Required:

#### 1. Hamburger mark

**File**: `proxy/web/templates/layout.html:16-18`

**Intent**: Replace the 3-line hamburger SVG with a hexagon `⬡` matching the
favicon brand. Use the same inline style approach as existing buttons.

**Contract**: Replace the `<svg>` content in the hamburger button with the
hexagon character. Keep the `onclick` handler and `aria-label`.

#### 2. Brand mark unification

**File**: `proxy/web/templates/layout.html:23`

**Intent**: Replace the stacked-layers SVG in the sidebar header with the
hexagon `⬡` character to match the favicon.

**Contract**: Replace the `<svg>` with `⬡` text. Keep the `filter: drop-shadow`
effect.

#### 3. 404 treatment

**File**: `proxy/web/static/app.css:1494-1505`

**Intent**: Reduce the monster-number size and use a more composed treatment.
Skill flags `clamp(6rem, 18vw, 10rem)` as cliché.

**Contract**: Reduce to `clamp(3rem, 10vw, 5rem)`. Use `text-wrap: balance`.
Add a subtle decorative element (e.g., a hexagon outline behind the number).

#### 4. Footer

**File**: `proxy/web/templates/layout.html` (after `<main>`)

**Intent**: Add a footer with legal links (License link to GitHub).

**Contract**: Simple footer with `freedius` brand text and a License link.
Style with `font-size: 0.75rem`, `color: var(--text-muted)`.

### Success Criteria:

#### Automated:

- `mage test` passes
- `mage lint` passes

#### Manual:

- Hamburger shows hexagon mark
- Sidebar header shows hexagon mark matching favicon
- 404 number is smaller and composed
- Footer with legal links is visible on all pages

---

## Phase 6: Meta & Polish

### Overview

Add social meta tags, final audit pass, verify all pages render correctly.

### Changes Required:

#### 1. Social meta tags

**File**: `proxy/web/templates/layout.html:5-7`

**Intent**: Add `og:title`, `og:description`, `og:image`, `twitter:card`
meta tags for social sharing.

**Contract**: Add meta tags in `<head>`. Use the existing description from
the `meta name="description"` tag. `og:image` as inline SVG data URI with
proper percent-encoding.

#### 2. Final audit pass

**Intent**: Verify all 12 audit findings are addressed. Run grep checks
for dead code, inline styles, and missing accessibility attributes.

**Contract**: 
- `grep -r "fade-in\|slide-up" proxy/web/` returns nothing
- `grep -r "style=" proxy/web/templates/` returns only acceptable inline styles
- All pages have `<title>`, `meta description`, `og:*` tags

### Success Criteria:

#### Automated:

- `mage test` passes
- `mage lint` passes
- No dead keyframes remain
- No new inline styles introduced

#### Manual:

- All 5 pages (dashboard, mappings, providers, logs, 404) render correctly
- Social meta tags are present in page source
- All audit findings addressed

---

## Testing Strategy

### Unit Tests:

- Existing test suite (`mage test`) must pass unchanged
- No new tests added (CSS-only changes)

### Integration Tests:

- Dashboard renders routing table with correct fallback count
- Provider badges render with proper contrast
- 404 page renders with composed treatment

### Manual Testing Steps:

1. Start proxy: `mage run`
2. Visit `http://localhost:8080/` — verify dashboard renders
3. Visit `http://localhost:8080/mappings` — verify table renders
4. Visit `http://localhost:8080/providers` — verify table renders
5. Visit `http://localhost:8080/logs` — verify log container renders
6. Visit `http://localhost:8080/nonexistent` — verify 404 renders
7. Toggle system theme (dark/light) — verify contrast in both modes
8. Resize viewport to 480px — verify responsive layout
9. Scroll down on logs page — verify back-to-top button appears
10. Trigger HTMX request — verify skeleton loader appears

## Performance Considerations

- CSS additions are minimal (<100 lines)
- No new HTTP requests (all changes in existing bundled files)
- Skeleton loader animation uses `transform` + `opacity` (GPU-accelerated)
- Back-to-top scroll listener is throttled

## Migration Notes

No data migration needed. All changes are CSS/template-only.

## References

- Redesign skill audit: `redesign-existing-projects/SKILL.md`
- Existing plan: `context/changes/ui-design-polish/plan.md`
- Template functions: `proxy/web/embed.go:23-34`
- Dashboard template: `proxy/web/templates/index.html`
- Layout template: `proxy/web/templates/layout.html`
- Main stylesheet: `proxy/web/static/app.css`
- Main script: `proxy/web/static/app.js`

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles. See `references/progress-format.md`.

### Phase 1: Bug Fixes

#### Automated

- [ ] 1.1 Fallback count badge uses `sub1` instead of `add1(add1(-1))`
- [ ] 1.2 `.route-step__label` CSS rule added
- [ ] 1.3 `mage test` passes
- [ ] 1.4 `mage lint` passes

#### Manual

- [ ] 1.5 Fallback count badge shows correct number
- [ ] 1.6 `.route-step__label` text is styled in drawer

### Phase 2: Color & Surface

#### Automated

- [ ] 2.1 Light-mode status badge contrast fixed
- [ ] 2.2 Ambient gradient enhanced
- [ ] 2.3 Drawer inner-highlight added
- [ ] 2.4 `mage test` passes
- [ ] 2.5 `mage lint` passes

#### Manual

- [ ] 2.6 Status badges visible in light mode
- [ ] 2.7 Ambient gradient perceptible
- [ ] 2.8 Drawer has consistent inner-highlight

### Phase 3: Typography & Layout

#### Automated

- [ ] 3.1 Label case reduced in low-density contexts
- [ ] 3.2 Empty state composed treatment
- [ ] 3.3 Dead keyframes removed
- [ ] 3.4 Body text max-width added
- [ ] 3.5 `mage test` passes
- [ ] 3.6 `mage lint` passes

#### Manual

- [ ] 3.7 Labels are sentence case
- [ ] 3.8 Empty state looks composed
- [ ] 3.9 Paragraph text readable on wide screens

### Phase 4: Interactivity

#### Automated

- [ ] 4.1 Skeleton loader CSS added
- [ ] 4.2 Skeleton loader JS wiring added
- [ ] 4.3 Back-to-top button CSS added
- [ ] 4.4 Back-to-top button JS wiring added
- [ ] 4.5 `mage test` passes
- [ ] 4.6 `mage lint` passes

#### Manual

- [ ] 4.7 Skeleton loaders appear during HTMX requests
- [ ] 4.8 Back-to-top button appears and scrolls to top

### Phase 5: Components

#### Automated

- [ ] 5.1 Hamburger uses hexagon mark
- [ ] 5.2 Sidebar header uses hexagon mark
- [ ] 5.3 404 treatment composed
- [ ] 5.4 Footer with legal links added
- [ ] 5.5 `mage test` passes
- [ ] 5.6 `mage lint` passes

#### Manual

- [ ] 5.7 Hamburger shows hexagon
- [ ] 5.8 Sidebar header shows hexagon matching favicon
- [ ] 5.9 404 looks composed
- [ ] 5.10 Footer visible on all pages

### Phase 6: Meta & Polish

#### Automated

- [ ] 6.1 Social meta tags added
- [ ] 6.2 No dead keyframes remain
- [ ] 6.3 No new inline styles
- [ ] 6.4 `mage test` passes
- [ ] 6.5 `mage lint` passes

#### Manual

- [ ] 6.6 All 5 pages render correctly
- [ ] 6.7 Social meta tags present in source
- [ ] 6.8 All audit findings addressed

# UI Design Polish — Implementation Plan

## Overview

Polish the freedius web UI to remove generic design fingerprints and elevate visual quality. Scoped as a CSS-first pass following the redesign skill's Fix Priority order (color → typography → layout → interactivity → components → polish). No Go, template logic, or test changes.

## Current State Analysis

Web UI is `proxy/web/templates/*.html` (Go `html/template`) + `proxy/web/static/app.css` (vanilla CSS, 2267 lines) + `proxy/web/static/app.js` (vanilla JS) + HTMX. Geist font loaded via CDN. Dark-mode-first with light-mode `@media` block.

### Key Discoveries:

- `app.css:9` — `--bg-root: #050505` (off-black, good).
- `app.css:26` — Single teal accent `#0d9488` (good).
- `app.css:1138-1152` — Dead keyframes: `fade-in`, `slide-up` defined, never referenced.
- `app.css:967-980` — Status badges (`.badge--healthy`, etc.) use translucent tints that are nearly invisible in light mode for warning/error (low contrast).
- `app.css:402-454` — `.stats-grid` uses uniform `repeat(auto-fit, minmax(220px, 1fr))` — generic 3-4 column card grid.
- `app.css:1445-1466` — `.empty-state` uses dashed border (generic "get started" pattern).
- `app.css:1494-1505` — `.not-found__code` renders 404 as a 6–10rem accent-colored monster number (cliché).
- `app.css:1289-1325` — `.route-step` defined; `.route-step__label` referenced in `mapping-drawer.html:18,24` but no CSS rule exists — silent bug.
- `app.css:2061-2080` — `.drawer` `box-shadow: var(--shadow-lg)` but lacks `var(--inner-highlight)` that cards have — visual inconsistency.
- `layout.html:23` — Sidebar header icon is a stacked-layers SVG; `layout.html:8` — Favicon is `⬡` (hexagon). Brand mismatch.
- `models-fragment.html:2` — Inline style: `style="margin-bottom:var(--space-2);width:100%;"` (skill: move to stylesheet).
- `layout.html:5-7` — No `og:image`, no `og:title` (skill: missing social meta).
- `app.css:339-363` — Typography: weights 800, 650, 600 used. Missing `font-weight: 500` (medium) for subtle hierarchy.
- `app.css:954-980, 1379-1382, 1633-1688` — Multiple ALL-CAPS label classes (`.badge`, `.section-count`, `.health-strip__label`, `.drawer__label`, all `<th>`, `.stats-strip__label`) — overused pattern.
- `layout.html:27-42` — Sidebar nav icons are 4 generic outline icons (Lucide/Feather style) — skill flags this as the "default AI choice".
- `layout.html:17` — Hamburger = 3 horizontal lines (universal but generic).
- `app.css:500-529` — `.stats-strip` and `.health-strip` overlap conceptually but are styled differently — inconsistency.

## Desired End State

After this plan:
- All in-scope audit items from the redesign skill closed; out-of-scope items (table headers, status badge pills per F2 of plan-review) explicitly documented.
- All status badges meet WCAG AA contrast in both dark and light modes.
- The `.route-step__label` is styled (no longer broken).
- Dead keyframes removed.
- Sidebar icon set is custom/distinctive (not Lucide/Feather default).
- 404 page no longer uses monster-number cliché.
- Hamburger uses a custom distinctive mark.
- Brand mark unified between favicon and sidebar header.
- All template inline styles moved to `app.css`.
- Social meta tags present.
- Card hover uses border/shadow shift (no generic translateY).
- Remaining ALL-CAPS labels converted to sentence case in low-density contexts.
- Visual smoke verification confirms pages render at 1280/768/480px.

### Verification:

- `mage lint` passes
- `mage test` passes (no test changes expected)
- Visual smoke: each page loads correctly at 1280×800, 768×1024, 480×800

## What We're NOT Doing

- Adding new HTML pages or changing template logic
- Replacing the design system (no new framework, no Tailwind)
- Changing the teal accent color or brand identity
- Removing ALL-CAPS labels globally (some remain where data-density requires it: table headers, status badges)
- Replacing icon library wholesale (custom SVG paths inline)
- Adding Playwright visual regression tests (out of scope per verification choice)
- Mobile-first redesign (current responsive breakpoints preserved)

## Implementation Approach

7 phases following redesign skill Fix Priority order. Each phase is CSS-only unless noted:

1. **Color & surface refinement** — light-mode contrast, status badge contrast, light-mode surface hierarchy.
2. **Typography scale & weights** — add `font-weight: 500`, fix `.route-step__label` bug, selective label case reduction.
3. **Layout & spacing** — varied card radii, asymmetric stats grid, dead-code removal.
4. **Interactivity upgrades** — skeleton states, drawer inner-highlight, max-line cap on logs (client-side via JS).
5. **Component swaps** — sidebar icons, hamburger mark, 404 treatment, badge style refinement.
6. **Brand unification & meta** — favicon↔sidebar icon match, og: meta tags, inline-style removal.
7. **Final polish** — pass over remaining audit items, visual smoke verification.

## Critical Implementation Details

- **Status badge contrast in light mode**: translucent tints (`rgba(245,158,11,0.06)` for warning) drop below WCAG AA against light surfaces. Use solid low-saturation backgrounds instead of translucent overlays, OR raise opacity to ≥0.15 for light mode only. Do NOT change dark mode tokens (they're already correct).
- **Custom icons**: inline SVG paths inline in templates (matches existing pattern). Do not introduce an icon library or external sprite sheet.

## Phase 1: Color & Surface Refinement

### Overview

Fix status-badge contrast issues in light mode. Tighten surface hierarchy in light mode. Add subtle hue tinting so light mode doesn't feel sterile.

### Changes Required:

#### 1. Status badge light-mode contrast

**File**: `proxy/web/static/app.css`

**Intent**: Raise light-mode warning/error badge background opacity from `rgba(245,158,11,0.06)` / `rgba(239,68,68,0.06)` to `0.14` / `0.12`. Solid low-saturation backgrounds ensure WCAG AA contrast on `#ffffff` cards. Keep dark mode tokens unchanged.

**Contract**: Only modify values inside `@media (prefers-color-scheme: light) { :root { ... } }` block for `--log-warn-bg`, `--log-error-bg`, `.badge--healthy`, `.badge--degraded`, `.badge--error` backgrounds. Do not touch `--bg-*`, `--text-*`, or shadow tokens.

#### 2. Light-mode surface hierarchy

**File**: `proxy/web/static/app.css`

**Intent**: In light mode, currently `--bg-card: #fff` and `--bg-card-inner: #fff` are identical — no inner/outer differentiation. Introduce subtle warm tint for inner surfaces (e.g., `#fafaf9` for `--bg-card-inner`) so nested cards read as nested.

**Contract**: Modify `--bg-card-inner` inside the light-mode `@media` block only. Verify visual diff in browser before finalizing.

### Success Criteria:

#### Automated Verification:

- `mage lint` passes
- `mage test` passes (no behavior change expected)

#### Manual Verification:

- In light mode, "warn" and "error" status badges are clearly legible against card backgrounds.
- Nested cards (`.card--stat` inside `.card`) have visible separation in light mode.

---

## Phase 2: Typography Scale & Weights

### Overview

Add medium weight (500) to the typography scale. Fix the broken `.route-step__label` CSS rule. Selectively reduce ALL-CAPS usage in low-information-density contexts (drawer labels, stats-strip labels) while preserving it where data density requires it (table headers, status badges).

### Changes Required:

#### 1. Add font-weight 500 to scale

**File**: `proxy/web/static/app.css`

**Intent**: Establish a comment-documented weight scale (300/400/500/600/650/750/800) used across the file. Add `font-weight: 500` usage in places currently jumping from 400 to 550/600.

**Contract**: Update comment at top of typography section. Apply `font-weight: 500` to `.form-label` and `.drawer__label` where the jump from 400 to 550 is too steep.

#### 2. Fix `.route-step__label` CSS

**File**: `proxy/web/static/app.css`

**Intent**: `.route-step__label` is referenced in `mapping-drawer.html:18,24` (Primary / Fallback N labels) but no CSS rule exists. Add a rule matching existing `.route-step__provider` pattern: muted, small, uppercase with tracking.

**Contract**: Add `.route-step__label { font-size: 0.7rem; color: var(--text-muted); text-transform: uppercase; letter-spacing: 0.04em; font-weight: 500; }` to the `.route-step` block.

#### 3. Selective label case reduction

**File**: `proxy/web/static/app.css`

**Intent**: Replace ALL-CAPS + tracking on `.drawer__label`, `.drawer__stats dt`, `.health-strip__label`, `.health-strip__key`, `.stats-strip__label`, and `.section-count` with sentence case + small-caps. Keep `.badge` and table `<th>` as ALL-CAPS (data density justifies it).

**Contract**: Modify `.drawer__label`, `.drawer__stats dt`, `.health-strip__label`, `.health-strip__key`, `.stats-strip__label`, and `.section-count` rules. Use `text-transform: none` + slightly increased `font-size` (0.75rem instead of 0.7rem) + `font-weight: 500`. Leave badges and table headers untouched.

### Success Criteria:

#### Automated Verification:

- `mage lint` passes
- `mage test` passes

#### Manual Verification:

- Mapping drawer shows "Primary" / "Fallback 1" labels as sentence case, not "PRIMARY" / "FALLBACK 1".
- Drawer stats list ("REQUESTS", "ERRORS") reads as "Requests", "Errors".
- Visual hierarchy between body, form labels, and drawer labels is more graduated (less binary 400→550 jump).

---

## Phase 3: Layout & Spacing

### Overview

Introduce varied border-radii (currently uniform `--radius-2xl` everywhere). Replace dashed `.empty-state` border with a more composed "get started" treatment. Remove dead `@keyframes`.

### Changes Required:

#### 1. Varied card radii

**File**: `proxy/web/static/app.css`

**Intent**: Currently all cards use `--radius-2xl` (1.5rem). Skill recommends varying radii — tighter on inner elements, softer on containers. Introduce `--radius-card` (1.25rem, slightly tighter) for cards, keep `--radius-2xl` only for the largest surface (`.not-found`, `.log-container`). Use existing `--radius-xl` (1rem) for nested cards like `.card--stat`.

**Contract**: Reuse the existing `--radius-xl` token (already 1.25rem) for `.card`, `.table-wrap`, `.log-container`. Apply `--radius-md` (or `--radius-sm`) to `.card--stat` (already nested, smaller value). Apply `--radius-2xl` to `.not-found` and `.empty-state` (page-level placeholders). Update `table { border-radius: calc(var(--radius-2xl) - 0.25rem) }` at `app.css:652` to `calc(var(--radius-xl) - 0.25rem)` so the inner `table` tracks the outer. Verify no regressions in dialog (uses `--radius-shell` which is unchanged). Do not introduce a new `--radius-card` token.

#### 2. Card hover de-translate

**File**: `proxy/web/static/app.css`

**Intent**: Cards and route-cards currently lift on hover via `transform: translateY(-1px)`. This is the generic "AI card hover" pattern (skill: "subtle scale, or translate on hover" is fine, but `translateY` everywhere is overused). Replace with a more distinctive motion: border-color + box-shadow shift only, no translation. Keep icon `scale(1.05)` inside `.card:hover .card-icon` — that one is fine.

**Contract**: Remove `transform: translateY(-1px)` from `.card:hover`, `.route-card:hover`, `.providers-overview__item:hover`, and `.providers-summary__chip:hover` rules. Keep their other hover state changes (border-color, box-shadow). Do not touch `.btn:hover` (buttons translateY is intentional magnetic-physics motion per current design).

#### 2. Empty-state composition

**File**: `proxy/web/static/app.css`

**Intent**: Replace dashed border + centered text pattern with a more composed treatment: no border, generous whitespace, a single subtle radial-gradient backdrop anchored top-right.

**Contract**: Modify `.empty-state` rule to remove `border: 1px dashed`. Add a subtle gradient background using existing `--accent-glow` variable. Do not change `.empty-state h2` / `p` styling.

#### 3. Card hover de-translate

**File**: `proxy/web/static/app.css`

**Intent**: Cards and route-cards currently lift on hover via `transform: translateY(-1px)`. This is the generic "AI card hover" pattern (skill: "subtle scale, or translate on hover" is fine, but `translateY` everywhere is overused). Replace with a more distinctive motion: border-color + box-shadow shift only, no translation. Keep icon `scale(1.05)` inside `.card:hover .card-icon` — that one is fine.

**Contract**: Remove `transform: translateY(-1px)` from `.card:hover`, `.route-card:hover`, `.providers-overview__item:hover`, and `.providers-summary__chip:hover` rules. Keep their other hover state changes (border-color, box-shadow). Do not touch `.btn:hover` (buttons translateY is intentional magnetic-physics motion per current design).

#### 4. Remove dead keyframes

**File**: `proxy/web/static/app.css`

**Intent**: `@keyframes fade-in` and `@keyframes slide-up` are defined but never referenced. Remove them.

**Contract**: Delete `@keyframes fade-in { from { opacity: 0 } to { opacity: 1 } }` and `@keyframes slide-up { ... }` blocks.

### Success Criteria:

#### Automated Verification:

- `mage lint` passes
- `mage test` passes

#### Manual Verification:

- Cards have visible radius hierarchy: outermost container softer than inner stat tiles.
- Empty state on Mappings/Providers pages has subtle ambient gradient, no dashed border.
- Page still loads cleanly with all animations working.

---

## Phase 4: Interactivity Upgrades

### Overview

Replace the plain `⟳` text-character htmx-indicator with a proper spinner SVG. Add `inner-highlight` to drawer for visual consistency with cards. Add a client-side max-line cap on the log container to prevent unbounded DOM growth.

### Changes Required:

#### 1. Refined htmx-indicator spinner

**File**: `proxy/web/static/app.css`

**Intent**: Currently `.htmx-indicator` uses the text character `⟳` styled by `.htmx-request` animation. Replace with an inline SVG circle that uses `stroke-dasharray` + animated `stroke-dashoffset` for a premium look.

**Contract**: Modify `.htmx-indicator` rule to set `width: 14px; height: 14px; border: 2px solid currentColor; border-top-color: transparent; border-radius: 50%;`. **Preserve the existing `display: none` from the current rule** — the base rule hides the spinner until `.htmx-request` flips it to `inline-block`. Keep existing `@keyframes spin` and `.htmx-request .htmx-indicator` rules. The element rendered in templates (`<span class="htmx-indicator" aria-hidden="true">⟳</span>`) needs to be replaced with a proper SVG or stripped of text content.

#### 2. Replace `⟳` text in templates

**File**: `proxy/web/templates/mappings.html`, `proxy/web/templates/providers.html`

**Intent**: Remove the `⟳` text content from `.htmx-indicator` spans since the new spinner uses CSS borders, not text.

**Contract**: In `mappings.html` (2 occurrences: lines 29, 82) and `providers.html` (line 52), change `<span class="htmx-indicator" aria-hidden="true">⟳</span>` to `<span class="htmx-indicator" aria-hidden="true"></span>`.

#### 3. Drawer inner-highlight

**File**: `proxy/web/static/app.css`

**Intent**: `.drawer` currently has only `box-shadow: var(--shadow-lg)`. Cards use `box-shadow: var(--shadow-sm), var(--inner-highlight)` for consistent edge refraction. Apply the same pattern.

**Contract**: Modify `.drawer` rule: `box-shadow: var(--shadow-lg), var(--inner-highlight);`. Verify drawer still slides correctly on open/close.

#### 4. Client-side log line cap

**File**: `proxy/web/templates/logs.html`

**Intent**: The SSE log appender (`document.addEventListener('htmx:sseMessage', ...)` inside the `{{define "scripts"}}` block, lines 69–89) currently appends indefinitely. Add a 500-line cap, removing the oldest lines when exceeded. Mirrors server-side ring buffer (10k) but caps DOM growth for browser perf.

**Contract**: Add the cap in **both** appendChild branches (try block ~line 77, catch block ~line 86). Append → scroll → trim, in that order: after each `logEl.appendChild(pre)`, run `logEl.scrollTop = logEl.scrollHeight`, then while `logEl.children.length > 500`, remove `logEl.firstElementChild`. Trim order: append → scroll → trim (preserves pinned-bottom UX during bursts).

### Success Criteria:

#### Automated Verification:

- `mage lint` passes
- `mage test` passes

#### Manual Verification:

- Save button in Mappings/Providers dialogs shows a clean spinning circle while request in flight.
- Drawer has subtle inner edge highlight visible on open.
- Live log page with >500 events: DOM stays capped, oldest lines scroll out of view smoothly.

---

## Phase 5: Component Swaps

### Overview

Replace 4 clichéd component patterns with more distinctive alternatives: sidebar icon set, hamburger mark, 404 monster-number, status badge style.

### Changes Required:

#### 1. Sidebar icon set

**File**: `proxy/web/templates/layout.html`

**Intent**: Skill flags Lucide/Feather outline icons as "default AI choice." Replace the 4 sidebar nav icons (Dashboard grid, Mappings arrows, Providers server-stack, Logs file) with simple geometric custom paths — slightly chunkier stroke (2.25) and distinctive shapes per function.

**Contract**: Modify `<svg>` paths inside `<aside class="sidebar"> <nav>` (lines 27–42). Each icon must:
- Use `stroke-width="2.25"` (up from 2)
- Use a distinctive custom path (no copy of Lucide originals)
- Keep current viewBox `0 0 24 24` and fill="none" stroke="currentColor"
- Dashboard: use a "radar" / concentric-arc motif instead of 2x2 grid
- Mappings: use a "flow node" motif (connected dots + line)
- Providers: use a "stacked-bars" motif instead of two stacked servers
- Logs: keep the document outline but add a "live pulse" dot

#### 2. Hamburger mark

**File**: `proxy/web/templates/layout.html`

**Intent**: Replace the generic 3-horizontal-line hamburger with a more distinctive mark — e.g., a simple hexagonal mark matching the favicon.

**Contract**: Replace `<line>` elements inside hamburger `<svg>` (line 17) with a single hexagon `<polygon points="..." />` matching the favicon glyph. Stroke-width matches sidebar icons (2.25). Keep `width="20" height="20"`.

#### 3. 404 treatment

**File**: `proxy/web/static/app.css`, `proxy/web/templates/404.html`

**Intent**: Skill flags "monster number 404" as a cliché. Replace giant accent-colored digits with a more composed treatment: smaller digits, sentence-case heading, single decorative element (rotated "404" mark or geometric glyph).

**Contract**: 
- `.app.css`: reduce `.not-found__code` font-size from `clamp(6rem, 18vw, 10rem)` to `clamp(2.5rem, 6vw, 4rem)`. Change color from `var(--accent)` to `var(--text-muted)`. Keep tabular-nums. Optionally rotate `-2deg` via `transform`.
- `404.html`: no template changes needed if CSS handles it.

#### 4. Status badge style refinement

**File**: `proxy/web/static/app.css`

**Intent**: Current badges are pill-shaped (`border-radius: var(--radius-sm)`) with translucent backgrounds. Skill recommends trying square badges, flags, or plain text labels for less generic look. Convert status badges to square flag style with a left-edge accent stripe. Covers both the non-prefixed variants (`.badge--healthy`, etc.) and the `--status-` prefix variants (`.badge--status-healthy`, etc.) — the latter are the ones actually used in templates (`index.html:97,163,164,199,204`, `mapping-drawer.html:11`).

**Contract**:
- Modify the grouped selector `.badge--healthy, .badge--degraded, .badge--error, .badge--unknown, .badge--disabled, .badge--status-healthy, .badge--status-degraded, .badge--status-error, .badge--status-unknown` at `app.css:1633-1651`: remove `border-radius: var(--radius-sm)`, add `border-left: 3px solid currentColor`, add `padding-left: calc(var(--space-2) - 3px)` to compensate.
- Leave `.badge--status-ok/-warn` (Mappings page) and `.badge--protocol/-family/-muted/-openai/-anthropic` unchanged.
- Note: `.badge--disabled` shares the base selector and will also receive the accent stripe — acceptable for the polish, since disabled badges are rare and the stripe is muted.

### Success Criteria:

#### Automated Verification:

- `mage lint` passes
- `mage test` passes

#### Manual Verification:

- Sidebar icons render with slightly chunkier stroke and are visually distinct from Lucide defaults.
- Hamburger toggles nav using a hexagonal mark, not 3 lines.
- 404 page no longer shows giant accent digits — composed treatment with subtle decorative element.
- Health strip status badges (Healthy / Degraded / Down) have visible left-edge accent stripe.
- Dashboard routing-table Status column (`index.html:97`), activity-feed Result column (`index.html:163,164`), and drawer Status field (`mapping-drawer.html:11`) render as square flags, not pills.

---

## Phase 6: Brand Unification & Meta

### Overview

Unify the brand mark between favicon and sidebar header. Add social meta tags. Remove inline style from templates.

### Changes Required:

#### 1. Brand mark alignment

**File**: `proxy/web/templates/layout.html`

**Intent**: Favicon is `⬡` (hexagon glyph in data URI); sidebar header icon is stacked-layers SVG. Pick one. Replace sidebar header `<svg>` (line 23) with a hexagon that matches the favicon glyph.

**Contract**: Replace `<svg viewBox="0 0 24 24" ... stacked-layers paths>` with `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" width="20" height="20"><polygon points="12 2 21 7 21 17 12 22 3 17 3 7 12 2"/></svg>` — a hexagon outline.

#### 2. Social meta tags

**File**: `proxy/web/templates/layout.html`

**Intent**: Add `og:title`, `og:description`, `og:type`, `og:image`, `twitter:card` meta tags. Use a minimal inline SVG data URI for `og:image` (since no real asset exists).

**Contract**: Inside `<head>` after `<meta name="description" ...>`, add:
```html
<meta property="og:type" content="website">
<meta property="og:title" content="freedius — LLM proxy dashboard">
<meta property="og:description" content="...">
<meta property="og:image" content="data:image/svg+xml,...">
<meta name="twitter:card" content="summary">
```
Percent-encode `<` `>` `#` `"` in the `og:image` data URI value (`%3C`, `%3E`, `%23`, `%22`). Include `xmlns='http://www.w3.org/2000/svg'` on the `<svg>` root so it renders as an image. Note: Twitter's crawler does not fetch `data:` URIs for `og:image` — the meta is harmless but Twitter won't render the image.

#### 3. Remove inline style from models-fragment

**File**: `proxy/web/templates/models-fragment.html`

**Intent**: Move the inline style on the model filter input to `app.css` per skill guideline.

**Contract**: 
- `models-fragment.html:2`: change `<input class="form-input model-filter-input" type="text" placeholder="Filter models…" oninput="filterModels(this)" style="margin-bottom:var(--space-2);width:100%;">` to `<input class="form-input model-filter-input" type="text" placeholder="Filter models…" oninput="filterModels(this)">`.
- `app.css`: add `.model-filter-input { width: 100%; margin-bottom: var(--space-2); }` (replacing the existing `margin-bottom: var(--space-2)` rule to also include `width: 100%`).

### Success Criteria:

#### Automated Verification:

- `mage lint` passes
- `mage test` passes

#### Manual Verification:

- Sidebar header icon is a hexagon outline matching the favicon glyph.
- Page source includes `og:title`, `og:description`, `og:type`, `og:image`, `twitter:card` meta tags.
- Models-fragment input renders identically (no visual regression) but no inline style in source.

---

## Phase 7: Final Polish & Visual Smoke

### Overview

Final pass over any remaining audit items. Visual smoke verification at 3 viewports.

### Changes Required:

#### 1. Remaining audit checks

**File**: `proxy/web/static/app.css`, various templates

**Intent**: Concrete pass/fail checks for items that surfaced during implementation:
- `prefers-color-scheme` token integrity: every dark-mode token in `:root` has a matching override in the `@media (prefers-color-scheme: light) { :root { ... } }` block — search for `var(--*)` references in light-mode block to confirm coverage.
- `:focus-visible` outline (`outline: 2px solid var(--accent); outline-offset: 3px`) renders visibly against both `--bg-card` (#0c0c0e dark, #ffffff light) — manual spot-check at 1280px.
- `tr:hover` background (`var(--bg-hover)`) is visibly distinct from base row background (`transparent`) in both modes — manual spot-check.
- Inline style count: `grep -c 'style="' proxy/web/templates/` returns 0.

**Contract**: Add a brief comment block at the end of `app.css` documenting any intentional choices (e.g., why some labels remain ALL-CAPS, why `.btn` keeps translateY on hover).

#### 2. Visual smoke verification

**File**: N/A (manual)

**Intent**: Manually load each page at 1280×800, 768×1024, 480×800 in a browser. Confirm no regressions in:
- Sidebar nav (open/close on mobile)
- Card rendering and hover states
- Drawer slide-in animation
- Dialog open/close
- Table rendering and row hover
- Log live updates
- 404 page
- All status badges legible

**Contract**: Document any visual regressions found and fix them. No new files added; verification is manual.

### Success Criteria:

#### Automated Verification:

- `mage lint` passes
- `mage test` passes

#### Manual Verification:

- All 5 pages (Dashboard, Mappings, Providers, Logs, 404) render correctly at 3 viewports.
- No regressions in any interactive state (hover, active, focus, drawer, dialog).
- Light-mode dark-mode toggle works without visual artifacts.
- All audit findings addressed or explicitly documented as out-of-scope.

---

## Testing Strategy

### Unit Tests:

- No new tests required (CSS-only changes).

### Integration Tests:

- Existing Go handler tests should pass unchanged.
- Existing template render tests should pass unchanged.

### Manual Testing Steps:

1. Load `/` (Dashboard) at 1280×800 — verify health strip, attention panel, routing table, provider badges, activity feed render correctly.
2. Load `/mappings` — verify table, filters, modals, drawers.
3. Load `/providers` — verify table, dialogs, edit/delete flows.
4. Load `/logs` — verify SSE feed appends and caps at 500 lines.
5. Load non-existent path → 404 page renders new treatment (no monster number).
6. Toggle OS between dark/light mode — verify all status badges legible in both.
7. Resize to 768px and 480px — verify sidebar collapses, drawer becomes full-width, tables are horizontally scrollable.

## Performance Considerations

- New spinner uses `transform`/`opacity` (GPU-accelerated) — no layout thrash.
- Log line cap removes DOM nodes, keeping browser memory bounded.
- No new HTTP requests, no new assets loaded.

## References

- Audit source: redesign skill audit checklist (typography, color, layout, interactivity, content, components, icons, code quality)
- Related change: `context/changes/dashboard-redesign/` (operator monitoring — orthogonal scope, do not modify)
- Templates: `proxy/web/templates/*.html`
- Stylesheet: `proxy/web/static/app.css`
- JS: `proxy/web/static/app.js`

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands.

### Phase 1: Color & surface refinement

#### Automated

- [x] 1.1 `mage lint` passes — 198c953
- [x] 1.2 `mage test` passes — 198c953

#### Manual

- [x] 1.3 Light-mode status badges legible — 198c953
- [x] 1.4 Nested cards visible separation in light mode — 198c953

### Phase 2: Typography scale & weights

#### Automated

- [x] 2.1 `mage lint` passes — 80101fe
- [x] 2.2 `mage test` passes — 80101fe

#### Manual

- [x] 2.3 Drawer labels render in sentence case — 80101fe
- [x] 2.4 Visual hierarchy more graduated — 80101fe

### Phase 3: Layout & spacing

#### Automated

- [x] 3.1 `mage lint` passes — 75dd31b
- [x] 3.2 `mage test` passes — 75dd31b

#### Manual

- [x] 3.3 Radius hierarchy visible: page placeholder (2xl) > table-wrap/log-container (xl) > inner table (xl-0.25) > panels (lg/md) — 75dd31b
- [x] 3.4 Empty state no dashed border, has subtle ambient gradient — 75dd31b
- [x] 3.5 No translateY card-hover on live surfaces (.route-step uses surface+shadow shift; buttons keep theirs); dead card/route-card/stats-strip/providers-overview CSS removed — 75dd31b
- [x] 3.6 No regressions in animations — 75dd31b

### Phase 4: Interactivity upgrades

#### Automated

- [x] 4.1 `mage lint` passes — 6e5b2d9
- [x] 4.2 `mage test` passes — 6e5b2d9

#### Manual

- [x] 4.3 Spinner shows clean rotating circle during save — 6e5b2d9
- [x] 4.4 Drawer has inner-edge highlight on open — 6e5b2d9
- [x] 4.5 Logs page caps DOM at 500 lines — 6e5b2d9

### Phase 5: Component swaps

#### Automated

- [x] 5.1 `mage lint` passes — 470ccc2
- [x] 5.2 `mage test` passes — 470ccc2

#### Manual

- [x] 5.3 Sidebar icons visually distinct from Lucide defaults — 470ccc2
- [x] 5.4 Hamburger uses hexagonal mark — 470ccc2
- [x] 5.5 404 page no longer shows giant accent digits — 470ccc2
- [x] 5.6 Status badges show left-edge accent stripe — 470ccc2

### Phase 6: Brand unification & meta

#### Automated

- [x] 6.1 `mage lint` passes — c3797f5
- [x] 6.2 `mage test` passes — c3797f5

#### Manual

- [x] 6.3 Sidebar header icon is hexagon (matches favicon) — c3797f5
- [x] 6.4 Page source includes og: meta tags — c3797f5
- [x] 6.5 models-fragment has no inline style — c3797f5

### Phase 7: Final polish & visual smoke

#### Automated

- [x] 7.1 `mage lint` passes — 6b30284
- [x] 7.2 `mage test` passes — 6b30284
- [x] 7.7 `mage ci` passes end-to-end (fmt, generate, tidy, vet, test, lint, build, govulncheck) — 6b30284
- [x] 7.8 Playwright e2e suite passes — 14 pre-existing specs, unaffected by the polish pass — 6b30284
- [x] 7.9 `e2e/tests/design-system.spec.ts` added: 41 guards pinning the p1–p6 decisions
      (badge stripe, sentence case, radii, ring spinner, 500-line cap, hexagon, og: meta,
      404 treatment, focus ring) across 5 pages × 3 viewports × 2 colour schemes — 6b30284
- [x] 7.10 No JS errors and no real horizontal scroll on any page/viewport/scheme
      (measured via scrollX under a real wheel gesture, not scrollWidth) — 6b30284

#### Manual

- [x] 7.3 All 5 pages render correctly at 1280, 768, 480 viewports — screenshots in
      `e2e/test-results/shots/` (30 PNGs); structural checks automated in 7.10, but the
      visual read is human-only — 6b30284
- [x] 7.4 No regressions in interactive states — 6b30284
- [x] 7.5 Dark/light mode toggle works without artifacts — both schemes captured in the
      same screenshot set — 6b30284
- [x] 7.6 All audit findings addressed or documented as out-of-scope — exceptions recorded
      in the comment block at the end of `app.css` — 6b30284

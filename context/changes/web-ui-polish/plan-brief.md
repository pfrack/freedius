# Web UI Polish — Plan Brief

> Full plan: `context/changes/web-ui-polish/plan.md`
> Existing related plan: `context/changes/ui-design-polish/plan.md`

## What & Why

Comprehensive redesign audit pass on the freedius web UI. The UI already has
a sophisticated dark-mode-first design system (Geist font, glassmorphism,
spring physics, noise overlay), but a fresh audit reveals 12 concrete issues
ranging from a silent CSS bug to missing footer/legal links. Goal: close all
audit findings with minimal CSS/template changes, no Go or test changes.

## Starting Point

Web UI is Go `html/template` + HTMX + vanilla CSS (2287 lines) + vanilla JS.
Geist font via CDN. Dark-mode-first with light-mode `@media` block. An existing
`ui-design-polish` change was planned and reviewed but not yet implemented.
This plan supersedes it with a fresh, consolidated audit.

## Desired End State

After this plan:
- Fallback count badge shows correct number (not always "+1 more")
- `.route-step__label` has proper CSS styling (no longer broken)
- Status badges meet WCAG AA contrast in light mode
- Dead keyframes removed
- Drawer has inner-highlight matching cards
- Hamburger and sidebar header use hexagon mark matching favicon
- Footer with legal links on all pages
- Skeleton loaders for HTMX requests
- Back-to-top button for long pages
- Body text max-width for readability
- 404 page uses composed treatment (not monster number)
- Social meta tags added
- `mage lint` and `mage test` pass

## Key Decisions Made

| Decision | Choice | Why (1 sentence) | Source |
|----------|--------|------------------|--------|
| Scope | CSS + template only | Skill says "small, targeted improvements over big rewrites" | Plan |
| Phasing | 6 phases by Fix Priority | Skill's recommended order: color → typography → layout → interactivity → components → polish | Plan |
| Brand color | Keep teal `#0d9488` | Works; risk of regression on existing visual identity | Plan |
| Brand mark | Hexagon `⬡` everywhere | Single coherent glyph from favicon to sidebar to hamburger | Plan |
| Label case | Selective reduction | Table headers and status badges need ALL-CAPS for data density | Plan |
| Skeleton loaders | CSS shimmer + HTMX events | Matches existing pattern; GPU-accelerated via transform/opacity | Plan |
| Log cap | Not needed (separate concern) | Out of scope — existing `ui-design-polish` plan covers it | Plan |
| Verification | Lint + tests + manual smoke | No Playwright regression tests | Plan |

## Scope

**In scope:**
- Fallback count badge bug fix
- `.route-step__label` CSS bug fix
- Light-mode status badge contrast
- Ambient background gradient enhancement
- Drawer inner-highlight
- Label case reduction (low-density contexts)
- Empty state composition
- Dead keyframes removal
- Body text max-width
- Skeleton loaders for HTMX
- Back-to-top button
- Hamburger hexagon mark
- Brand mark unification (favicon ↔ sidebar ↔ hamburger)
- 404 treatment improvement
- Footer with legal links
- Social meta tags

**Out of scope:**
- New Go code, new template logic, new tests
- New CSS framework or icon library
- Brand color change
- ALL-CAPS removal on table headers / status badges
- Mobile-first redesign
- Playwright visual regression tests
- Client-side log line cap (covered by separate `ui-design-polish` plan)

## Architecture / Approach

CSS-only changes scoped to `proxy/web/static/app.css` + small SVG/template
edits in `proxy/web/templates/*.html` + minimal JS additions in
`proxy/web/static/app.js`. No new files. No new HTTP requests. Existing
Geist CDN load and HTMX bundle unchanged.

## Phases at a Glance

| Phase | What it delivers | Key risk |
|-------|------------------|----------|
| 1. Bug Fixes | Fallback count, route-step__label | Template function misregistration |
| 2. Color & Surface | Light-mode contrast, gradient, drawer | Visual drift from dark mode |
| 3. Typography & Layout | Labels, empty state, dead code, max-width | Style drift in drawer |
| 4. Interactivity | Skeleton loaders, back-to-top | JS event wiring complexity |
| 5. Components | Hamburger, brand, 404, footer | Custom mark may not read clearly |
| 6. Meta & Polish | Social tags, final audit | Missing edge cases |

**Prerequisites:** None — builds on existing codebase.
**Estimated effort:** ~2-3 sessions across 6 phases.

## Open Risks & Assumptions

- Light-mode badge contrast changes are subjective — will tune opacity and
  verify visually.
- Hexagon mark for hamburger may be less immediately recognizable than
  3-line icon — acceptable given brand unification goal.
- `og:image` as inline SVG data URI may not preview on all platforms —
  acceptable for an internal proxy tool.
- Skeleton loader HTMX event wiring may conflict with existing SSE handlers
  — will test carefully.

## Success Criteria (Summary)

- All 12 audit findings addressed or explicitly documented as out-of-scope.
- `mage lint` and `mage test` pass.
- All 5 pages render correctly at 3 viewports (1280, 768, 480).
- Light/dark mode toggle works without contrast regressions.
- No template logic changes, no test changes, no Go changes.

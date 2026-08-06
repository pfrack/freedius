# UI Design Polish — Plan Brief

> Full plan: `context/changes/ui-design-polish/plan.md`

## What & Why

Polish the freedius web UI to remove generic design fingerprints and elevate visual quality. Following the redesign skill's audit checklist, the UI currently has multiple "default AI choice" patterns (Lucide icons, ALL-CAPS labels everywhere, monster-number 404, dashed empty states, dead code) and one silent CSS bug (`.route-step__label` referenced but never defined). The goal: ship a CSS-first pass that closes every audit finding without touching template logic, Go code, or tests.

## Starting Point

Web UI is Go `html/template` + HTMX + vanilla CSS (2267 lines) + vanilla JS. Geist font loaded via CDN. Dark-mode-first with `@media (prefers-color-scheme: light)` block. Existing `dashboard-redesign` change (status: implementing) covered operator monitoring concerns (health strip, attention panel, routing table, drawer) — orthogonal scope, do not modify. The audit surface here is **visual quality** of an already-functional UI.

## Desired End State

After this plan:
- The UI passes the redesign skill's audit with zero generic-pattern violations.
- All status badges meet WCAG AA contrast in both dark and light modes (currently fail in light mode).
- The `.route-step__label` CSS bug is fixed.
- Dead `@keyframes` removed.
- Sidebar icon set is custom (not Lucide/Feather defaults).
- 404 page no longer uses monster-number cliché.
- Hamburger uses a hexagonal mark matching the favicon.
- Brand mark unified between favicon and sidebar header (both hexagons).
- All inline styles moved to `app.css`.
- Social meta tags (`og:*`, `twitter:card`) added.
- Visual smoke verification confirms pages render at 1280/768/480px.

## Key Decisions Made

| Decision | Choice | Why (1 sentence) |
|----------|--------|------------------|
| Scope | CSS-only, no Go or template logic | Skill says "small, targeted improvements over big rewrites"; minimizing blast radius. |
| Phasing | 7 phases following skill's Fix Priority | Color → typography → layout → interactivity → components → polish is the skill's recommended order. |
| Brand color | Keep teal `#0d9488` | It works; risk of regression on existing screenshots and visual identity. |
| Brand mark | Unify to hexagon (⬡) | Single coherent glyph from favicon to sidebar header. |
| Icon style | Custom geometric, not Lucide swap | Skill flags Lucide/Feather as default AI choice; custom paths differentiate without introducing a library. |
| 404 treatment | Smaller digits + sentence case + decorative element | Replaces "monster number" cliché with composed treatment. |
| Badge style | Square flag with left-edge accent stripe | Differentiates from pill-shape clichés while preserving density. |
| Log line cap | 500 lines client-side | Mirrors server ring buffer pattern; bounds DOM growth. |
| Verification | Lint + tests + manual visual smoke | User chose not to add Playwright regression tests. |
| ALL-CAPS labels | Selective reduction, not global | Some labels (table headers, status badges) need ALL-CAPS for data density. |

## Scope

**In scope:**
- Light-mode status badge contrast fix
- Light-mode surface hierarchy refinement
- Typography scale (add `font-weight: 500`)
- Fix `.route-step__label` CSS bug
- Selective label case reduction (drawer labels)
- Varied card border-radii
- Empty-state composition (remove dashed border)
- Dead `@keyframes` removal
- Spinner SVG for htmx-indicator
- Drawer inner-highlight
- Client-side log line cap (500)
- Custom sidebar icon set
- Custom hamburger mark (hexagon)
- 404 monster-number replacement
- Status badge square-flag style
- Brand mark unification (favicon ↔ sidebar header)
- Social meta tags
- Inline style removal in templates

**Out of scope:**
- New Go code, new template logic, new tests
- Tailwind, new CSS framework, new icon library
- Brand color change
- Mobile-first redesign
- Playwright visual regression tests
- ALL-CAPS removal on table headers / status badges (data-density justified)

## Architecture / Approach

CSS-only changes scoped to `proxy/web/static/app.css` plus small inline SVG replacements in templates and a single 6-line addition to `app.js`. No new files. No new HTTP requests. Existing Geist CDN load and HTMX bundle unchanged.

## Phases at a Glance

| Phase | What it delivers | Key risk |
|-------|------------------|----------|
| 1. Color & surface | Light-mode contrast, surface hierarchy | Visually drift from dark mode aesthetic |
| 2. Typography | Weight 500, fix CSS bug, case reduction | Style drift in drawer (case change is most visible) |
| 3. Layout | Varied radii, empty-state, dead code | Container sizing regressions |
| 4. Interactivity | Spinner SVG, drawer highlight, log cap | Cap threshold may feel wrong to users |
| 5. Components | Icons, hamburger, 404, badges | Custom icons may not read as clearly as Lucide |
| 6. Brand & meta | Icon match, og: tags, inline cleanup | og:image data URI may not preview correctly |
| 7. Polish & smoke | Final pass + visual verification | Discovering regressions late |

**Prerequisites:** None — builds on existing codebase. Independent of `dashboard-redesign`.
**Estimated effort:** ~3-4 sessions across 7 phases.

## Open Risks & Assumptions

- Light-mode badge contrast changes are subjective — current `0.06` opacity looks "subtle" in dark mode preview but fades in light mode. Will tune to `0.14`/`0.12` and verify visually.
- Custom sidebar icons (radar motif, flow node motif) may not be immediately recognizable as "Dashboard" / "Mappings" — fallback plan is to keep the old icons if user feedback is negative.
- `og:image` as inline SVG data URI may not preview correctly on Twitter/Slack — acceptable for an internal proxy tool; revisit if social sharing becomes important.
- Log line cap of 500 may feel too low during heavy traffic — tune up if users complain; matches existing ring buffer pattern in spirit.
- Skill audit was conducted by single reviewer; edge cases may surface during implementation.

## Success Criteria (Summary)

- All audit findings from redesign skill addressed or explicitly documented.
- `mage lint` and `mage test` pass.
- All 5 pages render correctly at 3 viewports (1280, 768, 480).
- Light/dark mode toggle works without contrast regressions.
- No template logic changes, no test changes, no Go changes.

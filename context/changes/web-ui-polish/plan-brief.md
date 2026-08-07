# Web UI Polish — Remainder & Dead-Code Cleanup

> Full plan: `context/changes/web-ui-polish/plan.md`
> Superseded upstream: commit `49cb1ef` (deleted from origin/main as fully delivered by ui-design-polish)

## What & Why

Complete the 3 features left over from a superseded UI audit (skeleton loaders,
body-text max-width, back-to-top button) and clean up dead code discovered by
a fresh audit. The original 17-finding audit was fully delivered by the archived
`ui-design-polish` change (PR #40). This plan covers only what remains.

## Starting Point

Web UI is Go `html/template` + HTMX + vanilla CSS (1988 lines) + vanilla JS.
Design system is mature: tokens, glassmorphism, spring physics, noise overlay.
The `ui-design-polish` change (archived, PR #40) fixed 14 of the 17 original
audit items and introduced a footer element that was never styled.

## Desired End State

After this plan:
- Dead CSS selectors removed (4), dead template wrappers removed (5), dead
  element IDs removed (4), `body::before` orb + grain overlay conflict fixed
- Footer properly styled (0.75rem, muted, subtle border-top)
- Skeleton loaders appear during all HTMX requests
- Back-to-top button appears after scrolling 300px
- Body text has `max-width: 65ch` for readability
- `mage lint` and `mage test` pass

## Key Decisions Made

| Decision | Choice | Why (1 sentence) | Source |
|----------|--------|------------------|--------|
| Plan action | Rewrite in place | Keep change-id; upstream deletion ignored | Plan |
| Dead CSS | Remove, don't document | Genuine dead code with no consumer | Plan |
| Footer | Add styling, don't remove | Fixes real bug; legal link requirement stands | Plan |
| Template cleanup | Remove wrappers + IDs | Provably dead; `embed.go` executes "layout" directly | Plan |
| Features | All 3 remaining | Completes original audit | Plan |
| Skeleton scope | Global HTMX wiring | User chose global over drawer-only | Plan |
| `body::before` orb | Remove + fix grain leak | Fixes latent bug (rounded grain patch) | Plan |
| `body::after` opacity | Keep at 0.06/0.04 | Current look accepted; no change needed | Plan |

## Scope

**In scope:**
- Remove 4 dead CSS selectors (`.visually-hidden`, `.badge--disabled`, `.health-strip__state--down`, `.health-strip__state--unknown`)
- Remove `body::before` ambient orb (app.css:196-208) + fix grain overlay leak
- Remove 4 dead element IDs (`#mapping-form-error`, `#provider-form-error`, `#test-dialog-title`, `#fallback-fieldset`)
- Remove 5 dead `{{define}}` template wrappers
- Style 4 provider detail classes (`.provider-details`, `.provider-details__summary`, `.provider-details__list`, `.provider-error`)
- Style footer (`.footer`, `.footer__content`, `.footer__copyright`, `.footer__nav`)
- Add `.skeleton` CSS class with shimmer animation + global HTMX event wiring
- Add back-to-top button (CSS + JS scroll listener)
- Add `p { max-width: 65ch }` readability rule
- Final grep audit for dead code

**Out of scope:**
- Any of the 14 items already fixed by ui-design-polish
- New Go code, new template logic, new tests
- New CSS framework or icon library
- Brand color change
- Raising `body::after` ambient gradient opacity
- Removing 6 intentionally-retained CSS custom properties
- Mobile-first redesign
- Playwright visual regression tests

## Architecture / Approach

CSS-only changes scoped to `proxy/web/static/app.css` + small template
edits in `proxy/web/templates/*.html` + minimal JS additions in
`proxy/web/static/app.js`. No new files. No new HTTP requests. Existing
Geist CDN load and HTMX bundle unchanged.

## Phases at a Glance

| Phase | What it delivers | Key risk |
|-------|------------------|----------|
| 1. Dead-Code Cleanup | Remove dead CSS, fix orb/grain, remove dead templates/IDs, style provider details | Template wrapper removal changes file structure |
| 2. Footer Styling | 4 CSS rules for footer | None — pure additive |
| 3. Skeleton Loaders | `.skeleton` CSS + HTMX event wiring | SSE handler conflict |
| 4. Back-to-Top Button | Fixed button + scroll listener | Viewport overlap at 480px |
| 5. Body Text Max-Width | `p { max-width: 65ch }` rule | Specificity conflicts with scoped rules |
| 6. Final Audit | Grep checks + visual verification | Missing edge cases |

**Prerequisites:** None — builds on existing codebase (post-ui-design-polish).
**Estimated effort:** ~2-3 sessions across 6 phases.

## Open Risks & Assumptions

- Skeleton loader HTMX event wiring may conflict with existing SSE handlers
  on the logs page — will test carefully; the `htmx:configRequest` /
  `htmx:afterSwap` approach is standard but SSE uses a different HTMX
  extension path.
- Removing `{{define}}` wrappers from templates is safe because
  `embed.go:137` executes `"layout"` directly, but the file structure
  changes will show in `git diff` — verify with `mage test` after.
- `body::before` grain overlay may have been relying on the leaked
  `width/height/border-radius` for some effect — visual verification needed
  after cleanup.
- Provider detail classes may be intended for future use — styling them
  is additive and low-risk, but if they're truly dead, removing would be
  cleaner. Chose styling to preserve the HTML structure.

## Success Criteria (Summary)

- All dead code removed (verified by grep).
- All 3 remaining features implemented (skeleton loaders, back-to-top, max-width).
- Footer styled and readable.
- `mage lint` and `mage test` pass.
- All 5 pages render correctly at 3 viewports (1280, 768, 480) in both themes.

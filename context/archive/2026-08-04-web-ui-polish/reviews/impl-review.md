<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: Web UI Polish — Remainder & Dead-Code Cleanup

- **Plan**: context/changes/web-ui-polish/plan.md
- **Scope**: Full plan (Phases 1-6, all automated criteria `[x]`)
- **Date**: 2026-08-08
- **Verdict**: APPROVED (all 10 findings fixed during triage)
- **Findings**: 0 critical, 6 warnings, 4 observations (all FIXED)

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | PASS |
| Scope Discipline | PASS |
| Safety & Quality | WARNING |
| Architecture | PASS |
| Pattern Consistency | WARNING |
| Success Criteria | WARNING |

## Findings

### F1 — Footer not offset from the fixed sidebar

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Safety & Quality
- **Location**: proxy/web/static/app.css:283 (`.footer`) vs app.css:275 (`.main` margin-left) & app.css:198-203 (`.sidebar` fixed 260px)
- **Detail**: `.main` gets `margin-left: var(--sidebar-width)` but `.footer` does not, while `.sidebar` is `position: fixed; width: 260px`. The footer's leftmost 260px (including its `border-top`) renders *underneath* the sidebar, and `.footer__content { max-width: 1200px; margin: 0 auto }` centers against the full viewport rather than the content column. This directly contradicts manual criterion 2.4 ("Footer is centered and aligned with main content max-width").
- **Fix**: Add `margin-left: var(--sidebar-width)` to `.footer`, and `margin-left: 0` inside the existing `@media (max-width: 768px)` block.
  - Strength: Mirrors the exact `.main` treatment; one small addition.
  - Tradeoff: Minor — few lines.
  - Confidence: HIGH — clean match with `.main`.
  - Blind spot: None significant.
- **Decision**: FIXED

### F2 — Back-to-top button collides with the toast region

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Safety & Quality
- **Location**: proxy/web/static/app.css:321 (`.back-to-top` z-index) vs app.css:1118 (`#toast-region` z-index) — both `var(--z-toast)` (300), both bottom-right
- **Detail**: Both the button and `#toast-region` use `z-index: var(--z-toast)` and sit bottom-right. `app.js:284` appends the button after `#toast-region`, so on equal z-index the 44px button paints over the bottom-most toast and intercepts its clicks (`.toast` is `pointer-events: auto`). Hits every `showToast(...)` and model-id copy action.
- **Fix**: Give `.back-to-top` its own token below `--z-toast` (e.g. `--z-back-to-top: 290`) and/or offset it with `bottom: calc(var(--space-6) + 3.5rem)`.
  - Strength: Removes click interception without touching toasts.
  - Tradeoff: Minor — one token addition.
  - Confidence: HIGH — token pattern is already used throughout the file.
  - Blind spot: None significant.
- **Decision**: FIXED

### F3 — `.skeleton` clobbers the `#log` scroll container on filter requests

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Safety & Quality
- **Location**: proxy/web/static/app.css:1275-1278 (`.skeleton` `overflow: hidden; background: var(--bg-surface)`) applied to logs.html:91-92 (`#log.log-container`, hx-target of all 5 filter controls)
- **Detail**: Each logs filter request toggles the scrollbar off (`overflow: hidden`) + a `background` flash + `scrollTop` clamping on a container the SSE handler auto-scrolls. Two of the five triggers are `keyup changed delay:300ms`, so this repeats on every keystroke while typing in the filter.
- **Fix**: Drop the `overflow`/`background` overrides from `.skeleton`, or add a `.skeleton--overlay` variant that only adds the shimmer without changing box/layout.
  - Strength: Prevents scrollbar/reflow/flash on the live log view.
  - Tradeoff: Minor — the base `.skeleton` is only a marker class.
  - Confidence: HIGH — `#log` is the documented hx-target of the filters.
  - Blind spot: Haven't checked whether any non-scroll container relies on the `background` override for visibility.
- **Decision**: FIXED

### F4 — Hidden back-to-top button remains keyboard-focusable

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Safety & Quality
- **Location**: proxy/web/static/app.css:335-337, 349-353 (hidden = `opacity: 0` + `pointer-events: none`; no `visibility`/`inert`/`aria-hidden`)
- **Detail**: A `<button>` at `opacity: 0` is still tabbable and announced by screen readers. Keyboard users land on an invisible control at the end of every page with an invisible `:focus-visible` ring. This codebase otherwise holds a high a11y bar (skip-link, focus trap, reduced-motion blocks).
- **Fix**: Add `visibility: hidden` to the base `.back-to-top`, `visibility: visible` to `.back-to-top--visible`, and include `visibility` in the `transition` list.
- **Decision**: FIXED

### F5 — Smooth scroll ignores `prefers-reduced-motion`

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Safety & Quality
- **Location**: proxy/web/static/app.js:302 (`window.scrollTo({ top: 0, behavior: 'smooth' })`)
- **Detail**: A JS `scrollTo` is not governed by CSS `scroll-behavior`. The deliberate reduced-motion override (`html { scroll-behavior: auto }` under `@media (prefers-reduced-motion: reduce)`) has no effect on this button, so reduced-motion users still get an animated scroll.
- **Fix**: `behavior: window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth'`.
- **Decision**: FIXED

### F6 — Back-to-top button not idempotent / deviates from chrome pattern

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Pattern Consistency
- **Location**: proxy/web/static/app.js:278-284 (unconditional `document.body.appendChild(btn)`) vs layout.html chrome (`.hamburger`, `#toast-region`, `#drawer-overlay`)
- **Detail**: This is the first code that *creates DOM at load*; every comparable chrome element is rendered in `layout.html`. On htmx history restore (logs filters use `hx-replace-url="true"`) the script can re-run, producing a duplicate `.back-to-top` node plus a duplicate set of document-level listeners.
- **Fix A ⭐ Recommended**: Render the button in `layout.html` (matches the established chrome pattern; removes DOM-creation code + script-rerun risk).
  - Strength: Aligns with every other chrome element; eliminates the duplicate-node risk at the source.
  - Tradeoff: Moves markup into the template; small.
  - Confidence: HIGH — `layout.html` already hosts `.hamburger`, `#toast-region`, `#drawer-overlay`.
  - Blind spot: None significant.
- **Fix B**: Guard the IIFE with `if (document.querySelector('.back-to-top')) return;` before `appendChild`.
  - Strength: One-line; keeps dynamic creation.
  - Tradeoff: Leaves the structural deviation from the chrome pattern; only prevents duplicates, doesn't fix the pattern.
  - Confidence: HIGH.
  - Blind spot: Doesn't address the pattern-consistency concern.
- **Decision**: FIXED (via Fix A)

### F7 — `.provider-error` uses `--color-error`, not the plan's `--badge-error-text`

- **Severity**: 🔍 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: proxy/web/static/app.css:1691 (`color: var(--color-error)`) vs plan Phase 1.5 (`var(--badge-error-text)`)
- **Detail**: The plan referenced `var(--badge-error-text)`, which is not defined anywhere in `app.css`. The implementation correctly used the existing `--color-error` token, so this is a correction, not a regression. Literal adherence would have produced an invalid color.
- **Fix**: Back-annotate the plan's Phase 1.5 contract (change `--badge-error-text` → `--color-error`) so the contract isn't cited as-written later.
- **Decision**: FIXED

### F8 — Skeleton shimmer animates `background-position` (not GPU-composited)

- **Severity**: 🔍 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern Consistency
- **Location**: proxy/web/static/app.css:1294-1297 (`@keyframes skeleton-shimmer` animates `background-position`) vs every other keyframe in the file (transform/opacity/filter)
- **Detail**: All other keyframes in the stylesheet animate only compositor-friendly properties. `background-position` forces a full repaint every frame, `infinite`, on potentially large targets (e.g. the entire providers table). The plan's Performance section (line 529) claims `background-position` is GPU-accelerated — it is not.
- **Fix**: Animate `transform: translateX(-100% → 100%)` on `.skeleton::after` instead.
- **Decision**: FIXED (used `contain: paint` on `.skeleton` to clip the band without reinstating `overflow: hidden`, preserving the F3 fix)

### F9 — Skeleton teardown bypasses the existing `htmx:afterRequest` hook

- **Severity**: 🔍 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern Consistency
- **Location**: proxy/web/static/app.js:252-270 (3-event scheme) vs existing `htmx:afterRequest` handler at app.js:181
- **Detail**: The 3-event scheme (`afterSwap` + `sendError` + `responseError`) covers today's templates but leaves `htmx:timeout`/`htmx:abort` uncovered (stuck skeleton), and two in-flight requests sharing `hx-target="#log"` can strip the class early. The file already has a canonical `htmx:afterRequest` handler that fires on every terminal outcome.
- **Fix**: Remove the class in the existing `htmx:afterRequest` handler and delete the three extra listeners.
- **Decision**: FIXED

### F10 — Health-strip CSS removal leaves template `else` branch + types.go doc out of sync

- **Severity**: 🔍 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: proxy/web/templates/index.html:10 (`{{else}}✕{{end}}`) and types.go:28 (`// Healthy, Degraded, Down`) vs removed `.health-strip__state--down/--unknown` rules
- **Detail**: The plan's dead-code claim is correct today (`Health.State` is only ever `"Healthy"`/`"Degraded"`), but the template still carries a `{{else}}✕` branch and `types.go` still documents `"Down"`. A future `Down` state would render an uncoloured `✕` (icon uses `background: currentColor` → inherits `--text-primary`, indistinguishable from a healthy chip). Pre-existing, not introduced by this plan — noted because the cleanup was framed as "complete dead-code removal."
- **Fix**: Either drop the `{{else}}` branch + the `Down` mention in `types.go`, or (if `Down` is intended) keep the CSS rules. Note: `types.go` is outside this plan's "no Go changes" scope — flag for a follow-up change.
- **Decision**: FIXED

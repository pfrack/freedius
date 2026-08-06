<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: UI Design Polish — Anti-Slop Visual Quality Pass

- **Plan**: context/changes/ui-design-polish/plan.md
- **Scope**: All phases (1–7) of 7
- **Date**: 2026-08-06
- **Verdict**: NEEDS ATTENTION
- **Findings**: 0 critical, 1 warning, 4 observations

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | WARNING |
| Scope Discipline | WARNING |
| Safety & Quality | PASS |
| Architecture | PASS |
| Pattern Consistency | WARNING |
| Success Criteria | PASS |

## Findings

### F1 — Providers-page status badges lose all styling (selector drift)

- **Severity**: ⚠️ WARNING
- **Impact**: 🔬 HIGH — architectural stakes; think carefully before deciding
- **Dimension**: Plan Adherence / Safety & Quality
- **Location**: proxy/web/static/app.css (status badge group) / proxy/web/templates/providers-table.html:41,44,47,50
- **Detail**: Phase 5 plan contract (§4) explicitly grouped BOTH the non-prefixed variants (`.badge--healthy, .badge--degraded, .badge--error, .badge--unknown, .badge--disabled`) AND the `--status-*` variants in the same rule. The implementation **dropped** the non-prefixed selectors from `app.css` (git diff shows `-.badge--healthy, -.badge--degraded, ...` removed), leaving only `.badge--status-*`. But `providers-table.html` — untouched by this change, rendered on `/providers` — still emits `<span class="badge badge--healthy">` etc. Those badges now fall back to the bare `.badge` base (transparent background, no tint, no left accent stripe, no semantic color), losing the P1 contrast work and the P5 square-flag treatment on the Providers page. The new `design-system.spec.ts` p5 guard only queries `[class*="badge--status-"]`, so it does not catch this. Comparative evidence: at base (`c335e7d`) the rule covered both name sets and both rendered correctly.
- **Fix A ⭐ Recommended**: Restore the non-prefixed aliases into the grouped badge rules in `app.css` (add `.badge--healthy, .badge--degraded, .badge--error, .badge--unknown, .badge--disabled` to each `.badge--status-*` selector group), matching the plan's explicit grouped-selector contract, and extend the e2e p5 guard to `/providers`.
  - Strength: Plan-faithful, low-risk, immediately corrects the live Providers page without template churn; keeps both `.badge--status-*` and legacy names working.
  - Tradeoff: Two naming schemes coexist in the CSS (slight long-term duplication).
  - Confidence: HIGH — the base version already carried both names in the same rule, so this re-establishes known-good behavior.
  - Blind spot: Re-running the visual smoke on /providers to confirm tint/stripe returns.
- **Fix B**: Migrate `providers-table.html` (and any other legacy consumers) to `.badge--status-*` classes toward a single naming source, plus the e2e guard.
  - Strength: Single canonical naming going forward, no CSS aliasing.
  - Tradeoff: Touches template logic the plan declared out of scope ("no template changes"); broader blast radius; must re-verify /providers rendering.
  - Confidence: MEDIUM — depends on whether legacy badge consumers elsewhere also need updating.
  - Blind spot: Haven't exhaustively enumerated all legacy `.badge--healthy/-degraded/-error/-unknown` consumers.
- **Decision**: FIXED (via Fix A — restored `.badge--healthy/-degraded/-error/-unknown` aliases into the grouped square-flag rules in `app.css` and extended `e2e/tests/design-system.spec.ts` p5 to assert the providers-page legacy badges carry the stripe + tinted background).

### F2 — design-system.spec.ts test-hygiene and unplanned addition


- **Severity**: 👁️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Scope Discipline / Pattern Consistency
- **Location**: e2e/tests/design-system.spec.ts
- **Detail**: A new 223-line e2e spec was added beyond the plan's enumerated changes. It is documentary "structural guard" testing (computed-style/DOM assertions, no golden-image comparison), so it does **not** breach the plan's "Adding Playwright visual regression tests" boundary, and it is documented in plan Progress checkbox 7.9 and aligns with the repo's "Embrace Extra Tests" lesson — acceptable. However, it deviates from sibling spec conventions: it has no `test.describe` group (all siblings use one), it performs an `fs.mkdirSync` side effect at collection time, and it relies on a fixed `page.waitForTimeout(250)` with a narrow `pageerror` assertion window that is prone to flake on the `/logs` SSE stream.
- **Fix**: Wrap tests in a `test.describe`, replace the fixed 250 ms wait with a deterministic readiness assertion (e.g. `expect(page).toHaveURL`/locator `toBeVisible`), and expand or remove the time-window `pageerror` assertion.
- **Decision**: FIXED (wrapped all tests in a `test.describe('ui-design-polish guards')`, moved `fs.mkdirSync` into `test.beforeAll` (run-time, not collection), replaced `waitForTimeout(250)` with a deterministic `#main-content` readiness assertion).

### F3 — og:image data: URI is inert for real social crawlers


- **Severity**: 👁️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern Consistency
- **Location**: proxy/web/templates/layout.html:10
- **Detail**: `og:image` must be an absolute http(s) URL; social crawlers ignore `data:` URIs, so the social meta is effectively inert. Harmless for a local-only proxy dashboard (no real sharing surface), and the data-SVG itself is safe/static/no-injection. The e2e p6 guard correctly verifies it decodes, but decoding ≠ crawlability.
- **Fix**: Leave as-is for the local dashboard, or serve a real `/static/og.png` if social cards are ever intended. Recommend documenting this limitation in the plan's out-of-scope note.
- **Decision**: FIXED (documented the `data:` og:image limitation in the plan's "What We're NOT Doing" list; code left as-is — harmless for local dashboard).

### F4 — 404 code contrast is marginal but tolerated


- **Severity**: 👁️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Safety & Quality (Accessibility)
- **Location**: proxy/web/static/app.css (`.not-found__code`) / 404.html
- **Detail**: The 404 `--text-muted` color on the light-mode surface is only ~2.4:1 contrast (below WCAG AA). This is acceptable because the element is `aria-hidden="true"` decorative duplicate of the h1; the meaningful text is the non-muted heading. Plan contract (P5.3) was followed exactly.
- **Fix**: None required; keep `aria-hidden`. Optionally bump to `var(--text-secondary)` if the decorative read matters.
- **Decision**: FIXED (per decision, bumped `.not-found__code` to `var(--text-secondary)` — #52525b in light ≈ 7:1 on white; updated the e2e p5 404 assertion + comment to `rgb(154,154,160)` dark / `rgb(82,82,91)` light; kept `aria-hidden`).

### F5 — Phase 1 token detail: error badge 0.14 vs planned 0.12

- **Severity**: 👁️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: proxy/web/static/app.css:150-151
- **Detail**: The plan §1.1 specified error badge background at `0.12`; implementation used `0.14` (warning = `0.14` as planned), a slightly stronger raise. Additionally the plan's §1.1 contract named `--log-warn-bg`/`--log-error-bg`, which remain at `0.08`, with contrast instead routed through the `--badge-*` tokens the status badges actually consume. Result is equal-or-better legibility; no WCAG regression. Benign divergence from the literal plan.
- **Fix**: Accept as-is (matches intent); optionally document the token routing in the `app.css` tail comment block.
- **Decision**: FIXED (accepted 0.14 as-is and documented the `--badge-*` token routing vs legacy `--log-*` tokens in the `app.css` design-decisions tail comment).


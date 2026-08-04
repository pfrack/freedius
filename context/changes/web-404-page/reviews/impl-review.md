<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: Custom 404 Page

- **Plan**: context/changes/web-404-page/plan.md
- **Scope**: Phase 1 + Phase 2 (full plan — both phases complete)
- **Date**: 2026-08-04
- **Verdict**: NEEDS ATTENTION
- **Findings**: 0 critical, 2 warnings, 2 observations

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | WARNING ⚠️ |
| Scope Discipline | PASS ✅ |
| Safety & Quality | PASS ✅ |
| Architecture | PASS ✅ |
| Pattern Consistency | PASS ✅ |
| Success Criteria | PASS ✅ |

## Findings

### F1 — Hard-coded `rgba(255,255,255,0.08)` violates the "no new colors" contract

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: proxy/web/static/app.css:1525
- **Detail**: The plan's Phase 1, Change #3 contract says "No new colors or hard-coded z-index (respect the `--z-*` scale established in the prior redesign)." Implementation of `.not-found::before` uses `background: linear-gradient(135deg, rgba(255,255,255,0.08), transparent 50%);` — a literal color value. The same `rgba(255,255,255,0.08)` pattern already appears in `.card::before` (app.css:414) and `.providers-summary::before` (app.css:1247), so the implementation matches existing file convention but deviates from the explicit plan contract.
- **Fix A ⭐ Recommended**: Document the deviation in plan.md as an addendum (the rgba value matches existing conventions; introduce a `--surface-overlay` token if you want to retire the literal later).
  - Strength: Plan stays aligned with reality; existing pattern is already proven.
  - Tradeoff: Plan becomes a moving target.
  - Confidence: HIGH.
  - Blind spot: None.
- **Fix B**: Replace with a token (e.g. extract `--surface-overlay: rgba(255,255,255,0.08)`).
  - Strength: Strict plan adherence; prepares for future variants.
  - Tradeoff: Out-of-scope tokenization work; would touch `.card::before` and `.providers-summary::before` to keep them consistent (otherwise two patterns coexist).
  - Confidence: MEDIUM.
  - Blind spot: Token name choice — affects readability.
- **Decision**: FIXED via Fix A — added plan addendum A1 documenting the rgba deviation matches existing `.card::before` and `.providers-summary::before` convention. Tokenization deferred to a future refactor.

### F2 — `TestMissingStaticAsset_ReturnsBranded404` asserts `<!DOCTYPE html>` instead of `not-found__code`

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: proxy/web/handlers_404_test.go:81
- **Detail**: The plan's Phase 2, Change #3 contract says "body contains a branded marker (`not-found__code`)". The test asserts `strings.Contains(body, "<!DOCTYPE html>")` — a valid branded marker (it confirms the response is the full branded HTML page, not the FileServer's plain-text `404 page not found`) but not the marker the plan specified. The companion assertion `!strings.Contains(body, "404 page not found")` ensures the FileServer body is gone, so the test catches the intended regression. Just a different marker than the plan listed.
- **Fix A ⭐ Recommended**: Add the `not-found__code` assertion alongside `<!DOCTYPE html>` (defense-in-depth; both catch different regressions).
  - Strength: Matches plan contract; catches regressions where the body might lose the brand CSS hook.
  - Tradeoff: One extra line.
  - Confidence: HIGH.
  - Blind spot: None.
- **Fix B**: Update plan.md to reflect the actual marker.
  - Strength: Plan matches reality.
  - Tradeoff: Loses plan-locked specificity.
  - Confidence: HIGH.
  - Blind spot: None.
- **Decision**: FIXED via Fix A — added `strings.Contains(body, "not-found__code")` assertion alongside `<!DOCTYPE html>` in `TestMissingStaticAsset_ReturnsBranded404`. Defense-in-depth; both markers now verified.

### F3 — `notFoundInterceptWriter` lacks `Unwrap()` for Go 1.20+ ResponseController chain

- **Severity**: 🔵 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern Consistency
- **Location**: proxy/web/embed.go:142-146
- **Detail**: The wrapper embeds `http.ResponseWriter` but does not implement `Unwrap() http.ResponseWriter`. `http.NewResponseController(wrapped).Flush()` / `.SetReadDeadline` walk the wrapper chain via `Unwrap()` (Go 1.20+ idiom). Current use case (`http.FileServerFS`) doesn't use `http.NewResponseController`, so this is not a live bug — but it's the recommended pattern for response-writer wrappers.
- **Fix**: Add `func (w *notFoundInterceptWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }` after the struct.
- **Decision**: FIXED — added `Unwrap()` method on `*notFoundInterceptWriter` returning the embedded `http.ResponseWriter`, so `http.NewResponseController` can reach `Flush`/`Hijack`/`SetReadDeadline` on the real writer.

### F4 — `serveStatic` header-ordering invariant is undocumented

- **Severity**: 🔵 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern Consistency
- **Location**: proxy/web/embed.go:94-98
- **Detail**: `serveStatic` sets `Cache-Control: public, max-age=300` before delegating to FileServer. The `renderNotFound` branch's `Header().Del("Cache-Control")` runs before `WriteHeader(404)` flushes headers — the order is correct. A future refactor could easily reorder and break the contract; a one-line comment would prevent that.
- **Fix**: Add a one-line comment near `serveStatic` linking the header-ordering invariant to `renderNotFound`'s `Header().Del("Cache-Control")`.
- **Decision**: FIXED — added a doc-comment on `serveStatic` documenting the Cache-Control / WriteHeader ordering invariant and linking it to `renderNotFound`'s `Header().Del("Cache-Control")`.
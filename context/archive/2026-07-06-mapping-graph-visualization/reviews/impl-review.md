<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: Breadcrumb-Chain Mapping Visualization

- **Plan**: context/changes/mapping-graph-visualization/plan.md
- **Scope**: Phases 1–5 of 5
- **Date**: 2026-07-07
- **Verdict**: NEEDS ATTENTION
- **Findings**: 0 critical, 2 warnings, 2 observations

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | PASS |
| Scope Discipline | PASS |
| Safety & Quality | WARNING |
| Architecture | PASS |
| Pattern Consistency | WARNING |
| Success Criteria | PASS |

## Findings

### F1 — Double-escaping in data-fallbacks attribute

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Safety & Quality
- **Location**: proxy/web/templates/mappings-table.html:14
- **Detail**: `data-fallbacks='{{.Fallbacks | jsonMarshal | js}}'` applies `| js` (Go's `template.JSEscapeString`) to JSON output inside an HTML attribute context. The `js` pipe escapes characters for JavaScript string literals (e.g., `'` → `\x27`), then `html/template` additionally HTML-escapes the result for the attribute. When JavaScript reads `btn.dataset.fallbacks`, it gets the HTML-unescaped value — but that value still contains JS escape sequences (`\x27` etc.), which are not valid JSON. If a provider name or model contains a single quote, `JSON.parse()` will fail. The sibling `data-name`, `data-provider`, `data-model` attributes use `| js` correctly because they are consumed in an `onclick` handler (JS context), but `data-fallbacks` is consumed via `dataset` (HTML attribute context → raw string).
- **Fix A ⭐ Recommended**: Remove `| js` from the data-fallbacks attribute
  - Strength: `html/template` already context-escapes attribute values correctly. The `jsonMarshal` output (valid JSON) will be HTML-escaped in the attribute and decoded back by the browser when reading `dataset.fallbacks`, giving `JSON.parse()` clean input. This is how `data-*` attributes are designed to work.
  - Tradeoff: Need to switch from single quotes to double quotes on the attribute (`data-fallbacks="..."`) since JSON contains double quotes, or rely on html/template's entity encoding of `"` inside single-quoted attributes.
  - Confidence: HIGH — Go html/template docs confirm context-aware escaping handles attribute values.
  - Blind spot: Haven't tested with provider names containing `'`, `"`, `<`, or `&` to confirm round-trip.
- **Fix B**: Encode as base64 and decode in JS
  - Strength: Eliminates all escaping concerns entirely.
  - Tradeoff: Adds complexity; overkill for this case.
  - Confidence: MED — works but overengineered.
  - Blind spot: None significant.
- **Decision**: PENDING

### F2 — Unbounded DOM rendering in models fragment

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Safety & Quality
- **Location**: proxy/web/templates/models-fragment.html:3-8
- **Detail**: The template renders ALL models via `{{range .Models}}` without server-side truncation, then shows a "Truncated at 1000 models" message when `len(.Models) > 1000`. The message is misleading — all models ARE rendered into the DOM. Some upstream providers (e.g., NVIDIA NIM) can return thousands of models, resulting in a heavy DOM payload for a suggestion dropdown that is visually constrained to ~130px height.
- **Fix**: Truncate the slice to 1000 entries server-side in the handler before passing to the template, making the message accurate: `if len(models) > 1000 { models = models[:1000] }`.
- **Decision**: PENDING

### F3 — Unbalanced quote in hx-confirm dialog text

- **Severity**: ⚠️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern Consistency
- **Location**: proxy/web/templates/mappings-table.html:21
- **Detail**: `hx-confirm="Delete mapping '{{.Name}}?"` opens a single quote before the name but never closes it. The confirm dialog shows: `Delete mapping 'foo?` instead of `Delete mapping 'foo'?`. The same issue exists in the sibling `providers-table.html`.
- **Fix**: Change to `hx-confirm="Delete mapping '{{.Name}}'?"`.
- **Decision**: PENDING

### F4 — Discarded cache error on inflight dedup

- **Severity**: ⚠️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Safety & Quality
- **Location**: proxy/web/handlers.go:687-701
- **Detail**: In `handleRefreshModels`, when `TryLock()` fails (fetch already in progress), the handler returns cached models but discards the error from `ModelsCache.Get`. A user refreshing during an in-flight fetch sees stale data with no indication that a refresh is in progress.
- **Fix**: Include a "refresh in progress" indicator in the fragment data when `TryLock()` fails, so the UI accurately reflects state.
- **Decision**: PENDING

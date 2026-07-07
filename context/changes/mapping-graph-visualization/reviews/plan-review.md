<!-- PLAN-REVIEW-REPORT -->
# Plan Review: Breadcrumb-Chain Mapping Visualization

- **Plan**: `context/changes/mapping-graph-visualization/plan.md`
- **Mode**: Deep
- **Date**: 2026-07-06
- **Verdict**: SOUND
- **Findings**: 1 critical, 6 warnings, 0 observations

## Verdicts

| Dimension               | Verdict  |
|-------------------------|----------|
| End-State Alignment     | PASS     |
| Lean Execution          | PASS     |
| Architectural Fitness   | PASS     |
| Blind Spots             | PASS     |
| Plan Completeness       | PASS     |

## Grounding
5/5 paths ✓, 3/3 symbols ✓, brief↔plan ✓

## Findings

### F3 — No rollback plan for Phase 1

- **Severity**: ❌ CRITICAL
- **Impact**: 🔬 HIGH — architectural stakes; think carefully before deciding
- **Dimension**: Blind Spots
- **Location**: Phase 1 — Data Layer Restructure
- **Detail**: If the `[]fallbackEntry` change breaks tests or the UI, there’s no documented way to revert to the `string` format.
- **Fix Applied**: Added rollback step to Phase 1:
  ```markdown
  - [ ] 1.4 Rollback plan documented: If Phase 1 breaks tests or UI, revert `types.go` and `handlers.go` to restore the `Fallbacks string` format.
  ```
- **Decision**: FIXED

---

### F1 — CSS class references could be more explicit

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Lean Execution
- **Location**: Phase 2 — CSS
- **Detail**: The plan introduces `.route-step--primary` and `.route-step--fallback` but does not explicitly reference existing design tokens (e.g., `var(--color-success)`).
- **Fix Applied**: Updated CSS contract to use design tokens:
  ```css
  .route-step--primary {
    border-left-color: var(--color-success);
  }
  .route-step--fallback {
    border-left-color: var(--color-warning);
  }
  ```
- **Decision**: FIXED

---

### F2 — `mappingRow.Fallbacks` restructuring may affect string utilities

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Lean Execution
- **Location**: Phase 1 — Data Layer Restructure
- **Detail**: The plan restructures `Fallbacks` from `string` to `[]fallbackEntry` but does not address how this affects string-based utilities.
- **Fix Applied**: Added `String() string` method to `fallbackEntry` and `FallbacksString() string` to `mappingRow`:
  ```go
  func (f fallbackEntry) String() string {
      return fmt.Sprintf("→ %s/%s", f.ProviderName, f.Model)
  }
  
  func (m mappingRow) FallbacksString() string {
      var parts []string
      for _, fb := range m.Fallbacks {
          parts = append(parts, fb.String())
      }
      return strings.Join(parts, ", ")
  }
  ```
  Updated Phase 1 success criteria to include:
  ```markdown
  - [ ] 1.5 `FallbacksString()` method compiles and matches old string format.
  ```
- **Decision**: FIXED

---

### F4 — No error handling for JSON parsing in `editMapping()`

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Blind Spots
- **Location**: Phase 3 — Edit Dialog Integration
- **Detail**: The plan assumes `data-fallbacks` will always be valid JSON, but malformed data could crash the dialog.
- **Fix Applied**: Added error handling to Phase 3 contract:
  ```javascript
  try {
    var fallbacks = JSON.parse(this.dataset.fallbacks);
    fallbacks.forEach(function(fb) {
      addFallbackRow(fb.provider_name, fb.model);
    });
  } catch (e) {
    console.error("Failed to parse fallbacks:", e);
  }
  ```
- **Decision**: FIXED

---

### F5 — No test coverage for mobile responsiveness

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Blind Spots
- **Location**: Phase 4 — Testing & Verification
- **Detail**: The plan does not include automated tests for the `@media (max-width: 768px)` breakpoint.
- **Fix Applied**: Added manual test step to Phase 4:
  ```markdown
  - [ ] 4.8 Mobile responsiveness verified in Chrome DevTools (iPhone SE, iPhone 12, Pixel 5).
  ```
- **Decision**: FIXED

---

### F6 — Vague "update accordingly" in Phase 3

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Completeness
- **Location**: Phase 3 — Edit Dialog Integration
- **Detail**: The plan assumes the `addFallbackRow(provider, model)` helper is compatible with the new data structure.
- **Fix Applied**: Updated Phase 3 contract to explicitly state:
  ```markdown
  The `addFallbackRow(provider, model)` helper must accept `provider_name` and `model` as arguments (or be updated to do so).
  ```
- **Decision**: FIXED

---

### F7 — No example of the JSON structure for `data-fallbacks`

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Completeness
- **Location**: Phase 3 — Edit Dialog Integration
- **Detail**: The plan does not specify the schema for `data-fallbacks`.
- **Fix Applied**: Added JSON schema example to Phase 3 contract:
  ```json
  [
    {"provider_name": "zen", "model": "claude"},
    {"provider_name": "nim", "model": "step"}
  ]
  ```
- **Decision**: FIXED
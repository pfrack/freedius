<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: Regex model→mapping matching + protected default catch-all

- **Plan**: context/changes/regex-mapping-matching/plan.md
- **Scope**: Phase 1 of 4 (all phases)
- **Date**: 2026-08-10
- **Verdict**: APPROVED
- **Findings**: [0 critical] [0 warnings] [3 observations]

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | PASS ✅ |
| Scope Discipline | PASS ✅ |
| Safety & Quality | PASS ✅ |
| Architecture | PASS ✅ |
| Pattern Consistency | PASS ✅ |
| Success Criteria | PASS ✅ |

## Findings

### F1 — MappingMatcher type exported vs. plan's unexported

- **Severity**: OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: config/config.go:46
- **Detail**: Plan specified unexported `type mappingMatcher struct { name string; re *regexp.Regexp }`. Implementation exports `MappingMatcher` with exported fields `Name`, `Re` because `Matchers()` is exported and revive/golangci-lint rejects an exported method returning an unexported type. `BuildMatchers()`/`Matchers()` are exported accordingly.
- **Fix**: No fix needed — this is the lint-compliant reading; behavior is identical to the plan.
- **Decision**: ACCEPTED

### F2 — Default catch-all appended conditionally vs. plan's unconditional append

- **Severity**: OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: config/config.go:230
- **Detail**: Plan's `buildMatchers` contract says "append a catch-all `mappingMatcher{"default", regexp.MustCompile("")}`" unconditionally. Implementation appends it only `if _, ok := c.Mappings["default"]; ok`. This prevents a 500 ("provider not registered") when no default mapping exists in the map (the scan would otherwise match every model against a nonexistent default mapping).
- **Fix**: No fix needed — the conditional is a correctness improvement over the literal plan; the load pipeline always injects default first, so runtime behavior is unchanged.
- **Decision**: ACCEPTED

### F3 — Test-adapter changes outside the plan's file list

- **Severity**: OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Scope Discipline
- **Location**: e2e/tests/mappings-status.spec.ts:30, proxy/web/handlers_write_test.go:17
- **Detail**: The always-injected `default` mapping required adapting two fixtures not named in the plan: the "No API key" e2e filter now returns 2 rows (test-chat + injected default, both keyless), and the web `testConfigYAML` fixture gained an explicit `default`→`other` mapping so the protected default no longer pins the sole provider and breaks `TestDeleteProvider`. Both are forced consequences of the feature, not scope creep.
- **Fix**: No fix needed — both adaptations keep existing behavior green under the new invariant.
- **Decision**: ACCEPTED

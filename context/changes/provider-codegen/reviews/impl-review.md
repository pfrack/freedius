<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: Provider Codegen

- **Plan**: context/changes/provider-codegen/plan.md
- **Scope**: All 3 phases
- **Date**: 2026-06-18
- **Verdict**: APPROVED
- **Findings**: 0 critical, 0 warnings, 5 observations

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | PASS — 3 minor drifts (plan oversimplifications), all functionally correct |
| Scope Discipline | PASS — no out-of-scope additions |
| Safety & Quality | PASS — no CRITICAL or WARNING findings |
| Architecture | PASS — module boundaries respected; generated code replaces hand-written cleanly |
| Pattern Consistency | PASS — follows existing conventions |
| Success Criteria | PASS — all automated checks pass, all checkboxes are [x] |

**► Overall: APPROVED**

## Findings

### F1 — Dead code in generator (addBuildTag + unused flags)

- **Severity**: ◷ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Code Quality
- **Location**: `internal/genproviders/main.go:255-263, 320-326`
- **Detail**: The generator has `-out`, `-write`, and `-build-tag` flags plus a dead `addBuildTag` function. These were used in Phase 1 (build-tag codegen) but are no longer called by any gen.go directive. `addBuildTag` is unreachable code.
- **Fix**: Remove the unused `-build-tag` flag, the `addBuildTag` function, and the `-out` flag (only `-write` is actually useful).

### F2 — sortedKnownProviders helper retained

- **Severity**: ◷ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: `config/config.go:172-179`
- **Detail**: The plan said to remove hand-written code and replace with generated equivalents. `sortedKnownProviders()` was re-added as a hand-written helper. It's correctly implemented (iterates generated `KnownProviders`) so no behavioral issue.
- **Fix**: Could be kept as-is (it's a trivial helper that uses the generated map). No action needed.

### F3 — NewDefaultRegistry signature simplified in plan

- **Severity**: ◷ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: `main.go:211`, `proxy/adapters_gen.go:43-59`
- **Detail**: The plan said `NewDefaultRegistry(logger, overrides)` but actual signature is `NewDefaultRegistry(logger, streamTimeout, verboseErrors, overrides)`. The plan was an oversimplification — the extra params are required by the adapter constructors. The dual guard in `checkRequiredEnvVars` (`APIKeyEnv != "" && slices.Contains(presets, ...)`) keeps both pre-existing and new logic, which is the correct behavior.
- **Fix**: No action needed — the actual code is correct. The plan's wording was a minor simplification.

### F4 — Template injection via provider names (theoretical)

- **Severity**: ◷ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Safety & Quality
- **Location**: `internal/genproviders/main.go:342-416`
- **Detail**: Provider names from YAML keys are injected into Go source via text/template. YAML keys are under developer control, but a key containing `"` could break the generated Go string literal.
- **Fix**: Add a guard in `loadSpec()` that rejects provider names with characters illegal in Go identifiers (e.g., regex `^[a-z][a-z0-9_]*$`).

### F5 — go build before go generate in CI

- **Severity**: ◷ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Quality
- **Location**: `.github/workflows/ci.yml:27-31`
- **Detail**: CI runs `go build` before the "Check generated files are up to date" step. If generated files are stale but compile, build passes then the diff check catches the staleness. Functionally correct but more defensive to check freshness before building.
- **Fix**: Swap the order so `go generate ./...; git diff --exit-code` runs before `go build`.

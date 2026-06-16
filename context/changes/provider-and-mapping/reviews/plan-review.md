<!-- PLAN-REVIEW-REPORT -->
# Plan Review: Provider-and-Mapping (S-02)

- **Plan**: context/changes/provider-and-mapping/plan.md
- **Mode**: Deep
- **Date**: 2026-06-16
- **Verdict**: REVISE → SOUND (after triage)
- **Findings**: 0 critical, 1 warning, 2 observations — 3 fixed

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| End-State Alignment | PASS |
| Lean Execution | PASS |
| Architectural Fitness | PASS |
| Blind Spots | PASS |
| Plan Completeness | WARNING → PASS (after F1, F3 fixes) |

## Grounding
8/8 paths ✓, 5/5 symbols ✓, brief↔plan ✓

## Findings

### F1 — Phase 1 Success Criteria promise an automated test for the eager env-var check, but no Progress step covers it

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Plan Completeness
- **Location**: Phase 1, step 1.16 vs. Phase 1 Success Criteria (Automated)
- **Detail**: The Phase 1 Automated Success Criteria explicitly lists "env-var-missing startup failure" as one of the required new test cases. Step 1.16 implements the eager env-var check in `main.go`, but the Progress section only has a manual-verification step (1.29) for it — no automated test step exists. A reader of the Progress section would not know to add an automated test; the contract from Success Criteria and the Progress section diverge. The implementer has three plausible interpretations: (a) extract the check into a testable helper in `main.go` and unit-test it; (b) add a sub-process test that invokes the binary with a missing env var; (c) skip the test and rely on the manual step. (c) silently violates the contract. The same gap exists for the per-model `api_key_env` override check.
- **Fix A ⭐ Recommended**: Extract the eager env-var check into a testable helper and add a unit test
  - Strength: Clean in-process test; runs in milliseconds; matches the S-01/S-02 plan's single-method pattern.
  - Tradeoff: Requires a small refactor of step 1.16 — the eager check becomes `func checkRequiredEnvVars(cfg *config.Config) error` that `main.go.run()` calls. ~10 lines moved.
  - Confidence: HIGH — standard Go refactor; no architectural risk.
  - Blind spot: The function still has side effects on `os.Getenv` reads, so a test must `t.Setenv` carefully.
- **Fix B**: Add a sub-process test in test-manual.sh
  - Strength: No code refactor; tests the actual binary.
  - Tradeoff: Slower; depends on shell scripting in CI; doesn't unit-test the function.
  - Confidence: HIGH — pattern is already in use in test-manual.sh.
  - Blind spot: The plan defers test-manual.sh updates to Phase 3 step 3.11. Moving some of that work into Phase 1 breaks phase ordering.
- **Decision**: FIXED (Fix A)

  Applied edits:
  - Step 1.4 (config/defaults.go): added an exported narrow accessor `func ProviderEnvVar(name string) string` so `main.go`'s eager check can look up the env-var name without exposing the whole `knownProviderDefaults` map.
  - Step 1.16 (main.go): rewrote the contract to extract the eager check into an unexported `checkRequiredEnvVars(cfg *config.Config) error` helper, called from `main.go.run()`. The helper uses `os.Getenv` (hermetic with `t.Setenv`) and returns a descriptive `error` on the first miss.
  - Progress: inserted a new automated step 1.28 with a `main_test.go` unit test covering the four branches (missing preset env var, missing per-model env var, happy path, no-default-env-var pass-through). Renumbered the manual steps 1.28-1.30 to 1.29-1.31.

### F2 — Desired End State says `provider: zen` / `provider: go` return 501, but Phase 1 step 1.15 correctly changes that to 500

- **Severity**: 🔎 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: End-State Alignment
- **Location**: plan.md, Desired End State, line 54
- **Detail**: DES bullet reads "provider: zen and provider: go still return 501". Phase 1 step 1.15 documents the intentional switch to 500 with rationale ("more informative than failing silently with 501"). The implementation is correct; the DES is the out-of-date one.
- **Fix**: Update the DES bullet to read: "provider: zen and provider: go return 500 with 'provider not registered' (S-03 adds the real adapters)."
- **Decision**: FIXED

  Applied edit:
  - plan.md line 54: changed the DES bullet to reflect the post-Phase-1 500 behavior.

### F3 — Phase 3 Success Criteria list a `models:`-wins-over-family-match regression test, but Progress step 3.5 doesn't name it explicitly

- **Severity**: 🔎 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Completeness
- **Location**: Phase 3, step 3.5 vs. Phase 3 Success Criteria (Automated)
- **Detail**: The Phase 3 Automated Success Criteria list explicitly includes "exact-match models: still wins over family match (regression test)". Step 3.5 lists four test cases for dispatcher behavior but does not name the regression test. Step 2.4 covers the literal-key precedence case; step 3.5's cases assume `models:` doesn't have the key. A test where `models: claude-opus-4-1` AND `mappings: opus:` both exist and the request goes through `models:` is the regression test for Phase 3's family-pattern lookup.
- **Fix**: Add to step 3.5: "POST with a model present in models: AND a matching family in mappings: routes through models: (Phase 2 precedence regression holds after family patterns land)."
- **Decision**: FIXED

  Applied edit:
  - Step 3.5: appended the regression test case to the existing list of dispatcher test cases.

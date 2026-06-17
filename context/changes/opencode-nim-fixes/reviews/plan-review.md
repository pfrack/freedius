<!-- PLAN-REVIEW-REPORT -->
# Plan Review: OpenCode Go 401 + NIM SSE Fixes

- **Plan**: `context/changes/opencode-nim-fixes/plan.md`
- **Mode**: Deep
- **Date**: 2026-06-17
- **Verdict**: REVISE → SOUND (after triage)
- **Findings**: 0 critical, 1 warning, 4 observations

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| End-State Alignment | PASS |
| Lean Execution | PASS |
| Architectural Fitness | PASS |
| Blind Spots | WARNING (1) |
| Plan Completeness | WARNING (1) |

## Grounding

14/14 paths ✓, all key symbols ✓, brief↔plan ✓. `docs/reference/contract-surfaces.md` does not exist (skip per skill convention).

## Findings

### F1 — Per-phase "add" wording hides test-break contract

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Completeness
- **Location**: Phase 1 §1.3 (mix_test.go) and §1.4 (custom_test.go)
- **Detail**: Testing Strategy says "Update existing tests + add header/sanitization assertions", but §1.3/§1.4 contract wording says only "add assertions for x-api-key and anthropic-version". Existing assertions at `mix_test.go:24-25` (Anthropic path) and `custom_test.go:37-38` will FAIL after Phase 1 because `Authorization` will be empty. They must be REPLACED, not supplemented. OpenAI-path assertion at `mix_test.go:55-56` and NIM assertion at `nim_test.go:24-25` stay unchanged (Bearer is correct there).
- **Fix**: Change §1.3 and §1.4 contract wording to say REPLACE not add; explicitly carve out OpenAI-path assertions.
- **Decision**: FIXED — updated §1.3 and §1.4 contracts with REPLACE wording and explicit caller line ranges

### F2 — No automated end-to-end test for NIM-specific behavior

- **Severity**: OBSERVATION
- **Impact**: 🔎 MEDIUM — worth pausing; think before deciding
- **Dimension**: Blind Spots
- **Location**: Phase 3 §3.3 + §3.6/§3.7 manual verification
- **Detail**: `TranslateRequest(NoStreamUsage: true)` tested at function level; `sanitizeNIMBody` tested at function level. The integration through `NIMAdapter` — that the inner `OpenAICompatibleAdapter` actually receives `NoStreamUsage: true` AND has the sanitization hook wired — has no automated test. Manual-only verification (§3.6/§3.7). Future refactor that drops either field on `inner` won't be caught.
- **Fix**: Add one end-to-end test in `proxy/nim_test.go`: streaming + tool with boolean schema → assert `stream_options` absent AND `additionalProperties` stripped in upstream body.
- **Decision**: FIXED — added new §3.5 entry in Phase 3 success criteria + Progress checkbox

### F3 — 25 mechanical test-caller updates not quantified

- **Severity**: OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Completeness
- **Location**: Phase 3 §3.1
- **Detail**: Phase 3 changes `TranslateRequest` signature from 2 to 3 args. Exactly 25 callers at `TranslateRequest(in, "x")` in `proxy/translate/anthropic_openai_test.go` need mechanical update. Plan acknowledges qualitatively but doesn't quantify.
- **Fix**: Note "25 mechanical TranslateRequest caller updates — single Replace-All suffices" in §3.1.
- **Decision**: FIXED — updated §3.1 contract with explicit count and Replace-All note

### F4 — Wrong test name in §1.2

- **Severity**: OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Completeness
- **Location**: Phase 1 §1.2
- **Detail**: Plan references `TestAnthropicCompatTextPassThrough`; actual name is `TestAnthropicCompat_PassthroughText` at `proxy/anthropic_compat_test.go:20`.
- **Fix**: Correct the name in §1.2.
- **Decision**: FIXED — updated §1.2 contract

### F5 — `phase2_test.go` in blast radius, not mentioned

- **Severity**: OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Completeness
- **Location**: All phases (blast-radius check)
- **Detail**: `proxy/phase2_test.go` (301 lines) constructs both adapters but is not enumerated. Inspection shows it's unaffected: no Authorization assertions, no TranslateRequest callers, no constructor signature changes.
- **Fix**: Add one-line note to Testing Strategy confirming blast-radius verification.
- **Decision**: FIXED — added blast-radius note to Testing Strategy section (Phase 2 SSE blast-radius added post-triage: 19 TestTranslateStream_* + TestMixAdapter_OpenAITranslation verified unaffected — zero `reasoning_content`/`ReasoningContent` references exist in codebase; new emit branch gated by empty-string check; emitBlockStop addition purely additive)

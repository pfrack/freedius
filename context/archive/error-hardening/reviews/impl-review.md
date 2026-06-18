<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: Error hardening + env auto-injection + config template

- **Plan**: context/changes/error-hardening/plan.md
- **Scope**: All 4 phases
- **Date**: 2026-06-18
- **Verdict**: NEEDS ATTENTION
- **Findings**: 0 critical, 5 warnings, 3 observations

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | WARNING |
| Scope Discipline | WARNING |
| Safety & Quality | WARNING |
| Architecture | PASS |
| Pattern Consistency | PASS |
| Success Criteria | PASS |

## Overall

NEEDS ATTENTION — multiple warnings, no critical failures.

## Findings

### F1 — Scope creep: 6 unplanned source changes shipped with the review commit

- **Severity**: ⚠️ WARNING
- **Impact**: 🔬 HIGH — architectural stakes; think carefully before deciding
- **Dimension**: Scope Discipline
- **Location**: N/A (multiple files)
- **Detail**: Six source-level changes shipped in the same commit that were NOT described in the plan. A: Auto-write starter config when none found (main.go:155-172). B: Rename NIM_API_KEY → NVIDIA_NIM_API_KEY (breaking). C: Default port 8080 → 8082 (breaking). D: Use go/zen providers with OPENCODE_API_KEY in starter template. E: Starter model mapping updates. F: Support system message role in Anthropic→OpenAI conversion. Items A-C are potentially breaking; items D-F are non-breaking improvements.
- **Fix A ⭐ Recommended**: Update the plan to document all six as addenda
  - Strength: Preserves the work; restores plan as ground truth.
  - Tradeoff: Plan becomes a living document.
  - Confidence: HIGH — this team already documents discovered scope through plan updates.
  - Blind spot: None significant.
- **Fix B**: Revert A-C and ship as separate change
  - Strength: Tighter scope discipline.
  - Tradeoff: Breaking changes still need to ship; separate PR needed.
  - Confidence: MEDIUM — depends whether users already rely on 8080 or NIM_API_KEY.
  - Blind spot: Haven't checked for dependent code in the repo.
- **Decision**: FIXED via Fix A

### F2 — Middleware wiring order reversed from plan; comment inaccurate

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Plan Adherence
- **Location**: main.go:197-199, proxy/proxy.go:243-246
- **Detail**: Plan specifies wiring as: recover → accessLog → requestID → dispatcher. Implementation wires as: requestID → accessLog → recover → dispatcher. The comment at proxy/proxy.go:243-246 claims "RecoverMiddleware is wired outermost", which is incorrect. The code works and is arguably better (request ID available for all downstream logging), but the plan and comment are wrong.
- **Fix A ⭐ Recommended**: Update plan to match actual wiring; fix comment
  - Strength: Preserves better implementation; fixes documentation.
  - Tradeoff: None.
  - Confidence: HIGH — one-line plan update + one-line comment fix.
  - Blind spot: None significant.
- **Fix B**: Rewire to match plan order
  - Strength: Plan-accuracy.
  - Tradeoff: Reverting a deliberate improvement.
  - Confidence: LOW — likely reverting a deliberate improvement.
  - Blind spot: Haven't checked if internal code relies on requestID being outermost.
- **Decision**: FIXED via Fix A

### F3 — Adapter constructors don't propagate user-configurable streamTimeout

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Plan Adherence
- **Location**: proxy/nim.go:20, proxy/mix.go:20
- **Detail**: Plan requires NIMAdapter and MixAdapter constructors to accept and forward streamTimeout. Both use NewOpenAICompatibleAdapter(logger) (default 5m). The --stream-timeout flag only reaches the direct "openai" adapter at main.go:190. A user who explicitly sets --stream-timeout expects all adapters to honor it.
- **Fix**: Add streamTimeout parameter to NewNIMAdapter and NewMixAdapter; pass through to inner OpenAICompatibleAdapter.
  - Strength: Matches plan intent; honors user's --stream-timeout.
  - Tradeoff: Minor — constructor signature change in two files.
  - Confidence: HIGH — simple plumbing change.
  - Blind spot: None significant.
- **Decision**: FIXED (constructor signature change + plumbed through all call sites)

### F4 — No rollback on --force backup failure (data-loss risk)

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Safety & Quality (Data Safety)
- **Location**: init.go:48-67
- **Detail**: When --force runs, existing file is renamed to .bak, then new file is written. If WriteFile fails, original is gone. No automatic recovery.
- **Fix**: After a failed WriteFile, attempt os.Rename(backup, output) to restore the original. Log success/failure of recovery.
  - Strength: Prevents data loss from partial write failure.
  - Tradeoff: Adds ~5 lines; recovery call itself could fail.
  - Confidence: HIGH — straightforward defensive pattern.
  - Blind spot: Recovery also failing is degenerate; logging backup path is sufficient.
- **Decision**: FIXED (added rollback on WriteFile failure)

### F5 — Pre-WriteHeader error forwarding relies on contract, not explicit guard

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Safety & Quality (Reliability)
- **Location**: proxy/proxy.go:126-133
- **Detail**: Plan specifies the dispatcher should track whether headers were written via a wrapper and check before calling writeErrorJSON. The implementation relies on the Adapter Return Contract instead. If an adapter violates the contract, the dispatcher will attempt a second response write.
- **Fix A ⭐ Recommended**: Add explicit wroteHeader check using existing wroteHeaderResponseWriter wrapper
  - Strength: Defensive — immune to adapter contract violations.
  - Tradeoff: Requires wrapping the ResponseWriter at dispatcher level.
  - Confidence: HIGH — wroteHeaderResponseWriter already exists.
  - Blind spot: None significant.
- **Fix B**: Document the contract dependency more prominently
  - Strength: Less code change.
  - Tradeoff: No runtime protection for contract violations.
  - Confidence: MEDIUM — relies on developer discipline.
  - Blind spot: One adapter bug loses the error response.
- **Decision**: FIXED via Fix A (explicit wroteHeader guard added)

### F6 — writeErrorJSON API differs from plan (extra r param, WithVerbose→VerboseErrors)

- **Severity**: 👁 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: proxy/proxy.go:153
- **Detail**: Plan specified writeErrorJSON(w, status, code, message, opts...) with WithVerbose(error). Implementation has writeErrorJSON(w, r *http.Request, status, code, message, opts...) with only WithDetail(string). Gating on verbose is via d.VerboseErrors. Cleaner API than planned.
- **Fix**: Update plan to match the actual API in an addendum.
- **Decision**: FIXED (plan updated to match actual API)

### F7 — failf doesn't call slog.Error as planned

- **Severity**: 👁 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: main.go:250-253
- **Detail**: Plan says failf should call slog.Error in addition to stderr. Implementation writes to stderr only. Since failf is used before logger may be initialized, the plan intent may not always be safe.
- **Fix**: Accept as-is — the plan's intent is not always safe to follow.
- **Decision**: FIXED (plan updated to reflect actual behavior)

### F8 — RequestIDMiddleware signature lacks logger param

- **Severity**: 👁 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: proxy/proxy.go:192
- **Detail**: Plan says RequestIDMiddleware(logger, next). Implementation is RequestIDMiddleware(next) — no logger needed. Cleaner API.
- **Fix**: Update plan to match actual signature.
- **Decision**: FIXED (plan updated to match actual signature)

### F9 — Hardcoded anthropic-version header

- **Severity**: 👁 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Architecture
- **Location**: proxy/anthropic_compat.go:43,49
- **Detail**: anthropic-version header hardcoded to "2023-06-01" with no override mechanism. Future-proofing gap.
- **Fix**: Add optional api_version config field (default "2023-06-01") to Model struct.
- **Decision**: FIXED (added APIVersion config field)

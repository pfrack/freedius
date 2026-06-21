<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: go-package-layout

- **Plan**: `context/changes/go-package-layout/plan.md`
- **Scope**: All phases (1-2)
- **Date**: 2026-06-21
- **Verdict**: NEEDS ATTENTION
- **Findings**: 0 critical, 2 warnings, 3 observations

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | WARNING ⚠️ |
| Scope Discipline | WARNING ⚠️ |
| Safety & Quality | PASS ✅ |
| Architecture | PASS ✅ |
| Pattern Consistency | PASS ✅ |
| Success Criteria | PASS ✅ |

## Findings

### F1 — Commit structure doesn't match plan's single-atomic-commit requirement

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — minor structural drift; no functional impact
- **Dimension**: Plan Adherence
- **Location**: N/A (commit history)

- **Detail**: Plan specified "Single atomic commit containing two logical steps" but the file moves landed in commit `554493d` ("chore(tui-themes): apply review-triage fixes (post-epilogue)") alongside unrelated TUI model/style changes. The build-command updates landed in a separate commit `a5a8d53`. This creates a two-commit split instead of one atomic refactor commit. Future `git bisect` cannot cleanly isolate the move.

- **Fix**: Accept as-is — the move is already committed and the split is cosmetically unclean but functionally harmless. The functional outcome (binary builds at `cmd/freedius/`, all tests pass) is correct.
  - Strength: No risk; move is already committed to main.
  - Tradeoff: `git bisect` users see the move in a commit that also touches TUI code.
  - Confidence: HIGH — this is a cosmetic/考古 issue, not a correctness one.
  - Blind spot: None significant.
- **Decision**: PENDING

### F2 — test-manual.sh scope creep: TTY wrapper + schema migration

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff between completeness and scope discipline
- **Dimension**: Scope Discipline
- **Location**: `test-manual.sh`

- **Detail**: Plan specified a one-line build-path swap (`.` → `./cmd/freedius`). Implementation also added: (a) `script -eq -c` TTY wrapper for Bubble Tea, (b) all ~14 YAML config snippets rewritten from old `provider`/`model`/`base_url` schema to current `provider_name`/`model_string`/`providers:` schema, (c) removal of the 4.14 "freedius listening on" log check. Both are pre-existing issues that would have broken `mage ManualTest` regardless of this change, but the plan didn't call for fixing them.

- **Fix A ⭐ Recommended**: Accept as documented drift in change.md (already documented).
  - Strength: The test now passes, which is the success criterion. Documenting drift preserves the decision rationale.
  - Tradeoff: Plan becomes a moving target; future reviewers may be confused by unannounced scope.
  - Confidence: HIGH — the drift is documented in change.md and the outcome is strictly better (test passes).
  - Blind spot: None significant.
- **Fix B**: Revert the TTY/schema fixes and leave `mage ManualTest` broken.
  - Strength: Strict scope adherence.
  - Tradeoff: Loses working test coverage; `mage ManualTest` would remain broken.
  - Confidence: LOW — this is a worse outcome.
  - Blind spot: None.
- **Decision**: PENDING

### F3 — change.md status uses "implemented" instead of "complete"

- **Severity**: ⚠️ OBSERVATION
- **Impact**: 🏃 LOW — trivial naming difference
- **Dimension**: Plan Adherence
- **Location**: `context/changes/go-package-layout/change.md:4`

- **Detail**: Phase 2 Contract says `status: complete` but the global `/10x-implement` instructions use `status: implemented`. The implementation followed the global instructions (which take precedence). Content is otherwise correct.

- **Fix**: No action needed — `implemented` is the canonical status per the skill instructions.
- **Decision**: PENDING

### F4 — SIGTERM propagation race in test-manual.sh

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — test reliability; not a production issue
- **Dimension**: Safety & Quality
- **Location**: `test-manual.sh:59`

- **Detail**: When bash receives SIGTERM, it may deliver SIGHUP to background children before the EXIT trap fires, potentially killing the `script(1)` process via SIGHUP rather than SIGTERM. The server's graceful-shutdown handler only traps SIGTERM/SIGINT. This could cause flaky `SHUTDOWN_EXIT` assertions. The current `trap cleanup EXIT` only handles normal exit, not signals.

- **Fix**: Change `trap cleanup EXIT` to `trap cleanup EXIT SIGTERM SIGINT` so the cleanup fires synchronously on signal delivery before bash exits.
  - Strength: Ensures cleanup runs even on signal; no orphan risk.
  - Tradeoff: None — this is a correctness improvement.
  - Confidence: HIGH — standard bash pattern.
  - Blind spot: None significant.
- **Decision**: PENDING

### F5 — Missing jq dependency check in test-manual.sh

- **Severity**: ⚠️ OBSERVATION
- **Impact**: 🏃 LOW — test failure clarity; not a functional issue
- **Dimension**: Safety & Quality
- **Location**: `test-manual.sh:399`

- **Detail**: `jq -r '.error.type // empty' 2>/dev/null` silently fails if `jq` is not installed, causing `ERR_TYPE` to always be empty. Error-code-diff tests would still pass (they check `STATUS`, not `ERR_TYPE`) but the `jq` extraction would silently degrade. The script has no dependency check for `jq`.

- **Fix**: Add `command -v jq >/dev/null 2>&1 || { echo "jq is required"; exit 1; }` near the top of the script.
  - Strength: Fails fast with clear error if jq is missing.
  - Tradeoff: None — this is a usability improvement.
  - Confidence: HIGH — common pattern.
  - Blind spot: None.
- **Decision**: PENDING

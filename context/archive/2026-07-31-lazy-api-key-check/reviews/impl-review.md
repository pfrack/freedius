<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: Downgrade checkRequiredEnvVars to Warning Log

- **Plan**: context/changes/lazy-api-key-check/plan.md
- **Scope**: Phase 1 of 1 (full plan)
- **Date**: 2026-08-05
- **Verdict**: APPROVED (all findings fixed during triage)
- **Findings**: 0 critical · 5 warnings · 4 observations (all FIXED)

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | PASS |
| Scope Discipline | PASS |
| Safety & Quality | WARNING |
| Architecture | PASS |
| Pattern Consistency | WARNING |
| Success Criteria | WARNING |

## Findings

### F1 — Embedded starter config still claims a fatal startup gate

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Safety & Quality
- **Location**: cmd/freedius/templates/starter.yaml:17-20
- **Detail**: Comment says "the startup check (`checkRequiredEnvVars`) requires every provider referenced by a mapping to have its key set." This is now false — missing keys only emit a warning and the process starts. The file is `//go:embed`-ed (main.go:48-49) and shipped inside the binary as the bootstrap config when no config exists, so the wrong statement reaches users directly.
- **Fix**: Edit the comment to "missing keys now log a startup warning; the request fails with `authentication_error`".
  - Strength: One-line text change; matches the real behavior described in the plan's Desired End State.
  - Tradeoff: None significant.
  - Confidence: HIGH — confirmed against the committed code path.
  - Blind spot: None significant.
- **Decision**: FIXED

### F2 — Tests never assert the WARN level

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Success Criteria
- **Location**: cmd/freedius/main_test.go:25-33 (countWarnings), 52/77/104
- **Detail**: `countWarnings` counts non-empty lines, not WARN-level lines, and no test asserts `warn["level"] == "WARN"`. The JSON handler is created with nil opts (LevelInfo), so a regression changing `logger.Warn` to `logger.Info`/`Error` leaves all 8 tests green. Emitting at WARN is the entire point of this change.
- **Fix**: Assert `level == "WARN"` in the three attribute tests and filter by level inside `countWarnings`.
  - Strength: Directly pins the change's core guarantee.
  - Tradeoff: Minor — a few lines per test.
  - Confidence: HIGH — matches the structured-logging peer pattern in this file.
  - Blind spot: None significant.
- **Decision**: FIXED

### F3 — "No early-exit / multiple warnings" behavior untested

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Success Criteria
- **Location**: cmd/freedius/main_test.go (test suite), cmd/freedius/main.go:399-411
- **Detail**: Plan.md:105 ("Multiple missing keys produce multiple warnings (no early exit)") and the commit message both call out removing the early-return, yet every fixture has exactly one key-missing provider and `_AllSet` expects 0. Re-adding a `return` after the first `logger.Warn` would not fail the suite.
- **Fix**: Add a test with two providers/mappings both missing keys, asserting `countWarnings == 2` and both env names present.
  - Strength: Guards the headline behavioral change.
  - Tradeoff: Minor — one new test case.
  - Confidence: HIGH — fixtures already exist for single-provider cases.
  - Blind spot: None significant.
- **Decision**: FIXED

### F4 — TestRun_LazyConfigDoesNotWriteFile can hang on a root container

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Safety & Quality
- **Location**: cmd/freedius/main_test.go:478-544 (esp. 534)
- **Detail**: Before this change, `checkRequiredEnvVars` failed fast and the process exited before binding. Now startup proceeds to bind, then `waitForShutdown` blocks on SIGINT. On any host where binding port 1 is permitted (root in a CI container, `CAP_NET_BIND_SERVICE`), the un-timeouted `cmd.Run()` at line 534 never returns and the package hangs to the `go test` timeout. Also the `strings.Contains(output, "freedius:")` assertion (541) silently changed meaning from "env-var check rejected startup" to "bind failed".
- **Fix**: Switch to `exec.CommandContext` with a ~5s timeout and assert on the bind error specifically; keep the file-write assertion.
  - Strength: Removes a CI hang that this change newly introduced.
  - Tradeoff: Minor — needs ctx + timeout plumbing already importable.
  - Confidence: HIGH — pre-existing `--port 1` siblings already use this shape.
  - Blind spot: Haven't checked whether the `strings.Contains` assertion should be narrowed.
- **Decision**: FIXED

### F5 — CustomNoDefault test does not exercise the empty-env guard

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Success Criteria
- **Location**: cmd/freedius/main_test.go:109-129
- **Detail**: `TestCheckRequiredEnvVars_CustomNoDefault` sets `DefaultAPIKeyEnv: "CUSTOM_KEY"` and exports it, so it only re-tests the happy path. The `p.DefaultAPIKeyEnv != ""` guard at main.go:404 (plan.md:106: "providers without `DefaultAPIKeyEnv`, e.g. ollama, produce no warning") is never exercised with an empty value.
- **Fix**: Add a provider with `DefaultAPIKeyEnv: ""` asserting 0 warnings, and rename the existing test to `_CustomKeySet`.
  - Strength: Closes a named coverage gap in the plan's Testing Strategy.
  - Tradeoff: None significant.
  - Confidence: HIGH — mirrored by the existing single-provider fixtures.
  - Blind spot: None significant.
- **Decision**: FIXED

### F6 — Warning emitted on the bare root logger (no component)

- **Severity**: 📝 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern Consistency
- **Location**: cmd/freedius/main.go:405 (vs peers at openai_compat.go:52, mix.go:40, proxy.go:92, main.go:143)
- **Detail**: Every other warning-emitting site scopes its logger with `logger.With("component", ...)`. This warning uses the bare root logger, making it the only unattributable line in the dashboard log stream.
- **Fix**: Emit via `logger.With("component", "startup").Warn(...)`.
- **Decision**: FIXED

### F7 — Function name/semantics mismatch, no doc comment

- **Severity**: 📝 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern Consistency
- **Location**: cmd/freedius/main.go:397
- **Detail**: The contract flipped from "enforce" to "observe" but the name `checkRequiredEnvVars` still reads as a gate and there is no doc comment, unlike `run`/`waitForShutdown` peers in the same file.
- **Fix**: Add a doc comment, or rename to `warnMissingAPIKeys` to reflect the new semantics.
- **Decision**: FIXED

### F8 — Per-mapping loop can emit duplicate warnings

- **Severity**: 📝 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern Consistency
- **Location**: cmd/freedius/main.go:399-411
- **Detail**: The loop is per-mapping, so the shipped starter (5 mappings all -> `nim`) emits 5 identical warnings on first run. Plan.md:28 explicitly deferred dedup, so this is within scope — flagging the UX consequence only.
- **Fix**: Optionally collect env vars in a `map[string]struct{}` and warn once per provider (or attach a `mappings` slice attr).
- **Decision**: FIXED

### F9 — Test renames / parsing brittleness

- **Severity**: 📝 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern Consistency
- **Location**: cmd/freedius/main_test.go:228, 56/81/169/245, 184-190
- **Detail**: `TestCheckRequiredEnvVars_ProviderNameInError` keeps "InError" though no error path remains (rename to `_ProviderNameInWarning`). `json.Unmarshal(buf.Bytes())` parses the whole buffer (only valid because exactly one line is emitted); the in-file peer `TestNewLogger_JSONFormat` does `strings.TrimSpace` first — folding "parse line N" into a helper would make the multi-warning test (F3) trivial.
- **Fix**: Rename the test and route parsing through a per-line helper.
- **Decision**: FIXED

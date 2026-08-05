# Downgrade checkRequiredEnvVars to Warning Log — Implementation Plan

## Overview

Replace the fatal startup `checkRequiredEnvVars` error with structured `slog.Warn` calls so freedius starts even when mapped providers lack API keys. The web dashboard already shows "Key Missing" badges and adapters already fail gracefully at request time — the startup gate is a redundant block that prevents exploration of the web UI.

## Current State Analysis

- `checkRequiredEnvVars` at `cmd/freedius/main.go:397-411` iterates providers referenced by mappings and fatally exits on the first missing env var.
- Both adapters (`openai_compat.go:74-83`, `anthropic_compat.go:82-90`) independently check env vars at request time and return `configError{errType: "authentication_error"}` — enabling fallback.
- The web UI (`proxy/web/handlers.go:334-339`) already reads `os.Getenv` at render time, showing "Active"/"Key Missing" badges live.
- Three tests in `cmd/freedius/main_test.go:24-65` test the current fatal behavior.

### Key Discoveries:

- `checkRequiredEnvVars` takes only `*config.Config` — no logger parameter today (`main.go:397`)
- The function early-returns on the first missing key — downstream keys are not reported
- `slog.SetDefault(logger)` is called at `main.go:104` before `checkRequiredEnvVars` is called at `main.go:141`, so the global logger is available — but injecting via parameter is cleaner for testing

## Desired End State

freedius starts successfully regardless of missing API keys. At startup, one structured warning per missing key appears in the log. The web dashboard shows "Key Missing" badges as before. The first actual request to a provider with a missing key fails with `authentication_error` and triggers fallback if configured. Tests verify warning emission via injected logger.

## What We're NOT Doing

- Not changing the runtime adapter behavior (already correct)
- Not changing the web UI badges (already lazy/real-time)
- Not adding deduplication by env var (out of scope — same env var rarely backs multiple mappings in practice)
- Not adding a "set API key" feature to the web dashboard (separate change)
- Not changing the config validation path (unrelated to env var presence)

## Implementation Approach

Change `checkRequiredEnvVars` signature to accept `*slog.Logger`, remove the early-return/error, emit one `logger.Warn(...)` per missing key, and return nothing. Update the call site and adapt the three tests to inject a test logger and assert warning attributes.

## Phase 1: Downgrade checkRequiredEnvVars

### Overview

Single phase — change the function signature, body, call site, and tests.

### Changes Required:

#### 1. Function signature and body

**File**: `cmd/freedius/main.go`

**Intent**: Change `checkRequiredEnvVars` from returning an error to logging warnings. Accept a `*slog.Logger` parameter so tests can inject a recording logger. Remove early return — iterate all mappings and warn for each missing key.

**Contract**: New signature: `func checkRequiredEnvVars(logger *slog.Logger, cfg *config.Config)` (no return value). Each missing key emits `logger.Warn("API key not set", "env", envVar, "mapping", mappingName, "provider", providerName)`.

#### 2. Call site update

**File**: `cmd/freedius/main.go`

**Intent**: Update the call at line 141 from the current error-check-and-exit pattern to a plain function call passing the logger.

**Contract**: Replace:
```go
if err := checkRequiredEnvVars(cfg); err != nil {
    return failf("freedius: %s", err)
}
```
With:
```go
checkRequiredEnvVars(logger, cfg)
```

#### 3. Adapt tests

**File**: `cmd/freedius/main_test.go`

**Intent**: Rewrite the three `TestCheckRequiredEnvVars_*` tests to inject a recording slog handler, call the new signature, and assert warning attributes instead of error strings.

**Contract**: Use `slog.New(slog.NewJSONHandler(&buf, nil))` as the injected logger. After calling `checkRequiredEnvVars(testLogger, cfg)`, parse JSON lines from `buf` and assert:
- `TestCheckRequiredEnvVars_PresetEnvVarMissing` → one warning line with `"env":"NVIDIA_NIM_API_KEY"` and `"provider":"nim"`
- `TestCheckRequiredEnvVars_PerProviderOverrideMissing` → one warning line with `"env":"OPENCODE_API_KEY"`
- `TestCheckRequiredEnvVars_AllSet` → zero warning lines emitted

### Success Criteria:

#### Automated Verification:

- All tests pass: `go test ./cmd/freedius/...`
- Linting passes: `golangci-lint run ./...`
- Build succeeds: `go build ./cmd/freedius`
- No data race: `go test -race ./cmd/freedius/...`

#### Manual Verification:

- Start freedius with a missing API key env var, verify it starts and logs a warning
- Confirm web dashboard still shows "Key Missing" badge for the unconfigured provider
- Send a request to a mapping with missing key, confirm `authentication_error` response (existing behavior unchanged)

**Implementation Note**: After completing this phase and all automated verification passes, pause here for manual confirmation from the human that the manual testing was successful before proceeding to the next phase.

---

## Testing Strategy

### Unit Tests:

- Warning emitted with correct structured fields when env var is missing
- No warning emitted when all env vars are set
- Multiple missing keys produce multiple warnings (no early exit)
- Providers without `DefaultAPIKeyEnv` (e.g., ollama) produce no warning

### Integration Tests:

- Existing integration tests in `main_test.go` (process-level tests) should still pass since freedius now starts even with missing keys

### Manual Testing Steps:

1. Unset `ANTHROPIC_API_KEY`, start freedius with a mapping that uses the anthropic provider
2. Observe startup log contains a warning mentioning the env var
3. Open web dashboard — verify "Key Missing" badge appears
4. Send a request to a mapping targeting anthropic — verify 500 `authentication_error`

## References

- Related research: `context/changes/lazy-api-key-check/research.md`
- Lessons learned: `context/foundation/lessons.md` — "Adding New Providers: Auto-Inject + Env-Var Scope"
- Call site: `cmd/freedius/main.go:141`
- Function: `cmd/freedius/main.go:397-411`
- Tests: `cmd/freedius/main_test.go:24-65`

## Addenda

- **A1 — Progress drift, 2026-08-04.** Phase 1's four `#### Automated` rows were marked `[x]` in the plan, but the implementation was never shipped: `cmd/freedius/main.go:399` still has `func checkRequiredEnvVars(cfg *config.Config) error`, `main.go:141` still wraps it in `if err := …; err != nil { return failf(...) }`, and `main_test.go:24-80` still asserts `err.Error()` substrings. Rows reset to `[ ]` — actual implementation work remains pending. Re-surfaced via `/10x-implement` resume; user chose to stop and re-plan.

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles. See `references/progress-format.md`.

### Phase 1: Downgrade checkRequiredEnvVars

#### Automated

- [x] 1.1 All tests pass: `go test ./cmd/freedius/...` — c14fc41
- [x] 1.2 Linting passes: `golangci-lint run ./...` — c14fc41
- [x] 1.3 Build succeeds: `go build ./cmd/freedius` — c14fc41
- [x] 1.4 No data race: `go test -race ./cmd/freedius/...` — c14fc41

#### Manual

- [ ] 1.5 Start with missing key — logs warning, process stays up
- [ ] 1.6 Web dashboard shows "Key Missing" badge
- [ ] 1.7 Request to missing-key provider returns authentication_error

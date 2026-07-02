# Quality Gates in CI — Implementation Plan

## Overview

Audit and harden the freedius CI pipeline by closing 7 high-impact gaps: workflow resilience (concurrency, timeout), tool caching, module verification, format checking, redundant gate cleanup, pre-commit parity, and documentation alignment. Defers coverage threshold, parallelization, build flags, and dep-review to follow-up changes.

## Current State Analysis

The pipeline runs `mage ci` with 6 serial gates (Vet → GenerateCheck → Test → Lint → Build → Govulncheck). It works, but has meaningful gaps:

- No `concurrency:` or `timeout-minutes:` in the workflow — superseded pushes waste runners, stuck jobs burn 360 min default.
- `~/go/bin` not cached — staticcheck, golangci-lint, govulncheck, and mage recompile from source every run (30-60s wasted).
- Tool version constants at `magefiles/mage.go:16-23` are decorative — actual `go install` calls hardcode the same literals, so bumping a constant does nothing.
- `ModVerify` and `Tidy` targets exist (`magefiles/mage.go:181-194`) but are not chained into `CI()`.
- `Vet` runs 3 times per CI: once as step 1 (`magefiles/mage.go:292`), again inside `Lint` via `mg.SerialDeps(Vet, LintStatic, LintGolangci)` (`:272`), and a third time as `govet` inside golangci-lint (`.golangci.yaml:11`).
- No format check in CI despite `AGENTS.md:25` claiming gofumpt is enforced. The actual formatter chain is gofmt → goimports → golines → gci, configured in `.golangci.yaml:43-62`.
- Pre-commit hook (`scripts/pre-commit:5`) only runs `mage lint` — no generate check, so generated-file drift passes locally and fails CI.
- `README.md:129-144` references removed Makefile targets.

## Desired End State

After this plan, `mage ci` runs 9 blocking gates with clear progress, the workflow has resilience features, tools are cached, and local/CI parity is tighter. `AGENTS.md` and `README.md` accurately describe the pipeline.

### Key Discoveries:

- `CI()` is a hand-rolled `for` loop over a steps slice (`magefiles/mage.go:287-314`), not `mg.SerialDeps` — easy to add/remove/reorder steps.
- `.golangci.yaml` v2 schema already has a `formatters:` block (`:43-62`) with gofmt, goimports, gci, golines — `golangci-lint fmt --diff` can serve as the format gate with zero new config.
- `Lint()` at `magefiles/mage.go:271-274` uses `mg.SerialDeps(Vet, LintStatic, LintGolangci)` — dropping `Vet` from here eliminates redundancy since Vet already runs as CI step 1.
- The `Tidy` target (`:181-184`) is just `go mod tidy` — the CI gate needs a `git diff --exit-code -- go.mod go.sum` follow-up to verify tidiness.

## What We're NOT Doing

- **Coverage threshold enforcement** — needs a baseline measurement first; deferred to follow-up.
- **CI parallelization** (splitting into workflow jobs or `mg.Deps` inside `CI()`) — higher complexity, deferred.
- **Reproducible static build flags** (`-trimpath`, `-ldflags "-s -w"`, `CGO_ENABLED=0`) — separate concern.
- **`actions/dependency-review-action@v4`** — needs release workflow first.
- **Integration test convention** (`//go:build integration` tags) — needs definitional pass before wiring.
- **`.go-version` file adoption** — useful but orthogonal to this change.
- **`go generate` pathspec fix** (`.go` glob may under-match subdirs) — needs verification with intentional drift test.

## Implementation Approach

Two phases: (1) workflow + magefile hardening — all CI-facing changes; (2) pre-commit + docs alignment. Each phase is independently shippable and testable.

## Phase 1: CI Pipeline Hardening

### Overview

Add workflow resilience features, cache tools, wire version constants, chain missing gates, clean up redundancy, and add format checking.

### Changes Required:

#### 1. Add concurrency and timeout to workflow

**File**: `.github/workflows/ci.yml`

**Intent**: Prevent wasted CI minutes from superseded pushes and stuck jobs.

**Contract**: Add `concurrency:` block at workflow scope (group on ref, cancel-in-progress) and `timeout-minutes: 15` on the test job.

#### 2. Cache ~/go/bin in CI

**File**: `.github/workflows/ci.yml`

**Intent**: Stop recompiling staticcheck, golangci-lint, govulncheck, and mage from source on every run.

**Contract**: Add `actions/cache@v4` step after `actions/setup-go@v5`, caching `~/go/bin`, keyed on a hash of tool version literals (matching the constants in `magefiles/mage.go:16-23`). The cache key should include the Go version to invalidate on Go upgrades.

#### 3. Wire tool-version constants into install calls

**File**: `magefiles/mage.go`

**Intent**: Make the declared constants (`toolVersionStaticcheck`, etc.) the real source of truth instead of decorative helpers.

**Contract**: Replace hardcoded version literals in `go install` calls at lines 253, 263, 279, 339, 347, 355 with the corresponding `toolVersion*` constant references. No behavioral change — just eliminates the drift risk.

#### 4. Chain ModVerify and Tidy-check into CI()

**File**: `magefiles/mage.go`

**Intent**: Gate on module integrity and tidy cleanliness — both targets already exist but aren't chained.

**Contract**: Add two steps to the `CI()` steps slice:
- `ModVerify` (existing target at `:187-194`) after `Vet`.
- A new `TidyCheck` target that runs `go mod tidy` then `git diff --exit-code -- go.mod go.sum` — fails if tidy produces any diff. Define this as a new function, not reuse `Tidy` (which just runs tidy without checking).

#### 5. Drop redundant Vet from Lint()

**File**: `magefiles/mage.go`

**Intent**: Vet already runs as CI step 1; running it again inside `Lint` wastes time and produces confusing triple-output.

**Contract**: Change `Lint()` at `:271-274` from `mg.SerialDeps(Vet, LintStatic, LintGolangci)` to `mg.SerialDeps(LintStatic, LintGolangci)`. Standalone `mage lint` still works — it just skips the redundant vet (users who want vet can run `mage vet`).

#### 6. Add format check to CI()

**File**: `magefiles/mage.go`

**Intent**: Enforce formatting in CI using the existing golangci-lint formatter config — closes the gap documented (falsely) in `AGENTS.md:25`.

**Contract**: Add a `FormatCheck` step to `CI()` that runs `golangci-lint fmt --diff`. This uses the `formatters:` block already in `.golangci.yaml:43-62` (gofmt, goimports, gci, golines). No new config needed. Place it after `TidyCheck` and before `Test` (cheap check, fails fast).

### Success Criteria:

#### Automated

- `mage lint` passes (Vet + staticcheck + golangci-lint, without redundant vet)
- `mage test` passes
- `mage build` passes
- `mage ci` runs all 9 gates and passes
- `golangci-lint fmt --diff` exits 0 (all files formatted)

#### Manual

- Run `mage ci` locally and verify progress output shows 9 steps with `[i/9]` format
- Verify `Vet` no longer appears inside `Lint` output
- Verify format check fails on an intentionally unformatted file
- Verify `go mod tidy` + `git diff --exit-code` catches an untidy go.mod

---

## Phase 2: Pre-commit Parity + Documentation Alignment

### Overview

Expand the pre-commit hook to catch generated-file drift, fix stale documentation in README.md, and correct the false gofumpt claim in AGENTS.md.

### Changes Required:

#### 1. Expand pre-commit hook

**File**: `scripts/pre-commit`

**Intent**: Close the biggest hook↔CI drift class — generated-file drift passes locally and fails CI.

**Contract**: Add `mage generateCheck` after `mage lint`. The hook now runs `mage lint && mage generateCheck`. Keep it lightweight — no test, no build, no govulncheck (too slow for hooks).

#### 2. Fix README.md Development section

**File**: `README.md`

**Intent**: Replace stale Makefile references (lines 129-144) with actual mage commands.

**Contract**: Rewrite the Development section to show: `mage test`, `mage lint`, `mage ci`, `mage format`, `mage installHooks`. Remove all `make` references.

#### 3. Fix AGENTS.md format claim

**File**: `AGENTS.md`

**Intent**: Correct the false claim at line 25 that gofumpt is enforced in CI.

**Contract**: Change line 25 from `gofumpt (stricter than gofmt) enforced in CI` to reference the actual enforced formatters: `gofmt, goimports, gci, golines (via golangci-lint) enforced in CI`.

### Success Criteria:

#### Automated

- `scripts/pre-commit` is executable and runs `mage lint && mage generateCheck` without error
- `mage lint` passes
- `mage generateCheck` passes

#### Manual

- Verify README.md Development section shows correct mage commands
- Verify AGENTS.md no longer claims gofumpt is enforced
- Run the pre-commit hook on a test commit to verify it works

---

## Testing Strategy

### Unit Tests

- No new unit tests needed — this change is about CI infrastructure, not application logic.
- Existing tests (`mage test`) must continue passing.

### Integration Tests

- `mage ci` itself is the integration test — it runs all gates end-to-end.

### Manual Testing Steps

1. Run `mage ci` locally and verify all 9 gates pass with progress output
2. Intentionally break formatting in a file → verify `mage ci` fails at FormatCheck
3. Intentionally untidy go.mod → verify `mage ci` fails at TidyCheck
4. Run `scripts/pre-commit` and verify it runs lint + generateCheck
5. Push to a branch and verify CI workflow runs with concurrency cancellation and timeout

## Performance Considerations

- Tool caching eliminates ~30-60s of recompile per CI run (staticcheck, golangci-lint, govulncheck, mage).
- Concurrency cancellation saves runner minutes on force-pushes.
- Dropping redundant Vet from Lint saves ~2-5s per CI run and per standalone `mage lint`.
- Format check via `golangci-lint fmt --diff` is fast (<5s) since it uses cached tooling.

## Migration Notes

- No data migration needed.
- Contributors with existing `~/go/bin` installations will benefit from caching immediately.
- The `TidyCheck` target will fail on any branch with an untidy go.mod — fix by running `mage tidy` before pushing.

## References

- Research: `context/changes/quality-gates-in-ci/research.md`
- Prior CI change: `context/archive/2026-07-01-mage-ci-integration/plan.md`
- Lessons: `context/foundation/lessons.md` (SSE byte-fidelity makes format/coverage gates more valuable)
- CI workflow: `.github/workflows/ci.yml:1-34`
- Magefile: `magefiles/mage.go:1-527`
- Pre-commit: `scripts/pre-commit:1-7`

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles.

### Phase 1: CI Pipeline Hardening

#### Automated

- [x] 1.1 `mage lint` passes without redundant vet — 43652db
- [x] 1.2 `mage test` passes — 43652db
- [x] 1.3 `mage build` passes — 43652db
- [x] 1.4 `mage ci` runs all 9 gates and passes — 43652db
- [x] 1.5 `golangci-lint fmt --diff` exits 0 — 43652db

#### Manual

- [ ] 1.6 `mage ci` shows 9 steps with `[i/9]` progress
- [ ] 1.7 Format check fails on intentionally unformatted file
- [ ] 1.8 TidyCheck catches untidy go.mod

### Phase 2: Pre-commit Parity + Documentation Alignment

#### Automated

- [x] 2.1 `scripts/pre-commit` runs lint + generateCheck without error
- [x] 2.2 `mage lint` passes
- [x] 2.3 `mage generateCheck` passes

#### Manual

- [ ] 2.4 README.md shows correct mage commands
- [ ] 2.5 AGENTS.md format claim is corrected
- [ ] 2.6 Pre-commit hook works on test commit

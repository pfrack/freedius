# Integrate Mage into GitHub Actions CI

## Overview

Replace raw `go` commands in `.github/workflows/ci.yml` with `mage ci` (after installing `mage` binary) to achieve parity between local and CI pipelines. Add coverage artifact upload and linting (staticcheck + golangci-lint) to CI.

## Current State Analysis

### What Exists

- **Mage fully configured**: `magefiles/mage.go` defines 18 targets including `CI()` (`magefiles/mage.go:84-87`) which runs `Vet → GenerateCheck → Test → Build`
- **CI workflow**: `.github/workflows/ci.yml` runs raw `go vet`, `go test`, `go build`, `go generate`, `govulncheck` — duplicating Mage targets
- **Pre-commit hook**: `scripts/pre-commit` runs `mage lint` locally
- **Zero-install bootstrap**: `mage.go` (root) allows `go run mage.go <target>` without installing `mage` binary

### Key Gaps

1. `Test()` uses `-cover` (terminal output) not `-coverprofile` (file output) — CI can't upload coverage
2. `CI()` doesn't include `Lint` — staticcheck/golangci-lint only run via pre-commit hook
3. `LintGolangci()` does NOT auto-install `golangci-lint` — must be installed in CI workflow
4. Go version in CI (`1.26.1`) doesn't match `go.mod` (`1.26.4`)

### Key Discoveries

- `magefiles/mage.go:84-87` — `CI()` target already exists, just needs `Lint` added
- `magefiles/mage.go:58-65` — `LintStatic()` auto-installs staticcheck if missing
- `magefiles/mage.go:68-75` — `LintGolangci()` errors if golangci-lint missing (no auto-install)
- `magefiles/mage.go:16-18` — `Test()` needs `COVERPROFILE` env var support
- `mage.go` (root) — zero-install bootstrap (not used in CI, but available for local use)
- `mg.SerialDeps` deduplicates — adding `Lint` alongside `Vet` is safe

## Desired End State

After this plan:
- CI runs `mage ci` which executes `Vet → GenerateCheck → Test → Lint → Build`
- Coverage profile is uploaded as a GitHub Actions artifact (30-day retention)
- golangci-lint is installed in CI workflow before `mage ci` runs
- Go version matches `go.mod` (`1.26.4`)
- `govulncheck` remains as a separate CI step

## What We're NOT Doing

- Not using zero-install `go run mage.go` approach — installing `mage` binary in CI for faster execution
- Not adding `govulncheck` as a Mage target — keeping it as separate CI step
- Not changing `LintGolangci()` to auto-install — keeping explicit install in CI workflow
- Not adding Codecov/Coveralls integration — just uploading artifact for now

## Implementation Approach

Two-file change: modify `magefiles/mage.go` to support coverage profiles and include Lint in CI, then update `.github/workflows/ci.yml` to install `mage` binary and use `mage ci` with the new capabilities.

## Phase 1: Magefile Updates

### Overview

Update `magefiles/mage.go` to support coverage profile output and include Lint in the CI target chain.

### Changes Required:

#### 1. Add `COVERPROFILE` env var support to `Test()`

**File**: `magefiles/mage.go`

**Intent**: Allow `Test()` to write a coverage profile file when `COVERPROFILE` env var is set. This enables CI to generate `coverage.out` for artifact upload while keeping local `mage test` unchanged.

**Contract**: `Test()` checks `os.Getenv("COVERPROFILE")`. If set, appends `-coverprofile=<value>` to the test args. If empty, behavior unchanged (terminal output only).

#### 2. Add `Lint` to `CI()` dependency chain

**File**: `magefiles/mage.go`

**Intent**: Include linting in the CI pipeline so all PRs are linted, not just commits with pre-commit hooks.

**Contract**: Change `CI()` from `mg.SerialDeps(Vet, GenerateCheck, Test, Build)` to `mg.SerialDeps(Vet, GenerateCheck, Test, Lint, Build)`. `mg.SerialDeps` deduplicates `Vet` (called by both `CI` and `Lint`), so it runs only once.

### Success Criteria:

#### Automated Verification:

- `mage test` still works without `COVERPROFILE` set
- `COVERPROFILE=coverage.out mage test` produces `coverage.out` file
- `mage ci` runs Vet, GenerateCheck, Test, Lint, Build in order
- `go vet ./...` passes on modified file

#### Manual Verification:

- `mage -l` shows updated CI description mentioning lint
- Local `mage ci` completes successfully

---

## Phase 2: CI Workflow Update

### Overview

Replace raw `go` commands in `.github/workflows/ci.yml` with `mage ci` (after installing `mage` binary), add golangci-lint install, and add coverage artifact upload.

### Changes Required:

#### 1. Install `mage` binary and replace raw commands

**File**: `.github/workflows/ci.yml`

**Intent**: Install `mage` via `go install` and use `mage ci` instead of individual `go vet`, `go test`, `go build`, `go generate` commands. This ensures CI uses the same build logic as local development.

**Contract**: Add `go install github.com/magefile/mage@v1.17.2` step, then `mage ci` with `COVERPROFILE=coverage.out` env var set.

#### 2. Add golangci-lint install step

**File**: `.github/workflows/ci.yml`

**Intent**: Install `golangci-lint` before running `mage ci` since `LintGolangci()` does not auto-install it.

**Contract**: Use `golangci/golangci-lint-action@v6` or manual install via `go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest`.

#### 3. Add coverage artifact upload step

**File**: `.github/workflows/ci.yml`

**Intent**: Upload `coverage.out` as a GitHub Actions artifact for PR review and future Codecov integration.

**Contract**: Use `actions/upload-artifact@v4` with `name: coverage-report`, `path: coverage.out`, `if-no-files-found: warn`, `retention-days: 30`.

#### 4. Fix Go version to match `go.mod`

**File**: `.github/workflows/ci.yml`

**Intent**: Update `go-version` from `1.26.1` to `1.26.4` to match `go.mod`.

### Success Criteria:

#### Automated Verification:

- `mage ci` completes successfully locally
- `coverage.out` file is generated after `mage ci`
- `go vet ./...` passes on modified file
- YAML is valid (no syntax errors)

#### Manual Verification:

- Push to branch and verify GitHub Actions workflow passes
- Coverage artifact appears in workflow run
- Linting runs as part of CI (check workflow logs)

---

## Testing Strategy

### Unit Tests:

- No new unit tests needed — this is CI/build configuration, not application logic

### Integration Tests:

- Run `mage ci` locally to verify the full pipeline
- Verify `coverage.out` is generated when `COVERPROFILE` is set

### Manual Testing Steps:

1. Run `mage test` without `COVERPROFILE` — should work as before
2. Run `COVERPROFILE=coverage.out mage test` — should produce `coverage.out`
3. Run `mage ci` — should run Vet, GenerateCheck, Test, Lint, Build
4. Push to a branch and verify GitHub Actions workflow passes
5. Check that coverage artifact appears in the workflow run

## References

- Related research: `context/changes/mage-ci-integration/research.md`
- `magefiles/mage.go:84-87` — current `CI()` target
- `magefiles/mage.go:16-18` — current `Test()` target
- `magefiles/mage.go:77-81` — current `Lint()` target
- `.github/workflows/ci.yml` — current CI workflow

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles.

### Phase 1: Magefile Updates

#### Automated

- [x] 1.1 `mage test` still works without `COVERPROFILE` set — a350612
- [x] 1.2 `COVERPROFILE=coverage.out mage test` produces `coverage.out` file — a350612
- [x] 1.3 `mage ci` runs Vet, GenerateCheck, Test, Lint, Build in order — a350612
- [x] 1.4 `go vet ./...` passes on modified file — a350612

#### Manual

- [ ] 1.5 `mage -l` shows updated CI description mentioning lint
- [x] 1.6 Local `mage ci` completes successfully — fa33e98

### Phase 2: CI Workflow Update

#### Automated

- [x] 2.1 `mage ci` completes successfully locally — 3dbc44a
- [x] 2.2 `coverage.out` is generated when `COVERPROFILE` is set — 3dbc44a

#### Manual

- [ ] 2.3 Push to a branch and verify GitHub Actions workflow passes
- [ ] 2.4 Check that coverage artifact appears in the workflow run
- [x] 2.5 Verify linting runs as part of CI (check workflow logs) — fa33e98

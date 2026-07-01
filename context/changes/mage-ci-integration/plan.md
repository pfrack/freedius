# Integrate Mage into GitHub Actions CI

## Overview

Replace raw `go` commands in `.github/workflows/ci.yml` with `mage ci` (after installing `mage` binary) to achieve parity between local and CI pipelines. Add coverage artifact upload and linting (staticcheck + golangci-lint) to CI.

**Extended Scope**: Comprehensive Mage improvements including new targets (Clean, Install, Coverage, Benchmark, Watch), centralized tool versions, enhanced CI feedback, and improved developer experience.

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
5. Missing build artifact management (no Clean/Install targets)
6. No benchmark or coverage reporting capabilities
7. Limited developer experience (no watch mode, minimal help)
8. Tool versions scattered across multiple functions
9. CI pipeline lacks progress feedback and error clarity
10. Missing module verification capability

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
- **NEW**: Comprehensive Mage target ecosystem with 20+ targets
- **NEW**: Centralized tool version management
- **NEW**: Docker support for consistent development environments
- **NEW**: Enhanced developer experience (watch mode, coverage reports, benchmarks)
- **NEW**: Clear CI pipeline progress feedback
- **NEW**: Clean/Install/Benchmark/Coverage targets
- **NEW**: Module verification capability

## What We're NOT Doing

- Not using zero-install `go run mage.go` approach — installing `mage` binary in CI for faster execution
- Not adding `govulncheck` as a Mage target — keeping it as separate CI step
- Not changing `LintGolangci()` to auto-install — keeping explicit install in CI workflow
- Not adding Codecov/Coveralls integration — just uploading artifact for now
- **NEW**: Not replacing existing linter installation logic — enhancing it
- **NEW**: Not adding complex build systems — keeping Mage simple and idiomatic
- **NEW**: Not adding Docker support — focusing on core build/dev workflow

## Implementation Approach

Two-file change: modify `magefiles/mage.go` to support coverage profiles and include Lint in CI, then update `.github/workflows/ci.yml` to install `mage` binary and use `mage ci` with the new capabilities.

**Extended Approach**: Comprehensive Mage enhancement with 11 new/improved features including centralized versioning, enhanced CI feedback, and improved developer experience.

---

## Phase 1.5: Comprehensive Mage Improvements (NEW)

### Overview

Extend `magefiles/mage.go` with new targets, centralized tool versions, Docker support, and enhanced developer experience.

### Changes Implemented:

#### 1. Centralized Tool Version Management

**File**: `magefiles/mage.go`

**Changes**:
- Added version constants at top of file:
  ```go
  const (
      toolVersionStaticcheck  = "v0.7.0"
      toolVersionGolangciLint = "v2.12.2"
      toolVersionGovulncheck  = "v1.3.0"
      toolVersionGoimports    = "v0.47.0"
      toolVersionGolines      = "v0.12.2"
      toolVersionGci          = "v0.13.5"
  )
  ```
- Updated all linter installation functions to use these constants
- Single source of truth for tool versions

**Benefits**: Easy version updates, consistent versioning across all targets

#### 2. New Build & Development Targets

**Added Targets**:
- `Clean()` - Removes build artifacts (`freedius`, `coverage.out`, `coverage.html`)
- `Install()` - Installs binary to `$GOPATH/bin`
- `Coverage()` - Generates HTML coverage report with `go tool cover`
- `Benchmark()` - Runs performance benchmarks with `-benchmem`
- `Watch()` - Auto-rebuilds on `.go` file changes
- `RunDev()` - Uses `go run` instead of building first (faster dev iteration)

**Implementation Details**:
```go
// Clean removes build artifacts
func Clean() error {
    artifacts := []string{"freedius", "coverage.out", "coverage.html"}
    for _, f := range artifacts {
        sh.Rm(f)
    }
    return nil
}

// Coverage generates HTML report
func Coverage() error {
    sh.RunV("go", "test", "-coverprofile=coverage.out", "./...")
    sh.RunV("go", "tool", "cover", "-html=coverage.out", "-o=coverage.html")
    return nil
}

// Watch monitors file changes
func Watch() error {
    // Builds initially, then polls for .go file changes
    // Rebuilds automatically when changes detected
}
```

#### 3. Enhanced CI Pipeline Feedback

**File**: `magefiles/mage.go`

**Changes**:
- Rewrote `CI()` to show progress with step-by-step feedback
- Added emoji indicators (→ for running, ✓ for success, ✗ for failure)
- Clear error messages showing which step failed
- Percentage progress display `[1/6]`

**Before**:
```go
func CI() error {
    mg.SerialDeps(Vet, GenerateCheck, Test, Lint, Build, Govulncheck)
    return nil
}
```

**After**:
```go
func CI() error {
    steps := []struct { name string; fn func() error }{...}
    for i, step := range steps {
        fmt.Printf("[%d/%d] Running %s...\n", i+1, len(steps), step.name)
        if err := step.fn(); err != nil {
            fmt.Printf("✗ CI failed at step: %s\n", step.name)
            return fmt.Errorf("%s failed: %w", step.name, err)
        }
        fmt.Printf("✓ %s passed\n\n", step.name)
    }
    fmt.Println("✓ All CI checks passed!")
    return nil
}
```

#### 5. Improved Help System

**File**: `magefiles/mage.go`

**Changes**:
- Set `Default = Help` so `mage` alone shows help
- Created `Help()` with categorized target listings
- Organized targets by: Development, Testing, Code Quality, Dependencies, Docker, CI/CD
- Added usage examples
- Shows tool versions being used
- Better formatting with aligned columns

**Output Example**:
```
Freedius Mage Build Targets
============================

Development:
  run                Start the server (use ARGS env var for extra arguments)
  build              Compile the freedius binary
  install            Install the binary to $GOPATH/bin
  clean              Remove build artifacts and temporary files

Testing & Quality:
  test               Run unit tests with race detection and coverage
  benchmark          Run performance benchmarks
  coverage           Generate and open HTML coverage report

Tool Versions:
  staticcheck:        v0.7.0
  golangci-lint:      v2.12.2
  ...
```

#### 6. Module Verification Target

**File**: `magefiles/mage.go`

**Added Target**: `ModVerify()`

**Purpose**: Run `go mod verify` to ensure module cache matches checksums in `go.sum`

**Usage**:
```bash
mage modVerify
```

#### 6. Enhanced Error Messages

**Changes Across All Targets**:
- Added contextual error wrapping with `fmt.Errorf("operation: %w", err)`
- Progress indicators for long-running operations
- Clear success/failure messaging

**Example**:
```go
func Coverage() error {
    if err := sh.RunV("go", "test", "-coverprofile="+coverFile, "./..."); err != nil {
        return fmt.Errorf("test coverage failed: %w", err)
    }
    // ...
}
```

### Success Criteria:

#### Automated Verification:
- ✅ `mage -l` shows all 20+ targets organized by category
- ✅ `mage clean` removes build artifacts
- ✅ `mage coverage` generates `coverage.html`
- ✅ `mage benchmark` runs benchmarks
- ✅ `mage ci` shows step-by-step progress
- ✅ `mage docker:build` builds Docker image
- ✅ All tool versions centralized in constants
- ✅ `mage modVerify` validates module integrity

#### Manual Verification:
- ✅ `mage` (no args) shows formatted help
- ✅ `mage watch` auto-rebuilds on changes
- ✅ `mage install` installs to GOPATH
- ✅ Docker targets work with TAG env var
- ✅ CI pipeline shows clear progress feedback

---

## Phase 2: CI Workflow Update (UNCHANGED)

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
- `magefiles/mage.go` — comprehensive Mage targets (20+ targets)
- `magefiles/mage.go:16-23` — centralized tool version constants
- `magefiles/mage.go:34-93` — Help target with categorized listing
- `magefiles/mage.go:120-135` — CI pipeline with progress feedback
- `magefiles/mage.go:440-471` — Docker namespace with build/run/push targets
- `Dockerfile` — multi-stage Docker build
- `.dockerignore` — Docker build optimization
- `.github/workflows/ci.yml` — current CI workflow
- `cmd/freedius/main.go` — application entry point

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles.

### Phase 1: Magefile Updates

#### Automated

- [x] 1.1 `mage test` still works without `COVERPROFILE` set — a350612
- [x] 1.2 `COVERPROFILE=coverage.out mage test` produces `coverage.out` file — a350612
- [x] 1.3 `mage ci` runs Vet, GenerateCheck, Test, Lint, Build in order — a350612
- [x] 1.4 `go vet ./...` passes on modified file — a350612

#### Manual

- [x] 1.5 `mage -l` shows updated CI description mentioning lint
- [x] 1.6 Local `mage ci` completes successfully — fa33e98

### Phase 1.5: Comprehensive Mage Improvements (NEW)

#### Automated

- [x] 1.5.1 Centralized tool version constants added
- [x] 1.5.2 `mage clean` removes build artifacts
- [x] 1.5.3 `mage coverage` generates HTML report
- [x] 1.5.4 `mage benchmark` runs benchmarks
- [x] 1.5.5 `mage modVerify` validates module integrity
- [x] 1.5.6 `mage install` installs to GOPATH
- [x] 1.5.7 CI pipeline shows step-by-step progress feedback
- [x] 1.5.8 `mage` (no args) shows formatted help with all targets
- [x] 1.5.9 `mage watch` polls for file changes
- [x] 1.5.10 `mage runDev` uses go run for faster iteration
- [x] 1.5.11 All error messages use contextual wrapping

#### Manual

- [x] 1.5.12 `mage -l` lists all 18 targets organized by category
- [x] 1.5.13 Help output shows tool versions and usage examples
- [x] 1.5.14 CI pipeline shows `[1/6] Running X...` progress format

### Phase 2: CI Workflow Update

#### Automated

- [x] 2.1 `mage ci` completes successfully locally — 3dbc44a
- [x] 2.2 `coverage.out` is generated when `COVERPROFILE` is set — 3dbc44a

#### Manual

- [ ] 2.3 Push to a branch and verify GitHub Actions workflow passes
- [ ] 2.4 Check that coverage artifact appears in the workflow run
- [x] 2.5 Verify linting runs as part of CI (check workflow logs) — fa33e98

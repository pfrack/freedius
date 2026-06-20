# Magefile Migration Implementation Plan

## Overview

Replace the Makefile with Mage — a Go-based build tool. Targets live in `magefiles/` (its own Go module), with a zero-install `mage.go` bootstrap at the repo root. All existing targets port 1:1 to Go functions.

## Current State Analysis

- `Makefile:1-81` — 18 targets, all thin wrappers around `go` commands and linters
- `scripts/pre-commit:1-7` — calls `make lint`
- `go.mod` — production deps (bubbletea, lipgloss, go-yaml, tiktoken-go); `magefiles/go.mod` isolates the build dependency
- `generate-check` already exists as a Makefile target and is part of the `ci` pipeline
- The `ci` target runs `vet generate-check test build` (not lint); the CI workflow at `.github/workflows/ci.yml` runs raw `go` commands

## Desired End State

After this plan is complete:

1. `mage ci` (or `go run mage.go ci`) replaces `make ci` as the CI gate
2. `mage -l` lists all targets with doc comments as help text
3. The Makefile is deleted
4. `magefiles/` has its own `go.mod` — the main module stays production-deps-only
5. The pre-commit hook calls `mage lint`
6. No Makefile remains anywhere in the repository

Verification: `mage ci` passes. `mage -l` shows all targets.

### Key Discoveries:

- The `generate-check` target already exists in the Makefile — it is NOT a future S-05 concern; it must be ported
- The `ci` target composes `vet generate-check test build` — NOT `lint test build`; the lint targets are standalone
- The empty-package guards (`if [ -n "$(PKGS)" ]`) are unnecessary in Go 1.26 — `./...` handles it
- Mage's `mg.SerialDeps` replaces Make's dependency ordering for composite targets
- The `magefiles/` directory with separate `go.mod` keeps mage out of the production dependency tree
- The zero-install bootstrap (`go run mage.go <target>`) means no global `mage` binary required
- Format targets (`format`, `format-changed`) are shell-intensive — use `sh.RunV` with Go `filepath.Walk` or pass shell commands directly

## What We're NOT Doing

- Adding new build targets beyond what the Makefile currently has
- Changing CI workflow files (`.github/workflows/ci.yml`) — already uses raw `go` commands, no Makefile dependency
- Changing any runtime behavior of the application

## Implementation Approach

Two phases: first create the magefiles alongside the existing Makefile (verify equivalence), then delete the Makefile and update all references.

## Phase 1: Magefiles + bootstrap

### Overview

Create the `magefiles/` directory with its own Go module, port all Makefile targets to Go functions, and add the zero-install root bootstrap.

### Changes Required:

#### 1. Magefiles module

**File**: `magefiles/go.mod`

**Intent**: Isolate the mage dependency from the production module so `go.mod` stays clean.

**Contract**: Module `github.com/pfrack/freedius/magefiles`, requires `github.com/magefile/mage v1.16.0`, Go version matches main module.

#### 2. Mage targets

**File**: `magefiles/mage.go`

**Intent**: Port all 18 Makefile targets to Go functions with doc comments (which become `mage -l` help text).

**Contract**: `//go:build mage` tag, `package main`. Exported functions:

| Function | Equivalent Make target | Notes |
|---|---|---|
| `Test` | `test` | `go test -race -cover ./...` |
| `Vet` | `vet` | `go vet ./...` |
| `Build` | `build` | `go build -o freedius .` |
| `GenerateCheck` | `generate-check` | `go generate ./... && git diff --exit-code -- '*.go'` |
| `Tidy` | `tidy` | `go mod tidy` |
| `Run` | `run` | `go run . $(ARGS)` — read `ARGS` env var for passthrough |
| `Verbose` | `verbose` | `go run . --verbose-errors` |
| `LintStatic` | `lint-static` | Auto-install staticcheck via `exec.LookPath` + `go install` |
| `LintGolangci` | `lint-golangci` | Warn + exit if golangci-lint not found |
| `Lint` | `lint` | `mg.SerialDeps(Vet, LintStatic, LintGolangci)` |
| `CI` | `ci` | `mg.SerialDeps(Vet, GenerateCheck, Test, Build)` — matches actual Makefile composition |
| `ManualTest` | `manual-test` | `sh.RunV("./test-manual.sh")` |
| `InstallHooks` | `install-hooks` | Copy `scripts/pre-commit` to `.git/hooks/pre-commit`, chmod +x |
| `InstallGoimports` | `install-goimports` | Auto-install via `go install golang.org/x/tools/cmd/goimports@latest` |
| `InstallGolines` | `install-golines` | Auto-install via `go install github.com/segmentio/golines@latest` |
| `InstallGci` | `install-gci` | Auto-install via `go install github.com/daixiang0/gci@latest` |
| `Format` | `format` | Walk all `.go` files, run `gofmt` + `goimports` + `golines` + `gci` (see **Critical Detail** below) |
| `FormatChanged` | `format-changed` | Same formatting pipeline, only on files changed vs HEAD + untracked |

**Critical Detail — Format targets**: The formatters (`gofmt`, `goimports`, `golines`, `gci`) must run per-file with the `-w` flag. Since Mage targets are Go functions, use `filepath.Walk` and `os/exec` to pipe each file through the toolchain. The `GCI_SECTIONS` variable (`--skip-generated -s standard -s default -s "prefix(github.com/pfrack/freedius)" -s blank -s dot -s alias -s localmodule`) must be passed to `gci write`. `Format` calls `InstallGoimports`/`InstallGolines`/`InstallGci` first (via `mg.Deps`) then formats all files. `FormatChanged` uses `git diff --name-only` + `git ls-files --others --exclude-standard` to find Go files, then formats only those.

`LintStatic` auto-installs staticcheck if missing (using `exec.LookPath` check). `LintGolangci` warns if golangci-lint is missing and exits (does NOT auto-install). `InstallHooks` copies `scripts/pre-commit` to `.git/hooks/pre-commit` and chmods it.

#### 3. Zero-install bootstrap

**File**: `mage.go`

**Intent**: Allow `go run mage.go <target>` without installing the `mage` binary globally.

**Contract**: `//go:build ignore` tag, `package main`, imports `github.com/magefile/mage/mage`, calls `os.Exit(mage.Main())` in `main()`.

### Success Criteria:

#### Automated Verification:

- `mage ci` passes (equivalent to `make ci`)
- `mage -l` lists all 18 targets with descriptions
- `go run mage.go ci` works (zero-install path)
- `mage build` produces the `freedius` binary
- `mage generate-check` passes
- `mage run` starts the server (cancel immediately)
- `mage tidy` runs `go mod tidy` cleanly
- `mage install-hooks` installs the pre-commit hook

#### Manual Verification:

- `mage -l` output is readable and covers all 18 targets
- `mage format` formats all Go files without error
- `mage format-changed` formats only changed Go files
- `mage verbose` starts the server with `--verbose-errors`

**Implementation Note**: After completing this phase and all automated verification passes, proceed to Phase 2.

---

## Phase 2: Cutover

### Overview

Delete the Makefile and update the pre-commit hook to use Mage.

### Changes Required:

#### 1. Delete Makefile

**File**: `Makefile` (delete)

**Intent**: Remove the now-redundant Makefile.

**Contract**: File no longer exists.

#### 2. Update pre-commit hook

**File**: `scripts/pre-commit`

**Intent**: Replace `make lint` with `mage lint`.

**Contract**: The hook runs `mage lint` (or `go run mage.go lint` for environments without mage installed). Keep the same error-on-failure behavior.

#### 3. (No update needed — no downstream plans reference the Makefile)

No stale `make` references exist in other change plans. If a `context/changes/provider-codegen/` plan is created in the future, it should target Mage directly.

### Success Criteria:

#### Automated Verification:

- `mage ci` passes
- `ls Makefile` returns "No such file"
- `scripts/pre-commit` contains `mage lint`
- `make` returns "command not found" (no fallback to Makefile)

#### Manual Verification:

- `git commit` triggers the pre-commit hook and runs `mage lint` successfully
- `mage -l` still works (bootstrap not broken by Makefile deletion)

---

## Testing Strategy

### Unit Tests:

- None needed — Mage targets are thin wrappers around `go` commands and external tools

### Integration Tests:

- `mage ci` is the integration test — it runs vet + generate-check + test + build
- All existing Go tests pass unchanged (they don't reference the build system)
- `mage format` and `mage format-changed` should be tested by modifying a Go file, running the target, and verifying the file is reformatted

### Manual Testing Steps:

1. Run `mage -l` and verify all 18 targets are listed with descriptions
2. Run `mage ci` and verify it passes
3. Run `go run mage.go ci` (zero-install) and verify it passes
4. Make a commit and verify the pre-commit hook fires `mage lint`
5. Run `mage run` (with and without `ARGS` env var) and verify the server starts
6. Run `mage verbose` and verify `--verbose-errors` is active
7. Run `mage format` and verify Go files are reformatted
8. Verify `mage generate-check` passes

## Performance Considerations

None — Mage compiles targets on first run (cached thereafter). Cold start adds ~1s; subsequent runs are equivalent to `make`.

## References

- Research: `context/changes/magefile/research.md`
- Mage docs: https://magefile.org/
- Current Makefile: `Makefile:1-81`
- CI workflow: `.github/workflows/ci.yml`

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles. See `references/progress-format.md`.

### Phase 1: Magefiles + bootstrap

#### Automated

- [x] 1.1 `mage ci` passes (vet + generate-check + test + build) — f1aabd1
- [x] 1.2 `mage -l` lists all 18 targets with descriptions — f1aabd1
- [x] 1.3 `go run mage.go ci` works (zero-install path) — f1aabd1
- [x] 1.4 `mage build` produces the `freedius` binary — f1aabd1
- [x] 1.5 `mage generateCheck` passes — f1aabd1
- [x] 1.6 `mage run` starts the server — f1aabd1
- [x] 1.7 `mage tidy` runs cleanly — f1aabd1
- [x] 1.8 `mage installHooks` installs the pre-commit hook — f1aabd1

#### Manual

- [x] 1.9 `mage -l` output is readable and covers all 18 targets
- [x] 1.10 `mage format` formats all Go files without error
- [x] 1.11 `mage format-changed` formats only changed Go files
- [x] 1.12 `mage verbose` starts the server with `--verbose-errors`

### Phase 2: Cutover

#### Automated

- [x] 2.1 `mage ci` passes — d361805
- [x] 2.2 Makefile no longer exists — d361805
- [x] 2.3 `scripts/pre-commit` contains `mage lint` — d361805
- [x] 2.4 `make` fails (no Makefile) — d361805

#### Manual

- [x] 2.5 Pre-commit hook fires `mage lint` on commit — d361805
- [x] 2.6 `mage -l` still works after Makefile deletion — d361805

# Magefile Migration Implementation Plan

## Overview

Replace the Makefile with Mage — a Go-based build tool. Targets live in `magefiles/` (its own Go module), with a zero-install `mage.go` bootstrap at the repo root. All existing targets port 1:1 to Go functions.

## Current State Analysis

- `Makefile:1-36` — 12 targets, all thin wrappers around `go` commands and linters
- `scripts/pre-commit:1-6` — calls `make lint`
- `go.mod` — single dependency (`go-yaml`); production-only
- Provider-codegen plan (`context/changes/provider-codegen/plan.md`) Phase 3 references adding a `generate-check` Makefile target — that plan will be updated to target Mage directly since this lands first

## Desired End State

After this plan is complete:

1. `mage ci` (or `go run mage.go ci`) replaces `make ci` as the CI gate
2. `mage -l` lists all targets with doc comments as help text
3. The Makefile is deleted
4. `magefiles/` has its own `go.mod` — the main module stays production-deps-only
5. The pre-commit hook calls `mage lint`
6. Provider-codegen (S-05) plan references are updated from `make` to `mage`

Verification: `mage ci` passes. `mage -l` shows all targets.

### Key Discoveries:

- The empty-package guards (`if [ -n "$(PKGS)" ]`) are unnecessary in Go 1.26 — `./...` handles it
- Mage's `mg.SerialDeps` replaces Make's dependency ordering for composite targets
- The `magefiles/` directory with separate `go.mod` keeps mage out of the production dependency tree
- The zero-install bootstrap (`go run mage.go <target>`) means no global `mage` binary required

## What We're NOT Doing

- Adding new build targets beyond what the Makefile currently has
- Setting up CI workflow files (GitHub Actions) — that's a separate concern
- Adding the `GenerateCheck` target — that belongs to provider-codegen (S-05)
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

**Intent**: Port all 12 Makefile targets to Go functions with doc comments (which become `mage -l` help text).

**Contract**: `//go:build mage` tag, `package main`. Exported functions: `Test`, `Vet`, `Build`, `Tidy`, `Run`, `LintStatic`, `LintGolangci`, `Lint`, `CI`, `ManualTest`, `InstallHooks`. `Lint` uses `mg.SerialDeps(Vet, LintStatic, LintGolangci)`. `CI` uses `mg.SerialDeps(Lint, Test, Build)`. `Run` reads `os.Args` after `--` for passthrough. `LintStatic` auto-installs staticcheck if missing (using `exec.LookPath` check). `InstallHooks` copies `scripts/pre-commit` to `.git/hooks/pre-commit` and chmods it.

#### 3. Zero-install bootstrap

**File**: `mage.go`

**Intent**: Allow `go run mage.go <target>` without installing the `mage` binary globally.

**Contract**: `//go:build ignore` tag, `package main`, imports `github.com/magefile/mage/mage`, calls `os.Exit(mage.Main())` in `main()`.

### Success Criteria:

#### Automated Verification:

- `mage ci` passes (equivalent to `make ci`)
- `mage -l` lists all targets with descriptions
- `go run mage.go ci` works (zero-install path)
- `mage build` produces the `freedius` binary

#### Manual Verification:

- `mage -l` output is readable and covers all targets

**Implementation Note**: After completing this phase and all automated verification passes, proceed to Phase 2.

---

## Phase 2: Cutover

### Overview

Delete the Makefile, update the pre-commit hook and provider-codegen plan references to use Mage.

### Changes Required:

#### 1. Delete Makefile

**File**: `Makefile` (delete)

**Intent**: Remove the now-redundant Makefile.

**Contract**: File no longer exists.

#### 2. Update pre-commit hook

**File**: `scripts/pre-commit`

**Intent**: Replace `make lint` with `mage lint`.

**Contract**: The hook runs `mage lint` (or `go run mage.go lint` for environments without mage installed). Keep the same error-on-failure behavior.

#### 3. Update provider-codegen plan references

**File**: `context/changes/provider-codegen/plan.md`

**Intent**: Replace `make ci`, `make generate-check`, and Makefile references with Mage equivalents so S-05 implementation targets Mage directly.

**Contract**: All occurrences of `make ci` become `mage ci`. The Phase 3 `generate-check` Makefile target description becomes a `GenerateCheck` Mage target description. The `ci` target composition reference updates accordingly.

### Success Criteria:

#### Automated Verification:

- `mage ci` passes
- `ls Makefile` returns "No such file"
- `scripts/pre-commit` contains `mage lint`
- `grep -r "make ci" context/changes/provider-codegen/plan.md` returns no matches

#### Manual Verification:

- `git commit` triggers the pre-commit hook and runs `mage lint` successfully

---

## Testing Strategy

### Unit Tests:

- None needed — Mage targets are thin wrappers around `go` commands

### Integration Tests:

- `mage ci` is the integration test — it runs lint + test + build
- All existing Go tests pass unchanged (they don't reference the build system)

### Manual Testing Steps:

1. Run `mage -l` and verify all targets are listed
2. Run `mage ci` and verify it passes
3. Run `go run mage.go ci` (zero-install) and verify it passes
4. Make a commit and verify the pre-commit hook fires `mage lint`
5. Run `mage run -- --port 9090` and verify args pass through

## Performance Considerations

None — Mage compiles targets on first run (cached thereafter). Cold start adds ~1s; subsequent runs are equivalent to `make`.

## References

- Research: `context/changes/magefile/research.md`
- Mage docs: https://magefile.org/
- Provider-codegen plan: `context/changes/provider-codegen/plan.md`
- Current Makefile: `Makefile:1-36`

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles. See `references/progress-format.md`.

### Phase 1: Magefiles + bootstrap

#### Automated

- [ ] 1.1 `mage ci` passes
- [ ] 1.2 `mage -l` lists all targets with descriptions
- [ ] 1.3 `go run mage.go ci` works (zero-install path)
- [ ] 1.4 `mage build` produces the `freedius` binary

#### Manual

- [ ] 1.5 `mage -l` output is readable and covers all targets

### Phase 2: Cutover

#### Automated

- [ ] 2.1 `mage ci` passes
- [ ] 2.2 Makefile no longer exists
- [ ] 2.3 `scripts/pre-commit` contains `mage lint`
- [ ] 2.4 No `make ci` references in provider-codegen plan

#### Manual

- [ ] 2.5 Pre-commit hook fires `mage lint` on commit

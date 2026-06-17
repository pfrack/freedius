---
date: 2026-06-17T21:11:52+02:00
researcher: kiro
git_commit: 5025c6765f70c40c220820bd085228df527f2d81
branch: opencode-nim-fixes
repository: freedius
topic: "Makefile to Magefile migration"
tags: [research, codebase, build-system, mage]
status: complete
last_updated: 2026-06-17
last_updated_by: kiro
---

# Research: Makefile to Magefile Migration

**Date**: 2026-06-17T21:11:52+02:00
**Researcher**: kiro
**Git Commit**: 5025c67
**Branch**: opencode-nim-fixes
**Repository**: freedius

## Research Question

What would it take to replace the current Makefile with Mage (a Go-based build tool), and what does the equivalent magefile look like?

## Summary

The current Makefile has 12 targets totaling 36 lines of shell. Mage is a natural fit: the project is pure Go, has no non-Go build steps, and the current targets map 1:1 to simple Go functions using `sh.Run`. The migration is straightforward — a single `magefiles/mage.go` replaces the Makefile entirely. The zero-install option (`go run mage.go`) means no new binary dependency for contributors.

## Detailed Findings

### Current Makefile Targets

| Target | Command(s) | Notes |
|--------|-----------|-------|
| `test` | `go test -race -cover ./...` | Guards against empty package list |
| `vet` | `go vet ./...` | Guards against empty package list |
| `build` | `go build -o freedius .` | — |
| `tidy` | `go mod tidy` | — |
| `run` | `go run . $(ARGS)` | Passes through args |
| `lint-static` | `staticcheck ./...` | Auto-installs if missing |
| `lint-golangci` | `golangci-lint run ./...` | Requires pre-installed |
| `lint` | `vet` + `lint-static` + `lint-golangci` | Composite |
| `ci` | `lint` + `test` + `build` | Composite — the main gate |
| `manual-test` | `./test-manual.sh` | — |
| `install-hooks` | Copies `scripts/pre-commit` | — |

### Mage Equivalents

Every target maps to a plain Go function:

```go
//go:build mage

package main

import (
    "github.com/magefile/mage/mg"
    "github.com/magefile/mage/sh"
)

// Test runs unit tests with race detection and coverage.
func Test() error {
    return sh.RunV("go", "test", "-race", "-cover", "./...")
}

// Vet runs go vet.
func Vet() error {
    return sh.RunV("go", "vet", "./...")
}

// Build compiles the binary.
func Build() error {
    return sh.RunV("go", "build", "-o", "freedius", ".")
}

// Tidy runs go mod tidy.
func Tidy() error {
    return sh.RunV("go", "mod", "tidy")
}

// Run starts the server, passing through extra args via ARGS env var.
func Run() error {
    return sh.RunV("go", "run", ".")
}

// LintStatic runs staticcheck, installing it if missing.
func LintStatic() error {
    if _, err := exec.LookPath("staticcheck"); err != nil {
        if err := sh.RunV("go", "install", "honnef.co/go/tools/cmd/staticcheck@latest"); err != nil {
            return err
        }
    }
    return sh.RunV("staticcheck", "./...")
}

// LintGolangci runs golangci-lint.
func LintGolangci() error {
    return sh.RunV("golangci-lint", "run", "./...")
}

// Lint runs all linters (vet + staticcheck + golangci-lint).
func Lint() error {
    mg.SerialDeps(Vet, LintStatic, LintGolangci)
    return nil
}

// CI runs the full CI pipeline: lint + test + build.
func CI() error {
    mg.SerialDeps(Lint, Test, Build)
    return nil
}

// ManualTest runs the manual test script.
func ManualTest() error {
    return sh.RunV("./test-manual.sh")
}

// InstallHooks copies the pre-commit hook into .git/hooks/.
func InstallHooks() error {
    return sh.Copy(".git/hooks/pre-commit", "scripts/pre-commit")
}
```

### Mage Key Features Relevant to This Project

1. **Zero-install option** — a `mage.go` file at repo root allows `go run mage.go <target>` without installing the `mage` binary. Uses the `//go:build ignore` tag.

2. **`magefiles/` directory** — Mage supports putting targets in a `magefiles/` subdirectory. If present, `mage` auto-discovers it. Keeps root clean.

3. **Parallel dependencies** — `mg.Deps(A, B)` runs A and B in parallel. `mg.SerialDeps(A, B)` runs them serially. Useful for `CI` which needs ordering.

4. **Helper packages**:
   - `sh` — shell command execution (`sh.Run`, `sh.RunV` for verbose, `sh.Output` for capturing)
   - `mg` — dependency management (`mg.Deps`, `mg.SerialDeps`)
   - `target` — file timestamp comparison (not needed here)

5. **Comments as help text** — function doc comments become `mage -l` output automatically.

6. **No external dependency beyond Go** — Mage compiles magefiles on the fly using the Go toolchain.

### Integration Considerations

| Concern | Current (Makefile) | Mage equivalent |
|---------|-------------------|-----------------|
| CI invocation | `make ci` | `mage ci` or `go run mage.go ci` |
| pre-commit hook | `make lint` | `mage lint` — update `scripts/pre-commit` |
| Provider-codegen plan references `make ci` | Multiple references in plans | Update to `mage ci` |
| ARGS passthrough for `run` | `make run ARGS="--port 9090"` | Use env var or Mage flag arguments |
| Empty package guard | Shell `if [ -n "$(PKGS)" ]` | Unnecessary — `go test ./...` handles it natively in Go 1.26 |

### Directory Structure Options

**Option A: `magefiles/` directory (recommended)**
```
magefiles/
  mage.go      # //go:build mage — targets live here
mage.go        # //go:build ignore — zero-install bootstrap
```

**Option B: Single `magefile.go` at root**
```
magefile.go    # //go:build mage — all targets
mage.go        # //go:build ignore — zero-install bootstrap
```

Option A is cleaner for projects that may grow more build logic (e.g., `generate-check` from the provider-codegen plan).

### Provider-Codegen Plan Impact

The `provider-codegen` plan (`context/changes/provider-codegen/plan.md`) Phase 3 adds a `generate-check` target to the Makefile:
```makefile
generate-check:
    go generate ./...
    git diff --exit-code -- '*.go'
```

In Mage:
```go
// GenerateCheck ensures generated files are up to date.
func GenerateCheck() error {
    if err := sh.RunV("go", "generate", "./..."); err != nil {
        return err
    }
    return sh.RunV("git", "diff", "--exit-code", "--", "*.go")
}
```

And `CI` would become:
```go
func CI() error {
    mg.SerialDeps(Lint, GenerateCheck, Test, Build)
    return nil
}
```

### Dependency Addition

Mage requires adding to `go.mod`:
```
require github.com/magefile/mage v1.16.0
```

This is a development-only dependency. Since `magefiles/` uses `//go:build mage`, it won't be included in the production binary. However, it will appear in `go.mod` — some teams put magefiles in a separate module (`magefiles/go.mod`) to isolate the dependency.

**Separate module approach:**
```
magefiles/
  go.mod       # module github.com/pfrack/freedius/magefiles
  mage.go
```

This keeps the main `go.mod` clean but adds module management overhead. For a small project, adding mage to the main `go.mod` is simpler.

## Code References

- `Makefile:1-36` — Current build targets
- `scripts/pre-commit:1-6` — Git hook that calls `make lint`
- `context/changes/provider-codegen/plan.md:207-210` — Planned `generate-check` target
- `go.mod:1-5` — Current dependencies (only `go-yaml`)

## Architecture Insights

- The project has no non-Go build steps — everything is `go test`, `go build`, `go vet`, or external Go-based linters
- The empty-package guards in the Makefile are legacy — Go 1.26 `./...` handles empty package sets gracefully
- The `ARGS` passthrough pattern is the only slightly tricky bit — Mage handles this via function arguments or env vars
- The pre-commit hook would need updating from `make lint` to `mage lint`

## Historical Context (from prior changes)

- `context/changes/provider-codegen/plan.md` — Phase 3 adds a `generate-check` Makefile target and modifies `ci` to include it. If Mage migration lands first, provider-codegen should target Mage directly. If provider-codegen lands first, the Mage migration absorbs the `generate-check` target.

## Open Questions

1. **Separate module for magefiles?** — Keep `github.com/magefile/mage` out of the main `go.mod` by using `magefiles/go.mod`? Adds complexity but keeps production deps clean.
2. **Ordering with provider-codegen** — Should this land before or after S-05? If before, provider-codegen's Phase 3 targets Mage directly. If after, this migration absorbs the Makefile changes from provider-codegen.
3. **`ARGS` passthrough** — Current `make run ARGS="--port 9090"` pattern. Mage alternative: `mage run --port 9090` using Mage's flag argument feature, or read `ARGS` env var.

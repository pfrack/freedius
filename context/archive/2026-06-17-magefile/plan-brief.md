# Magefile Migration — Plan Brief

> Full plan: `context/changes/magefile/plan.md`
> Research: `context/changes/magefile/research.md`

## What & Why

Replace the Makefile with Mage — a Go-based build tool where targets are plain Go functions. Motivation: the project is pure Go, and Mage eliminates bash/make syntax in favor of type-checked, cross-platform Go code that contributors already know.

## Starting Point

An 81-line Makefile with 18 targets (thin wrappers around `go test`, `go build`, `go vet`, linters, format tools, and `generate-check`). A pre-commit hook that calls `make lint`. The `ci` target runs `vet + generate-check + test + build` (not lint).

## Desired End State

`mage ci` replaces `make ci` as the build gate. All targets are Go functions with auto-generated help text. The mage dependency is isolated in `magefiles/go.mod` (production module untouched). Zero-install via `go run mage.go <target>` — no global binary required.

## Key Decisions Made

| Decision | Choice | Why (1 sentence) | Source |
|---|---|---|---|---|
| Directory layout | `magefiles/` + root bootstrap | Keeps root clean, scales for future targets. | Research |
| Module isolation | Separate `magefiles/go.mod` | Keeps build toolchain out of production module. | Plan |
| Target count | All 18 Makefile targets ported 1:1 | Prevents regression; format/install/generate-check targets are actively used. | Plan (review) |
| CI composition | `Vet + GenerateCheck + Test + Build` | Matches actual Makefile `ci: vet generate-check test build` — does NOT include lint. | Plan (review) |
| Args passthrough for `run` | Read `ARGS` env var | Preserves backward compatibility with existing usage (`ARGS="--port 9090"`). | Plan |
| Format targets | Go `filepath.Walk` + `os/exec` per file | Shell-intensive pipeline in Makefile; Go code handles it with proper error reporting. | Plan |

## Scope

**In scope:**
- Port all 18 Makefile targets to Mage functions
- `magefiles/go.mod` (isolated module)
- Root `mage.go` zero-install bootstrap
- Delete Makefile
- Update pre-commit hook

**Out of scope:**
- Adding new targets beyond what the Makefile already has
- Changing `.github/workflows/ci.yml` — already uses raw `go` commands
- Runtime application changes

## Architecture / Approach

```
magefiles/
  go.mod       # isolated module with mage dependency
  mage.go      # //go:build mage — 18 targets as exported Go funcs
mage.go        # //go:build ignore — zero-install bootstrap
```

Composite targets (`Lint`, `CI`) use `mg.SerialDeps()` for ordered execution. `CI` = `Vet + GenerateCheck + Test + Build` (matches Makefile). Format targets use Go `filepath.Walk` + `os/exec` per file. Each function returns `error` and uses `sh.RunV()` for command execution.

## Phases at a Glance

| Phase | What it delivers | Key risk |
|---|---|---|---|
| 1. Magefiles + bootstrap | All 18 targets working alongside existing Makefile | Format targets are shell-intensive in Go; filepath.Walk correctness |
| 2. Cutover | Makefile deleted, pre-commit hook updated | Something breaks and no Makefile fallback |

**Prerequisites:** None — this is independent of all other changes.
**Estimated effort:** ~1 session.

## Open Risks & Assumptions

- Assumes `mage` v1.16.0 is compatible with Go 1.26.4
- The separate module approach requires `go mod tidy` in `magefiles/` separately
- No downstream plans reference the Makefile (dead `provider-codegen` reference removed)

## Success Criteria (Summary)

- `mage ci` passes identically to `make ci` (vet + generate-check + test + build)
- `mage -l` shows all 18 targets with help text
- No Makefile exists; pre-commit hook uses `mage lint`
- `mage format` and `mage format-changed` reformat Go files correctly

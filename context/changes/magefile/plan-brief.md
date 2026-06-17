# Magefile Migration — Plan Brief

> Full plan: `context/changes/magefile/plan.md`
> Research: `context/changes/magefile/research.md`

## What & Why

Replace the Makefile with Mage — a Go-based build tool where targets are plain Go functions. Motivation: the project is pure Go, and Mage eliminates bash/make syntax in favor of type-checked, cross-platform Go code that contributors already know.

## Starting Point

A 36-line Makefile with 12 targets (all thin wrappers around `go test`, `go build`, `go vet`, and linters). One dependency in `go.mod` (`go-yaml`). A pre-commit hook that calls `make lint`.

## Desired End State

`mage ci` replaces `make ci` as the build gate. All targets are Go functions with auto-generated help text. The mage dependency is isolated in `magefiles/go.mod` (production module untouched). Zero-install via `go run mage.go <target>` — no global binary required.

## Key Decisions Made

| Decision | Choice | Why (1 sentence) | Source |
|---|---|---|---|
| Directory layout | `magefiles/` + root bootstrap | Keeps root clean, scales for future targets (e.g. GenerateCheck). | Research |
| Module isolation | Separate `magefiles/go.mod` | Main `go.mod` stays production-only (currently just go-yaml). | Plan |
| Sequencing vs S-05 | Land Mage first | Avoids creating a throwaway Makefile target in provider-codegen. | Plan |
| Args passthrough for `run` | `os.Args` after `--` | Familiar UX (`mage run -- --port 9090`), no env var needed. | Plan |

## Scope

**In scope:**
- Port all 12 Makefile targets to Mage functions
- `magefiles/go.mod` (isolated module)
- Root `mage.go` zero-install bootstrap
- Delete Makefile
- Update pre-commit hook
- Update provider-codegen plan references

**Out of scope:**
- Adding new targets (GenerateCheck belongs to S-05)
- CI workflow files (GitHub Actions)
- Runtime application changes

## Architecture / Approach

```
magefiles/
  go.mod       # isolated module with mage dependency
  mage.go      # //go:build mage — all targets as exported Go funcs
mage.go        # //go:build ignore — zero-install bootstrap
```

Composite targets (`Lint`, `CI`) use `mg.SerialDeps()` for ordered execution. Each function returns `error` and uses `sh.RunV()` for command execution.

## Phases at a Glance

| Phase | What it delivers | Key risk |
|---|---|---|
| 1. Magefiles + bootstrap | All targets working alongside existing Makefile | Mage dep version compatibility |
| 2. Cutover | Makefile deleted, all references updated | Missed reference somewhere |

**Prerequisites:** None — this is independent of all other changes.
**Estimated effort:** ~1 session, single phase each.

## Open Risks & Assumptions

- Assumes `mage` v1.16.0 is compatible with Go 1.26.4
- The separate module approach requires `go mod tidy` in `magefiles/` separately

## Success Criteria (Summary)

- `mage ci` passes identically to `make ci`
- `mage -l` shows all targets with help text
- No Makefile exists; pre-commit hook uses `mage lint`

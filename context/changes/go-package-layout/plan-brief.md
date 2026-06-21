# Move main package to `cmd/freedius/` — Plan Brief

> Full plan: `context/changes/go-package-layout/plan.md`
> Research: `context/changes/go-package-layout/research.md`

## What & Why

Move `package main` from the repository root into `cmd/freedius/main.go` to align with the Go community convention used by the stdlib, Kubernetes, Moby, and Zalando skipper. The research doc (`research.md`) showed every major Go project places executables under `cmd/<binary>/`; today `freedius` is the lone exception. The prior rejection of `cmd/` at `context/archive/error-hardening/research.md:261` was about **adding a second binary**, not about the `cmd/` convention per se — this change keeps one binary, just at the conventional path.

## Starting Point

The repo currently has `package main` at the root ([`main.go`](https://github.com/pfrack/freedius/blob/d361805f91845be2e59a9b253f46e5b1b37c634d/main.go), 378 lines), an internal `main_test.go` (468 lines, `package main`), and an embedded `templates/starter.yaml`. Build commands at `magefiles/mage.go:27,45,54`, `test-manual.sh:42`, `README.md:11`, and `AGENTS.md:7-8` all point at `.` (the root). All CI gates use `./...` globs and already work regardless of where `package main` lives.

## Desired End State

After this plan lands, `package main` lives at `cmd/freedius/main.go` with `cmd/freedius/main_test.go` and `cmd/freedius/templates/starter.yaml` next to it. `go build -o freedius ./cmd/freedius` produces the same binary that `go build -o freedius .` did before (the embedded `starter.yaml` byte sequence is bit-identical — both files move together and the `//go:embed` path is resolved relative to the source file). All existing CI gates pass unchanged.

## Key Decisions Made

| Decision | Choice | Why (1 sentence) | Source |
| --- | --- | --- | --- |
| Scope: include `cmd/genproviders/` too? | No — defer to a follow-up | Keeps this diff small and reviewable; the `package main` under `internal/` is anomalous but legal. | Plan |
| Commit strategy | One commit | The full relocation is one atomic change; `git bisect` would never need to isolate the moves from the command updates. | Plan |
| CI workflow glob scopes | Leave `./...` unchanged | Globs already work and catch any new packages added later; tightening is a "principle of least surprise" change that's not worth bundling here. | Plan |
| Embed path handling | No content change needed | `//go:embed templates/starter.yaml` is resolved relative to the source file's directory; moving both `main.go` and `templates/starter.yaml` together keeps the relative path identical. | Plan |
| Tooling shims (`mage.go`, `tools.go` at root) | Stay at root | Standard Go practice: build-tool shims at the module root, application binaries under `cmd/`. | Research |
| Module path | Unchanged | No external consumers exist; only the location of `package main` shifts within the module. | Research |

## Scope

**In scope:**
- `git mv` of `main.go`, `main_test.go`, `templates/starter.yaml` into `cmd/freedius/`
- Single-line edits in 5 files to update build/run commands (`.` → `./cmd/freedius`)
- One-line edit in `AGENTS.md`'s project-structure list
- Removal of the now-empty `templates/` directory at the repo root
- One commit titled `refactor: move main package to cmd/freedius/`

**Out of scope:**
- Moving `internal/genproviders/` → `cmd/genproviders/` (follow-up change)
- Moving `proxy/translate/` or `proxy/tui/` under `internal/proxy/` (follow-up change)
- Changing the module path
- Changing CI workflow glob scopes
- Adding new tests, fixtures, or test data
- Changing `mage.go` or `tools.go` at the repo root

## Architecture / Approach

Pure file relocation with a single atomic commit. The order of operations within the commit is:

1. Create `cmd/freedius/templates/` and `git mv` three files into it (`main.go`, `main_test.go`, `templates/starter.yaml`).
2. Remove the now-empty `templates/` directory at the repo root.
3. Edit 5 build/run command references: 3 in `magefiles/mage.go` (Build/Run/Verbose Mage targets), 1 in `test-manual.sh`, 1 in `README.md`, and 2 in `AGENTS.md` (build commands + project-structure entry).

No logic changes, no API surface changes, no generated code regeneration. The `//go:embed templates/starter.yaml` byte sequence embedded into the binary is bit-identical before and after the move.

## Phases at a Glance

| Phase | What it delivers | Key risk |
| --- | --- | --- |
| 1. Relocate + update commands | Working build via `cmd/freedius/`, all CI gates green | Forgetting one of the 5 build-command references (mitigation: Phase 2 verification hits `mage Build`, `mage CI`, `mage ManualTest`) |
| 2. Closeout | `change.md` captures the commit SHA + deferred follow-ups | Drift between plan and reality if a file needed editing beyond what was anticipated |

**Prerequisites:** Working `mage` toolchain (`mage -l` lists targets); CI green on `main` before starting (so the diff is a clean relocation).
**Estimated effort:** ~30 minutes. Single commit, ~10 file edits, no logic to verify.

## Open Risks & Assumptions

- **Risk: stale references in code comments.** `internal/genproviders/main.go:8-9` has doc comments referencing `./internal/genproviders`. Those stay valid because the generator isn't being moved in this change. **Mitigation:** add a note to the deferred follow-up.
- **Assumption: `git mv` preserves history for `git blame`.** Verified pattern in the repo (other files have been moved this way); no reason to expect different behavior here.
- **Risk: an unknown script or CI step outside the audited paths still uses `.` as a build target.** Audited: `magefiles/mage.go`, `test-manual.sh`, `README.md`, `AGENTS.md`, `.github/workflows/ci.yml`, `providers.yaml`. None others. `mage Build` + `mage CI` + `mage ManualTest` in Phase 1 catch any omission by failing fast.

## Success Criteria (Summary)

1. `go build -o freedius ./cmd/freedius` produces a working binary that runs the proxy + TUI with the same embedded `starter.yaml` as before.
2. `mage CI` and `mage ManualTest` both exit 0.
3. `git log --follow cmd/freedius/main.go` traces history through the rename — history is preserved.

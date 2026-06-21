# Move main package to `cmd/freedius/` — Implementation Plan

## Overview

Mechanical relocation of the application entry point from the repository root into `cmd/freedius/main.go`, aligning with the Go community convention used by the stdlib (`src/cmd/go`), Kubernetes (`cmd/kubelet`), Moby (`cmd/dockerd`), and Zalando skipper (`cmd/skipper`). The change keeps a single binary and does not touch any logic, package surface, or generated code. Module path `github.com/pfrack/freedius` is unchanged.

## Current State Analysis

- The repo has a `package main` at the root ([`main.go:1`](https://github.com/pfrack/freedius/blob/d361805f91845be2e59a9b253f46e5b1b37c634d/main.go#L1), 378 lines) plus its sibling test (`main_test.go`, 468 lines, `package main`).
- `templates/starter.yaml` is embedded into the binary via `//go:embed templates/starter.yaml` ([`main.go:46`](https://github.com/pfrack/freedius/blob/d361805f91845be2e59a9b253f46e5b1b37c634d/main.go#L46)) — referenced **only** by `main.go:46` anywhere in the tree (verified by grep).
- Build commands point at the root: `magefiles/mage.go:27,45,54` (Mage `Build`/`Run`/`Verbose`), `test-manual.sh:42`, `README.md:11`, `AGENTS.md:7-8`.
- All CI gates use `./...` globs (`.github/workflows/ci.yml` and Mage targets `Test`/`Vet`/`GenerateCheck`), so they already work regardless of where `package main` lives.
- The prior rejection of a `cmd/` layout at [`context/archive/error-hardening/research.md:261`](https://github.com/pfrack/freedius/blob/d361805f91845be2e59a9b253f46e5b1b37c634d/context/archive/error-hardening/research.md#L261) was specifically about **adding a second binary** (`cmd/freedius-init/`), not about the `cmd/` convention per se. This change keeps one binary at the conventional path.
- `.golangci.yaml` and `.github/workflows/ci.yml` contain no path-specific config that would break after a directory rename (`gci` uses the module prefix; CI uses `./...`).

## Desired End State

After this plan lands:

1. `package main` lives at `cmd/freedius/main.go` (with `cmd/freedius/main_test.go` and `cmd/freedius/templates/starter.yaml` next to it).
2. `go build -o freedius ./cmd/freedius` produces a working binary that runs the proxy + TUI and embeds the same starter template.
3. All existing CI gates pass unchanged (`go vet ./...`, `go test -race -cover ./...`, `go build ./...`, `go generate ./... && git diff --exit-code`, `govulncheck ./...`).
4. Mage targets `Build`, `Run`, `Verbose`, `CI`, and `ManualTest` all succeed.
5. README and AGENTS.md document the new build command and path.

### Key Discoveries:

- `main_test.go` is `package main` (internal test), not `package main_test` — it moves cleanly with `main.go` to `cmd/freedius/`. Source: [`main_test.go:1`](https://github.com/pfrack/freedius/blob/d361805f91845be2e59a9b253f46e5b1b37c634d/main_test.go#L1).
- `templates/starter.yaml` is referenced **only** by the embed directive at `main.go:46`; no other code or test depends on its location. The embed path `./templates/starter.yaml` is resolved relative to the source file, so the relative path stays the same after the move and the embedded content is unchanged.
- `internal/genproviders/` references `./internal/genproviders` only in doc comments (`internal/genproviders/main.go:8-9`). Those references remain valid as long as we don't move the generator, which is out of scope for this change.
- `mage.go` (root, `//go:build ignore`) and `tools.go` (root, `//go:build tools`) are tooling shims, not user-facing binaries — they stay at the root per Go convention (build tools at the module root; application binaries under `cmd/`).
- `providers.yaml:5` references `go generate ./...` — that glob still triggers the `//go:generate` directives in `config/gen.go:1` and `proxy/gen.go:1` regardless of where the generator lives, so no edit is needed there either.

## What We're NOT Doing

- **Not moving `internal/genproviders/` to `cmd/genproviders/`.** Deferred to a follow-up change. The `package main` under `internal/` is anomalous but legal, and bundling the two moves doubles the diff without a corresponding reduction in risk.
- **Not moving `proxy/translate/` or `proxy/tui/` under `internal/proxy/`.** Research flagged this as a "tighten the public surface" cleanup. Out of scope here.
- **Not changing the module path** (`github.com/pfrack/freedius`). No external consumers; path stays the same.
- **Not changing any CI workflow glob scopes.** `./...` already works after the move and catches any new packages added later.
- **Not adding new tests, fixtures, or test data.** The existing 468-line `main_test.go` covers the same surface from the new location.
- **Not changing `mage.go` (root) or `tools.go` (root).** These are build-tool shims with their own build tags; they belong at the module root.

## Implementation Approach

Single atomic commit containing two logical steps applied in this order:

1. **Relocate files** using `git mv` (preserves rename history for `git blame` / `git log --follow`). Three files: `main.go`, `main_test.go`, `templates/starter.yaml`. **No content change** to any of them — the embed path `./templates/starter.yaml` is identical relative to the source file before and after.
2. **Update build/run commands** in 5 locations: `magefiles/mage.go` (3 Mage targets), `test-manual.sh`, `README.md`, `AGENTS.md`. Each edit is a single token swap from `.` to `./cmd/freedius` (plus one path rename in `AGENTS.md`'s project-structure list).

Then verify by running the full CI gate suite plus a manual smoke test.

## Critical Implementation Details

- **`git mv` matters.** Use `git mv <from> <to>` rather than delete-and-create. This preserves rename detection in `git log --follow` and `git blame`, so future archaeology on `main.go` still works.
- **Embed path invariance.** The line `//go:embed templates/starter.yaml` at `main.go:46` does **not** need editing after the move — Go resolves the embed path relative to the source file's directory, and both the source file and the embedded file move together into `cmd/freedius/`. The compiled binary's embedded byte sequence is bit-identical before and after.
- **Commit hygiene.** One commit titled `refactor: move main package to cmd/freedius/` per the project's conventional-commit convention. Do not split into "moves" + "command updates" — that would create an intermediate broken state in `git bisect`.
- **No new dependencies, no `go.mod` change.** Module path is unchanged; only on-disk layout changes.

## Phase 1: Relocate main package and update build commands

### Overview

Move `main.go`, `main_test.go`, and `templates/starter.yaml` into `cmd/freedius/` (preserving history with `git mv`), then update all build/run command references that currently point at `.` (the root) to point at `./cmd/freedius` instead. Land as a single commit.

### Changes Required:

#### 1. Create `cmd/freedius/` and move files

**Intent**: Use `git mv` to relocate the application entry point and its dependencies into `cmd/freedius/`. No content changes inside the moved files.

**Contract**:
- `mkdir -p cmd/freedius/templates` (creates both `cmd/freedius/` and the `templates/` subdirectory).
- `git mv main.go cmd/freedius/main.go`
- `git mv main_test.go cmd/freedius/main_test.go`
- `git mv templates/starter.yaml cmd/freedius/templates/starter.yaml`
- The `templates/` directory at the repo root becomes empty after the move; remove it (`rmdir templates/`) — verify it's empty first.

After this step, `go build -o freedius ./cmd/freedius` produces a working binary; `go build -o freedius .` no longer compiles (this is expected and is what Phase 2 of Mage targets fixes).

#### 2. Update Mage build/run targets

**File**: [`magefiles/mage.go`](https://github.com/pfrack/freedius/blob/d361805f91845be2e59a9b253f46e5b1b37c634d/magefiles/mage.go)

**Intent**: Three Mage targets currently invoke the binary at the repo root (`.`). Update them to point at the new `cmd/freedius/` path. Other Mage targets (`Test`, `Vet`, `GenerateCheck`, `Tidy`, `LintStatic`, `LintGolangci`, `Format`, `FormatChanged`) use `./...` globs and need no change.

**Contract**: Three single-line edits, all the same pattern — swap the trailing `.` for `./cmd/freedius`:
- Line 27 (`Build`): `sh.RunV("go", "build", "-o", "freedius", ".")` → `sh.RunV("go", "build", "-o", "freedius", "./cmd/freedius")`
- Line 45 (`Run`): `args := []string{"run", "."}` → `args := []string{"run", "./cmd/freedius"}`
- Line 54 (`Verbose`): `sh.RunV("go", "run", ".", "--verbose-errors")` → `sh.RunV("go", "run", "./cmd/freedius", "--verbose-errors")`

#### 3. Update manual test script

**File**: [`test-manual.sh:42`](https://github.com/pfrack/freedius/blob/d361805f91845be2e59a9b253f46e5b1b37c634d/test-manual.sh#L42)

**Intent**: The manual integration test script builds the binary from the repo root. Update the build path to the new location.

**Contract**: One-line edit. The current line is `if ! go build -o "$BIN" .; then`; change `.` to `./cmd/freedius`.

#### 4. Update README build instructions

**File**: [`README.md:11`](https://github.com/pfrack/freedius/blob/d361805f91845be2e59a9b253f46e5b1b37c634d/README.md#L11)

**Intent**: The README's build command example still points at the root. Update to match the new path.

**Contract**: One-line edit. Replace the `go build -o freedius .` example with `go build -o freedius ./cmd/freedius`.

#### 5. Update AGENTS.md build commands and project-structure entry

**File**: [`AGENTS.md`](https://github.com/pfrack/freedius/blob/d361805f91845be2e59a9b253f46e5b1b37c634d/AGENTS.md)

**Intent**: AGENTS.md has two places that reference the old path: the build/run quick commands at lines 7-8, and the project-structure list at line 15. Update both.

**Contract**: Three edits in one file:
- Line 7: `- **Run**: \`go run .\` — starts the proxy server locally.` → `- **Run**: \`go run ./cmd/freedius\` — starts the proxy server locally.`
- Line 8: `- **Build**: \`go build -o freedius .\` — produces a static binary.` → `- **Build**: \`go build -o freedius ./cmd/freedius\` — produces a static binary.`
- Line 15: `- \`main.go\` — entry point, HTTP server setup, proxy routing …` → `- \`cmd/freedius/\` — entry point (single binary), HTTP server setup, proxy routing …`

### Success Criteria:

#### Automated Verification:

- 1.1 `git status` shows only renames (no content changes for the moved files) plus the 5 file edits in `magefiles/mage.go`, `test-manual.sh`, `README.md`, `AGENTS.md`.
- 1.2 `ls cmd/freedius/` shows `main.go`, `main_test.go`, `templates/`.
- 1.3 `ls templates/` at the repo root is empty (or the directory has been removed entirely).
- 1.4 `go vet ./...` exits 0.
- 1.5 `go test -race -cover ./...` exits 0.
- 1.6 `go build ./...` exits 0.
- 1.7 `go build -o /tmp/freedius-build ./cmd/freedius` exits 0 and produces a binary.
- 1.8 `go build -o /tmp/freedius-build .` exits non-zero (confirms the old path is intentionally gone — no `package main` at the root).
- 1.9 `go generate ./...` produces no diff (`git diff --exit-code` exits 0).
- 1.10 `mage CI` (which runs Vet + GenerateCheck + Test + Build) exits 0.
- 1.11 `mage ManualTest` exits 0.

#### Manual Verification:

- 1.12 `go run ./cmd/freedius` launches the binary; the TUI renders with the expected three tabs (Log / Providers / Config); the proxy listens on `127.0.0.1:8082`.
- 1.13 Confirm that the embedded starter template still loads: rename or remove the user's local `freedius.yaml`, restart freedius, and verify the embedded defaults appear (this is the `loadConfig` lazy-startup path in `main.go:288-297`).
- 1.14 `git log --follow cmd/freedius/main.go` shows the file's history traces back through the rename — confirms `git mv` preserved history.
- 1.15 Spot-check `git blame` on a random line of `cmd/freedius/main.go` — should attribute to the same author as before the move.

**Implementation Note**: After completing Phase 1 and confirming all automated verification passes, pause here for human confirmation of the manual smoke tests before considering the change complete.

---

## Phase 2: Final cleanup and follow-up notes

### Overview

After Phase 1 verifies green, capture any drift in the change description and flag the deferred follow-ups so they show up in the project log. This phase has **no code changes** — it's a documentation/handoff step.

### Changes Required:

#### 1. Update `context/changes/go-package-layout/change.md` closeout

**File**: `context/changes/go-package-layout/change.md`

**Intent**: Mark the change done. Document what landed, what was deferred, and any drift between the plan and what was actually implemented (e.g., if a specific test needed updating beyond what was anticipated).

**Contract**: Update the YAML frontmatter (`status: complete`, `updated: <commit-date>`), append a short "Closeout" section listing:
- The commit SHA of `refactor: move main package to cmd/freedius/`
- The deferred follow-ups: (a) `internal/genproviders/` → `cmd/genproviders/`, (b) `proxy/translate/` + `proxy/tui/` → `internal/proxy/`, (c) AGENTS.md project-structure list refresh
- Any drift discovered during implementation (e.g., if a file needed editing that wasn't listed in the plan)

### Success Criteria:

#### Automated Verification:

- 2.1 `git log --oneline -5` shows the new `refactor:` commit at HEAD.

#### Manual Verification:

- 2.2 The change.md closeout reads cleanly and accurately captures the actual commit + deferred items.

---

## Testing Strategy

### Unit Tests:

- `go test -race -cover ./...` runs every package's tests including the relocated `cmd/freedius/main_test.go`. No new tests; the existing 468-line internal test covers `checkRequiredEnvVars`, `resolveInt`, `resolveConfigPath`, and other root-package helpers — all of these exercise the binary's setup logic regardless of where `package main` lives.
- `internal/envinject`, `internal/genproviders`, `config`, `proxy`, `proxy/translate`, `proxy/tui` tests are unaffected by the move (they don't depend on the location of `package main`).

### Integration Tests:

- `mage ManualTest` runs `test-manual.sh`, which builds the binary from the new path and exercises the proxy + TUI against a live upstream. This is the only integration test in the repo.
- `.github/workflows/ci.yml` runs `go test ./...` + `go vet ./...` + `go build ./...` + `go generate && git diff --exit-code` + `govulncheck ./...` on push to any branch.

### Manual Testing Steps:

1. `mage Build` → verify `freedius` binary is produced.
2. `./freedius` (or `mage Run`) → confirm TUI launches, three tabs render, proxy serves `GET /health`.
3. Move/rename any user-local `freedius.yaml` → restart freedius → confirm the embedded `starter.yaml` defaults load (the lazy-startup path in `main.go:288-297`).
4. `git log --follow cmd/freedius/main.go` → confirm history traces back through the rename.
5. `git blame cmd/freedius/main.go` → confirm authorship is preserved.

## Performance Considerations

None. This change is pure file relocation; runtime behavior is unchanged. The `//go:embed` byte sequence embedded into the compiled binary is bit-identical before and after.

## Migration Notes

No data migration. No external consumers of the module path exist. The module path `github.com/pfrack/freedius` is unchanged; only the location of `package main` shifts within the module.

## References

- Research: `context/changes/go-package-layout/research.md` — full citation set for the `cmd/` convention, with quotes from the Go team's [`go.dev/doc/modules/layout`](https://go.dev/doc/modules/layout), Kubernetes, Moby, and Zalando skipper.
- Prior rejection (narrowed): [`context/archive/error-hardening/research.md:261`](https://github.com/pfrack/freedius/blob/d361805f91845be2e59a9b253f46e5b1b37c634d/context/archive/error-hardening/research.md#L261) — was about adding a *second* binary (`cmd/freedius-init/`), not about the `cmd/` convention per se.
- Go team "Organizing a Go module": https://go.dev/doc/modules/layout — authoritative layout reference.
- Stdlib precedent: `src/cmd/<toolname>/main.go` (e.g., `src/cmd/go`, `src/cmd/gofmt`).
- Pre-existing lessons: `context/foundation/lessons.md` — no prior lesson covers layout conventions; this change doesn't add one because it's a mechanical move with no implementation gotcha.

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles. See `references/progress-format.md`.

### Phase 1: Relocate main package and update build commands

#### Automated

- [x] 1.1 `git status` shows only renames + the 5 edits — a5a8d53
- [x] 1.2 `ls cmd/freedius/` shows `main.go`, `main_test.go`, `templates/` — a5a8d53
- [x] 1.3 `ls templates/` at the repo root is empty (or removed) — a5a8d53
- [x] 1.4 `go vet ./...` exits 0 — a5a8d53
- [x] 1.5 `go test -race -cover ./...` exits 0 — a5a8d53
- [x] 1.6 `go build ./...` exits 0 — a5a8d53
- [x] 1.7 `go build -o /tmp/freedius-build ./cmd/freedius` exits 0 — a5a8d53
- [x] 1.8 `go build -o /tmp/freedius-build .` exits non-zero (confirms old path gone) — a5a8d53
- [x] 1.9 `go generate ./...` produces no diff — a5a8d53
- [x] 1.10 `mage CI` exits 0 — a5a8d53
- [x] 1.11 `mage ManualTest` exits 0 — a5a8d53

#### Manual

- [ ] 1.12 `go run ./cmd/freedius` launches; TUI renders; proxy listens on 127.0.0.1:8082
- [ ] 1.13 Embedded starter template still loads (lazy-startup path verified)
- [x] 1.14 `git log --follow cmd/freedius/main.go` traces history through the rename — a5a8d53
- [x] 1.15 `git blame cmd/freedius/main.go` preserves authorship — a5a8d53

### Phase 2: Final cleanup and follow-up notes

#### Automated

- [x] 2.1 `git log --oneline -5` shows the new `refactor:` commit at HEAD

#### Manual

- [ ] 2.2 change.md closeout reads cleanly and captures commit SHA + deferred items

---
date: 2026-07-02T22:37:00+02:00
researcher: pfrack
git_commit: 162f690c3c151917b2e7935f6fe983e36ebd37a7
branch: streaming-edge-cases
repository: freedius
topic: "Quality gates in CI: audit, gaps, performance, and local/CI parity"
tags: [research, ci, mage, quality-gates, github-actions, tooling]
status: complete
last_updated: 2026-07-02
last_updated_by: pfrack
---

# Research: Quality gates in CI

**Date**: 2026-07-02T22:37:00+02:00
**Researcher**: pfrack
**Git Commit**: 162f690c3c151917b2e7935f6fe983e36ebd37a7
**Branch**: streaming-edge-cases
**Repository**: freedius

## Research Question

What quality gates does the freedius CI pipeline currently enforce, where do they live, and how do they compare to a healthy Go CI baseline — with special focus on (a) a current-state audit, (b) gaps vs standard practice, (c) pipeline performance and feedback quality, and (d) parity between local (`scripts/pre-commit`, `AGENTS.md`) and CI (`mage ci` + `.github/workflows/ci.yml`)?

## Summary

The pipeline is fundamentally sound: one job runs `mage ci`, which chains six blocking gates (`Vet → GenerateCheck → Test (-race -cover) → Lint (staticcheck + golangci-lint) → Build → Govulncheck`) with clear step-by-step progress and a coverage artifact upload on both success and failure. Go version, mage version, and linter config are all pinned.

But there are meaningful gaps and drift risks:

- **Format checking is documented as enforced (`AGENTS.md:25` — "gofumpt") but is not enforced anywhere.** No mage target and no CI step runs a format check; `gofumpt` isn't even installed. The actual formatter toolchain is `gofmt → goimports → golines → gci` (via `Format`/`FormatChanged`), and those are also not in `CI()`.
- **No coverage threshold.** Coverage is collected and uploaded, but nothing prevents a regression from shipping an untested translation path — a real risk given the SSE/JSON byte-fidelity lessons in `context/foundation/lessons.md`.
- **`go mod tidy` and `go mod verify` are not gated.** `Tidy` and `ModVerify` targets exist in `magefiles/mage.go` but are not chained into `CI()`.
- **Pre-commit hook is a strict subset of CI** — it runs only `mage lint`, so a commit can pass locally and break `GenerateCheck`, `Test`, `Build`, or `Govulncheck` in CI.
- **Pipeline is fully serial and reinstalls tools every run.** `Lint`, `Test`, `Build`, and `Govulncheck` could run in parallel; `staticcheck`, `golangci-lint`, `govulncheck`, and `mage` are recompiled from source on every CI run because `~/go/bin` is not cached.
- **No `concurrency:` block, no `timeout-minutes:`, no `$GITHUB_STEP_SUMMARY`** in the workflow — cheap wins that are still missing.
- **Tool-version constants are decorative.** The `toolVersionX` constants at `magefiles/mage.go:16-23` are only used for `Help` output; the actual `go install` calls hardcode the same literals, so bumping the constant does not bump the install.

Nothing here is a correctness bug in shipped code; every finding is either a missing gate or a DX/perf improvement to the gating system itself. This is a strong foundation to plan targeted additions against.

## Detailed Findings

### Current-state audit: gates actually running in CI

`CI()` is defined at `magefiles/mage.go:287-314` as a hand-rolled sequential `for` loop over a `steps` slice (not `mg.SerialDeps`), returning on first error and printing `[i/N] Running X…` / `✓ X passed`. The six steps, in order:

| # | Step | Command | Defined at | Env vars |
|---|------|---------|------------|----------|
| 1 | Vet | `go vet ./...` | `magefiles/mage.go:132-135` | — |
| 2 | Generate Check | `go generate ./...` then `git diff --exit-code -- *.go` | `magefiles/mage.go:172-178` | — |
| 3 | Test | `go test -race -cover [-coverprofile=$COVERPROFILE] ./...` | `magefiles/mage.go:95-104` | `COVERPROFILE` |
| 4 | Lint | `mg.SerialDeps(Vet, LintStatic, LintGolangci)` | `magefiles/mage.go:271-274`; sub-targets `:251-258` and `:261-268` | — |
| 5 | Build | `go build -o freedius ./cmd/freedius` | `magefiles/mage.go:138-141` | — |
| 6 | Govulncheck | auto-install `golang.org/x/vuln/cmd/govulncheck@v1.3.0`, then `govulncheck ./...` | `magefiles/mage.go:277-284` | — |

Workflow layer around `mage ci` (`.github/workflows/ci.yml`):

- `actions/checkout@v4` (`:16`)
- `actions/setup-go@v5` with `go-version: '1.26.4'`, `cache: true` (`:17-20`)
- `go install github.com/magefile/mage@v1.17.2` (`:21-22`)
- `mage ci` with `COVERPROFILE=coverage.out` (`:23-26`)
- `actions/upload-artifact@v4` with `if: success() || failure()`, 30-day retention (`:27-33`)

Triggers: push + pull_request on `**` (`.github/workflows/ci.yml:3-7`). Permissions: `contents: read` (`:9-10`).

The lint config that governs step 4's golangci-lint invocation is `.golangci.yaml` (v2 schema) at repo root — enables `govet, errcheck, staticcheck, unused, ineffassign, bodyclose, durationcheck, copyloopvar, exhaustive, revive, gocritic, gocyclo, nakedret, unconvert, misspell, whitespace, nolintlint, gosec` plus a `formatters:` block for `gofmt, goimports, gci, golines`.

**Redundancy**: `Vet` runs twice per CI (once as step 1 at `magefiles/mage.go:292`, again inside `Lint` via `mg.SerialDeps(Vet, LintStatic, LintGolangci)` at `:272`), and `govet` runs a third time as a golangci-lint linter (`.golangci.yaml:11`).

### Mage targets defined but NOT chained into CI

From `magefiles/mage.go`, targets that exist but are absent from `CI()`:

- `Tidy` (`:181-184`) — `go mod tidy`
- `ModVerify` (`:187-194`) — `go mod verify`
- `Format` (`:361-376`) — full-repo gofmt + goimports + golines + gci
- `FormatChanged` (`:379-404`) — same on changed files
- `Coverage` (`:113-129`) — HTML coverage report
- `Benchmark` (`:107-110`)
- `Clean` (`:150-169`), `Install` (`:144-147`), `Watch` (`:221-248`), `RunDev` (`:207-213`), `Verbose` (`:216-218`), `InstallHooks` (`:322-334`), etc.

The pre-commit hook at `scripts/pre-commit:5` runs only `mage lint` (i.e. `Vet + LintStatic + LintGolangci`) — no test, no generate check, no build, no vuln check, no format check.

### Gap analysis vs healthy Go CI baseline

20-gate scorecard:

| # | Gate | Status | Evidence / Recommendation |
|---|------|--------|---------------------------|
| 1 | `go vet ./...` | PRESENT | `magefiles/mage.go:132-135, 292` |
| 2 | `go test -race ./...` | PRESENT | `magefiles/mage.go:97-103`; consider adding `-shuffle=on` for a proxy with concurrent goroutines |
| 3 | Coverage profile artifact | PRESENT | `.github/workflows/ci.yml:26-33`; consider also printing `go tool cover -func` summary in log |
| 4 | Coverage threshold enforcement | **MISSING** | Parse `go tool cover -func=coverage.out` total, fail below N% (start at current level and ratchet) |
| 5 | `staticcheck ./...` | PRESENT | `magefiles/mage.go:251-258`; also runs via `golangci-lint` (`.golangci.yaml:13`) — redundant but harmless |
| 6 | `golangci-lint run` with config | PRESENT | Runner `magefiles/mage.go:261-268`; config `.golangci.yaml` |
| 7 | Format check enforced in CI | **MISSING (docs lie)** | `AGENTS.md:25` claims gofumpt enforced; nothing runs a format check. Add `golangci-lint fmt --diff` or `gofmt -l` + `goimports -l` + `gci diff` + `golines -l --max-len=120`, or fix `AGENTS.md` |
| 8 | `go mod tidy` cleanliness | PARTIAL | `Tidy` target exists (`magefiles/mage.go:181-184`) but no `tidy && git diff --exit-code -- go.mod go.sum` gate |
| 9 | `go mod verify` | PARTIAL | `ModVerify` exists (`magefiles/mage.go:187-194`), never chained into `CI()`; one-line fix |
| 10 | `govulncheck ./...` blocking | PRESENT | `magefiles/mage.go:277-284, 297` |
| 11 | `go generate` drift check | PRESENT (weak) | `magefiles/mage.go:172-178`; pathspec `*.go` may under-match subdirs and misses non-Go generated files. Consider `git diff --exit-code` (all paths). |
| 12 | Build check + static binary | PARTIAL | `go build -o freedius ./cmd/freedius` (`magefiles/mage.go:138-141`); consider `go build ./...` and static build flags (see #15) |
| 13 | Integration vs unit test separation | MISSING | No `//go:build integration` tags in repo; `mage test` runs everything indiscriminately. Establish convention while suite is small. |
| 14 | Go version pinning consistency | PRESENT | `go.mod:3` = `.github/workflows/ci.yml:19` = `1.26.4`; consider `.go-version` + `go-version-file:` to prevent drift (drift was the exact `plan.md:23` bug last time) |
| 15 | Reproducibility flags | MISSING | `Build` uses no `-trimpath`, no `-ldflags "-s -w"`, no version stamp, no `CGO_ENABLED=0` — contradicts "single static binary" claim in `AGENTS.md:3` |
| 16 | SBOM / provenance / attestations | MISSING (future) | Defer until release workflow exists |
| 17 | Tag / release lint | N/A | No semver tags in repo |
| 18 | Secret scanning / dep review | MISSING | Add `actions/dependency-review-action@v4` on `pull_request` — cheap CVE/license guard |
| 19 | Concurrency cancellation | MISSING | Add `concurrency: {group: ci-${{ github.ref }}, cancel-in-progress: true}` at workflow scope |
| 20 | Least-privilege `permissions:` | PRESENT | `.github/workflows/ci.yml:9-10` |

### Performance and feedback quality

**Caching**
- `actions/setup-go@v5` with `cache: true` (`.github/workflows/ci.yml:20`) caches `$GOMODCACHE` and `$GOCACHE`, keyed on `go.sum`.
- `~/go/bin` is **not** cached — so `mage`, `staticcheck`, `golangci-lint`, and `govulncheck` are recompiled from source every run (`.github/workflows/ci.yml:22`, `magefiles/mage.go:252-256, 262-266, 278-282`). Each install is 10-30 s of pure compile.
- No separate `actions/cache` step, no dedicated build-cache action.

**Parallelism**
- Single job (`test`). No matrix, no split lint/test/build jobs.
- Inside `mage ci`, all six steps are serial (hand-rolled `for` loop at `magefiles/mage.go:303-310`).
- `Lint()` explicitly uses `mg.SerialDeps` (`:272`), not `mg.Deps`.
- `Test`, `Lint`, `Build`, and `Govulncheck` are logically independent and could run in parallel — worst case wall-time drops from `sum(...)` to `max(...)`, likely 40-60% faster on failing runs.

**Feedback / DX**
- `CI()` prints `[i/N] Running X…` / `✓ X passed` and wraps step failure as `%s failed: %w` (`magefiles/mage.go:303-310`). Good.
- Sub-targets have inconsistent error wrapping: `Coverage`, `ModVerify`, `InstallHooks` wrap; `Test`, `Vet`, `Build`, `LintStatic`, `LintGolangci`, `Govulncheck`, `GenerateCheck`, `Tidy` return raw `sh.RunV` errors. Outer wrapping from `CI()` mitigates this at the pipeline level but not for standalone target invocation.
- Coverage upload runs on both success and failure (`if: success() || failure()`, `.github/workflows/ci.yml:28`). Good.
- No `continue-on-error` anywhere on CI path. Good.
- No `concurrency:` block — superseded pushes waste runners.
- No `timeout-minutes:` — a stuck job burns the runner default (360 min).
- No `$GITHUB_STEP_SUMMARY` writes, no coverage-comment action, no test-report action — users must download `coverage.out` to see anything beyond the raw log.

**Time-to-signal**
- Order: `Vet → GenerateCheck → Test → Lint → Build → Govulncheck`. Cheapest gate fires first (good).
- But `Test` (with `-race`, usually the slowest step) runs before `Lint`, so lint typos surface only after the race-tested suite completes.
- Reordering to `Vet → Lint → GenerateCheck → Build → Test → Govulncheck` (or parallelizing `Lint`+`Test`) surfaces cheap failures faster.
- Dropping the redundant `Vet` inside `Lint` (`magefiles/mage.go:272`) is a zero-cost cleanup.

### Local ↔ CI parity

**Command surface**

| Surface | Command | Evidence |
|---------|---------|----------|
| Pre-commit hook | `mage lint` | `scripts/pre-commit:5` |
| AGENTS.md guidance | five individual targets, no `mage ci` | `AGENTS.md:7-11, 36` |
| README.md guidance | **stale** — refers to `make test`, `make lint`, `make ci`, `make install-hooks` (Makefile removed) | `README.md:131-143` |
| CI | `mage ci` with `COVERPROFILE=coverage.out` | `.github/workflows/ci.yml:24-26` |

**Parity matrix** (P = pre-commit, M = `mage ci`, C = ci.yml)

| Gate | P | M | C | Notes |
|------|---|---|---|-------|
| `go vet` | ✅ | ✅ | ✅ | Aligned |
| `staticcheck` | ✅ | ✅ | ✅ | Version literal, not constant (see drift D7) |
| `golangci-lint` | ✅ | ✅ | ✅ | Same |
| `generateCheck` | ❌ | ✅ | ✅ | Hook gap |
| `test -race -cover` | ❌ | ✅ | ✅ | Hook gap; `COVERPROFILE` only in CI |
| `build` | ❌ | ✅ | ✅ | Hook gap |
| `govulncheck` | ❌ | ✅ | ✅ | Hook gap |
| gofumpt | ❌ | ❌ | ❌ | Claimed by AGENTS.md, never installed anywhere |
| gofmt/goimports/golines/gci check | ❌ | ❌ | ❌ | Format targets exist, not gated |
| `go mod tidy` clean | ❌ | ❌ | ❌ | Target exists, not gated |
| `go mod verify` | ❌ | ❌ | ❌ | Target exists, not gated |
| Coverage artifact | ❌ | ❌ | ✅ | CI-only side effect |

**Version pinning**
- Go: `go.mod:3` = `magefiles/go.mod:3` = `.github/workflows/ci.yml:19` = `1.26.4`. Aligned.
- Mage: `go.mod:10` = `magefiles/go.mod:5` = `ci.yml:22` = `v1.17.2`. Aligned.
- Tool version constants at `magefiles/mage.go:16-23` are **decorative** — used only in `Help` output. Actual installs hardcode literal versions (`:253, 263, 279, 339, 347, 355`), so bumping a constant does nothing.

**Env drift**
- `COVERPROFILE` only set in CI (`.github/workflows/ci.yml:26`). Local `mage ci` runs without producing `coverage.out`.

**Bootstrap friction**
- All lint/vuln tools auto-install via `which <tool> || go install <tool>@<version>` (`magefiles/mage.go:252, 262, 278, 338, 346, 354`). The `which` guard has no version check — a pre-existing older tool on PATH silently satisfies it.
- `gofumpt` is not installed anywhere despite `AGENTS.md:25`.
- `mage` itself is only auto-installed in CI (`.github/workflows/ci.yml:22`); local contributors either install it or use the zero-install `go run mage.go ci` path — the latter is undocumented.
- `InstallHooks` target (`magefiles/mage.go:322-334`) exists to symlink (or copy) `scripts/pre-commit` into `.git/hooks/pre-commit`, but nothing tells contributors to run it. `README.md:142` still references the removed `make install-hooks`. `scripts/pre-commit` is mode 644 in the repo; `InstallHooks` chmods only the destination.

## Code References

- `.github/workflows/ci.yml:3-34` — full CI workflow (triggers, permissions, steps, artifact upload)
- `.github/workflows/ci.yml:19` — Go version pin (`1.26.4`)
- `.github/workflows/ci.yml:22` — Mage install (`v1.17.2`)
- `.github/workflows/ci.yml:24-26` — `mage ci` invocation with `COVERPROFILE`
- `.github/workflows/ci.yml:27-33` — coverage artifact upload with `if: success() || failure()`
- `magefiles/mage.go:16-23` — tool version constants (decorative)
- `magefiles/mage.go:95-104` — `Test` target with optional `COVERPROFILE`
- `magefiles/mage.go:132-135` — `Vet`
- `magefiles/mage.go:138-141` — `Build` (no static flags, no ldflags, no trimpath)
- `magefiles/mage.go:172-178` — `GenerateCheck` (`go generate` + `git diff --exit-code -- *.go`)
- `magefiles/mage.go:181-194` — `Tidy` and `ModVerify` (defined, not chained)
- `magefiles/mage.go:251-268` — `LintStatic`, `LintGolangci` with lazy `go install`
- `magefiles/mage.go:271-274` — `Lint` (`mg.SerialDeps(Vet, LintStatic, LintGolangci)` — Vet redundant with step 1)
- `magefiles/mage.go:277-284` — `Govulncheck` with lazy install
- `magefiles/mage.go:287-314` — `CI()` step slice + progress loop + error wrapping
- `magefiles/mage.go:322-334` — `InstallHooks` (symlink with copy fallback)
- `magefiles/mage.go:361-431` — `Format` and `FormatChanged` (unused by CI)
- `.golangci.yaml:1-97` — golangci-lint v2 config, incl. formatters block
- `scripts/pre-commit:5` — hook runs only `mage lint`
- `AGENTS.md:25` — false claim that gofumpt is enforced in CI
- `AGENTS.md:7-11, 36` — points contributors at individual targets, never mentions `mage ci`
- `README.md:131-143` — stale documentation referencing removed Makefile
- `context/foundation/lessons.md:3-13` — SSE/JSON byte-fidelity lessons that make coverage regressions expensive

## Architecture Insights

- The pipeline is a good "one command, one job, six blocking gates" design — simple and legible.
- Mage bought parity in principle (CI runs the same code as local), but three practical layers still diverge: pre-commit (subset), AGENTS.md (individual targets), CI (`mage ci`).
- The auto-install-on-demand pattern in `magefiles/mage.go` optimizes for a zero-friction fresh clone at the cost of CI recompile time on every push. Caching `~/go/bin` (keyed by tool-version constants) reconciles both goals.
- The tool-version constants are the right idea structurally, but need to be referenced by the actual install calls to become a real single source of truth.
- The proxy's byte-fidelity nature (SSE framing, JSON encoding, streaming) makes coverage-threshold and format-check gates more valuable than for a typical Go service — see `lessons.md` (`json.Marshal` vs `json.NewEncoder`, `bufio.Reader.ReadBytes` vs `bufio.Scanner`).

## Historical Context (from prior changes)

- `context/archive/2026-06-17-magefile/` — introduced the magefile itself; established the target set and the auto-install pattern.
- `context/archive/2026-07-01-mage-ci-integration/plan.md` — introduced `mage ci`, coverage artifact, and the six-step chain. Explicitly deferred Codecov/Coveralls integration, kept `govulncheck` as a mage target (not "separate CI step" — the plan changed course), and did not touch format check as a CI gate.
- `context/archive/2026-07-01-mage-ci-integration/plan.md:23` — noted Go version drift (`1.26.1` vs `1.26.4`) as a gap fixed in that change. Suggests `.go-version` + `go-version-file:` as the durable fix (see #14).
- `context/foundation/lessons.md` — several lessons (SSE framing, `x-api-key`, adapter return contract) illustrate why coverage and format gates matter more here than in a typical CRUD service.

## Related Research

- `context/archive/2026-07-01-mage-ci-integration/research.md` — prior investigation of the Mage/CI integration.
- `context/archive/2026-06-17-magefile/research.md` — original magefile research.

## Open Questions

1. **Coverage floor: what's the honest starting number?** Need a baseline `go tool cover -func` run on `coverage.out` to pick a non-punitive initial threshold that still ratchets up.
2. **Parallelize inside one job (via `mg.Deps`) or split into workflow jobs?** Job splitting gives per-check red/green in the PR UI but incurs setup-Go + cache overhead per job. `mg.Deps` inside `mage ci` is cheaper but loses the visual split.
3. **Does `git diff --exit-code -- *.go` in `GenerateCheck` actually cover subdirectories on this Git version?** Should be verified with an intentional generated-file drift test — the pathspec semantics are subtle.
4. **`gofumpt` decision**: adopt it (install + wire into `Format` + config in `.golangci.yaml`) or delete the claim from `AGENTS.md`? Current chain is `gofmt → goimports → golines → gci`; gofumpt would conflict with the existing formatters unless carefully sequenced.
5. **Integration test convention**: `//go:build integration` tag + separate `mage testIntegration` target — but which tests belong there? Real-upstream tests? Long-running streaming tests? Needs a definitional pass before wiring a CI job for it.
6. **Should the pre-commit hook run more than `mage lint`?** Even `mage generateCheck` + `mage vet` would eliminate the biggest hook↔CI drift class at negligible cost. Full `mage ci` in the hook is likely too slow to keep contributors compliant.
7. **`.go-version` file**: worth adopting to eliminate CI vs `go.mod` drift permanently, per the last CI change's own hindsight?

## Prioritized Recommendations

Ordered by cost/benefit for this specific repo (byte-faithful proxy, SSE streaming):

1. **Add `concurrency:` and `timeout-minutes:` to the workflow.** ~5 lines, zero risk, saves CI minutes on force-push.
2. **Cache `~/go/bin` keyed by tool-version constants** so `mage`, `staticcheck`, `golangci-lint`, `govulncheck` don't recompile every run. Requires wiring the constants (`magefiles/mage.go:16-23`) into both the install calls and an `actions/cache` step.
3. **Chain `ModVerify` and a `Tidy`-clean check into `CI()`.** Highest ROI-per-LOC on the whole list; both are already implemented as targets.
4. **Wire a real format check into `CI()`** (either `golangci-lint fmt --diff` or a dedicated `FormatCheck` mage target), and reconcile `AGENTS.md:25` with reality (adopt gofumpt or delete the claim).
5. **Add a coverage-threshold gate** parsing `go tool cover -func=coverage.out`. Start at the honest current number and ratchet up.
6. **Fix the tool-version-constant drift** — replace hardcoded literals in `go install` calls with the `toolVersion*` constants they already declared.
7. **Reorder `CI()` to fail cheap first** (drop redundant `Vet` in `Lint`; consider `Vet → Lint → GenerateCheck → Build → Test → Govulncheck`).
8. **Establish `//go:build integration` convention + `mage testIntegration`** before the suite grows real-upstream tests.
9. **Fix docs drift**: update `README.md:131-143` (remove Makefile references), mention `mage ci` and `go run mage.go ci` in `AGENTS.md`, document `mage installHooks`.
10. **Reproducible static build**: `CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=..."` in `Build`.
11. **Add `actions/dependency-review-action@v4`** on `pull_request` events for supply-chain guard.

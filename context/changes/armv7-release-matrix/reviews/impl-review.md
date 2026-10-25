<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: armv7 Release Matrix

- **Plan**: context/changes/armv7-release-matrix/plan.md
- **Scope**: Phases 1-4 (full plan, all phases marked implemented)
- **Implementation**: 7faa4f5 → 820f020 (5 commits spanning GoReleaser matrix, docs, CI integration, re-cut)
- **Date**: 2026-10-25
- **Verdict**: APPROVED
- **Findings**: 0 critical, 0 warnings, 3 observations

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | ✅ PASS |
| Scope Discipline | ✅ PASS |
| Safety & Quality | ✅ PASS |
| Architecture | ✅ PASS |
| Pattern Consistency | ✅ PASS |
| Success Criteria | ✅ PASS |

## Verification Results

| Check | Command | Result |
|---|---|---|
| GoReleaser config valid | `goreleaser check` | ✅ PASS |
| GoReleaser mage target | `mage goreleaserCheck` | ✅ PASS |
| Go build (root) | `go build ./...` | ✅ PASS |
| Go build (magefiles) | `go build ./...` in `magefiles/` | ✅ PASS |
| Docs mention armv7 | `grep -c "armv7" README.md AGENTS.md` | ✅ PASS (3 matches) |

### Verification re-run at review time

```
goreleaser check                                      → ✅ validated
go build ./...                                        → ✅ BUILD_OK
go build ./... (magefiles/)                           → ✅ BUILD_OK
grep -c "armv7" README.md AGENTS.md                   → ✅ 3 matches
```

## Findings

### F1 — Go toolchain bump (1.27.2 → 1.27.3) is unplanned but incidental

- **Severity**: 👁️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; no action needed on armv7 change scope
- **Dimension**: Scope Discipline
- **Location**: `go.mod`, `magefiles/go.mod`, `Dockerfile`, `.github/workflows/ci.yml`, `.github/workflows/release.yml` (commit `e1998fd`)
- **Detail**: The Go version bump is not part of the armv7 plan. It was bundled in a separate commit (`e1998fd`, "fix(ci): bump Go toolchain to 1.27.3 for stdlib govulncheck fix"). The plan's "Files Touched" section does not list any of these files. The change is legitimate (stdlib govulncheck fix) and consistent across all files, but it is outside the plan's scope.
- **Fix**: No fix required — this is an acknowledged incidental change that does not affect the armv7 feature itself.
- **Decision**: ACCEPTED — matches plan scope

### F2 — `CI()` doc comment relabels pre-existing steps

- **Severity**: 👁️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; no action needed
- **Dimension**: Plan Adherence
- **Location**: `magefiles/mage.go:393-394`
- **Detail**: The plan's contract for D3.2 only asks to "update the doc comment to mention the new step" (i.e., add `go-releaser-check`). The implementation also reordered the labels in the doc comment to match the actual step order (`vet + mod-verify + tidy-check + generate-check...` instead of the previous `vet + generate-check + mod-verify + tidy-check...`). This is a harmless incidental correctness fix, but it exceeds the plan's stated contract.
- **Fix**: No action — the reordering is a minor correctness improvement; it doesn't change the intent or introduce risk.
- **Decision**: ACCEPTED

### F3 — `goreleaserCheck` help line placement under "Docker/CI" section

- **Severity**: 👁️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; no action needed
- **Dimension**: Pattern Consistency
- **Location**: `magefiles/mage.go:85`
- **Detail**: The new `goreleaserCheck` help line is placed in the Docker/CI section (after `goreleaser`), which is where the plan specified. This is consistent with the existing placement of `goreleaser` there. No issue — this follows the plan contract and matches the existing organization.
- **Fix**: None.
- **Decision**: ACCEPTED — matches plan scope

## Areas Verified Clean

- **GoReleaser matrix correct.** `.goreleaser.yaml` gains `arm` in `goarch`, adds `goarm: ["7,hardfloat"]`, and adds `ignore` entries for `darwin/arm` and `windows/arm` — matching the plan's contract exactly. `goreleaser check` validates the config.
- **Docs updated consistently.** Both `README.md` (two locations) and `AGENTS.md` (one location) updated from `(amd64/arm64)` to `(amd64/arm64/armv7)`. Verified with grep.
- **`GoreleaserCheck()` mirrors `Goreleaser()` pattern.** Same install-if-missing guard, same `toolVersionGoreleaser` constant, same error return shape. No `ARGS` passthrough (correct for `goreleaser check`).
- **CI integration correct.** `GoreleaserCheck` added to `steps` slice after Format Check and before Test, as the plan contracts.
- **Go version bump is consistent.** All 5 files (`go.mod`, `magefiles/go.mod`, `Dockerfile`, `.github/workflows/ci.yml`, `.github/workflows/release.yml`) bumped uniformly `1.27.2 → 1.27.3`.
- **Scope discipline held.** Plan's "Files Touched" for Phase 4 states "none (git/GitHub operations)" — verified: commits `072a1a4` and `820f020` touched no implementation files.
- **No regressions.** `go build ./...` passes for both root and magefiles modules.

## Unrun Manual Criteria

4.3–4.5 remain `[ ]` — all require downloading the re-cut release asset and running it on the target `192.168.0.2` device. This is a post-release verification step that cannot be run from this review environment.

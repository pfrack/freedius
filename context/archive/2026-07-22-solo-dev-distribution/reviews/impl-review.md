<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: GoReleaser Integration (solo-dev-distribution)

- **Plan**: context/changes/solo-dev-distribution/plan.md
- **Scope**: Phases 1, 2, 4 of 4 (Phase 3 manual items pending, skipped)
- **Date**: 2026-07-22
- **Verdict**: NEEDS ATTENTION
- **Findings**: 0 critical, 4 warnings, 4 observations

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | PASS ✅ |
| Scope Discipline | WARNING ⚠️ |
| Safety & Quality | PASS ✅ |
| Architecture | PASS ✅ |
| Pattern Consistency | WARNING ⚠️ |
| Success Criteria | PASS ✅ |

## Findings

### F1 — README.md modified outside plan scope

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Scope Discipline
- **Location**: README.md
- **Detail**: README.md was modified (table restructuring for flags/variables, section reformatting) without a corresponding entry in any phase's "Changes Required". Changes appear to be benign formatting/documentation improvements, but they're unplanned.
- **Fix A ⭐ Recommended**: Document the README changes in the plan as an addendum
  - Strength: Preserves the work already done; updates the source of truth before future reviews.
  - Tradeoff: Plan becomes a slightly broader scope reference.
  - Confidence: HIGH — the changes are harmless formatting updates.
  - Blind spot: None significant.
- **Fix B**: Revert README changes for strict scope discipline
  - Strength: Keeps scope bounded — no unplanned changes remain.
  - Tradeoff: Loses benign formatting improvements.
  - Confidence: MEDIUM — depends on whether the reformatting is desired.
  - Blind spot: Haven't confirmed whether the README updates were intentional.
- **Decision**: Fixed via Fix A — README addendum appended to plan end (to be removed if not desired).

### F2 — GoReleaser action version unpinned as `latest`

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — release pipeline could break silently
- **Dimension**: Pattern Consistency
- **Location**: .github/workflows/release.yml:26
- **Detail**: `goreleaser/goreleaser-action@v6` uses `version: latest`, but the magefile pins `toolVersionGoreleaser = "v2.6.2"` and `ci.yml` pins all other action versions. An unpinned `latest` could break the release pipeline silently on a new GoReleaser minor release.
- **Fix**: Pin `version: v2.6.2` in `.github/workflows/release.yml` to match the magefile pin.
- **Decision**: Fixed via Fix now — `version: latest` → `version: v2.6.2` in release.yml:26.

### F3 — Missing `concurrency` block in release workflow

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Pattern Consistency
- **Location**: .github/workflows/release.yml
- **Detail**: `ci.yml` uses a `concurrency` block to cancel in-progress runs on new pushes. `release.yml` has no equivalent. Two rapid `v*` tag pushes could cause overlapping GoReleaser runs, potentially producing duplicate or corrupted release artifacts.
- **Fix**: Add the same `concurrency` block pattern from ci.yml:
  ```yaml
  concurrency:
    group: ${{ github.workflow }}-${{ github.ref }}
    cancel-in-progress: true
  ```
- **Decision**: Fixed via Fix now — concurrency block added to release.yml.

### F4 — Missing `-gcflags=-l` in `.goreleaser.yaml`

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern Consistency
- **Location**: .goreleaser.yaml
- **Detail**: `magefiles/mage.go:153` passes `-gcflags=-l` (disable inlining) to produce smaller binaries. `.goreleaser.yaml` doesn't include this flag, so CI-released binaries will be slightly larger than `mage build` output, breaking the parity described in the plan.
- **Fix**: Add `- -gcflags=-l` under the `flags:` section in `.goreleaser.yaml` to match the mage build.
- **Decision**: Fixed via Fix now — `-gcflags=-l` added to goreleaser.yaml flags.

### F5 — No installation/release section in README

- **Severity**: 👁️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern Consistency
- **Location**: README.md
- **Detail**: The new release infrastructure (`.github/workflows/release.yml`, `.goreleaser.yaml`) creates GitHub Releases for each `v*` tag, but README has no installation or release instructions to guide users on obtaining pre-built binaries.
- **Fix**: Add an "Installation" or "Releases" section pointing to GitHub Releases with `go install` and download links.
- **Decision**: Fixed via Fix now — Installation section added to README.

### F6 — README quickstart shows plain `go build` not `mage build`

- **Severity**: 👁️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern Consistency
- **Location**: README.md
- **Detail**: `magefiles/mage.go` now injects version info via `git describe --tags --always --dirty` in the `Build` target, but the README Quickstart still shows plain `go build -o freedius ./cmd/freedius` as the primary build instruction — users building that way get version "dev" with no way to inject a tag.
- **Fix**: Update README Quickstart to note `mage build` for versioned builds, or document the `-ldflags` equivalent.
- **Decision**: Fixed via Fix now — README Quickstart updated to reference `mage build`.

### F7 — `FREEDIUS_HOST` documented but not implemented

- **Severity**: 👁️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern Consistency
- **Location**: README.md
- **Detail**: `FREEDIUS_HOST` is documented as an override for `--host`, but `cmd/freedius/main.go` only reads `FREEDIUS_UI_HOST` (UI server). The proxy host has no env override, only the CLI flag. This documentation inconsistency is pre-existing, not introduced by this change.
- **Fix**: Either implement `FREEDIUS_HOST` in `main.go` or remove it from the README table.
- **Decision**: Fixed via Fix A — `FREEDIUS_HOST` env override added to `main.go:114-116`.

### F8 — `checkRequiredEnvVars` error silently discarded

- **Severity**: 👁️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Reliability
- **Location**: cmd/freedius/main.go:135
- **Detail**: `_ = checkRequiredEnvVars(cfg)` silently discards the error returned by this function, which includes descriptive messages about missing env vars (lines 396-402). Pre-existing, not introduced by this change.
- **Fix**: Log or handle the returned error instead of discarding it.
- **Decision**: Fixed via Fix now — `checkRequiredEnvVars` error now handled as `failf` at main.go:137-139; test updated accordingly.

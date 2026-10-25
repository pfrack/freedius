# armv7 Release Matrix Implementation Plan

## Overview

Extend the GoReleaser release matrix with 32-bit ARM (`armv7`, hard-float) so the pre-built binary runs on the user's Armbian `armhf` device at `192.168.0.2`. Update the platform documentation to match, add GoReleaser config validation to CI, and re-cut the `v0.1.0` release so the new binary is published.

## Current State Analysis

- `.goreleaser.yaml:20-26` builds `linux/darwin/windows × amd64/arm64` — **no 32-bit ARM**. 6 artifacts.
- The target device is `armv7l` / `armhf` (Allwinner sun4i/sun5i, VFPv3+NEON), has **no Go toolchain**, so a GitHub Release binary is the only viable install path (see `research.md` §2).
- Cross-compilation is **already verified**: `GOOS=linux GOARCH=arm GOARM=7` and `GOARM=7,hardfloat` binaries both ran on the device (`research.md` §3).
- The release workflow is tag-triggered and works (`.github/workflows/release.yml:3-6`); tag `v0.1.0` currently points at `773e224` with 6 assets.
- GoReleaser is pinned to `v2.17.1` (`.github/workflows/release.yml:33`, `magefiles/mage.go:25`), which supports `goarm` float-ABI suffixes (`7,hardfloat`).

## Desired End State

Every `v*` tag produces **7** release artifacts — `linux/{amd64,arm64,arm}`, `darwin/{amd64,arm64}`, `windows/{amd64,arm64}` — where the `linux_arm` binary is a 32-bit ARMv7 hard-float build. The `v0.1.0` release is re-published with the armv7 asset, the docs advertise `armv7`, and `mage ci` validates the GoReleaser config so an invalid matrix combination fails before a tag is cut.

### Key Discoveries:

- `darwin/arm` is **not** a valid Go target; adding `arm` to `goarch` requires an `ignore` entry for `darwin`+`arm` or the matrix generation fails (`research.md` §4).
- GoReleaser's default `goarm` is `6`; explicit `goarm: ["7,hardfloat"]` is required for an ARMv7 hard-float binary (`research.md` §4).
- `windows/arm` is valid but unused; the plan excludes it to keep the release at 7 artifacts (`research.md` §4).
- `CGO_ENABLED=0` + `netgo,osusergo` (`.goreleaser.yaml:11-13`) make this a pure packaging change — no source edits.

## What We're NOT Doing

- **Docker multi-arch** — the `Dockerfile` stays `linux/amd64`-only. A `linux/arm/v7` image is a separate future change (confirmed out of scope).
- **Other architectures** — no `386`, `riscv64`, `ppc64le`, `s390x`, `mips`. Only the ARM family.
- **Windows ARM** — excluded via `ignore`.
- **Homebrew / Scoop / Nix** — still out of scope (unchanged from `solo-dev-distribution`).
- **Self-update subcommand** — unchanged.
- **Soft-float variant** — only `7,hardfloat` is shipped (no second armv7 ABI).

## Implementation Approach

A four-phase, config-only change: (1) extend the GoReleaser matrix and validate locally, (2) correct the platform docs, (3) add `goreleaser check` to `mage ci`, (4) re-cut `v0.1.0` and verify the armv7 binary on the device. No Go source code is modified.

## Critical Implementation Details

- **Release re-cut is destructive.** Phase 4 deletes the existing `v0.1.0` release and tag, then force-pushes the tag at the new HEAD. This replaces the 6 previously-published assets with 7. It must run only after Phases 1–3 are committed and green.
- **`ignore` must list every invalid arm combination.** `darwin`+`arm` is invalid outright; `windows`+`arm` is valid but intentionally excluded. Both entries are required for the intended 7-artifact matrix.

## Phase 1: Extend the GoReleaser Release Matrix

### Overview

Add 32-bit ARM (`armv7`, hard-float) to the GoReleaser build matrix and validate the config locally.

### Changes Required:

#### 1. GoReleaser build matrix

**File**: `.goreleaser.yaml`

**Intent**: Add `arm` to the architecture list, pin the ARM float ABI to hard-float for the `armhf` target, and exclude the two unwanted arm combinations so the matrix resolves to 7 targets.

**Contract**: The `builds[0]` block gains `arm` in `goarch`, a `goarm` list, and an `ignore` list. The resulting block:

```yaml
    goos:
      - linux
      - darwin
      - windows
    goarch:
      - amd64
      - arm64
      - arm
    goarm:
      - "7,hardfloat"
    ignore:
      - goos: darwin
        goarch: arm
      - goos: windows
        goarch: arm
```

The `archives`, `checksum`, and `release` blocks are unchanged — the existing `name_template` yields `freedius_<version>_linux_arm.tar.gz` and the single checksum file covers all 7 archives automatically.

### Success Criteria:

#### Automated Verification:

- `goreleaser check` exits 0: `goreleaser check`
- Snapshot build produces 7 binaries including a 32-bit ARM one: `goreleaser build --snapshot --clean`
- The armv7 binary is a static 32-bit ARM ELF: `file dist/*/freedius_linux_arm`
- `go build ./...` still succeeds (no source change, but confirms nothing broke): `go build ./...`

#### Manual Verification:

- `dist/` contains exactly 7 binaries; `freedius_linux_arm` is 32-bit ARM, statically linked.
- `goreleaser check` reports no errors.

**Implementation Note**: After completing this phase and all automated verification passes, pause here for manual confirmation from the human that the manual testing was successful before proceeding to the next phase.

---

## Phase 2: Update Platform Documentation

### Overview

Correct the three places that advertise the supported platform matrix so they include 32-bit ARM.

### Changes Required:

#### 1. README Installation section

**File**: `README.md`

**Intent**: State that pre-built binaries now include 32-bit ARM.

**Contract**: Update the Installation paragraph (currently "Pre-built static binaries for Linux, macOS, and Windows (amd64/arm64) are published on every `v*` tag.") to include `armv7`, e.g. "(amd64/arm64/armv7)".

#### 2. README Release section

**File**: `README.md`

**Intent**: Keep the Release section consistent with the new matrix.

**Contract**: Update the sentence "GoReleaser publishes static binaries for Linux, macOS, and Windows (amd64/arm64) to the Releases page." to include `armv7`.

#### 3. AGENTS.md Release section

**File**: `AGENTS.md`

**Intent**: Keep the contributor guide's build-matrix description accurate.

**Contract**: Update "`@goreleaser.yaml` defines the build matrix (Linux/macOS/Windows, amd64/arm64)." to include `armv7`.

### Success Criteria:

#### Automated Verification:

- Docs mention armv7: `rtk grep -q "armv7" README.md AGENTS.md`

#### Manual Verification:

- The three sections read naturally and consistently.

**Implementation Note**: After completing this phase and all automated verification passes, pause here for manual confirmation from the human that the manual testing was successful before proceeding to the next phase.

---

## Phase 3: Add GoReleaser Config Validation to CI

### Overview

Add a `goreleaser check` step to the `mage ci` pipeline so an invalid matrix combination fails before a release tag is cut.

### Changes Required:

#### 1. New `GoreleaserCheck` mage target

**File**: `magefiles/mage.go`

**Intent**: Provide a target that validates `.goreleaser.yaml`, mirroring the install-if-missing pattern of the existing `Goreleaser()` target.

**Contract**: New function `GoreleaserCheck() error` that installs GoReleaser if absent (using `toolVersionGoreleaser`) and runs `goreleaser check`. Same shape as `Goreleaser()` at `magefiles/mage.go:604-617`, but with no `ARGS` passthrough and the `check` subcommand.

#### 2. Wire it into the CI pipeline

**File**: `magefiles/mage.go`

**Intent**: Run the validation as part of `mage ci`.

**Contract**: Add a `{"GoReleaser Check", GoreleaserCheck}` entry to the `steps` slice in `CI()` (`magefiles/mage.go:391-422`), placed after `Generate Check` and before `Test`. Update the `CI()` doc comment to mention the new step.

#### 3. Help output

**File**: `magefiles/mage.go`

**Intent**: Keep `mage help` accurate.

**Contract**: Add a `goreleaserCheck` line to the Docker/CI section of `Help()` (`magefiles/mage.go:80-90`), alongside the existing `goreleaser` entry.

### Success Criteria:

#### Automated Verification:

- `mage goreleaserCheck` exits 0: `mage goreleaserCheck`
- `mage ci` runs the new step: `mage ci`
- magefiles still compile and the target is listed: `mage -l`

#### Manual Verification:

- `mage ci` output shows the `GoReleaser Check` step passing.

**Implementation Note**: After completing this phase and all automated verification passes, pause here for manual confirmation from the human that the manual testing was successful before proceeding to the next phase.

---

## Phase 4: Re-cut the v0.1.0 Release

### Overview

Force-move the `v0.1.0` tag to the new HEAD (which now carries the armv7 matrix) and verify the published armv7 binary on the target device.

### Changes Required:

#### 1. Re-publish the release at the new HEAD

**File**: none (git/GitHub operations)

**Intent**: Replace the existing `v0.1.0` release so it includes the armv7 asset, matching the tag-at-HEAD convention the user already established.

**Contract**: Commit Phases 1–3 first, then:
1. `gh release delete v0.1.0 --yes --cleanup-tag` (removes the stale release + tag).
2. `rtk git tag -f -a v0.1.0 -m "Initial release" <new-HEAD>`.
3. `rtk git push origin v0.1.0` (re-creates the tag; triggers `.github/workflows/release.yml`).

No `.goreleaser.yaml` change is needed here — the workflow reads the committed config.

#### 2. Verify the armv7 binary on the device

**File**: none

**Intent**: Prove the released armv7 binary runs on `192.168.0.2`.

**Contract**: Download `freedius_0.1.0_linux_arm.tar.gz` from the release, extract, copy to the device, and run `freedius --version` (expect exit 0). Optionally start it and `curl` the health endpoint.

### Success Criteria:

#### Automated Verification:

- Release run succeeds: `rtk gh run view <run-id> --json status,conclusion`
- Release has 7 assets including the armv7 tarball: `rtk gh release view v0.1.0`

#### Manual Verification:

- `freedius_0.1.0_linux_arm.tar.gz` extracts to a working `freedius` binary.
- On `192.168.0.2`, `./freedius --version` prints a version and exits 0.
- Optionally: freedius starts and `curl -s http://127.0.0.1:8082/health` returns `{"status":"ok"}`.

---

## Testing Strategy

### Unit Tests:

- No Go unit tests are affected — this change touches no Go source.
- The new mage target is exercised by `mage goreleaserCheck` and `mage ci`.

### Integration Tests:

- `goreleaser build --snapshot --clean` proves the full 7-target matrix compiles.
- The re-cut release proves the tag-triggered workflow publishes all 7 assets.

### Manual Testing Steps:

1. `goreleaser build --snapshot --clean` → confirm 7 binaries, `file` on the armv7 one.
2. Read the updated README/AGENTS sections.
3. `mage ci` → confirm the GoReleaser Check step passes.
4. After the re-cut, download the armv7 tarball and run it on the device.

## Performance Considerations

- GoReleaser builds in parallel; the extra `linux/arm` target adds roughly 20–50s to the release job (currently ~57s for 6 builds).
- `mage ci` gains a `go install goreleaser` step (~20–40s) unless GoReleaser is already cached in `~/go/bin` (CI caches that path in `ci.yml:29-35`).
- The armv7 binary is ~22 MB, comparable to the existing artifacts.

## Migration Notes

- No data or configuration migration.
- Existing users are unaffected: `amd64`/`arm64` artifacts keep their names.
- The re-cut replaces the `v0.1.0` release assets in place (same tag, same filenames, new content).

## References

- Related research: `context/changes/armv7-release-matrix/research.md`
- Current matrix: `.goreleaser.yaml:20-26`
- Release workflow: `.github/workflows/release.yml`
- CI pipeline: `magefiles/mage.go:391-422`
- Prior distribution work: `context/archive/2026-07-22-solo-dev-distribution/plan.md`

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles. See `references/progress-format.md`.

### Phase 1: Extend the GoReleaser Release Matrix

#### Automated

- [x] 1.1 `goreleaser check` exits 0 — 7faa4f5
- [x] 1.2 Snapshot build produces 7 binaries including a 32-bit ARM one — 7faa4f5
- [x] 1.3 The armv7 binary is a static 32-bit ARM ELF — 7faa4f5
- [x] 1.4 `go build ./...` still succeeds — 7faa4f5

#### Manual

- [ ] 1.5 `dist/` contains exactly 7 binaries; `freedius_linux_arm` is 32-bit ARM, statically linked
- [ ] 1.6 `goreleaser check` reports no errors

### Phase 2: Update Platform Documentation

#### Automated

- [x] 2.1 Docs mention armv7

#### Manual

- [ ] 2.2 The three sections read naturally and consistently

### Phase 3: Add GoReleaser Config Validation to CI

#### Automated

- [ ] 3.1 `mage goreleaserCheck` exits 0
- [ ] 3.2 `mage ci` runs the new step
- [ ] 3.3 magefiles still compile and the target is listed

#### Manual

- [ ] 3.4 `mage ci` output shows the `GoReleaser Check` step passing

### Phase 4: Re-cut the v0.1.0 Release

#### Automated

- [ ] 4.1 Release run succeeds
- [ ] 4.2 Release has 7 assets including the armv7 tarball

#### Manual

- [ ] 4.3 `freedius_0.1.0_linux_arm.tar.gz` extracts to a working `freedius` binary
- [ ] 4.4 On `192.168.0.2`, `./freedius --version` prints a version and exits 0
- [ ] 4.5 Optionally: freedius starts and `curl -s http://127.0.0.1:8082/health` returns `{"status":"ok"}`

---
date: 2026-10-25T09:27:50+01:00
researcher: pfrack
git_commit: c60a678512cce5696c96f2a7dee901b7409cdb1e
branch: main
repository: freedius
topic: "Add cross-compilation for ARM devices (armv7) to the release matrix"
tags: [research, release, goreleaser, cross-compilation, armv7, armbian, distribution]
status: complete
last_updated: 2026-10-25
last_updated_by: pfrack
---

# Research: Add cross-compilation for ARM devices (armv7) to the release matrix

**Date**: 2026-10-25T09:27:50+01:00
**Researcher**: pfrack
**Git Commit**: c60a678 (HEAD, local — `origin/main` is at `bc6b2b9`; permalinks below point at `bc6b2b9` so they resolve)
**Branch**: main
**Repository**: freedius

## Research Question

> "add crosscompilation for arm devices etc or add to releases and move release"

Clarified with the user:

- **Scope**: extend the release matrix (not a new distribution channel).
- **Targets**: the ARM family — add 32-bit `armv7` alongside the existing `arm64`.
- **Move release**: keep the existing tag-triggered workflow (`push: tags: v*`).
- **Concrete target device**: an Armbian box at `192.168.0.2` (reachable via `ssh u1a`), which the user says is the main reason for this work.

## Summary

The target device is **32-bit ARMv7 (`armv7l`) with a hard-float `armhf` userland** (Allwinner sun4i/sun5i). The current release matrix builds **no 32-bit ARM at all** — only `linux/darwin/windows × amd64/arm64` (`.goreleaser.yaml:20-26`). The device has **no Go toolchain**, so a pre-built binary from GitHub Releases is the only realistic install path.

Adding armv7 is a **~5-line change to `.goreleaser.yaml`** — no source changes, no Go toolchain change, no workflow change:

```yaml
    goarch:
      - amd64
      - arm64
      - arm
    goarm:
      - "7"
    ignore:
      - goos: darwin
        goarch: arm
```

**Verified empirically**: a locally cross-compiled `linux/arm` binary (both `GOARM=7` soft-float and `GOARM=7,hardfloat`) was copied to the device and ran successfully (`freedius --version` → exit 0). `goreleaser check` passes on the proposed config. The only remaining work is the config edit plus doc updates (`README.md:25-27`, `README.md:284`, `AGENTS.md:55`).

## Detailed Findings

### 1. Current release pipeline

The release pipeline already exists and works; it is **tag-triggered** (the user's "move release" concern is already satisfied).

- **Build matrix** — `.goreleaser.yaml:20-26`: `goos: [linux, darwin, windows]`, `goarch: [amd64, arm64]`. **No `arm` (32-bit), no `goarm`.** 6 artifacts.
- **Build flags** — `.goreleaser.yaml:10-19`: `CGO_ENABLED=0`, `-tags=netgo,osusergo`, `-trimpath`, `-gcflags=-l`, `-s -w` ldflags with `version`/`commit`/`date` injection. Pure Go; cross-compiles cleanly.
- **Archives** — `.goreleaser.yaml:28-38`: `tar.gz` for Unix, `zip` for Windows via `format_overrides`; `name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"`.
- **Checksums** — `.goreleaser.yaml:40-41`: single `freedius_<version>_checksums.txt` covering all archives.
- **Release** — `.goreleaser.yaml:43-46`: GitHub `pfrack/freedius`.
- **Trigger** — `.github/workflows/release.yml:3-6`: `on: push: tags: ['v*']`; `contents: write` (line 9); Go `1.27.2` (line 12); `goreleaser/goreleaser-action@v6` pinned to `v2.17.1` (lines 31-34).
- **CI** — `.github/workflows/ci.yml` does **not** touch releases or cross-compilation (runs `mage ci` on `ubuntu-latest`).
- **Mage** — `magefiles/mage.go:25` pins `toolVersionGoreleaser = "v2.17.1"`; `Goreleaser()` target at `magefiles/mage.go:604-617`. `Build()` (`:178-193`) is native-only (no `GOOS`/`GOARCH`).
- **Docker** — `Dockerfile:8-13` hardcodes `GOOS=linux`, no `GOARCH`, no `--platform`; single-arch. `mage DockerBuild/DockerPush` are local-only.

No `ignore:` block, no build `id`, no platform-specific exclusions exist today.

### 2. Target device (empirical probe)

Probed live over SSH (`ssh u1a` → `pawel@192.168.0.2`, key `~/.ssh/oracle-hunter`):

| Property | Value |
|---|---|
| Kernel | `Linux measyu1a 6.18.58-current-sunxi` |
| Machine | **`armv7l`** (32-bit ARM) |
| Board | `Hardware: Allwinner sun4i/sun5i Families`, `ARMv7 Processor rev 2 (v7l)` |
| Float ABI | **`armhf`** (`dpkg --print-architecture` = `armhf`) |
| CPU features | `half thumb fastmult vfp edsp neon vfpv3 tls vfpd32` (VFPv3 + NEON) |
| RAM | 991 MiB total, ~909 MiB available, 1 GiB swap |
| Disk | 29 GB, 26 GB free |
| Tooling | `curl`, `wget`, `tar` present; **no `go`** installed |
| freedius | **not running**; ports 8082/8083 not listening |

Implication: `go install` is not viable on the device (no Go toolchain, ~1 GB RAM). A **pre-built GitHub Release binary** is the intended distribution path.

### 3. Cross-compilation verification (empirical)

Built from `main` and copied to the device:

```bash
GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build -tags netgo,osusergo -o freedius_armv7 ./cmd/freedius
GOOS=linux GOARCH=arm GOARM=7,hardfloat CGO_ENABLED=0 go build -tags netgo,osusergo -o freedius_armv7_hf ./cmd/freedius
scp freedius_armv7 u1a:/tmp/ && ssh u1a '/tmp/freedius_armv7 --version'
```

Results:

- Both binaries are `ELF 32-bit LSB executable, ARM, EABI5, statically linked`.
- **Both run on the device**: `freedius v0.1.1-0.20261016201418-c60a678512cc+dirty`, exit 0.
- No build tags, assembly, `unsafe`, or cgo constraints block 32-bit ARM (`go.mod` has no CGO-relevant deps).

Conclusion: **the only blocker is the release matrix itself** — the toolchain and source already produce working armv7 binaries.

### 4. GoReleaser config change (docs-backed)

GoReleaser v2.17.1 supports the needed knobs (Context7 `/goreleaser/goreleaser`, `builds` → Go builder):

- `goarm` — "GOARM to build for when GOARCH is arm." Default is `[6]`. **Explicit `goarm: ["7"]` is required** for armv7 (default 6 would target ARMv6).
- Float ABI — since Go 1.22 / GoReleaser v2.17, `goarm` accepts `"7,softfloat"` / `"7,hardfloat"`. Only one ABI per GOARM version; `ignore` must match the exact form.
- `ignore` — "List of combinations of GOOS + GOARCH + GOARM to ignore." Example includes `- goos: darwin, goarch: 386`.

**Valid 32-bit ARM targets:** `linux/arm` ✅, `windows/arm` ✅ (Go supports it since 1.17). **`darwin/arm` ❌ — never a valid Go target** (macOS has no 32-bit ARM). Adding `arm` to `goarch` therefore **requires ignoring `darwin`+`arm`**, otherwise the matrix generation fails.

**Proposed minimal diff** (validated with `goreleaser check` → `1 configuration file(s) validated`, exit 0):

```yaml
    goarch:
      - amd64
      - arm64
      - arm
    goarm:
      - "7"
    ignore:
      - goos: darwin
        goarch: arm
```

Resulting matrix: `linux/{amd64,arm64,arm}`, `darwin/{amd64,arm64}`, `windows/{amd64,arm64,arm}` → **8 artifacts** (was 6). Add `- goos: windows, goarch: arm` to `ignore` if 32-bit Windows ARM is not wanted (it is rarely used).

**Float ABI recommendation:** the device is `armhf`, so `goarm: ["7,hardfloat"]` is the native ABI. However `GOARM=7` (soft-float) was **verified to run on the device** and is the more broadly compatible choice (runs on both `armhf` and `armel`). Either is valid; pick one.

### 5. Archive naming and checksum implications

- `name_template` uses `.Os` and `.Arch` only → armv7 artifacts are named `freedius_<version>_linux_arm.tar.gz` / `..._windows_arm.zip`. No collision with arm64 (different `.Arch`).
- If more than one `goarm` value is ever added, append `{{ .Arm }}` to the template (e.g. `..._{{ .Arch }}{{ .Arm }}`) to disambiguate — with a single value it is unnecessary.
- Checksums: one `_checksums.txt` automatically covers all archives; no change needed.
- Archive `files` (`LICENSE`, `README.md`) and `format_overrides` are unchanged.

### 6. Historical context (from prior changes)

- **Matrix was a coverage default, not a researched decision.** `context/archive/2026-07-22-solo-dev-distribution/plan.md:5` and `research.md:188-189` fixed `linux/darwin/windows × amd64/arm64`. `plan-brief.md:30` calls it "Covers all major platforms and architectures". **`armv7` was never considered or explicitly excluded** — searches for `armv7|armv6|GOARM|32-bit` across `context/` return zero matches.
- **Tag-triggered workflow is present and works.** `plan.md:217-229` specified `push: tags: ['v*']`; `release.yml:3-6` implements it. The first run failed only because GoReleaser `v2.6.2` 404'd; fixed to `v2.17.1` in commit `926051f`. Tag `v0.1.0` (2026-08-10) produced 6 assets + checksums.
- **Explicitly deferred, still not done** (`plan.md:35-41`, `plan-brief.md:42-45`, `readme-ready-to-sell/plan.md:57-58`): Homebrew tap, Scoop bucket, Docker publishing, `freedius update`, `install.sh`. None are in `.goreleaser.yaml`.
- **Release has always been kept separate from CI** (`frame.md:133`, `plan-brief.md:31`). No prior proposal to "move/merge" it exists — the user's "move release" maps to the already-correct tag-triggered `release.yml`.

### 7. Documentation to update

The current docs claim only `amd64/arm64`:

- `README.md:25-27` — "Pre-built static binaries for Linux, macOS, and Windows (amd64/arm64) are published on every `v*` tag."
- `README.md:284` — "GoReleaser publishes static binaries for Linux, macOS, and Windows (amd64/arm64) to the Releases page."
- `AGENTS.md:55` — "`@goreleaser.yaml` defines the build matrix (Linux/macOS/Windows, amd64/arm64)."

Update to include `armv7` (e.g. `amd64/arm64/armv7`), or phrase as "including 32-bit ARM (armv7)".

## Code References

- `.goreleaser.yaml:20-26` — current build matrix (no 32-bit ARM)
- `.goreleaser.yaml:10-19` — CGO/tags/ldflags flags
- `.goreleaser.yaml:28-41` — archives, checksums
- `.goreleaser.yaml:43-46` — GitHub release target
- `.github/workflows/release.yml:3-6` — tag trigger `v*`
- `.github/workflows/release.yml:30-34` — GoReleaser action pinned `v2.17.1`
- `magefiles/mage.go:25` — `toolVersionGoreleaser = "v2.17.1"`
- `magefiles/mage.go:604-617` — `Goreleaser()` target
- `magefiles/mage.go:178-193` — native-only `Build()`
- `Dockerfile:8-13` — single-arch `GOOS=linux` build
- `README.md:25-27`, `README.md:284`, `AGENTS.md:55` — platform claims to update
- `go.mod:3` — Go 1.27.2 (supports `GOARM=7,hardfloat` since 1.22)
- `context/archive/2026-07-22-solo-dev-distribution/plan.md:5,35-41,217-229` — original matrix + deferred items + tag workflow
- `context/archive/2026-07-22-solo-dev-distribution/research.md:188-216` — distribution research, 6-artifact matrix

## Architecture Insights

- The project's "single static binary, zero external runtime dependencies" promise (`.goreleaser.yaml:11`, `CGO_ENABLED=0`, `netgo,osusergo`) makes adding architectures a **pure packaging change** — the Go cross-compiler handles it with no source edits.
- GoReleaser's default `goarm` is `6`; for an ARMv7 target you must opt into `7` explicitly, or you ship an ARMv6 binary to an ARMv7 device.
- `darwin/arm` is the one matrix combination that must be excluded; `windows/arm` and `linux/arm` are valid. This is a common footgun when adding `arm` to `goarch`.
- Because the device has no Go, the release matrix (not `go install`) is the distribution surface — which is exactly why the matrix, not the toolchain, is the fix.

## Historical Context (from prior changes)

- `context/archive/2026-07-22-solo-dev-distribution/change.md` — the change that introduced GoReleaser, the 6-platform matrix, and the tag workflow; archived 2026-07-31.
- `context/archive/2026-07-22-solo-dev-distribution/frame.md` — reframe that re-opened first-time-adopter distribution after `solo-dev-positioning` had narrowed the persona.
- `context/archive/2026-07-21-solo-dev-positioning/frame.md` — predecessor that excluded cold-start distribution.
- `context/archive/2026-07-02-web-ui/plan.md` — introduced the Dockerfile and locked the `CGO_ENABLED=0` / `netgo,osusergo` flags that GoReleaser later mirrored.
- `context/archive/2026-07-31-readme-ready-to-sell/plan.md` — cut the first `v0.1.0` tag and verified the release end-to-end; reaffirmed no Homebrew/Scoop/Docker publishing.
- `context/archive/2026-07-02-quality-gates-in-ci/plan.md` — noted no semver tags existed at the time; release workflow is separate from CI.

## Related Research

- `context/archive/2026-07-22-solo-dev-distribution/research.md` — distribution options and the original GoReleaser config (6 artifacts).
- `context/archive/2026-07-22-solo-dev-distribution/plan.md` — the implementation plan that produced the current `.goreleaser.yaml` and `release.yml`.

## Open Questions

1. **Soft-float vs hard-float**: ship `goarm: ["7"]` (broad compatibility, verified) or `goarm: ["7,hardfloat"]` (native `armhf` ABI, verified)? Recommendation: `7,hardfloat` for the armhf target, or `7` if `armel` compatibility matters.
2. **Windows ARM**: include `windows/arm` (adds a 32-bit Windows ARM artifact) or ignore it? Recommendation: ignore unless a user asks.
3. **Docker on the device**: the device is 32-bit; the current `Dockerfile` is linux/amd64-only. If freedius is ever containerized on `192.168.0.2`, a `linux/arm/v7` image is a separate future change.
4. **Verification in CI**: should `goreleaser check` (and/or `goreleaser build --snapshot`) be added to `mage ci` to catch invalid matrix combos before a tag?

## Verification Log

| Check | Result |
|---|---|
| `GOOS=linux GOARCH=arm GOARM=7 CGO_ENABLED=0 go build` | ✅ static ELF 32-bit ARM EABI5 |
| `GOOS=linux GOARCH=arm GOARM=7,hardfloat CGO_ENABLED=0 go build` | ✅ static ELF 32-bit ARM EABI5 |
| Copy to `192.168.0.2`, run `--version` (soft-float) | ✅ exit 0, `freedius v0.1.1-...+dirty` |
| Copy to `192.168.0.2`, run `--version` (hard-float) | ✅ exit 0, `freedius v0.1.1-...+dirty` |
| `goreleaser check` on proposed config (v2.17.1) | ✅ `1 configuration file(s) validated` |
| Device probe (`uname -m`, `dpkg --print-architecture`, `/proc/cpuinfo`) | ✅ `armv7l` / `armhf` / VFPv3+NEON |

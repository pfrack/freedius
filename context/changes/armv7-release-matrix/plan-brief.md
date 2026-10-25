# armv7 Release Matrix — Plan Brief

> Full plan: `context/changes/armv7-release-matrix/plan.md`
> Research: `context/changes/armv7-release-matrix/research.md`

## What & Why

Add 32-bit ARM (`armv7`, hard-float) to the GoReleaser release matrix so the pre-built `freedius` binary runs on the user's Armbian `armhf` device at `192.168.0.2`. The device has no Go toolchain, so a GitHub Release binary is the only viable install path — but the current matrix ships no 32-bit ARM at all. Cross-compilation is already verified; only the release matrix is missing.

## Starting Point

The release pipeline exists and is tag-triggered: `.goreleaser.yaml:20-26` builds `linux/darwin/windows × amd64/arm64` (6 artifacts), published by `.github/workflows/release.yml` on every `v*` tag. GoReleaser is pinned to `v2.17.1`, which supports `goarm` float-ABI suffixes. A locally cross-compiled `linux/arm` binary (both `GOARM=7` and `GOARM=7,hardfloat`) already ran on the device.

## Desired End State

Every `v*` tag produces **7** artifacts — `linux/{amd64,arm64,arm}`, `darwin/{amd64,arm64}`, `windows/{amd64,arm64}` — where `linux_arm` is a 32-bit ARMv7 hard-float binary. The `v0.1.0` release is re-published with the armv7 asset, the docs advertise `armv7`, and `mage ci` validates the GoReleaser config so an invalid matrix combination fails before a tag is cut.

## Key Decisions Made

| Decision | Choice | Why (1 sentence) | Source |
| --- | --- | --- | --- |
| Float ABI | `7,hardfloat` | Native hard-float ABI for the device's `armhf` userland; verified running. | Plan |
| Windows ARM | Ignore | 32-bit Windows-on-ARM is essentially unused; keeps the release at 7 artifacts. | Plan |
| Docker | Out of scope | The immediate need is a binary; a `linux/arm/v7` image is a separate change. | Plan |
| Release strategy | Re-cut `v0.1.0` | Matches the user's established tag-at-HEAD convention; one version, always current. | Plan |
| CI validation | Add `goreleaser check` | Catches invalid matrix combos (e.g. `darwin/arm`) before a tag is cut; cheap. | Plan |
| Scope | Extend matrix only | No source changes; `CGO_ENABLED=0` + `netgo,osusergo` make it pure packaging. | Research |
| Target device | `armv7l`/`armhf` Armbian | The concrete reason for the change; no Go toolchain on it. | Research |

## Scope

**In scope:**
- `.goreleaser.yaml`: add `arm` to `goarch`, `goarm: ["7,hardfloat"]`, and `ignore` for `darwin`+`arm` and `windows`+`arm`.
- Docs: `README.md` Installation + Release sections, `AGENTS.md` Release section.
- `magefiles/mage.go`: new `GoreleaserCheck()` target wired into `CI()` + Help output.
- Re-cut the `v0.1.0` release and verify the armv7 binary on the device.

**Out of scope:**
- Docker multi-arch / `linux/arm/v7` image.
- Other architectures (`386`, `riscv64`, `ppc64le`, `s390x`, `mips`).
- Windows ARM.
- Homebrew / Scoop / Nix / self-update.
- A second (soft-float) armv7 ABI.

## Architecture / Approach

Config-only change. Phase 1 edits the GoReleaser `builds[0]` matrix and validates locally with `goreleaser check` + `goreleaser build --snapshot`. Phase 2 corrects the three doc claims. Phase 3 adds `goreleaser check` to the `mage ci` pipeline. Phase 4 deletes and re-publishes the `v0.1.0` release at the new HEAD, then downloads the armv7 tarball and runs it on the device. No Go source is touched.

## Phases at a Glance

| Phase | What it delivers | Key risk |
| --- | --- | --- |
| 1. Extend the GoReleaser release matrix | 7-target matrix incl. `linux/arm` (armv7 hard-float) | Forgetting to `ignore` `darwin`+`arm` → matrix generation fails |
| 2. Update platform documentation | README + AGENTS advertise armv7 | Doc drift if the matrix changes later |
| 3. Add GoReleaser config validation to CI | `goreleaser check` in `mage ci` | Slower CI from the GoReleaser install step |
| 4. Re-cut the v0.1.0 release | armv7 binary published + verified on device | Destructive: replaces 6 published assets with 7 |

**Prerequisites:** Phases 1–3 committed and green before Phase 4. GoReleaser `v2.17.1` available locally (`go install github.com/goreleaser/goreleaser/v2@v2.17.1`).
**Estimated effort:** ~1 session across 4 phases (the config change is ~10 lines).

## Open Risks & Assumptions

- The re-cut replaces the published `v0.1.0` assets; anyone who already downloaded the old 6 binaries will see new content under the same filenames. Acceptable per the user's tag-at-HEAD intent.
- `goreleaser build --snapshot` in Phase 1 needs network access for module downloads (cached after the first run).
- CI's `go install goreleaser` adds time unless `~/go/bin` is cached (`ci.yml:29-35` already caches it).

## Success Criteria (Summary)

- `v0.1.0` publishes 7 assets, including a working `freedius_0.1.0_linux_arm.tar.gz`.
- The armv7 binary runs on `192.168.0.2` (`./freedius --version` exits 0).
- `mage ci` fails on an invalid GoReleaser matrix before a tag is cut.

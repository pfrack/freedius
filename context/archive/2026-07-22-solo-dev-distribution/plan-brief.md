# GoReleaser Integration — Plan Brief

> Full plan: `context/changes/solo-dev-distribution/plan.md`
> Research: `context/changes/solo-dev-distribution/research.md`

## What & Why

Add GoReleaser to freedius to automate building and publishing static binaries for 6 platforms (linux/darwin/windows × amd64/arm64) on `git tag`. This replaces the current manual `go build` / `docker build` workflow and enables `go install @latest` to display the correct version instead of "dev".

## Starting Point

freedius currently has zero distribution automation. The only install path is `go build` from source (9 steps, ~10-20 min). The `version` variable at `cmd/freedius/main.go:45` is hardcoded to `"dev"` and never injected. The Dockerfile at `Dockerfile:8-13` already uses the correct build flags (`CGO_ENABLED=0`, `netgo,osusergo`, `-s -w`, `-trimpath`), but these aren't applied in the magefiles `Build` target or any release process. CI (`.github/workflows/ci.yml`) covers test/lint only — no release workflow exists.

## Desired End State

When a maintainer pushes a git tag `v0.1.0`:
1. GitHub Actions release workflow triggers
2. GoReleaser builds 6 static binaries with correct version injection
3. Archives (tar.gz for Unix, zip for Windows) + checksums are uploaded to GitHub Releases
4. `freedius --version` prints the tag (e.g., `v0.1.0`) for both GoReleaser builds and `go install @latest`
5. `mage build` still works for local development with version info

## Key Decisions Made

| Decision | Choice | Why | Source |
|---|---|---|---|
| Version detection | `debug.ReadBuildInfo()` fallback | `go install @latest` works immediately after first tag with no ldflags dependency; version display is independent of build method | Plan |
| Docker in GoReleaser | No — keep separate | Minimal change to existing Docker workflow; Dockerfile stays as source of truth | Plan |
| Archive formats | tar.gz (Unix), zip (Windows) | Matches standard Go CLI distribution conventions | Plan |
| Platforms | linux/darwin/windows × amd64/arm64 | Covers all major platforms and architectures | Plan |
| CI trigger | `push: tags: ['v*']` + goreleaser-action | Standard GoReleaser pattern; clean separation from ci.yml | Plan |

## Scope

**In scope:**
- `debug.ReadBuildInfo()` version fallback in `main.go`
- `.goreleaser.yaml` config (builds, archives, checksums, release)
- `.github/workflows/release.yml` (tag-triggered release workflow)
- Magefile updates (goreleaser target, enhanced Build with version injection)

**Out of scope:**
- Homebrew tap, Scoop bucket, uvx distribution
- Docker image publishing via GoReleaser
- Self-update subcommand (`freedius update`)
- `install.sh` script

## Architecture / Approach

GoReleaser builds 6 binaries in parallel on tag push, using the same build flags already established in the Dockerfile. Version is injected via ldflags (`-X github.com/pfrack/freedius/cmd/freedius.version={{.Version}}`), with a `debug.ReadBuildInfo()` fallback in `main.go` for non-GoReleaser builds (like `go install @latest`). Docker image building remains separate — GoReleaser produces binaries only, the existing Dockerfile + mage targets handle container builds.

## Phases at a Glance

| Phase | What it delivers | Key risk |
|---|---|---|
| 1. Version Detection | `getVersion()` with `debug.ReadBuildInfo()` fallback | Fallback may return `(devel)` for `go run` builds |
| 2. .goreleaser.yaml | Build config, archives, checksums, release | Config must match Dockerfile flags exactly |
| 3. Release CI | Tag-triggered workflow with goreleaser-action | Workflow may fail on first tag if permissions misconfigured |
| 4. Magefiles | Local goreleaser target + enhanced Build | Enhanced Build may break if git describe fails |

**Prerequisites:** None — all changes are additive
**Estimated effort:** ~2-3 sessions across 4 phases

## Open Risks & Assumptions

- GoReleaser version (`v2.6.2`) may need updating at implementation time
- The release workflow's `GITHUB_TOKEN` permissions may need adjustment if the repo has restricted settings
- First tag push will create the first GitHub Release — maintainer should verify before tagging

## Success Criteria (Summary)

- `freedius --version` prints correct version for both GoReleaser builds and `go install @latest`
- `goreleaser build --clean` produces 6 binaries + archives in `dist/`
- Tag push triggers release workflow and creates GitHub Release with archives + checksums
- `mage build` produces a binary with version info

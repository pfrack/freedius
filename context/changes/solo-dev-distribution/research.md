---
date: 2026-07-22T00:40:00+02:00
researcher: pfrack
git_commit: cf029c56beed8736f7c293381b7d03df0786df1c
branch: auto-review
repository: freedius
topic: "Should freedius have brew, npx, etc. for solo dev distribution?"
tags: [research, distribution, dx, packaging, goreleaser, homebrew, onboarding, cold-start, uvx, go-install, docker]
status: complete
last_updated: 2026-07-22T12:30:00+02:00
last_updated_by: pfrack
---

# Research: Should freedius have brew, npx, etc. for solo dev distribution?

**Date**: 2026-07-22
**Researcher**: pfrack
**Git Commit**: cf029c5
**Branch**: auto-review
**Repository**: freedius

## Research Question

"Shouldn't we have brew, npx etc for solo dev" — i.e., should freedius add package-manager distribution (Homebrew, npx, scoop, etc.) to ease installation for its solo-developer user?

## Summary

**The project has explicitly rejected cold-start distribution as a problem worth solving — twice.** The framing (`context/changes/solo-dev-positioning/frame.md:96-98`) defines DX as "legibility of system state for the returning maintainer," not first-time adoption. No distribution mechanism (brew, npx, goreleaser, install.sh) has ever been proposed in any change, plan, or foundation document. The sole exception — a `daemon-mode` plan that mentioned Homebrew as a deployment path for `os.Executable()` resolution (`context/archive/2026-06-21-daemon-mode/plan.md:217`) — was about the developer's own install, not end-user distribution.

**However, there is a real gap.** Today's only install path is `go build` from a clone — 9 steps, ~10–20 min, Go 1.26.x + git + GOPATH + API key required. No binary releases, no Homebrew, no installer script, no published Docker image. The README's own "single static binary, zero external runtime dependencies" tagline promises something the install flow doesn't deliver. A reviewer comparing freedius to peer tools (age, gost, mitmproxy, ngrok) will notice the gap.

**If distribution is pursued, GoReleaser + a Homebrew tap is the highest-leverage, lowest-maintenance path** — 1 YAML file, triggered on `git tag`, zero ongoing maintenance. Comparable Go proxy `gost` already uses this pattern. The npm/npx path is the wrong audience and requires GoReleaser Pro. **uvx** (Astral's Python tool runner) is technically viable via a per-platform Python wheel with embedded binaries, but the maintenance overhead (~5 wheel files, ~21MB each, synced versions) and **audience mismatch** (Go devs vs Python tool runners) make it impractical.

## Detailed Findings

### 1. Existing research context — distribution was deliberately rejected

The strongest signal is the **explicit, documented rejection** of the cold-start framing:

> *"The README rephrase is bounded by the lock that the maintainer IS the end user — no cold-start persona write-up, no alternative-tool pitch. It answers legibility of the system as built, not adoption."*
> — `context/changes/solo-dev-positioning/frame.md:96-98`

> *"Each earlier framing (README positioning, cold-start arc, PRD drift) was a reading-the-code shape that did not match the actual user pain."*
> — `context/changes/solo-dev-positioning/frame.md:52-54`

The user's own framing shifted: `cold-start-onboarding → resumability → "I reached for something and it didn't behave as I expected" → "where did this mapping come from?"` (`frame.md:48-51`). The project chose **resumability** as the real DX problem.

A `freedius init` subcommand was planned in `error-hardening` (would have written a starter config, offered `--shell-install`) but was **removed** in `unified-server-logs-tab` because subcommand dispatch was dropped. What survived: the embedded `starter.yaml` auto-written on first run (`cmd/freedius/main.go:155-172`), currently the only first-run DX feature.

### 2. uvx (Astral uv) — not a viable distribution path for freedius

**How uvx works**: `uvx` is Astral's `uv tool run` — equivalent to `pipx run`. It installs a Python package into a temporary isolated virtualenv and discovers executables via PEP 621 `[project.scripts]` (console_scripts entry points) or setuptools binary scripts. The core mechanism is Python-package-centric.

**How a Go binary could reach uvx**: via a per-platform Python wheel that embeds the pre-built Go binary + a Python wrapper. Each wheel (~21MB binary + ~1MB Python shim) would have a platform tag (`linux_amd64`, `darwin_arm64`, etc.).

**Realistic assessment**: uvx would require 5+ platform-specific wheels, platform-specific CI jobs, PyPI publishing, and version sync between Go and Python. Astral has **no announced mechanism** for distributing non-Python executables through uv. The `astral-sh/self-extras` repo referenced in uv docs returns HTTP 404.

**Audience fit**: freedius's target user is a Go-developing AI coding agent user on macOS. That audience is already on GitHub Releases, Homebrew, or `go install`. **The overlap between "Go dev wanting a local proxy" and "Python user running tools via uvx" is negligible.** This is the wrong channel.

**Effort comparison**: uvx + Python wheel requires ~3× the effort of GoReleaser, produces larger artifacts (~21MB per wheel), and reaches nobody in freedius's actual user base. **Verdict: Skip uvx for freedius.** The only scenario where uvx makes sense is if Astral ships a standalone binary mechanism, which is not currently available.

### 3. Persona — consistent "solo dev maintainer"

| Document | Definition |
|---|---|
| `context/foundation/prd.md:26` | "Solo developer using Claude Code... One person, one machine. Terminal-native." |
| `context/foundation/shape-notes.md:45` | Identical |
| `context/foundation/roadmap.md:20` | "Existing solutions are production gateways (overkill for a solo dev's laptop)" |
| `README.md:5` | "a live dashboard for the solo-dev maintainer" |

One persona attribute is decisive: **"returning after a gap"** — the actual friction is resumability ("I don't remember why this mapping exists"), not first-time install. Distribution tooling serves a *different* persona: the first-time adopter the PRD explicitly excluded.

### 4. Current cold-start friction — 9 steps, no shortcuts

**Install paths that exist today:**

| Method | Documented | Reality |
|---|---|---|
| `go build` from source | `README.md:25-27` | ✅ The only real path |
| `go install` / `mage install` | `magefiles/mage.go:144-148` | ✅ Requires Go + mage |
| Docker build + run | `Dockerfile`, `docker-compose.yml` | ⚠️ Builds locally, no published image |
| `go install @latest` | ❌ | **Broken** — version hardcoded as `"dev"`, no git tag yet shipped |
| Pre-built binary release | ❌ | **Missing** — no `.goreleaser.yaml`, no GitHub Releases |
| brew / scoop / nix | ❌ | **Missing** — zero mentions in entire repo (except daemon-mode plan) |

**Step-by-step cold start (source build):**

1. Install Go 1.26.x (`go.mod` pins `go 1.26.5`)
2. Install git
3. Clone repo
4. (Optional) `go install mage`
5. `go build -o freedius ./cmd/freedius`
6. Sign up for an upstream API key
7. `export OPENCODE_API_KEY=...`
8. `./freedius`
9. Verify with curl

**9 steps, 4–6 distinct commands, ~10–20 min** on a fresh machine.

**Bright spot:** the embedded `starter.yaml` (`cmd/freedius/main.go:155-172`) means **no YAML config file is required** — the server starts with zero-config. But the README doesn't surface this; users reach for `config.example.yaml` unnecessarily.

**Latent bug surfacing:** `checkRequiredEnvVars` return value is discarded at `main.go:134` (`_ = checkRequiredEnvVars(cfg)`), so missing API keys produce **no startup warning** — only a silent 401 on the first request. More important than packaging for the first-run experience.

### 5. Docker state — local-only, no published image

**Current Docker state**:

- `Dockerfile:1-18` — 2-stage build: `golang:1.26.4` → `gcr.io/distroless/static-debian12:nonroot`, exposes 8082/8083, `USER nonroot:nonroot`
- `Dockerfile:8-13` — build uses `CGO_ENABLED=0 GOOS=linux`, but **no multi-platform cross-compile** — only builds linux binary
- `docker-compose.yml:1-12` — local dev only, builds from context, no image reference
- `magefiles/mage.go:520-551` — `DockerBuild`, `DockerRun`, `DockerPush` targets exist. `DockerPush` reads `IMAGE_NAME` env var (e.g. `IMAGE_NAME=ghcr.io/pfrack/freedius:v0.1.0 mage dockerPush`)
- **No published image** — zero references to Docker Hub, GHCR, or any registry in the codebase
- CI exists (`.github/workflows/ci.yml`) but covers test/lint only — no Docker publish step
- No `.goreleaser.yml` — no Docker image automation

**What's needed for published Docker images**: `.goreleaser.yml` with a `dockers` block + GitHub Actions release workflow + GHCR package permissions. GoReleaser can automate both binary builds and Docker image pushes to `ghcr.io/pfrack/freedius`.

### 6. Build pipeline — what exists and what's missing for releases

**Current build targets in magefiles**:

| Target | Path | Does it produce release artifact? |
|---|---|---|
| `Build` | `magefiles/mage.go:139-142` | `go build -o freedius ./cmd/freedius` — bare, no CGO_ENABLED, no ldflags, no version |
| `Install` | `magefiles/mage.go:145-148` | `go install ./cmd/freedius` — no version injection |
| `DockerBuild` | `magefiles/mage.go:520-523` | `docker build -t freedius:dev .` — local dev only |
| `DockerPush` | `magefiles/mage.go:540-551` | Needs `IMAGE_NAME` env var, no login/tag |

**Dockerfile build flags** (`Dockerfile:8-13`):
```dockerfile
CGO_ENABLED=0 GOOS=linux \
-ldflags="-s -w" \
-tags netgo,osusergo \
-trimpath
```

- `CGO_ENABLED=0` ✅ static binary
- `GOOS=linux` ❌ hardcoded — no cross-compile
- `-ldflags="-s -w"` ❌ no version/commit/date injection

**Version variable** (`cmd/freedius/main.go:45`):
```go
var version = "dev"
```

- In `package main` — injectable via `-ldflags="-X github.com/pfrack/freedius/cmd/freedius.version=vX.Y.Z"`
- **Currently permanently "dev"** — nothing injects it in any build path

**Cross-compilation**: **None.** No GOOS/GOARCH matrix anywhere. The `mage install` target does `go install ./cmd/freedius` which defaults to local platform only.

**Version test** (`cmd/freedius/main_test.go:261-267`):
```go
cmd := exec.Command("go", "run", ".", "--version")
```
- Runs `go run` → always produces `"dev"` — doesn't verify any version value

### 7. Distribution patterns for Go CLIs (when/if pursued)

If the framing decision is revisited, the researched options ranked by fit:

| Approach | Setup effort | Maintenance | Solo-dev UX | Platform | Fit |
|---|---|---|---|---|---|
| **`go install @latest`** | LOW (tag + go.mod) | LOW | MEDIUM (compiles from source) | All | ★★★★ |
| **GoReleaser + GitHub Releases** | 1 YAML file | Near-zero (tag-driven) | Excellent | macOS/Linux/Windows | ★★★★★ |
| **GoReleaser + Homebrew tap** | +10 lines YAML | Zero (auto-generated formula) | `brew install pfrack/freedius/freedius` | macOS + Linux | ★★★★★ |
| **GoReleaser + Scoop bucket** | +10 lines YAML | Zero (auto-generated manifest) | `scoop install pfrack/freedius` | Windows | ★★★★ |
| **GoReleaser + Nix NUR** | MEDIUM-HIGH | MEDIUM | `nix run pfrack#freedius` | NixOS | ★★★ |
| **uvx via Python wheel** | HIGH (5 wheels) | HIGH (version sync, wheel rebuilds) | `uvx freedius` | All | ★★ (wrong audience) |
| **Chocolatey** | MEDIUM | MEDIUM (review queue) | `choco install` | Windows | ★★★ |
| **MacPorts portfile** | HIGH | HIGH (manual Portfile diff) | LOW — niche | macOS | ★★ |
| **Self-update subcommand** | MEDIUM | MEDIUM | `freedius update` | All | ★★★★ |
| **GitHub Releases only** | Minimal | Low | Manual download | All | ★★★ |

**Comparable tools**: `gost` (Go tunnel/proxy) ships via GoReleaser + Homebrew + Snap + Docker. `age` ships to 14+ package managers via GoReleaser. `mitmproxy` uses pipx but that's a Python project.

### 8. Detailed GoReleaser + package manager configs

#### 8a. Minimal `.goreleaser.yaml`

```yaml
builds:
  - env: [CGO_ENABLED=0]
    flags: [-tags=netgo,osusergo]
    ldflags:
      - -s -w -X github.com/pfrack/freedius/cmd/freedius.version={{.Version}}
      - -X github.com/pfrack/freedius/cmd/freedius.commit={{.Commit}}
      - -X github.com/pfrack/freedius/cmd/freedius.date={{.Date}}
    goos: [linux, darwin, windows]
    goarch: [amd64, arm64]

archives:
  - format: tar.gz
    name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
  - format: zip
    name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    goos: [windows]

checksum:
  name_template: "{{ .ProjectName }}_{{ .Version }}_checksums.txt"

release:
  github:
    owner: pfrack
    repo: freedius

before:
  hooks:
    - go mod tidy
```

**Key notes**:
- `-X github.com/pfrack/freedius/cmd/freedius.version` is the fully qualified path to the `version` variable in `main.go:45`. GoReleaser's `{{.Version}}` comes from the git tag (e.g. `v0.1.0`).
- `netgo,osusergo` tags match the Dockerfile's existing flags (`Dockerfile:10`)
- `CGO_ENABLED=0` produces a truly static binary, matching the project's "zero external dependencies" promise
- No `gomod.proxy` needed — GoReleaser auto-does `go mod download`
- 6 build artifacts (linux/darwin/windows × amd64/arm64)

#### 8b. Homebrew tap (self-hosted)

Create repo `pfrack/homebrew-freedius` (empty repo with README). GoReleaser auto-generates a Ruby formula and pushes it to the tap repo on each release:

```yaml
brews:
  - repository:
      owner: pfrack
      name: homebrew-freedius
      branch: main
    homepage: https://github.com/pfrack/freedius
    description: Local Claude Code proxy with fallback chains and live dashboard
    install: |
      bin.install "freedius"
    test: |
      system "#{bin}/freedius --version"
```

**User installs**: `brew install pfrack/freedius/freedius`

**Extra setup**: Create tap repo, enable `contents: write` permission for the CI workflow.

#### 8c. Scoop bucket

```yaml
scoops:
  - name: freedius
    url_template: "https://github.com/pfrack/freedius/releases/download/{{ .Tag }}/{{ .ArtifactName }}"
    repository:
      owner: pfrack
      name: scoop-freedius
      branch: main
    homepage: "https://github.com/pfrack/freedius"
    description: Local Claude Code proxy with fallback chains and live dashboard
    license: MIT
```

**User installs**: `scoop bucket add pfrack https://github.com/pfrack/scoop-freedius.git && scoop install freedius`

#### 8d. Nix NUR

```yaml
nix:
  - name: freedius
    url_template: "https://github.com/pfrack/freedius/releases/download/{{ .Tag }}/{{ .ArtifactName }}"
    repository:
      owner: pfrack
      name: nur
      branch: main
    homepage: "https://github.com/pfrack/freedius"
    description: Local Claude Code proxy with fallback chains and live dashboard
    license: "mit"
    main_program: "freedius"
```

**Constraints**: Requires `nix-hash` in `$PATH`. Cannot use `archives.format: binary` (must use tar.gz/zip). Cannot compile from source.

### 9. `go install @latest` — the version variable blocks it

**The `var version = "dev"` problem** (`main.go:45`):

When a user runs `go install github.com/pfrack/freedius@latest`, Go compiles the module and runs it — but the `version` variable stays `"dev"` because GoReleaser's `-ldflags` injection only happens during GoReleaser builds, not during `go install`.

**Two solutions**:

**Option A: Runtime version detection via `debug.ReadBuildInfo()`** (no ldflags needed):

```go
import "runtime/debug"

func getVersion() string {
    if version != "dev" {
        return version
    }
    if info, ok := debug.ReadBuildInfo(); ok {
        if v := info.Main.Version; v != "(devel)" && v != "" {
            return v
        }
    }
    return "dev"
}
```

Then `--version` prints the module version (e.g. `v0.1.0` from the module proxy) instead of `"dev"`. This is the cleanest approach — `go install @latest` works immediately after the first tag is pushed.

**Option B: Add `makepkg.go`-style ldflags to the mage build target** (releases only):

In `magefiles/mage.go:141`, change:
```go
return sh.RunV("go", "build", "-o", "freedius", "./cmd/freedius")
```
to inject version from `git describe --tags`:
```go
return sh.RunV("go", "build",
    "-o", "freedius",
    "-ldflags", "-X github.com/pfrack/freedius/cmd/freedius.version="+gitVersion(),
    "-tags", "netgo,osusergo",
    "CGO_ENABLED=0",
    "./cmd/freedius")
```

**Option A is preferred** — it fixes `go install @latest` with no ldflags dependency, and makes version display independent of build method.

### 10. Self-updating binary patterns

Three approaches for adding `freedius update`:

| Approach | Library / Pattern | Setup | Complexity |
|---|---|---|---|
| **`creativeprojects/go-selfupdate`** | Library with GitHub API, binary diff, rollback | Add `freedius update` subcommand | MEDIUM |
| **`rhysd/go-github-selfupdate`** | Simpler library, GitHub-only, no rollback | Add `freedius update` subcommand | LOW-MEDIUM |
| **"Update Available" notice** | Manual — just GitHub API check, print message | ~30 lines in `cmd/freedius/main.go` | MINIMAL |

**"Update Available" pattern** (minimal implementation):
```go
func checkForUpdate(logger *slog.Logger) {
    client := &http.Client{Timeout: 5 * time.Second}
    req, _ := http.NewRequest("GET", "https://api.github.com/repos/pfrack/freedius/releases/latest", nil)
    req.Header.Set("User-Agent", "freedius")
    resp, err := client.Do(req)
    // parse "tag_name" from JSON → semver compare → print "Update available: v0.2.0 → ..."
}
```

The `version` variable must become a proper semver string for any self-update pattern to work. Currently it's `"dev"` which is not a valid semver.

### 11. CI state — existing workflows

Two GitHub Actions workflows exist:

- `.github/workflows/ci.yml:1-46` — test/lint pipeline: `actions/checkout@v4`, `actions/setup-go@v5`, `mage ci`. Runs vet → mod-verify → tidy-check → generate-check → format-check → test → lint → build → govulncheck. Uploads coverage artifact. **No distribution/release steps.**
- `.github/workflows/code-review.yml:1-20` — AI code review on PRs using `pfrack/review-action@v1`.

For distribution, a new `.github/workflows/release.yml` would be fed by a `git tag` event, running `goreleaser release` which auto-handles binaries, checksums, Docker images, Homebrew formula, and Scoop manifest.

## Code References

- `magefiles/mage.go:139-142` — `Build` target (bare `go build`, no flags, no version)
- `magefiles/mage.go:144-148` — `Install` target (`go install ./cmd/freedius`)
- `magefiles/mage.go:520-551` — `DockerBuild` / `DockerRun` / `DockerPush` (local-only, no published image)
- `magefiles/mage.go:139-142` — `Build` lacks `CGO_ENABLED=0`, `-ldflags`, `-tags`
- `Dockerfile:1-18` — distroless static binary build, `GOOS=linux` hardcoded
- `Dockerfile:8-13` — build flags: `CGO_ENABLED=0`, `netgo,osusergo`, `-ldflags="-s -w"` (no version)
- `README.md:24-27` — Quickstart (source-build only, mentions "single static binary" promise)
- `main.go:45` — `var version = "dev"` (not injectable currently, no ldflags)
- `main.go:134` — `_ = checkRequiredEnvVars(cfg)` (discarded — latent bug, more impactful than packaging)
- `cmd/freedius/main.go:45` — version variable definition
- `cmd/freedius/main.go:155-172` — auto-write starter config (the only onboarding DX that survived)
- `main_test.go:261-271` — `--version` test (always prints "dev")
- `.github/workflows/ci.yml` — existing CI (no release/distribution steps)
- `.github/workflows/code-review.yml` — AI review only
- `go.mod:1` — module path `github.com/pfrack/freedius`
- `tools.go` — only imports `magefile/mage` (no release tooling)

## Architecture Insights

- The project delivers **exactly one install method** (source `go build`) despite "single static binary" being its core promise — a gap between marketing and mechanics.
- Distribution is not merely an add-on but a **framing decision**: the existing frame explicitly scopes the user as "returning maintainer," making distribution a non-goal. Adding brew requires either adopting a *new* persona (first-time adopter) or reframing the existing one.
- The module path (`github.com/pfrack/freedius` at `go.mod:1`) makes `go install @latest` structly valid after the first tag — the only blocker is the `"dev"` version string.
- The CI pipeline (`mage ci` → 9 sequential steps) is well-structured and would naturally integrate GoReleaser as a final step — but only after the framing decision is revisited.
- `CGO_ENABLED=0` + `netgo,osusergo` tags are already locked into the Dockerfile — cross-compiling these flags to macOS/Windows is trivial (set `GOOS`/`GOARCH`), but nobody has done it yet.
- GoReleaser's `brews` publisher auto-generates formula Ruby files and pushes to a separate tap repo — no formula maintenance required beyond the `.goreleaser.yaml` config block. The formula is regenerated on every tag.
- GoReleaser's `dockers` publisher can push multi-platform images to `ghcr.io/pfrack/freedius` using `buildx` under the hood — integrating with the existing Dockerfile stage.

## Historical Context (from prior changes)

| When | Change | Distribution/DX impact |
|---|---|---|
| 2026-06-21 | `daemon-mode` (archived) | **Only Homebrew mention in repo** — about `os.Executable()` resolution for the dev's own install (`plan.md:217`) |
| 2026-07-02 | `web-ui` (archived) | Docker introduced as motivation; Dockerfile, distroless image, binary build flags locked |
| 2026-07-02 | `error-hardening` (archived) | Planned `freedius init` subcommand + env auto-injection. Implemented auto-write starter config (later flagged as undocumented breaking change) |
| 2026-07-07 | `unified-server-logs-tab` (archived) | **Removed `freedius init`**, subcommand dispatch, env auto-injection. Eval-snippet survives as `--no-export-hint` |
| 2026-07-21 | `solo-dev-positioning` (ACTIVE) | Strategic reframe: cold-start framing explicitly rejected. DX = "legibility of system state." README rewritten purpose-first |

## Distribution ranking with implementation details

| Approach | Setup effort | Maintenance | Solo-dev UX | Platform | Fit | Notes |
|---|---|---|---|---|---|---|
| **GoReleaser + GitHub Releases** | 1 YAML file | Near-zero (tag-driven) | Excellent | macOS/Linux/Windows | ★★★★★ | Config at ~80 lines. Version injection via ldflags. `go install @latest` needs `debug.ReadBuildInfo()` fallback. |
| **Homebrew tap** (via GoReleaser) | +10 lines YAML + tap repo | Zero (auto-pushes formula) | `brew install pfrack/freedius/freedius` | macOS + Linux | ★★★★★ | Creates 1-line Ruby formula on each tag. Requires `contents: write` on tap repo. |
| **Scoop bucket** (via GoReleaser) | +10 lines YAML + bucket repo | Zero (auto-pushes manifest) | `scoop install pfrack/freedius` | Windows | ★★★★ | JSON manifest auto-generated. |
| **Nix NUR** (via GoReleaser) | MEDIUM-HIGH | MEDIUM | `nix run pfrack#freedius` | NixOS | ★★★ | Requires `nix-hash`, cannot use binary archives. Must maintain NUR repo structure. |
| **Self-update** (`go-selfupdate`) | MEDIUM | MEDIUM | `freedius update` | All | ★★★★ | Requires semver version string (currently "dev"). Best UX but binary replacement complexity. |
| **`go install @latest`** | LOW | LOW | Compiles from source (10s) | All | ★★★★ | After first tag, `go install github.com/pfrack/freedius@latest` works. Needs `debug.ReadBuildInfo()` for proper version display. |
| **uvx** | HIGH | HIGH | `uvx freedius` | All | ★★ | 5 platform wheels (~21MB each), version sync, wrong audience. Skip unless Astral ships binary runner. |
| **MacPorts** | HIGH | HIGH | Niche | macOS | ★★ | Manual Portfile, PR to macports-ports tree. |
| **install.sh** | HIGH | HIGH (shell maintenance) | `curl \| sh` | Unix | ★★ | 930-line rustup-level burden for a solo dev. Skip. |

**Recommendation when/if pursued:** GoReleaser + GitHub Releases + Homebrew tap as primary (near-zero maintenance, triggered on `git tag`), Scoop as secondary for Windows. Skip uvx (wrong audience, technically heavy) and install.sh (maintenance trap).

## Related Research

- `context/changes/solo-dev-positioning/frame.md` — the authoritative "cold-start rejected" framing
- `context/changes/solo-dev-distribution/research.md` (this document)
- `context/archive/2026-06-21-daemon-mode/plan.md:217` — only Homebrew mention in repo history

## GitHub Code Permalinks (commit cf029c5)

- [magefiles/mage.go:139-142 — Build target](https://github.com/pfrack/freedius/blob/cf029c56beed8736f7c293381b7d03df0786df1c/magefiles/mage.go#L139-L142)
- [magefiles/mage.go:144-148 — Install target](https://github.com/pfrack/freedius/blob/cf029c56beed8736f7c293381b7d03df0786df1c/magefiles/mage.go#L145-L148)
- [magefiles/mage.go:520-551 — Docker targets](https://github.com/pfrack/freedius/blob/cf029c56beed8736f7c293381b7d03df0786df1c/magefiles/mage.go#L520-L551)
- [Dockerfile:8-13 — Build flags](https://github.com/pfrack/freedius/blob/cf029c56beed8736f7c293381b7d03df0786df1c/Dockerfile#L8-L13)
- [Dockerfile:1-18 — Distroless image](https://github.com/pfrack/freedius/blob/cf029c56beed8736f7c293381b7d03df0786df1c/Dockerfile#L1-L18)
- [cmd/freedius/main.go:45 — Version variable](https://github.com/pfrack/freedius/blob/cf029c56beed8736f7c293381b7d03df0786df1c/cmd/freedius/main.go#L45)
- [cmd/freedius/main.go:134 — checkRequiredEnvVars discarded](https://github.com/pfrack/freedius/blob/cf029c56beed8736f7c293381b7d03df0786df1c/cmd/freedius/main.go#L134)
- [cmd/freedius/main.go:155-172 — Starter config auto-write](https://github.com/pfrack/freedius/blob/cf029c56beed8736f7c293381b7d03df0786df1c/cmd/freedius/main.go#L155-L172)
- [.github/workflows/ci.yml — CI pipeline](https://github.com/pfrack/freedius/blob/cf029c56beed8736f7c293381b7d03df0786df1c/.github/workflows/ci.yml)
- [go.mod:1 — Module path](https://github.com/pfrack/freedius/blob/cf029c56beed8736f7c293381b7d03df0786df1c/go.mod#L1)

## Open Questions

1. **Is the persona scope being revisited?** If the "returning maintainer" scope is held, distribution tooling is correctly out of scope — this research becomes a case for "won't do, here's why." If the scope is expanding to include first-time adopters, GoReleaser + Homebrew is the path.
2. **Would surfacing the existing zero-config first run (`cmd/freedius/main.go:155-172`, embedded starter.yaml) plus fixing the discarded `checkRequiredEnvVars` return value (`main.go:134`) remove enough first-run friction to make the distribution question moot?**
3. **Does anybody actually ask for this?** The research found zero inbound requests for brew/npx distribution across all foundation docs, plans, and frames. If the motivation is internal ("it should have this") rather than user-driven, that changes the cost-benefit.
4. **What version-tagging strategy?** GoReleaser requires `git tag vX.Y.Z` to trigger releases. A `VERSION` file or `git describe` convention needs to be established. Also: the `main.go:45` version variable needs `debug.ReadBuildInfo()` enhancement for `go install @latest` to display the module version.
4. **What version-tagging strategy?** GoReleaser requires `git tag vX.Y.Z` to trigger releases. A `VERSION` file or `git describe` convention needs to be established. Also: the `main.go:45` version variable needs `debug.ReadBuildInfo()` enhancement for `go install @latest` to display the module version.
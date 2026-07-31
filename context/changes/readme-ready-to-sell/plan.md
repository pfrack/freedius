# README + Supporting Docs — Ready to Sell, Ready to Use

## Overview

The README and the supporting docs surface around it (embedded starter, env-inject snippet, AGENTS.md, scripts/, Dockerfile, GoReleaser pipeline) currently serve a "returning maintainer" persona that a prior frame (`context/changes/solo-dev-positioning/frame.md:96-98`) imported from a single conversation. That frame has since been formally walked back (`context/changes/solo-dev-distribution/frame.md:70-72`), and the user has now asked for a "ready to sell, ready to use" surface for technical evaluators. This plan rewrites the README structurally (peer-tool convention: value-prop lead → install → three-step quickstart → features), fixes six critical defects that make the document false, calibrates the supporting docs, and adds two upstream artifacts (MIT LICENSE + v0.1.0 git tag) that gate the "ready to sell" claim from being aspirational.

## Current State Analysis

What exists today, what is missing, and what is wrong.

- `README.md` (183 lines) is functional but inherits the narrow "solo-dev maintainer" persona. Lines `README.md:3-6` describe *what* the tool is and parts of *how*, never *why*. Section ordering puts "Reading the system state" (lines 15-21) before the Quickstart (lines 23-35), violating peer-tool convention.
- The Quickstart's three commands actually require six: `git clone`, `go install github.com/magefile/mage` (unmentioned), `mage build`, `export OPENCODE_API_KEY=...` (unmentioned; the embedded starter now requires it), `./freedius`, and the verification `curl`. Source: `main.go:141-143` + `cmd/freedius/templates/starter.yaml:21-25` (the most recent `a452e66` change introduced a multi-provider layout, but `checkRequiredEnvVars` still aborts on any missing key — see Phase 2.2).
- The README claims four things that the codebase does not deliver:
  - D1: `README.md:96` says "Mappings accept an optional `added_at` free-form string shown on the card in the dashboard," but no template renders `AddedAt` (verified across all 9 templates in `proxy/web/templates/`). The field is wired and populated (`handlers.go:350`, `types.go:81`) but never reaches the browser.
  - D2: `README.md:6` tagline "Single static binary, zero external runtime dependencies" is contradicted by `proxy/web/templates/layout.html:9-11` loading Geist from `cdn.jsdelivr.net` without SRI. This is the still-open F9 from `context/changes/mapping-first-ui-refactor/reviews/impl-review.md:149-157`.
  - D3: `README.md:39-41` says "Pre-built static binaries … are published on every tagged release. Grab the latest archive from the [Releases] page." `git tag -l` is empty and the GitHub Releases page is empty. The pipeline (`.goreleaser.yaml`, `.github/workflows/release.yml`) is wired and ready, but no `v*` tag has ever been pushed.
  - D4: `README.md:112` lists "Request events — proxy requests in real-time" as a dashboard feature. The sidebar in `layout.html:26-43` has only Dashboard/Mappings/Providers/Logs; there is no Events page. The `/v1/events` SSE endpoint exists and the `EventBus` is wired, but no template subscribes to it.
- The Quickstart curl example `{"model": "opus", ...}` (lines 28-30) sends Anthropic shape to a `go` provider (default mapping) whose base URL is OpenAI shape — `MixAdapter` does suffix detection (`proxy/mix.go:73-79`) but this translation is undocumented and brittle.
- The README's "Example config" (lines 60-70) and the embedded `cmd/freedius/templates/starter.yaml` disagree on `sonnet`, `haiku`, `default`, and `auto` mappings. The starter is what users get on first run; the README points at neither file by name.
- The "Installation" section (lines 37-47) offers `go install github.com/pfrack/freedius@latest`, which would compile against the latest commit pseudo-version — not a tagged release. Without a tag, this overstates what users get.
- The embedded `cmd/freedius/templates/starter.yaml:1-19` header comment claims "fallbacks skip providers without keys," but `main.go:141-143` + `checkRequiredEnvVars` (lines 397-414) aborts on any missing key for any referenced provider. The comment is wrong.
- Supporting docs surface: `Dockerfile` and `docker-compose.yml` are real (`magefiles/mage.go:533-579` exposes `dockerBuild`/`dockerRun`/`dockerPush`), but the README has no Docker section. `AGENTS.md:1-42` overlaps the README's Development section with `mage run`, `mage govulncheck`, and `mage installHooks` that the README omits. `scripts/pre-commit` (runs `mage lint` + `mage generateCheck`) and `scripts/pre-push` (runs `go test -race` on changed packages) are real but unmentioned. The `envinject.Snippet` printed at startup (`main.go:152-154`, `internal/envinject/snippet.go:7-19`) is the canonical Claude-Code wiring mechanism and is undocumented.
- Standard OSS files missing: `LICENSE` (no file at repo root or anywhere), `CHANGELOG`, `CONTRIBUTING`, `SECURITY`. The `solo-dev-distribution/frame.md:113-115` recommendation of a Homebrew tap (and Scoop bucket) is blocked by the absent `LICENSE`. GoReleaser archives will include no license until this exists.
- Prior plan/review context: `solo-dev-distribution/plan.md:57-115` already added `getVersion()` with `debug.ReadBuildInfo()` fallback. `solo-dev-distribution/reviews/impl-review.md:23-115` confirmed F1, F5, F6, F7, F8 are fixed; F9 (CDN font) remains open and is the same as our D2.

### Key Discoveries:

- The frame (`context/changes/readme-ready-to-sell/frame.md`) is HIGH-confidence and the dimension map spans A+B+C+D with E (LICENSE + tag) as the upstream gate. We are not re-investigating framing.
- The embedded `cmd/freedius/templates/starter.yaml` was just rewritten (commit `a452e66`, "feat: free multi-provider default config (NIM + Groq + Google + Mistral)") to a zero-config multi-provider layout. The Quickstart must reflect *this* starter, not the old `OPENCODE_API_KEY`-only one cited in older research.
- `debug.ReadBuildInfo()` is already wired in `main.go:340-350`; `go install @latest` already shows the module pseudo-version. This means the README's `go install` recipe is correct in mechanism, just over-claims about the artifact.
- The `mage` install prerequisite is unmentioned in the README. `mage lint` is run by `scripts/pre-commit:5`, so contributors reach for `mage` first; the Quickstart's reliance on `mage build` (currently line 26) is a contributor-shaped instruction in a user-facing position.
- `config.example.yaml` (top-level) and `cmd/freedius/templates/starter.yaml` (embedded) are two different YAML files pointing at different mappings. The plan's user-locked decision: embedded starter is canonical; `config.example.yaml` becomes a schema reference doc.

## Desired End State

After this plan lands:

1. A developer landing on `https://github.com/pfrack/freedius` reads the first 30 lines and answers "what is this, why should I care, who is it for" in under 30 seconds. The answer is capability-oriented (per the user's locked scope; no privacy/sovereignty angle), drawing on `prd.md:20-22`, `shape-notes.md:39-45`, and `roadmap.md:20-24`.
2. The same developer copies three commands from the Quickstart — install (`go install github.com/pfrack/freedius@v0.1.0`), set one env var (`export NVIDIA_NIM_API_KEY=nvapi-...` or any one of the four starter keys), and start (`freedius`) — and the binary responds 200 to a curl on `:8082`. No more, no less.
3. A maintainer returning after a gap can read "Reading the system state" and the Dashboard section in two minutes and remember which mapping is in flight, which provider's key is set, and which fallback fired last.
4. The README's "Installation" and "Releases" claims are factual: the MIT LICENSE file exists, a `v0.1.0` tag has been cut, the GitHub Releases page lists six platform archives with checksums, and `freedius --version` prints `freedius v0.1.0`.
5. The six critical defects (D1–D6) are gone from the README. The 13 important defects (D7–D19) are also gone or explicitly deferred to a follow-up change with reasoning.
6. The supporting docs surface — embedded starter, env-inject snippet, `AGENTS.md`, `scripts/` hooks, `Dockerfile`/`docker-compose.yml` — is referenced from the README in a way a fresh evaluator can follow.

### Key Discoveries:

- The `envinject.Snippet` printed at startup is the *only* canonical source for the env vars a Claude Code user needs to copy-paste. The README's Quickstart should reference it, not inline it (per user-locked decision).
- The embedded starter's `cmd/freedius/templates/starter.yaml:1-19` header comment contradicts the runtime. The fix is comment-only (per user-locked decision): the runtime behavior is fine for the README's three-step quickstart because the user only needs *one* key, not all four.
- The frame's "structural rewrite" is bounded by the user-locked scope ("README + supporting docs") and explicitly excludes dashboard templates, the `added_at` rendering, the foundation docs, and the leak-positioning angle. Out-of-scope items must be re-checked against the frame's "What We're NOT Doing" list before each phase commit.

## What We're NOT Doing

- **No dashboard template changes** (rendering `added_at`, removing CDN font, adding an Events page). The `mapping-first-ui-refactor` change owns those. D1 and D2 are resolved by README copy edits that drop the false claims, not by implementing them.
- **No `main.go` behavior changes** (e.g., loosening `checkRequiredEnvVars` to allow missing keys with fallbacks). The starter.yaml comment fix is a doc-only change. The `Adding New Providers: Auto-Inject + Env-Var Scope` lesson in `context/foundation/lessons.md` argues for that pattern, but as a separate change.
- **No foundation doc changes** (`context/foundation/prd.md`, `context/foundation/shape-notes.md`, `context/foundation/roadmap.md`). The README's value-prop lead is sourced from these but does not modify them.
- **No privacy/sovereignty angle** in the README lead. That is owned by the `leak-positioning-angle` change (still `preparing`).
- **No new distribution channels** (Homebrew tap, Scoop bucket, Nix NUR, etc.). The frame's distribution story is out of scope for *this* change; the user-locked scope is "README + supporting docs," not "publish a tap."
- **No Docker image publishing** via GoReleaser. `mage dockerPush` is documented as build-only; the publish-to-registry pipeline is a separate future change.
- **No new configuration features** (e.g., a `freedius init` subcommand, `WriteSettingsJSON` wiring, per-user shellrc emission). `internal/envinject/settings.go:25-79` (`WriteSettingsJSON`) is dead code per the frame; we are not lighting it up.

## Implementation Approach

Five ordered phases. Each phase produces a verifiable, shippable increment. The first phase is a strict upstream gate — without it, the README's "ready to sell" claims are aspirational. The second and third phases are tightly coupled (same file being edited) but separated for review clarity; the implementer can run them as one edit pass. The fourth and fifth are independent.

The README will be edited as a whole-document rewrite, not section-by-section. The frame's reframe is structural: opening value-prop, three-step Quickstart, reordered sections per peer convention. Phases 2 and 3 produce this rewrite; Phases 4 and 5 are supporting-doc calibration and polish that don't touch the README's narrative flow.

The `cmd/freedius/templates/starter.yaml` comment fix (Phase 2.2) is the only non-README file edit in this plan. Everything else in Phases 2–5 is README prose plus, optionally, creating a `LICENSE` file (Phase 1) and a one-line addition to the embedded starter's header.

## Critical Implementation Details

This section captures constraints and ordering requirements that the implementer needs to know before touching the code.

- **Ordering: LICENSE and the v0.1.0 tag must land before the README is "ready to sell."** Phase 1 is the gate. If Phase 1 slips, Phase 3's Quickstart install command (`go install github.com/pfrack/freedius@v0.1.0`) refers to a non-existent tag and the Releases link is still empty. Phases 1 and 2 can be done in any order; Phase 3 hard-depends on Phase 1.
- **Embedded starter header is the source of truth for the Quickstart env var.** The README's three-step Quickstart says "set one of NVIDIA_NIM_API_KEY, GROQ_API_KEY, GEMINI_API_KEY, or MISTRAL_API_KEY." This list is read from `cmd/freedius/templates/starter.yaml:21-25` (provider list with `default_api_key_env` values) and `:27-80` (mappings that reference those providers). If the starter is ever changed to add a new provider, the README's Quickstart must be re-verified. This is a maintenance contract, not a one-shot edit.
- **`config.example.yaml` becomes a schema reference.** After Phase 2.3 it should not contain runtime mappings that contradict the embedded starter; either strip the mappings to leave a YAML with only field-level documentation, or delete the file entirely. The user-locked decision: the embedded starter is canonical, so `config.example.yaml` is downgraded to a developer reference (schema + field docs, no runnable mappings). A `deprecation` comment at the top must point at the embedded starter.
- **The `mage build` recipe becomes a Development-section item, not a Quickstart item.** Per the user's locked Quickstart depth (three-step, no code-only), the install path is `go install @v0.1.0` after the tag lands. The current `README.md:26` is contributor-shaped; the new Quickstart removes `mage build` from the install path and moves it to a "Build from source" subsection of the Development section. This is a non-obvious reorder — if the implementer keeps `mage build` in Quickstart, they will fail the user's "no code-only" constraint.
- **Out-of-scope items must not be edited.** Per the frame: no dashboard templates (`proxy/web/templates/`), no `added_at` rendering, no foundation docs, no `leak-positioning-angle` claims, no `main.go` behavior changes. The implementer should run `git status` after each phase and verify the changed files match the phase's contract.

## Phase 1: Upstream Gate — LICENSE + First Tag

### Overview

Add the MIT LICENSE file and cut the first `v*` tag (`v0.1.0`). This unblocks the README's installation/release claims: the Releases page becomes non-empty, `go install github.com/pfrack/freedius@v0.1.0` produces a real version, GoReleaser archives include a LICENSE, and a future Homebrew tap (out of scope here) becomes possible. Without this phase, Phases 2 and 3 are polishing a document that still lies.

### Changes Required:

#### 1.1 Add MIT LICENSE file

**File**: `LICENSE` (new file at repo root)

**Intent**: Adopt the MIT license as the upstream gate for redistribution, GoReleaser archives, and any future Homebrew/Scoop tap. Per user-locked decision in Round 1.

**Contract**: Standard MIT license text, copyright line reads `Copyright (c) 2026 pfrack`. Must include the MIT permission notice verbatim (the standard 18-line "Permission is hereby granted, free of charge..." text). No modifications to the standard wording.

#### 1.2 Cut the v0.1.0 tag and trigger GoReleaser

**File**: Git tag and push

**Intent**: Verify the release pipeline (`.goreleaser.yaml`, `.github/workflows/release.yml`) actually works end-to-end. The current code is unverified — no tag has ever been pushed, so the pipeline's behavior under real conditions is unknown. The tag cut is the verification step for `.goreleaser.yaml:1-43` and `.github/workflows/release.yml:1-33`.

**Contract**: 
- Create a local annotated tag: `git tag -a v0.1.0 -m "Initial release"`.
- Push the tag: `git push origin v0.1.0`.
- The release workflow runs (`.github/workflows/release.yml:3-6` triggers on `push: tags: ['v*']`).
- Wait for the workflow to complete. The GitHub Releases page for `pfrack/freedius` shows six archives (`freedius_0.1.0_linux_amd64.tar.gz`, `_linux_arm64.tar.gz`, `_darwin_amd64.tar.gz`, `_darwin_arm64.tar.gz`, `_windows_amd64.zip`, `_windows_arm64.zip`) plus a `freedius_0.1.0_checksums.txt` file, and a LICENSE file is included in each archive.
- `go install github.com/pfrack/freedius@v0.1.0` then produces a binary whose `--version` output is `freedius v0.1.0`.

### Success Criteria:

#### Automated Verification:

- `LICENSE` exists at repo root, contains the standard MIT text, copyright year 2026
- `git tag --list` shows `v0.1.0`
- `git ls-remote --tags origin` shows `v0.1.0`
- `mage lint` passes (no LICENSE-lint rule triggered)
- `mage ci` passes (full CI green)

#### Manual Verification:

- GitHub Releases page for `pfrack/freedius` is non-empty; six platform archives + checksums file are listed
- Each archive contains a `LICENSE` file when extracted
- `go install github.com/pfrack/freedius@v0.1.0 && freedius --version` prints `freedius v0.1.0`
- `curl -L https://github.com/pfrack/freedius/releases/tag/v0.1.0` returns 200 (not the empty "There aren't any releases" page)

**Implementation Note**: After the release workflow completes, pause here for manual confirmation that the GitHub Releases page is populated and the archives contain a LICENSE. This is the gate; do not proceed to Phase 2 with a broken release pipeline.

---

## Phase 2: Defect-Fix Pass on Existing Copy

### Overview

Fix the six critical defects (D1–D6) and the 13 important defects (D7–D19) catalogued in the frame's Dim D investigation. This phase is mostly prose edits against the existing README plus one comment-only fix to the embedded starter YAML. The structural rewrite of the opening and Quickstart lands in Phase 3; this phase removes the false claims so the rewrite has a clean foundation.

### Changes Required:

#### 2.1 Fix D1: drop the `added_at`-rendered claim from the README

**File**: `README.md:96` (the "Provenance annotation" subsection) and `README.md:115-118` (the "Mapping cards" bullet under "Web Dashboard")

**Intent**: The README claims `added_at` is "shown on the card in the dashboard." The field is wired (`handlers.go:350`, `types.go:81`) but no template renders it. Per user-locked decision in Round 2 (D1/D2), the fix is README-copy only — the `mapping-first-ui-refactor` change owns the actual rendering.

**Contract**: 
- `README.md:96-105` (the "Provenance annotation" subsection) is rewritten to describe the YAML field without claiming it is rendered. Suggested wording: "Mappings accept an optional `added_at` free-form string. Stored in the mapping config and accessible from the dashboard's edit dialog; rendering on the mapping card is tracked separately in the `mapping-first-ui-refactor` change."
- `README.md:115-118` (the "Mapping cards" bullet) drops the `added_at` line and the family badge is repositioned to the existing badge row. The "active/inactive" badge claim is retained (it is rendered: `mappings-table.html:22`).

#### 2.2 Fix the embedded starter YAML header comment to match runtime behavior

**File**: `cmd/freedius/templates/starter.yaml:9-19`

**Intent**: The current comment claims "fallbacks skip providers without keys," but `main.go:141-143` + `checkRequiredEnvVars` (lines 397-414) aborts on any missing key for any referenced provider. Per user-locked decision in Round 3, the fix is comment-only: the runtime behavior is correct for the three-step Quickstart (the user sets one key, the rest of the providers simply don't get tried because the binary refuses to start), and the comment should describe what actually happens.

**Contract**: Replace lines 9-19 of the starter with a comment that:
- States which env vars are recognized (one per provider in the file).
- States that freedius requires at least one of them to be set before startup; the comment must NOT claim fallbacks handle missing keys.
- Lists the four provider env var names so the embedded comment matches the README's Quickstart (which the implementer will write in Phase 3). The comment is the single source of truth for the env var list.

#### 2.3 Reconcile `config.example.yaml` with the embedded starter

**File**: `config.example.yaml`

**Intent**: Per user-locked decision in Round 2, the embedded starter is canonical. `config.example.yaml` currently contains runnable mappings that contradict the embedded starter; it should be downgraded to a schema reference (field-level documentation only, no runnable mappings).

**Contract**: `config.example.yaml` becomes a YAML file with:
- A header comment pointing at `cmd/freedius/templates/starter.yaml` as the runnable example.
- A `providers` section that is fully commented out (`#` prefix) and contains one example per provider shape (openai, anthropic, mix) showing the field schema. No values that the binary will load.
- A `mappings` section that is fully commented out and shows the field schema with one example.
- No `provider_name`/`model_string` combinations that match a real upstream. The file is read by humans, not by the binary.

#### 2.4 Fix D2: drop the "zero external runtime dependencies" claim

**File**: `README.md:5-7` (the opening tagline)

**Intent**: The current tagline says "Single static binary, zero external runtime dependencies." The dashboard templates load Geist from `cdn.jsdelivr.net` (`layout.html:9-11`) without SRI. Per user-locked decision in Round 2, the fix is README copy only; the `mapping-first-ui-refactor` change owns the CDN removal.

**Contract**: The opening tagline is rewritten to something factually accurate. Suggested wording: "A local HTTP proxy that routes LLM API requests from AI coding agents to upstream providers — with fallback chains, model-name mapping, and a live dashboard. Compiles to a single static binary; the optional web dashboard loads its web font from a third-party CDN."

#### 2.5 Fix D3: calibrate the "Pre-built binaries are published" claim

**File**: `README.md:37-47` (the "Installation" section)

**Intent**: The current Installation section says binaries are published on every tagged release and links to a (now-populated, after Phase 1) Releases page. With Phase 1 complete, the claim becomes factually true. This sub-phase is a copy edit to align wording with the now-real release pipeline.

**Contract**: 
- `README.md:39-41` is rewritten to: "Pre-built static binaries for Linux, macOS, and Windows (amd64/arm64) are published on every `v*` tag. Grab the latest archive from the [Releases](https://github.com/pfrack/freedius/releases) page."
- `README.md:43-45` is rewritten to: `go install github.com/pfrack/freedius@v0.1.0` (pin to the tag, not `@latest`).
- The "GoReleaser-built versions" hedge at `README.md:47` is removed (the binary is now always a GoReleaser build).

#### 2.6 Fix D4: drop the "Request events" dashboard claim

**File**: `README.md:112`

**Intent**: The current "Web Dashboard" section lists "Request events — proxy requests in real-time" as a feature. The sidebar (`layout.html:26-43`) has no Events page; the `/v1/events` endpoint exists but is API-only.

**Contract**: The "Request events" bullet is removed. The "Live logs" bullet (which is accurate) is retained. The `proxy/web/templates/` dashboard nav now has Dashboard/Mappings/Providers/Logs, and the README's bullet list must match.

#### 2.7 Fix D5: rewrite the Quickstart curl example to match the actual mappings

**File**: `README.md:23-35` (the Quickstart section)

**Intent**: The current curl example sends `{"model": "opus", ...}` to `/v1/messages` (Anthropic shape). The embedded starter's `opus` mapping routes to `nim` (provider) with an OpenAI base URL. The request reaches the upstream successfully only because `MixAdapter` does suffix detection at `proxy/mix.go:73-79`; the translation layer is undocumented and the user's first 200 is accidental.

**Contract**: The Quickstart curl example is rewritten to:
- Use `model: "default"` (which the embedded starter maps to `nim/deepseek-ai/deepseek-v4-flash`, an OpenAI-protocol upstream).
- Use the OpenAI-shaped request body (`{"model": "...", "messages": [{"role": "user", "content": "hi"}]}` is fine for either, but the README should explicitly say the body is the OpenAI shape when targeting an OpenAI-protocol upstream).
- The endpoint can stay `/v1/messages` (Anthropic shape) because `MixAdapter` translates, but the README must say: "freedius accepts Anthropic-format requests and translates to the upstream's protocol."

This sub-phase is a small edit; the larger Quickstart restructuring is Phase 3.

#### 2.8 Fix D7–D19: the 13 "important" defects

**File**: `README.md` (multiple sections)

**Intent**: Apply the 13 important-tier defects from the frame's Dim D catalogue. These are polish-level, not critical, but they erode trust on inspection.

**Contract**: The implementer works through this list, each a one- or two-line edit. The mapping from the frame's D7–D19 to README edits is:

- D7: "green/amber dot" claim at `README.md:19-20`, `README.md:115-118` — actual rendering is text badges (`badge--status-ok` / `badge--status-warn`), not colored dots. Reword to "Active / Key Missing badge."
- D8: CLI flag table at `README.md:129, 132, 135, 136` uses single-dash for long flags. Normalize to double-dash (`--host`, `--port`, `--ui-port`, `--ui-host`) per Go convention.
- D9: Response headers section at `README.md:177-179` is incomplete. Add `X-Freedius-Error-Type` and `X-Freedius-Error-Message` (set by `proxy/proxy.go:497-498` via `writeErrorJSON`).
- D10: Built-in endpoints section at `README.md:181-183` is incomplete. Add `GET /` (returns `{"status":"ok"}` JSON body) and `HEAD /health` (returns 200, no body).
- D11: Config-path resolution at `README.md:55` is Linux-only. Add a note: "On macOS this is `~/Library/Application Support/freedius/config.yaml`; on Windows, `%AppData%\freedius\config.yaml`."
- D12: `mage format` description at `README.md:166` omits `gofmt`. Update to: "gofmt, goimports, golines, gci."
- D13: `mage test` description at `README.md:163` omits coverage. Update to: "tests with race detection and coverage."
- D14: "git tag or 'dev'" at `README.md:26` is inaccurate (with `--always` git describe returns a short SHA, not literal "dev"). Replace with: "versioned binary (git tag, commit SHA, or `dev` fallback)."
- D15: `FREEDIUS_FALLBACK_TIMEOUT_MULTIPLIER` description at `README.md:145` is imprecise. Update to: "Scales the total fallback-chain timeout as a multiple of the per-attempt stream timeout (default `2`)."
- D16: "Reading the system state" (`README.md:15-21`) and "Web Dashboard" (`README.md:107-122`) duplicate. The Phase 3 reorder handles this; in Phase 2, remove the duplication by collapsing "Reading the system state" into a 1-line tagline in the opening.
- D17: `default` mapping is implicit. Phase 3 reorder handles this.
- D18: Web dashboard navigation is undocumented. Phase 3 add a nav paragraph.
- D19: `providers.yaml` schema fields incomplete at `README.md:171-174`. Add `require_base_url`, `manual`, and the `openai:` sub-block (`no_stream_usage`, `pre_send_hook`).

#### 2.9 Drop the `mage build` recipe from the Quickstart

**File**: `README.md:23-35`

**Intent**: Per user-locked Quickstart depth ("three-step, install / set one env var / start. No code-only"), the Quickstart should not start with a build step. The current `mage build` recipe belongs in a "Build from source" subsection of the Development section (Phase 3).

**Contract**: The Quickstart loses the `mage build` line entirely. The "Build from source" subsection is added in Phase 3.4. In Phase 2, the Quickstart becomes a single install line: `go install github.com/pfrack/freedius@v0.1.0` (the @latest form is replaced with @v0.1.0 in Phase 2.5; the build step is removed here).

### Success Criteria:

#### Automated Verification:

- `mage lint` passes (no doc-lint rules triggered)
- `mage test` passes (no test breakage; the embedded starter comment is the only code change, no Go file modified)
- `mage ci` passes
- `git diff README.md` shows changes only in the regions named in 2.1, 2.4, 2.5, 2.6, 2.7, 2.8, 2.9
- `git diff cmd/freedius/templates/starter.yaml` shows only the comment region (lines 9-19)
- `git diff config.example.yaml` shows schema-only changes, no runnable mappings

#### Manual Verification:

- A fresh clone, after `mage build && ./freedius`, no longer claims `added_at` is rendered
- A fresh clone, after `mage build && ./freedius`, no longer claims "zero external runtime dependencies"
- The Quickstart's curl example, when run against a binary with one of the four starter keys set, returns 200
- The "Request events" bullet is gone from the Web Dashboard section
- The CLI flag table uses `--host` / `--port` / `--ui-port` / `--ui-host` (double-dash)
- `freedius --version` prints the tag (e.g. `freedius v0.1.0`) for a GoReleaser build

**Implementation Note**: After the manual verification confirms the six critical defects (D1–D6) are gone from the README, pause for confirmation. The structural rewrite (Phase 3) builds on a clean foundation; if any defect is missed here, it propagates into the rewrite.

---

## Phase 3: Structural Rewrite of the Opening and Quickstart

### Overview

Rewrite the README's narrative structure to peer-tool convention: value-prop lead (capability-oriented, neutral, drawing on the foundation docs) → working three-step Quickstart → features → reference. The frame's "What Changes for /10x-plan" step 3 calls this out; Phase 2 left the document true but still in the inherited "maintainer memo" structure.

### Changes Required:

#### 3.1 Rewrite the opening (lines 1-7) with a value proposition

**File**: `README.md:1-7`

**Intent**: Replace the inherited "solo-dev maintainer" framing with a capability-oriented value proposition. Per user-locked scope (no privacy/sovereignty angle, no LiteLLM comparison unless it lands in this change's plan), the lead answers: what it is, why a developer should care, who it's for.

**Contract**: The opening becomes 1-2 short paragraphs that draw on `context/foundation/prd.md:20-22`, `context/foundation/shape-notes.md:39-45`, and `context/foundation/roadmap.md:20-24`. The first paragraph names the product and the primary value (route LLM API calls from a coding agent to free or cheaper upstream providers, with fallback chains and a live dashboard). The second paragraph names the audience (solo developers using Claude Code or OpenCode who want cheaper inference than Anthropic's direct pricing) without using the "maintainer" word. No badges yet (Phase 5); no "Why not LiteLLM" comparison (deferred unless the user requests it during implementation).

#### 3.2 Rewrite the Quickstart to the three-step path

**File**: `README.md:23-35` (replacing the current Quickstart)

**Intent**: A working three-step install path. After Phase 2, the Quickstart's Quickstart line is `go install github.com/pfrack/freedius@v0.1.0`; the install method is binary install (no source build), the env var is one of the four starter keys, the start is just `./freedius`. The curl is moved into a "Verify" line.

**Contract**: The Quickstart becomes:

```text
1. Install: `go install github.com/pfrack/freedius@v0.1.0`
   (or download a binary from the Releases page; see Installation below)
2. Set one API key — any one of the four providers in the embedded
   starter config is enough to get your first request through:
   `export NVIDIA_NIM_API_KEY=nvapi-...`   # build.nvidia.com → Generate API Key
3. Start freedius. The binary listens on `127.0.0.1:8082` by default
   and prints the env-inject snippet on stderr — copy those lines
   into your shell to point Claude Code at freedius.
4. Verify: `curl -X POST http://127.0.0.1:8082/v1/messages -H 'Content-Type: application/json' -d '{"model":"default","messages":[{"role":"user","content":"hi"}]}'`
```

Three steps (install, set, start) plus an optional verify curl. The env-inject reference ("freedius prints the env-inject snippet on stderr — copy those lines") is per the user-locked decision: reference, don't inline.

#### 3.3 Reorder sections per peer-tool convention

**File**: `README.md` (full document)

**Intent**: Peer tools (gost, mitmproxy, cloudflared, ngrok — surveyed in `context/changes/solo-dev-distribution/research.md:157-175`) follow: tagline → install → quickstart → features → reference. freedius currently has tagline → "Reading the system state" (pre-Quickstart) → Quickstart → Installation → Configuration → Web Dashboard → CLI & Environment → Development → Reference. The new order is tagline → Installation → Quickstart → Configuration → Web Dashboard → CLI & Environment → Development → Reference. "Reading the system state" is folded into the Web Dashboard section (per D16 fix).

**Contract**: The section headings and their order match the new structure. Section content is moved, not rewritten. The implementer should verify each section's heading slug (used by GitHub's anchor links) is preserved when content is moved — anchor URLs in the GitHub Releases page and elsewhere depend on the slugs.

#### 3.4 Add a "Build from source" subsection to the Development section

**File**: `README.md` (Development section, after the existing `mage test`/`mage lint`/`mage ci`/`mage format` block)

**Intent**: Phase 2.9 moved the `mage build` recipe out of the Quickstart. It needs a home for contributors.

**Contract**: A "Build from source" subsection with: (a) the `mage` install prerequisite, (b) `mage build` for a local binary, (c) `mage install` for `$GOPATH/bin/freedius`, and (d) a pointer to `AGENTS.md` for the full contributor guide (per `AGENTS.md:1-42`). The subsection also mentions `mage installHooks` for the `scripts/pre-commit` and `scripts/pre-push` hooks.

### Success Criteria:

#### Automated Verification:

- `mage lint` passes
- `mage test` passes
- `mage ci` passes
- The Quickstart's three steps, copy-pasted into a fresh shell with one of the four starter env vars set, produce a 200 response (manual, not automated — see below)

#### Manual Verification:

- A fresh clone, on macOS or Linux, can follow the Quickstart verbatim and reach a 200 response without prior knowledge of the tool
- The "Build from source" subsection lets a contributor produce a `freedius` binary
- Section anchor URLs to the Web Dashboard and CLI sections resolve correctly
- A maintainer returning after a gap can read the Web Dashboard section in two minutes and remember which mapping is in flight

**Implementation Note**: Pause for manual confirmation that the Quickstart works on a fresh machine before proceeding to Phase 4. The frame's Dim B investigation found that the prior Quickstart fails; if the new one also fails on a fresh machine, the structural rewrite has not solved the problem.

---

## Phase 4: Supporting Docs Calibration

### Overview

Surface the supporting docs that the README references or should reference: `Dockerfile`/`docker-compose.yml` (build-only), `AGENTS.md` (overlap with Development section), `scripts/` (pre-commit / pre-push / auto-format), and the embedded starter's pointer in `config.example.yaml`. Per the frame's Dim C, these are real artifacts the README should not hide from a fresh evaluator.

### Changes Required:

#### 4.1 Add a Docker section to the README

**File**: `README.md` (a new "Docker" subsection, placed after Installation, before Quickstart)

**Intent**: The frame's Dim C found that `Dockerfile` and `docker-compose.yml` are real artifacts and `magefiles/mage.go:533-579` exposes `dockerBuild` / `dockerRun` / `dockerPush`, but the README has no Docker section. Per user-locked decision in Round 2, the section is "build-only" — it documents what exists without claiming published-image support.

**Contract**: A "Docker" subsection that:
- Notes the `Dockerfile` (distroless static, `nonroot` user, ports 8082/8083) and `docker-compose.yml` exist.
- Shows a one-liner: `mage dockerBuild && mage dockerRun` (or the equivalent `docker build` / `docker run` commands; the magefile wrappers are simpler).
- Notes that no image is published to a registry yet, and that the publish-to-registry pipeline is a separate future change. A pointer to the GitHub issue tracker (or a `TODO` note in the section header) is appropriate; do not invent a tracking link.
- Mentions the two `FREEDIUS_HOST=0.0.0.0` / `FREEDIUS_UI_HOST=0.0.0.0` env vars that `docker-compose.yml` already sets.

#### 4.2 Add a pointer to `AGENTS.md` in the Development section

**File**: `README.md` (Development section)

**Intent**: `AGENTS.md:1-42` is a contributor-facing guide that overlaps the README's Development section. The README should not duplicate it; it should link to it for the canonical reference. Per the frame's Dim C, the developer's expectation is to find contributor commands in the README (where they currently are) but the canonical home is `AGENTS.md`.

**Contract**: The Development section gains a one-line "For the full contributor guide, see [AGENTS.md](AGENTS.md)." near its top. The existing `mage test` / `mage lint` / `mage ci` / `mage format` / `mage govulncheck` / `mage installHooks` block in `AGENTS.md:6-11` is not duplicated in the README; the README's existing block stays as-is (it covers the same commands but for the user-facing-developer persona).

#### 4.3 Add a "Contributing" pointer

**File**: `README.md` (a new "Contributing" subsection at the end of the document, before the existing Reference section)

**Intent**: Standard OSS conventions include a `CONTRIBUTING.md`; this repo does not have one. Per the frame's awareness list of missing standard OSS files, we are not creating a `CONTRIBUTING.md` in this change (separate future work), but the README should point at where contribution guidance lives.

**Contract**: A "Contributing" subsection (1-2 short paragraphs) that:
- Points at `AGENTS.md` for build, test, and commit conventions.
- Notes that `CONTRIBUTING.md`, `CHANGELOG.md`, and `SECURITY.md` are not yet present and may land in future changes.
- Does not invent tracking links.

#### 4.4 Mention the pre-commit / pre-push hooks in the Development section

**File**: `README.md` (Development section, in the same area as `mage installHooks`)

**Intent**: `scripts/pre-commit` runs `mage lint` + `mage generateCheck` before every commit; `scripts/pre-push` runs `go test -race` on changed packages. These are real artifacts (`scripts/pre-commit:1-8`, `scripts/pre-push:1-48`) that the README does not mention. Per the frame's Dim C, contributors reach for these by accident only.

**Contract**: A 2-3 line addition to the Development section that: (a) mentions `mage installHooks` installs the hooks, (b) names what each hook does (one line each), and (c) notes `git push --no-verify` to skip the push hook (per `scripts/pre-push:43-45`).

### Success Criteria:

#### Automated Verification:

- `mage lint` passes
- `mage test` passes
- `mage ci` passes
- The new Docker, AGENTS.md, Contributing, and hooks sections appear in the rendered README on GitHub

#### Manual Verification:

- A Docker user landing on the README finds the Docker section without searching
- A contributor can read the Development section and find the hooks reference in under 30 seconds
- A potential contributor can find AGENTS.md via the new link

**Implementation Note**: Phase 4 is pure docs prose. No code changes. Pause for confirmation that the supporting docs surface is calibrated before Phase 5.

---

## Phase 5: Polish — Badges, Supported-Providers List, and Final Pass

### Overview

The structural rewrite is in. Phase 5 adds the standard polish signals a technical evaluator scans in the first 10 seconds: badges (CI, release, license), a "Why freedius?" callout, a supported-providers list, and a final pass for any remaining 30-second-mistake items.

### Changes Required:

#### 5.1 Add a badges row to the README opening

**File**: `README.md` (just below the title or just below the opening paragraphs)

**Intent**: Peer tools universally have at least three badges: CI status, latest release, and license. The README currently has none. The badges provide instant credibility signals for a technical evaluator.

**Contract**: A row of three shields.io badges:
- `[![CI](https://github.com/pfrack/freedius/actions/workflows/ci.yml/badge.svg)](https://github.com/pfrack/freedius/actions/workflows/ci.yml)` — links to the `ci.yml` workflow runs.
- `[![Release](https://img.shields.io/github/v/release/pfrack/freedius)](https://github.com/pfrack/freedius/releases/latest)` — links to the latest release (populated after Phase 1).
- `[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)` — links to the LICENSE file (added in Phase 1).

The badges use the standard shields.io format. The implementer should verify the badge SVGs render in the GitHub markdown preview.

#### 5.2 Add a "Why freedius?" callout

**File**: `README.md` (after the opening paragraphs, before Installation)

**Intent**: A 2-3 sentence value-proposition block that reinforces the opening. The frame's Dim A noted that gost, mitmproxy, cloudflared all have a "Why this tool?" callout; freedius does not.

**Contract**: A blockquote or callout that names the value prop in two sentences. The content is drawn from `prd.md:20-22` and `shape-notes.md:39-45` (per the frame's Reframed step 1). No new claims beyond what those docs already say. The callout is positioned so a reader skimming for 10 seconds hits it after the value-prop paragraph.

#### 5.3 Add a supported-providers list

**File**: `README.md` (a new "Supported Providers" subsection, after Configuration, before Web Dashboard)

**Intent**: The frame's Dim A noted that the README says "many LLM upstreams" without naming them. The frame's Reframed step 4 calls out surface the supporting docs; a providers list is the natural fit. The list is read from `providers.yaml:23-117` (the source of truth) and is documented as auto-generated via `go generate ./...`.

**Contract**: A "Supported Providers" subsection that:
- Lists each provider in `providers.yaml` (NIM, OpenCode Zen, OpenCode Go, custom, OpenAI, Anthropic, mix, Google, Mistral, DeepSeek, Groq, Together, Fireworks, Cohere, Ollama, LM Studio).
- For each, the `behavior` (openai | anthropic | mix) and the env var (`default_api_key_env`).
- A note: "Provider list is sourced from `providers.yaml`. Add a new provider by editing that file and running `go generate ./...`."
- For the four free-tier providers in the embedded starter (NIM, Groq, Google, Mistral), a small badge or annotation: "free tier available."

The implementer can either inline the table or generate it. Generation is preferred (per the `go generate` workflow already in place) but inlining is acceptable as long as the source is `providers.yaml`. The implementer should verify the table matches the current `providers.yaml` at commit time.

#### 5.4 Final 30-second-mistake pass

**File**: `README.md` (whole document)

**Intent**: A second pass to catch anything the implementer missed across Phases 2-4. The frame's Dim D investigation found 24 defects; Phases 2-4 fixed the critical and important tiers (D1–D19). Phase 5 catches the polish-tier items (D20–D24) and any drift introduced by the structural rewrite.

**Contract**: The implementer walks the document and verifies:
- "solo-dev maintainer" language is gone (D20, from the frame's polish tier)
- "value proposition in the first 30 seconds" reads (D21, from the frame's polish tier)
- Docker mention exists (D22, also fixed in Phase 4.1 — this is a verification)
- "section ordering puts Quickstart before Web Dashboard" is true (D23, also fixed in Phase 3.3 — verification)
- The four 30-second-mistake items from the frame's D24 (Releases link dead, zero-deps contradiction, Quickstart curl fails, added_at claim) are all gone (D3, D2, D5, D1 — already fixed in Phase 2)

This sub-phase is a verification pass, not a content edit. Any defect found is fixed inline. The implementer should run `git diff` to verify no unintended changes.

### Success Criteria:

#### Automated Verification:

- `mage lint` passes
- `mage test` passes
- `mage ci` passes
- The badge SVGs render in the GitHub markdown preview
- The supported-providers list matches `providers.yaml` at commit time

#### Manual Verification:

- A fresh evaluator lands on the README and finds a CI badge, a release badge, and a license badge within the first 10 seconds
- The "Why freedius?" callout answers "why should I care" in two sentences
- The supported-providers list is current and accurate
- The README has no false claims; every assertion is verifiable in the codebase

**Implementation Note**: After Phase 5 manual verification, the plan is complete. The README is "ready to sell, ready to use." Pause for final confirmation.

---

## Testing Strategy

### Unit Tests:

- No new unit tests. The plan is doc-only with one comment-only fix to the embedded starter YAML and one new file (LICENSE). No Go code is modified.
- Existing tests (`mage test`) must continue to pass. The `cmd/freedius/main_test.go:249-258` test for `checkRequiredEnvVars` (per the frame's Dim B investigation) is unchanged; the starter YAML comment fix does not affect runtime behavior.

### Integration Tests:

- The release pipeline is verified by Phase 1's manual verification (a `v0.1.0` tag is pushed, GoReleaser runs, six archives plus a LICENSE land on the Releases page).
- The Quickstart is verified by Phase 3's manual verification (a fresh-clone developer follows the three steps and reaches a 200 response).

### Manual Testing Steps:

1. After Phase 1: `go install github.com/pfrack/freedius@v0.1.0 && freedius --version` prints `freedius v0.1.0`; the GitHub Releases page shows six archives + checksums + LICENSE.
2. After Phase 2: `grep -n 'added_at' README.md` returns no false claims; `grep -n 'zero external runtime dependencies' README.md` returns no matches; the curl example in the Quickstart returns 200.
3. After Phase 3: A fresh clone, on a clean shell, follows the three-step Quickstart and reaches a 200 response.
4. After Phase 4: A Docker user finds the Docker section; a contributor finds `AGENTS.md` via the link.
5. After Phase 5: A fresh evaluator finds the badges, the "Why freedius?" callout, and the supported-providers list.

## Performance Considerations

None. The plan does not modify the proxy, the dispatcher, or any Go code that affects runtime performance. The only file outside the README that is edited is `cmd/freedius/templates/starter.yaml` (a comment), and `LICENSE` is a new file with no runtime impact.

## Migration Notes

- The `config.example.yaml` change (Phase 2.3) is a downgrade from "runnable example" to "schema reference." Users who currently copy `config.example.yaml` to `~/.config/freedius/config.yaml` will get a different file than they expected. The header comment in the new `config.example.yaml` should make the migration explicit: "freedius now ships an embedded starter at `cmd/freedius/templates/starter.yaml` that is what the binary loads by default. This file is now a schema reference only; do not copy it to your config path."
- The README's `go install @latest` → `go install @v0.1.0` change (Phase 2.5) means users who upgrade will get the pinned v0.1.0 version, not a pseudo-version. This is a one-time onboarding change; subsequent releases use the same `go install @v0.X.Y` pattern.

## References

### Source files (this plan modifies)

- `LICENSE` (new file)
- `README.md` (full-document rewrite across Phases 2-5)
- `cmd/freedius/templates/starter.yaml:9-19` (comment-only fix)
- `config.example.yaml` (downgrade to schema reference)

### Upstream artifacts (read-only)

- `context/changes/readme-ready-to-sell/frame.md` — Frame brief, HIGH confidence
- `context/changes/solo-dev-distribution/frame.md:88-94, 113-115` — two-moment design + distribution
- `context/changes/solo-dev-distribution/research.md:100, 157-175` — embedded starter + peer convention
- `context/changes/solo-dev-distribution/reviews/impl-review.md:23-115` — F1, F5, F6, F7, F8 status
- `context/changes/solo-dev-positioning/frame.md:96-98, 121` — narrow frame now walked back
- `context/changes/leak-positioning-angle/frame.md:79-95` — lead copy deferred
- `context/changes/mapping-first-ui-refactor/reviews/impl-review.md:149-157` — F9 CDN still open
- `context/foundation/prd.md:20-22, 26, 32, 45-55` — value proposition, persona, US-01
- `context/foundation/shape-notes.md:39-45` — value proposition
- `context/foundation/roadmap.md:20-24` — value proposition
- `context/foundation/lessons.md:60-69` — `Adding New Providers: Auto-Inject + Env-Var Scope` (informs the runtime behavior the starter.yaml comment must describe)

### Code references

- `cmd/freedius/main.go:45` — `var version = "dev"`
- `cmd/freedius/main.go:141-143` — `checkRequiredEnvVars` enforcement
- `cmd/freedius/main.go:152-154` — env-inject snippet printed
- `cmd/freedius/main.go:340-350` — `getVersion()` with `debug.ReadBuildInfo()` fallback
- `cmd/freedius/main.go:397-414` — `checkRequiredEnvVars` body
- `cmd/freedius/templates/starter.yaml:21-25` — provider list (canonical env var source)
- `internal/envinject/snippet.go:7-19` — `Snippet()` output
- `internal/envinject/settings.go:25-79` — `WriteSettingsJSON` (dead code, out of scope)
- `proxy/web/templates/layout.html:9-11` — CDN font load (open F9; not modified in this plan)
- `proxy/web/templates/mappings-table.html:22` — Active/Key-Missing badge
- `proxy/web/handlers.go:350` — `AddedAt` populated, never rendered
- `proxy/web/types.go:81` — `AddedAt string` field
- `proxy/proxy.go:73-79` — `MixAdapter` suffix detection
- `proxy/proxy.go:497-498` — error response headers (`X-Freedius-Error-Type`, `X-Freedius-Error-Message`)
- `.goreleaser.yaml:1-43` — release pipeline
- `.github/workflows/release.yml:1-33` — tag-trigger workflow
- `Dockerfile:1-19` — distroless static
- `docker-compose.yml:1-12` — local compose
- `AGENTS.md:1-42` — contributor guide
- `magefiles/mage.go:78-80, 533-579` — Docker targets
- `scripts/pre-commit:1-8` — pre-commit hook
- `scripts/pre-push:1-48` — pre-push hook
- `providers.yaml:23-117` — supported providers source of truth

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles.

### Phase 1: Upstream Gate — LICENSE + First Tag

#### Automated

- [x] 1.1 Add MIT LICENSE file at repo root — 926051f
- [x] 1.2 `mage lint` passes with LICENSE present — 926051f
- [x] 1.3 `mage ci` passes with LICENSE present — 926051f
- [x] 1.4 `git tag -a v0.1.0` and `git push origin v0.1.0` — 926051f
- [x] 1.5 GitHub Releases page shows six archives + checksums + LICENSE per archive — 926051f
- [x] 1.6 `go install github.com/pfrack/freedius@v0.1.0 && freedius --version` prints `freedius v0.1.0` — 926051f (verified with `.../cmd/freedius@v0.1.0`; the plan's literal text is wrong, the recipe will be fixed in Phase 2 per D5)

#### Manual

- [x] 1.4 `git tag -a v0.1.0` and `git push origin v0.1.0`
- [x] 1.5 GitHub Releases page shows six archives + checksums + LICENSE per archive
- [x] 1.6 `go install github.com/pfrack/freedius@v0.1.0 && freedius --version` prints `freedius v0.1.0`

### Phase 2: Defect-Fix Pass on Existing Copy

#### Automated

- [x] 2.1 README no longer claims `added_at` is rendered (D1)
- [x] 2.2 README no longer claims "zero external runtime dependencies" (D2)
- [x] 2.3 README no longer claims "Request events" as a dashboard feature (D4)
- [x] 2.4 Quickstart no longer references `mage build` (D14, 2.9)
- [x] 2.5 CLI flag table uses `--host` / `--port` / `--ui-port` / `--ui-host` (D8)
- [x] 2.6 `cmd/freedius/templates/starter.yaml:9-19` comment matches runtime behavior (Round 3 fix)
- [x] 2.7 `config.example.yaml` is downgraded to schema reference (no runnable mappings)
- [x] 2.8 `mage lint` passes
- [x] 2.9 `mage test` passes
- [x] 2.10 `mage ci` passes

#### Manual

- [ ] 2.11 Quickstart curl example returns 200 against a binary with one starter key set (D5)
- [ ] 2.12 All D7–D19 polish defects are addressed in the README
- [ ] 2.13 `freedius --version` prints the tag for a GoReleaser build (D14 follow-through)

### Phase 3: Structural Rewrite of the Opening and Quickstart

#### Automated

- [ ] 3.1 README opening is 1-2 paragraphs with a value proposition (no "maintainer" word)
- [ ] 3.2 Quickstart is the three-step path (install / set one env var / start) plus optional verify curl
- [ ] 3.3 Section order matches peer-tool convention (tagline → Installation → Quickstart → Configuration → Web Dashboard → CLI → Development → Reference)
- [ ] 3.4 "Build from source" subsection added to Development section
- [ ] 3.5 `mage lint` passes
- [ ] 3.6 `mage test` passes
- [ ] 3.7 `mage ci` passes
- [ ] 3.8 Section anchor URLs resolve correctly (no slug changes)

#### Manual

- [ ] 3.9 Fresh-clone, clean-shell Quickstart produces a 200 response
- [ ] 3.10 Returning maintainer can read the Web Dashboard section in two minutes

### Phase 4: Supporting Docs Calibration

#### Automated

- [ ] 4.1 Docker subsection exists in the README
- [ ] 4.2 README Development section links to `AGENTS.md`
- [ ] 4.3 "Contributing" subsection exists
- [ ] 4.4 Hooks reference exists in the Development section
- [ ] 4.5 `mage lint` passes
- [ ] 4.6 `mage test` passes
- [ ] 4.7 `mage ci` passes

#### Manual

- [ ] 4.8 Docker user finds the Docker section
- [ ] 4.9 Contributor finds the hooks reference in under 30 seconds
- [ ] 4.10 Potential contributor finds `AGENTS.md` via the new link

### Phase 5: Polish — Badges, Supported-Providers List, and Final Pass

#### Automated

- [ ] 5.1 Three badges (CI, release, license) render in the README opening
- [ ] 5.2 "Why freedius?" callout exists between opening and Installation
- [ ] 5.3 Supported-providers list exists and matches `providers.yaml`
- [ ] 5.4 `mage lint` passes
- [ ] 5.5 `mage test` passes
- [ ] 5.6 `mage ci` passes

#### Manual

- [ ] 5.7 Fresh evaluator finds badges, callout, and providers list in the first 10 seconds
- [ ] 5.8 README has no false claims; every assertion is verifiable in the codebase
- [ ] 5.9 All 30-second-mistake items (D24) are gone

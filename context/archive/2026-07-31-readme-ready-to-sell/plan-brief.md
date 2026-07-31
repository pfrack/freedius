# README + Supporting Docs — Ready to Sell, Ready to Use — Plan Brief

> Full plan: `context/changes/readme-ready-to-sell/plan.md`
> Frame brief: `context/changes/readme-ready-to-sell/frame.md`

## What & Why

The README and the supporting docs around it (embedded starter, env-inject snippet, AGENTS.md, scripts/, Dockerfile, GoReleaser pipeline) currently serve a "returning maintainer" persona that a prior frame imported from a single conversation. That frame has been formally walked back, and the user has now asked for a "ready to sell, ready to use" surface for technical evaluators. We rewrite the README structurally (peer-tool convention: value-prop lead → install → three-step quickstart → features), fix six critical defects that make the document false, calibrate the supporting docs, and add two upstream artifacts (MIT LICENSE + v0.1.0 git tag) that gate the "ready to sell" claim from being aspirational.

## Starting Point

The current 183-line README inherits a narrow persona and has six critical defects: it claims `added_at` is rendered on the mapping card (D1, but the field is wired but never reaches a template), its "zero external runtime dependencies" tagline is contradicted by a CDN font load in the dashboard (D2), its "Pre-built binaries are published" claim points to an empty GitHub Releases page (D3, no `v*` tag has ever been pushed), it lists a "Request events" dashboard page that does not exist (D4), its Quickstart curl example sends Anthropic shape to a default mapping that lands on an OpenAI-shape upstream (D5, the `MixAdapter` translation layer is undocumented), and its inline example config disagrees with the embedded starter YAML that the binary actually loads on first run (D6). The 13 important-tier defects (D7–D19) include single-dash CLI flags, an incomplete response-headers table, and a Linux-only config-path description. The release pipeline (GoReleaser + a tag-trigger GitHub Actions workflow) is wired but unexercised. `LICENSE`, `CHANGELOG.md`, and `CONTRIBUTING.md` are absent. The embedded `cmd/freedius/templates/starter.yaml` was just rewritten (commit `a452e66`) to a multi-provider zero-config layout (NIM + Groq + Google + Mistral), but its header comment claims fallbacks skip providers without keys — which `checkRequiredEnvVars` does not actually do.

## Desired End State

After this plan lands: a developer landing on the GitHub repo reads the first 30 lines and answers "what is this, why should I care, who is it for" in under 30 seconds, drawing on the value prop in the foundation docs (no privacy/sovereignty angle; that's owned by the `leak-positioning-angle` change). The same developer copies three commands from the Quickstart — `go install github.com/pfrack/freedius@v0.1.0`, `export NVIDIA_NIM_API_KEY=...` (or any one of the four starter keys), `freedius` — and reaches a 200 response on `:8082` without prior knowledge of the tool. The README's installation and release claims are factual: the MIT LICENSE exists, a `v0.1.0` tag has been cut, the Releases page lists six platform archives with checksums, and `freedius --version` prints `freedius v0.1.0`. The six critical defects are gone from the README, the 13 important defects are addressed, and the supporting docs (Docker build-only, AGENTS.md, scripts/ hooks, embedded starter) are surfaced in a way a fresh evaluator can follow.

## Key Decisions Made

| Decision | Choice | Why | Source |
| --- | --- | --- | --- |
| LICENSE | MIT | Lowest friction for Homebrew tap (when that lands); familiar to most Go projects; matches what most users assume. | Plan (Round 1) |
| Plan scope | Structural rewrite (not just polish) | The frame's reframe is HIGH confidence and matches peer-tool convention; the prior `solo-dev-positioning` change was rejected for over-narrowing, this is the inverse correction. | Plan (Round 1) |
| First tag | v0.1.0 | Pre-1.0.0 signals API may change; lowest commitment for a tool that has never shipped; conservative for a solo-dev project. | Plan (Round 1) |
| D1/D2 resolution | README copy only | The dashboard templates, `added_at` rendering, and CDN font load are owned by `mapping-first-ui-refactor`; this change removes the false claims, doesn't implement them. | Plan (Round 2) |
| Starter surface | Embedded starter is canonical | One source of truth for what the user gets on first run; `config.example.yaml` is downgraded to a schema reference. | Plan (Round 2) |
| Env-inject | Reference, don't inline | Single source of truth (the runtime print); zero drift risk; the runtime already prints the snippet on stderr at startup. | Plan (Round 2) |
| Docker | Document build-only | Honest about the state; surfaces the existing artifacts without claiming a publish path that doesn't exist. | Plan (Round 2) |
| Missing-key policy | Fix starter.yaml comment only | Doc-only fix; the runtime behavior is correct for the three-step Quickstart (user sets one key, the rest don't matter because the binary refuses to start). Loosening `checkRequiredEnvVars` is a behavior change and a separate future change. | Plan (Round 3) |
| Frame scope | Defect-fix + structural rewrite, no dashboard templates, no foundation docs, no leak-positioning angle | Per the frame's "What We're NOT Doing" list; the plan must not cross into other changes' surfaces. | Frame |

## Scope

**In scope:**
- New `LICENSE` file (MIT)
- `git tag -a v0.1.0` and push to trigger the GoReleaser pipeline
- README structural rewrite (opening + Quickstart + section reorder)
- Six critical defect fixes (D1–D6) and 13 important defect fixes (D7–D19)
- One comment-only fix to `cmd/freedius/templates/starter.yaml:9-19`
- Downgrade `config.example.yaml` to a schema reference
- Docker build-only subsection
- AGENTS.md pointer, "Contributing" subsection, hooks reference
- Badges (CI, release, license), "Why freedius?" callout, supported-providers list, final 30-second-mistake pass

**Out of scope:**
- Dashboard template changes (rendering `added_at`, removing CDN font, adding an Events page) — owned by `mapping-first-ui-refactor`
- `main.go` behavior changes (loosening `checkRequiredEnvVars`, wiring `WriteSettingsJSON`) — separate future changes
- Foundation doc changes (`prd.md`, `shape-notes.md`, `roadmap.md`) — read-only
- Privacy/sovereignty angle in the lead — owned by `leak-positioning-angle` (still `preparing`)
- New distribution channels (Homebrew tap, Scoop bucket, Nix NUR) — separate future changes
- Docker image publishing via GoReleaser — separate future change
- New configuration features (e.g., a `freedius init` subcommand) — separate future changes

## Architecture / Approach

Five ordered phases, each producing a verifiable, shippable increment:

1. **Upstream gate** — Add MIT LICENSE; cut v0.1.0 tag; verify GoReleaser pipeline + populated Releases page. Unblocks the README's installation/release claims.
2. **Defect-fix pass** — Resolve D1–D6 and D7–D19 in the README. Fix the embedded starter YAML's header comment to match runtime behavior. Downgrade `config.example.yaml` to a schema reference.
3. **Structural rewrite** — Value-prop opening, three-step Quickstart, peer-tool section order (tagline → Installation → Quickstart → Configuration → Web Dashboard → CLI → Development → Reference), "Build from source" subsection.
4. **Supporting docs calibration** — Docker build-only subsection, AGENTS.md pointer, "Contributing" subsection, hooks reference in Development.
5. **Polish** — Badges (CI, release, license), "Why freedius?" callout, supported-providers list, final 30-second-mistake verification pass.

The README is edited as a whole-document rewrite in Phases 2 and 3, not section-by-section. Phases 1, 4, and 5 are independent. The only non-README file edit is the embedded starter YAML's header comment (Phase 2.2). The `LICENSE` file is the only new file (Phase 1).

## Phases at a Glance

| Phase | What it delivers | Key risk |
| --- | --- | --- |
| 1. Upstream gate | MIT LICENSE file + v0.1.0 tag + verified Releases page | GoReleaser pipeline is unverified; the tag cut is the verification step. If it fails, the rest of the plan ships with aspirational claims. |
| 2. Defect-fix pass | D1–D6 gone, D7–D19 addressed, starter.yaml comment fixed, config.example.yaml downgraded | The starter.yaml comment is the only non-README file edit; mis-edits here break the Quickstart. |
| 3. Structural rewrite | Value-prop lead, three-step Quickstart, peer-tool section order | Quickstart must work on a fresh clone with one env var set; the prior Quickstart fails. |
| 4. Supporting docs | Docker subsection, AGENTS.md link, "Contributing" subsection, hooks reference | Pure docs; low risk. |
| 5. Polish | Three badges, "Why freedius?" callout, supported-providers list, final verification | Badge SVGs must render; supported-providers list must match `providers.yaml` at commit time. |

**Prerequisites:** None at the project level; the plan ships from a clean working tree. The implementer needs push access to the `pfrack/freedius` repo to cut the `v0.1.0` tag.

**Estimated effort:** ~3-5 sessions across 5 phases. Phases 1 and 2 are sequential (Phase 3 hard-depends on Phase 1). Phases 4 and 5 are independent of Phase 3 and can be done in any order after Phase 2.

## Open Risks & Assumptions

- **GoReleaser pipeline may not work first time.** `.goreleaser.yaml` and `.github/workflows/release.yml` exist but are unexercised (no `v*` tag has ever been pushed). Phase 1's tag cut is the verification step. If the pipeline fails, the plan pauses there. The mitigation is a one-time setup already in the prior `solo-dev-distribution/plan.md`, but the actual run is unverified.
- **The Quickstart's "set one env var" step depends on the user picking one of the four starter keys.** If the user's network can't reach NIM, Groq, Google, or Mistral signup, they can't get past step 2. The frame's "free multi-provider" assumption is reasonable for technical evaluators but not universal; the Quickstart's verbiage must say "any one of these free providers" with links, not just "set the env var."
- **The badge SVGs require shields.io to be reachable** at the time a viewer loads the README. This is a third-party dependency the project doesn't control. The risk is cosmetic; if shields.io is down, the README still works, just without badges. The frame's "ready to sell" intent accepts this.
- **The supported-providers list is a maintenance contract.** If `providers.yaml` is edited and `go generate ./...` is not run, the README's list drifts. The plan documents the source of truth but does not automate the sync. The plan can include a note in the supported-providers section that the list is auto-generated at release time, or leave the list as a manual artifact; the implementer should confirm the maintenance model.
- **The plan does not create `CONTRIBUTING.md` or `CHANGELOG.md`.** Standard OSS conventions include these; the frame's "awareness list" notes their absence. The plan points at `AGENTS.md` for contributor guidance, but a full `CONTRIBUTING.md` is a separate future change. The user accepted this when answering Round 4.

## Success Criteria (Summary)

- A fresh-clone developer on a clean shell can follow the three-step Quickstart and reach a 200 response on `127.0.0.1:8082` without prior knowledge of the tool.
- The README's installation and release claims are factual: `LICENSE` exists, a `v0.1.0` tag has been cut, the Releases page lists six platform archives with checksums, and `freedius --version` prints the tag.
- The six critical defects (D1–D6) and 13 important defects (D7–D19) catalogued in the frame's Dim D investigation are gone from the README.
- The embedded `cmd/freedius/templates/starter.yaml` header comment accurately describes the runtime behavior of `checkRequiredEnvVars`.
- `config.example.yaml` is a schema reference, not a runnable example, and the embedded starter is the canonical first-run config.
- The README leads with a capability-oriented value proposition (no "maintainer" language), a three-step Quickstart that works on a fresh clone, and a peer-tool section order. Privacy/sovereignty framing is explicitly out of scope (owned by the in-flight `leak-positioning-angle` change).

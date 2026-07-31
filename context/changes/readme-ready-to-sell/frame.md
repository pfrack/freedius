# Frame Brief: README + supporting docs — ready to sell, ready to use

> Framing step before /10x-plan. Captures what is actually at issue, separated
> from what was initially assumed. The user asked to "update README to be more
> ready to sell, ready to use" — the investigation below shows the deliverable
> is bigger than a polish pass and the surface is wider than the README alone.

## Reported Observation

The current `/home/pawel/code/freedius/README.md` is functional and recently
rewritten, but a developer landing on the repo for the first time would not
install it. The opening describes what the tool *is* but not *why* a developer
should care; the Quickstart's three commands do not actually produce a 200 on
a fresh machine; the example config disagrees with the embedded starter; the
"zero external runtime dependencies" claim is contradicted by a CDN font load
in the dashboard; the Releases link points to an empty page; the dashboard
documentation claims `added_at` is rendered when it is not. The user wants
the README to land for a technical evaluator: clearly positioning the tool,
with a working three-step path from clone to first call, consistent with the
docs and code that back it.

## Initial Framing (preserved)

- **User's stated cause or approach**: The README is not yet "ready to sell,
  ready to use" — implied: presentation polish + first-run usability gaps.
- **User's proposed direction**: Update the README (and, per scope answer,
  supporting docs) to land for technical evaluators as both a sales surface
  and a first-run guide, with three-step quickstart and neutral/capability
  framing (no privacy/sovereignty angle in this pass).
- **Pre-dispatch narrowing** (Step 1.5):
  - Primary goal: "Both equally" — sell + first-run usability
  - Primary reader: technical evaluators
  - Scope: README plus supporting docs
  - Sales narrative: capability-oriented, neutral (leak/sovereignty angle
    explicitly deferred to `leak-positioning-angle`)
  - Quickstart depth: three-step (install, set one env var, start)

## Dimension Map

The observation could originate at any of these dimensions. The user's framing
"polish the README" lands mostly on Dim D; the investigation found Dims A, B,
C are equally load-bearing, and Dim E (prerequisite artifacts) is upstream of
all four.

1. **Dim A — Sales narrative (opening 30 lines)** — the README answers "what"
   and parts of "how," but never "why should I care?" No problem statement,
   no "Why freedius?" callout, no comparison to LiteLLM or other gateways, no
   badges, no visual proof, no supported-providers table. Peer tools
   (gost, mitmproxy, cloudflared) lead with a one-paragraph value proposition
   and a feature checklist.
2. **Dim B — First-run usability (Quickstart through first call)** — the
   displayed three commands produce 6 actual steps; the embedded starter
   config requires an `OPENCODE_API_KEY` env var that the README never names;
   the curl example uses `model: opus` whose default mapping (post-fallback)
   lands on a different provider than the README's example config claims; the
   `envinject.Snippet` printed at startup is the actual Claude-Code wiring
   mechanism and is undocumented. The Quickstart fails to deliver its own
   promise.  ← user's framing touches this
3. **Dim C — Supporting docs surface** — the README points at `providers.yaml`
   correctly, but does not mention `Dockerfile` / `docker-compose.yml` (real
   artifacts), `AGENTS.md` (overlap with Development section), `scripts/`
   (pre-commit / pre-push / auto-format), `internal/envinject/` (the
   Claude-Code wiring mechanism), the embedded `cmd/freedius/templates/starter.yaml`
   (different from the README's example and not referenced), or any of
   LICENSE/CHANGELOG/CONTRIBUTING (all missing). Two YAML examples drift.
4. **Dim D — Existing copy defects** — 24 specific defects catalogued
   (Dim D investigation), six critical: `added_at` claimed-but-not-rendered,
   CDN font contradicts "zero external deps" tagline, empty Releases link,
   claimed "Request events" dashboard page that does not exist, broken
   Quickstart curl, README example vs. embedded starter config drift.  ←
   user's primary framing
5. **Dim E — Prerequisite artifacts (upstream blockers)** — the "ready to
   sell" intent is gated by LICENSE (absent) and the first git tag (absent).
   No tag → Releases link dead, `go install @latest` produces a pseudo-version,
   `freedius --version` shows a commit hash, GoReleaser archives never publish.
   Distribution channels (Homebrew tap, Scoop) cannot accept a repo without
   LICENSE.

**Initial framing landed on Dim D + Dim A's polish bucket.** The investigation
shows the actual surface spans A + B + C + D, and the order of work is gated
by E.

## Hypothesis Investigation

| Hypothesis | Evidence | Verdict |
| --- | --- | --- |
| **Dim A: README opening needs a value proposition, not just polish** | PRD `prd.md:20-22`, shape-notes `shape-notes.md:39-45`, and roadmap `roadmap.md:20-24` all name the value (cheap/free inference, "few lines of config, not a project") but the README opening describes process, not value. `README.md:3-6` covers *what* and parts of *how*; never *why*. The "solo-dev maintainer" language at `README.md:5` reflects the previously-rejected narrow frame (`solo-dev-positioning/frame.md:96-98`) which `solo-dev-distribution/frame.md:70-72` has already identified as overriding the PRD. Peer convention: gost/mitmproxy/cloudflared lead with a one-paragraph value statement + feature checklist. | STRONG |
| **Dim B: Quickstart needs a working three-step path, not just a defect fix** | `README.md:23-35` shows 3 commands; actual fresh-install path is 6 (clone, install mage, build, export `OPENCODE_API_KEY`, run, curl). `main.go:141-143` + `templates/starter.yaml:22-30` require `OPENCODE_API_KEY`; the README Quickstart never names this. The `envinject.Snippet` printed at startup (`main.go:152-154`, `internal/envinject/snippet.go:7-19`) is the canonical Claude-Code wiring, and is unmentioned. `solo-dev-distribution/frame.md:92` already prescribed a "2-sentence 'how to get running'" pattern leveraging the embedded starter + env-inject. | STRONG |
| **Dim C: Supporting docs surface is inconsistently mapped** | `config.example.yaml` and `cmd/freedius/templates/starter.yaml` are two different YAMLs (`Dim D D6`), neither referenced by name. `Dockerfile` + `docker-compose.yml` exist and `magefiles/mage.go:533-579` exposes `dockerBuild/dockerRun/dockerPush`, but README has no Docker section. `AGENTS.md:1-42` overlaps the README Development section with commands the README omits (`mage run`, `mage govulncheck`, `mage installHooks`). `scripts/pre-commit`, `pre-push`, `auto-format.sh` are unmentioned. The `envinject` hint printed at startup is the actual first-run mechanism. | STRONG |
| **Dim D: 24 catalogued defects, 6 critical** | Critical: (D1) `added_at` claimed in `README.md:96` and `README.md:115-118` but no template renders it (verified across all 9 templates in `proxy/web/templates/`). (D2) `README.md:6` "zero external runtime dependencies" contradicted by `layout.html:9-11` loading Geist font from `cdn.jsdelivr.net` without SRI — still-open prior F9. (D3) `README.md:39-41` "Pre-built static binaries are published on every tagged release" — `git tag -l` empty, Releases page empty. (D4) `README.md:112` "Request events" page does not exist (sidebar in `layout.html:26-43` has only Dashboard/Mappings/Providers/Logs). (D5) Quickstart curl body is Anthropic shape but default `opus` mapping lands on `go` provider with OpenAI base URL — `MixAdapter` does suffix detection (`proxy/mix.go:73-79`) but this translation layer is undocumented. (D6) README example config (`README.md:65-69`) and embedded starter (`templates/starter.yaml:22-30`) disagree on `sonnet`, `haiku`, `default`, and `auto`. | STRONG |
| **Dim E: LICENSE + first tag are upstream blockers** | Glob: no `LICENSE*` file at repo root or anywhere. `git tag -l` returns empty. Without LICENSE: Homebrew tap (recommended by `solo-dev-distribution/frame.md:113-115`) cannot be accepted; `go install @latest` semantics for redistribution are legally undefined; GoReleaser archives will include no license. Without first tag: `go install @latest` produces pseudo-version; Releases link is dead; `freedius --version` is a commit hash, not a release identifier; the four README claims that depend on a tag (`README.md:26, 39-41, 44, 47`) overstate reality. | STRONG |

## Narrowing Signals

- The prior frame `solo-dev-positioning/frame.md:96-98` explicitly narrowed
  the persona to "returning maintainer" and the README inherited that
  framing (`README.md:5` "solo-dev maintainer"). That narrowing has since been
  formally re-examined by `solo-dev-distribution/frame.md:70-72` (verdict:
  the narrowing overrode the PRD without PRD-level justification) and is now
  contradicted by the user's current ask ("ready to sell"). The README is
  wearing a frame that has been walked back.
- `solo-dev-distribution/frame.md:88-94` already designed the two-moment
  README structure ("2-sentence 'how to get running'...then pivot to 'how
  to read the system state' for the returning maintainer"). That design
  has not landed in the README.
- `solo-dev-distribution/research.md:100` already noted that the README does
  not surface the embedded starter config (which means zero-config first run).
- The user's locked scope ("README + supporting docs") is consistent with
  the structural scope, not a polish scope: the supporting docs (embedded
  starter, env-inject, Docker) are the load-bearing artifacts the README
  needs to surface, not optional add-ons.
- The user's locked decision to defer the privacy/sovereignty angle is
  consistent with the framing docs: `leak-positioning-angle/frame.md` is
  still in `preparing` and its plan has not yet been written. The neutral
  capability framing in this change should not pre-empt that frame's lead.
- The six critical defects (D1–D6) are independently confirmed by an
  unanchored pressure-test agent that walked the README cold.

## Cross-System Convention

Peer Go CLI tools with dashboards (gost, ngrok, mitmproxy, cloudflared,
direnv, aws-vault — surveyed in `solo-dev-distribution/research.md:157-175`
and `frame.md:53-66`) all follow a single continuous narrative: one-paragraph
value proposition → install → get started (3-step quickstart) → features/ongoing
use. None create a separate "maintainer mode." All include badges (CI, release,
license). All include a CONTRIBUTING.md. Most include a LICENSE. gost, ngrok,
and mitmproxy each lead with a single image or animated GIF.

freedius's current README is the documented outlier (`solo-dev-distribution/frame.md:67-68`):
it treats the gap-between-uses as the primary friction point and structures
the README around that. The user's "ready to sell" ask is a return to the
peer convention, not a novel position.

## Reframed Problem Statement

> **The actual problem to plan around is:** the README and supporting docs
> currently serve a "returning maintainer" persona that a prior frame
> (`solo-dev-positioning/frame.md:96-98`) imported from a single conversation,
> that frame has since been formally walked back (`solo-dev-distribution/frame.md:70-72`),
> and the user has now explicitly asked for a "ready to sell, ready to use"
> surface for technical evaluators. The deliverable is not a polish pass on
> the existing memo — it is a structural rewrite to a two-moment narrative
> anchored on a problem statement (per `solo-dev-distribution/frame.md:88-94`),
> with a working three-step quickstart that uses the actually-installed
> artifacts (embedded starter, env-inject snippet, version-aware install),
> and the supporting docs (embedded starter, env-inject, Docker, AGENTS.md)
> surfaced in a way a fresh evaluator can actually follow. The work is
> ordered by an upstream prerequisite (LICENSE + first git tag) that the
> framing inherits from the foundation docs and the `solo-dev-distribution`
> plan.

What this means concretely for what the plan needs to do (not *how* — that's
plan's job):

1. **Anchor a problem statement in the first 30 lines**, capability-oriented
   (per user's locked scope), drawing on the value proposition already in
   `prd.md:20-22`, `shape-notes.md:39-45`, `roadmap.md:20-24`. Not the
   privacy/sovereignty angle (deferred to `leak-positioning-angle`).
2. **Rewrite Quickstart as a working three-step path** that names the
   `OPENCODE_API_KEY` env var, surfaces the embedded starter's zero-config
   behavior, and references the env-inject snippet as the Claude-Code
   wiring step. The `mage build` recipe becomes a development-only option,
   not the headline.
3. **Fix the six critical defects** (D1–D6 above) — these are not
   optional, they make the document false.
4. **Surface the supporting docs surface** — the embedded starter,
   `envinject` mechanism, `Dockerfile`/`docker-compose.yml`, `AGENTS.md`
   overlap, `scripts/` hooks.
5. **Defer the user-locked exclusions** explicitly: no privacy/sovereignty
   lead; no per-LiteLLM "Why not X?" table unless it lands in this change's
   plan; no scope creep into the dashboard's `added_at` rendering (that's
   `mapping-first-ui-refactor`).

## Confidence

- **HIGH** — strong evidence across all five dimensions, matches peer-tool
  convention, the structural reframe is independently confirmed by an
  unanchored pressure-test, and the ordering constraint (LICENSE + first
  tag upstream of "ready to sell") is verifiable from current repo state.

## What Changes for /10x-plan

The plan should be a **structural rewrite of the README and a calibration of
the supporting docs surface**, ordered as:

1. **Upstream prerequisite** — add LICENSE; cut the first `v*` tag and verify
   the GitHub Release page is no longer empty. (This is the gate; without
   it, the README's claims remain aspirational.)
2. **Defect fix pass on the existing README copy** — resolve the six critical
   defects (D1–D6) and the "important" tier (D7–D19), calibrate the 24-item
   defect catalogue down to the items the plan can verify.
3. **Structural rewrite of the opening + Quickstart** — anchor the
   problem statement, rework Quickstart to the working three-step path
   (using the embedded starter + env-inject), reorder sections per peer-tool
   convention.
4. **Supporting docs calibration** — surface the embedded starter as the
   canonical example (replacing the drift with `config.example.yaml`), add a
   Docker section, link `AGENTS.md`, mention `scripts/` hooks.
5. **Polish** — badges (CI, release, license), supported-providers list,
   "Why freedius?" callout, syntax-correct flag table (`--host` not `-host`),
   canonical response-headers and built-in-endpoints sections, and a
   "Verified by release" note for the previously-claimed features.

The plan must NOT touch the dashboard templates, the `added_at` rendering,
the foundation docs (`prd.md`, `shape-notes.md`, `roadmap.md`), or the
leak-positioning angle. Those are owned by other changes.

## References

### Source files
- `/home/pawel/code/freedius/README.md` — current state (183 lines, 6.4 KB)
- `/home/pawel/code/freedius/cmd/freedius/main.go:45` — `var version = "dev"`
- `/home/pawel/code/freedius/cmd/freedius/main.go:141-143` — `checkRequiredEnvVars` enforcement
- `/home/pawel/code/freedius/cmd/freedius/main.go:152-154` — env-inject snippet printed
- `/home/pawel/code/freedius/cmd/freedius/templates/starter.yaml` — embedded starter (canonical)
- `/home/pawel/code/freedius/config.example.yaml` — top-level example (drift vs. starter)
- `/home/pawel/code/freedius/internal/envinject/snippet.go:7-19` — `Snippet()` output
- `/home/pawel/code/freedius/internal/envinject/settings.go:25-79` — `WriteSettingsJSON` (dead code)
- `/home/pawel/code/freedius/proxy/web/templates/layout.html:9-11` — CDN font load (open F9)
- `/home/pawel/code/freedius/proxy/web/templates/mappings-table.html` — no `AddedAt` reference
- `/home/pawel/code/freedius/proxy/web/handlers.go:350` — `AddedAt` populated, never rendered
- `/home/pawel/code/freedius/proxy/web/types.go:81` — `AddedAt string` field
- `/home/pawel/code/freedius/.goreleaser.yaml:1-43` — release pipeline (ready, unexercised)
- `/home/pawel/code/freedius/.github/workflows/release.yml` — tag-trigger workflow
- `/home/pawel/code/freedius/Dockerfile:1-19` — distroless static
- `/home/pawel/code/freedius/docker-compose.yml:1-12` — local compose
- `/home/pawel/code/freedius/AGENTS.md:1-42` — overlaps Development section
- `/home/pawel/code/freedius/magefiles/mage.go:533-579` — Docker targets

### Related research and frames
- `context/foundation/prd.md:20-22, 26, 32, 45-55` — value proposition, persona, US-01
- `context/foundation/shape-notes.md:39-45` — value proposition
- `context/foundation/roadmap.md:20-24` — value proposition
- `context/changes/solo-dev-positioning/frame.md:96-98, 52-53, 89, 121` — narrow frame now walked back
- `context/changes/solo-dev-distribution/frame.md:70-92, 113-115` — reframe + two-moment design + distribution
- `context/changes/solo-dev-distribution/research.md:100, 157-175, 220-251` — embedded starter note + peer convention
- `context/changes/solo-dev-distribution/reviews/impl-review.md:23-104` — F1, F5, F6, F9 status
- `context/changes/leak-positioning-angle/frame.md:79-95` — lead copy deferred
- `context/changes/mapping-first-ui-refactor/reviews/impl-review.md:149-157` — F9 CDN still open

### Investigation tasks
- Dim A — sales narrative evidence
- Dim B — first-run usability evidence
- Dim C — supporting docs surface
- Dim D — existing copy defects
- Pressure test (unanchored)

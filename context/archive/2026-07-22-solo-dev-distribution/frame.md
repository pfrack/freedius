---
id: solo-dev-positioning-revisit
title: Reframe — returning-maintainer narrowing vs. PRD scope
status: preparing
created: 2026-07-22
updated: 2026-07-22
plan: null
roadmap_id: null
prd_refs:
  - prd.md
  - frame.md
tags:
  - framing
  - persona
  - distribution
  - dx
---

# Frame Brief: Returning-maintainer narrowing vs. PRD scope

> Framing step before /10x-plan. This document captures what is *actually*
> at issue, separated from what was initially assumed.

## Reported Observation

The `solo-dev-positioning` change (`context/changes/solo-dev-positioning/`) narrowed freedius's persona from the PRD's "solo developer" to "returning maintainer only," explicitly excluding first-time adoption and cold-start distribution tooling. The user wants to reconsider whether that narrowing was correct.

## Initial Framing (preserved)

- **User's stated cause:** The "returning maintainer" persona is too narrow — the PRD originally said "solo developer" generally, not just returning ones. The narrowing in solo-dev-positioning went beyond PRD intent.
- **User's stated approach:** Broaden the persona to include first-time adopters, which unlocks distribution tooling (GoReleaser + Homebrew tap).
- **Pre-dispatch narrowing:** "Broader persona — first-timers included"

## Dimension Map

The observation (persona narrowing exclusion of first-timers) could originate at any of these dimensions:

1. **Signal quality** — The "returning maintainer" evidence was one conversation ("where did this mapping come from?"). Is that sufficient to permanently exclude first-timers from the persona?
2. **PRD scope vs. frame scope** — The PRD explicitly included first-time install as a primary success criterion (US-01). Did frame.md legitimately narrow the scope, or extend beyond the PRD's intent?
3. **Distribution maintenance burden** — Is "distribution adds ongoing solo-dev burden" a real constraint or a perceived one? Research found GoReleaser + Homebrew is near-zero ongoing maintenance.
4. **Lifecycle moments** — Are "first install" and "returning use" different personas or the same person at different times? If the same person, one narrative covers both; if different, two narratives or two personas are needed.
5. **Cross-system convention** — How do peer tools (gost, ngrok, mitmproxy) handle the first-time vs returning-use distinction?

The user's initial framing (frame.md) lands on dimension #1 (signal quality — one conversation about provenance) and dimension #4 (lifecycle moments — returning maintainer as a distinct primary persona).

## Hypothesis Investigation

| Hypothesis | Evidence | Verdict |
| --- | --- | --- |
| **Dim #1: Signal quality insufficient for permanent exclusion** | frame.md narrowing was based on a single maintainer conversation. No broader user study, no inbound user complaints about first-time friction. The "deliberately rejected" framing at `frame.md:52-53` describes a decision made *within the frame*, not evidence found in user data. | STRONG |
| **Dim #2: PRD scope was overridden, not aligned** | `prd.md:32` — primary success criterion explicitly requires a dev installing and making their first routed call. `prd.md:45-55` — full US-01 user story for first-time setup. `prd.md:26` — persona is "solo developer," never "maintainer." `frame.md:96-98` explicitly says "the maintainer IS the end user — no cold-start persona write-up." The PRD never stated or implied this exclusion. | STRONG |
| **Dim #3: Distribution maintenance burden is near-zero** | GoReleaser + Homebrew tap requires only `git tag && git push` per release. Between releases: zero config changes needed. Setup is ~1 hour (one-time). GoReleaser auto-generates formula and manifest on each tag. Research doc (`solo-dev-distribution/research.md`) already has working config snippets. `framework.md:124-128` never investigated distribution; it assumed "solo-dev burden" without checking the actual mechanics. | STRONG |
| **Dim #4: Lifecycle moments — same person, not different personas** | Peer tool research: gost, ngrok, cloudflared, direnv, aws-vault all use a single continuous narrative (install → get started → ongoing features). None treat "returning after a gap" as a *separate* primary persona. The "returning maintainer" framing is *unique* to freedius — no peer tool targets resumability as the primary friction point. Dashboards in all peer tools serve operational visibility for *any* user at *any* time, not specifically returning maintainers. | STRONG |
| **Dim #5: Cross-system convention — single narrative for both moments** | All surveyed Go CLI tools with dashboards (gost, ngrok, mitmproxy, cloudflared) serve both first-time and returning users through one README structure: "Install" → "Get started" → "Features/Ongoing use." The dashboard is the connective tissue, accessible to both moments. No tool creates a separate "maintainer mode." | STRONG |

## Narrowing Signals

- The PRD's primary success criterion (`prd.md:32`) requires a dev installing and routing their first call — this is unambiguously a first-time scenario, yet frame.md excluded "cold-start" from scope.
- The word "maintainer" appears in frame.md for the *first time* at line 89 — it does not exist in `prd.md` or `shape-notes.md`. The narrowing created a new term ("maintainer") that the PRD never used.
- frame.md's "deliberately rejected" claim (`frame.md:121`) references a decision *it made*, not evidence found elsewhere: "NOT expand into README / cold-start / PRD terrain — that framing was deliberately rejected" — rejected by *whom*? The PRD never rejected it.
- GoReleaser's actual mechanics (research doc + external docs confirmation) show zero ongoing maintenance between releases. The "solo-dev burden" argument was asserted, not investigated.
- Peer tool research: freedius's "returning maintainer as primary persona" is unique. No comparable tool treats "resumability after a gap" as the primary user moment.

## Cross-System Convention

The standard pattern for CLI tools with dashboards is a **single continuous narrative**: install → get started → ongoing features. The dashboard serves *operational visibility for any user at any time*. Peer tools (gost, ngrok, cloudflared, mitmproxy, direnv, aws-vault) all follow this pattern. None create a separate "maintainer mode."

freedius's "returning maintainer" framing is an outlier — it treats the gap-between-uses as the *primary* friction point, not an afterthought. The breadcrumb-card provenance feature (the specific signal that triggered the frame) serves *resumability*, which is genuinely useful — but it does not justify making "returning maintainer" the *exclusive* persona that gates all other DX work (distribution, README scope, first-run friction).

## Reframed Problem Statement

> **The actual problem to plan around is:** The `solo-dev-positioning` frame narrowed freedius's persona from the PRD's "solo developer" to "returning maintainer only" based on one conversation's signal, without PRD-level justification, and without investigating the actual maintenance cost of the distribution tooling it excluded. The PRD's primary success criterion (first-time install and first routed call) requires serving a first-install moment that the frame explicitly closed. Peer tools serve both moments through one continuous narrative — first-time adoption and returning use are not competing scope decisions, they are the same person at different times.

The narrower frame served one implementation cycle (enriching breadcrumb cards) well — the provenance feature is valid and valuable. But narrowing it to a *permanent* persona exclusion has three costs:

1. **It excludes work that would help actual users.** The discarded `checkRequiredEnvVars` return value (`main.go:134`), the undocumented starter config (`main.go:155-172`), and the version string stuck at `"dev"` (`main.go:45`) all affect first runs, not maintaining sessions. These are known-pattern gaps that the "returning maintainer only" frame prevents anyone from fixing.

2. **It contradicts the PRD's intent without evidence that the PRD was wrong.** The PRD defined a broader persona; frame.md narrowed it without PRD-level discussion or user-data supporting the narrower view.

3. **It was built on a burden assumption that doesn't hold.** The "distribution is too much maintenance for a solo dev" argument was asserted, not investigated. GoReleaser + Homebrew + Scoop requires zero ongoing maintenance per release beyond `git tag && git push`.

## Confidence

- **HIGH** — strong evidence across all five dimensions. The PRD-level discrepancy is verifiable from `prd.md:32` vs `frame.md:96-98`. The maintenance-burden claim is empirically refuted by GoReleaser's actual mechanics. The cross-system convention finding (all peer tools use one narrative) is consistent across 6 surveyed tools. The signal-quality finding (one conversation) is self-evident. No hypothesis contradicts the others; they converge on the same reframe.

## What Changes for /10x-plan

If we adopt a **two-moment model** (first encounter + returning use, same persona at different lifecycle points), the plan should:

- Treat the returning-maintainer feature work (breadcrumb provenance, env-status indicators, fallback rationale) as **unchanged** — these are valid and valuable.
- Add first-moment DX work surfacing by the existing mechanics: `debug.ReadBuildInfo()` for version display, un-discard `checkRequiredEnvVars` at `main.go:134`, document the starter config auto-write in README.
- Rephrase README to serve both moments without diluting either: 2-sentence "how to get running" (since starter config means zero-config first run), then pivot to "how to read the system state" for the returning maintainer. One README, two sections, no "cold-start pitch" needed.

The return-maintainer breadcrumb-card work from `solo-dev-positioning` continues unchanged.

## Distribution Shipping Options (from research)

If the reframed scope includes first-time adopters, the following shipping mechanisms are ranked by fit for freedius:

| Approach | Setup effort | Maintenance | Solo-dev UX | Platform | Fit |
|---|---|---|---|---|---|
| **GoReleaser + GitHub Releases** | 1 YAML file | Near-zero (tag-driven) | Excellent | macOS/Linux/Windows | ★★★★★ |
| **Homebrew tap** (via GoReleaser) | +10 lines YAML + tap repo | Zero (auto-generated formula) | `brew install pfrack/freedius/freedius` | macOS + Linux | ★★★★★ |
| **Scoop bucket** (via GoReleaser) | +10 lines YAML + bucket repo | Zero (auto-generated manifest) | `scoop install pfrack/freedius` | Windows | ★★★★ |
| **`go install @latest`** | LOW (tag + debug.ReadBuildInfo()) | LOW | Compiles from source | All | ★★★★ |
| **Self-update** (`go-selfupdate`) | MEDIUM | MEDIUM | `freedius update` | All | ★★★★ |
| **Nix NUR** | MEDIUM-HIGH | MEDIUM | `nix run pfrack#freedius` | NixOS | ★★★ |
| **uvx** | HIGH (5 platform wheels) | HIGH | `uvx freedius` | All | ★★ — skip, wrong audience |
| **install.sh** | HIGH | HIGH | `curl | sh` | Unix | ★★ — skip, maintenance trap |

**Recommended stack** (one-time setup, near-zero ongoing maintenance):

- **Primary**: GoReleaser + GitHub Releases + Homebrew tap (`pfrack/homebrew-freedius`)
- **Secondary**: Scoop bucket (`pfrack/scoop-freedius`) for Windows
- **Already unlocked**: `go install @latest` after first tag + `debug.ReadBuildInfo()` fix
- **Skip**: uvx (audience mismatch, 5+ platform wheels), install.sh (rustup-level maintenance burden)

Distribution is additive — it does not compete with provenance features for scope or attention.

## References

- `context/foundation/prd.md:26` — original "solo developer" persona definition
- `context/foundation/prd.md:32` — primary success criterion requiring first-time install
- `context/foundation/prd.md:45-55` — US-01: first-time route-through user story
- `context/foundation/shape-notes.md:35` — "easy as fuck to configure" shaping decision
- `context/changes/solo-dev-positioning/frame.md:96-98` — "the maintainer IS the end user — no cold-start persona write-up"
- `context/changes/solo-dev-positioning/frame.md:52-53` — "deliberately rejected" claim (no PRD-level evidence)
- `context/changes/solo-dev-positioning/frame.md:89` — "solo dev maintainer" term introduced here (absent from PRD)
- `cmd/freedius/main.go:45` — `var version = "dev"` (broken version display)
- `cmd/freedius/main.go:134` — `_ = checkRequiredEnvVars(cfg)` (discarded return value)
- `cmd/freedius/main.go:155-172` — starter config auto-write (undocumented)
- `context/changes/solo-dev-distribution/research.md` — distribution patterns, GoReleaser configs, maintenance analysis
- `.github/workflows/ci.yml` — existing CI pipeline (release CI would be separate, tag-triggered)
- `magefiles/mage.go:139-142` — bare `go build` target (no ldflags, no cross-compile)
- Diversified research: gost (Go proxy — GoReleaser + Homebrew + Snap + Docker), ngrok (install funnel → ongoing features), cloudflared (dashboard as gate, not returning-only), mitmproxy (parallel UIs for different expertise levels, not personas)

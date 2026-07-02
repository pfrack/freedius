# Quality Gates in CI — Plan Brief

> Full plan: `context/changes/quality-gates-in-ci/plan.md`
> Research: `context/changes/quality-gates-in-ci/research.md`

## What & Why

The `mage ci` pipeline has 6 gates but several documented gaps: no format check (despite AGENTS.md claiming gofumpt is enforced), no module verification, no tidy check, tools recompile from source every run, and no workflow resilience features. This plan closes the 7 highest-impact gaps without adding complexity.

## Starting Point

`mage ci` runs Vet → GenerateCheck → Test → Lint → Build → Govulncheck serially (6 gates). The workflow has no concurrency cancellation or timeout. `~/go/bin` is not cached. Tool version constants are decorative. Pre-commit only runs `mage lint`. AGENTS.md and README.md contain stale claims.

## Desired End State

`mage ci` runs 9 gates (Vet → ModVerify → TidyCheck → GenerateCheck → FormatCheck → Test → Lint → Build → Govulncheck). Workflow has concurrency cancellation and 15-min timeout. Tools are cached. Pre-commit runs lint + generateCheck. Documentation is accurate.

## Key Decisions Made

| Decision | Choice | Why |
|----------|--------|-----|
| Scope | High-impact subset (7 gaps) | Shippable in one session, low risk; defers coverage threshold, parallelization, build flags |
| Format check | `golangci-lint fmt --diff` | Uses existing `.golangci.yaml` formatters; zero new config or tool installs |
| Pre-commit depth | lint + generateCheck | Biggest drift class (generated files) at negligible cost; full ci too slow for hooks |
| Tool caching | `actions/cache` on `~/go/bin` | Eliminates 30-60s recompile per run; keyed on version constants |
| Docs fix | Replace Makefile refs with mage | Aligns README with reality in one pass |
| Redundant Vet | Drop from `Lint()` | Already runs as CI step 1; triple-output is confusing |

## Scope

**In scope:**
- Workflow: concurrency, timeout, tool caching
- Magefile: wire version constants, chain mod-verify + tidy-check, add format check, drop redundant vet
- Pre-commit: add generateCheck
- Docs: fix README.md Development section, fix AGENTS.md gofumpt claim

**Out of scope:**
- Coverage threshold enforcement
- CI parallelization (workflow job split or mg.Deps)
- Reproducible static build flags
- dependency-review-action
- Integration test convention
- .go-version file adoption

## Architecture / Approach

Two phases: (1) workflow + magefile hardening — all CI-facing changes; (2) pre-commit + docs alignment. Each phase is independently shippable. The `CI()` function at `magefiles/mage.go:287-314` is a hand-rolled `for` loop over a steps slice — adding/removing/reordering steps is a one-line change per step.

## Phases at a Glance

| Phase | What it delivers | Key risk |
|-------|-----------------|----------|
| 1. CI Pipeline Hardening | Workflow resilience, tool caching, 3 new gates, redundant vet removed | TidyCheck may fail on branches with untidy go.mod |
| 2. Pre-commit + Docs | Hook parity, accurate documentation | None — purely additive |

**Prerequisites:** None — all changes are self-contained.
**Estimated effort:** ~1-2 sessions across 2 phases.

## Open Risks & Assumptions

- `TidyCheck` will fail on any branch with an untidy go.mod — contributors must run `mage tidy` first. This is intentional (catches real drift) but may surprise on first run.
- `golangci-lint fmt --diff` behavior with the existing formatters block hasn't been tested against the full codebase yet — may surface pre-existing formatting issues that need a one-time fix.
- The cache key for `~/go/bin` must stay in sync with version constants — but since Phase 1.3 wires the constants into the install calls, this is self-enforcing.

## Success Criteria (Summary)

- `mage ci` runs 9 gates with clear `[i/9]` progress output
- CI workflow cancels superseded pushes and times out after 15 minutes
- Tools don't recompile from source on cached runs
- Pre-commit catches generated-file drift before CI
- AGENTS.md and README.md accurately describe the pipeline

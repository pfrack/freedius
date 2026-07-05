# Test Plan Refresh — Plan Brief

> Full plan: `context/changes/test-plan-refresh-2026-07-05/plan.md`
> Research: `context/foundation/test-plan.md` (the artifact being refreshed)

## What & Why

The test plan (`context/foundation/test-plan.md`) was last updated 2026-07-02. Since then, Phase 2 (streaming edge cases) and Phase 3 (quality gates in CI) completed, the Bubble Tea TUI was replaced by an embedded Web UI with its own test suite, tool versions drifted, and §7 negative-space references a TUI that no longer exists. This refresh brings the test plan back to single-source-of-truth status.

## Starting Point

The test plan tracks 6 risk scenarios across 3 rollout phases, with a cookbook of testing patterns. Phase 1 (proxy integration) is implemented, Phase 2 (streaming) and Phase 3 (quality gates) are now done. A new Web UI surface area landed with 6 test files but no cookbook entry.

## Desired End State

The test plan accurately reflects:
- Phase 2/3 marked `done`, new Phase 4 (Web UI) added as `done`
- Tool versions matching `magefiles/mage.go` (Go 1.26.4, staticcheck v0.7.0, golangci-lint v2.12.2, govulncheck v1.3.0)
- Quality gates table reflecting all 9 CI steps now enforced
- §6.7 cookbook entry for Web UI handler testing
- §7 no longer references the removed TUI
- §8 freshness dates stamped 2026-07-05

## Key Decisions Made

| Decision | Choice | Why (1 sentence) |
|----------|--------|-------------------|
| Scope | Full refresh — all 6 stale sections | User chose thoroughness over minimal update |
| Web UI cookbook | Add §6.7 | 6 new test files had no cookbook entry; pattern is distinct from proxy testing |
| §7 TUI exclusion | Replace with Web UI exclusion | TUI no longer exists; Web UI is the new management surface |
| Phase 4 | Add as `done` (not `planned`) | Web UI is already implemented and reviewed |

## Scope

**In scope:**
- §3 status updates (Phase 2/3 done, Phase 4 added)
- §4 version refresh (Go, staticcheck, golangci-lint, govulncheck, goimports, golines, gci)
- §5 quality gates enforcement status
- §6.7 Web UI cookbook patterns
- §7 exclusion updates (remove TUI, add Web UI)
- §8 freshness dates

**Out of scope:**
- Risk map (§2) re-evaluation — no new top-3 risks surfaced
- Strategy principles (§1) — unchanged
- New rollout phases beyond Web UI
- Any code changes

## Architecture / Approach

Single-phase, documentation-only edit. Update `context/foundation/test-plan.md` in place across 6 sections. No new files beyond this plan and its brief.

## Phases at a Glance

| Phase | What it delivers | Key risk |
|-------|-----------------|----------|
| 1. Refresh test-plan.md | Updated §3/4/5/6/7/8 | §6.7 patterns may not match actual test code exactly |
| 2. Write plan brief | Two-pager summary | Minor — brief is derived from plan |

**Prerequisites:** None — documentation-only change.
**Estimated effort:** ~15 minutes, single session.

## Open Risks & Assumptions

- §4 tool versions are taken from `magefiles/mage.go` constants, not re-verified against actual installed binaries (version constants are the source of truth)
- §6.7 patterns are derived from reading test files, not from running them — patterns should be validated by reading the actual test code during implementation

## Success Criteria (Summary)

- §3 statuses match archive reality (Phase 2/3 done)
- §4 versions match `magefiles/mage.go`
- §6.7 patterns match `proxy/web/*_test.go`
- §7 has no TUI references
- `mage test` still passes after the edit (sanity check)

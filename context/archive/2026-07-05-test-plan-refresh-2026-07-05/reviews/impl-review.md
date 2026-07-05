<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: Test Plan Refresh — Implementation Plan

- **Plan**: context/changes/test-plan-refresh-2026-07-05/plan.md
- **Scope**: Phase 1–2 of 2 (full plan, both complete)
- **Date**: 2026-07-05
- **Verdict**: APPROVED
- **Findings**: 0 critical, 0 warnings, 2 observations (both triaged: F1 fixed via Fix A, F2 fixed)

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | PASS |
| Scope Discipline | PASS |
| Safety & Quality | PASS |
| Architecture | PASS |
| Pattern Consistency | PASS |
| Success Criteria | PASS |

## Findings

### F1 — §2 Risk Map still describes "TUI output" after §7 declares the TUI gone

- **Severity**: 🔵 OBSERVATION
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Scope Discipline
- **Location**: context/foundation/test-plan.md:47, context/foundation/test-plan.md:58
- **Detail**: Risk #6 ("API key or sensitive config leaked in logs, error bodies, or TUI output") and its Risk Response Guidance row (Ground-truth column: "TUI data flow") both still reference the TUI. The plan correctly scoped §2 out ("Not re-evaluating the risk map — no new top-3 risks have surfaced"), and this refresh didn't touch it. But §7 now states plainly the TUI no longer exists and was replaced by the Web UI — so a reader moving from §7 back to §2 hits a live contradiction: one section says TUI is gone, the other still treats it as the exposure surface. This is a pre-existing gap the plan chose not to close, not something introduced by this refresh, but it's now more visible.
- **Fix A ⭐ Recommended**: Leave §2 untouched now (respects the plan's declared scope boundary) and open a follow-up note in §6.8 "Per-rollout-phase notes" or the next `/10x-test-plan --refresh` flagging §2 for a wording pass (TUI → Web UI / log-output surface) next time risks are revisited.
  - Strength: Keeps this refresh's diff exactly what the plan promised — documentation-only, 6 sections, no risk-map re-litigation.
  - Tradeoff: The contradiction persists in the doc until the next refresh touches §2.
  - Confidence: HIGH — matches the plan's explicit "not doing" boundary and the freshness ledger's own refresh triggers (§8: "§7 negative-space no longer matches what the team believes" is exactly this kind of drift).
  - Blind spot: None significant.
- **Fix B**: Swap "TUI" → "Web UI"/"management UI output" in both §2 spots now, even though it's technically outside this plan's declared scope.
  - Strength: Removes the contradiction immediately; it's a 2-line wording change, not a risk re-evaluation.
  - Tradeoff: Extends the change beyond what was planned and reviewed as "documentation-only, 6 sections" — a small scope creep that wasn't asked for.
  - Confidence: MEDIUM — low risk technically, but violates the plan's own stated boundary without a decision to do so.
  - Blind spot: Haven't confirmed whether risk #6's likelihood/impact rating should also change now that TUI (a local, non-networked surface) is gone and Web UI (a networked management surface) is the new exposure — that's a real re-evaluation, not just a word swap.
- **Decision**: FIXED via Fix A — added a "Known gap (deferred)" note to §8 Freshness Ledger flagging §2 for a wording/re-evaluation pass on the next refresh.

### F2 — §5 gates read unconditionally "required" while §3 still shows Phase 1 as "planned"

- **Severity**: 🔵 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: context/foundation/test-plan.md:106-110 (§5), context/foundation/test-plan.md:69 (§3)
- **Detail**: This was a deliberate, plan-documented choice (the plan's §3 contract explicitly says to keep Phase 1's status as `planned` "since the test plan tracks rollout phases, not implementation"). But the net effect after this refresh is that §5 now shows "unit + integration" and "streaming edge-case suite" as unconditionally `required` (dropping the old "required after §3 Phase 1" qualifier), while §3 still shows the very phase those gates depend on as `planned`/not-done. A reader skimming §3 alone would conclude the proxy-integration tests aren't enforced yet; §5 says otherwise. Not a defect in the implementation — it matches the plan exactly — but it's a rough edge the plan itself introduced.
- **Fix**: Add a short parenthetical to the two §5 rows, e.g. "required (enforced in CI regardless of §3 Phase 1 rollout-tracking status)", so the two sections don't read as contradictory on a quick skim.
- **Decision**: FIXED — parenthetical added to the "unit + integration" and "streaming edge-case suite" rows in §5.

## Success criteria verification (evidence gathered this review)

- Go 1.26.4 confirmed in `go.mod` and `.github/workflows/ci.yml`.
- Tool versions (`staticcheck` v0.7.0, `golangci-lint` v2.12.2, `govulncheck` v1.3.0, `goimports` v0.47.0, `golines` v0.12.2, `gci` v0.13.5) confirmed against `magefiles/mage.go` constants — exact match. `gofumpt` correctly dropped from the table since it's no longer referenced anywhere in `magefiles/mage.go` (replaced by goimports+golines+gci).
- CI 9-step pipeline (vet → mod verify → tidy check → generate check → format check → test → lint → build → govulncheck) confirmed against `magefiles/mage.go`'s `CI()` function — exact match, exact order.
- §3 Phase 2/3 `done` status confirmed against `context/archive/2026-07-02-streaming-edge-cases/` and `context/archive/2026-07-02-quality-gates-in-ci/` (both `status: archived`).
- §6.7 Web UI cookbook: all claims verified against actual code — `proxy/web/` has exactly 6 test files (embed, forms, handlers, handlers_write, log_filter, server); `newTestMux()` and `newWriteMux(t)` both exist as described; the `/dev/null/cannot-create-subdir/freedius.yaml` save-failure trick and the `"validation_failed"` JSON error key both appear verbatim in the test/source files cited.
- 323 total `func Test*` across the codebase confirmed via project-wide grep, matching the plan's Key Discoveries claim.
- No TUI references remain outside §2 (confirmed via grep) — §7 is clean as required by the success criteria.
- All markdown tables checked for column-count consistency (no ragged rows introduced by the edit).
- Diff scope matches the plan exactly: only `context/foundation/test-plan.md` plus this change's own `plan.md`/`plan-brief.md`/`change.md` were touched — no unplanned files.

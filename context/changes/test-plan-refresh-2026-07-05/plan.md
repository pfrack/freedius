# Test Plan Refresh — Implementation Plan

## Overview

Refresh `context/foundation/test-plan.md` to reflect the project's current state as of 2026-07-05. The test plan was last updated 2026-07-02 and has fallen behind: Phase 2 (streaming) and Phase 3 (quality gates in CI) are now done, the Bubble Tea TUI has been replaced by an embedded Web UI with its own test suite, tool versions have drifted, and §7 negative-space no longer matches reality.

## Current State Analysis

- **§3 Phased Rollout**: Phase 2 (streaming edge cases) archived 2026-07-02; Phase 3 (quality gates in CI) archived 2026-07-02. Both still show `planned`/`not started`.
- **§4 Stack**: Go 1.26.4, staticcheck v0.7.0, golangci-lint v2.12.2, govulncheck v1.3.0. Stack grounding section references Context7 docs MCP — verify still accurate.
- **§5 Quality Gates**: Phase 1 (proxy integration) and Phase 2 (streaming) are now enforced via `mage ci`. Phase 3 (CI quality gates) is now landed. The gates table doesn't reflect this.
- **§6 Cookbook**: No Web UI testing patterns. The `proxy/web/` package has 6 test files (server, handlers, handlers_write, forms, log_filter, embed) with ~30 test functions — cookbook gap.
- **§7 What We Don't Test**: Lists "TUI visual layout" as an exclusion — TUI no longer exists. Must replace with Web UI exclusion scope.
- **§8 Freshness Ledger**: All dates are 2026-07-02, now 3 days stale.

### Key Discoveries:

- TUI is completely gone from the codebase — no `tui` references remain in any Go file
- Web UI has 6 new test files in `proxy/web/` covering CRUD write operations, form validation, log filtering, embedded assets, and server lifecycle
- CI pipeline runs 9 steps: vet → mod verify → tidy check → generate check → format check → test → lint → build → govulncheck
- 323 total test functions across the codebase
- The `context/archive/2026-07-02-quality-gates-in-ci/` confirms Phase 3 is complete
- The `context/changes/web-ui/` change is at `impl_reviewed` status

## What We're NOT Doing

- Not changing any test code — this is a documentation-only refresh
- Not re-evaluating the risk map (§2) — no new top-3 risks have surfaced
- Not adding new rollout phases beyond Web UI (Phase 4)
- Not changing the strategy principles (§1)

## Implementation Approach

Single-phase, documentation-only change. Edit `context/foundation/test-plan.md` in place, updating 6 sections. No code changes, no new files (except this plan and its brief).

## Phase 1: Refresh test-plan.md

### Overview

Update all stale sections of `context/foundation/test-plan.md` to reflect the project state as of 2026-07-05.

### Changes Required:

#### 1. §3 Phased Rollout — Status Updates

**File**: `context/foundation/test-plan.md`

**Intent**: Mark Phase 2 and Phase 3 as done, add Phase 4 (Web UI) as a new completed phase.

**Contract**: Update the Status column in the §3 table:
- Phase 1 (Proxy integration): `planned` → remains `planned` (implementation is done but plan says `planned`; keep as-is since the test plan tracks rollout phases, not implementation)
- Phase 2 (Streaming edge cases): → `done`
- Phase 3 (Quality gates in CI): → `done`
- Add Phase 4 row: Web UI CRUD + handlers, risks: new surface area, test types: unit + integration, status: `done`, change folder: `web-ui`

#### 2. §4 Stack — Version Refresh

**File**: `context/foundation/test-plan.md`

**Intent**: Update tool versions to match `magefiles/mage.go` constants and verify stack grounding.

**Contract**: Update the §4 table:
- Go version: confirm 1.26.4 (matches `ci.yml`)
- staticcheck: v0.7.0
- golangci-lint: v2.12.2
- govulncheck: v1.3.0
- goimports: v0.47.0
- golines: v0.12.2
- gci: v0.13.5

Update "Stack grounding tools" dates:
- Context7 docs MCP: checked 2026-07-05
- Search: none available in current session (unchanged)
- Runtime/browser: none — not used (unchanged)

#### 3. §5 Quality Gates — Enforcement Status

**File**: `context/foundation/test-plan.md`

**Intent**: Update the "Required?" column to reflect which gates are now enforced.

**Contract**: Update the §5 table:
- lint + typecheck: `required` (unchanged)
- unit + integration: `required after §3 Phase 1` → `required` (Phase 1 landed)
- streaming edge-case suite: `required after §3 Phase 2` → `required` (Phase 2 landed)
- race detection: `required` (unchanged)
- Add row: CI pipeline (mage ci) — 9-step pipeline now includes format check, generate check, tidy check, mod verify — `required` (Phase 3 landed)

#### 4. §6 Cookbook — Add §6.7 Web UI Testing Patterns

**File**: `context/foundation/test-plan.md`

**Intent**: Add cookbook entry for Web UI testing patterns discovered during the Web UI implementation.

**Contract**: Add §6.7 with this structure:

```markdown
### 6.7 Adding a test for a Web UI handler

- **Location**: `proxy/web/<feature>_test.go` (same package `web`).
- **Test mux setup**: Use `newTestMux()` for read-only handlers or `newWriteMux(t)` for CRUD handlers that need a temp config file on disk.
- **Pattern**: Table-driven with `[]struct{ name string; path string; wantStatus int; ... }`.
- **Form data**: Send `application/x-www-form-urlencoded` bodies; assert status codes and in-memory config mutations.
- **Save-failure rollback**: Use an unwritable `CfgPath` (e.g. `/dev/null/cannot-create-subdir/freedius.yaml`) to force `SaveData` failure; assert in-memory state is restored before mutex release.
- **Validation errors**: Assert JSON error body contains `"validation_failed"` and the specific field error message.
- **Reference tests**: `proxy/web/handlers_test.go` (page handlers, static, health), `proxy/web/handlers_write_test.go` (CRUD), `proxy/web/forms_test.go` (validation), `proxy/web/log_filter_test.go` (log level filtering).
- **Run locally**: `mage test`.
```

#### 5. §7 What We Don't Test — Update Exclusions

**File**: `context/foundation/test-plan.md`

**Intent**: Remove TUI exclusion (TUI no longer exists), add Web UI exclusion scope.

**Contract**: Replace the TUI exclusion with:

```markdown
- **Web UI visual layout and responsiveness** — the Web UI is a management surface, not the product core. Break it and fix it, but don't slow the pipeline for pixel-perfect layout testing. Re-evaluate if Web UI becomes a primary user interaction surface. (Source: web-ui change, 2026-07-05.)
```

Keep the generated code and magefile exclusions unchanged.

#### 6. §8 Freshness Ledger — Update Dates

**File**: `context/foundation/test-plan.md`

**Intent**: Stamp the refresh date.

**Contract**: Update §8:
- Strategy (§1–§5) last reviewed: 2026-07-05
- Stack versions last verified: 2026-07-05
- AI-native tool references last verified: N/A (no AI-native tools in use)

### Success Criteria:

#### Automated Verification:

- File parses as valid markdown: visual inspection
- No broken internal links or references

#### Manual Verification:

- §3 Phase 2/3 statuses match archive reality
- §4 tool versions match `magefiles/mage.go` constants
- §5 gates match what `mage ci` actually runs
- §6.7 patterns match actual test code in `proxy/web/*_test.go`
- §7 no longer references TUI
- §8 dates are current

**Implementation Note**: After completing this phase and all automated verification passes, pause here for manual confirmation from the human that the manual testing was successful before proceeding to the next phase.

---

## Phase 2: Write Plan Brief

### Overview

Create `context/changes/test-plan-refresh-2026-07-05/plan-brief.md` summarizing this refresh.

### Changes Required:

#### 1. Write plan-brief.md

**File**: `context/changes/test-plan-refresh-2026-07-05/plan-brief.md`

**Intent**: Two-pager summarizing the refresh for quick review.

**Contract**: Follow the plan-brief template from the 10x-plan skill.

### Success Criteria:

#### Automated Verification:

- File exists at the expected path

#### Manual Verification:

- Brief accurately summarizes all 6 section changes
- Brief fits on ~2 printed pages

---

## Testing Strategy

### Unit Tests:

- N/A — documentation-only change

### Integration Tests:

- N/A — documentation-only change

### Manual Testing Steps:

1. Read the refreshed `test-plan.md` end-to-end
2. Verify §3 Phase statuses match `context/archive/` reality
3. Verify §4 versions match `magefiles/mage.go`
4. Verify §6.7 patterns match actual test code
5. Verify §7 has no TUI references
6. Run `mage test` to confirm test suite still passes (sanity check)

## Performance Considerations

None — documentation-only change.

## Migration Notes

None — in-place file edit.

## References

- Test plan being refreshed: `context/foundation/test-plan.md`
- Quality gates archive: `context/archive/2026-07-02-quality-gates-in-ci/change.md`
- Streaming archive: `context/archive/2026-07-02-streaming-edge-cases/change.md`
- Web UI change: `context/changes/web-ui/change.md`
- Mage targets: `magefiles/mage.go` (tool version constants)
- CI config: `.github/workflows/ci.yml`

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles.

### Phase 1: Refresh test-plan.md

#### Automated

- [x] 1.1 File parses as valid markdown — b79fb2d
- [x] 1.2 No broken internal links or references — b79fb2d

#### Manual

- [x] 1.3 §3 Phase 2/3 statuses match archive reality — b79fb2d
- [x] 1.4 §4 tool versions match magefiles/mage.go — b79fb2d
- [x] 1.5 §5 gates match mage ci pipeline — b79fb2d
- [x] 1.6 §6.7 patterns match proxy/web/*_test.go — b79fb2d
- [x] 1.7 §7 has no TUI references — b79fb2d
- [x] 1.8 §8 dates are current — b79fb2d

### Phase 2: Write Plan Brief

#### Automated

- [x] 2.1 plan-brief.md exists at expected path

#### Manual

- [x] 2.2 Brief accurately summarizes all changes
- [x] 2.3 Brief fits on ~2 pages

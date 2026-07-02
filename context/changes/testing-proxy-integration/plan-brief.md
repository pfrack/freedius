# Testing Proxy Integration — Plan Brief

> Full plan: `context/changes/testing-proxy-integration/plan.md`
> Research: `context/changes/testing-proxy-integration/research.md`

## What & Why

Add integration + unit tests covering 5 risks from the test plan (translation format, routing, config validation, error propagation, API key leakage). Includes implementing API key redaction in `translateUpstreamError` to fix the upstream error body snippet leakage vector. This is rollout Phase 1 of `context/foundation/test-plan.md`.

## Starting Point

The proxy has ~255 tests across 25 files. Research mapped all 5 risks to specific code paths and identified gaps: no full Anthropic response schema verification (substring checks only), no multi-provider routing test, no test for large/malformed/HTML upstream error bodies, and no runtime test for API key leakage. The weakest coverage is Risk #6 — only a source-code comment check exists.

## Desired End State

Every risk in test-plan.md §2 (#1, #3, #4, #5, #6) has at least one test proving the protection described. `translateUpstreamError` redacts API key patterns from upstream error body snippets. test-plan.md §6 cookbook patterns 6.4 and 6.5 are filled in. `mage test` passes with race detection.

## Key Decisions Made

| Decision | Choice | Why (1 sentence) | Source |
|----------|--------|------------------|--------|
| Sub-phase organization | Group by code area (3 phases) | Fewer sub-phases, groups related risks by the code they touch | Plan |
| API key redaction | Implement redaction + test | Fixes the real leakage vector identified by research, not just documenting it | Research |
| Dead code `forwardUpstreamError` | Don't remove | Out of scope for a testing change; removing production code is a separate concern | Plan |
| Test file organization | Add to existing test files | Follows project convention — tests live next to the file they test | Research |

## Scope

**In scope:**
- Integration tests for Risks #1, #3, #5, #6
- Unit tests for Risks #4, #6
- API key redaction implementation in `proxy/errors.go`
- test-plan.md §6 cookbook pattern updates

**Out of scope:**
- Streaming edge-case tests (Risk #2) — test-plan.md Phase 2
- Quality gates / CI wiring — test-plan.md Phase 3
- Removing dead code `forwardUpstreamError()`
- Testing generated code, TUI layout, magefile scripts (test-plan.md §7)

## Architecture / Approach

Tests follow the established pattern: `httptest.NewServer` for mock providers, `httptest.ResponseRecorder` for response capture, table-driven tests with `t.Setenv` for API keys. The API key redaction adds a `redactSensitive` function to `proxy/errors.go` that scans for common key patterns (`sk-`, `sk-ant-`, `Bearer `, long alphanumeric tokens adjacent to keywords) and replaces them with `[REDACTED]`. Called from `translateUpstreamError` before including upstream body snippets in the Anthropic error envelope.

## Phases at a Glance

| Phase | What it delivers | Key risk |
|-------|-----------------|----------|
| 1. Translation + routing | Anthropic response format verification, multi-provider routing test | Substring checks may be the only practical assertion for streaming SSE |
| 2. Error + config | Large/HTML/malformed error body handling, config edge cases | HTML error page handling may reveal `sanitizePrintable` needs tag stripping |
| 3. Privacy | API key redaction implementation + leakage tests | Redaction regex may produce false positives on normal error messages |

**Prerequisites:** None — this is the first rollout phase.
**Estimated effort:** ~3 sub-phases across 1-2 sessions.

## Open Risks & Assumptions

- `redactSensitive` regex patterns must be conservative enough to avoid false positives on normal error messages — verify with real upstream error bodies
- Upstream providers may include API key prefixes in error messages — this is the primary vector, not logging
- The `custom` → `mix` rewriting lesson in `lessons.md:15-19` is stale — config validation tests should use provider names as-is

## Success Criteria (Summary)

- `mage test` passes with all new tests green and race detector clean
- Every risk in test-plan.md §2 has at least one test proving the protection
- `translateUpstreamError` redacts API key patterns before including them in error messages
- test-plan.md §6 cookbook patterns 6.4 and 6.5 are filled in

<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: Custom → Mix + Protocol Field

- **Plan**: context/changes/custom-to-mix-protocol/plan.md
- **Scope**: Phase 1-4 of 4 (full plan)
- **Date**: 2026-06-18
- **Verdict**: APPROVED
- **Findings**: 0 critical, 1 warning, 3 observations

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | WARNING (1 finding — deviation is correct, plan was internally inconsistent) |
| Scope Discipline | PASS |
| Safety & Quality | PASS |
| Architecture | PASS |
| Pattern Consistency | PASS |
| Success Criteria | PASS (automated: all green; manual: 3 items pending user verification) |

## Automated Verification

```
go build ./...                       → ok
go vet ./...                         → ok
go test ./...                        → ok (5/5 packages)

TestLoad/valid_protocol_openai       → PASS
TestLoad/valid_protocol_anthropic    → PASS
TestLoad/invalid_protocol            → PASS (rejects "grpc" with "invalid protocol" error)
TestLoad/valid_custom_alias_rewrite  → PASS (custom → mix)
TestMixAdapter_ProtocolAnthropicOverridesURL → PASS
TestMixAdapter_ProtocolOpenAIOverridesURL    → PASS
TestMixAdapter_ProtocolAnthropic_ClientDisconnect → PASS (added during triage, F4)
```

## Manual Checks Still Pending

- [ ] 1.3 — Existing `freedius.yaml` with `provider: custom` loads correctly
- [ ] 2.3 — Custom + ambiguous URL + explicit protocol routes correctly
- [ ] 3.3 — End-to-end custom entry routes through mix

These require human verification with a real config and cannot be run by an automated agent.

## Findings

### F1 — Plan defect: `custom` rewrite placement instruction was self-contradictory

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: config/defaults.go:54-56 (and plan.md Phase 1 #2)
- **Detail**: Plan line 68 said "Move the `custom` check to be adjacent to the `zen`/`go` check (after defaults are applied, before the `zen`/`go` rewrite, so `custom` also rewrites to `mix`)". Implementation kept the `custom` block at lines 54-56 (top of `applyEntryDefaults`), still separated from the `zen`/`go` block at lines 67-69. Following the plan literally would have broken the feature: `knownProviderDefaults` has no `"custom"` entry (only nim/zen/go/anthropic). If the rewrite were moved to "after defaults are applied" (i.e., after line 57's lookup), the lookup would return `ok=false` for `Provider=="custom"` and the function would return at line 59 — never reaching the new rewrite block. The implementation's placement (top of function, before defaults lookup) is the only one that makes the rewrite work. The plan's prose was internally inconsistent with its own behavioral claim.
- **Fix**: Update the plan's Phase 1 #2 contract to describe the actual placement and explain the constraint.
  - Strength: Plan becomes a true source of truth; prevents the same self-contradiction from reappearing.
  - Tradeoff: None — doc-only edit.
  - Confidence: HIGH — verified by tracing execution with `custom` and `mix` inputs.
  - Blind spot: None significant.
- **Decision**: FIXED — plan.md updated (commit uncommitted in working tree)

### F2 — Stale lesson in context/foundation/lessons.md

- **Severity**: 📝 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: context/foundation/lessons.md:15-19
- **Detail**: Lesson titled "`custom` → `anthropic` Rewrite in `applyDefaults`" documents the rewrite that this change explicitly retired. The rewrite target is now `mix`, not `anthropic`. The general lesson shape (rewrite runs before validation; tests must use post-rewrite names) is still true and worth keeping, but the specific mention of `anthropic` is stale.
- **Fix**: Update the lesson title and body to reference `mix`.
- **Decision**: FIXED — lessons.md updated

### F3 — Defense-in-depth: mix protocol switch silently falls through

- **Severity**: 📝 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Safety & Quality
- **Location**: proxy/mix.go:50-57
- **Detail**: The `switch m.Protocol` has cases for `"anthropic"` and `"openai"`; any other value silently falls through to URL sniffing. `validateModel` (config/config.go:172-180) rejects unknown protocol values at startup, so this is defense-in-depth only — but if a future code path constructed a `config.Model` programmatically (skipping validation), a typo'd protocol would be silently misrouted.
- **Fix**: Add `default:` branch to the switch that logs a warning.
- **Decision**: FIXED — `default: a.logger.Warn("mix: unknown protocol, falling back to URL sniffing", "protocol", m.Protocol)` added; build/vet/tests all green

### F4 — Lost test: `TestCustomAdapter_ClientDisconnect` had no direct replacement

- **Severity**: 📝 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern Consistency
- **Location**: proxy/mix_test.go (gap, not a specific line)
- **Detail**: The deleted `proxy/custom_test.go` contained `TestCustomAdapter_ClientDisconnect`, which used a custom `failingRecorder` to verify the adapter returns `nil` after the response writer fails on Write. The plan said "The equivalent behavior is tested through mix adapter tests" — partially accurate. The Adapter Return Contract is still enforced by the dispatcher's branch and indirectly exercised by `TestMixAdapter_Upstream401_*`, but the specific failing-recorder scenario lost direct coverage.
- **Fix**: Add a `TestMixAdapter_ProtocolAnthropic_ClientDisconnect` test using the same `failingRecorder` pattern.
- **Decision**: FIXED — test added; `failingRecorder` type ported to mix_test.go; new test passes (full suite green)

## Triage Summary

- F1: FIXED (plan.md updated)
- F2: FIXED (lessons.md updated)
- F3: FIXED (default warn log added to mix.go switch)
- F4: FIXED (ClientDisconnect test + failingRecorder type ported to mix_test.go)

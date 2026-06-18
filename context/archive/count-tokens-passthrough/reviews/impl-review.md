<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: count-tokens-passthrough

- **Plan**: context/changes/count-tokens-passthrough/plan.md
- **Scope**: Full plan (3 of 3 phases)
- **Date**: 2026-06-18
- **Verdict**: NEEDS ATTENTION
- **Findings**: 0 critical  |  1 warning  |  7 observations

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | WARNING |
| Scope Discipline | WARNING |
| Safety & Quality | PASS |
| Architecture | WARNING |
| Pattern Consistency | PASS |
| Success Criteria | PASS |

## Findings

### F1 — Unplanned HEAD / and GET /health handler bypasses count_tokens 501 gate; bundled under misleading commit

- **Severity**: ⚠️ WARNING
- **Impact**: 🔬 HIGH — architectural stakes; think carefully before deciding
- **Dimension**: Plan Adherence / Scope Discipline / Architecture
- **Location**: proxy/proxy.go:71-75  +  commit 50e2eab
- **Detail**: Phase 3 commit `50e2eab` (message: "feat: add HEAD / and GET /health health-check handlers") added an early-return that returns 200 for any HEAD path AND for GET /health. The plan does not call for either; §"What We're NOT Doing" explicitly defers HEAD/OPTIONS probe support. The HEAD branch is also too broad — it bypasses the count_tokens 501 gate (HEAD /v1/messages/count_tokens now returns 200 instead of 501/405). The same commit misbundles all of Phase 3 (openai_compat.go, errors.go, proxy.go dispatcher 529 wiring, 130 lines of new tests) under a commit message that describes only the unauthorized extra.
- **Fix A ⭐ Recommended**: Revert the HEAD/health handler; keep Phase 3
  - Strength: Honors the plan ("What We're NOT Doing"); restores count_tokens → 501 contract for HEAD probes; splits Phase 3 into its own commit with the correct message.
  - Tradeoff: Loses the operator-facing /health endpoint. If a health probe is genuinely needed, ship it as a separate plan.
  - Confidence: HIGH — handler isn't referenced by any test or other call site, removal is mechanical.
  - Blind spot: Whether the operator actually wanted /health for external probes (e.g., a future container deployment); if yes, that's a separate feature with its own plan.
- **Fix B**: Narrow the HEAD check to `HEAD /` only, add tests, keep /health
  - Strength: Preserves the operator-facing health endpoint; restores the 501 path for HEAD /v1/messages/count_tokens.
  - Tradeoff: Still adds unplanned work to this change; commit hygiene issue (the "feat: add HEAD /" message would be correct, but Phase 3 would still be misbundled).
  - Confidence: MEDIUM — depends on whether /health is actually used by anything today.
  - Blind spot: The /health endpoint is unauthenticated and would be exposed if --host 0.0.0.0 is ever set.
- **Decision**: FIXED via Fix A — handler reverted (HEAD / + GET /health removed from `proxy/proxy.go:71-75`)

### F2 — `translateUpstreamError` doesn't drain `resp.Body`

- **Severity**: ℹ️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Architecture
- **Location**: proxy/errors.go:47-83
- **Detail**: Reads up to 256 bytes for the message snippet; doc says "Does NOT close resp.Body" (caller does via defer). But Go's http.Transport won't reuse the keep-alive connection when the body isn't drained to EOF, so every non-Anthropic upstream error burns a TCP connection. Cold path; previous `forwardUpstreamError` did `io.Copy` and drained.
- **Fix**: Drain the body to EOF (cap at e.g. 4 KiB) before returning, or document the keep-alive cost in the function comment.
- **Decision**: FIXED — body now drained to io.Discard via `io.LimitReader(resp.Body, 4*1024)`; doc comment updated.

### F3 — Minor DRIFT: `"upstream: "` prefix not in plan

- **Severity**: ℹ️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: proxy/errors.go:82
- **Detail**: Plan said "the message includes the upstream body snippet". Code prefixes with `"upstream: "` via `fmt.Sprintf("upstream: %s", msg)`. Intent preserved; format is a small implementation choice.
- **Fix**: Either accept the prefix as a non-material enhancement, or remove it to match the plan literally.
- **Decision**: FIXED — prefix removed; `fmt` import dropped.

### F4 — `sanitizePrintable` strips non-ASCII UTF-8

- **Severity**: ℹ️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Data safety
- **Location**: proxy/errors.go:92-100
- **Detail**: Keeps only bytes 0x20-0x7E. UTF-8 multi-byte sequences (e.g. ü = 0xC3 0xBC) are entirely stripped, with no validity check. If an upstream returns a Unicode error message, it gets silently mangled. Moot for current NIM/Anthropic providers; matters for any future provider that returns non-ASCII errors.
- **Fix**: Use `unicode.IsPrint` rune-by-rune to preserve valid UTF-8, OR document the ASCII-only assumption in the function comment.
- **Decision**: FIXED — `sanitizePrintable` now uses `unicode.IsPrint` rune-by-rune; preserves valid UTF-8.

### F5 — Empty body produces `"upstream: "` (trailing space)

- **Severity**: ℹ️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Reliability
- **Location**: proxy/errors.go:50, 82
- **Detail**: `io.ReadAtLeast(snippet, 1)` returns n=0, err=EOF on empty body; error is ignored; resulting message is `"upstream: "`. Cosmetic; tests don't cover the empty case.
- **Fix**: If n == 0, skip the prefix or use a distinct placeholder like `"upstream: <empty body>"`.
- **Decision**: FIXED via F3 (prefix removed; empty body now produces `message: ""`); new test `TestTranslateUpstreamError_EmptyBody` locks in the behavior.

### F6 — Test duplication in `TestOpenAICompat_Timeout_ReturnsAnthropicOverloaded`

- **Severity**: ℹ️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Architecture (test quality)
- **Location**: proxy/error_propagation_test.go:58-99
- **Detail**: The test calls `a.Handle`, asserts an error, then calls `writeAnthropicError` directly on a fresh recorder. The direct call duplicates `TestWriteAnthropicError` (errors_test.go:90) and the end-to-end path is already covered by `TestDispatcher_AdapterError_ForwardedAsUpstreamError` (phase2_test.go:35). 40 lines, little new coverage.
- **Fix**: Drop the direct `writeAnthropicError` half; rely on the existing tests for that coverage.
- **Decision**: FIXED — direct `writeAnthropicError` half dropped; comment points at the existing coverage.

### F7 — Stale test function names in phase2_test.go

- **Severity**: ℹ️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern consistency
- **Location**: proxy/phase2_test.go:35, 103
- **Detail**: `TestDispatcher_AdapterError_ForwardedAsUpstreamError` no longer "forwards" anything — it translates to Anthropic format. `TestFreediusErrorHandler_UnifiedShape` no longer tests the "unified" (freedius) shape — it tests Anthropic format. Cosmetic only.
- **Fix**: Rename to `TestDispatcher_AdapterError_TranslatedAsAnthropicOverloaded` and `TestFreediusErrorHandler_AnthropicFormat`.
- **Decision**: FIXED — both functions renamed; the cross-reference in `error_propagation_test.go` updated. File also renamed: `phase2_test.go` → `adapter_errors_test.go` (per user follow-up; matches the existing `<component>_test.go` naming convention).

### F8 — `freediusErrorHandler` swallows verbose-errors detail

- **Severity**: ℹ️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Architecture
- **Location**: proxy/errors.go:107-131
- **Detail**: Plan acknowledged this (verboseErrors is no longer used in the Anthropic response body, kept for API stability). The flag `--verbose-errors` is now a no-op on the transport-error path. Not a bug per the plan, but the flag becomes misleading if a developer enables it expecting upstream error detail.
- **Fix**: Document the behavior on the flag's help text, OR log the full upstream error at Debug level when verboseErrors=true.
- **Decision**: FIXED — added a Debug log gated on `verboseErrors` that includes the full upstream error string. Doc comment on `freediusErrorHandler` updated to explain the trade-off.

## Verified Success Criteria

### Automated (all PASS this run)
- 3.1 go test ./proxy/...                    ok (5.558s)
- 3.2 go test ./... integration              ok
- 3.3 go test ./... full module              ok
- 3.4 go vet ./...                           clean (exit 0)
- 3.5 go build -o freedius .                 clean (exit 0)
- 3.6 gofumpt -l proxy/ .                    clean (exit 0)

### Manual (rubber-stamp flag)
- 3.7, 3.8, 3.9 all marked [x] in the Progress section based on a single "go with all phases pls" confirmation. No diff evidence of the specific NIM 429 retry UI, unreachable-upstream 529 UI, or Anthropic streaming regression tests. Recommend re-verifying on a real Claude Code session if any of these behaviors matter to the rollout.

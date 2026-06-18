<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: Local Token Counting for OpenAI-Protocol Upstreams

- **Plan**: context/changes/openai-count-tokens/plan.md
- **Scope**: Phases 1, 2, 4 of 4 (Phase 3 partially complete — excluded per Progress checklist)
- **Date**: 2026-06-18
- **Verdict**: NEEDS ATTENTION → all findings resolved
- **Findings**: 0 critical 2 warnings 4 observations

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | PASS ✅ |
| Scope Discipline | PASS ✅ |
| Safety & Quality | PASS ✅ (all fixes applied) |
| Architecture | PASS ✅ |
| Pattern Consistency | PASS ✅ (all fixes applied) |
| Success Criteria | PASS ✅ (env fixed, go test ./... passes) |

## Findings

### F1 — json.Number re-marshal distortion in lenient re-parse

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Safety & Quality
- **Location**: proxy/translate/count.go:136-171
- **Detail**: The plan specifies `dec.UseNumber()` in the lenient re-parse path, and the implementation follows it. However, `json.Number` values re-marshalled via `json.Marshal(schema)` at line 171 produce JSON strings (`"1"` instead of `1`), distorting BPE tokenization for tool `input_schema` fields with numeric properties. The strict path (`countTools`) does not have this issue because it uses standard `json.Unmarshal` without `UseNumber`. The plan itself specified `UseNumber` without considering this side effect.
- **Fix A ⭐ Recommended**: Remove `dec.UseNumber()` from the lenient path. Accept that very large integers may lose precision — since the lenient path already contributes 0 for unparseable fields, this is acceptable best-effort behavior.
  - Strength: Eliminates the distortion class entirely; matches the strict path's behavior for numbers.
  - Tradeoff: Very large integers (>2^53) lose precision when decoded as float64 — but these contribute 0 to token count regardless in the lenient path.
  - Confidence: HIGH — the lenient path is already best-effort; precision loss for extreme values is acceptable.
  - Blind spot: None significant.
- **Fix B**: Keep `UseNumber()` but use a custom marshal that converts `json.Number` back to numeric form before re-marshalling.
  - Strength: Preserves the plan's intent of avoiding large-integer precision loss.
  - Tradeoff: More code; the lenient path is already a fallback for malformed bodies.
  - Confidence: LOW — adds complexity for a path that handles malformed input.
  - Blind spot: The set of tool schemas that hit the lenient path is unknown.
- **Decision**: FIXED (Fix A) — removed `dec.UseNumber()` from `countLenient`.

### F2 — serveLocalCountTokens silently discards Encode errors

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern Consistency
- **Location**: proxy/count_tokens_local.go:60
- **Detail**: `_ = json.NewEncoder(w).Encode(resp)` silently discards encoding errors. In the same file, `proxy.go:291-298` (`writeErrorJSON`) logs encoding failures at Error level. If `Encode` fails (e.g., connection reset), the error is invisible — operators have no signal the response was truncated or never sent.
- **Fix**: Log the error if `Encode` fails, matching the pattern in `writeErrorJSON`.
- **Decision**: FIXED — logs at Error level on encode failure.

### F3 — Error wrapping discards original strict error

- **Severity**: OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern Consistency
- **Location**: proxy/translate/count.go:114
- **Detail**: `fmt.Errorf("unmarshal anthropic request: %w", lerr)` wraps only the lenient-path error, discarding the original `err` from the strict `json.Unmarshal` at line 111. If both paths fail, the caller sees only the lenient error without context on why the strict path also failed. Additionally, the prefix `"unmarshal anthropic request:"` is inconsistent with the `"translate:"` prefix used by other exported functions in this package (e.g., `anthropic_openai.go:39` `"translate: %w"`).
- **Fix**: Use `"translate:"` prefix and wrap both errors — Go 1.20+ `fmt.Errorf("translate: strict %w; lenient %w", err, lerr)`.
- **Decision**: FIXED — matches `"translate:"` prefix, wraps both errors.

### F4 — Unnecessary mutex in getEncoder

- **Severity**: OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Safety & Quality
- **Location**: proxy/translate/count.go:54-67
- **Detail**: `getEncoder` acquires `countEncMu.Lock()` on every call, but the `countEncChx` map is initialized at package-init time and never mutated — only individual encoder slots are lazily filled via `sync.Once`. The mutex provides no synchronization benefit since `sync.Once` already guarantees visibility of the initialized fields. Map reads without concurrent writes are safe in Go.
- **Fix**: Remove `countEncMu` entirely. `sync.Once` already handles the lazy-load race; map reads are safe when no concurrent writes exist.
- **Decision**: FIXED — removed `countEncMu` and lock/unlock calls.

### F5 — Block type names more comprehensive than planned

- **Severity**: OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: proxy/translate/count.go:245-251
- **Detail**: The plan listed zero-contribution block types using abbreviated names (e.g., `web_search`, `code_execution`). The implementation uses Anthropic's full wire-format names (`web_search_tool_result`, `code_execution_tool_result`) and additionally handles `container_upload` and `mid_conv_system` — block types not enumerated in the plan. This is positive drift: the implementation is more thorough than specified, handling all known Anthropic block types for forward compatibility.
- **Fix**: Document in the plan as an addendum or accept as implementation-ahead-of-plan. No code change needed.
- **Decision**: ACCEPTED — positive drift, no code change needed.

### F6 — accuracy test fails with invalid ANTHROPIC_API_KEY in this environment

- **Severity**: OBSERVATION
- **Impact**: 🔬 HIGH — architectural stakes; think carefully before deciding
- **Dimension**: Success Criteria
- **Location**: proxy/translate/count_accuracy_test.go:28
- **Detail**: `ANTHROPIC_API_KEY` is set to a non-empty but invalid value in the current shell. The accuracy test correctly skips when the env var is unset, but since it's set (even to an invalid value), the test runs and all 8 corpus entries fail with 401. This causes `go test ./...` to fail — and would also fail in CI if this env var leaks. The Phase 4.1 CI gate (`make ci`) was marked done but `go test ./...` currently fails. Note: Phase 3 is not fully complete (3.1 accuracy test with valid key is unchecked), so this is expected state — but the failure could mask real regressions.
- **Fix**: Unset `ANTHROPIC_API_KEY` in this environment: `unset ANTHROPIC_API_KEY`. The test must skip when the key is not explicitly valid. Alternatively, validate the key format (e.g., starts with `sk-ant-`) before attempting the upstream call, skipping if it looks invalid.
- **Decision**: FIXED (environmental) — `go test ./...` passes when key is unset.

<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: First call routed — NIM adapter + custom passthrough (S-01)

- **Plan**: `context/changes/first-call-routed/plan.md`
- **Scope**: Phase 1 + 2 + 3 (all)
- **Date**: 2026-06-16
- **Verdict**: NEEDS ATTENTION
- **Findings**: 2 critical, 4 warnings, 4 observations

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | WARNING |
| Scope Discipline | PASS |
| Safety & Quality | FAIL |
| Architecture | PASS |
| Pattern Consistency | PASS |
| Success Criteria | WARNING |

## Findings

### F1 — Missing eager NIM_API_KEY startup check

- **Severity**: ❌ CRITICAL
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Plan Adherence + Safety & Quality
- **Location**: main.go:82-86
- **Detail**: plan.md:505-510 requires fail-fast at startup: if any model uses `provider: nim` and `NIM_API_KEY` is unset, exit with an actionable error before the server starts. No such check exists; main.go:84 reads `os.Getenv` and passes it to the adapter with no emptiness check. `configUsesProvider` helper never implemented (zero occurrences in source). A user with `provider: nim` in their config and unset `NIM_API_KEY` will see freedius start cleanly and get NIM's opaque 401 on the first request — the exact failure mode the plan was designed to prevent.
- **Fix A ⭐ Recommended**: Add `(*Config).UsesProvider(name string) bool` and an eager check in `run()` after `config.Load`, per plan.md:505-510.
  - Strength: Matches plan verbatim; restores the boot-time safety property.
  - Tradeoff: Adds a small helper method; couples startup ordering to config-shape knowledge.
  - Confidence: HIGH — plan is explicit, helper is a 3-line method.
  - Blind spot: None significant.
- **Fix B**: Document the deviation in plan.md as an accepted risk
  - Strength: No code change required.
  - Tradeoff: Loses the boot-time property permanently.
  - Confidence: MED — depends on whether the user values this property.
  - Blind spot: Future maintainers will be surprised by silent late-failure.
- **Decision**: PENDING

### F2 — NIM adapter returns error after WriteHeader, corrupting response

- **Severity**: ❌ CRITICAL
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Safety & Quality
- **Location**: proxy/nim.go:62-67 + proxy/proxy.go:96-99
- **Detail**: The Provider contract (plan.md:92, research.md:6.1) states: "An adapter that has called WriteHeader and then encounters an error mid-stream returns nil." NIMAdapter writes `Content-Type: text/event-stream` + `WriteHeader(200)` at nim.go:62-65, then calls `translate.TranslateStream`. If `TranslateStream` returns an error mid-stream (client disconnect, upstream reset, non-clean EOF), the error propagates to the dispatcher at proxy.go:96-99, which logs and calls `d.writeError(w, 502, "upstream error")`. That writes a JSON error body *into a response that has already been committed*, producing corrupted bytes on the wire and a misleading "adapter failed" log line.
- **Fix**: Wrap the `translate.TranslateStream(...)` return value so a post-WriteHeader error is classified as nil (Debug-log on non-Canceled) and the adapter's return becomes unconditional `return nil` after the streaming call.
  - Strength: Honors the documented Provider contract; the body stays well-formed.
  - Tradeoff: Loses visibility into non-cancel mid-stream errors (mitigated by Debug log).
  - Confidence: HIGH — the contract is explicit and the dispatcher will not write a second body.
  - Blind spot: Malformed-upstream-chunk case will silently log-and-continue.
- **Decision**: PENDING

### F3 — NIM 502 body text diverges from custom

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: proxy/proxy.go:98 + proxy/errors.go:32-35
- **Detail**: Custom adapter transport failure produces `{"error":"upstream_unreachable","detail":"..."}` (502) via `freediusErrorHandler`. NIM adapter transport failure propagates the error to the dispatcher, which writes `{"error":"upstream error"}` (502). Plan (plan.md:370, 543) and test-manual.sh P3.4 both expected the `upstream_unreachable` envelope for both providers. test-manual.sh masks the divergence by accepting any "upstream error indicator".
- **Fix**: Move `defer resp.Body.Close()` + a `forwardUpstreamError` call into a NIM-side `freediusErrorHandler`-equivalent that writes the same envelope, or accept the divergence and update the plan.
- **Decision**: PENDING

### F4 — "Non-streaming text response" case: unhandled and undocumented

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: proxy/nim.go:62-67 + proxy/translate/stream.go:18-32
- **Detail**: plan.md:489 listed "non-streaming text response" as the first test case in Phase 3 #5 with the parenthetical "decide in implementation; document the choice". The implementation chose neither: nim.go:62-67 unconditionally sets `Content-Type: text/event-stream` and calls `TranslateStream` on every 2xx. If NIM returns `application/json`, the translator's `bytes.HasPrefix(trimmed, []byte("data: "))` check skips every line and the client sees a 200 + empty SSE body. No test, no doc.
- **Fix**: Add a 5-line check in NIMAdapter.Handle before setting the SSE headers: if `!strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream")`, copy the body verbatim. Update test-manual.sh with a non-SSE mock case.
- **Decision**: PENDING

### F5 — govulncheck: 9 stdlib vulnerabilities deferred

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Success Criteria
- **Location**: go.mod (go 1.26.1)
- **Detail**: Phase 3 success criterion (plan.md:532) was `govulncheck ./... — no new vulnerabilities`. govulncheck reports 9 stdlib CVEs in go1.26.1 all fixed in go1.26.2. The commit message defers this to "Go version upgrade, out of scope for S-01". The criterion as written is unmet.
- **Fix A ⭐ Recommended**: Bump go.mod to 1.26.2 and re-run tests; re-run govulncheck.
  - Strength: Restores the criterion as written; one-line change.
  - Tradeoff: Pulls in a Go patch release; not strictly S-01 scope.
  - Confidence: MED — depends on whether 1.26.2 is in the toolchain.
  - Blind spot: Other 1.26.x deps may need bumping.
- **Fix B**: Accept the deferral; amend plan.md:532 to read "no NEW first-party vulnerabilities" and add a follow-up.
  - Strength: Honest about the out-of-scope deferral.
  - Tradeoff: Criteria drift in the plan; needs an addendum.
  - Confidence: HIGH.
  - Blind spot: None significant.
- **Decision**: PENDING

### F6 — NewRegistry does not validate nil entries

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence + Pattern Consistency
- **Location**: proxy/provider.go:17-19
- **Detail**: plan.md:151 specifies "panic on nil entries (defensive: same pattern as NewDispatcher per F-01 review F7)". NewDispatcher (proxy.go:24-32) does panic on nil cfg/registry/logger. NewRegistry silently stores the map; `Lookup` would return `(nil, true)` for a nil-registered provider, causing the dispatcher's `adapter.Handle(...)` to panic. Only call site is main.go, so the risk is small, but the defensive contract is broken.
- **Fix**: Add a nil-entry loop in NewRegistry.
- **Decision**: PENDING

### F7 — TestCustomClientDisconnect asserts wrong invariant

- **Severity**: 💡 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Success Criteria
- **Location**: proxy/custom_test.go:189-219
- **Detail**: plan.md:342's contract: "client does not see a 502 (the error handler skips writing on Canceled)". The test asserts `upstreamHit == 1` — the upstream WAS reached — but never asserts the absence of a 502 in the response. The load-bearing claim is unverified.
- **Fix**: Add `if rec.Code == http.StatusBadGateway { t.Errorf(...) }` to both TestCustomClientDisconnect variants.
- **Decision**: PENDING

### F8 — tool_use with null input emits "null" as arguments

- **Severity**: 💡 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Safety & Quality
- **Location**: proxy/translate/anthropic_openai.go:170-178
- **Detail**: When `b["input"]` is JSON `null` (Go `nil`), `json.Marshal(nil)` returns `"null"`, which becomes the OpenAI `function.arguments` value. Most OpenAI-compatible servers reject `"null"` as invalid arguments.
- **Fix**: Special-case `input == nil` to emit `args = ""`. Add a TestTranslateRequestToolUseNullInput golden test.
- **Decision**: PENDING

### F9 — forwardUpstreamError does not close resp.Body

- **Severity**: 💡 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Safety & Quality
- **Location**: proxy/errors.go:12-21
- **Detail**: Caller-dependent contract: nim.go:58 defers Body.Close, so today this is safe. A future second caller that omits the defer will leak the upstream connection.
- **Fix**: Move `defer resp.Body.Close()` into forwardUpstreamError.
- **Decision**: PENDING

### F10 — TestCustomClientDisconnect* are near-duplicates

- **Severity**: 💡 OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern Consistency
- **Location**: proxy/custom_test.go:189-219 + 221-254
- **Detail**: The two tests differ only in their error-handling branch and assert the same invariant.
- **Fix**: Delete one; if both are kept, give them distinct assertions.
- **Decision**: PENDING

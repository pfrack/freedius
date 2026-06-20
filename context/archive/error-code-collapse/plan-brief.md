# Error Code Differentiation — Plan Brief

> Full plan: `context/changes/error-code-collapse/plan.md`
> Frame brief: `context/changes/error-code-collapse/frame.md`

## What & Why

> **The actual problem**: Error code differentiation is insufficient — adapter configuration errors (missing API key, bad BaseURL) and unrecognized upstream 5xx responses both collapse to HTTP 529 "overloaded_error", making the client unable to distinguish between transient overload (retryable) and permanent misconfiguration (retry won't help).

Six distinct error conditions all land on 529 today. We introduce a `configError` sentinel so the dispatcher can classify adapter errors at the pre-WriteHeader branch: config errors → 500 (no retry), transport errors → 529 (retry). Two other code sites (`translateUpstreamError` default, `freediusErrorHandler`) get targeted status-code differentiation.

## Starting Point

- The dispatcher at `proxy/proxy.go:231` hard-codes `writeAnthropicError(w, 529, "overloaded_error", ...)` for ALL adapter errors — no differentiation between missing API key, bad BaseURL, and genuine transport failures.
- `translateUpstreamError`'s default case (`errors.go:83-86`) collapses 502/504/505 to 529 instead of passing through the original upstream code.
- `freediusErrorHandler` maps all transport errors to 529 — even permanent DNS/TLS failures that will never succeed on retry.
- The 404 code path is correct and tested; not touched.

## Desired End State

- Claude Code gets 500 with `authentication_error` or `invalid_request_error` for config mistakes — no retry UI, straight failure message.
- Claude Code gets 529 `overloaded_error` with retry headers for genuine transport failures — retry UI works.
- Upstream 5xx codes (502, 504, 505) pass through at their original status with `api_error` + retry headers.
- Permanent transport failures (DNS, TLS) get 502 with no retry — don't waste time on dead hosts.

## Key Decisions Made

| Decision | Choice | Why (1 sentence) | Source |
|---|---|---|---|
| Config error status code | 500 Internal Server Error | Unambiguous signal — server-side config problem, not upstream overload. | Plan |
| Config error differentiation | Varied Anthropic error.type per cause | `authentication_error` for missing keys, `invalid_request_error` for bad URLs — operator sees specific cause. | Plan |
| Sentinel error mechanism | `configError` struct + `errors.As` in dispatcher | Adapters own classification; dispatcher maps type → status. No string-matching. | Plan |
| Adapter error envelope format | `writeAnthropicError` (not `writeErrorJSON`) | Consistent Anthropic envelope across all adapter/upstream paths. Claude Code already parses this shape. | Plan |
| Both adapters treated the same | Yes — 500 for config errors in both | A missing API key is a proxy config problem regardless of upstream protocol. | Plan |
| `translateUpstreamError` default | Pass through original upstream status | Preserves what the upstream actually said — 502→502, 504→504. | Plan |
| `freediusErrorHandler` differentiation | DNS/TLS → 502, rest → 529 | Permanent failures don't benefit from retry; transient ones do. | Plan |
| Log level for config vs transport | Warn for config, Error for transport | Config errors are operator-setup issues, not runtime emergencies — reduces alert noise. | Plan |

## Scope

**In scope:**
- `proxy/proxy.go:219-232` — dispatcher adapter-error classification (config vs transport)
- `proxy/errors.go:83-86` — `translateUpstreamError` default case → pass through
- `proxy/errors.go:116-148` — `freediusErrorHandler` DNS/TLS detection
- `proxy/openai_compat.go` — wrap pre-flight errors as `configError`
- `proxy/anthropic_compat.go` — wrap pre-flight errors as `configError`
- `proxy/mix.go` — wrap URL parse error as `configError`
- Tests for all new status code paths + updated existing tests

**Out of scope:**
- Error envelope format changes (both `writeErrorJSON` and `writeAnthropicError` stay as-is)
- Transport-error subtype differentiation beyond permanent/transient
- Adding `detail` field to Anthropic error envelope
- 404 model-not-found path (already correct)

## Architecture / Approach

A lightweight `configError` sentinel wraps adapter pre-flight errors. The dispatcher detects it at `proxy.go:231` → 500 (config) vs 529 (transport). Two other sites self-classify: `translateUpstreamError` uses upstream status codes directly; `freediusErrorHandler` inspects the Go error chain for DNS/TLS types. All errors use the existing `writeAnthropicError` envelope — no format drift.

```
Adapter pre-flight checks → configError{err, errType}
                                ↓
                         adapter.Handle returns error
                                ↓
            dispatcher: errors.As(err, &configError)?
              YES → 500 + errType + Warn log (no retry)
              NO  → 529 overloaded_error + Error log (retry)

translateUpstreamError: default case → resp.StatusCode (not 529)
freediusErrorHandler:    net.DNSError / TLS → 502 (no retry)
                         everything else   → 529 (retry)
```

## Phases at a Glance

| Phase | What it delivers | Key risk |
|---|---|---|
| 1. Sentinel + adapter wrapping | `configError` type; adapters wrap pre-flight errors | Existing `err.Error()` assertions in tests must pass through `configError.Error()` — verified by Phase 1 success criteria |
| 2. Dispatcher classification | `proxy.go:231` branches on `errors.As` → 500 vs 529 | Interface change — config errors no longer get retry headers; verify Claude Code error display still looks right |
| 3. translateUpstreamError pass-through | Default case preserves upstream status (502→502, 504→504) | Upstream 502 with retry headers may trigger Claude Code retry when it shouldn't — but this is correct for proxy semantics (gateway error is retryable) |
| 4. freediusErrorHandler DNS/TLS | Permanent transport failures → 502, transient → 529 | TLS error type matching in Go is fragile — test with real errors in the test harness |
| 5. Tests | All new code paths tested, existing tests updated | None — pure test additions |

**Prerequisites:** Go toolchain, existing test suite passing (`go test ./...`)
**Estimated effort:** ~1 session across 5 phases

## Open Risks & Assumptions

- **TLS error detection fragility**: Go's standard library does not export consistent error types for all TLS/x509 failures. The `isPermanentTransportError` helper may need to use string-based detection for some sub-cases. This is acceptable for `freediusErrorHandler` (which fires rarely) but worth confirming during implementation.
- **Claude Code 500 display**: Claude Code is known to handle Anthropic-format errors well (parses `{"type":"error","error":{"type":"...","message":"..."}}`). Changing from 529 to 500 for config errors may display differently in the Claude Code UI. Manual verification to confirm the error message is still shown clearly.
- **Upstream 502 with retry headers**: `translateUpstreamError`'s new default case emits `retryAfter: 15` on 502 — this may cause Claude Code to retry on upstream gateway errors. This is desirable (gateway errors are often transient), but if the upstream is permanently broken, the retry loop will exhaust. Consider `retryAfter=0` if this becomes an issue — monitor after deployment.

## Success Criteria (Summary)

- Config errors (missing API key, bad BaseURL) → HTTP 500 with no retry headers
- Transport errors (connection refused, timeout) → HTTP 529 with retry headers (unchanged)
- Upstream 502/504/505 → preserved at original code with retry headers
- DNS/TLS permanent transport failures → HTTP 502 with no retry
- All existing tests pass; new tests cover each differentiated path

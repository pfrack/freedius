# Frame Brief: Error Code Collapse to 529

> Framing step before /10x-plan. This document captures what is *actually*
> at issue, separated from what was initially assumed.

## Reported Observation

Error return codes from the proxy consistently map to HTTP 529, and HTTP 404 is never observed in the response. The user sees this across both upstream error responses AND config/pre-flight adapter errors (missing API key, bad BaseURL).

## Initial Framing (preserved)

- **User's stated cause or approach**: Not explicitly stated. Implicit framing: "error codes are incorrectly collapsed to 529, upstream 404 is being remapped."
- **User's proposed direction**: Not stated. Goal is to understand the problem before planning a fix.
- **Pre-dispatch narrowing**: The 529 is seen on "Upstream error responses"; the 404 expectation is for "Upstream 404 response" — when the upstream returns 404, freedius changes it. Also seen on config errors (missing API keys, bad base URLs). Status codes only (not format).

## Dimension Map

The observation could originate at any of these dimensions:

1. **`translateUpstreamError` switch statement** (`proxy/errors.go:64-87`) — Maps upstream status codes to Anthropic error types. The `default` case catches all unrecognized 5xx (502, 504, 505) and maps them to 529. The `503/529` case also maps to 529. 404 IS correctly preserved (`case >=400 && <500`). If the upstream returns a non-404 code for model-not-found, it becomes 529 here.

2. **Dispatcher adapter error fallback** (`proxy/proxy.go:219-232`) — When `adapter.Handle()` returns an error before writing headers, the dispatcher writes `writeAnthropicError(w, 529, "overloaded_error", "upstream provider not reachable", 15)`. This path catches ALL pre-flight errors: missing BaseURL, missing API key, request translation failure, NIM sanitize hook failure, `client.Do` transport error. Six distinct error conditions → one status code (529).

3. **`freediusErrorHandler` transport errors** (`proxy/errors.go:116-148`) — All non-cancel transport errors (DNS failure, connection refused, TLS error, timeout) emit 529 via `writeAnthropicError`. No differentiation by error type.

4. **ReverseProxy pass-through (Anthropic adapter)** (`proxy/anthropic_compat.go:73-83`) — Preserves upstream HTTP status codes as-is, including 404. No `ModifyResponse` set. `ErrorHandler` fires only on transport failures, not HTTP error responses. ← **Not the problem area**

5. **Dispatcher `writeErrorJSON` 404 path** (`proxy/proxy.go:168-175`) — Returns HTTP 404 with error code `"no_match"` for unknown models. Uses a DIFFERENT error envelope format (`{"error":"no_match","message":"..."}`) than the Anthropic format used everywhere else (`{"type":"error","error":{"type":"...","message":"..."}}`). This 404 exists and works, but the format mismatch is a related design inconsistency.

## Hypothesis Investigation

| Hypothesis | Evidence | Verdict |
| --- | --- | --- |
| `translateUpstreamError` mangles upstream 404 to 529 | `errors.go:80-82` — 404 lands on `resp.StatusCode >= 400 && resp.StatusCode < 500`, status preserved as 404. Test at `errors_test.go:155` confirms (`{"404", 404, "", 404, "invalid_request_error", "", true}`). | **NONE** |
| Adapter `Handle` error return triggers 529 instead of passing through 404 | `openai_compat.go:109-111` — upstream 404 enters `if resp.StatusCode >= 400`, calls `translateUpstreamError` (which returns void), then `return nil`. Dispatcher's 529 fallback at `proxy.go:219` is never entered for an HTTP 404 response. | **NONE** |
| Anthropic ReverseProxy remaps 404 to 529 | `anthropic_compat.go:73-83` — no `ModifyResponse` set, ReverseProxy passes HTTP responses through as-is. `ErrorHandler` only fires on transport failures, not HTTP error responses. | **NONE** |
| Upstream returns non-404 status for model-not-found, which `translateUpstreamError` maps to 529 | `errors.go:68-71` maps 503/529→529. `errors.go:83-86` default maps all other 5xx→529. Many providers return 500, 502, or 503 for model-not-found, not 404. | **STRONG** |
| Pre-flight adapter errors lack code differentiation | `proxy.go:231` hard-codes `writeAnthropicError(w, 529, "overloaded_error", ...)` for ALL adapter errors — missing BaseURL, missing API key, translate failure, transport error. Six distinct error conditions indistinguishable at the status-code level. | **STRONG** |
| `freediusErrorHandler` lacks transport-error differentiation | `errors.go:145` — all non-cancel transport errors emit 529 "overloaded_error". DNS failure, connection refused, and TLS errors are indistinguishable. | **MEDIUM** (by design — per Anthropic retry convention) |

## Narrowing Signals

Decisive observations from Step 4 that narrowed the hypothesis space:

- **User sees 529 from config errors too** (missing API keys, bad base URLs): pre-flight adapter errors → dispatcher 529 fallback confirmed as a contributing factor.
- **User has NOT verified what the upstream actually returns** for model-not-found: the upstream may return 5xx codes (503, 502) rather than 404, which would explain the 529 mapping.
- **Status codes only concern** (not format): the two different error envelope formats (`writeErrorJSON` vs `writeAnthropicError`) are acknowledged but not the primary concern.

## Cross-System Convention

The 529 status code is an **Anthropic API convention** for "overloaded" — used to trigger Claude Code's retry logic (`x-should-retry: true`, `retry-after` headers). The S-04 (`error-hardening`) plan originally intended 502 Bad Gateway for adapter errors via `writeErrorJSON`. The later `count-tokens-passthrough` change deliberately replaced this with 529 `writeAnthropicError` for better Claude Code retry behavior on transient failures.

However, the change over-applied 529: it now covers **permanent configuration errors** (missing API key, bad BaseURL) where retry is guaranteed to fail. The two error envelope formats are a separate design inconsistency but not the subject of this issue.

## Reframed (or Confirmed) Problem Statement

> **The actual problem to plan around is**: Error code differentiation is insufficient — adapter configuration errors (missing API key, bad BaseURL) and unrecognized upstream 5xx responses both collapse to HTTP 529 "overloaded_error", making the client unable to distinguish between transient overload (retryable) and permanent misconfiguration (retry won't help).

The "no 404" observation is a symptom of two mechanisms: (1) the upstream likely returns 5xx codes for model-not-found (not 404), and (2) all adapter errors converge on 529 regardless of cause. The code path for actual upstream 404 → client 404 is correct and tested. The real gap is that **non-404 upstream errors and adapter configuration errors have no differentiated status codes**.

If addressed, the client would be able to distinguish:
- "Provider is overloaded — retry with backoff" (529)
- "Freedius is misconfigured — retry won't help, check config" (500 or similar)
- "Upstream says model not found" (404 or the actual upstream code)
- "Upstream is unreachable" (502 or similar, not 529)

## Confidence

- **HIGH** — strong evidence from code and sub-agent investigation, matches documented design history (S-04 → S-05 transition), decisive narrowing signals from user.

## What Changes for /10x-plan

The plan should address insufficient error code differentiation in `proxy.go:219-232` (adapter error → 529 fallback) and `proxy/errors.go:83-86` (default 5xx → 529). Specifically: differentiate config/permanent errors from transient ones at the status-code level, and consider whether the default case in `translateUpstreamError` should map unknown 5xx codes more precisely.

## References

- Source files:
  - `proxy/errors.go:64-87` — `translateUpstreamError` switch statement
  - `proxy/errors.go:116-148` — `freediusErrorHandler`
  - `proxy/proxy.go:219-232` — dispatcher adapter error → 529
  - `proxy/proxy.go:168-175` — dispatcher 404 `no_match`
  - `proxy/openai_compat.go:65-111` — OpenAI adapter Handle
  - `proxy/anthropic_compat.go:73-83` — Anthropic adapter ReverseProxy
  - `proxy/errors_test.go:155` — 404 test case
- Related research:
  - `context/archive/error-hardening/` — S-04 error unification (intended 502 for adapter errors)
  - `context/archive/count-tokens-passthrough/plan.md` — S-05 changed to 529 for Anthropic retry
  - `context/foundation/lessons.md:33-43` — Adapter Return Contract
- Investigation tasks: ses_1246a527dffeoTgxG0JXKomyP5, ses_1246a4981ffed00uCFUzvoexbI, ses_1246a3ac0ffe4idy3ZU93M06tw, ses_12466bc33ffe3CXoc68RRjWRGN, ses_12466b3c7ffeNtCSgDqfGMTp2t

# count_tokens Passthrough + Anthropic Error Propagation — Plan Brief

> Full plan: `context/changes/count-tokens-passthrough/plan.md`
> Research: `context/changes/count-tokens-passthrough/research.md`

## What & Why

Two proxy correctness fixes so Claude Code works reliably through freedius: (1) Route `/v1/messages/count_tokens` correctly (done ✓), and (2) translate upstream errors from non-Anthropic providers into Anthropic-format responses so Claude Code's retry logic triggers properly instead of seeing garbled JSON or hard failures.

## Starting Point

Phase 1 (count_tokens routing) is complete. For error propagation: `forwardUpstreamError` copies upstream error responses verbatim (status + headers + body). When the upstream is NIM/OpenAI, the body shape is OpenAI format — Claude Code doesn't parse it for retry decisions. Transport errors (timeouts, DNS failures) produce a freedius-format 502 with no `x-should-retry` header, so Claude Code doesn't retry at all.

## Desired End State

Every error from every adapter produces an Anthropic-format JSON response with `retry-after` and `x-should-retry: true` headers (for retryable conditions). Claude Code shows "Retrying in X seconds…" when NIM is overloaded or unreachable, instead of a hard failure.

## Key Decisions Made

| Decision | Choice | Why (1 sentence) | Source |
|---|---|---|---|
| Translate anthropic adapter transport errors too | Yes — all paths | Consistent retry behavior regardless of which adapter/failure mode. | Plan |
| Default retry-after when upstream doesn't provide | 15 seconds | Moderate — gives upstream breathing room without killing CC's retry budget. | Plan |
| Which statuses get x-should-retry | 429, 503, 529, timeouts (not auth failures) | Matches real Anthropic API behavior. | Research |
| freedius's own errors (404, 405) | Keep freedius-format | Clear separation — proxy errors shouldn't be retried. | Plan |

## Scope

**In scope:** `writeAnthropicError` + `translateUpstreamError` functions; wiring into OpenAI-compat error path, dispatcher 502 path, and `freediusErrorHandler`; updating existing NIM tests to expect new format; integration tests.

**Out of scope:** Mid-stream SSE error events (response already started); freedius-level retry/failover logic; local rate limiting.

## Architecture / Approach

A single `writeAnthropicError(w, status, errType, message, retryAfter)` function emits the Anthropic envelope + headers. `translateUpstreamError(w, resp)` maps upstream status codes to the right Anthropic error type. Three call sites are updated: OpenAI-compat adapter's `StatusCode >= 400` branch, dispatcher's adapter-error branch, and `freediusErrorHandler`.

## Phases at a Glance

| Phase | What it delivers | Key risk |
|---|---|---|
| 1. count_tokens routing ✓ | Correct 501/passthrough for all provider types | Done |
| 2. Anthropic error writer | `writeAnthropicError` + `translateUpstreamError` with tests | Low — pure functions, no wiring yet |
| 3. Wire into error paths | All errors reach Claude Code as retryable Anthropic-format | Existing NIM tests must be updated to new format |

**Prerequisites:** Phase 1 complete (✓).
**Estimated effort:** ~1-2 sessions across 2 phases.

## Open Risks & Assumptions

- Existing NIM 429/401 tests assert verbatim passthrough — they need updating to assert Anthropic format (behavior intentionally changes).
- Mid-stream failures (timeout after 200 OK + SSE headers) are NOT fixable without SSE error events — left out of scope. Claude Code handles truncated SSE as a hard fail.
- `retry-after: 15` default may be too conservative or too aggressive depending on provider — tunable later via config if needed.

## Success Criteria (Summary)

- Claude Code shows "Retrying in X seconds…" when NIM returns 429 or times out (instead of hard failure or garbled error).
- Anthropic adapter transport errors (unreachable upstream) also trigger Claude Code retry.
- Normal streaming responses are unaffected across all providers.
- `go test ./...`, `go vet ./...`, `go build`, `gofumpt` all clean.

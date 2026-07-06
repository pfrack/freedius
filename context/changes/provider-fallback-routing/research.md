---
date: 2026-07-06T20:22:37+02:00
researcher: Claude
git_commit: 3d15b912c6cadbcb0e99b39a4d6e10090ec789e3
branch: web-ui-redesing
repository: freedius
topic: "Fallback routing for model mappings when a provider/model stops working"
tags: [research, codebase, dispatch, config, routing, error-handling, fallback]
status: complete
last_updated: 2026-07-06
last_updated_by: Claude
---

# Research: Fallback routing for model mappings when a provider/model stops working

**Date**: 2026-07-06T20:22:37+02:00
**Researcher**: Claude
**Git Commit**: 3d15b912c6cadbcb0e99b39a4d6e10090ec789e3
**Branch**: web-ui-redesing
**Repository**: freedius

## Research Question

If a configured model mapping "stops working" — the upstream provider errors,
times out, or is misconfigured — should freedius automatically retry the
request against a different, explicitly-configured fallback provider/model
instead of returning an error to the client? Scoped per user answers: cover
**both** live upstream failures and pre-flight config problems, target design
is an **explicit per-mapping ordered fallback list**, and this pass is
research-only (no plan/shape commitment yet).

## Summary

**No such mechanism exists today, and none has ever been designed, proposed,
or rejected in this project's history.** Routing is currently, deliberately,
and by tested contract a single-target lookup: exact mapping name → family
pattern → 404 (`proxy/proxy.go:94-111`, confirmed as an intentional invariant
by `context/archive/2026-07-02-testing-proxy-integration/research.md:99`:
*"No silent fallback exists... Missing provider returns 500, not a
fallback"*). "Fallback" already means something else in this codebase (a
*mapping-name* fallback via `extractFamily`, not a provider-health fallback),
and "provider health" already means something else too (a passive TUI/web-ui
monitoring panel, never wired into dispatch).

Building real fallback-on-failure is greenfield work, but the codebase has
good bones for it:

- The `config.Mapping`/`config.Provider` structs are single-target today and
  would need a new ordered-list field — no schema groundwork exists yet.
- The dispatcher already has the one clean insertion point this needs: the
  `!ww.wroteHeader` branch after `adapter.Handle(...)` in
  `proxy/proxy.go:269-296`, which is the sole place that both knows a call
  failed *and* can prove no bytes reached the client yet.
- Error classification (retryable vs. permanent) partially exists already
  (`translateUpstreamError`, `isPermanentTransportError` in
  `proxy/errors.go`) but is inconsistent between adapters and only drives
  client-facing `retry-after` headers today — nothing consumes it server-side.
- A hard architectural constraint (the "Adapter Return Contract",
  `context/foundation/lessons.md:33-43`) means fallback is only safe
  *before* any response bytes are written — which today excludes upstream
  4xx/5xx error bodies (they're written and swallowed inside the adapter,
  never surfaced as a retryable `error` to the dispatcher).

## Detailed Findings

### Current routing: single-target, two-tier lookup

`Dispatcher.resolveMapping` (`proxy/proxy.go:94-111`) is the entire routing
decision:

1. Look up `d.Cfg.Mappings[model]` by exact request model name.
2. On miss, call `extractFamily(model)` (`proxy/families.go:10-25`) — a fixed
   list of regexes (`opus`, `sonnet`, `haiku`, `auto`, `default`) checked in
   order, where `default` matches the empty pattern and therefore matches
   *any* string, acting as a catch-all if a `Mappings["default"]` entry
   exists. Retry the mapping lookup with the family name.
3. Still no match → `(Mapping{}, nil, false)`, and `ServeHTTP` returns
   `404 no_match` (`proxy.go:193-210`).
4. On a mapping hit, look up `d.Cfg.Providers[mapping.ProviderName]`; if the
   provider itself isn't registered, return `(mapping, nil, true)` and
   `ServeHTTP` returns `500 provider_not_registered` (`proxy.go:211-230`).

This is **routing** (which single target to use *before* any request is
sent), not **failover** (what to do if the chosen target fails *after* being
tried). Nothing downstream of a successful `resolveMapping` ever tries a
second provider.

`config.Mapping` (`config/config.go:58-61`) is `{ProviderName string;
ModelString string}` — exactly one target, no slice/array field. Every prior
design of this schema (S-01's flat `models:` map, S-02/S-03's family-aware
`mappings:` block, `providers-section-refactor`'s `Provider`+`Mapping` split)
kept it single-target; an ordered fallback list would be new schema surface,
not an extension of anything half-built.

### The dispatch/error-handling flow, and the one place fallback could hook in

`Dispatcher.ServeHTTP` (`proxy/proxy.go:113-311`) wraps the response writer in
a `wroteHeaderResponseWriter` (`proxy.go:395-415`) before calling
`adapter.Handle(...)` (`proxy.go:269`). That wrapper is what makes the
following branch possible:

```go
if err := adapter.Handle(ww, r, *provider, mapping, body); err != nil {
    if !ww.wroteHeader {
        // Pre-WriteHeader error — safe to forward to the client.
        ...
    } else {
        // Post-WriteHeader error — adapter already sent a response.
        // Log and discard to avoid "superfluous WriteHeader" panics.
        ...
    }
}
```
(`proxy.go:269-310`)

This `!ww.wroteHeader` branch is the **only** point in the current codebase
with both the knowledge that a call failed and proof that no bytes reached
the client — i.e. the only safe place to retry against a different
provider/mapping. A fallback loop would need to: on `!ww.wroteHeader`, consult
a (currently nonexistent) ordered fallback list instead of writing 500/529
immediately, build a **new** `wroteHeaderResponseWriter` around the
*original* `w` (the exhausted `ww` can't be reused), and re-invoke
`adapter.Handle` with the next candidate `config.Provider`/`config.Mapping`.
Adapters carry no per-call state (`provider config.Provider` and `mapping
config.Mapping` are passed fresh by value on every call — see
`proxy/provider.go:12-20`), so nothing needs to be reset between fallback
attempts.

### Why retries/fallback are constrained to *before* any bytes are written

The **Adapter Return Contract** (`context/foundation/lessons.md:33-43`, cited
across many archived plans) is an established, load-bearing rule: *"Once an
adapter has written any part of the response (including forwarding an
upstream error with its body), it must return `nil`. Any subsequent errors
should be logged (or ignored) but not returned to the dispatcher."* This
means fallback is impossible once headers are written — retrying after that
point would double-write headers (`"superfluous WriteHeader"` panics) or
interleave a second SSE stream into a response the client is already
consuming.

Concretely, per adapter:

- **`OpenAICompatibleAdapter.Handle`** (`proxy/openai_compat.go:60-176`): all
  config problems (missing base URL/API key, translate failure, pre-send-hook
  failure, bad request construction) are `*configError`, returned
  **pre-write** (`67-133`). A `client.Do` transport failure (timeout,
  connection refused, DNS failure, TLS error — **all lumped into one plain
  `error`, no classification**) is also **pre-write** (`140-143`). But once
  the upstream responds with `>= 400`, `translateUpstreamError(w, resp)`
  writes the full error body directly and the adapter **returns `nil`**
  (`146-149`) — this status-code-driven error is never surfaced to the
  dispatcher as a retryable condition today. Once a `200` + streaming headers
  are written (`151-154`), the adapter always returns `nil` regardless of
  mid-stream failures (`156-175`).
- **`AnthropicCompatibleAdapter.Handle`** (`proxy/anthropic_compat.go:39-98`):
  config problems are `*configError`, pre-write (`46-77`). Everything else
  delegates to `httputil.ReverseProxy` (`86-96`), whose `ErrorHandler`
  (`freediusErrorHandler`, `proxy/errors.go:202-239`) *does* use
  `isPermanentTransportError` (`errors.go:171-194`) to pick 502 (permanent:
  DNS/TLS/cert errors) vs. 529 (transient: everything else) — but by the time
  `Handle` returns, `ReverseProxy` has always already written a response
  (success or synthetic error), so `wroteHeader` is always `true` here. This
  adapter's transport-error classification is *more* granular than the OpenAI
  path's, but it's unreachable for fallback purposes because it always writes
  before returning.
- **`MixAdapter.Handle`** (`proxy/mix.go:49-79`) is a pure router (picks
  anthropic/openai sub-adapter by `provider.Protocol` or URL-path sniffing)
  and passes through whichever of the above behaviors applies; its own only
  error is a pre-write `*configError` on bad `base_url` parsing (`66-72`).

Net effect: **today, a 429/503/500 from the upstream is never retryable at
the dispatcher level** — it's already been written to the client by the time
`Handle` returns. Only upstream problems caught *before* any request reaches
the provider (missing config) or *during* the transport call itself
(connection failure, before any response arrives) are currently pre-write and
thus fallback-eligible. Supporting fallback on "the upstream came back with a
529/429" (very plausibly part of what the user means by "a model starts to
not work") requires changing the adapter contract itself: `resp.StatusCode
>= 400` would need to produce a typed, pre-header return value instead of
calling `translateUpstreamError` and writing immediately, so the dispatcher
can inspect and decide fallback-vs-terminal before anything is written. This
is a bigger change than "add a schema field" — it touches the return
contract every adapter obeys.

### Error classification that already exists (and where it falls short)

`proxy/errors.go` already has real classification machinery a fallback
feature would want to reuse rather than reinvent:

- `translateUpstreamError` (`errors.go:67-113`) maps upstream status codes to
  Anthropic error types and a `retryAfter` value: 429 → `rate_limit_error`;
  503/529 → `overloaded_error`; 500 / other 5xx → `api_error`; 401/403 →
  `authentication_error` (no retry signal); other 4xx →
  `invalid_request_error` (no retry signal). This is effectively a
  retryable/permanent split already, but it's baked into a function whose
  job is "write the client-facing error," not "tell the caller whether to
  retry."
- `isPermanentTransportError` (`errors.go:171-194`) classifies Go transport
  errors (DNS/TLS/cert failures = permanent; connection refused/reset/timeout
  = transient) — but it's **only wired into the Anthropic ReverseProxy path**
  (`anthropic_compat.go:94`). `OpenAICompatibleAdapter`'s `client.Do` failure
  path (`openai_compat.go:140-143`) doesn't use it at all, so a DNS failure
  and a connection-refused both collapse into the same generic 529 for
  OpenAI-behavior providers. Any fallback design needs this classification
  applied uniformly across adapters (or needs to accept "exhaust the fallback
  list" as the only terminal condition, without trying to be clever about
  which failures deserve a fallback attempt).
- The **adapter registry is keyed by `Behavior` class** (`"openai"`,
  `"anthropic"`, `"mix"` — `proxy/adapters_gen.go:110-140`,
  `proxy.go:250`), not by provider name — the same adapter instance handles
  every provider of that behavior class, and `config.Provider`/`config.Mapping`
  are passed fresh by value per call. This is good news for fallback: no
  adapter-side state needs resetting between a primary attempt and a fallback
  attempt.
- No provider-level health/circuit-breaker state exists anywhere (confirmed
  by a repo-wide grep for `retry|circuit|breaker|health|unhealthy|backoff` —
  the only hits are the classification helpers above, the unrelated `/health`
  process-liveness endpoint at `cmd/freedius/main.go:441-464` /
  `proxy/web/handlers.go:25-33`, and TUI/web-ui "provider health" *display*
  panels — see Historical Context). A stateless "try fallback list in order,
  per-request" design needs no new shared state; a stateful "mark provider
  unhealthy for N seconds" (circuit breaker) design would need new
  concurrency-safe state alongside `config.Config`'s existing
  `sync.RWMutex`-guarded maps, with nothing today to extend.

## Code References

- `proxy/proxy.go:94-111` — `resolveMapping`, the entire current routing
  decision (exact → family → miss).
- `proxy/proxy.go:113-311` — `ServeHTTP`, full request lifecycle.
- `proxy/proxy.go:269-296` — the `!ww.wroteHeader` branch: the one safe
  insertion point for a fallback retry.
- `proxy/proxy.go:395-415` — `wroteHeaderResponseWriter`, the mechanism that
  proves whether a response has been committed.
- `proxy/families.go:10-25` — `extractFamily` and the fixed family-pattern
  list; the *only* fallback concept that exists today (mapping-name, not
  provider-health).
- `proxy/errors.go:22-28` — `configError`, the pre-flight typed error wrapper.
- `proxy/errors.go:67-113` — `translateUpstreamError`, upstream status-code →
  client error-type/retry-after classification.
- `proxy/errors.go:171-194` — `isPermanentTransportError`, transport-error
  classification (Anthropic path only).
- `proxy/errors.go:202-239` — `freediusErrorHandler`, `ReverseProxy`'s error
  handler (Anthropic path only).
- `proxy/openai_compat.go:60-176` — `OpenAICompatibleAdapter.Handle`, all
  pre-write vs. post-write error points.
- `proxy/anthropic_compat.go:39-98` — `AnthropicCompatibleAdapter.Handle`,
  always writes before returning (ReverseProxy-delegated).
- `proxy/mix.go:49-79` — `MixAdapter.Handle`, protocol router.
- `proxy/provider.go:12-20` — the `Provider`/adapter `Handle` interface
  signature (stateless per-call).
- `config/config.go:28-61` — `Config`, `Provider`, `Mapping` struct
  definitions — single-target schema.
- `config/config.go:182-285` — `validate`, `validateProvider`,
  `validateMapping` — no fallback/priority-list validation exists because no
  such field exists.
- `config/defaults.go:16-44` — `applyDefaults`, how provider defaults merge
  in at load time.
- `context/foundation/lessons.md:33-43` — the Adapter Return Contract lesson
  that constrains where retries are safe.

## Architecture Insights

- **"Fallback" and "health" are already-loaded terms in this codebase, and
  they don't mean what this feature request means.** `extractFamily`'s
  family-pattern lookup is called "fallback" in code comments and prior plans
  but is a pre-dispatch mapping-name resolution, not a post-failure retry.
  "Provider health" in the TUI/web-ui plans is a passive monitoring display,
  never wired to routing decisions. Any design doc or plan for this feature
  should pick new, unambiguous terminology (e.g. "failover chain" or
  "fallback targets") to avoid colliding with existing usage.
- **Deterministic single-target routing was validated as a feature, not a
  gap**, per `2026-07-02-testing-proxy-integration/research.md:99`: *"No
  silent fallback exists... Missing provider returns 500, not a fallback."*
  A team discussion (or at least an explicit note in whatever plan follows
  this research) should acknowledge that this feature is a **deliberate
  reversal** of a previously-tested invariant, not a bug fix or gap-fill.
- **The real design fork isn't schema — it's the adapter contract.** Adding
  an ordered list to `config.Mapping` is comparatively mechanical. The harder
  question is which failures are fallback-eligible: pre-flight config errors
  and pre-response transport errors are fallback-eligible *today* with no
  contract changes; upstream 4xx/5xx responses are *not* fallback-eligible
  without first changing `OpenAICompatibleAdapter`/`AnthropicCompatibleAdapter`
  to stop writing the error response themselves and instead return it as data
  for the dispatcher to act on. That's the crux of any future plan/shape pass.
- **Streaming responses cannot be retried once `200` is written.** A
  worthwhile scope question for later: does "fallback" only apply to the
  pre-response window (config + connection failures + non-2xx status before
  any bytes are streamed), or does the user also want fallback for
  mid-stream failures — which today is architecturally impossible without a
  buffering/replay layer this codebase doesn't have.

## Historical Context (from prior changes)

- `context/archive/error-code-collapse/frame.md` — origin of the
  500-vs-529 error-code split; established that "retry" in this codebase's
  history means *Claude Code retrying the same freedius endpoint*
  (`x-should-retry`/`retry-after` contract), never freedius switching
  providers.
- `context/archive/zen-go-adapters/research.md` (follow-up #4, ~lines
  1132-1325) — origin of `extractFamily`/the `mappings:` block; single
  provider+model per family, by design, with zero mention of ordered/list
  fallback targets.
- `context/archive/provider-and-mapping/research.md` — S-02 slice summary;
  same two-tier lookup, same absence of runtime fallback/health concepts.
- `context/archive/providers-section-refactor/plan.md:24,46` — documents that
  a transitional 3-tier lookup (`Models` → `Mappings` → family-fallback)
  existed briefly during S-01→S-02 and was collapsed to the current 2-tier
  (`Mappings` → family-fallback) when the standalone `Models` map was
  deleted. Purely a config-schema simplification, unrelated to provider
  failover.
- `context/foundation/roadmap.md:83` — the "custom passthrough serves as a
  fallback validation path" line is an **S-01 delivery-risk/scope statement**
  ("this milestone still ships if only one provider path works"), not a
  runtime feature, and no later slice ever revisited it as one.
- `context/archive/tui-dashboard/*`, `context/archive/2026-07-02-web-ui/plan.md:103-105`,
  `context/archive/tui-error-detail-provider-defaults/plan.md:61` — "provider
  health" has only ever been scoped as a passive monitoring panel
  (up/down, avg response time, error rate) for a human to look at, explicitly
  *not* a live surface driving routing behavior.
- `context/archive/2026-07-02-testing-proxy-integration/research.md:31,36,99`
  — explicitly validates the current no-fallback behavior as correct and
  tested, the strongest piece of historical evidence that this is a
  considered invariant, not an oversight.

## Related Research

- `context/foundation/test-plan.md` §2 Risk #3 ("Config-to-provider routing
  sends request to wrong provider or model") is the closest existing risk
  entry to this topic, but it's about *misrouting*, not *failover* — worth
  distinguishing explicitly if this becomes a tracked risk.
- No other `research.md` in `context/changes/**` overlaps this topic
  (`context/changes/` currently holds only this new folder).

## Open Questions

1. **Scope of "stops working" for upstream HTTP responses**: should fallback
   trigger on 429/503/529 (capacity/overload signals) only, or also on 500 /
   other 5xx, or also on some 4xx (e.g. a model-not-found from the upstream)?
   This directly determines how much of the adapter contract needs to change
   (see Architecture Insights).
2. **Streaming scope**: is mid-stream failure (after `200`+SSE headers are
   already sent) in scope at all, given it's not retryable without a
   buffering/replay layer that doesn't exist? If yes, that's a materially
   larger feature than pre-response fallback.
3. **Fallback-list validation**: should a fallback target's provider be
   required to exist and validate the same way `validateMapping` requires
   today (`config/config.go:261-269`), and should there be a limit on chain
   length / cycle detection (a fallback list must not eventually point back
   to itself)?
4. **Retry-after semantics when the *primary* returns 429 but a fallback
   succeeds**: should the client still see `x-should-retry`/`retry-after`
   headers from the primary's failure, or should a successful fallback look
   like a transparent 200 with no trace of the primary's failure (beyond
   logs/events)?
5. **Should `X-Freedius-Matched-Provider`/`X-Freedius-Matched-Model`
   (`proxy.go:243-244`) reflect the fallback target that actually served the
   request, or the originally-requested mapping?** Affects observability/TUI
   dashboards that read these headers today.

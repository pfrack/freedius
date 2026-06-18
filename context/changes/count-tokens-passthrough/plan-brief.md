# /v1/messages/count_tokens Passthrough — Plan Brief

> Full plan: `context/changes/count-tokens-passthrough/plan.md`
> Research: `context/changes/count-tokens-passthrough/research.md`

## What & Why

freedius's dispatcher currently ignores `r.URL.Path`, so Claude Code's `/v1/messages/count_tokens` probe flows through `httputil.ReverseProxy` to Anthropic-protocol upstreams (works by accident) and silently breaks on OpenAI-protocol upstreams (wrong endpoint, wrong response shape, no translation). This change adds path-aware routing so Anthropic-protocol providers keep working and OpenAI-protocol providers correctly return `501 Not Implemented` instead of corrupting the request.

## Starting Point

`Dispatcher.ServeHTTP` (`proxy/proxy.go:70-244`) validates method + content type, reads body, parses the `"model"` field, resolves it to a `config.Model`, looks up the provider in the `Registry`, and forwards — never inspecting the path. `AnthropicCompatibleAdapter` (`proxy/anthropic_compat.go:39-84`) uses `httputil.ReverseProxy` which preserves the original path, so `count_tokens` reaches upstream Anthropic correctly. `OpenAICompatibleAdapter` (`proxy/openai_compat.go:90-127`) builds a hardcoded request to `m.BaseURL` and runs `translate.Stream`, which expects OpenAI SSE chunks and returns garbage on count_tokens. `MixAdapter` (`proxy/mix.go:44-69`) already routes to anthropic/openai sub-adapters based on `m.Protocol` or URL sniff (`strings.HasSuffix(parsedURL.Path, "/v1/messages")`).

## Desired End State

`POST /v1/messages/count_tokens` against an Anthropic-protocol provider passes through to upstream and returns Anthropic's `{"input_tokens":N,...}` response. `POST /v1/messages/count_tokens` against an OpenAI-protocol provider returns `501 Not Implemented` with the freedius error envelope `{"error":"not_supported","message":"...provider \"<name>\"..."}` — no upstream call. Regular `/v1/messages` requests are unaffected.

## Key Decisions Made

| Decision                                | Choice                                                                                                       | Why (1 sentence)                                                                       | Source        |
| --------------------------------------- | ------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------- | ------------- |
| Where capability knowledge lives        | Helper function in `proxy/capabilities.go` (NOT on `Provider` interface)                                     | Smallest surface for one endpoint; matches research's proposed sketch; no interface change. | Plan          |
| How mix's sub-routing is determined     | Dispatcher peeks at `m.Protocol` + URL sniff (duplicating mix's logic in ~5 lines)                           | No behavior change to MixAdapter; synchronous check before adapter dispatch; unit test catches drift. | Plan          |
| Error response shape                    | `writeErrorJSON` (freedius envelope: `error`, `message`, `request_id`)                                       | Consistent with every other dispatcher error (no_match, missing_model, etc.).         | Plan          |
| Test coverage                           | Focused matrix (~6 cases) + `supportsCountTokens` unit test                                                  | Covers the bug + the routing matrix + the regression in one file; mirrors existing `TestServeHTTP` table pattern. | Plan          |
| Generalization                          | Endpoint-specific; repeat pattern for future Anthropic-protocol endpoints                                    | Smallest possible change; no speculation about future endpoints.                      | Plan          |
| OpenAI-protocol support for count_tokens | Deferred to follow-up plan (Phase 2); implementation choice (char-based heuristic vs `tiktoken-go`) TBD     | Your DeepSeek-via-OpenCode-Go scenario needs it, but it's a feature addition not a correctness fix. | Plan          |

## Scope

**In scope:**
- `proxy/capabilities.go` (new) with `isCountTokensPath` + `supportsCountTokens` helpers
- `proxy/proxy.go` — insert capability check between model resolution and adapter dispatch
- `proxy/capabilities_test.go` (new) — unit tests for both helpers
- `proxy/proxy_test.go` — 6-case table-driven integration test for the routing matrix

**Out of scope:**
- Local token counting for OpenAI-protocol upstreams (deferred to follow-up)
- HEAD/OPTIONS probe handling for count_tokens (Claude Code doesn't probe this endpoint)
- Translation layer changes
- New config flags
- Provider-interface capability methods
- Telemetry beyond what `AccessLogMiddleware` already emits

## Architecture / Approach

```
Claude Code ─POST /v1/messages/count_tokens─▶ Dispatcher.ServeHTTP
                                                    │
                                                    ├─ method/content-type/body checks (existing)
                                                    ├─ model resolution (existing)
                                                    ▼
                                  isCountTokensPath(r.URL.Path)?
                                                    │
                                              ┌─────┴─────┐
                                              │yes        │no
                                              ▼           ▼
                                  supportsCountTokens(m)?  fall through to adapter dispatch
                                              │                  (existing /v1/messages path)
                                        ┌─────┴─────┐
                                        │yes        │no
                                        ▼           ▼
                                  adapter.Handle  writeErrorJSON(501, "not_supported")
                                  (pass-through)  return
```

The capability check is ~5 lines in the dispatcher plus the two helpers in `proxy/capabilities.go`. `supportsCountTokens` encodes four rules: `provider == "anthropic"` → true; `provider == "mix"` with `protocol == "anthropic"` → true; `provider == "mix"` with no protocol and `BaseURL` path ending in `/v1/messages` → true (URL sniff mirrors `MixAdapter.Handle` line 64); everything else → false.

## Phases at a Glance

| Phase | What it delivers                                                       | Key risk                                                                  |
| ----- | ---------------------------------------------------------------------- | ------------------------------------------------------------------------- |
| 1     | Path-aware count_tokens routing — pass-through for Anthropic-protocol, 501 for OpenAI-protocol, with focused test matrix. | Mix routing rule duplication drift between `MixAdapter.Handle` and `supportsCountTokens` (mitigated by unit test + cross-reference comment). |

**Prerequisites:** None — S-01 (first-call-routed) and S-02 (provider-and-mapping) are done; this change modifies the dispatcher that they already produced.
**Estimated effort:** ~1 session, single phase, ~70 LOC + ~150 LOC tests.

## Open Risks & Assumptions

- **Mix routing rule duplication**: `supportsCountTokens` duplicates `MixAdapter.Handle`'s protocol-and-URL-sniff rule (`proxy/mix.go:50-69`). If mix's rule changes (e.g. a third protocol), both must update. Mitigated by a doc comment pointing to `MixAdapter.Handle` and `TestSupportsCountTokens` covering each branch.
- **Phase 2 (local token counting) is filed separately** — your DeepSeek-via-OpenCode-Go scenario still gets `501 not_supported` for count_tokens until that lands. Not a regression (today it silently corrupts the request), but not a fix either.
- **Exact path match (`r.URL.Path == "/v1/messages/count_tokens"`) assumes Anthropic's API surface won't grow a path-prefixed variant**. If you need prefix support later, this becomes a 2-line change in `isCountTokensPath`.

## Success Criteria (Summary)

- `POST /v1/messages/count_tokens` to an Anthropic-protocol provider returns the upstream's token count response (verified by integration test + manual curl).
- `POST /v1/messages/count_tokens` to an OpenAI-protocol provider returns `501 Not Implemented` with `{"error":"not_supported",...}` and the provider name in the message (verified by integration test + manual curl).
- `POST /v1/messages` continues to work identically for every provider (regression test covers one case; existing test suite covers the rest).
- `go test ./...`, `go vet ./...`, `go build`, `gofumpt` all clean.
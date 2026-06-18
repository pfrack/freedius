# Local Token Counting for OpenAI-Protocol Upstreams — Plan Brief

> Full plan: `context/changes/openai-count-tokens/plan.md`
> Research: `context/changes/openai-count-tokens/research.md`

## What & Why

Claude Code sends `POST /v1/messages/count_tokens` as a pre-flight probe to estimate input token usage. Today freedius rejects this with 501 for any provider that doesn't natively implement the endpoint (NIM, OpenCode Go, custom OpenAI-compat). The fix is a local BPE-based counter in freedius that emits a 200 response matching Anthropic's native shape, so Claude Code's count probe works for every provider. S-08 in the roadmap.

## Starting Point

The previous change (`context/archive/count-tokens-passthrough/`, just archived) added path-aware routing: the dispatcher detects `count_tokens` and gates the request through a `supportsCountTokens(m)` capability check. Anthropic-protocol providers get passthrough via `httputil.ReverseProxy`; everyone else gets 501. The body is fully buffered at `proxy/proxy.go:97` (max 10 MiB), the capability rule is at `proxy/capabilities.go:33-54`, and the wire-format structs at `proxy/translate/types.go:52-75` already model every tokenizable region. What's missing is a counter and the dispatcher branch to call it.

## Desired End State

`POST /v1/messages/count_tokens` succeeds for every configured provider. The 200 response body is `{"input_tokens": N, "context_management": {"original_input_tokens": N}}` — matching Anthropic's native shape byte-for-byte. For OpenAI-protocol providers the count is computed locally in freedius via a BPE tokenizer; no upstream round-trip. The Anthropic-protocol passthrough path is unchanged and still returns the upstream's exact count.

## Key Decisions Made

| Decision | Choice | Why (1 sentence) | Source |
| --- | --- | --- | --- |
| Counter library | `github.com/pkoukk/tiktoken-go` v0.1.8 | MIT-licensed Go port of OpenAI's reference BPE tokenizer, actively maintained, ~5 μs/KB encoding | Research |
| BPE bundling | `github.com/pkoukk/tiktoken-go-loader` v0.0.2 (offline) | Embeds BPE via `go:embed`; no network or disk at runtime; satisfies NFR-Privacy | Research |
| Default encoding(s) | Both `cl100k_base` AND `o200k_base` | Better accuracy for gpt-4o/4.1/4.5/o1/o3/o4 upstreams; +12 MB binary is within budget | Plan |
| Encoding selection | Heuristic substring match on upstream model name | gpt-4o/4.1/4.5/o1/o3/o4 → o200k_base, else cl100k_base; simple, good enough for v1 | Plan |
| Parse-error behavior | Best-effort re-parse with `json.Decoder.UseNumber()` to `map[string]any`, then 0 | Recovers from bodies that are valid JSON but fail strict validation; doesn't fail closed | Plan |
| Image/document cost | Fixed constants: 160/image, 500/document | Reasonable ballpark for typical Claude Code workloads; better than 0; configurable in a follow-up | Plan |
| Log level | `slog.Debug` for the count line | Default `info` stays quiet; operators can opt in to count visibility | Plan |
| File layout | Two new files: `proxy/translate/count.go` + `proxy/count_tokens_local.go` | Counter stays in `translate` to reuse unexported wire-format structs; dispatcher glue stays in `proxy/` | Research |
| Dispatcher integration | Replace 501 branch at `proxy/proxy.go:188-197`; set `X-Freedius-Matched-*` headers before the call | Minimal change; uses the already-buffered body; preserves the matched-headers invariant | Research |
| Accuracy verification | Round-trip test against real Anthropic API, gated on `ANTHROPIC_API_KEY`, 10% tolerance, 95% pass rate | Real-world signal without requiring the test in CI; tolerance is realistic for ~6 MB BPE approximation | Plan |
| Test changes | Invert 2 sub-cases of `TestServeHTTPCountTokens` (nim and mix+openai) from 501 to 200 + adapter-not-invoked | Smallest test delta; all 4 passthrough cases stay unchanged | Research |

## Scope

**In scope:**
- Add `tiktoken-go` and `tiktoken-go-loader` to `go.mod`
- New `proxy/translate/count.go` with `CountInputTokens` function, encoder singleton, content-block walk, encoding-selection heuristic, best-effort re-parse
- New `proxy/count_tokens_local.go` with `serveLocalCountTokens` dispatcher method and `countTokensResponse` struct
- Modify `proxy/proxy.go:188-197` to replace the 501 with a local-counter branch
- New `proxy/translate/count_test.go` with table-driven unit tests
- New `proxy/translate/count_accuracy_test.go` (env-gated round-trip test)
- Invert 2 sub-cases of `TestServeHTTPCountTokens` in `proxy/proxy_test.go`

**Out of scope:**
- Touching the Anthropic-protocol passthrough path
- Changing the `Provider` interface or `Registry` type
- Bundling `p50k_base` or `r50k_base` encodings
- Per-encoding config fields beyond the substring-match heuristic
- Image/document token-cost math from Anthropic's published formula
- Returning errors to the client on malformed bodies
- Logging the local count at info level
- A CLI command for manual accuracy verification

## Architecture / Approach

```
  POST /v1/messages/count_tokens
           │
           ▼
  ┌─────────────────────────┐
  │ Dispatcher.ServeHTTP    │   body already buffered (10 MiB cap)
  │ proxy/proxy.go:188-197  │   isCountTokensPath? supportsCountTokens?
  └────────────┬────────────┘
               │
       ┌───────┴───────┐
       │               │
   passthrough      local counter
   (anthropic)     (nim, openai, mix+openai)
       │               │
       ▼               ▼
   ┌──────────┐  ┌─────────────────────────┐
   │ Anthropic│  │ translate.CountInputTok │   cl100k_base or
   │ Compat   │  │ (proxy/translate/       │   o200k_base, picked
   │ Adapter  │  │  count.go)              │   by m.Model heuristic
   │          │  │                         │   + best-effort re-parse
   │ Reverse  │  │  - count system         │   + image=160, doc=500
   │ Proxy    │  │  - count messages       │   fixed costs
   │ (path    │  │  - count tools          │
   │ preserved│  │                         │
   │ )        │  └────────────┬────────────┘
   └────┬─────┘               │
        │                     │
        ▼                     ▼
   upstream             200 + Anthropic-format
   Anthropic            count_tokens response
```

**Key invariant:** the local counter short-circuits the adapter dispatch. The adapter is **never** invoked for the local-counter path. The matched `X-Freedius-Matched-Provider` / `X-Freedius-Matched-Model` headers are set before the call so the access log records them uniformly across all paths.

## Phases at a Glance

| Phase | What it delivers | Key risk |
| --- | --- | --- |
| 1. Counter implementation | `go.mod` deps + `proxy/translate/count.go` + `count_test.go` — counter function works in isolation | BPE blob loading is one-time slow on first call (acceptable) |
| 2. Dispatcher integration | `proxy/count_tokens_local.go` + `proxy/proxy.go:188-197` edit + 2 inverted sub-cases of `TestServeHTTPCountTokens` | Subtle: matched-headers invariant must be preserved for all 3 paths (passthrough, local counter, regular messages) |
| 3. Accuracy verification | `count_accuracy_test.go` round-trip test gated on `ANTHROPIC_API_KEY` | Network-dependent test; not in CI; tolerance 10% may be tight for unusual content blocks |
| 4. CI gate | `go test` + `go vet` + `go build` + `gofumpt` + `govulncheck` all pass | New dep must pass `govulncheck` clean (likely fine — both libs are actively maintained) |

**Prerequisites:** S-01 (first-call-routed) and the now-archived `count-tokens-passthrough` change. Both already landed.
**Estimated effort:** ~2-3 focused sessions. Phase 1 is the heaviest (counter + walk + tests); Phases 2-4 are each 1-2 hours.

## Open Risks & Assumptions

- **Encoding selection heuristic may misfire for unusual upstream model names.** Mitigation: defaults to cl100k_base, which matches ~95% of OpenAI-protocol upstreams. A future change can introduce a per-provider config field for explicit encoding selection.
- **The `imageTokenCost = 160` and `documentTokenCost = 500` constants are approximations.** Mitigation: matched roughly to Anthropic's lower bound for small images / 5-page PDFs. Heavy-image workflows will under-count, but count_tokens is documented as "best effort" by Anthropic.
- **`tiktoken-go` may not be byte-for-byte identical to Anthropic's tokenizer for edge-case Unicode.** Mitigation: the 10% tolerance in the round-trip test absorbs this; if a particular corpus entry exceeds 15% relative error, document it as a known limitation.
- **Binary size growth of +12 MB.** Mitigation: within the 50 MB NFR budget. If size becomes a concern, drop o200k_base in a follow-up (cl100k_base alone is +6 MB).
- **`tiktoken-go-loader` is a separate package with 16 stars and 5 commits.** Mitigation: it's a thin wrapper around `tiktoken-go`'s `BpeLoader` interface, MIT-licensed, and the alternative (downloading BPE at runtime) violates NFR-Privacy. If the loader becomes unmaintained, we can vendor a 30-line `BpeLoader` implementation ourselves.

## Success Criteria (Summary)

- `POST /v1/messages/count_tokens` returns 200 with a valid `input_tokens` count for every provider, not just Anthropic-protocol ones
- The 2 inverted sub-cases of `TestServeHTTPCountTokens` (nim and mix+openai) pass — proving the adapter is not invoked and the counter is computing
- The round-trip accuracy test shows the local count within 10% of Anthropic's count for ≥95% of the corpus (when run with `ANTHROPIC_API_KEY`)
- `make ci` passes; `govulncheck` reports no vulnerabilities in the new dependencies

# First Call Routed (S-01) — Plan Brief

> Full plan: `context/changes/first-call-routed/plan.md`
> Research: `context/changes/first-call-routed/research.md`

## What & Why

Wire the S-01 north-star slice for freedius: two real provider adapters (Nvidia NIM via Anthropic→OpenAI translation, and a custom Anthropic-compatible passthrough) plugged into the F-01 dispatch seam. Proves the entire product hypothesis — Claude Code calls route through freedius to a non-Anthropic provider with tool use, streaming, and error forwarding all intact.

## Starting Point

The F-01 foundation is in place: `proxy/proxy.go:32-91` has a working HTTP server, YAML config loader, body parser, and a 501 dispatch stub at lines 86-90. The `Config{Models map[string]Model}` struct is minimal (just `Provider` + `Model`). The strict-mode YAML decoder means any new config field must be added to `Model` in the same commit as the code that consumes it. F-01 review hardened timeouts, nil-checks, and error shapes; none of it constrains the adapter design.

## Desired End State

After S-01 lands, `freedius` starts, loads a config that maps a Claude Code model to either `provider: nim` or `provider: custom`, and proxies the request to the configured upstream. A real `claude-code` session (with `ANTHROPIC_BASE_URL` pointed at freedius) completes a tool-using, multi-turn conversation routed through freedius. Upstream errors (NIM 401/429/500, custom shim 4xx/5xx) reach Claude Code verbatim. Missing API key env vars fail at startup with a clear actionable message.

## Key Decisions Made

| Decision                       | Choice            | Why (1 sentence)  | Source           |
| ------------------------------ | ----------------- | ----------------- | ---------------- |
| Schema extension               | Per-model `base_url` + `api_key_env` (env-var NAME, not value) | Self-describing per mapping; no shared provider state; atomic with code that consumes it. | Plan Q1 / Research |
| Env-var loading                | Eager at startup, fail-fast | Misconfigurations surface at boot, not at 3 a.m. on first request. | Plan Q2 |
| S-01 scope                     | Text + tool-use only (no images, no documents) | Smallest translator state machine, fastest path to end-to-end proof. | Plan Q3 |
| Routing                        | Keep `mux.Handle("/", ...)` catch-all | S-01 doesn't know Claude Code's full path surface; tightening risks breaking an undocumented endpoint. | Plan Q4 / F-01 deferral |
| Adapter interface              | One-method `Provider.Handle(w, r, m, body) error` | Body is already buffered by the dispatcher; one seam hides the custom/NIM asymmetry. | Research §6.1 |
| Custom adapter impl            | `*httputil.ReverseProxy` with `Rewrite` (Go 1.20+) | Zero custom streaming code; SSE passes through verbatim; the textbook use case. | Research §2 |
| NIM adapter impl               | `http.Client` + `bufio.Reader` + `http.ResponseController` | `ReverseProxy.ModifyResponse` is the wrong layer for response-stream rewriting. | Research §2.3, §3.5 |
| Translation module location    | `proxy/translate/` package, pure bytes-in/bytes-out | TDD-friendly under `/10x-tdd`: golden files, no `httptest` server needed. | Research §6.2 |
| Upstream call timeout          | None — rely on `r.Context()` for client disconnect | Matches Anthropic SDK behavior; no wall-clock bound on long tool-use loops. | Plan Q5 |
| Error forwarding               | Verbatim upstream status + headers + body | Zero transformation logic; Claude Code logs the upstream JSON. | Plan Q6 / Research §5 |
| Custom provider auth           | `Authorization: Bearer <key>` only | Maximum compatibility with non-Anthropic custom shims. | Plan Q7 |
| `max_tokens` clamping          | Pass through unchanged | Honors user intent; NIM errors reach Claude Code if too large. | Plan Q8 |
| Test strategy                  | Golden-file + `httptest.NewServer` mocks; no real NIM in CI | Hermetic CI; no API key in secrets; matches F-01 test pattern. | Plan Q9 |
| Multi-modal `image` blocks     | Forward verbatim to NIM, let it handle | No code to maintain for an out-of-scope edge case; user sees a real NIM error. | Plan Q10 |
| Adapter construction           | Once at startup, share immutably across requests | NFR-Multi-agent satisfied with no locks; `http.Client` and `*ReverseProxy` are concurrent-safe. | Research §4 |
| `hop-by-hop` header handling   | `Rewrite` (Go 1.20+) over `Director` | `Director` runs before header cleanup; `Rewrite` runs after — auth headers survive. | Research §2.2 |
| `FlushInterval` for SSE        | Default (auto-flush for `text/event-stream`) | NIM's response is `text/event-stream`; `ReverseProxy` flushes immediately. | Research §2.6 |

## Scope

**In scope:**
- Schema extension: `config.Model.BaseURL` + `config.Model.APIKeyEnv` with validation
- `Provider` interface + `Registry` lookup
- `CustomAdapter` (passthrough via `*httputil.ReverseProxy`)
- `NIMAdapter` (Anthropic→OpenAI request translation + OpenAI→Anthropic SSE translation)
- Pure translation module in `proxy/translate/` (`TranslateRequest`, `TranslateStream`)
- `main.go` eager env-var loading + adapter construction + registry wiring
- Golden-file tests for the translator; `httptest.NewServer` tests for the adapters
- Eager failure on missing API keys for referenced providers

**Out of scope:**
- Multi-modal content blocks (images, documents) — forward verbatim to NIM only
- Prompt caching fields — emitted as `0` in Anthropic response
- `max_tokens` clamping — passed through unchanged
- Tightened HTTP routing — `mux.Handle("/", ...)` stays
- Total upstream-call timeout — rely on client disconnect
- Anthropic-version header on custom adapters — Bearer only
- Upstream error envelope wrapping — verbatim passthrough
- Real NIM in CI — `httptest` mocks only
- Top-level `providers:` block in YAML — per-model fields only
- Provider adapters for `zen` and `go` — still 501, deferred to S-02
- `freedius init` command and config template generation — S-03
- Auto-injection of Claude Code env vars — S-03
- Request-body logging (NFR-Privacy)
- Metrics endpoint, pprof, in-flight counters

## Architecture / Approach

Single `Provider` interface hides an asymmetry: the custom adapter is a 5-line `*ReverseProxy` wrapper, the NIM adapter is `http.Client` + stateful SSE translator. Both go through the dispatcher → registry → adapter path with no special-casing. The translation lives in pure functions (`proxy/translate/`) so it's TDD-friendly; the adapter's `Handle` is the only place HTTP I/O exists. Adapters are constructed once at startup and shared across requests — no locks, no atomics. Per-request state (bufio reader, translator instance) is allocated locally.

```
                          ┌─────────────────┐
   Claude Code ──POST──▶  │  net/http       │
                          │  ServeMux (/)   │
                          └────────┬────────┘
                                   │
                                   ▼
                          ┌─────────────────┐
                          │  Dispatcher     │  ← reads body, parses model, looks up
                          │  (proxy/)       │
                          └────────┬────────┘
                                   │ registry.Lookup(m.Provider)
                                   ▼
                          ┌─────────────────┐
                          │  Registry       │
                          │  (proxy/)       │
                          └────────┬────────┘
                                   │
                ┌──────────────────┴──────────────────┐
                ▼                                     ▼
       ┌─────────────────┐                   ┌─────────────────┐
       │  CustomAdapter  │                   │  NIMAdapter     │
       │  (proxy/)       │                   │  (proxy/)       │
       │  ReverseProxy   │                   │  http.Client    │
       └────────┬────────┘                   └────────┬────────┘
                │ Rewrite: SetURL + Auth             │ translate → POST → translate stream
                ▼                                     ▼
       ┌─────────────────┐                   ┌─────────────────┐
       │ Custom Anthropic-│                  │ NIM (OpenAI-    │
       │ compatible shim │                   │ compatible)     │
       └─────────────────┘                   └─────────────────┘
```

## Phases at a Glance

| Phase     | What it delivers       | Key risk                  |
| --------- | ---------------------- | ------------------------- |
| 1. Schema + Provider registry | `BaseURL` + `APIKeyEnv` config fields, `Provider` interface, `Registry` lookup, dispatcher wired to registry; 501 stub replaced with 500-on-unknown-provider | Strict-mode YAML decoder will reject unknown fields — new fields must land atomically with consumer code |
| 2. Custom passthrough adapter | `CustomAdapter` wrapping `*httputil.ReverseProxy` with `Rewrite`; 7 httptest cases; dispatcher end-to-end test | Body re-injection into `r.Body` is easy to forget — without it the upstream sees an empty body |
| 3. NIM adapter + translation module | Pure `proxy/translate/` package (golden-file tests), `NIMAdapter.Handle` with `http.Client` + `bufio.Reader` + `http.ResponseController`, eager `NVIDIA_NIM_API_KEY` check in `main.go` | Two SSE footguns: `json.NewEncoder.Encode` trailing newline and `bufio.Scanner` 64KB cap — both corrupt streaming if missed |

**Prerequisites:** F-01 landed (`make ci` green; dispatcher 501 stub in place). User has a real NIM API key for Phase 3 manual verification. User has a real Anthropic-compatible shim for Phase 2 manual verification.
**Estimated effort:** ~2-3 focused sessions across 3 phases. Phase 3 is the largest (~60% of the slice). The first two phases can land as single commits each; Phase 3 likely needs 2-3 commits (translation module, adapter, integration).

## Open Risks & Assumptions

- **NIM free-tier streaming format** (flagged in `roadmap.md:73` as a roadmap unknown): owner: user. Block: no — translator is permissive; partial streaming support is acceptable per FR-002 Socrates resolution. Tolerated: missing `[DONE]`, re-orderings of finish vs. usage chunks, empty content blocks.
- **Claude Code's exact request paths** (flagged in `roadmap.md:74`): S-01 keeps `mux.Handle("/", ...)`; tightening deferred until a real call confirms what paths Claude Code actually uses.
- **`input_tokens` in `message_start` may be 0** if NIM's usage chunk arrives late in the stream. Acceptable — the `output_tokens` count in the final `message_delta` is the real number. If a future S-XX requires accurate input tokens, a tokenizer pass is needed.
- **Goccy/go-yaml strict mode** may reject nested map fields differently from the F-01 baseline. If the new field validation surfaces an unexpected edge case, the fix is one or two lines in `config/config.go:51-63`.
- **`httputil.ReverseProxy` per-request construction** (intentional, documented in Phase 2 design note): adds a tiny garbage-collection pressure but keeps the test surface clean. If profiling shows this matters, switch to a single shared proxy with a per-call `Rewrite` closure.

## Success Criteria (Summary)

- `make ci` is green; per-package coverage: `config` ≥ 90%, `proxy` ≥ 85%, `proxy/translate` ≥ 90%.
- A real `claude-code` session with `ANTHROPIC_BASE_URL=http://127.0.0.1:8080` completes a tool-using, multi-turn conversation routed through freedius to NIM.
- The custom (Anthropic-compatible) adapter proxies a request to a real shim and Claude Code completes a real conversation.
- Upstream errors (NIM 401/429/500, custom shim 4xx/5xx) reach Claude Code verbatim — same status code, same body.
- Missing `NVIDIA_NIM_API_KEY` (when config references `provider: nim`) fails at startup with a clear actionable message.
- `X-Freedius-Matched-Provider` and `X-Freedius-Matched-Model` headers are present on every dispatcher's response, including adapter responses.
- The `context/foundation/lessons.md` file exists with at least the two SSE footguns documented for future slices.

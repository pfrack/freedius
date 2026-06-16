---
date: 2026-06-16T13:30:17+02:00
researcher: opencode
git_commit: fe44ae681ca7de5397d27af99a32b1b2a8ae7187
branch: proxy-skeleton
repository: pfrack/freedius
topic: "S-01 first-call-routed — NIM adapter + custom passthrough (adapter architecture in Go)"
tags: [research, s-01, first-call-routed, adapter, nim, custom, reverse-proxy, sse-translation, streaming]
status: complete
last_updated: 2026-06-16
last_updated_by: opencode
---

# Research: S-01 first-call-routed — adapter architecture in Go

**Date**: 2026-06-16 13:30 CEST
**Researcher**: opencode
**Git Commit**: `fe44ae681ca7de5397d27af99a32b1b2a8ae7187` (local only — branch `proxy-skeleton` not yet pushed to `origin`)
**Branch**: `proxy-skeleton`
**Repository**: `pfrack/freedius`

## Research Question

What is the recommended Go-stdlib-only adapter architecture for the S-01 (first-call-routed) slice? Specifically: where exactly in the F-01 foundation do the NIM translation adapter and the custom-passthrough adapter plug in, what does the `Provider` interface look like, how does `httputil.ReverseProxy` factor in, how is the OpenAI↔Anthropic SSE translation pipeline structured, and what config-schema extensions are required?

## Summary

The F-01 dispatch seam is a single 14-line block at `proxy/proxy.go:86-90` that S-01 replaces with a `Provider` registry dispatch. The custom (Anthropic-compatible) adapter is a textbook `httputil.ReverseProxy` wrapped behind a one-method `Provider.Handle` interface. The NIM adapter uses `http.Client` + `bufio.Reader.ReadBytes('\n')` + `http.NewResponseController(w).Flush()` for streaming translation — *not* `ReverseProxy.ModifyResponse`, which is too painful for response-stream rewriting. All translation logic lives in pure-bytes-in/bytes-out functions under `proxy/translate/`, separated from I/O so they're TDD-friendly under `/10x-tdd`. The config schema must grow two fields per model — `base_url` and `api_key_env` (env-var *name*, not value) — to support the custom passthrough and per-model NIM overrides. Concurrency is free: all adapters are constructed once at startup, share state immutably, and the per-request translation state is allocated locally per call.

**Recommended package layout** (single `proxy` package, no subpackage yet):

```
proxy/
  proxy.go            # Dispatcher (existing — minimal diff)
  proxy_test.go
  provider.go         # Provider interface + Registry
  provider_test.go
  nim.go              # NIMAdapter
  nim_translate.go    # pure request/response translation funcs
  nim_translate_test.go
  custom.go           # CustomAdapter (ReverseProxy wrapper)
  custom_test.go
  errors.go           # forwardUpstreamError, freediusErrorHandler
```

**Recommended `Provider` interface:**

```go
type Provider interface {
    Handle(w http.ResponseWriter, r *http.Request, m config.Model, body []byte) error
}
```

Adapter contract: returning `nil` means the adapter has called `w.WriteHeader` (success or upstream-mirrored error); returning a non-nil error means "I did not write a response — dispatcher, write 502". This single-owner rule eliminates response-state confusion.

**Top three things S-01 must add beyond the F-01 baseline:**
1. `config.Model.BaseURL` + `config.Model.APIKeyEnv` schema fields with validation
2. `proxy.Registry` keyed by provider name, populated at boot in `main.go` from `os.Getenv`
3. `proxy/translate/anthropic_openai.go` — the pure translation module (the only non-trivial code in the slice)

## Detailed Findings

### 1. F-01 foundation seams (where S-01 plugs in)

#### 1.1 The dispatch seam — `proxy/proxy.go:86-90`

The current stub is 14 lines that return `501 Not Implemented` with a JSON body echoing the matched provider/model. S-01 replaces this entire block. The model lookup at `proxy/proxy.go:76` (the `m, ok := d.Cfg.Models[req.Model]` line and its `if !ok → 404` branch) is **not** S-01's concern — keep it.

Data available at the moment of dispatch (the seam):

| Symbol | Source | What it contains |
|---|---|---|
| `body` | `proxy/proxy.go:47` | Raw request body bytes (the full Anthropic `/v1/messages` payload) |
| `req.Model` | `proxy/proxy.go:64` | Parsed `model` field (string) |
| `m` | `proxy/proxy.go:76` | Matched `config.Model{Provider, Model}` |
| `m.Provider` | `config/config.go:18` | String discriminator: `nim` / `zen` / `go` / `custom` |
| `d.Cfg` | `proxy/proxy.go:18,29` | Full config |
| `d.Logger` | `proxy/proxy.go:19,29` | Pre-tagged slog logger (`component=proxy`) |
| `r` | `proxy/proxy.go:32` | Original `*http.Request` |

Pre-dispatch side effects that S-01 must preserve (per F-01 plan `plan.md:83-89`):
- `d.Logger.Debug("dispatch", ...)` at line 83
- `X-Freedius-Matched-Provider` and `X-Freedius-Matched-Model` response headers at lines 84-85

Pre-dispatch validation pipeline (lines 33-74) that S-01 inherits unchanged: method check (405), Content-Type check (415), body size cap (10 MiB → 413), body read (400), JSON validation (400), missing-`model` (400). The F-01 review (`reviews/impl-review.md:54-61`) settled these status codes.

#### 1.2 The provider discriminator

`Model.Provider` is a string in the closed set `{nim, zen, go, custom}` (`config/config.go:22-27`). F-01 only uses it to echo into the 501 response body. S-01 must add the actual dispatch — a `switch m.Provider` or a `map[string]Provider` lookup. Only `nim` and `custom` need real implementations in S-01; `zen` and `go` continue to return 501 (deferred to S-02).

#### 1.3 Config schema gaps

F-01's `Model` struct (`config/config.go:17-20`) is `{Provider, Model}` — two fields. S-01 needs:

```go
type Model struct {
    Provider  string `yaml:"provider"`
    Model     string `yaml:"model"`
    BaseURL   string `yaml:"base_url,omitempty"`    // required for provider=custom; optional override for provider=nim
    APIKeyEnv string `yaml:"api_key_env,omitempty"` // env-var NAME (e.g. "NIM_API_KEY"), not the value
}
```

Validation rules to add in `config.Load`:
- Reject `provider=custom` without `base_url`
- Reject `base_url` with non-`http(s)` scheme (defensive: config-load-time is the cheapest place to fail)
- Reject `api_key_env` with newlines or `=` (env-var name syntax)

The `api_key_env` field stores the *name* of the env var, not the value itself (per PRD FR-004 — credentials in env vars, mappings in config). The adapter does `os.Getenv(m.APIKeyEnv)` and returns 500 with a clear "missing API key" message if unset.

#### 1.4 Error handling seam

`writeError` at `proxy/proxy.go:101-107` returns `{"error": "..."}`. This is the F-01 contract for **dispatcher-side** errors (bad config, missing env var, adapter failure). For **upstream errors** (NIM 401/429/500), the recommendation is **verbatim passthrough** — forward the upstream status code, response headers (with hop-by-hop filtered), and body bytes to the client. Claude Code sees NIM's actual error body and can act on it. Implementing this requires a new helper, e.g. `forwardUpstreamError(w, resp)` at `proxy/proxy.go:~95`.

#### 1.5 Logging seam

Current pattern: `slog` with `With("component", "proxy")` on the dispatcher (`proxy/proxy.go:29`). New component tags S-01 adds: `adapter.nim`, `adapter.custom`, `proxy.stream` (or `proxy.upstream` for call-level events). Convention: dotted lowercase to match `"proxy"`. Adapter log lines should be Debug level by default (no per-request log spam, per F-01 plan `plan.md:346`); NFR-Privacy forbids logging request/response bodies (F-01 plan `plan.md:59, 250`).

#### 1.6 Test infrastructure seam

`newTestDispatcher` at `proxy/proxy_test.go:15-24` is extendable. S-01 adds:
- A second `custom` model entry to cover both branches
- A `httptest.NewServer`-based mock NIM upstream that returns canned SSE
- A `httptest.NewServer`-based mock custom upstream that returns canned Anthropic-format SSE
- New test files: `proxy/nim_test.go`, `proxy/custom_test.go`, `proxy/nim_translate_test.go`, `proxy/provider_test.go`

The table-driven test pattern at `proxy/proxy_test.go:97-136` carries over directly to adapter tests.

#### 1.7 Module path

`go.mod:1` and all imports use `github.com/pfrack/freedius` (consistent with `AGENTS.md:40`). The F-01 plan (`plan.md:27`) referenced the original `github.com/user/freedius` module name — that was a stale observation in the plan, not a current inconsistency. S-01 uses `github.com/pfrack/freedius/...` everywhere.

### 2. `httputil.ReverseProxy` API surface (Go 1.20+)

Verified against Go 1.25.3 / 1.26 stdlib docs. All patterns are stdlib-only.

#### 2.1 Struct shape

```go
type ReverseProxy struct {
    Rewrite        func(*ProxyRequest)               // Go 1.20+, preferred
    Director       func(*http.Request)               // legacy
    Transport      http.RoundTripper                 // nil ⇒ http.DefaultTransport
    FlushInterval  time.Duration
    ErrorLog       *log.Logger
    BufferPool     BufferPool
    ModifyResponse func(*http.Response) error
    ErrorHandler   func(http.ResponseWriter, *http.Request, error)
}
```

At most one of `Rewrite` or `Director` may be set. Use `Rewrite` (Go 1.20+) — it is the documented preferred form because hop-by-hop header cleanup happens *after* `Director` returns, which can strip headers Director set. `Rewrite` operates on a `ProxyRequest` with `In`/`Out` fields and helpers (`SetURL`, `SetXForwarded`).

#### 2.2 `FlushInterval` semantics for SSE

`FlushInterval = 0` (default) — no periodic flushing. A negative value flushes after every write. **But**: "The FlushInterval is ignored when ReverseProxy recognizes a response as a streaming response, or if its ContentLength is -1; for such responses, writes are flushed to the client immediately." NIM returns `Content-Type: text/event-stream` and `Content-Length: -1` (chunked), so the proxy auto-flushes with no extra configuration. Setting `FlushInterval: -1` defensively is harmless.

#### 2.3 `ModifyResponse` and `ErrorHandler` for freedius

`ModifyResponse` is called when the backend returns *any* response (including 4xx/5xx). Returning an error routes to `ErrorHandler`. For the NIM adapter this is the wrong layer to do SSE translation — see §3.5.

`ErrorHandler` is called only when the transport itself fails (dial error, TLS error, or `ModifyResponse` returns an error). Default implementation writes `502 Bad Gateway` with no body. For freedius, override:

```go
func freediusErrorHandler(logger *slog.Logger) func(http.ResponseWriter, *http.Request, error) {
    return func(w http.ResponseWriter, r *http.Request, err error) {
        if errors.Is(err, context.Canceled) {
            logger.Debug("client disconnect", "path", r.URL.Path)
            return  // client is gone; do not write
        }
        logger.Error("upstream error", "err", err, "path", r.URL.Path)
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusBadGateway)
        _ = json.NewEncoder(w).Encode(map[string]string{
            "error": "upstream_unreachable",
            "detail": err.Error(),
        })
    }
}
```

#### 2.4 Recommendation for the custom adapter

`CustomAdapter` wraps a single `*httputil.ReverseProxy` constructed at startup. `Handle` re-injects the already-buffered body into `r.Body` and delegates to the proxy. The proxy's `Rewrite` does URL swap + auth header. No `ModifyResponse` (response is Anthropic SSE, passed through unchanged).

### 3. SSE translation pipeline

#### 3.1 Anthropic SSE format (what Claude Code expects)

Wire shape: each event is `event: <type>\ndata: <json>\n\n`. The blank line (`\n\n` after `data:`) is the event terminator — Claude Code's SDK buffers until it sees it.

Verified event types and payloads (from Anthropic SDK test fixtures):

- `message_start` — once, at the start. Carries `message.id`, `message.model`, `message.usage.input_tokens` (1, 11 in fixtures), `message.usage.output_tokens` (1).
- `content_block_start` — per block. Text variant: `{type:"text", text:""}`. Tool-use variant: `{type:"tool_use", id, name, input:{}}`.
- `content_block_delta` — many per block. Text: `delta:{type:"text_delta", text:"..."}`. Tool-use: `delta:{type:"input_json_delta", partial_json:"..."}` (string fragment of arguments, accumulated by client).
- `content_block_stop` — per block.
- `message_delta` — once, near end. Carries `delta.stop_reason` and `usage.output_tokens` (the real final count).
- `message_stop` — terminator. Canonical: `data: {"type":"message_stop"}`; some servers emit just the event line — be safe and always emit both.
- `ping` — heartbeat. **Drop these on translation** (OpenAI has no equivalent, Claude Code tolerates absence).
- `error` — mid-stream. `data: {"type":"error","error":{"type":"api_error","message":"..."}}`.

`index` is the zero-based counter of content blocks in the current assistant turn, assigned by the translator as blocks are opened.

#### 3.2 OpenAI SSE format (what NIM emits)

Wire shape: `data: <json>\n\n` only — no `event:` line. Final terminator: `data: [DONE]\n\n`.

Chunk shape:

```json
{
  "id": "chatcmpl-abc123",
  "object": "chat.completion.chunk",
  "created": 1756315657,
  "model": "meta/llama-3.1-70b-instruct",
  "choices": [
    {"index": 0, "delta": {"role": "assistant"|"content":"..."|"tool_calls":[...]}, "finish_reason": null|"stop"|"tool_calls"|"length"|"content_filter" }
  ]
}
```

`tool_calls[].index` is the parallel-call index, **not** a content-block index. The first delta carries `id` and `function.name`; subsequent deltas carry `function.arguments` fragments (string). The translator must maintain per-`tool_calls.index` state.

`finish_reason` mapping:

| OpenAI | Anthropic | Notes |
|---|---|---|
| `null` | (mid-stream) | more content coming |
| `"stop"` | `"end_turn"` | natural stop |
| `"length"` | `"max_tokens"` | truncated |
| `"tool_calls"` / `"function_call"` | `"tool_use"` | at least one tool call |
| `"content_filter"` | `"refusal"` | filtered |

OpenAI cannot signal Anthropic's `stop_sequence` (when model stops on a custom string, OpenAI reports `"stop"`). Default to `end_turn`; this fidelity loss is unavoidable.

Usage chunk — when `stream_options: {"include_usage": true}` is set, OpenAI emits one extra chunk *after* the finish_reason chunk, *before* `[DONE]`, with `choices: []` and a `usage: { prompt_tokens, completion_tokens, total_tokens }` object. **Always request this** — without it, `output_tokens` is unknowable mid-stream.

#### 3.3 Request translation rules (Anthropic → OpenAI)

| Anthropic | OpenAI | Notes |
|---|---|---|
| `system: "..."` (string) | prepend `{"role":"system","content":"..."}` to `messages` | OpenAI has no top-level system field |
| `system: [...]` (array) | concat text blocks → system msg | drop non-text system blocks (rare) |
| `messages[].content` (string) | `content: "..."` | pass through |
| `messages[].content` (array, user) | `content_parts` array OR flatten to string | image → `image_url`; tool_result → separate `role:"tool"` msg |
| `messages[].content` (array, assistant) | `content: "..."` + `tool_calls` array | text stringified; tool_use → completed tool_calls |
| `max_tokens` | `max_tokens` | identical |
| `tools[]` | `tools[]` with `type:"function"` | unwrap `{name,description,input_schema}` → `{type:"function",function:{name,description,parameters}}` |
| `tool_choice` | `tool_choice` | `auto`↔`auto`, `any`↔`required`, `tool:{name}`↔`{type:"function",function:{name}}` |
| `temperature`, `top_p` | `temperature`, `top_p` | identical |
| `stop_sequences` | `stop` | as string or array |
| `stream: true` | `stream: true` + `stream_options:{"include_usage":true}` | mandatory for output_tokens |
| `metadata`, `service_tier` | (drop) | no equivalents |

#### 3.4 Response translation state machine

The translator carries across chunks: `blockIdx int`, `currentBlockType string` (`""`/`"text"`/`"tool"`), `toolState map[int]toolAccum` (keyed by OpenAI `tool_calls.index`), `model`, `messageID`, `inputTok int`, `outputTok int`, `sawUsage bool`, `done bool`.

- **Role chunk** (first, no content): emit `message_start` with generated `msg_<uuid>`, real `model`, `usage: { input_tokens: 0, output_tokens: 0 }`.
- **Text content delta**: open a text block (`content_block_start` with `type:"text",text:""`) if not already open; emit `content_block_delta` with `text_delta`.
- **Tool call delta**: per entry, look up `toolState[entry.index]`. New → emit `content_block_start` (tool_use) and save `anthropicIndex` to `toolState`. Then emit `content_block_delta` with `input_json_delta.partial_json = entry.function.arguments` (skip if empty).
- **`finish_reason` chunk**: emit `content_block_stop` for the open block (if any), then `message_delta` with translated `stop_reason` and the most recent `outputTok`. Reset `currentBlockType`.
- **Usage chunk** (`len(choices) == 0 && usage != nil`): save `completion_tokens` to `outputTok`. Do not emit an event yet.
- **`[DONE]` sentinel**: emit `message_stop` (`data: {"type":"message_stop"}`), close.
- **Error chunk** (top-level `error` field, or HTTP non-2xx before stream begins): emit `event: error\ndata: {"type":"error","error":{"type":"api_error","message":...}}\n\n`, then `message_stop`, close.

#### 3.5 Go implementation patterns

**Reading the upstream stream** — use `bufio.Reader.ReadBytes('\n')` rather than `bufio.Scanner`. Scanner's default `MaxScanTokenSize` is 64 KB (`bufio.MaxScanTokenSize`) and exceeding it fails silently; tool-use `arguments` can exceed this. `bufio.Reader` has no fixed line cap.

```go
br := bufio.NewReaderSize(upstream, 64*1024)
for {
    line, err := br.ReadBytes('\n')
    if len(line) > 0 { /* process */ }
    if errors.Is(err, io.EOF) { return nil }
    if err != nil { return err }
}
```

**Custom split for SSE events** (SSE events are blocks terminated by blank line, not single lines):

```go
func scanSSEEvents(data []byte, atEOF bool) (advance int, token []byte, err error) {
    if atEOF && len(data) == 0 { return 0, nil, nil }
    if i := bytes.Index(data, []byte("\n\n")); i >= 0 {
        return i + 2, data[:i], nil
    }
    if atEOF { return len(data), data, nil }
    return 0, nil, nil
}
```

Per block, concatenate lines starting with `data: ` (strip the prefix); check for `[DONE]`; otherwise `json.Unmarshal`.

**Writing the downstream stream** — use `http.NewResponseController(w).Flush()` (Go 1.20+), not the older `http.Flusher` type assertion. `ResponseController` walks the `Unwrap() http.ResponseWriter` chain, returning `http.ErrNotSupported` (Go 1.21+) if absent.

```go
rc := http.NewResponseController(w)
emit := func(eventType string, payload any) error {
    raw, err := json.Marshal(payload)            // json.Marshal — NOT json.NewEncoder.Encode
    if err != nil { return err }
    if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, raw); err != nil {
        return err
    }
    return rc.Flush()
}
```

**The `json.Encoder` trailing-newline trap** — `json.NewEncoder(w).Encode(v)` appends `\n` to the JSON. Using the resulting bytes in `Fprintf(w, "data: %s\n\n", buf)` produces `data: {...}\n\n\n` (three newlines = corrupted event framing). Use `json.Marshal` (no trailing newline).

**Architecture recommendation** — single goroutine, sequential read-parse-emit:

```go
func (a *NIMAdapter) Handle(w http.ResponseWriter, r *http.Request, m config.Model, body []byte) error {
    upstreamBody, err := translateRequest(body, m.Model)
    if err != nil { return err }
    req, _ := http.NewRequestWithContext(r.Context(), "POST", a.baseURL+"/v1/chat/completions", bytes.NewReader(upstreamBody))
    req.Header.Set("Authorization", "Bearer "+a.apiKey)
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Accept", "text/event-stream")

    resp, err := a.client.Do(req)
    if err != nil { return err }  // ErrorHandler routes to 502
    defer resp.Body.Close()

    if resp.StatusCode >= 400 {
        return forwardUpstreamError(w, resp)
    }

    w.Header().Set("Content-Type", "text/event-stream")
    w.WriteHeader(http.StatusOK)
    rc := http.NewResponseController(w)
    return translateStream(rc, w, resp.Body)
}
```

**Backpressure** — `io.Copy`-style writes from the upstream block on `w.Write`, so a slow client backpressures the upstream automatically. No ringbuffer needed.

**Cancellation** — build the upstream request with `r.Context()` via `http.NewRequestWithContext`. Client disconnect propagates as `context.Canceled` from `client.Do` and `resp.Body.Read`; the loop returns the error and the stream closes.

#### 3.6 Why not `ReverseProxy.ModifyResponse` for NIM

`ModifyResponse` can wrap `resp.Body` in a translating `io.ReadCloser`, but the translator is push-style (state machine over SSE events) while `Read` is pull-style. The wrapper ends up holding a tiny `bufio` plus output queueing, and the lifecycle is coupled to the reverse-proxy in a way that's painful to test. Manual `http.Client` + `bufio.Reader` + `http.ResponseController` is more explicit, easier to TDD, and is what the same Anthropic→OpenAI proxy space uses in practice (e.g. `kyungw00k/anthropic-openai-proxy-go`).

### 4. Concurrency and the multi-agent NFR

NFR-Multi-agent: multiple concurrent Claude Code sessions. Verified that `httputil.ReverseProxy` is safe for concurrent use — its struct fields are read-only after construction, functions take per-request arguments, and the docs explicitly say "Rewrite must not access the provided ProxyRequest or its contents after returning." `http.Client` and `http.Transport` are also safe for concurrent use.

Per-request state (`bufio.Reader`, translator instance) is allocated locally in each `Handle` call. No `sync.RWMutex` is needed — `*config.Config` and the adapter registry are loaded once at startup and never mutated. If S-03 adds config hot-reload, use `atomic.Pointer[Config]`, not `RWMutex`.

### 5. Recommended S-01 data flow

#### 5.1 Non-streaming request (custom passthrough)

```
Claude Code POST /v1/messages {model:"foo"}
  → mux → Dispatcher.ServeHTTP
    → reads body, parses {model}, looks up cfg.Models["foo"]
    → looks up registry["custom"] → *CustomAdapter
  → CustomAdapter.Handle(w, r, m, body)
    → r.Body = NopCloser(bytes.NewReader(body)); r.ContentLength = len(body)
    → ReverseProxy.ServeHTTP(w, r)
      → Rewrite: SetURL(m.BaseURL); req.Host = baseURL.Host; Authorization = Bearer os.Getenv(m.APIKeyEnv)
      → RoundTrip → upstream
      → copy resp headers + body to w (verbatim 4xx/5xx pass through)
  → Claude Code receives upstream response unchanged
```

#### 5.2 Streaming SSE (NIM translation)

```
Claude Code POST /v1/messages {model:"claude-opus-4", stream:true, tools:[...]}
  → Dispatcher → NIMAdapter.Handle(w, r, m, body)
    → translateRequest(body, m.Model) → OpenAI-shaped []byte
    → http.NewRequestWithContext(r.Context(), POST, baseURL+"/v1/chat/completions", body)
    → resp, err := client.Do(req); if err → return err (dispatcher writes 502)
    → if resp.StatusCode >= 400 → forwardUpstreamError(w, resp); return nil
    → w.Header().Set("Content-Type","text/event-stream"); w.WriteHeader(200)
    → rc := http.NewResponseController(w)
    → translateStream(rc, w, resp.Body)
      → br := bufio.NewReaderSize(resp.Body, 64*1024)
      → t := newAnthropicEmitter()  // state machine
      → for each chunk: t.consumeOpenAILine → []anthropicEvent → emit + rc.Flush()
      → on [DONE] → emit message_stop
```

### 6. Test strategy

Capture-and-assert, no mocking framework. All translation functions are pure bytes-in/bytes-out:

1. **Request translation** — golden-file tests. Each test loads an Anthropic JSON fixture, calls `translate.TranslateRequest`, asserts byte-exact equality with a stored OpenAI JSON fixture. Cover: text-only, single tool, parallel tools, system-as-string, system-as-blocks, stop_sequences, tool_choice variants.
2. **Stream translation** — pipeline test with `strings.Reader` (upstream), `bytes.Buffer` (downstream), and a `func() error` flush stub. Assert byte-exact output against a recorded Anthropic SSE fixture; assert flush call count equals event count.
3. **Adversarial streams** — empty stream (just `[DONE]`), error chunk instead of `[DONE]`, parallel tools with interleaved deltas, `finish_reason: "content_filter"`, no usage chunk (omit `stream_options`), `finish_reason` chunk with trailing content.
4. **Encoding tests** — assert output never contains `\n\n\n` (trailing-newline trap), every event followed by a flush, `event:`/`data:` never split.
5. **Adapter integration** — `httptest.NewServer` mock NIM + custom upstreams, assert that `CustomAdapter` sends the expected `Authorization` header, `NIMAdapter` translates the body and re-emits as Anthropic SSE.
6. **Dispatcher-level** — extend `newTestDispatcher` with both `nim` and `custom` model entries; assert 502 (with adapter-stubbed failure), 200 (passthrough), and the existing 404/405/413/415/400 behaviors are preserved.

The byte-exact property is load-bearing: Anthropic's SDK parses events strictly, and Claude Code's UI depends on `id` and `index` being stable. A single off-by-one in `blockIdx` corrupts the conversation's tool-call display. Golden files catch this cheaply.

### 7. Open questions for the planner

1. **Per-mapping `BaseURL` vs. provider-global?** Per-mapping (`Model.BaseURL`) is correct for `custom` and works for NIM as an override; a top-level `providers:` block is cleaner long-term and matches S-02's expansion to Zen/Go. Recommendation: per-mapping for S-01 simplicity; revisit at S-02.
2. **How does the dispatcher hand off the body?** Currently `Dispatcher` reads + parses model; the adapter receives `body []byte`. This is fine for both passthrough (re-injects into `r.Body`) and translation (passes to translator). Document this as the seam.
3. **Inbound `Content-Type` and `Accept` for the custom adapter?** Custom forwards them; NIM overrides to `application/json` and `text/event-stream`. Surface in the adapter test matrix.
4. **Output token accounting without the usage chunk?** If a user misconfigures and `stream_options.include_usage` doesn't reach NIM, `output_tokens` will be 0 in the final `message_delta`. Document this as a known limitation; recommend always sending `include_usage: true` in the request translation.
5. **NIM free-tier streaming quirks** (flagged in `roadmap.md:73` as a roadmap unknown)? Owner: user. Partial streaming support is acceptable per FR-002 Socrates resolution. The translator should tolerate empty content blocks, missing `[DONE]`, and re-orderings of finish vs. usage chunks.

## Code References

### Foundation (already in repo)

- `main.go:36-118` — entry point, server lifecycle
- `main.go:82` — `proxy.NewDispatcher(cfg, logger)` (becomes `proxy.NewDispatcher(cfg, registry, logger)`)
- `main.go:84` — `mux.Handle("/", dispatcher)` (optionally tighten to `mux.Handle("POST /v1/messages", dispatcher)` per F-01 plan `plan.md:314`)
- `proxy/proxy.go:15` — `MaxBodyBytes = 10 * 1024 * 1024`
- `proxy/proxy.go:32-91` — `Dispatcher.ServeHTTP` (S-01's primary edit target)
- `proxy/proxy.go:76-81` — model lookup + 404 on no-match (preserve)
- `proxy/proxy.go:83-85` — debug log + `X-Freedius-Matched-*` headers (preserve)
- `proxy/proxy.go:86-90` — 501 stub (S-01's entire replace target)
- `proxy/proxy.go:93-99` — `writeJSON` (reusable for dispatcher-side responses)
- `proxy/proxy.go:101-107` — `writeError` (reusable for dispatcher-side errors)
- `proxy/proxy.go:15-30` — `Dispatcher` struct + `NewDispatcher` (extends with `Registry` field)
- `proxy/proxy_test.go:15-24` — `newTestDispatcher` (extend with custom model)
- `proxy/proxy_test.go:97-136` — table-driven test pattern (reuse)
- `config/config.go:13-27` — `Config`, `Model`, `KnownProviders` (extend `Model` with `BaseURL`, `APIKeyEnv`)
- `config/config.go:51-63` — per-model validation loop (add new field validation)
- `config/config.go:43` — strict YAML mode (will reject new fields not added to `Model` struct)
- `config.example.yaml:1-7` — example config (update to demonstrate new fields)
- `AGENTS.md:31-32` — test conventions (next to file, `httptest` for HTTP tests)
- `AGENTS.md:26` — Go 1.22+ ServeMux pattern matching (use for tightened routing if S-01 wants it)

### To be added by S-01

- `proxy/provider.go` — `Provider` interface, `Registry` type
- `proxy/provider_test.go`
- `proxy/nim.go` — `NIMAdapter` struct + `NewNIMAdapter` + `Handle`
- `proxy/nim_translate.go` — `TranslateRequest` + `TranslateStream` + `anthropicEmitter` (pure functions)
- `proxy/nim_translate_test.go` — golden-file request + stream tests
- `proxy/nim_test.go` — adapter integration tests with `httptest.NewServer`
- `proxy/custom.go` — `CustomAdapter` struct + `NewCustomAdapter` + `Handle` (wraps `*httputil.ReverseProxy`)
- `proxy/custom_test.go`
- `proxy/errors.go` — `forwardUpstreamError`, `freediusErrorHandler`

### F-01 review fixes S-01 inherits

- `proxy-skeleton/reviews/impl-review.md:54-61` (F4) — 413 for oversize body, 404 for unknown model
- `proxy-skeleton/reviews/impl-review.md:73-82` (F6) — Content-Type validation, encode-error logging
- `proxy-skeleton/reviews/impl-review.md:88-95` (F7) — nil-check in `NewDispatcher`, single-source `KnownProviders`
- `proxy-skeleton/reviews/impl-review.md:96-104` (F8) — `yaml.FormatError(..., true, ...)` with source excerpt, single-string startup log

## Architecture Insights

1. **The provider interface is the wrong place to put translation logic.** Keep `Provider.Handle` as a one-method interface; do the request/response translation in pure functions under `proxy/translate/`. This is what makes the slice TDD-able: `bytes.Buffer` in, `bytes.Buffer` out, golden files in/out. Without the pure-function split, the translation tests would have to spin up `httptest` servers and parse wire formats — much slower feedback.

2. **`httputil.ReverseProxy` is for passthrough, not translation.** For the custom adapter it's perfect: URL swap, auth header, body forward, SSE forward, hop-by-hop cleanup, error forwarding — all built in. For NIM, the response stream must be rewritten, and `ModifyResponse` is the wrong layer. Use `http.Client` + manual streaming for the NIM adapter. The unified `Provider` interface hides this asymmetry from the dispatcher.

3. **Concurrency is free because config and adapters are immutable post-boot.** No mutexes, no atomics. Each request allocates a `bufio.Reader` and a translator state machine locally. The pattern scales to NFR-Multi-agent (multiple concurrent Claude Code sessions) without lock contention.

4. **The schema extension is small but unavoidable.** `config.Model` grows by two fields (`base_url`, `api_key_env`). The existing strict-mode YAML decoder (`config/config.go:43`) will fail startup on unknown fields, so the new fields must be added in the same commit as the code that consumes them — no in-between state. The F-01 review's F7 finding (single-source `KnownProviders`) means the new fields should follow the same single-source pattern (one struct, one validation loop).

5. **Error forwarding strategy: verbatim upstream, structured dispatcher.** Two different shapes, intentional. Upstream 401/429/500 from NIM should reach Claude Code unchanged (Claude Code's UI knows how to render Anthropic-format errors; let it). Dispatcher-side errors (missing env var, adapter panic, transport error) use the existing `writeError` shape `{"error":"..."}` — single-source, consistent, easy to grep.

6. **`json.Marshal`, never `json.NewEncoder.Encode` for SSE.** The trailing-newline trap (`Encode` adds `\n`) corrupts event framing. This is a class-of-bug worth surfacing in `context/foundation/lessons.md` after S-01 lands.

7. **The `json.Encoder`-newline trap and the `bufio.Scanner` 64 KB cap are both well-known Go footguns.** They will hit S-01 immediately; documenting them in the implementation plan prevents a debugging session when tool-use `arguments` exceed 64 KB or when Claude Code hangs waiting for the second `\n`.

## Historical Context (from prior changes)

- `context/foundation/roadmap.md:23-24` — S-01 is the "north star" slice; the dual-provider scope (NIM + custom) is intentional because the custom passthrough serves as a fallback validation path if NIM's API surprises us.
- `context/foundation/roadmap.md:73-74` — Two roadmap unknowns for S-01: NIM free-tier streaming format (does NIM's SSE match the OpenAI format the adapter targets?) and Claude Code's `CLAUDE_CODE_API_BASE` env-var behavior. Both are flagged as "no block" — the slice still ships if one provider path works.
- `context/foundation/prd.md:74-83` — FR-006 (NIM) and FR-009 (custom) are the S-01 functional requirements. FR-004 (env-var credentials) is the only one that constrains the schema extension.
- `context/foundation/prd.md:88` — NFR-Latency ("imperceptible overhead") justifies the inline single-goroutine translation architecture (no `io.Pipe` overhead).
- `context/changes/proxy-skeleton/plan.md:21,23,55,56,57,83,235` — Every "S-01" mention in the F-01 plan enumerates what F-01 deliberately didn't do. S-01 owns: provider translation, adapters, credential loading, real upstream calls, streaming support.
- `context/changes/proxy-skeleton/plan.md:31` — "F-01 only touches the mappings side; credentials are S-01's concern" — the schema extension is the direct consequence of this deferral.
- `context/changes/proxy-skeleton/plan.md:68-79` — F-01 locked the YAML schema at planning time to avoid S-01 schema migration. S-01's schema extension (`base_url`, `api_key_env`) is therefore a *new* migration, but the strict-mode decoder means it must be atomic with the code that consumes the fields.
- `context/changes/proxy-skeleton/plan.md:250` — "do not log the body — NFR-Privacy" — applies to upstream requests too; S-01 must not log request or response bodies.
- `context/changes/proxy-skeleton/plan.md:314` — "Register `mux.Handle("/", dispatcher.ServeHTTP)` (catch-all — Claude Code's exact paths are not yet known; S-01 will tighten the routing)" — S-01 may want to switch to `mux.Handle("POST /v1/messages", ...)` per AGENTS.md:26.
- `context/changes/proxy-skeleton/plan.md:360` — "full integration tests come in S-01 when the proxy actually proxies to a real provider" — S-01 introduces the first `httptest.NewServer`-based integration tests.
- `context/changes/proxy-skeleton/reviews/impl-review.md:54-104` — All eight F-01 review findings are F-01 hardening that S-01 inherits. None of them constrain S-01's adapter design.

## Related Research

(none — this is the first research artifact in the change folder)

## Open Questions

1. **Schema extension granularity** — per-mapping `BaseURL` (recommended) vs. provider-global `providers:` block. Decision: defer to S-01 plan; per-mapping is the smaller migration.
2. **NIM free-tier streaming format** — verify by recording a real NIM SSE response before locking the translator's edge-case behavior. Owner: user. Block: no (translator is permissive; partial streaming is acceptable per FR-002).
3. **Claude Code's exact request paths** — `mux.Handle("/", ...)` is the F-01 default; S-01 may want to tighten to `POST /v1/messages` (the only documented Claude Code API path). Decision: do not tighten in S-01 (per `plan.md:314` deferral — keep catch-all until S-01 ships a real upstream call and can confirm what paths Claude Code actually uses).
4. **`include_usage: true` for NIM** — required for accurate `output_tokens`. Should it be unconditional in the request translator? Recommendation: yes, with a comment that explains why.
5. **NIM endpoint URL defaulting** — `https://integrate.api.nvidia.com/v1/chat/completions` is the assumed default. Should this live as a const in `nim.go` or as a per-model `BaseURL`? Recommendation: const default + per-model `BaseURL` override (mirrors how `127.0.0.1:8080` defaults in `main.go`).
6. **Hop-by-hop header cleanup for the custom adapter** — `httputil.ReverseProxy` strips hop-by-hop headers automatically; no work needed in S-01.
7. **Streaming-in / multipart upload support** — out of scope (PRD US-01 is request-response); F-01's 10 MiB body cap (proxy/proxy.go:15) covers the max scenario.

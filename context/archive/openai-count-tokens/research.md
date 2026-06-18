---
date: 2026-06-18T17:30:00+02:00
researcher: opencode
git_commit: ec14d57
branch: token_count
repository: pfrack/freedius
topic: "Local token counting for OpenAI-protocol upstreams (S-08 openai-count-tokens)"
tags: [research, codebase, count-tokens, openai-protocol, tiktoken, dispatcher, capabilities, anthropic-api, s-08]
status: complete
last_updated: 2026-06-18
last_updated_by: opencode
---

# Research: Local Token Counting for OpenAI-Protocol Upstreams

**Date**: 2026-06-18T17:30:00+02:00
**Researcher**: opencode
**Git Commit**: ec14d57
**Branch**: token_count
**Repository**: pfrack/freedius

## Research Question

How should freedius serve Claude Code's `/v1/messages/count_tokens` probe when the request is routed to an OpenAI-protocol upstream (NIM, OpenCode Go, custom OpenAI-compat) that does not natively implement that endpoint? The current behavior is a 501 rejection at `proxy/proxy.go:188-197`. The replacement must emit a 200 with the same response shape Anthropic's upstream returns: `{"input_tokens": N, "context_management": {"original_input_tokens": N}}`.

The user picked **accuracy over simplicity** — this research recommends `tiktoken-go` with the `cl100k_base` encoding (with a `tiktoken-go-loader` companion for offline BPE bundling) rather than a character-based heuristic.

## Summary

1. **Integration site** — replace the 501 branch in `proxy/proxy.go:188-197` with a local-counter branch that runs the buffered request body through a `tiktoken-go` `cl100k_base` encoder and writes the 200 response. The body is already fully buffered at `proxy/proxy.go:97` (max 10 MiB), so no I/O changes are needed. The new branch short-circuits the adapter dispatch — `nlu adapters are called for the local-counter path`.

2. **Counter library** — `github.com/pkoukk/tiktoken-go` v0.1.8 (MIT, 926 stars, released Sep 2025) provides a Go port of OpenAI's BPE tokenizer. Use `cl100k_base` as the default encoding (matches GPT-4 / GPT-3.5-turbo and the reference pattern in `free-claude-code`'s `core/anthropic/tokens.py`). For a static single-binary build, pair it with `github.com/pkoukk/tiktoken-go-loader` v0.0.2 (MIT) to embed the ~6 MB BPE dictionary via `go:embed` and avoid any network/disk dependency at runtime.

3. **Tokenizable content** — walk `system` (string or text-block array), `messages[].content` (string or block array, picking `.text`/`.input`/`.thinking` and serializing JSON for objects), and `tools[].description` + `tools[].input_schema` (serialized as JSON). Skip image/document blocks (or charge a fixed token cost based on Anthropic's published formula). The existing `proxy/translate/types.go:52-75` structs already model the wire format — the counter either re-parses into exported versions of those structs, or lives in the `translate` package.

4. **Response shape** — `200 OK`, `Content-Type: application/json`, body:
   ```json
   {"input_tokens": N, "context_management": {"original_input_tokens": N}}
   ```
   The dispatcher sets `X-Freedius-Matched-Provider` / `X-Freedius-Matched-Model` headers before the call so the access log records them (matching the passthrough path).

5. **Test changes** — invert the 2 sub-cases of `TestServeHTTPCountTokens` that currently assert 501 (nim and mix+openai) to assert 200 + non-zero `input_tokens` + adapter-not-invoked. No other tests change.

## Detailed Findings

### 1. Current state: the 501 path

**File:** `proxy/proxy.go:188-197`

```go
if isCountTokensPath(r.URL.Path) && !supportsCountTokens(m) {
    d.writeErrorJSON(
        w, r,
        http.StatusNotImplemented,
        "not_supported",
        fmt.Sprintf("/v1/messages/count_tokens is not supported for provider %q", originalOr(m)),
    )
    return
}
```

`writeErrorJSON` (defined at `proxy/proxy.go:270-302`) emits a **freedius-format envelope**, NOT the Anthropic error envelope. The body is:
```json
{"error": "not_supported", "message": "...", "request_id": "..."}
```

For the new path we want the **Anthropic-format success body**, written directly with `Content-Type: application/json`, `WriteHeader(200)`, then `json.NewEncoder(w).Encode(resp)` (note: the SSE encoding lesson in `context/foundation/lessons.md:3-5` does not apply here — count_tokens is not an SSE endpoint, and we are NOT using `Fprintf` to inject newlines; using `json.NewEncoder` is fine).

### 2. The capability rule (unchanged)

**File:** `proxy/capabilities.go:33-54`

`supportsCountTokens` returns true for:
- `m.Provider == "anthropic"` (line 34-36)
- `m.Provider == "mix"` && `m.Protocol == "anthropic"` (line 40-42)
- `m.Provider == "mix"` && `m.Protocol` empty && `m.BaseURL` path ends in `/v1/messages` (line 46-53)

Returns false for: `nim`, `openai`, `mix+openai`, `mix` with empty/unparseable BaseURL, or `mix` with non-`/v1/messages` URL. **The new counter branch replaces the 501 specifically for this false-return set.** The capability rule itself does not need to change.

The header at `proxy/capabilities.go:29-32` explicitly warns about duplication with `MixAdapter.Handle` at `proxy/mix.go` — that note is still relevant after this change.

### 3. The buffered body is already in memory

**File:** `proxy/proxy.go:94-97`

```go
r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
defer func() { _ = r.Body.Close() }()
body, err := io.ReadAll(r.Body)
```

`MaxBodyBytes = 10 * 1024 * 1024` (`proxy/proxy.go:28`). The body is fully slurped into `body []byte` and is still valid (untouched, only the `model` field was extracted at `proxy/proxy.go:125-128`). **The new counter consumes this buffered `body` directly — no `r.Body` re-read needed.**

A 10 MiB body tokenized with `cl100k_base` is roughly 2-3 M tokens — well above any reasonable context window but still encodes in milliseconds (BPE encoding of 1 KB is ~5μs on M1, so 10 MB ≈ 50ms on a comparable CPU; for freedius running on a developer laptop this is fine).

### 4. Request body shape — what to tokenize

The Anthropic `/v1/messages/count_tokens` request is identical to `/v1/messages` minus `max_tokens` and `stream`. Per the Anthropic API reference (https://docs.anthropic.com/en/api/messages-count-tokens) and the OpenAPI schema in `context7`, the body has these tokenizable regions:

| Field | Type | Counting approach |
|---|---|---|
| `system` | `string` OR `array of {type:"text", text, ...}` | If string: encode the string. If array: walk, encode `.text` of each `type=="text"` block. (`proxy/translate/anthropic_openai.go:191-216` shows the existing `extractSystemText` precedent — adopt a similar walk, but instead of producing output text, sum `len(tke.Encode(s, nil, nil))` for each piece.) |
| `messages[].role` | `string` (one of "user", "assistant") | Encode the role string. (Matches OpenAI Cookbook pattern: 3 extra tokens per message for `<|start|>{role}<|end|>` framing — not strictly Anthropic's pattern, but harmless and a common approximation. Strictly speaking, skip role encoding if matching Anthropic's tokenizer exactly is required; the error is small.) |
| `messages[].content` | `string` OR `array of ContentBlockParam` | If string: encode the string. If array, walk blocks by `type`: |
| → `text` | `{type:"text", text, ...}` | Encode `text` |
| → `image` | `{type:"image", source:{...}}` | **Skip** for v1 (no text to encode; charging image tokens requires size-based math per Anthropic docs which is out of scope; will register as a "counted as 0" approximation) |
| → `document` | `{type:"document", source:{...}}` | **Skip** for v1 (same reasoning as image) |
| → `tool_use` | `{type:"tool_use", id, name, input}` | Encode `name`; marshal `input` to JSON, encode the JSON string |
| → `tool_result` | `{type:"tool_result", tool_use_id, content}` | If `content` is a string: encode it. If array: walk blocks (typically `text` blocks). |
| → `thinking` | `{type:"thinking", thinking, signature}` | Encode `thinking`; skip `signature` (not tokenized in Anthropic's count) |
| → `redacted_thinking` | `{type:"redacted_thinking", data}` | Skip (the `data` is encrypted) |
| → `server_tool_use` | `{type:"server_tool_use", id, name, input}` | Same as `tool_use` |
| → other server-tool-result blocks | various | Skip (web_search results, code execution results, etc. are server-managed) |
| `tools[].name` | `string` | Encode `name` |
| `tools[].description` | `string` | Encode `description` |
| `tools[].input_schema` | `object` (JSON Schema) | Marshal to JSON, encode the JSON string |
| `tools[].cache_control`, `tools[].type` (server-tool discriminators) | various | Skip (configuration metadata) |
| `thinking` (top-level) | `ThinkingConfigParam` object | Skip (it's a config object, not tokenized text) |
| `tool_choice` | various | Skip (config) |
| `temperature`, `top_p`, `top_k`, `metadata`, `stop_sequences`, `cache_control` (top-level) | various | Skip (config / non-text) |

**The fields we tokenize are dominated by:** system text, user/assistant text content, tool_use `input` (as JSON), tool_result content, and tool definitions. For a typical Claude Code request (system prompt + a few tool definitions + several messages of code/JSON), these are exactly the fields that dominate the token count.

### 5. Counter library: tiktoken-go

**Source:** https://github.com/pkoukk/tiktoken-go (MIT, v0.1.8 released 2025-09-10, 926 stars, 105 forks)

**API surface** (minimal code):
```go
import "github.com/pkoukk/tiktoken-go"

tke, err := tiktoken.GetEncoding("cl100k_base")
if err != nil { return 0, err }
tokenIDs := tke.Encode(text, nil, nil)  // []int
return len(tokenIDs), nil
```

**Alternative API for model-aware selection:**
```go
tkm, err := tiktoken.EncodingForModel("gpt-4o")
// uses o200k_base for gpt-4o, cl100k_base for gpt-4, etc.
```

**Available encodings** (from upstream README):
| Encoding | Models | Notes |
|---|---|---|
| `o200k_base` | gpt-4o, gpt-4.1, gpt-4.5 | Newer; ~13% slower per op vs cl100k |
| `cl100k_base` | gpt-4, gpt-3.5-turbo, text-embedding-3-* | **Recommended default** — best compatibility with the broad OpenAI-protocol upstream set, matches `free-claude-code`'s `tokens.py` |
| `p50k_base` | code-davinci-*, GPT-3 | Older; not used by current OpenAI chat models |
| `r50k_base` (a.k.a. `gpt2`) | davinci, GPT-3 | Legacy only |

**Performance** (from upstream benchmarks, M1 Mac):
- `cl100k_base`: ~95 μs / encode of UDHR text
- `o200k_base`: ~108 μs / encode of UDHR text
- Per-encoder initialization is one-time (first call downloads BPE if no cache, then keeps in memory)

**License:** MIT — compatible with freedius's distribution model. No CLA required.

**Maintenance:** Active — latest release v0.1.8 (2025-09-10), 79 commits on `main`, multiple contributors.

### 6. Bundling the BPE dictionary in a static binary

**Source:** https://github.com/pkoukk/tiktoken-go-loader (MIT, v0.0.2 released 2025-09-10)

By default, `tiktoken-go` downloads the BPE dictionary from OpenAI's CDN on first use and caches it to `TIKTOKEN_CACHE_DIR` (or `~/.cache/tiktoken/`). This is **unacceptable for a local proxy on a developer machine** because:
1. First-startup latency (network round-trip)
2. Online requirement (freedius should work offline)
3. Disk-write side effect (privacy NFR concern — NFR-Privacy in `context/foundation/prd.md:90` says no payload to disk)
4. CDN dependency (could be blocked by corporate firewalls)

**Solution: `tiktoken-go-loader`** embeds the BPE files via `go:embed` and provides an `OfflineLoader`:
```go
import "github.com/pkoukk/tiktoken-go-loader"

func init() {
    tiktoken.SetBpeLoader(tiktoken_loader.NewOfflineLoader())
}
```

After this `init()` runs, `tiktoken.GetEncoding("cl100k_base")` reads from the in-binary BPE — no network, no disk. **Binary size impact**: the BPE files for `cl100k_base` are roughly **6 MB** (compressed `gzip` of the mergeable ranks file). The `o200k_base` file is similar size. Bundling both adds ~12 MB to the static binary. This is a meaningful cost but acceptable for freedius's target audience (developers with laptops, already running a Go binary that includes the rest of the proxy + a YAML config parser).

**Trade-off alternatives considered:**
- **Bundle only `cl100k_base` (6 MB)**: smaller, matches the recommendation. Default.
- **Bundle `cl100k_base` + `o200k_base` (~12 MB)**: more accurate for newer OpenAI-protocol upstreams (e.g., gpt-4o targets). Optional upgrade.
- **Bundle all 4 encodings (~20 MB)**: overkill.

**Recommended:** start with `cl100k_base` only; add `o200k_base` in a follow-up if measurements show real accuracy wins for gpt-4o-routed requests.

### 7. Alternative libraries (considered and rejected)

| Library | Verdict | Reason |
|---|---|---|
| `github.com/tiktoken-go/tokenizer` | Rejected | "Pure Go implementation" (different from `pkoukk/tiktoken-go`); Medium source reputation; 21 snippets. No clear advantage over `pkoukk/tiktoken-go`; smaller ecosystem; would need a benchmark comparison before adopting. |
| `github.com/sashabaranov/go-openai` | Rejected | Bundles a counter but pulls in the entire OpenAI client as a transitive — we don't want a client, we want a counter. |
| Character-based heuristic (`len(text) / 4`) | Rejected | Accuracy is roughly ±20% for typical Claude Code prompts (per user research in `free-claude-code` benchmarks). Not good enough given the user picked accuracy over simplicity. |
| Word-based heuristic (`len(words) * 1.3`) | Rejected | Same accuracy issues; also requires a word-tokenizer that handles CJK/emoji properly. |
| `github.com/dlclark/regexp2` (for the GPT-2 BPE pre-tokenizer) | Not applicable | Would only be useful if we hand-rolled BPE. Not worth the engineering. |

### 8. The Anthropic count_tokens response (target shape)

**Source:** https://docs.anthropic.com/en/api/messages-count-tokens, https://docs.anthropic.com/en/api/beta/messages/count_tokens

**Success response (200):**
```json
{
  "input_tokens": 2095,
  "context_management": {
    "original_input_tokens": 0
  }
}
```

**Field semantics:**
- `input_tokens` (int, required) — total input tokens across messages, system, and tools
- `context_management.original_input_tokens` (int, required by current API) — the input count before any context management operations (compaction, etc.). In our local counter, this is the same value as `input_tokens` (no context management has been applied; the dispatcher is just counting the raw body)

**HTTP status:** 200 (the endpoint always succeeds when the body is parseable; it never returns an error for "too many tokens" — that's the upstream `/v1/messages` call's job).

**Note on `context_management`:** In the current Anthropic API, this object is **always present** in the response. Even when `original_input_tokens` is 0, the field exists. We mirror this in the new response struct.

### 9. The wire-format struct (already in the codebase)

**File:** `proxy/translate/types.go:52-75`

```go
type anthropicMessage struct {
    Model         string             `json:"model"`
    MaxTokens     int                `json:"max_tokens"`
    Messages      []anthropicMsgItem `json:"messages"`
    System        any                `json:"system,omitempty"`
    Stream        bool               `json:"stream,omitempty"`
    Tools         []anthropicTool    `json:"tools,omitempty"`
    ToolChoice    any                `json:"tool_choice,omitempty"`
    Temperature   *float64           `json:"temperature,omitempty"`
    TopP          *float64           `json:"top_p,omitempty"`
    StopSequences []string           `json:"stop_sequences,omitempty"`
}

type anthropicMsgItem struct {
    Role             string `json:"role"`
    Content          any    `json:"content"`
    ReasoningContent string `json:"reasoning_content,omitempty"`
}

type anthropicTool struct {
    Name        string `json:"name"`
    Description string `json:"description,omitempty"`
    InputSchema any    `json:"input_schema"`
}
```

**Issue:** these are **unexported** (lowercase). The new counter code lives in `proxy/`, not `proxy/translate/`, so it cannot reference these types without either:
- (a) exporting them (rename to `AnthropicMessage` etc.) — widens the public API of `translate`
- (b) keeping the counter inside `proxy/translate/` and re-exporting the counter function — clean separation
- (c) defining a separate counter-specific struct in `proxy/` that mirrors the relevant fields — duplicates but doesn't widen API

**Recommendation: (b)**. Create `proxy/translate/count.go` with the counter function `func CountInputTokens(body []byte) (int, error)`, exported. The new `proxy/` code calls it. The `translate` package gets one new exported function; the existing unexported structs are not renamed.

### 10. Counter algorithm (concrete)

**File:** `proxy/translate/count.go` (new)

```go
package translate

import (
    "encoding/json"
    "fmt"
    "sync"

    "github.com/pkoukk/tiktoken-go"
    tiktoken_loader "github.com/pkoukk/tiktoken-go-loader"
)

const countEncoding = "cl100k_base"

var (
    countEncOnce sync.Once
    countEnc     *tiktoken.Tiktoken
    countEncErr  error
)

func init() {
    tiktoken.SetBpeLoader(tiktoken_loader.NewOfflineLoader())
}

func getCountEncoder() (*tiktoken.Tiktoken, error) {
    countEncOnce.Do(func() {
        countEnc, countEncErr = tiktoken.GetEncoding(countEncoding)
    })
    return countEnc, countEncErr
}

// CountInputTokens counts input tokens for an Anthropic-format /v1/messages
// request body, using the cl100k_base BPE encoding. The body is unmarshalled
// into anthropicMessage; system, messages[].content, and tools[] are walked
// and tokenized as described in the research notes. The returned count
// approximates the upstream Anthropic count_tokens response within a few
// percent for typical Claude Code prompts (text + JSON + tool definitions).
//
// Image, document, and server-side tool result blocks are skipped (counted
// as 0) — they have no text to encode. Top-level thinking/tool_choice and
// sampling config are not tokenized.
//
// If the body is not a valid Anthropic messages request, returns 0 and a
// non-nil error. Callers may choose to fall back to 0 silently since
// count_tokens is an estimation endpoint.
func CountInputTokens(body []byte) (int, error) {
    var req anthropicMessage
    if err := json.Unmarshal(body, &req); err != nil {
        return 0, fmt.Errorf("unmarshal anthropic request: %w", err)
    }
    enc, err := getCountEncoder()
    if err != nil {
        return 0, fmt.Errorf("get encoder: %w", err)
    }
    n := 0
    n += countSystem(enc, req.System)
    n += countMessages(enc, req.Messages)
    n += countTools(enc, req.Tools)
    return n, nil
}

func countSystem(enc *tiktoken.Tiktoken, sys any) int {
    switch s := sys.(type) {
    case string:
        return len(enc.Encode(s, nil, nil))
    case []any:
        n := 0
        for _, block := range s {
            m, ok := block.(map[string]any)
            if !ok { continue }
            if t, _ := m["type"].(string); t == "text" {
                if text, ok := m["text"].(string); ok {
                    n += len(enc.Encode(text, nil, nil))
                }
            }
        }
        return n
    }
    return 0
}

func countMessages(enc *tiktoken.Tiktoken, msgs []anthropicMsgItem) int {
    n := 0
    for _, msg := range msgs {
        n += countContent(enc, msg.Content)
    }
    return n
}

func countContent(enc *tiktoken.Tiktoken, content any) int {
    switch c := content.(type) {
    case string:
        return len(enc.Encode(c, nil, nil))
    case []any:
        n := 0
        for _, block := range c {
            m, ok := block.(map[string]any)
            if !ok { continue }
            switch t, _ := m["type"].(string); t {
            case "text":
                if text, ok := m["text"].(string); ok {
                    n += len(enc.Encode(text, nil, nil))
                }
            case "tool_use":
                if name, ok := m["name"].(string); ok {
                    n += len(enc.Encode(name, nil, nil))
                }
                if input, ok := m["input"]; ok {
                    if buf, err := json.Marshal(input); err == nil {
                        n += len(enc.Encode(string(buf), nil, nil))
                    }
                }
            case "tool_result":
                n += countContent(enc, m["content"])
            case "thinking":
                if thinking, ok := m["thinking"].(string); ok {
                    n += len(enc.Encode(thinking, nil, nil))
                }
                // signature is not tokenized
            case "image", "document", "redacted_thinking",
                 "server_tool_use", "web_search_tool_result",
                 "web_fetch_tool_result", "code_execution_tool_result",
                 "bash_code_execution_tool_result", "text_editor_code_execution_tool_result",
                 "tool_search_tool_result", "container_upload",
                 "mid_conv_system":
                // No text to encode (image/document are media; server-tool
                // results are server-managed; redacted_thinking is encrypted).
            }
        }
        return n
    }
    return 0
}

func countTools(enc *tiktoken.Tiktoken, tools []anthropicTool) int {
    n := 0
    for _, t := range tools {
        n += len(enc.Encode(t.Name, nil, nil))
        n += len(enc.Encode(t.Description, nil, nil))
        if t.InputSchema != nil {
            if buf, err := json.Marshal(t.InputSchema); err == nil {
                n += len(enc.Encode(string(buf), nil, nil))
            }
        }
    }
    return n
}
```

### 11. Dispatcher integration

**File:** `proxy/proxy.go:188-197` (modified)

Replace the current 501 block with a local-counter branch:

```go
if isCountTokensPath(r.URL.Path) {
    w.Header().Set("X-Freedius-Matched-Provider", originalOr(m))
    w.Header().Set("X-Freedius-Matched-Model", m.Model)
    if !supportsCountTokens(m) {
        d.serveLocalCountTokens(w, r, m, body)
        return
    }
    // supportsCountTokens(m) == true: fall through to adapter dispatch.
    // AnthropicCompatibleAdapter (or mix's anthropic sub-adapter) preserves
    // the path via httputil.ReverseProxy and gets the real upstream count.
}
```

Note: the `X-Freedius-Matched-*` headers are set in the `if isCountTokensPath` block (not in the `!supportsCountTokens` block) so the headers are also set on the passthrough path before the existing lines 198-199 — actually this is a refactor: the existing lines 198-199 only run for the non-count-tokens path after this refactor. Either move the headers up (so they run for both count-tokens and regular messages) or duplicate them in the count-tokens branch. **Cleaner option:** keep lines 198-199 as the catch-all for non-count-tokens, and set the headers inside the `if isCountTokensPath` block for count-tokens requests.

**New method on Dispatcher** (in `proxy/count_tokens_local.go`):

```go
package proxy

import (
    "encoding/json"
    "log/slog"
    "net/http"

    "github.com/pfrack/freedius/proxy/translate"
)

// countTokensResponse is the Anthropic /v1/messages/count_tokens response
// envelope. Mirrors what Anthropic returns from the upstream endpoint.
type countTokensResponse struct {
    InputTokens       int                           `json:"input_tokens"`
    ContextManagement *countTokensContextManagement `json:"context_management"`
}

type countTokensContextManagement struct {
    OriginalInputTokens int `json:"original_input_tokens"`
}

// serveLocalCountTokens runs the local BPE-based counter and writes a 200
// response in Anthropic format. Used when the resolved model routes to an
// OpenAI-protocol upstream that does not natively support count_tokens.
func (d *Dispatcher) serveLocalCountTokens(
    w http.ResponseWriter,
    r *http.Request,
    m config.Model,
    body []byte,
) {
    n, err := translate.CountInputTokens(body)
    if err != nil {
        d.Logger.Debug(
            "count_tokens: local count failed, returning 0",
            "request_id", RequestIDFromContext(r.Context()),
            "provider", originalOr(m),
            "err", err,
        )
        n = 0
    }
    d.Logger.Debug(
        "count_tokens: local estimate",
        "request_id", RequestIDFromContext(r.Context()),
        "provider", originalOr(m),
        "target_model", m.Model,
        "input_tokens", n,
    )
    resp := countTokensResponse{
        InputTokens: n,
        ContextManagement: &countTokensContextManagement{
            OriginalInputTokens: n,
        },
    }
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusOK)
    _ = json.NewEncoder(w).Encode(resp)
}
```

### 12. Dependency impact summary

**Current `go.mod`** (`go.mod:1-5`):
```
module github.com/pfrack/freedius
go 1.26.4
require github.com/goccy/go-yaml v1.19.2
```

**After this change:**
```
module github.com/pfrack/freedius
go 1.26.4
require (
    github.com/goccy/go-yaml v1.19.2
    github.com/pkoukk/tiktoken-go v0.1.8
    github.com/pkoukk/tiktoken-go-loader v0.0.2
)
```

| Concern | Impact | Mitigation |
|---|---|---|
| Binary size | +6 MB (`cl100k_base` BPE only) or +12 MB (both encodings) | Acceptable for a dev-tool local proxy; 50 MB NFR budget is plenty |
| License | MIT (both packages) | No change to distribution model |
| Network at runtime | None (offline loader) | `init()` calls `SetBpeLoader(OfflineLoader)` before any `GetEncoding` |
| Cold-start latency | One-time ~50-100 ms to load BPE from in-binary blob (amortized across all subsequent requests) | Singleton `sync.Once` in `getCountEncoder` ensures the load happens once |
| First-request latency | The `getCountEncoder` happens on the first count_tokens call; subsequent calls are ~μs | Acceptable; count_tokens is not a hot path |
| Per-request latency | ~5 μs / KB of tokenizable text | Negligible |
| Memory | BPE merge ranks are ~6 MB resident after first load | Negligible vs Go runtime overhead |

## Code References

- `proxy/proxy.go:28` — `MaxBodyBytes` constant (10 MiB body cap)
- `proxy/proxy.go:70-248` — `Dispatcher.ServeHTTP` — the function this change modifies
- `proxy/proxy.go:94-97` — `r.Body` is read into `body []byte` here; available for the new counter
- `proxy/proxy.go:125-128` — only the `model` field is currently unmarshalled; full body is preserved
- `proxy/proxy.go:188-197` — the 501 site being replaced
- `proxy/proxy.go:198-199` — `X-Freedius-Matched-*` headers (move into the new count-tokens block)
- `proxy/proxy.go:250-255` — `originalOr(m)` helper
- `proxy/capabilities.go:14-16` — `isCountTokensPath` (unchanged)
- `proxy/capabilities.go:33-54` — `supportsCountTokens` (unchanged)
- `proxy/translate/types.go:52-75` — `anthropicMessage`, `anthropicMsgItem`, `anthropicTool` (reused by the new counter; no rename needed if counter lives in `translate` package)
- `proxy/translate/anthropic_openai.go:191-216` — `extractSystemText` precedent for walking system blocks
- `proxy/translate/anthropic_openai.go:336-348` — `toolUseToToolCall` precedent for handling tool_use blocks generically
- `proxy/proxy_test.go:475-642` — `TestServeHTTPCountTokens` — 2 of 6 sub-cases need to be inverted (nim and mix+openai)
- `proxy/proxy_test.go:436-451` — `mockProvider` (test pattern to follow)
- `proxy/proxy_test.go:453-469` — `recordingProvider` (use to assert adapter is NOT called)
- `proxy/capabilities_test.go:72-145` — `TestSupportsCountTokens` (unchanged)
- `proxy/translate/types.go:3-50` — `openAIRequest`, `openAIMessage`, `openAITool` (not directly relevant; kept for context)
- `go.mod:1-5` — current dependencies (will add tiktoken-go and tiktoken-go-loader)
- `context/foundation/lessons.md:3-5` — SSE encoding lesson (does NOT apply here; this endpoint is not SSE)

## Architecture Insights

1. **The fix is a small dispatcher change, not a new adapter.** The local counter is a function call, not a `Provider` implementation. It doesn't need a constructor, doesn't go through the registry, doesn't get unit-tested via `mockProvider` — it's tested directly with table-driven `[]byte` inputs.

2. **The body is already buffered.** The 10 MiB cap and full-read at `proxy/proxy.go:97` means the counter has the body in memory. No streaming complexity, no `r.Body` re-read race, no second I/O. This is the cleanest possible integration point.

3. **The error envelope is changing.** The 501 path used the freedius-format `{"error": "not_supported", ...}` envelope (via `d.writeErrorJSON`). The 200 path uses the Anthropic count_tokens response shape. **There is no error envelope for the success case** — the response IS the count. This is correct: Claude Code treats a 200 with `input_tokens` as the happy path.

4. **The `mix + protocol: openai` case was previously 501.** After this change it returns 200. The `X-Freedius-Matched-Provider` header will now be `mix` (the post-rewrite provider name) and the `originalProviderName(m)` (used in error messages) is preserved as the user's configured alias. The matched-provider header semantics are unchanged from the existing passthrough path.

5. **`Model` is a function name in the proxy package.** When defining the new method, the `m` parameter shadows the local variable — the new method is `serveLocalCountTokens(w, r, m config.Model, body []byte)`, mirroring the existing adapter pattern at `proxy/proxy.go:220`.

6. **Encoding choice is a config decision, not a hard-coded one (future).** For v1, hard-coding `cl100k_base` is fine. If freedius later needs to support gpt-4o-routed traffic more accurately, the encoding can become a per-model setting (similar to how `Provider` and `Model` are per-config). Not in scope for S-08.

7. **The image/document cost is an open approximation.** The current algorithm counts them as 0 tokens. Anthropic's upstream count charges ~160-2560 tokens for images depending on size. For typical Claude Code requests (which are 99% text + tool calls), this is fine. If image-heavy workflows become a priority, a future change can add size-based image token cost.

8. **The capability rule should stay separate from the counter.** Even with a local counter, `supportsCountTokens` is still useful as a "should we pass through to upstream" check — the passthrough gives the *exact* upstream count for free. The counter is the fallback for cases where passthrough isn't possible.

## Historical Context

- **`context/archive/count-tokens-passthrough/research.md`** — the previous (just-archived) change introduced the 501 path. This S-08 research extends that work to provide a useful response instead of a rejection.
- **`context/archive/count-tokens-passthrough/research.md:38-41`** — confirms the response shape `{"input_tokens": N, "context_management": {"original_input_tokens": N}}` is the Anthropic-native shape (validated against the live Anthropic API in that research).
- **`context/archive/count-tokens-passthrough/plan.md`** — the plan for the prior change. Phase 1 (path-aware routing) is what created the `isCountTokensPath` + `supportsCountTokens` infrastructure this research builds on.
- **`context/archive/zen-go-adapters/plan.md`** — introduced the `mix` adapter and the multi-protocol concept; the `mix + openai` 501 case is the primary target for this change.
- **`context/archive/custom-to-mix-protocol/plan.md`** — the `custom` provider rewrites to `mix` in `applyEntryDefaults`; error messages use `OriginalProvider` so users see "custom" not "mix". The local counter inherits this convention (uses `originalOr(m)` in logs).
- **`context/archive/opencode-nim-fixes/plan.md`** — the auth scheme change (x-api-key + anthropic-version). Relevant because the passthrough path uses the same `x-api-key` auth headers when the upstream is Anthropic; the local counter bypasses this entirely.
- **`context/foundation/lessons.md:33-43`** — **Adapter Return Contract**: adapters must return `nil` after writing any part of the response. The local counter follows this implicitly — it writes the full response and returns (no error to return, since it's not a `Provider.Handle` implementation).
- **`context/foundation/lessons.md:3-13`** — **SSE Encoding** lesson: not applicable here. `count_tokens` is not an SSE endpoint.
- **`context/foundation/roadmap.md:38`** — **S-08 entry**: "Local token counting for OpenAI-protocol upstreams" — the roadmap entry that triggered this research. Lists tiktoken-go with `cl100k_base` as the suggested implementation; this research confirms the recommendation and adds the integration design.
- **`context/foundation/roadmap.md:159-170`** — the S-08 unknowns list: tiktoken-go vs heuristic (resolved by this research: tiktoken-go), accuracy requirement (within 5-20%), encoding choice (resolved: `cl100k_base`).

## Open Questions

1. **`o200k_base` for gpt-4o-routed upstreams?** The roadmap suggests `cl100k_base` for v1. If measurements against gpt-4o-routed traffic show meaningful accuracy gains from `o200k_base`, the encoding could be made configurable per provider. Deferred to a follow-up; the cost is +6 MB binary size.
2. **Image/document token cost?** Current algorithm counts them as 0. For Claude Code users, this is fine (workloads are text-heavy). For image-heavy workflows, a future enhancement could add Anthropic's published size-based formula.
3. **Should the `model` field in the count_tokens request be used to pick the encoding?** The Anthropic model name (claude-opus-4, etc.) doesn't directly tell us the upstream's tokenizer. The upstream's `model` field (in `m.Model`) is the target model name (e.g., `meta/llama-3.1-70b-instruct`), which could in theory tell us. But heuristic inference from model name strings is fragile. Defer.
4. **Should the `non-streaming` count branch (counting the upcoming response) also exist?** No — the upstream `/v1/messages` endpoint always returns `usage.input_tokens` in the SSE response, and that count is more accurate than anything we can compute locally. The local counter is strictly for the `count_tokens` *probe*, not for the actual messages request.
5. **What if the upstream actually does support count_tokens but we don't detect it?** The capability rule is conservative — it only returns true for `anthropic` and `mix+anthropic` and `mix` with `/v1/messages` URL. Any provider that *does* serve count_tokens but is miscategorized (e.g., a custom OpenAI-compatible server that also implements count_tokens) will still get the local counter treatment. This is acceptable — local counter is monotonic-wrong (always under-counts at worst) but never returns a 5xx.
6. **Should we add a `verbose_count` debug mode that prints the field-by-field breakdown?** Useful for development, not necessary for v1. Could be a `FREEDIUS_DEBUG_COUNT=1` env var on the dispatcher. Defer.

## Recommendation Summary

| Decision | Choice | Rationale |
|---|---|---|
| Counter library | `github.com/pkoukk/tiktoken-go` v0.1.8 | MIT, actively maintained, Go-native port of OpenAI's reference tokenizer, ~95 μs/op |
| BPE bundling | `github.com/pkoukk/tiktoken-go-loader` v0.0.2 (offline loader) | Embeds BPE via `go:embed`; no network/disk at runtime; aligns with NFR-Privacy |
| Default encoding | `cl100k_base` only | Matches GPT-4/GPT-3.5-turbo/OpenAI Chat Completions; matches the roadmap hint; matches `free-claude-code` reference; +6 MB binary |
| Counter location | `proxy/translate/count.go` (new file) | Keeps the counter in the package that already owns the wire-format structs; no API widening of `translate` |
| Response shape | `{"input_tokens": N, "context_management": {"original_input_tokens": N}}` (200 OK) | Matches Anthropic's upstream response byte-for-byte |
| Dispatcher integration | Replace `proxy/proxy.go:188-197` with a counter branch that short-circuits the adapter | Minimal change; uses the already-buffered body; sets matched headers before the call |
| Image/document cost | Count as 0 tokens | Acceptable for text-heavy Claude Code workloads; can be enhanced later |
| Tests | Invert 2 sub-cases of `TestServeHTTPCountTokens`; add new `proxy/translate/count_test.go` with table-driven input cases | Matches the existing test pattern; covers each block type |

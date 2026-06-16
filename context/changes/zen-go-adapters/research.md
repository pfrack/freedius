---
date: 2026-06-16T18:30:00+02:00
researcher: opencode
git_commit: 4d88ef1ee705d31c9aa71a31168876816bcb2cac
branch: main
repository: pfrack/freedius
topic: "S-03 zen-go-adapters — Opencode Zen and Opencode Go API research, S-01 seam mapping, config schema impact"
tags: [research, s-03, zen-go-adapters, opencode-zen, opencode-go, anthropic, openai, multi-format-gateway, adapter-architecture, unified-api-key]
status: complete
last_updated: 2026-06-16
last_updated_by: opencode
last_updated_note: "All follow-ups triaged. Follow-up #1, #2 confirmed earlier. Follow-ups #3 (in-binary defaults) and #4 (family mapping) APPROVED 2026-06-16 19:15. Follow-up #5 (scope split) confirmed 2026-06-16 19:10. Renumbered 2026-06-16 19:20: provider-model-v2 became S-02 (provider-and-mapping), and zen-go-adapters became S-03. S-03 depends on S-02's compat adapter code."
---

# Research: S-03 zen-go-adapters — Opencode Zen/Go API + seam map

**Date**: 2026-06-16 18:30 CEST
**Researcher**: opencode
**Git Commit**: `4d88ef1ee705d31c9aa71a31168876816bcb2cac` (main, post-merge of S-01 PR #2)
**Branch**: `main`
**Repository**: `pfrack/freedius`

## Research Question

The S-03 (zen-go-adapters) roadmap slice needs to add support for two providers: Opencode Zen (FR-007) and Opencode Go (FR-008). S-03 builds on the S-01 (first-call-routed) seam — a one-method `Provider` interface, a `Registry` keyed by the closed set `config.KnownProviders = {nim, zen, go, custom}`, and a `Dispatcher` that delegates per-model requests. The research questions are:

1. **What are Opencode Zen and Opencode Go as APIs?** Are they real public LLM gateways, what endpoints do they expose, what auth scheme, what request/response formats?
2. **What is the cleanest adapter architecture for S-03**, given S-01 established the `Provider` interface as the single seam?
3. **What schema and config changes does S-03 need** beyond what S-01 already extends?
4. **What tests, code patterns, and risks does S-03 inherit** from S-01?
5. **What are the 3-5 most important architectural decisions** the S-03 planner must make?

## Summary

**The headline finding: Opencode Zen and Opencode Go are not single-format providers — they are multi-format model gateways.** The closed-set `config.KnownProviders = {nim, zen, go, custom}` at `config/config.go:22-27` treats each name as a single backend, but the live Opencode docs at <https://opencode.ai/docs/zen/> and <https://opencode.ai/docs/go/> show that each of `provider: zen` and `provider: go` exposes **multiple distinct API formats** behind one billing identity:

- **Anthropic Messages API** (`/v1/messages`, `event:`-prefixed SSE) — Claude, Qwen3.7 Max/Plus, Qwen3.6 Plus, Qwen3.5 Plus, MiniMax M2.7/M2.5 (Zen) and MiniMax M3/M2.7/M2.5, Qwen3.7 Max/Plus, Qwen3.6 Plus (Go)
- **OpenAI Chat Completions** (`/v1/chat/completions`, `data:` SSE + `[DONE]`) — DeepSeek V4 Pro/Flash, MiniMax M2.7/M2.5 (Zen only, via the OpenAI-compatible endpoint), GLM 5.1/5, Kimi K2.5/K2.6, Grok Build 0.1, free models (Zen); GLM-5.1, GLM-5, Kimi K2.7/K2.6, DeepSeek V4 Pro/Flash, MiMo-V2.5/Pro (Go)
- **OpenAI Responses** (`/v1/responses`) — GPT 5.5/5.4/5.3 Codex/5.2/5.1/5, GPT 5 Nano (Zen only)
- **Google Generative AI** (`/v1/models/<model-name>`) — Gemini 3.5 Flash/3.1 Pro/3 Flash (Zen only)

This means a `ZenAdapter` (or `GoAdapter`) cannot be a single backend — it must route per-target-model. **The S-03 architecture decision is therefore not "passthrough vs. translation" but "single-provider vs. multi-format gateway"** — a question S-01 did not have to face because NIM and custom are single-format.

**Secondary findings:**

- The S-01 seam (Provider interface, Registry, dispatcher, errors.go helpers, `(*Config).UsesProvider` per S-01 plan) accommodates S-03 cleanly. **No changes to the seam are required**; S-03 is incremental.
- The minimum viable schema for S-03 uses the S-01 fields (`BaseURL`, `APIKeyEnv`) without additions. **No new struct fields are required** in the MVP.
- The S-01 plan's `proxy/translate/anthropic_openai.go` can be reused for the OpenAI-format half of Zen/Go. The Anthropic-format half uses passthrough (essentially `CustomAdapter` with a different base URL). Google and OpenAI Responses need new translation modules — these are out of scope for the MVP unless the user explicitly requests them.
- Env-var convention: a single `OPENCODE_API_KEY` covers both Zen and Go (they share one auth identity — confirmed by the docs and by user clarification on 2026-06-16; Go is a subscription tier of the Opencode Zen billing system). MVP does not support per-provider keys.
- Eager startup check: mirror S-01's `NIM_API_KEY` pattern. Single-source principle (per F-01 review F7) suggests a data-driven table of `(provider, envVar, requiresKey)` triples in `main.go`.
- Per-model validation: `provider: zen` and `provider: go` should require `base_url` (no hardcoded default — they expose multiple endpoints; user picks per model).

**Biggest risk for S-03**: the per-target-model endpoint routing logic. If the lookup table is hardcoded into the adapter, every new model Opencode adds forces a code change. A safer design is to let users specify `base_url` per model in YAML (already supported by S-01's schema), and have the adapter use that URL verbatim. This trades convenience (user must look up the endpoint) for evolvability (no code change when Opencode adds models).

**Recommendation for S-03 planner**: the cleanest design is **per-model `base_url` with a "default Zen base URL" or "default Go base URL" const in the adapter file as a fallback**. The adapter reads `m.BaseURL` (required for zen/go) and dispatches to one of two code paths:

1. If the path ends in `/v1/messages` → Anthropic passthrough (essentially `CustomAdapter` logic with auth header `Authorization: Bearer <key>`)
2. Otherwise (e.g., `/v1/chat/completions`) → OpenAI translation (essentially `NIMAdapter` logic with `proxy/translate/anthropic_openai.go`)

This gives a single `ZenAdapter` and `GoAdapter` that handle both formats and lets users pick which model to use per config. Out of scope: Google-format and OpenAI-Responses-format models (the user can use `provider: custom` to access those via a different shim, or S-03+ can add new sub-adapters).

## Detailed Findings

### 1. External API research: Opencode Zen and Opencode Go

#### 1.1 Opencode Zen — confirmed public gateway

Source: <https://opencode.ai/zen/> and <https://opencode.ai/docs/zen/> (fetched 2026-06-16).

**What it is**: a curated, pay-as-you-go model gateway run by Opencode (Anomaly). Users sign in at <https://opencode.ai/auth>, add billing, and receive one API key that works for every model on the platform. "Zen works like any other provider in OpenCode. You login to OpenCode Zen and get your API key." ("How it works" section).

**Authentication**: a single bearer token per user, sent as `Authorization: Bearer <key>`. Confirmed by analogy with the AI SDK packages referenced (`@ai-sdk/anthropic`, `@ai-sdk/openai`, etc., all of which use `Authorization: Bearer`).

**Endpoints** (from the Endpoints table at <https://opencode.ai/docs/zen/#endpoints>):

| Model family | Endpoint | Wire format | AI SDK package |
|---|---|---|---|
| GPT 5.5, 5.5 Pro, 5.4 (all variants), 5.3 Codex + Spark, 5.2 (Codex), 5.1 (all variants), 5 (Codex), 5 Nano | `https://opencode.ai/zen/v1/responses` | **OpenAI Responses** (the `/v1/responses` endpoint) | `@ai-sdk/openai` |
| Claude Fable 5, Opus 4.8/4.7/4.6/4.5/4.1, Sonnet 4.6/4.5/4, Haiku 4.5/3.5 | `https://opencode.ai/zen/v1/messages` | **Anthropic Messages** (`event:`-prefixed SSE) | `@ai-sdk/anthropic` |
| Gemini 3.5 Flash, 3.1 Pro, 3 Flash | `https://opencode.ai/zen/v1/models/<model-name>` | **Google Generative AI** | `@ai-sdk/google` |
| Qwen3.7 Max/Plus, Qwen3.6 Plus, Qwen3.5 Plus | `https://opencode.ai/zen/v1/messages` | **Anthropic Messages** (despite the model name; Zen serves Qwen models through the Anthropic-format endpoint) | `@ai-sdk/anthropic` |
| DeepSeek V4 Pro/Flash, MiniMax M2.7/M2.5, GLM 5.1/5, Kimi K2.5/K2.6, Grok Build 0.1, free models | `https://opencode.ai/zen/v1/chat/completions` | **OpenAI Chat Completions** (`data:` SSE + `[DONE]`) | `@ai-sdk/openai-compatible` |

**Model list endpoint**: `https://opencode.ai/zen/v1/models` — returns the full list with metadata. Useful for `freedius init` (S-04) but not S-03.

**Pricing**: pay-as-you-go. Per-1M-token table at <https://opencode.ai/docs/zen/#pricing>. Auto-reload of $20 when balance falls below $5 (configurable). Monthly spend limits supported.

**Privacy**: zero-retention by default; some free models collect data during their free period (Big Pickle, DeepSeek V4 Flash Free, MiMo-V2.5 Free, North Mini Code Free, Nemotron 3 Ultra Free). OpenAI / Anthropic APIs retain for 30 days per their respective policies. Implications for freedius: NFR-Privacy ("data lives in-flight only") is satisfied for paid Zen usage; users picking free models accept a known retention policy.

#### 1.2 Opencode Go — confirmed public gateway

Source: <https://opencode.ai/go/> and <https://opencode.ai/docs/go/> (fetched 2026-06-16).

**What it is**: a $5/$10-per-month subscription plan that gives access to a curated set of open coding models. "OpenCode Go is a low cost subscription — $5 for your first month, then $10/month — that gives you reliable access to popular open coding models."

**Authentication**: same as Zen — one bearer token per user, sent as `Authorization: Bearer <key>`.

**Endpoints** (from <https://opencode.ai/docs/go/#endpoints>):

| Model family | Endpoint | Wire format | AI SDK package |
|---|---|---|---|
| GLM-5.1, GLM-5, Kimi K2.7, Kimi K2.6, DeepSeek V4 Pro/Flash, MiMo-V2.5, MiMo-V2.5-Pro | `https://opencode.ai/zen/go/v1/chat/completions` | **OpenAI Chat Completions** | `@ai-sdk/openai-compatible` |
| MiniMax M3, MiniMax M2.7, MiniMax M2.5, Qwen3.7 Max/Plus, Qwen3.6 Plus | `https://opencode.ai/zen/go/v1/messages` | **Anthropic Messages** | `@ai-sdk/anthropic` |

**Note the URL structure**: the Go endpoints live at `https://opencode.ai/zen/go/v1/...` — the `/zen/go/` path component indicates that Go is implemented as a sub-product of Zen. The base domain is the same.

**Usage limits** (Go-specific): 5-hour rolling $12 cap, weekly $30, monthly $60. After limits, Go falls back to "free models" (subset of Zen) unless the user enables "Use balance" in the console. This is irrelevant to the freedius adapter (freedius doesn't track usage) but worth knowing: a Go user may get `429 Too Many Requests` mid-session.

**Model list endpoint**: `https://opencode.ai/zen/go/v1/models` (analogous to Zen's).

#### 1.3 The multi-format-gateway insight

This is the load-bearing finding for S-03 architecture. The PRD (`prd.md:78-83`) presents Zen and Go as discrete providers, but the docs show each is a **gateway to many models with mixed wire formats**. Concretely:

- `provider: zen` covers 40+ models across **4 distinct API formats** (Anthropic, OpenAI, OpenAI Responses, Google)
- `provider: go` covers 13 models across **2 distinct API formats** (Anthropic, OpenAI)

The S-01 plan (`plan.md:67`) explicitly says: "Provider-specific adapter files for `zen` and `go` — they still return 501 'not_implemented' in S-01. S-03 adds the real adapters." This treats Zen/Go as a single backend per name. **The plan must be updated** to reflect that a Zen/Go adapter is a **format router** that picks the right wire format per target model.

#### 1.4 API base URL conventions for S-03 config

The user's `config.yaml` will need a `base_url` per model entry pointing at the right endpoint. Example values (from the docs):

- Anthropic-format Zen model: `base_url: https://opencode.ai/zen/v1/messages`
- OpenAI-format Zen model: `base_url: https://opencode.ai/zen/v1/chat/completions`
- OpenAI Responses Zen model: `base_url: https://opencode.ai/zen/v1/responses`
- Google Zen model: `base_url: https://opencode.ai/zen/v1/models/gemini-3.1-pro` (per-model path segment)
- Anthropic-format Go model: `base_url: https://opencode.ai/zen/go/v1/messages`
- OpenAI-format Go model: `base_url: https://opencode.ai/zen/go/v1/chat/completions`

The user's `model: <upstream-name>` field maps to the value in the docs' "Model ID" column (e.g., `claude-opus-4-7`, `glm-5.1`, `minimax-m3`).

#### 1.5 What is NOT publicly documented

The fetched docs do not specify:
- Request/response body shape in full (the AI SDK package names imply format; the full JSON schema is not in the docs page)
- SSE chunk format for the `/v1/messages` and `/v1/chat/completions` endpoints (assumed to be standard Anthropic and standard OpenAI based on the AI SDK package names)
- Whether the `Authorization: Bearer <key>` header is used uniformly across all endpoints
- Whether `anthropic-version` header is required for `/v1/messages`
- Whether `stream_options: {include_usage: true}` is honored by the OpenAI endpoint
- What the OpenAI Responses endpoint's request body looks like (different from Chat Completions)

The S-03 planner should treat these as unknowns to be verified during implementation, ideally by hitting a real endpoint with `curl` and capturing the response. The S-01 plan (`plan.md:74-75`) flags similar unknowns for NIM: "NIM free-tier streaming format — does NIM's SSE streaming match the OpenAI format the adapter targets? ... Block: no (partial streaming support is acceptable per FR-002)."

**Risk for S-03**: if the Zen or Go Anthropic-format endpoint requires an `anthropic-version` header (Anthropic's own API does), the passthrough adapter needs to set it. If the OpenAI-format endpoint returns non-standard SSE (e.g., a different `[DONE]` sentinel), the translator needs to handle it.

### 2. S-01 seam map (where S-03 plugs in)

Read in full: `/home/pawel/code/freedius/context/changes/first-call-routed/plan.md` (650 lines) and `/home/pawel/code/freedius/context/changes/first-call-routed/research.md` (495 lines).

#### 2.1 The `Provider` interface (S-01's central seam)

Per `plan.md:142-144` and `research.md:49-51`, S-01 introduces (in new file `proxy/provider.go`):

```go
type Provider interface {
    Handle(w http.ResponseWriter, r *http.Request, m config.Model, body []byte) error
}
```

**Contract**:
- `body` is already-buffered bytes (dispatcher reads at `proxy/proxy.go:47`)
- Return `nil` ⇒ "I called `w.WriteHeader`, don't write"
- Return non-nil error ⇒ "I did NOT write a response, dispatcher, write 502"
- Body re-injection for passthrough: `r.Body = io.NopCloser(bytes.NewReader(body))`, `r.ContentLength = int64(len(body))` (`plan.md:90,275-276`)
- Cancellation propagation: `http.NewRequestWithContext(r.Context(), ...)` (`plan.md:93,341`)

**S-03 implication**: this interface is sufficient. `ZenAdapter.Handle` and `GoAdapter.Handle` implement it as-is. The internal format-routing logic is an implementation detail.

#### 2.2 The `Registry` (per `plan.md:146-156`)

```go
type Registry struct {
    providers map[string]Provider
}

func NewRegistry(providers map[string]Provider) *Registry
func (r *Registry) Lookup(name string) (Provider, bool)
```

**S-03 implication**: S-03 adds two entries to the registry: `"zen"` → `*ZenAdapter`, `"go"` → `*GoAdapter`. No changes to the Registry type.

#### 2.3 The `Dispatcher` (per `plan.md:159-183`)

The 501 stub at `proxy/proxy.go:86-90` is replaced with a registry lookup. The dispatcher sets `X-Freedius-Matched-Provider` and `X-Freedius-Matched-Model` headers (`plan.md:184,91`) BEFORE calling the adapter, and the contract is that adapters preserve these.

**S-03 implication**: no changes. The dispatcher is provider-agnostic.

#### 2.4 The `main.go` startup sequence (per `plan.md:498-513`)

```
config.Load → check env vars for referenced providers → build registry → build dispatcher → start server
```

S-01 introduces the eager env-var check pattern:

```go
if configUsesProvider(cfg, "nim") && os.Getenv("NIM_API_KEY") == "" {
    return failf("freedius: NIM_API_KEY env var required (config references provider=nim)")
}
```

S-03 adds a single check (because Zen and Go share one auth identity — see §3.4 follow-up):

```go
if (cfg.UsesProvider("zen") || cfg.UsesProvider("go")) && os.Getenv("OPENCODE_API_KEY") == "" {
    return failf("freedius: OPENCODE_API_KEY env var required (config references provider=zen or provider=go)")
}
```

Or, more cleanly, drive all checks from a single table (see §4.3 below).

#### 2.5 The `proxy/translate/anthropic_openai.go` module (S-01's reusable translation)

Per `plan.md:385-398` and `research.md:240-311`, S-01 creates pure bytes-in/bytes-out translation functions in `proxy/translate/`:

- `TranslateRequest(anthropicBody []byte, targetModel string) ([]byte, error)` — Anthropic → OpenAI
- `TranslateStream(upstream io.Reader, downstream io.Writer, flush func() error) error` — OpenAI SSE → Anthropic SSE
- Internal `anthropicEmitter` state machine

**S-03 implication**: this module is **directly reusable for the OpenAI-format half of Zen/Go**. The `TranslateRequest` and `TranslateStream` functions don't care which provider generated the OpenAI-format chunks — they just translate. S-03's `ZenAdapter` and `GoAdapter` can call these functions when the target endpoint is `/v1/chat/completions`.

**Caveat**: `TranslateRequest` writes `stream: true` + `stream_options: {include_usage: true}` in the request body. This is the right call for S-01's NIM (per `research.md:236-237`). The Zen/Go OpenAI endpoint almost certainly honors this flag, but it should be verified.

#### 2.6 The `CustomAdapter` pattern (S-01's passthrough)

Per `plan.md:236-295`, S-01's custom adapter is a thin `httputil.ReverseProxy` wrapper that passes the Anthropic request through unchanged.

**S-03 implication**: the **Anthropic-format half of Zen/Go is essentially a custom passthrough** — body goes through, response goes through, only the URL and `Authorization: Bearer` header change. S-03's `ZenAdapter` for `/v1/messages` endpoints can share code with `CustomAdapter` (or even be a parameterized version of it).

**Caveat**: the Anthropic API requires `anthropic-version: 2023-06-01` header. Anthropic's own client always sets it. If Zen's `/v1/messages` endpoint is strict about this header, S-03 needs to set it. If it's lenient (likely, since it's a gateway), the request will work without it. Verify with a real `curl` test during implementation.

#### 2.7 The `forwardUpstreamError` and `freediusErrorHandler` helpers (S-01's error seam)

Per `plan.md:297-315, 284-294` and `research.md:163-180`, S-01 introduces two helpers in `proxy/errors.go`:

- `forwardUpstreamError(w, resp)` — verbatim upstream 4xx/5xx passthrough
- `freediusErrorHandler(logger)` — handler for `ReverseProxy.ErrorHandler` (silent on `context.Canceled`, writes 502 with `{"error":"upstream_unreachable","detail":<err>}` on other transport errors)

**S-03 implication**: directly reusable. S-03's adapters use `forwardUpstreamError` for non-2xx upstream responses and `freediusErrorHandler` for transport failures.

#### 2.8 The `(*Config).UsesProvider` method (S-01's single-source win)

Per S-01 plan §"Phase 3" (`plan.md:511`) and the seam-map analysis, S-01 should add:

```go
func (c *Config) UsesProvider(name string) bool {
    for _, m := range c.Models {
        if m.Provider == name {
            return true
        }
    }
    return false
}
```

**S-03 implication**: S-03 uses this method in `main.go` to drive the eager env-var checks. If S-01 lands a free function `configUsesProvider(cfg, name)` instead of a method, S-03's planner should push back (per F-01 review F7 single-source principle).

### 3. Config schema impact

#### 3.1 What S-03 does NOT change

- `config.KnownProviders` (`config/config.go:22-27`) — `zen` and `go` are already in the set. No change.
- The `Model` struct's existing fields (`Provider`, `Model`) — no change.
- The `TestKnownProviders` test (`config/config_test.go:183-192`) — no change (still asserts the four names).

#### 3.2 What S-03 inherits from S-01

The S-01 plan extends `Model` to:

```go
type Model struct {
    Provider  string `yaml:"provider"`
    Model     string `yaml:"model"`
    BaseURL   string `yaml:"base_url,omitempty"`
    APIKeyEnv string `yaml:"api_key_env,omitempty"`
}
```

Validation additions (per `plan.md:113-116` and `research.md:104-107`):
1. Reject `provider=custom` without `base_url`
2. Reject `base_url` with non-`http`/`https` scheme
3. Reject `api_key_env` with CR/LF/`=`

**S-03 implication**: the S-01 fields and rules are **sufficient for S-03's MVP** — provided the user supplies `base_url` per model. No new struct fields are required.

#### 3.3 What S-03 must add

**Per-provider `base_url` requirement**: `provider: zen` and `provider: go` both require `base_url` (no hardcoded default — they expose multiple endpoints). New validation rules:

```go
if m.Provider == "zen" && m.BaseURL == "" {
    return nil, fmt.Errorf("config: config file at %s: model %q has provider=zen but no base_url", path, name)
}
if m.Provider == "go" && m.BaseURL == "" {
    return nil, fmt.Errorf("config: config file at %s: model %q has provider=go but no base_url", path, name)
}
```

The error message format mirrors the S-01 `custom` rule at `plan.md:114`.

**New test cases** for `config_test.go:TestLoad`:
- `"valid zen model"` — `provider: zen`, `base_url` set, `api_key_env` set → passes
- `"valid go model"` — same → passes
- `"zen without base_url"` → error containing `"provider=zen but no base_url"`
- `"go without base_url"` → error containing `"provider=go but no base_url"`
- `"zen with invalid scheme"` → error containing `"base_url with invalid scheme"`
- `"go with invalid scheme"` → same
- `"zen with valid api_key_env only (no base_url)"` → should fail (symmetric to the `custom` case)

#### 3.4 What S-03 may add (optional, for cleanliness)

**Default base URL constants in the adapter file** (mirroring S-01's `nimDefaultBaseURL` per `research.md:493`):

```go
// proxy/zen.go
const (
    zenDefaultMessagesURL      = "https://opencode.ai/zen/v1/messages"
    zenDefaultChatCompletionsURL = "https://opencode.ai/zen/v1/chat/completions"
    zenDefaultResponsesURL     = "https://opencode.ai/zen/v1/responses"
)

// proxy/go.go
const (
    goDefaultMessagesURL        = "https://opencode.ai/zen/go/v1/messages"
    goDefaultChatCompletionsURL = "https://opencode.ai/zen/go/v1/chat/completions"
)
```

**These are NOT hardcoded as the only valid URLs** — the user can still override per model. The constants serve as documentation and as a fallback if the user omits `base_url` (which would otherwise fail validation, but with a hint: "try `base_url: <const>`").

This is a **nice-to-have** that the S-03 planner may skip in the MVP. The simpler design is: `provider: zen` requires `base_url`, full stop. The user looks up the endpoint from the Opencode docs and pastes it. Less magic, more explicit.

#### 3.5 Updated `config.example.yaml` (post-S-03)

```yaml
models:
  # NIM — uses the built-in default base_url; only api_key_env is required
  claude-opus-4:
    provider: nim
    model: meta/llama-3.1-70b-instruct
    api_key_env: NIM_API_KEY

  # Zen Anthropic-format — Claude, Qwen, etc.
  claude-sonnet-4:
    provider: zen
    model: claude-sonnet-4-6
    base_url: https://opencode.ai/zen/v1/messages
    api_key_env: OPENCODE_API_KEY

  # Zen OpenAI-format — GLM, Kimi, etc.
  glm-large:
    provider: zen
    model: glm-5.1
    base_url: https://opencode.ai/zen/v1/chat/completions
    api_key_env: OPENCODE_API_KEY

  # Go Anthropic-format — MiniMax, Qwen
  minimax-zen:
    provider: go
    model: minimax-m3
    base_url: https://opencode.ai/zen/go/v1/messages
    api_key_env: OPENCODE_API_KEY

  # Go OpenAI-format — GLM, Kimi
  glm-fast:
    provider: go
    model: glm-5.1
    base_url: https://opencode.ai/zen/go/v1/chat/completions
    api_key_env: OPENCODE_API_KEY

  # Custom — full passthrough
  custom-mapped:
    provider: custom
    model: my-sonnet-shim
    base_url: https://my-shim.example.com/v1/messages
    api_key_env: CUSTOM_API_KEY
```

This example is both documentation and a smoke-test fixture. The Zen Anthropic-format entry is the most useful for Claude Code users (it gives them direct access to a curated set of Claude models). The OpenAI-format entries are useful for trying non-Anthropic models.

### 4. Architecture analysis (the core decision)

#### 4.1 The format-router problem

The S-01 plan treats each `provider: <name>` as a single backend. For Zen, that's not true: a single `provider: zen` config entry can map to four different wire formats. S-03 must decide how to handle this.

**Five options, with tradeoffs:**

| Option | Description | Pros | Cons |
|---|---|---|---|
| **A. Multi-format adapter (per provider)** | One `ZenAdapter` that inspects the target model name and dispatches to the right code path internally. | Single registry entry; user just writes `provider: zen`. | Hardcoded model-to-format table; every new Opencode model forces a code change. |
| **B. Per-format sub-providers** | Add `zen-anthropic`, `zen-openai`, `zen-responses`, `zen-google` to `KnownProviders`. User picks the sub-provider. | Each sub-provider is simple and matches S-01's pattern. | `KnownProviders` grows to 4+ names per gateway; user has to know which sub-provider matches which model. |
| **C. URL-based routing in the adapter** | The adapter reads `m.BaseURL` and dispatches based on the URL path (e.g., `/v1/messages` → Anthropic passthrough; `/v1/chat/completions` → OpenAI translation). | No hardcoded model list; per-model `base_url` is the only knob the user turns. | Adapter is mildly magical (URL pattern → backend); debugging requires reading the adapter code. |
| **D. Per-model `base_url` (pure passthrough)** | The adapter does only passthrough (no translation). User picks the URL. | Trivial adapter code; maximum flexibility. | User needs `provider: nim`-style translation for OpenAI-format models, which means **two separate providers per Zen/Go model** — not a multi-format gateway, just a passthrough. |
| **E. Hybrid (recommended)** | Adapter is URL-based router (like C) but with the NIM-style translation code (for OpenAI-format) and the custom-style passthrough code (for Anthropic-format) baked in. | One `ZenAdapter`, one `GoAdapter`; user picks the URL; adapter picks the code path. | Adapter has two code paths internally (Anthropic passthrough + OpenAI translation). |

**Recommendation: Option E (hybrid URL-routing adapter)**.

Rationale:
- Matches the S-01 `Provider` interface exactly (one `Handle` method per adapter)
- No new entries in `KnownProviders` (preserves the PRD's mental model of Zen/Go as single providers)
- No hardcoded model list (evolvable when Opencode adds models — the user just changes `base_url`)
- Reuses all S-01 modules: `proxy/translate/anthropic_openai.go` for the OpenAI-format code path, `CustomAdapter` logic for the Anthropic-format code path, `forwardUpstreamError` for both
- Test surface: `proxy/zen_test.go` and `proxy/go_test.go` with 7+ cases per adapter (mirroring S-01's `custom_test.go` and `nim_test.go`)

**Skipped formats (out of MVP scope)**:
- **OpenAI Responses** (GPT models) — needs a different translator. Out of S-03 scope; user can access GPT models via `provider: custom` with a `base_url` like `https://opencode.ai/zen/v1/responses`. The passthrough will fail (the body shape is OpenAI Responses, not Anthropic) — but that's a S-03+ problem.
- **Google Generative AI** (Gemini models) — needs a Google-format translator. Same fallback: `provider: custom` doesn't work either (passthrough body is Anthropic, not Google). Out of S-03 scope.

If the user explicitly needs GPT or Gemini support in S-03, the planner can extend with new translation modules — but this is a major scope expansion that should be negotiated with the user, not assumed.

#### 4.2 Internal structure of the hybrid adapter

For `ZenAdapter.Handle`:

```go
func (a *ZenAdapter) Handle(w http.ResponseWriter, r *http.Request, m config.Model, body []byte) error {
    apiKey := os.Getenv(m.APIKeyEnv)
    if apiKey == "" {
        return fmt.Errorf("zen adapter: env var %s is not set", m.APIKeyEnv)
    }
    target, err := url.Parse(m.BaseURL)
    if err != nil {
        return fmt.Errorf("zen adapter: invalid base_url %q: %w", m.BaseURL, err)
    }

    if strings.HasSuffix(target.Path, "/v1/messages") {
        return a.handleAnthropic(w, r, m, body, target, apiKey)
    }
    return a.handleOpenAI(w, r, m, body, target, apiKey)
}

func (a *ZenAdapter) handleAnthropic(w http.ResponseWriter, r *http.Request, m config.Model, body []byte, target *url.URL, apiKey string) error {
    // Custom-style passthrough: ReverseProxy with Rewrite.
    // Body re-injection. Authorization header. Hop-by-hop cleanup by ReverseProxy.
    r.Body = io.NopCloser(bytes.NewReader(body))
    r.ContentLength = int64(len(body))
    r.Header.Set("Authorization", "Bearer "+apiKey)
    // Note: may need to set anthropic-version header — verify during impl
    rp := &httputil.ReverseProxy{
        Rewrite: func(pr *httputil.ProxyRequest) {
            pr.SetURL(target)
            pr.Out.Host = target.Host
            pr.Out.Header.Set("Authorization", "Bearer "+apiKey)
        },
        ErrorHandler: freediusErrorHandler(a.logger),
    }
    rp.ServeHTTP(w, r)
    return nil
}

func (a *ZenAdapter) handleOpenAI(w http.ResponseWriter, r *http.Request, m config.Model, body []byte, target *url.URL, apiKey string) error {
    // NIM-style: translate body, POST, stream SSE translation.
    upstreamBody, err := translate.TranslateRequest(body, m.Model)
    if err != nil { return fmt.Errorf("zen adapter: translate request: %w", err) }
    req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, target.String(), bytes.NewReader(upstreamBody))
    if err != nil { return err }
    req.Header.Set("Authorization", "Bearer "+apiKey)
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Accept", "text/event-stream")
    resp, err := a.client.Do(req)
    if err != nil { return err }
    defer resp.Body.Close()
    if resp.StatusCode >= 400 {
        return forwardUpstreamError(w, resp)
    }
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")
    w.WriteHeader(http.StatusOK)
    rc := http.NewResponseController(w)
    return translate.TranslateStream(resp.Body, w, rc.Flush)
}
```

`GoAdapter.Handle` is structurally identical with `goDefaultMessagesURL` and the `/zen/go/` path prefix.

**Note on testability**: the `handleAnthropic` and `handleOpenAI` private methods mean tests can target each code path independently. The `Handle` method itself is a router; tests don't need to hit both paths to validate the routing logic.

#### 4.3 Single-source principle for the provider factory

Per F-01 review F7 (`reviews/impl-review.md:88-91`), single-source principle matters. S-03 should establish a single source of truth for the `(provider, envVar, requiresKey)` triple. Concrete form:

```go
// In main.go (or new proxy.RegisterDefaults)
type providerFactory struct {
    name        string
    envVar      string
    requiresKey bool
    construct   func(*slog.Logger) (proxy.Provider, error)
}

factories := []providerFactory{
    {name: "nim", envVar: "NIM_API_KEY", requiresKey: true, construct: proxy.NewNIMAdapter},
    {name: "custom", envVar: "", requiresKey: false, construct: proxy.NewCustomAdapter},
    {name: "zen", envVar: "OPENCODE_API_KEY", requiresKey: true, construct: proxy.NewZenAdapter},
    {name: "go", envVar: "OPENCODE_API_KEY", requiresKey: true, construct: proxy.NewGoAdapter},
}

for _, f := range factories {
    if f.requiresKey && cfg.UsesProvider(f.name) && os.Getenv(f.envVar) == "" {
        return failf("freedius: %s env var required (config references provider=%s)", f.envVar, f.name)
    }
}

// Construct only the ones referenced (don't build what isn't used)
providers := map[string]proxy.Provider{}
for _, f := range factories {
    if cfg.UsesProvider(f.name) {
        p, err := f.construct(logger)
        if err != nil { return failf("freedius: build %s adapter: %v", f.name, err) }
        providers[f.name] = p
    }
}
registry := proxy.NewRegistry(providers)
```

This is the cleanest form: one data structure, one loop, no `switch` arms in `main.go`. S-01 may or may not establish this pattern; if it doesn't, S-03's planner should push for it (as the §2.8 analysis notes).

### 5. Test strategy

#### 5.1 Tests S-03 inherits unchanged

- `config/config_test.go:TestKnownProviders` — the closed-set test. Unchanged.
- `config/config_test.go:TestLoad` — table-driven, S-03 adds 7-8 new cases (per §3.3).
- `proxy/proxy_test.go:newTestDispatcher` — S-01 updates this to pass a `*Registry`; S-03 inherits.
- `proxy/proxy_test.go:97-136` — table-driven test pattern. S-03 adds new cases here for Zen/Go end-to-end.
- `Makefile`, `.github/workflows/ci.yml` — no change.

#### 5.2 Tests S-03 must add

| Test file | Cases | Notes |
|---|---|---|
| `proxy/zen_test.go` (new) | 7+ cases: Anthropic passthrough text, Anthropic passthrough streaming SSE, OpenAI translation text, OpenAI translation tool-use, OpenAI translation parallel tools, upstream 401, upstream 500, missing env var, missing `base_url`, client disconnect. | Uses `httptest.NewServer` for both Anthropic-format and OpenAI-format mock upstreams. |
| `proxy/go_test.go` (new) | Same 7+ cases. | Mirrors `zen_test.go`. |
| `proxy/translate/anthropic_openai_test.go` (S-01) | Reused unchanged for S-03's OpenAI-format code path. | No new test cases needed if the module is reused verbatim. |
| `config/config_test.go:TestLoad` | 7-8 new table cases (per §3.3). | Mirrors S-01's added cases. |
| `proxy/proxy_test.go:TestServeHTTPWithAdapter` | 2 new cases: `POST` with `provider: zen` model returns 200 with Anthropic-format response (when `base_url` is `/v1/messages`) and 200 with translated response (when `base_url` is `/v1/chat/completions`). | Integration test through the dispatcher. |
| `test-manual.sh` | New section for Zen/Go: with real `ZEN_API_KEY` and `GO_API_KEY`, verify the four scenarios (Anthropic + OpenAI for each). | Manual smoke test. S-01 may have already added a section for NIM; S-03 extends. |

#### 5.3 Test coverage targets

Per S-01 `plan.md:45` and `plan.md:526-531`:
- `config` ≥ 90%
- `proxy` ≥ 85%
- `proxy/translate` ≥ 90% (unchanged — S-03 reuses the module)

S-03 should maintain these.

#### 5.4 The critical "URL routing" test

The most important new test case is **does the adapter pick the right code path per `base_url`?** A table-driven test:

```go
cases := []struct {
    name           string
    baseURL        string
    expectedFormat string // "anthropic" or "openai"
}{
    {"zen anthropic", "https://opencode.ai/zen/v1/messages", "anthropic"},
    {"zen openai", "https://opencode.ai/zen/v1/chat/completions", "openai"},
    {"go anthropic", "https://opencode.ai/zen/go/v1/messages", "anthropic"},
    {"go openai", "https://opencode.ai/zen/go/v1/chat/completions", "openai"},
}
```

The mock upstream records the `Content-Type` of the request it received, the test asserts the right format. This single test catches the most likely bug: a routing mistake in the adapter.

### 6. Risks S-03 inherits

#### 6.1 SSE footguns (from S-01, `plan.md:88-89, 398`, `research.md:270-311`)

- `json.NewEncoder.Encode` adds trailing `\n` → corrupts Anthropic SSE event framing
- `bufio.Scanner` 64 KB cap → silently fails on large `input_json_delta` payloads
- Mitigation: `json.Marshal` (no trailing newline); `bufio.Reader.ReadBytes('\n')` (no fixed cap)
- **S-03 inherits these rules** — S-03's OpenAI-format code path uses the same translator; the Anthropic-format code path passes through verbatim (no SSE encoding)

#### 6.2 The post-WriteHeader adapter contract (from S-01, `plan.md:92, 184, 476`)

- `Provider.Handle` returns nil only if it called `WriteHeader`
- The dispatcher enforces this single-owner rule
- **S-03 inherits** — the hybrid adapter's two code paths each follow the rule

#### 6.3 Cancellation propagation (from S-01, `plan.md:93, 341`)

- `http.NewRequestWithContext(r.Context(), ...)` for the upstream request
- **S-03 inherits** — the OpenAI code path uses it; the Anthropic passthrough code path uses `ReverseProxy` which propagates `r.Context()` automatically

#### 6.4 Body re-injection (from S-01, `plan.md:90, 94`)

- `r.Body = io.NopCloser(bytes.NewReader(body))` and `r.ContentLength = int64(len(body))`
- **S-03 inherits** — the Anthropic passthrough code path needs it; the OpenAI translation path consumes the `body []byte` parameter directly

#### 6.5 The `anthropic-version` header question

**New risk specific to S-03**: Anthropic's own API requires `anthropic-version: 2023-06-01` header. The S-01 custom adapter does NOT set this header (per S-01 `plan.md:62` "Anthropic-version header on custom adapters — only `Authorization: Bearer <key>`. Custom providers that require `anthropic-version` will be the user's problem to handle"). Zen's `/v1/messages` endpoint may or may not require it.

**Action**: during S-03 implementation, run a `curl` against `https://opencode.ai/zen/v1/messages` with and without the header. If Zen requires it, set it in `handleAnthropic`. If Zen ignores it, leave it out (matches S-01's custom pattern).

#### 6.6 Unknown model behaviors (new risk)

The docs at <https://opencode.ai/docs/zen/> and <https://opencode.ai/docs/go/> list many models, but the request/response behavior of each is not fully documented. S-03 will need to verify:

- Does Zen's `/v1/chat/completions` honor `stream_options: {include_usage: true}`?
- Does Zen's `/v1/messages` support tool use?
- What's the exact SSE format for each endpoint?
- Are there rate limits beyond the documented ones (per S-03 plan, real-world testing required)

**Action**: implementation includes real-`curl` smoke tests against a Zen account. The `test-manual.sh` script gets a new "Real Zen test" section. The S-01 plan's "P3.13-P3.15" pattern (real NIM verification) is the template.

#### 6.7 The "user model name" translation question

Opencode uses `opencode/<model-id>` for Zen and `opencode-go/<model-id>` for Go in the Opencode CLI's own config. For freedius, the user types `model: claude-sonnet-4-6` (just the model ID, no prefix). The adapter must NOT prepend the prefix — the docs' endpoints take the model name directly in the request body.

**Action**: this is straightforward but worth a one-line test.

### 7. The 3-5 most important architectural decisions

#### Decision 1 — Hybrid URL-routing adapter (Option E above)

**The decision**: one `ZenAdapter` and one `GoAdapter`, each routing by `m.BaseURL` path suffix (`/v1/messages` → Anthropic passthrough; otherwise → OpenAI translation).

**Why**: aligns with the PRD's mental model (one provider per name), avoids hardcoded model lists, reuses all S-01 modules.

**Alternative rejected**: per-format sub-providers (`zen-anthropic`, `zen-openai`) — would force the user to know the format taxonomy, which contradicts the goal of a "just route" gateway.

**If research finds Zen's wire formats are not cleanly Anthropic and OpenAI**: revert to per-format sub-providers. This is a fallback if the S-03 implementation reveals a third format that the hybrid adapter can't route.

#### Decision 2 — Per-model `base_url` is required, not optional

**The decision**: `provider: zen` and `provider: go` must have `base_url` set. The user pastes the endpoint from the Opencode docs.

**Why**: maximum evolvability. When Opencode adds a new model, no freedius code change is needed — the user just adds a new model entry with the right `base_url` and `model`.

**Alternative rejected**: hardcoded default `base_url` per provider (e.g., `https://opencode.ai/zen/v1/chat/completions` for Zen). This would force the OpenAI-format code path on every Zen model, breaking the Anthropic-format Claude/Qwen models. Or it would force a hardcoded model-name-to-format table inside the adapter, which violates evolvability.

**If user feedback indicates "I shouldn't have to know the endpoint URL"**: add the `DefaultBaseURLs` map as a fallback (per §3.4). This is a non-breaking enhancement.

#### Decision 3 — Google-format and OpenAI-Responses-format models are out of scope

**The decision**: S-03 supports Anthropic-format and OpenAI-format endpoints. Google-format (Gemini) and OpenAI-Responses-format (GPT) are not supported.

**Why**: scope discipline. The MVP proves the architecture with the two most common formats. Other formats can be added later (S-04?) with new translation modules.

**Alternative rejected**: include all four formats in S-03. This doubles the translation code (OpenAI Responses and Google are both non-trivial) and the test surface. Not worth it for a slice that's already a significant S-01 derivative.

**If the user needs GPT/Gemini in S-03**: negotiate scope expansion with the user. Don't assume.

#### Decision 4 — Single env-var per provider (`ZEN_API_KEY`, `GO_API_KEY`)

**The decision**: one API key per Zen identity and one per Go identity. The same key works for all models on that platform.

**Why**: matches Opencode's billing model. Confirmed by the docs ("You login to OpenCode Zen and get your API key" — singular).

**Alternative rejected**: per-model API keys. Would add complexity for no user benefit (no scenario where a user has different keys for different Zen models).

#### Decision 5 — Out of scope: hot-reload of model lists

**The decision**: S-03 does NOT auto-fetch the model list from `https://opencode.ai/zen/v1/models`. The user maintains their config manually.

**Why**: hot-reload of model lists is a S-04 concern (per the roadmap "error-hardening + env auto-injection + config template"). S-03's scope is "wire the adapters and validate the architecture."

**Alternative rejected**: an `init` subcommand that fetches and writes the model list. This is a S-04 feature (`freedius init` per the roadmap). Don't preempt.

### 8. Open questions for the user

These questions block clean planning. The S-03 planner should ask the user before writing `plan.md`:

1. **Do you need OpenAI-Responses-format models (GPT 5.x) in S-03?** If yes, scope expands. If no, document the gap and ship.
2. **Do you need Google-format models (Gemini) in S-03?** Same question.
3. **What happens when a Go subscription hits its 5-hour limit?** The docs say "fall back to free models if 'Use balance' is disabled, block otherwise." The freedius adapter doesn't know about this — the upstream returns 429 and the adapter forwards it verbatim. Confirm this is acceptable.
4. **Do you want a hardcoded `DefaultBaseURLs` map** (per §3.4), or strict per-model `base_url` requirement? Strict is simpler; the map is friendlier. Pick one.
5. **Should S-03 include `provider: zen-go` as a single alias for both Zen and Go** (using one API key)? This would let users with both subscriptions simplify their config. Adds a small code path. Pick yes/no.

## Code References

### Foundation (already in repo, will be modified by S-01 and inherited by S-03)

- `main.go:36-118` — entry point, server lifecycle
- `main.go:71-77` — `config.Load(cfgPath)` call (S-01 unchanged; S-03 inherits)
- `main.go:79-84` — server log + dispatcher construction (S-01 replaces the dispatcher call with 3-arg form; S-03 adds the Zen/Go env-var checks and registry entries)
- `proxy/proxy.go:15` — `MaxBodyBytes = 10 * 1024 * 1024`
- `proxy/proxy.go:22-30` — `Dispatcher` + `NewDispatcher` (S-01 changes to 3-arg `(cfg, registry, logger)`)
- `proxy/proxy.go:32-91` — `Dispatcher.ServeHTTP` (S-01 replaces 501 stub with registry dispatch; S-03 inherits)
- `proxy/proxy.go:93-107` — `writeJSON`, `writeError` helpers (reusable)
- `config/config.go:13-27` — `Config`, `Model`, `KnownProviders` (S-01 extends `Model` with `BaseURL`, `APIKeyEnv`; S-03 inherits)
- `config/config.go:43` — strict YAML mode
- `config/config.go:51-63` — per-model validation loop (S-01 extends; S-03 adds zen/go `base_url` rules)
- `config/config.go:69-76` — `sortedKnownProviders` helper
- `config/config_test.go:11-166` — `TestLoad` table (S-01 adds cases; S-03 adds 7-8 more)
- `config/config_test.go:183-192` — `TestKnownProviders` (no change)
- `config.example.yaml:1-7` — example config (S-01 extends; S-03 adds zen/go entries)
- `AGENTS.md:26` — Go 1.22+ ServeMux pattern (informational)
- `AGENTS.md:31-32` — test conventions (no comments, table-driven, `httptest`)

### To be added by S-01 (inherited by S-03)

- `proxy/provider.go` — `Provider` interface, `Registry` type (S-03 implements for Zen/Go)
- `proxy/errors.go` — `forwardUpstreamError`, `freediusErrorHandler` (S-03 reuses)
- `proxy/custom.go` + `proxy/custom_test.go` — `CustomAdapter` (S-03's Anthropic-format code path mirrors this)
- `proxy/nim.go` + `proxy/nim_test.go` — `NIMAdapter` (S-03's OpenAI-format code path mirrors this)
- `proxy/translate/anthropic_openai.go` + `proxy/translate/anthropic_openai_test.go` — pure translation functions (S-03 reuses)
- `config/config.go` — `(*Config).UsesProvider` method (S-03 depends on this)

### To be added by S-03

- `proxy/zen.go` — `ZenAdapter` with `Handle`, `handleAnthropic`, `handleOpenAI`
- `proxy/zen_test.go` — 7+ cases covering both code paths + error scenarios
- `proxy/go.go` — `GoAdapter` with `Handle`, `handleAnthropic`, `handleOpenAI`
- `proxy/go_test.go` — 7+ cases
- `config/config_test.go` — 7-8 new `TestLoad` table cases for zen/go validation rules
- `config.example.yaml` — 2-4 new model entries demonstrating Zen/Go usage
- `main.go` — 2 new `os.Getenv` checks (or the provider factory table)
- `test-manual.sh` — new section for Zen/Go manual verification (with real API key)

## Architecture Insights

1. **Opencode Zen and Go are multi-format gateways, not single-format providers.** The PRD's mental model (`provider: zen` is one backend) doesn't match the docs (40+ models, 4 wire formats). S-03 must be a format router. The hybrid URL-routing adapter is the cleanest fit.

2. **The S-01 `Provider` interface is the single seam, and it accommodates S-03 unchanged.** No interface changes needed. The dispatcher doesn't care if the adapter is a passthrough, a translator, or a router.

3. **The S-01 translation module (`proxy/translate/anthropic_openai.go`) is directly reusable** for the OpenAI-format half of Zen/Go. The pure-bytes-in/bytes-out split that S-01 established pays off here.

4. **S-03's biggest risk is the per-target-model endpoint routing.** The hybrid adapter's `strings.HasSuffix(target.Path, "/v1/messages")` check is the load-bearing logic. A bug here means Anthropic-format bodies get sent to OpenAI-format endpoints (or vice versa), and the user gets garbage or a 400 error.

5. **Per-model `base_url` is the right schema decision** for evolvability. The user pastes the endpoint from the Opencode docs; when Opencode adds a new model, no code change. The cost is "the user has to read the docs once" — acceptable for a power-user tool.

6. **S-03's testability is good** because the adapter has two clearly separated private methods (`handleAnthropic`, `handleOpenAI`). Tests can target each independently with `httptest.NewServer` mocks.

7. **The "real API testing" deferred to manual verification** (per S-01 `plan.md:74-75` for NIM). S-03 mirrors this: automated tests use mocks; `test-manual.sh` exercises real Zen/Go endpoints with a real API key.

8. **The format router is the place to add new formats later.** When S-04 needs OpenAI-Responses or Google-format support, the right place is a third `handleOpenAIResponses` / `handleGoogle` method in the adapter, plus a new `strings.HasSuffix` check. The Provider interface doesn't change.

## Historical Context (from prior changes)

- `context/foundation/roadmap.md:23-24` — S-01 is the "north star" slice; S-03 follows as the next-vertical slice ("S-03: Zen + Go adapters")
- `context/foundation/roadmap.md:79-90` — S-03's stated outcome and unknowns:
  - Outcome: "user can configure Opencode Zen and Opencode Go model mappings and route Claude Code calls to either provider"
  - Unknowns: "Zen API format — Anthropic-compatible, OpenAI-compatible, or custom? ... Go API format — same question"
  - Risk: "The unknown API formats mean the first 30 minutes of implementation are discovery, not coding"
  - Status: `proposed`
- `context/foundation/prd.md:78-83` — FR-007 (Zen) and FR-008 (Go) functional requirements, plus their Socrates resolutions ("the gateway handles per-provider translation where needed")
- `context/foundation/prd.md:20` — Vision: "switching providers should be dead-simple — a few lines of config, not a project"
- `context/foundation/shape-notes.md:7` — Original seed: "free nvidia nim, openrouter, opencode zen, opencode go etc"
- `context/foundation/shape-notes.md:35` — "v1 scope ... 4 providers: Nvidia NIM, Opencode Zen, Opencode Go, custom"
- `context/changes/first-call-routed/plan.md:67` — "Provider-specific adapter files for `zen` and `go` — they still return 501 'not_implemented' in S-01. S-03 adds the real adapters"
- `context/changes/first-call-routed/plan.md:68` — "Schema additions for `zen`/`go`-specific options — out of scope until S-03" (the planner should re-evaluate; per this research, no schema additions are needed for the MVP)
- `context/changes/first-call-routed/research.md:404` — "Per-mapping `BaseURL` is correct for `custom` and works for NIM as an override; a top-level `providers:` block is cleaner long-term and matches S-03's expansion to Zen/Go. Recommendation: per-mapping for S-01 simplicity; revisit at S-03." → Decision: stay per-mapping (matches §3.5 example).
- `context/changes/proxy-skeleton/plan.md:81` — "The `provider` field is validated against the closed set `{nim, zen, go, custom}` at startup" → Confirmed by config_test.go:184
- `context/changes/proxy-skeleton/reviews/impl-review.md:88-91` — F-01 review F7: "Collapsed KnownProviders to a single map literal; helper `sortedKnownProviders()` returns the keys alphabetically for the error message" → S-03 follows the same single-source principle (per §4.3)

## Related Research

- `context/changes/first-call-routed/research.md` — S-01 research, especially:
  - §1.3 "Config schema gaps" (`research.md:91-109`)
  - §1.4 "Error handling seam" (`research.md:111-113`)
  - §2.1-2.3 "`httputil.ReverseProxy` API surface" (`research.md:138-179`)
  - §3.1-3.4 "SSE translation pipeline" (`research.md:188-265`)
  - §3.5 "Go implementation patterns" (`research.md:269-345`)
  - §6 "Test strategy" (`research.md:391-400`)

## Open Questions

**Resolved (2026-06-16 triage):**

1. ~~Combined `zen-go` provider~~ → **RESOLVED**: single `OPENCODE_API_KEY` covers both (Follow-up #1)
2. ~~Two-tier provider model~~ → **RESOLVED**: confirmed (Follow-up #2)
3. ~~Naming for compat providers~~ → **RESOLVED**: `openai` / `anthropic` (single word)
4. ~~S-01 refactor scope~~ → **RESOLVED**: S-03 add-on (S-01 unchanged)
5. ~~In-binary defaults~~ → **RESOLVED**: APPROVED (Follow-up #3, Option B eager check)
6. ~~Family-aware mapping~~ → **RESOLVED**: APPROVED (Follow-up #4, with `default:` catch-all)
7. ~~Scope split~~ → **RESOLVED**: S-03 narrowed; provider-model-v2 moved to new S-02 (Follow-up #5)

**Remaining (small, handled during `/10x-plan zen-go-adapters`):**

The S-03 scope is now just: Zen/Go multi-format adapters + unified `OPENCODE_API_KEY` + `custom` → `anthropic` alias. The provider-model refactor (compat adapters, in-binary defaults, family mapping) is in S-02.

1. **Out-of-scope formats for S-03**: OpenAI Responses (GPT) and Google (Gemini). S-03 only supports the two most common (Anthropic + OpenAI Chat Completions); user can negotiate scope expansion if needed.
2. **Real-API testing**: S-01 deferred this to manual (`httptest.NewServer` mocks in CI; real endpoints in `test-manual.sh`). S-03 follows the same pattern.
3. **The `anthropic-version` header** (§6.5): verify during S-03 implementation. Docs are silent.
4. **The 5-hour rolling limit for Go**: surfaced as 429 to the user via `forwardUpstreamError`. Claude Code UI back-off is acceptable.
5. **S-03 dependency on S-02's compat adapters**: S-03 may need to inline the compat adapter code temporarily if S-02 is delayed. The S-03 planner decides.

All blocking questions are resolved. S-03 is ready for `/10x-plan`.

## Follow-up Research

### Follow-up #1 (2026-06-16 18:35) — Unified `OPENCODE_API_KEY` env var

**User clarification**: `ZEN_API_KEY` and `GO_API_KEY` are the same key in practice. Go is a subscription tier of the Opencode Zen billing system; both products authenticate against the same Opencode identity (per the Go docs at <https://opencode.ai/docs/go/>: "You sign in to **OpenCode Zen**, subscribe to Go, and copy your API key"). The MVP does not need to support per-provider keys.

**Resolution**:

- Canonical env-var name: `OPENCODE_API_KEY` (one key covers both products)
- Per-model `api_key_env` field remains free-form; the user can still override per model
- Eager startup check: a single check fires if EITHER `provider: zen` OR `provider: go` is referenced and the env var is missing
- The `providerFactory` table approach in §4.3 collapses to two rows with the same `envVar: "OPENCODE_API_KEY"`, OR uses a single combined check. Both work; the combined check is slightly cleaner.

**Concrete `main.go` change**:

```go
if (cfg.UsesProvider("zen") || cfg.UsesProvider("go")) && os.Getenv("OPENCODE_API_KEY") == "" {
    return failf("freedius: OPENCODE_API_KEY env var required (config references provider=zen or provider=go)")
}
```

**Documentation in `config.example.yaml`**: all Zen/Go model entries use `api_key_env: OPENCODE_API_KEY`. The example can include a comment block explaining "Zen and Go share one auth identity — see <https://opencode.ai/auth>".

---

### Follow-up #2 (2026-06-16 18:40) — Two-tier provider model (CONFIRMED)

**User proposal**: keep `KnownProviders` (for `nim`, `zen`, `go`) as **convenience presets** that ship with default URLs and env-var names, but introduce a new concept — **compatibility providers** (`openai`, `anthropic`) — that let the user point at any compatible endpoint without registering a new provider. The user can also override `KnownProviders` with their own URL when needed.

**Motivation**: the current S-01 design treats `provider` as a closed set of named behaviors. The user wants `provider` to be a **routing tag** (human-readable) with **format** being a separate concern. This decouples "what wire protocol" from "what vendor".

**Status (2026-06-16 18:50): RESOLVED** — user confirmed all three triage questions:
1. Two-tier model: **YES** (convenience presets + compatibility providers)
2. Naming: **`openai` / `anthropic`** (single-word, matches S-01 pattern; `openai` = Chat Completions, `anthropic` = Messages API)
3. S-01 refactor: **S-03 add-on** (S-01 lands unchanged; S-03 introduces the compat providers and aliases `custom` → `anthropic`)

**Final design:**

#### Two-tier provider model (confirmed)

**Tier 1: Convenience presets** (`KnownProviders` retains its current set, with semantic shift):

| Provider string | Wire format | Default URL | Default env var | Eager check |
|---|---|---|---|---|
| `nim` | OpenAI Chat Completions | `https://integrate.api.nvidia.com/v1/chat/completions` | `NIM_API_KEY` | yes |
| `zen` | multi-format (Anthropic or OpenAI, derived from `base_url` path) | none (user must specify `base_url`) | `OPENCODE_API_KEY` | yes |
| `go` | multi-format (Anthropic or OpenAI, derived from `base_url` path) | none (user must specify `base_url`) | `OPENCODE_API_KEY` | yes |

**Tier 2: Compatibility providers** (new — agnostic, format-explicit):

| Provider string | Wire format | URL source | Env var source |
|---|---|---|---|
| `openai` | OpenAI Chat Completions | `base_url` (required) | `api_key_env` (required) |
| `anthropic` | Anthropic Messages | `base_url` (required) | `api_key_env` (required) |

These are **agnostic**: they don't tie to a specific vendor. The user can point `openai` at OpenRouter, vLLM with OpenAI-format enabled, a local llama.cpp server, etc. The user can point `anthropic` at any Anthropic-compatible endpoint.

#### Resolution for S-01's `custom` provider (confirmed: alias for `anthropic`)

S-01 lands with `custom` unchanged. S-03 introduces `anthropic` as a new explicit name and registers `custom` as an alias that resolves to the same adapter (`AnthropicCompatibleAdapter`). Backward compatibility is preserved; the canonical name going forward is `anthropic`.

**Concrete `KnownProviders` after S-03**: `{nim, zen, go, custom, openai, anthropic}` — six names, two of which (`custom` and `anthropic`) resolve to the same adapter. The dispatcher treats them identically; the distinction is for human readability of the config file.

#### Example `config.yaml` under the confirmed model

```yaml
models:
  # Tier 1: known provider, no URL needed
  claude-opus-4:
    provider: nim
    model: meta/llama-3.1-70b-instruct
    api_key_env: NIM_API_KEY

  # Tier 1: known provider with URL override (Zen Anthropic-format)
  claude-sonnet-4:
    provider: zen
    model: claude-sonnet-4-6
    base_url: https://opencode.ai/zen/v1/messages
    api_key_env: OPENCODE_API_KEY

  # Tier 1: known provider with URL override (Zen OpenAI-format)
  glm-large:
    provider: zen
    model: glm-5.1
    base_url: https://opencode.ai/zen/v1/chat/completions
    api_key_env: OPENCODE_API_KEY

  # Tier 2: agnostic compat provider — any OpenAI-compatible endpoint
  openrouter-gpt:
    provider: openai
    model: openai/gpt-5
    base_url: https://openrouter.ai/api/v1/chat/completions
    api_key_env: OPENROUTER_API_KEY

  # Tier 2: agnostic compat provider — any Anthropic-compatible endpoint
  my-anthropic-shim:
    provider: anthropic
    model: my-sonnet
    base_url: https://my-shim.example.com/v1/messages
    api_key_env: MY_SHIM_KEY

  # Tier 2 (alias): old "custom" still works, treated as anthropic
  legacy-custom:
    provider: custom
    model: legacy-sonnet
    base_url: https://legacy.example.com/v1/messages
    api_key_env: LEGACY_KEY
```

#### Dispatch logic under the confirmed model

The dispatcher maps `provider` strings to adapter instances via a single lookup table:

1. **`nim`** → `NIMAdapter` (preset; uses hardcoded `https://integrate.api.nvidia.com/v1/chat/completions` if `base_url` is empty, else user override)
2. **`zen`** → `ZenAdapter` (preset; multi-format router by `base_url` path suffix)
3. **`go`** → `GoAdapter` (preset; multi-format router by `base_url` path suffix)
4. **`openai`** → `OpenAICompatibleAdapter` (compat; requires `base_url` + `api_key_env`)
5. **`anthropic`** → `AnthropicCompatibleAdapter` (compat; requires `base_url` + `api_key_env`)
6. **`custom`** → `AnthropicCompatibleAdapter` (alias for `anthropic`; requires `base_url` + `api_key_env`)
7. **otherwise** → 500 with "provider not registered"

**Adapter factoring**:

- `proxy/translate/` — pure translation (S-01)
- `proxy/openai_compat.go` — `OpenAICompatibleAdapter` (new in S-03) — `http.Client` + `translate.TranslateRequest` + `translate.TranslateStream`
- `proxy/anthropic_compat.go` — `AnthropicCompatibleAdapter` (new in S-03) — `httputil.ReverseProxy` passthrough with body re-injection
- `proxy/zen.go` and `proxy/go.go` — thin wrappers that delegate to the above based on URL path

`ZenAdapter.handleOpenAI` and `OpenAICompatibleAdapter.Handle` are functionally identical; only the routing logic differs. `ZenAdapter.handleAnthropic` and `AnthropicCompatibleAdapter.Handle` are also functionally identical. S-03 factors the shared code into the compat adapters and has the preset adapters call into them.

#### Implications for S-01

**None** — the S-03 add-on approach means S-01 lands unchanged. S-03 introduces:
- The `openai` and `anthropic` compat providers (new in `KnownProviders`)
- The `custom` → `anthropic` alias registration in the dispatcher
- The `ZenAdapter` and `GoAdapter` preset adapters (multi-format routers)
- The shared `OpenAICompatibleAdapter` and `AnthropicCompatibleAdapter` (new files; S-01's `NIMAdapter` and `CustomAdapter` continue to exist and are not renamed)

`KnownProviders` after S-03: `{nim, zen, go, custom, openai, anthropic}`. Strict-mode YAML still validates against this set.

#### Single-source principle

The `providerFactory` table in `main.go` (per §4.3) extends to 6 rows:

```go
factories := []providerFactory{
    {name: "nim", envVar: "NIM_API_KEY", requiresKey: true, construct: proxy.NewNIMAdapter},
    {name: "zen", envVar: "OPENCODE_API_KEY", requiresKey: true, construct: proxy.NewZenAdapter},
    {name: "go", envVar: "OPENCODE_API_KEY", requiresKey: true, construct: proxy.NewGoAdapter},
    {name: "openai", envVar: "", requiresKey: false, construct: proxy.NewOpenAICompatibleAdapter},
    {name: "anthropic", envVar: "", requiresKey: false, construct: proxy.NewAnthropicCompatibleAdapter},
    {name: "custom", envVar: "", requiresKey: false, construct: proxy.NewAnthropicCompatibleAdapter}, // alias
}
```

`requiresKey: false` for compat providers means the user MUST set `api_key_env` per model — the dispatcher validates this at request time, not at startup. This is consistent with S-01's `custom` behavior (per `plan.md:62`).

#### Open questions for the planner (post-triage)

All previously-blocking questions are resolved. Remaining questions for the planner to handle during `/10x-plan`:

1. **`base_url` requirement for compat providers**: should the config validation REQUIRE `base_url` for `provider: openai` and `provider: anthropic`? Recommendation: yes, mirror the S-01 `custom` rule. New validation cases in `config_test.go`.
2. **`api_key_env` requirement for compat providers**: should the config validation REQUIRE `api_key_env` for compat providers? Recommendation: yes, since the user must specify the env var. (Eager startup check is not possible without knowing the env var name.) New validation cases.
3. **Test coverage**: does the existing `TestKnownProviders` test (`config/config_test.go:183-192`) need updating to assert all 6 names? Yes — but the test was written for the F-01 set `{nim, zen, go, custom}`. S-03 must update the assertion to `{nim, zen, go, custom, openai, anthropic}`.
4. **`zen` and `go` validation**: should they require `base_url` (since they have no default)? Recommendation: yes, per §3.3.
5. **The `anthropic-version` header question** (§6.5): verify during S-03 implementation.

These are all small and resolved by the recommended choices. The S-03 planner can write the plan without further triage.

---

### Follow-up #3 (2026-06-16 18:55) — Default values for known providers stored in the exe (CONFIRMED)

**User proposal**: for known providers (`nim`, `zen`, `go`), store default YAML values inside the executable. When the user omits a field, the loader fills it in from the exe's default table. The user can still override per model.

**Status (2026-06-16 19:15): APPROVED** — user confirmed all options (Option B for eager check).

**Motivation**: less YAML repetition, single source of truth for "what does provider X typically look like", cleaner example config, easier to evolve the defaults without touching user files.

**Status: PROPOSED — not yet triaged with user, but architecturally sound. Confirm during planning.**

#### Concrete design

Add a new file `config/defaults.go` (new in S-03):

```go
package config

type modelDefaults struct {
    BaseURL   string
    APIKeyEnv string
}

var knownProviderDefaults = map[string]modelDefaults{
    "nim": {
        BaseURL:   "https://integrate.api.nvidia.com/v1/chat/completions",
        APIKeyEnv: "NIM_API_KEY",
    },
    "zen": {
        // No default base_url — multi-format gateway; user must specify per model.
        APIKeyEnv: "OPENCODE_API_KEY",
    },
    "go": {
        APIKeyEnv: "OPENCODE_API_KEY",
    },
    // custom, openai, anthropic: no defaults — user provides everything.
}

func (c *Config) applyDefaults() {
    for name, m := range c.Models {
        d, ok := knownProviderDefaults[m.Provider]
        if !ok {
            continue
        }
        if m.BaseURL == "" {
            m.BaseURL = d.BaseURL
        }
        if m.APIKeyEnv == "" {
            m.APIKeyEnv = d.APIKeyEnv
        }
        c.Models[name] = m
    }
}
```

In `config.Load` (existing file, modified by S-03):

```go
// After strict YAML parse, before validation
cfg.applyDefaults()
// ... existing validation as before
```

#### What this changes

**`config.example.yaml` becomes much smaller** (post-S-03):

```yaml
models:
  # NIM — both fields defaulted from known provider
  claude-opus-4:
    provider: nim
    model: meta/llama-3.1-70b-instruct

  # Zen Anthropic-format — api_key_env defaulted; base_url required
  claude-sonnet-4:
    provider: zen
    model: claude-sonnet-4-6
    base_url: https://opencode.ai/zen/v1/messages

  # Zen OpenAI-format — api_key_env defaulted; base_url required
  glm-large:
    provider: zen
    model: glm-5.1
    base_url: https://opencode.ai/zen/v1/chat/completions

  # Compat provider — all fields required (no defaults)
  openrouter-gpt:
    provider: openai
    model: openai/gpt-5
    base_url: https://openrouter.ai/api/v1/chat/completions
    api_key_env: OPENROUTER_API_KEY

  # Custom (alias for anthropic) — all fields required
  legacy-custom:
    provider: custom
    model: legacy-sonnet
    base_url: https://legacy.example.com/v1/messages
    api_key_env: LEGACY_KEY
```

**Validation rules update** (S-03):
- `provider: nim` no longer requires `base_url` (default is filled in)
- `provider: nim` no longer requires `api_key_env` (default is filled in)
- `provider: zen` no longer requires `api_key_env` (default is filled in) — but still requires `base_url`
- `provider: go` no longer requires `api_key_env` (default is filled in) — but still requires `base_url`
- `provider: openai` / `provider: anthropic` / `provider: custom` — both `base_url` and `api_key_env` required (no defaults)

**Adapter code simplification** (S-03):
- S-01's `NIMAdapter` was going to have a hardcoded `https://integrate.api.nvidia.com/v1/chat/completions` const default (per `research.md:493`). With the merge-at-load approach, the adapter doesn't need a fallback — it just reads `m.BaseURL` directly. The const goes away.
- The `(*Config).UsesProvider` check at startup still works: iterate models, if any model uses `nim` and `m.APIKeyEnv == "NIM_API_KEY"` (the default), check the env var. (Or, simpler: just check the env var once for each preset name, as before.)

#### What stays the same

- `KnownProviders` closed set — unchanged
- `Model` struct — unchanged
- Validation loop — extends with new rules, doesn't restructure
- Strict-mode YAML — unchanged (defaults are applied AFTER strict parse, so unknown fields are still rejected)
- Adapter contract (`Provider.Handle(w, r, m, body)`) — unchanged; `m` has the merged values

#### Eager startup check — two design options

**Option A** (simpler, current approach): hardcoded preset check in `main.go`:
```go
if cfg.UsesProvider("nim") && os.Getenv("NIM_API_KEY") == "" {
    return failf("freedius: NIM_API_KEY env var required (config references provider=nim)")
}
if (cfg.UsesProvider("zen") || cfg.UsesProvider("go")) && os.Getenv("OPENCODE_API_KEY") == "" {
    return failf("freedius: OPENCODE_API_KEY env var required (config references provider=zen or provider=go)")
}
```
- Misses cases where the user has overridden `api_key_env` per model (e.g., `api_key_env: NIM_KEY_2`)
- But these are rare and the request-time check inside the adapter handles them

**Option B** (more thorough, suggested): iterate merged models:
```go
for name, m := range cfg.Models {
    if m.APIKeyEnv != "" && os.Getenv(m.APIKeyEnv) == "" {
        return failf("freedius: %s env var required (config model %q references it)", m.APIKeyEnv, name)
    }
}
```
- Catches ALL missing env vars, including per-model overrides
- One loop, no preset-specific knowledge
- Cleaner, scales to new providers without code changes

**Recommendation**: Option B. The single-source principle (F-01 review F7) is the right match here. The current preset-specific code is a "code-arm-grown" pattern that the user has been pushing back on throughout this conversation.

#### Tests to add

In `config/config_test.go` (new `TestApplyDefaults` function):
- `nim` model with empty `base_url` → filled with `https://integrate.api.nvidia.com/v1/chat/completions`
- `nim` model with explicit `base_url` → keeps user value
- `nim` model with empty `api_key_env` → filled with `NIM_API_KEY`
- `zen` model with empty `api_key_env` → filled with `OPENCODE_API_KEY`
- `zen` model with empty `base_url` → stays empty (no default)
- `go` model with empty `api_key_env` → filled with `OPENCODE_API_KEY`
- `openai` model with empty fields → all stay empty
- `custom` model with empty fields → all stay empty
- Unknown provider with empty fields → all stay empty (no defaults)

In `config/config_test.go:TestLoad` (new cases):
- Valid `nim` model with only `provider` and `model` (no `base_url`, no `api_key_env`) → passes
- Valid `zen` model with `provider`, `model`, and `base_url` (no `api_key_env`) → passes
- Invalid `zen` model with only `provider` and `model` (no `base_url`) → fails with "provider=zen but no base_url"

#### Open questions for the planner

1. **Option A vs Option B for the eager startup check**: recommend Option B (single loop, no preset knowledge). Confirm during planning.
2. **Should defaults be discoverable via a `freedius init` command or a `--print-defaults` flag?** S-04 concern, not S-03. But S-02's defaults should be designed to be exposed cleanly if/when S-04 adds this.
3. **What if a preset provider gets a new default value in a future release?** Users on the old release have a stale default. Acceptable for a local tool — bump version + changelog entry.
4. **Should the default values be in a separate YAML file (compiled into the binary via `embed`)?** This would let users fork and modify without touching Go code. Overkill for S-03; recommend hardcoded in Go for simplicity. The values are short and rarely change.

---

### Follow-up #4 (2026-06-16 19:05) — Family-aware model mapping (CONFIRMED)

**User proposal**: the user should not write regex patterns in the config. Instead, they declare mappings by **semantic family name** (`opus`, `sonnet`, `haiku`, `auto`) and the router knows the patterns internally. This replaces (or supplements) the current exact-match `models:` map.

**Status (2026-06-16 19:15): APPROVED** — user confirmed the full design including the `default:` catch-all family.

**Concrete example** (from the user's message): route all opus requests to DeepSeek V4 Pro via Opencode Go, sonnet to DeepSeek V4 Flash via Opencode Go, and auto/haiku to Step 3.5 via NVIDIA NIM.

```yaml
mappings:
  opus:   { provider: go,  model: deepseek-v4-pro,   base_url: https://opencode.ai/zen/go/v1/chat/completions, api_key_env: OPENCODE_API_KEY }
  sonnet: { provider: go,  model: deepseek-v4-flash, base_url: https://opencode.ai/zen/go/v1/chat/completions, api_key_env: OPENCODE_API_KEY }
  haiku:  { provider: nim, model: step-3.5 }
  auto:   { provider: nim, model: step-3.5 }
```

**Motivation**: Claude Code sends model names with predictable patterns (`claude-opus-4-1`, `claude-opus-4-5`, `claude-opus-4-7`, `claude-sonnet-4-6`, `claude-haiku-3-5`, `claude-3-5-sonnet-latest`, etc.). Forcing the user to enumerate every version is tedious and brittle — a new Claude version would require a config update. Family-aware mapping is the natural abstraction.

**Status: PROPOSED — not yet triaged. Awaiting user confirmation.**

#### Concrete design

**Schema**: a new `mappings:` block at the top of the config. Each key is a semantic family name. The value is the same shape as a `Model` entry (provider, model, base_url, api_key_env).

```yaml
mappings:
  <family_name>:
    provider: <string>
    model: <string>
    base_url: <string>     # optional for presets, required for compat
    api_key_env: <string>  # optional for presets, required for compat
```

**Built-in family patterns** (in the router, hardcoded):

| Family name | Regex pattern | Matches examples |
|---|---|---|
| `opus` | `opus` (case-insensitive substring) | `claude-opus-4-1`, `claude-opus-4-5`, `claude-opus-4-7`, `claude-3-opus-20240229` |
| `sonnet` | `sonnet` | `claude-sonnet-4-6`, `claude-3-5-sonnet-latest`, `claude-3-sonnet-20240229` |
| `haiku` | `haiku` | `claude-haiku-3-5`, `claude-haiku-4-5` |
| `auto` | `auto` OR matches when no other family matches | `auto`, the default model Claude Code uses |

**Family extraction algorithm** (in `config` or `proxy` package):

```go
var knownFamilies = []struct {
    name    string
    pattern *regexp.Regexp
}{
    {"opus",   regexp.MustCompile(`(?i)opus`)},
    {"sonnet", regexp.MustCompile(`(?i)sonnet`)},
    {"haiku",  regexp.MustCompile(`(?i)haiku`)},
    {"auto",   regexp.MustCompile(`(?i)auto`)},
}

// extractFamily returns the first matching family name, or "" if none.
// The order of knownFamilies defines priority (more specific families first).
func extractFamily(modelName string) string {
    for _, f := range knownFamilies {
        if f.pattern.MatchString(modelName) {
            return f.name
        }
    }
    return ""
}
```

**Lookup order in the dispatcher** (per request):

1. **Exact match in `models:` map** (current behavior, kept for power users) — first
2. **Family match in `mappings:` block** — second
3. **404 not found** — fallback

```go
// In proxy.Dispatcher.ServeHTTP, replace the m, ok := d.Cfg.Models[req.Model] lookup:

// 1. Exact match
if m, ok := d.Cfg.Models[req.Model]; ok {
    // dispatch to m
} else if family := extractFamily(req.Model); family != "" {
    if mapping, ok := d.Cfg.Mappings[family]; ok {
        // dispatch to mapping
    } else {
        // 404
    }
} else {
    // 404
}
```

**Validation rules** (new in S-03):
- `mappings:` block is optional; if absent, only `models:` is consulted
- Family names must be from the built-in set (`opus`, `sonnet`, `haiku`, `auto`) — reject unknown family names with a clear error
- A family may map to any valid provider+model combination (no new constraints)
- **Priority order**: the user lists families in YAML insertion order; the first family (in the `knownFamilies` priority) wins at lookup time. This means the user's YAML order doesn't matter for family matching — only the built-in priority does. The user's order matters only within each family (which is a single entry).

**What stays the same**:
- `models:` exact-match map (S-01 unchanged) — for power users who want exact match
- `KnownProviders` closed set (per Follow-up #2)
- `Model` struct (per Follow-up #3) — same fields, used inside each mapping entry
- In-binary defaults (per Follow-up #3) — apply to mapping entries the same way

#### What this means for the config example

**Before** (S-01 design, exact match):
```yaml
models:
  claude-opus-4-1:
    provider: nim
    model: meta/llama-3.1-70b-instruct
  claude-opus-4-5:
    provider: nim
    model: meta/llama-3.1-70b-instruct
  claude-opus-4-6:
    provider: nim
    model: meta/llama-3.1-70b-instruct
  claude-opus-4-7:
    provider: nim
    model: meta/llama-3.1-70b-instruct
  # ... etc, every Claude Code model version
```

**After** (S-03, family mapping + in-binary defaults):
```yaml
mappings:
  opus:   { provider: go,  model: deepseek-v4-pro,   base_url: https://opencode.ai/zen/go/v1/chat/completions, api_key_env: OPENCODE_API_KEY }
  sonnet: { provider: go,  model: deepseek-v4-flash, base_url: https://opencode.ai/zen/go/v1/chat/completions, api_key_env: OPENCODE_API_KEY }
  haiku:  { provider: nim, model: step-3.5 }
  auto:   { provider: nim, model: step-3.5 }
```

This is **a 6x reduction in config size** and decouples the user from Claude's version numbering.

#### Migration path from S-01

S-01 lands with `models:` as the only mapping mechanism. S-03 adds `mappings:` as a parallel block. Both can coexist:
- User with only `models:` (S-01 style) — works unchanged
- User with only `mappings:` (S-03 family style) — works as designed
- User with both — `models:` exact match takes priority; `mappings:` is the fallback

No breaking change. S-01's `models:` continues to work for power users and for specific overrides (e.g., a single experimental model that doesn't fit a family).

#### Risks

1. **False positives**: a model name with "opus" or "sonnet" in it from a non-Anthropic provider could match. Mitigation: the user can override with `models:` for any specific case; documented in the example.
2. **"auto" ambiguity**: different clients may use different names for their "default" model. Claude Code's default is well-known but other clients may differ. Mitigation: the `auto` family is a best-effort catch-all; users can override.
3. **The "extractFamily" function is hardcoded in Go**: adding a new family (e.g., "instant" for a future Claude Instant model) requires a code change. Acceptable for MVP; the function could become data-driven in a future release.
4. **No nested families**: `opus-4-5` and `opus-3` both match "opus". If the user wants to distinguish, they can use `models:` exact match for the specific case.
5. **Case sensitivity**: model names from Claude Code are lowercase. The regex uses `(?i)` for safety. If a future client sends mixed case, the router still works.

#### Tests to add

In `config/config_test.go` (new `TestExtractFamily` function):
- `claude-opus-4-1` → `opus`
- `claude-opus-4-7` → `opus`
- `claude-3-opus-20240229` → `opus`
- `claude-sonnet-4-6` → `sonnet`
- `claude-3-5-sonnet-latest` → `sonnet`
- `claude-haiku-3-5` → `haiku`
- `claude-haiku-4-5` → `haiku`
- `auto` → `auto`
- `gpt-5` → `""` (no family match; falls through to exact match in `models:`)
- `unknown-model` → `""`

In `config/config_test.go:TestLoad` (new cases):
- Valid `mappings:` block with all four families → passes
- `mappings:` with unknown family name (e.g., `foo`) → fails with "unknown mapping family"
- `mappings:` with `opus` family but missing `provider` → fails with the standard "no provider" error
- `mappings:` with `opus` family using `provider: custom` but no `base_url` → fails (same as exact-match case)
- `mappings:` and `models:` both present → both validated independently

In `proxy/proxy_test.go` (new cases for the dispatcher):
- `POST /v1/messages {model: "claude-opus-4-7"}` with `mappings: {opus: ...}` → dispatches to the opus mapping
- `POST /v1/messages {model: "claude-opus-4-7-experimental"}` with both `models: {claude-opus-4-7-experimental: ...}` and `mappings: {opus: ...}` → dispatches to the exact match in `models:`
- `POST /v1/messages {model: "gpt-5"}` with only `mappings: {opus: ...}` → 404 (no family match, no exact match)
- `POST /v1/messages {model: "gpt-5"}` with `models: {gpt-5: ...}` → dispatches to exact match

#### Open questions for the planner

1. **Should "auto" be a hardcoded family or a special name?** The user explicitly mentioned `auto`, so it's clearly desired. But the "auto" pattern is fuzzy — it should probably match "when no other family matches" as a fallback. Recommendation: match `auto` literally, AND have a special `default:` key (if present) that catches anything not matched by another family.
2. **Should the family priority order be configurable?** Probably no for MVP; the built-in order is fine.
3. **Should `mappings:` and `models:` be allowed to conflict (same model in both)?** No — `models:` always wins. Document this.
4. **What if the user wants to disable a family?** E.g., "I want all sonnet requests to 404, not fall through to a default". They can omit the family from `mappings:`. Already supported.
5. **Should there be a `default:` key in `mappings:` that catches unmatched models?** This is the natural complement to the family system. Recommendation: yes, support it. If `mappings: {default: ...}` is set, any model not matched by an exact entry or a named family dispatches to the default. This gives the user a clean way to say "anything not in a family, go here".

This follow-up is ready for user confirmation. If approved, the S-03 planner incorporates it into the plan. The combined design (Follow-ups #1-4) is the full S-03 architecture:

1. Unified `OPENCODE_API_KEY` (Zen and Go share one key)
2. Two-tier model: `KnownProviders` (presets) + `openai`/`anthropic` (compat) + `custom` alias
3. In-binary defaults for known providers
4. Family-aware mapping (opus/sonnet/haiku/auto + optional default)

This follow-up is ready for user confirmation. If approved, the S-03 planner incorporates it into the plan.

---

### Follow-up #5 (2026-06-16 19:10) — Scope split: S-03 narrowed; provider-model-v2 → new S-02

**User question**: "Should we divide the research to more slices and update roadmap or is doable in one slice?"

**Decision (2026-06-16 19:10)**: Two slices. The follow-up work is too large to fit in S-03 cleanly; the architectural changes (compat providers, in-binary defaults, family mapping) deserve their own slice.

**Final S-03 scope** (this change):
- `ZenAdapter` and `GoAdapter` (multi-format routers, per Follow-up #4 / §4.1)
- `OPENCODE_API_KEY` unified env var (per Follow-up #1)
- The `custom` → `anthropic` alias (per Follow-up #2, applied as a single-line change in the dispatcher)
- S-03 may depend on the compat adapter code from S-02; if S-02 is delayed, S-03 inlines the router code temporarily

**New S-02 scope** (extracted to `context/changes/provider-and-mapping/`):
- Compat providers `openai` and `anthropic` (the generic adapter code that S-03's `ZenAdapter`/`GoAdapter` delegate to)
- Two-tier provider model with `KnownProviders` extended to 6 names (per Follow-up #2)
- In-binary defaults for known providers (per Follow-up #3)
- Family-aware mapping with `mappings:` block, family extraction (`opus`/`sonnet`/`haiku`/`auto`/`default`) (per Follow-up #4)
- Eager startup check refactor to single-loop pattern (Option B from Follow-up #3)

**Roadmap impact (after renumber on 2026-06-16 19:20)**:
- S-02 = provider-and-mapping (was provider-model-v2, was the new scope)
- S-03 = zen-go-adapters (this slice; was originally S-02)
- S-04 = error-hardening (was originally S-03)
- S-02 ships FIRST so S-03 can build on its compat adapter code

**Implications for S-03's plan (this slice)**:
- The plan SHOULD reference S-02 as a dependency
- The plan SHOULD NOT propose any schema changes that belong to S-02 (no `mappings:` block, no in-binary defaults, no `KnownProviders` extension)
- The plan SHOULD mention that `custom` continues to work as an alias (one-line change in the dispatcher, not a separate work item)
- If S-02 is delayed, S-03 may inline the compat adapter code as a temporary measure

**Implications for S-02's plan (the new scope)**:
- S-02 needs the S-01 `Provider` interface seam
- S-02 produces `OpenAICompatibleAdapter` and `AnthropicCompatibleAdapter` which S-03 (and future adapters) delegate to
- S-02 includes the family extraction logic
- S-02 includes the in-binary defaults

**Updated roadmap** (`context/foundation/roadmap.md`):

| ID | Change ID | Outcome | Status |
|---|---|---|---|
| F-01 | proxy-skeleton | (foundation) HTTP server listens, config loaded, dispatch stub | done |
| S-01 | first-call-routed | route a Claude Code call through freedius to NIM or a custom Anthropic-compatible provider | done |
| **S-02** | **provider-and-mapping** | family-aware mapping + compat providers + in-binary defaults | proposed |
| **S-03** | **zen-go-adapters** | route calls to Opencode Zen or Opencode Go via multi-format adapters | proposed |
| **S-04** | **error-hardening** | clear error messages + env injection + config template | proposed |

The full S-02 design lives at `context/changes/provider-and-mapping/research.md` (extracted from this file's Follow-ups #2, #3, #4). The S-03 plan reads this S-03 research for the Zen/Go adapter work and references the S-02 research for the compat adapter API contract.


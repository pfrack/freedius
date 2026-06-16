---
id: provider-and-mapping
title: Provider-and-mapping — family-aware mapping + compat providers + in-binary defaults
status: planned
created: 2026-06-16
updated: 2026-06-16
roadmap_id: S-02
prd_refs:
  - FR-001
  - FR-003
  - FR-004
  - FR-009
  - NFR-Error-handling
---

# Provider-and-Mapping (S-02) Implementation Plan

## Overview

Wire the S-02 architectural refactor: a `mappings:` config block that routes by semantic family name (opus/sonnet/haiku/auto/default), a two-tier provider model (preset providers with in-binary defaults + agnostic compat providers for any compatible endpoint), and the S-01 foundation (Provider interface, Registry, NIM+custom adapters, env-var-based credentials) that those compat adapters are built on. Bundled into one PR because the schema, dispatcher, and adapter layers are interdependent.

## Current State Analysis

- `config/config.go:13-15` — `Config{Models map[string]Model}`. No `Mappings` field. `Model` has only `Provider` and `Model` (line 17-20); no `BaseURL` or `APIKeyEnv`.
- `config/config.go:22-27` — `KnownProviders = {nim, zen, go, custom}`. Closed set validated at load time.
- `config/config.go:51-63` — per-model validation loop. Rejects unknown provider, missing `model`, missing `provider`, and unsafe characters in `model` (CR/LF/colon). The S-01 review (F-01 review F5) established the CRLF defense.
- `proxy/proxy.go:17-30` — `Dispatcher` struct with `Cfg` and `Logger`; 2-arg `NewDispatcher` (no registry). F-01 review F7 added nil checks.
- `proxy/proxy.go:32-91` — `Dispatcher.ServeHTTP` with the 501 stub at line 86-90; the F-01 review changed this from a 400 to a 501 with `X-Freedius-Matched-*` headers. The body-read at line 47 is a load-bearing seam: by the time any adapter runs, the body is already a `[]byte`.
- `proxy/proxy.go:93-107` — `writeJSON` and `writeError` helpers. F-01 review F6 added logging on encode errors.
- `main.go:82` — `proxy.NewDispatcher(cfg, logger)` (2-arg). No Registry, no env-var checks, no startup-time credential validation.
- `config.example.yaml:1-7` — the F-01 stub example. Uses `provider: nim` and `provider: custom` with no `base_url` / `api_key_env`.
- `config/config_test.go:11-166` — `TestLoad` table (11 cases) covers the F-01 surface. The S-01 review (F-01 review F7) updated `TestKnownProviders` to expect the sorted closed set `{custom, go, nim, zen}`.
- `proxy/proxy_test.go:15-24` — `newTestDispatcher` helper. S-02 will need to extend it to pass a Registry.
- F-01 review (`context/changes/proxy-skeleton/reviews/impl-review.md`) settled the hardening S-02 inherits: ReadTimeout/IdleTimeout, Content-Type validation, CRLF defense, defensive nil checks, single-source `KnownProviders`.
- **The S-01 plan at `context/changes/first-call-routed/plan.md` describes the Provider interface, Registry, NIM+custom adapters, and `(*Config).UsesProvider` method — none of which are on disk yet.** The git log's `4d88ef1 First call routed (#2)` is misleading; the commit message describes F-01 work that was renamed post-hoc and the Go files were not modified by it. S-02 therefore folds the S-01 foundation into Phase 1.

## Desired End State

After S-02 lands:

- A user can write a five-line config:
  ```yaml
  mappings:
    opus:    { provider: go,  model: deepseek-v4-pro }
    sonnet:  { provider: go,  model: deepseek-v4-flash }
    haiku:   { provider: nim, model: step-3.5 }
    auto:    { provider: nim, model: step-3.5 }
    default: { provider: nim, model: step-3.5 }
  ```
  and freedius routes every Claude Code request (`claude-opus-4-1`, `claude-sonnet-4-6`, `claude-haiku-3-5`, `auto`, anything-else) to the right provider, translating the Anthropic format to OpenAI Chat Completions or passing it through to an Anthropic-compatible endpoint as needed.
- `provider: nim` works with no `base_url` and no `api_key_env` in the config — the in-binary defaults fill them in. Setting `api_key_env: NIM_API_KEY` is still supported as an explicit override.
- `provider: openai` and `provider: anthropic` are agnostic compat providers. The user supplies `base_url` and `api_key_env` per model; freedius translates or passes through as appropriate.
- `provider: custom` is accepted and rewritten to `provider: anthropic` at config-load time. Users who upgrade from S-01 keep their existing configs working.
- `provider: zen` and `provider: go` return 500 with `provider not registered` (S-03's work; the `mappings:` block is the load-bearing new surface).
- The S-01 power-user path (`models:` exact-match) continues to work unchanged. Exact match always wins over family match.
- Missing env vars fail at startup with a clear actionable message. Per-model overrides are caught.
- `make ci` is green. Coverage: `config` ≥ 90%, `proxy` ≥ 85%, `proxy/translate` ≥ 90%.

### Key Discoveries

- The dispatcher's body-read at `proxy/proxy.go:47` is the load-bearing seam for every adapter that follows. Custom-style passthrough adapters must re-inject `r.Body` and `r.ContentLength` from the buffered `[]byte`; translation adapters consume the `[]byte` directly. F-01 review hardening (Content-Type, MaxBytesReader, 413) is preserved.
- The F-01 review's single-source principle (F7) carries forward: one `KnownProviders` map, one `knownProviderDefaults` map, one `knownFamilies` slice. S-02 follows this — no parallel definitions of "the list of providers" or "the list of families".
- The `custom` → `anthropic` alias is resolved in `applyDefaults` (before validation), so error messages that reference the provider name will say "anthropic" even when the user wrote `custom`. Tests for the `custom` case must use the post-rewrite name in expected error substrings.
- Family pattern priority is fixed by the order of entries in the `knownFamilies` slice, not by YAML order. The user cannot override priority by reordering their `mappings:` block. This is by design — predictable routing beats flexible-but-confusing routing.
- The compat adapter factoring is DRY at the adapter level (one `AnthropicCompatibleAdapter`, one `OpenAICompatibleAdapter`) but NOT at the routing level. `CustomAdapter` and `AnthropicCompatibleAdapter` are the same code; `NIMAdapter` and `OpenAICompatibleAdapter` share translation internals. The DRY structure is: shared code in compat adapters, thin wrappers in `nim.go` / `custom.go` for S-01 compatibility and S-03 ergonomics.
- S-01's SSE footguns (json.Encoder newline, bufio.Scanner 64KB cap) carry forward into S-02's `OpenAICompatibleAdapter` and translation package. The implementer uses `json.Marshal` and `bufio.Reader.ReadBytes` — not `json.NewEncoder.Encode` or `bufio.Scanner`.

## What We're NOT Doing

- **Zen/Go multi-format adapter support** — `provider: zen` and `provider: go` still return 501 in S-02. S-03 builds multi-format routers on top of S-02's compat adapters.
- **OpenAI Responses / Google Generative AI formats** — only Anthropic Messages and OpenAI Chat Completions are supported. Per the S-03 research (open question #1-#2), these are out of MVP scope.
- **Hot-reload of model lists** — `provider: zen` / `provider: go` users maintain their config manually. S-04's `freedius init` may fetch the model list, but S-02 doesn't.
- **`freedius init` command / config template generation** — S-04. S-02 only updates `config.example.yaml` to demonstrate the new schema.
- **Auto-injection of Claude Code env vars** — S-04. S-02 only validates that env vars are set; it doesn't write them.
- **Provider-specific env-var constraints** — every provider accepts any `api_key_env` name. The defaults map is the only preset; users can override per model.
- **Family name customization** — `opus`/`sonnet`/`haiku`/`auto`/`default` are hardcoded. The user cannot add a new family. This is a deliberate v1 constraint: the user opted for a 5-line config over a custom-regex DSL.
- **`base_url` defaults for compat providers** — `provider: openai` and `provider: anthropic` require the user to supply `base_url`. No in-binary default.
- **Pattern-based `base_url` per provider** — `base_url` is always user-supplied (or in-binary default for presets). The dispatcher does not synthesize URLs.
- **Top-level `providers:` block in YAML** — per-model and per-mapping `base_url` + `api_key_env` only. Matches the S-01 design.
- **Lazy env-var re-read** — read once at adapter construction (eager check at startup is the operational guarantee). Rotation requires a freedius restart.
- **Per-model `Provider` interface customization** — the `Provider` interface has one method (`Handle`). Adapters that need more per-request state can store it in the `body []byte` argument or extend `Config.Model` (e.g., the S-03 router needs `m.BaseURL`; this is already in the schema).
- **Tightening the dispatcher's URL routing** — `mux.Handle("/", dispatcher)` stays. S-02 doesn't know what paths Claude Code calls beyond `/v1/messages`; tightening risks breaking an undocumented path.
- **Total upstream-call timeout** — no `http.Client.Timeout`, no `context.WithTimeout` on the outbound call. Wall-clock is bounded only by the inbound `r.Context()` (client disconnect). Carries over from the S-01 design.
- **Request-body logging** — NFR-Privacy forbids it; carries over from F-01.
- **Metrics endpoint, pprof, in-flight counters** — not in v1 PRD.
- **Windows support** — parked per roadmap. Linux + macOS only for v1.

## Implementation Approach

Three phases, each ending with `make ci` green and a clear manual verification step. The phase order is dependency-driven: schema + provider foundation (Phase 1) → `mappings:` block routing (Phase 2) → family patterns (Phase 3). Phase 1 is the largest (~60% of slice effort) because it absorbs the S-01 foundation work that the S-02 design depends on. Phases 2 and 3 are smaller follow-ups that build on the Phase 1 seam.

The `Provider` interface is the single seam: it hides the asymmetry between Anthropic passthrough (`httputil.ReverseProxy`) and OpenAI translation (`http.Client` + stateful SSE translator) from the dispatcher. The dispatcher doesn't care which kind of adapter it called.

Compat adapters are the new public API surface for the S-02 architecture: `OpenAICompatibleAdapter` and `AnthropicCompatibleAdapter` are the canonical implementations. `NIMAdapter` and `CustomAdapter` (from S-01) are thin wrappers that delegate to the compat adapters — this is what makes S-03's `ZenAdapter` and `GoAdapter` (multi-format routers) easy to write later.

Pure translation functions live in `proxy/translate/` with no I/O. This split is what makes the translator TDD-friendly: golden-file tests run in milliseconds against `bytes.Buffer`, no `httptest` server needed.

Adapter construction is eager at startup in `main.go`. `os.Getenv` is called once per provider; missing keys fail-fast before the server starts listening. This trades a tiny boot-time coupling (env vars must be set before `freedius` starts) for the operational property that misconfigurations surface immediately, not on first request.

Family patterns are hardcoded in `proxy/families.go`. The dispatcher extracts the family from the incoming model name using a built-in pattern table. The user doesn't write regex.

## Critical Implementation Details

These are facts the implementer needs to know before they touch the code — things that aren't visible from file paths alone.

- **Body re-injection in passthrough adapters**: the dispatcher reads the body into `[]byte` at `proxy/proxy.go:47` before calling the adapter. Passthrough adapters (`AnthropicCompatibleAdapter`, `CustomAdapter`) MUST set `r.Body = io.NopCloser(bytes.NewReader(body))` and `r.ContentLength = int64(len(body))` before calling `ReverseProxy.ServeHTTP`. Without this, the proxy sees an empty body and the upstream gets nothing. This is the single most common S-01 implementation bug per the research.
- **`custom` → `anthropic` rewrite happens in `applyDefaults`, before validation**: this means error messages about `custom` will reference `anthropic`. If a test asserts "config error mentions `custom`", the test will fail post-rewrite. The correct assertion is "config error mentions `anthropic`" (the rewritten provider). The `applyDefaults` order is: strict YAML parse → `applyDefaults` (alias rewrite + defaults merge) → per-entry validation.
- **Family pattern priority is fixed by code order, not YAML order**: the `knownFamilies` slice in `proxy/families.go` defines priority. The first family in the slice whose pattern matches wins. A `mappings:` block listing `auto` before `opus` does NOT make `auto` higher priority. This is by design — predictable routing beats flexible-but-confusing routing. Tests must assert the priority behavior independently of YAML ordering.
- **`default:` is opt-in by inclusion in the user's `mappings:` block**: if the user does not include a `default:` mapping, an unmatched model returns 404 (not a "default to anything" catch-all). The user opts in to having a catch-all by writing `default: { provider: ..., model: ... }`. The dispatcher's lookup chain is: `models` exact match → `mappings` family match (in `knownFamilies` order, with `default` last) → 404.
- **Family patterns are substring + case-insensitive regex**: `opus` matches anywhere in the model name (`(?i)opus` → `claude-opus-4-1`, `claude-opus-4-5`, `claude-opus-4-7`, `claude-3-opus-20240229`). This is the simple-but-correct choice for v1: case-insensitive substring matching is what users expect, and the patterns are short enough that false positives are unlikely. A model name like `claude-haiku-sonnet-2024` would match BOTH `haiku` and `sonnet` — priority resolves the ambiguity (the higher-priority family in the slice wins).
- **SSE encoding trap (inherited from S-01)**: `json.NewEncoder(w).Encode(v)` appends `\n` to the marshalled JSON. Using those bytes in `Fprintf(w, "data: %s\n\n", buf)` produces `data: {...}\n\n\n` (three newlines = extra blank line that corrupts SSE event framing). The translator MUST use `json.Marshal` (no trailing newline) when emitting Anthropic SSE events.
- **SSE reader line-cap trap (inherited from S-01)**: `bufio.Scanner` defaults to a 64 KB `MaxScanTokenSize`. Tool-use `arguments` payloads can exceed this. The translator MUST use `bufio.Reader.ReadBytes('\n')` rather than `bufio.Scanner`.
- **Adapter return contract (inherited from S-01)**: `Provider.Handle` returns `nil` only if it has called `w.WriteHeader` (success or upstream-mirrored error). Returning a non-nil error means "I did not write a response — dispatcher, write 502". The dispatcher enforces this single-owner rule. An adapter that has called `WriteHeader` and then encounters an error mid-stream returns `nil` (the response is already in flight; nothing to do).
- **Request-Context propagation for cancellation (inherited from S-01)**: adapters MUST build the upstream request with `http.NewRequestWithContext(r.Context(), ...)` so a client disconnect propagates as `context.Canceled` to the upstream transport and to the streaming reader. Without this, a closed Claude Code session leaves a hung goroutine draining the upstream response.
- **`(*Config).UsesProvider` is called by the eager env-var check loop**: the loop iterates `cfg.Models` (post-`applyDefaults`) and calls `UsesProvider("nim")` for each known provider whose `requiresKey` flag is true. The method iterates the map once per call; this is fine for freedius's scale (single user, local) but if `main.go` ever needs to check 100+ providers, the loop is O(providers × models). For S-02 this is constant time in practice.

## Phase 1: Schema + provider foundation (S-01 work + S-02 schema/defaults/alias/compat)

### Overview

Lay the foundation: extend the config schema (S-01 fields + S-02 `Mappings` field + extended `KnownProviders`), add the in-binary defaults system (`config/defaults.go` + `applyDefaults`), add the `Provider` interface and `Registry`, build the two compat adapters (`OpenAICompatibleAdapter`, `AnthropicCompatibleAdapter`), wire the S-01 NIM+custom adapters as thin wrappers, replace the dispatcher's 501 stub with a Registry lookup, and update `main.go` to build the Registry and do the eager env-var check. End of Phase 1: a config with `models:` (exact match) or `provider: openai` / `provider: anthropic` / `provider: custom` / `provider: nim` all route correctly. `mappings:` is not yet consulted (Phase 2).

This phase is large because it absorbs the S-01 work that was supposed to land in a separate slice. The S-01 work is folded in because S-02's design depends on the S-01 seam and shipping S-01 separately would require a half-shipped S-02 (or a half-shipped S-01 with no `mappings:` block to use it from).

### Changes Required:

#### 1. Extend `config.Model` with S-01 fields and extend `Config` with S-02 `Mappings`

**File**: `config/config.go`

**Intent**: Add `BaseURL` and `APIKeyEnv` to `Model` (S-01) and add `Mappings map[string]Model` to `Config` (S-02). Strict-mode YAML decoding requires the struct fields exist before any code reads them.

**Contract**: `Model` gains two `yaml` tags:
- `BaseURL string \`yaml:"base_url,omitempty"\`` — the upstream endpoint
- `APIKeyEnv string \`yaml:"api_key_env,omitempty"\`` — the env-var *name* (e.g. `NIM_API_KEY`), not the value

`Config` gains one field:
- `Mappings map[string]Model \`yaml:"mappings,omitempty"\`` — family-name → model mapping. The map key (string) is the family name (`opus`/`sonnet`/`haiku`/`auto`/`default` in Phase 3; any string in Phase 2).

#### 2. Extend `KnownProviders` and add per-entry validation

**File**: `config/config.go`

**Intent**: Add `openai` and `anthropic` to the closed set so users can opt into the agnostic compat providers. The `custom` → `anthropic` alias is resolved in `applyDefaults` (Change #4), so `custom` stays in the closed set as a user-facing name but is rewritten to `anthropic` before validation runs.

**Contract**: `KnownProviders` becomes `{nim, zen, go, custom, openai, anthropic}`. The per-entry validation loop is extended:
- Reject `provider=openai` / `provider=anthropic` without `BaseURL` (error: `"config: config file at <path>: model %q has provider=%s but no base_url"`). The error message references the post-rewrite provider name (e.g., `anthropic` even if the user wrote `custom`).
- Reject `BaseURL` whose scheme is not `http` or `https`
- Reject `APIKeyEnv` containing CR/LF or `=`

The same validation runs against `cfg.Mappings` entries (Phase 2 confirms this; the per-entry loop covers both maps).

The existing CRLF/colon check on `Model.Model` (line 61) does NOT need extension to `BaseURL` — `net/http` will reject malformed URLs at request time with a clear error.

#### 3. Add "at least one of models/mappings" rule

**File**: `config/config.go`

**Intent**: Allow pure-`mappings:` configs (Phase 2) and pure-`models:` configs (S-01 power-user path) and mixed configs. Reject the empty-both case at load time.

**Contract**: The current check at `config/config.go:47-49` becomes: `if len(cfg.Models) == 0 && len(cfg.Mappings) == 0 { return ... }`. Error message: `"config: config file at <path> contains no model mappings"`. (Kept the same wording; the rule covers both blocks.)

#### 4. Add `config/defaults.go` with in-binary defaults and `applyDefaults`

**File**: `config/defaults.go` (new)

**Intent**: A new file that defines the `knownProviderDefaults` map and an `(*Config).applyDefaults()` method. The method runs after strict YAML parse, before validation. It fills in `BaseURL` and `APIKeyEnv` for preset providers that don't have them set, and rewrites `custom` → `anthropic` (the alias resolution point).

**Contract**: One exported method `func (c *Config) applyDefaults()`. Implementation iterates `c.Models` (Phase 1) and `c.Mappings` (Phase 2) in a single loop. For each entry: if `m.Provider` is in `knownProviderDefaults`, fill in missing `BaseURL` and `APIKeyEnv`. Then if `m.Provider == "custom"`, set `m.Provider = "anthropic"` (alias rewrite).

The `knownProviderDefaults` map is a package-level `var`:
```go
var knownProviderDefaults = map[string]modelDefaults{
    "nim": {BaseURL: "https://integrate.api.nvidia.com/v1/chat/completions", APIKeyEnv: "NIM_API_KEY"},
    "zen": {APIKeyEnv: "OPENCODE_API_KEY"},
    "go":  {APIKeyEnv: "OPENCODE_API_KEY"},
}
```
No defaults for `openai`, `anthropic`, `custom` (post-rewrite) — these require the user to supply both `base_url` and `api_key_env`.

The `modelDefaults` struct is unexported (single-source principle, F-01 review F7):
```go
type modelDefaults struct {
    BaseURL   string
    APIKeyEnv string
}
```

Export a narrow accessor so `main.go`'s eager env-var check can look up the env-var name without exposing the whole map:
```go
func ProviderEnvVar(name string) string
```
Returns the `APIKeyEnv` for a preset provider, or `""` if the provider has no default env-var. The map itself stays unexported; only this one getter is public.

#### 5. Add `(*Config).UsesProvider` method

**File**: `config/config.go`

**Intent**: A method that the eager env-var check loop (Change #10) uses to determine which providers the user's config references. S-02 owns this method (per the planning decision) so the eager-check loop has a single-source-of-truth lookup.

**Contract**: `func (c *Config) UsesProvider(name string) bool` returns true if any entry in `c.Models` or `c.Mappings` has `m.Provider == name` (post-`applyDefaults`, so `custom` is already rewritten). Implementation is a single loop over the union of the two maps.

#### 6. Add the `Provider` interface and `Registry`

**File**: `proxy/provider.go` (new)

**Intent**: The single seam that hides the asymmetry between Anthropic passthrough and OpenAI translation adapters from the dispatcher. The interface has one method: `Handle`.

**Contract**:
```go
package proxy

import (
    "net/http"
    "github.com/pfrack/freedius/config"
)

type Provider interface {
    Handle(w http.ResponseWriter, r *http.Request, m config.Model, body []byte) error
}

type Registry struct {
    providers map[string]Provider
}

func NewRegistry(providers map[string]Provider) *Registry

func (r *Registry) Lookup(name string) (Provider, bool)
```

`NewRegistry` panics on nil entries (defensive, per F-01 review F7). `Lookup` returns `(nil, false)` for unknown names; the caller writes 500.

#### 7. Add `proxy/errors.go` with shared error helpers

**File**: `proxy/errors.go` (new)

**Intent**: Two helpers used by every adapter: `forwardUpstreamError` (verbatim 4xx/5xx passthrough) and `freediusErrorHandler` (handler for `ReverseProxy.ErrorHandler` that writes 502 on transport failures, silent on `context.Canceled`).

**Contract**: Per the S-01 design. `forwardUpstreamError` copies status, headers, and body from the upstream response. `freediusErrorHandler` returns silently on `context.Canceled`, otherwise writes `{"error":"upstream_unreachable","detail":"<err>"}` with 502 and logs at Error level.

#### 8. Add `proxy/anthropic_compat.go` with `AnthropicCompatibleAdapter`

**File**: `proxy/anthropic_compat.go` (new)

**Intent**: The canonical Anthropic-format passthrough adapter. Used directly for `provider: anthropic` and as the underlying implementation for `provider: custom` (post-`applyDefaults` rewrite). S-03's `ZenAdapter.handleAnthropic` and `GoAdapter.handleAnthropic` will also use this.

**Contract**: `AnthropicCompatibleAdapter.Handle` does the standard `httputil.ReverseProxy` passthrough:
1. Read `m.APIKeyEnv` env var; return error if missing
2. Parse `m.BaseURL`; return error if invalid
3. Re-inject body: `r.Body = io.NopCloser(bytes.NewReader(body))`, `r.ContentLength = int64(len(body))`
4. Set `Authorization: Bearer <key>` on `r.Header`
5. Construct per-call `*httputil.ReverseProxy` with `Rewrite` (sets URL, host, `Authorization` header) and `ErrorHandler: freediusErrorHandler(logger)`
6. Call `rp.ServeHTTP(w, r)`, return `nil`

The per-call `ReverseProxy` construction is intentional: a `*ReverseProxy` is ~100 bytes, freed when the goroutine returns, no shared state.

#### 9. Add `proxy/openai_compat.go` with `OpenAICompatibleAdapter`

**File**: `proxy/openai_compat.go` (new)

**Intent**: The canonical OpenAI Chat Completions format translation adapter. Used directly for `provider: openai` and as the underlying implementation for `provider: nim` (S-01) and S-03's Zen/Go `handleOpenAI` paths.

**Contract**: `OpenAICompatibleAdapter.Handle` does the OpenAI translation:
1. Read `m.APIKeyEnv` env var; return error if missing
2. Build upstream URL from `m.BaseURL` (used verbatim, no path manipulation)
3. Call `translate.TranslateRequest(body, m.Model)` to convert Anthropic request body to OpenAI format
4. Build upstream request with `http.NewRequestWithContext(r.Context(), http.MethodPost, targetURL, bytes.NewReader(upstreamBody))`
5. Set `Authorization: Bearer <key>`, `Content-Type: application/json`, `Accept: text/event-stream`
6. `client.Do(req)`; on error, return wrapped error
7. `defer resp.Body.Close()` (critical — without this, upstream connection leaks on every request)
8. If `resp.StatusCode >= 400`, call `forwardUpstreamError(w, resp)` and return its error (which is `nil` after a clean write, or an error mid-copy)
9. Set `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `Connection: keep-alive`
10. `w.WriteHeader(http.StatusOK)`
11. `rc := http.NewResponseController(w)`; call `translate.TranslateStream(resp.Body, w, rc.Flush)`

The `client` field is a `*http.Client` with no `Timeout` (per S-01 design — wall-clock bounded by `r.Context()` only).

#### 10. Add `proxy/translate/` package with the Anthropic↔OpenAI translator

**File**: `proxy/translate/types.go` (new), `proxy/translate/anthropic_openai.go` (new), `proxy/translate/anthropic_openai_test.go` (new), `proxy/translate/testdata/` (new fixtures)

**Intent**: Pure bytes-in/bytes-out translation functions. No I/O, no `http.ResponseWriter`, no `*http.Client`. This split is what makes the translator TDD-friendly.

**Contract**:
- `TranslateRequest(anthropicBody []byte, targetModel string) ([]byte, error)` — translates the Anthropic request body to OpenAI format
- `TranslateStream(upstream io.Reader, downstream io.Writer, flush func() error) error` — reads OpenAI SSE chunks from `upstream`, writes Anthropic SSE chunks to `downstream`. Calls `flush` after every Anthropic event. Returns `nil` on clean `[DONE]`, an error on upstream failure or protocol violation.
- Internal `anthropicEmitter` type (state machine for the SSE translation). Constructed with `newAnthropicEmitter()`; exposes `consumeOpenAILine(line []byte) ([][]byte, error)` returning 0+ Anthropic event bytes to emit.

The full translation rules and edge cases are documented in the S-03 research (the original S-01 source for this work). The implementer reads those sections before writing the code; the plan does not duplicate the rules here.

The two SSE footguns (json.Encoder newline, bufio.Scanner 64KB cap) are called out in Critical Implementation Details above. The implementer MUST use `json.Marshal` (not `json.NewEncoder.Encode`) and `bufio.Reader.ReadBytes('\n')` (not `bufio.Scanner`).

#### 11. Add `proxy/custom.go` as a thin wrapper over `AnthropicCompatibleAdapter`

**File**: `proxy/custom.go` (new)

**Intent**: The S-01 `CustomAdapter` exists for backward compatibility with users who already wrote `provider: custom` in their configs. After `applyDefaults` rewrites `custom` to `anthropic`, the dispatcher never looks up `custom` in the Registry. But the Registry still needs an entry for `custom` (so that any code that does direct lookup, e.g., tests, doesn't fail with "provider not registered"). The `CustomAdapter` is a thin wrapper that delegates to `AnthropicCompatibleAdapter`.

**Contract**: `CustomAdapter` holds a `*AnthropicCompatibleAdapter`. `CustomAdapter.Handle` calls `a.inner.Handle(w, r, m, body)` after one no-op step (returning `nil` if `m.Provider` is `custom` to confirm the alias is post-rewrite). The wrapper exists for S-01 naming compatibility; in S-02's plan, the `custom` adapter is the same as the `anthropic` adapter.

**Alternative considered**: skip the `CustomAdapter` file entirely; let `applyDefaults` rewrite and have only the `anthropic` entry in the Registry. **Rejected** because the S-01 plan documents the `CustomAdapter` as a separate type, and the wrapper makes the S-03 router code (which still mentions `custom` in places) easier to read.

#### 12. Add `proxy/nim.go` as a thin wrapper over `OpenAICompatibleAdapter`

**File**: `proxy/nim.go` (new)

**Intent**: The S-01 `NIMAdapter` is the bridge from S-01's NIM-as-distinct-provider mental model to S-02's NIM-as-preset-with-defaults. After Phase 1, `NIMAdapter` is structurally identical to `OpenAICompatibleAdapter` — the in-binary default URL is filled by `applyDefaults`, so the adapter has no special-case code. The wrapper exists for S-01 naming compatibility and for S-03's router (which may want a `NIMAdapter` distinct from a generic OpenAI adapter).

**Contract**: `NIMAdapter` holds an `*OpenAICompatibleAdapter`. `NIMAdapter.Handle` calls `a.inner.Handle(w, r, m, body)`. No extra logic. If the S-01 plan's design adds a hardcoded fallback URL to the NIM adapter, the fallback is now in `knownProviderDefaults` and the adapter code is simpler — no special-case URL handling.

#### 13. Update `Dispatcher` to consult the Registry

**File**: `proxy/proxy.go`

**Intent**: Replace the 501 stub at `proxy/proxy.go:86-90` with a Registry lookup. Preserve all pre-dispatch behavior (method check, content-type check, body read, JSON parse, model lookup, debug log, matched-headers). On unknown provider: 500 with a clear message. On adapter error: 502.

**Contract**:
- `Dispatcher` struct gains a `Registry *Registry` field.
- `NewDispatcher(cfg, registry, logger)` becomes the 3-arg constructor. Add a nil check on `registry` (defensive, per F-01 review F7).
- The replacement for lines 86-90 is roughly:
  ```go
  adapter, ok := d.Registry.Lookup(m.Provider)
  if !ok {
      d.Logger.Error("provider not registered", "provider", m.Provider)
      d.writeError(w, http.StatusInternalServerError, "provider not registered: "+m.Provider)
      return
  }
  if err := adapter.Handle(w, r, m, body); err != nil {
      d.Logger.Error("adapter failed", "provider", m.Provider, "err", err)
      d.writeError(w, http.StatusBadGateway, "upstream error")
  }
  ```
- The `X-Freedius-Matched-Provider` and `X-Freedius-Matched-Model` headers stay set BEFORE the adapter call (preserved contract from F-01; documented in Critical Implementation Details).
- The `Mappings` lookup is NOT in Phase 1's dispatcher. Phase 2 adds it.
- A config that maps a model to `provider: zen` or `provider: go` will pass config validation but fail at the Registry lookup with 500. This is the right behavior — the dispatcher's 500 message tells the user "you configured a provider that has no adapter yet", which is more informative than failing silently with 501.

#### 14. Update `main.go` to build the Registry and run the eager env-var check

**File**: `main.go`

**Intent**: Build the Registry with the four registered adapters (`nim`, `custom`, `openai`, `anthropic`; `zen` and `go` return 500/501 from the dispatcher). Run the eager env-var check loop after `config.Load`, extracted into a testable helper so the helper is covered by the Phase 1 unit test step 1.28.

**Contract**:
- Define an unexported helper in `main.go`:
  ```go
  func checkRequiredEnvVars(cfg *config.Config) error {
      for _, name := range []string{"nim", "zen", "go"} {
          envVar := config.ProviderEnvVar(name)
          if envVar == "" { continue }
          if cfg.UsesProvider(name) && os.Getenv(envVar) == "" {
              return fmt.Errorf("%s env var required (config references provider=%s)", envVar, name)
          }
      }
      for name, m := range cfg.Models {
          if m.APIKeyEnv != "" && os.Getenv(m.APIKeyEnv) == "" {
              return fmt.Errorf("%s env var required (config model %q references it)", m.APIKeyEnv, name)
          }
      }
      for name, m := range cfg.Mappings {
          if m.APIKeyEnv != "" && os.Getenv(m.APIKeyEnv) == "" {
              return fmt.Errorf("%s env var required (config mapping %q references it)", m.APIKeyEnv, name)
          }
      }
      return nil
  }
  ```
  Returns a descriptive `error` on first miss; `nil` when all required env vars are set. The helper uses `t.Setenv`-friendly `os.Getenv` calls so a unit test can drive every branch without sub-process overhead.
- In `main.go.run()`, call the helper after `config.Load`:
  ```go
  if err := checkRequiredEnvVars(cfg); err != nil {
      return failf("freedius: %s", err)
  }
  ```
- Build the Registry: `registry := proxy.NewRegistry(map[string]proxy.Provider{ "nim": proxy.NewNIMAdapter(logger), "custom": proxy.NewCustomAdapter(logger), "openai": proxy.NewOpenAICompatibleAdapter(logger), "anthropic": proxy.NewAnthropicCompatibleAdapter(logger) })`.
- Pass the Registry to `NewDispatcher`: `proxy.NewDispatcher(cfg, registry, logger)`.

The Registry only constructs adapters for the four registered names. `zen` and `go` are not in the Registry; the dispatcher returns 500 ("provider not registered: zen") for any model mapping to them. This is correct for S-02 — S-03 adds Zen/Go adapters.

#### 15. Update `config.example.yaml` to document the new schema

**File**: `config.example.yaml`

**Intent**: Replace the F-01 stub with a config that demonstrates the new schema: a `models:` entry with the new fields, a `mappings:` entry with the family-name structure (Phase 2 makes it functional; Phase 3 makes family patterns work).

**Contract**:
```yaml
models:
  # Direct model mapping (S-01 power-user path)
  claude-opus-4:
    provider: nim
    model: meta/llama-3.1-70b-instruct
    # api_key_env defaults to NIM_API_KEY via in-binary defaults

  # Custom Anthropic-compatible provider
  my-sonnet-shim:
    provider: custom
    model: sonnet-shim-v1
    base_url: https://my-shim.example.com/v1/messages
    api_key_env: CUSTOM_API_KEY

mappings:
  # Family-based routing (Phase 3 makes these work; Phase 2 supports literal key lookup)
  opus:
    provider: nim
    model: meta/llama-3.1-70b-instruct
  default:
    provider: nim
    model: meta/llama-3.1-70b-instruct
```

This file is both documentation and a smoke-test fixture. Each phase's manual verification uses it.

#### 16. Update tests

**File**: `config/config_test.go`, `proxy/proxy_test.go`, `proxy/custom_test.go` (new), `proxy/nim_test.go` (new), `proxy/openai_compat_test.go` (new), `proxy/anthropic_compat_test.go` (new), `proxy/translate/anthropic_openai_test.go` (new)

**Intent**: Keep the test suite green and add new cases for Phase 1's contract changes.

**Contract**:
- `config/config_test.go:TestLoad`: add 6-8 cases — `provider=openai` without `base_url` (error), `provider=anthropic` without `base_url` (error), `base_url` with `ftp://` scheme (error), `api_key_env` with newline (error), valid `nim` (no `base_url`, no `api_key_env` — uses defaults), valid `custom` (alias rewrites to `anthropic`, has `base_url` and `api_key_env`), valid `mappings:` block (literal key in Phase 2), empty both `models:` and `mappings:` (error). Update `TestKnownProviders` to expect the extended 6-name closed set.
- `proxy/proxy_test.go`: update `newTestDispatcher` to pass a `*Registry`. Add a test case "provider not registered" asserting the 500 response with the `provider not registered:` message. Add a test case for the `mappings:` literal-key lookup (Phase 2).
- `proxy/custom_test.go`: 5-7 cases mirroring the S-01 design — passthrough text, passthrough streaming, upstream 401, upstream 500, missing env var, missing `base_url`, client disconnect.
- `proxy/openai_compat_test.go`: 5-7 cases — translation text, translation streaming, translation tool-use, upstream 401, upstream 429, transport error, client disconnect. Uses `httptest.NewServer` for the mock upstream.
- `proxy/anthropic_compat_test.go`: 5-7 cases mirroring `proxy/custom_test.go`. After Phase 1, `CustomAdapter` is a thin wrapper over `AnthropicCompatibleAdapter`; the tests should cover both adapters or share test code.
- `proxy/translate/anthropic_openai_test.go`: golden-file tests for `TranslateRequest` and `TranslateStream` per the S-01 design.
- `proxy/nim_test.go`: 5-7 cases — non-streaming text response, streaming text response, streaming tool-use, upstream 401, transport error, client disconnect. After Phase 1, `NIMAdapter` is a thin wrapper; the test surface is smaller than the S-01 plan's NIM tests because the translation internals are tested in `openai_compat_test.go`.

### Success Criteria:

#### Automated Verification:

- `make ci` is green
- `go test -race -cover ./config/...` — coverage ≥ 90%
- `go test -race -cover ./proxy/...` — coverage ≥ 85% (the new Registry lookup path, the new adapters)
- `go test -race -cover ./proxy/translate/...` — coverage ≥ 90% (golden-file tests)
- `go vet ./...` — clean
- `go build ./...` — succeeds
- `govulncheck ./...` — no new vulnerabilities
- New test cases for: `provider=openai` without `base_url` (error), `provider=anthropic` without `base_url` (error), invalid `base_url` scheme (error), invalid `api_key_env` (error), valid `nim` with defaults (passes), valid `custom` alias rewrite (passes), provider-not-registered 500 path, env-var-missing startup failure, all custom/openai/anthropic/nim adapter cases, all translation cases

#### Manual Verification:

- Start `./freedius` with the new `config.example.yaml`. Verify: `curl -X POST http://127.0.0.1:8080/v1/messages -H 'content-type: application/json' -d '{"model":"claude-opus-4"}'` returns 200 (or an upstream error) from the NIM adapter, with `X-Freedius-Matched-Provider: nim` header.
- Same with `model: my-sonnet-shim` → routes through the custom/Anthropic passthrough.
- With `NIM_API_KEY` unset: `./freedius` fails to start with a clear "NIM_API_KEY env var required" message.
- With `provider: zen` in the config: dispatcher returns 500 with `provider not registered: zen`.
- A `models:` config with `provider: openai` and no `base_url` fails at startup with a clear error.
- A `models:` config with `provider: openai`, `base_url: https://api.openai.com/v1/chat/completions`, `api_key_env: OPENAI_API_KEY` and the env var unset fails at startup with a clear error (per-model override check).
- `go test -race -cover ./...` shows the coverage targets met.

**Implementation Note**: After completing this phase and all automated verification passes, pause here for manual confirmation that the manual testing was successful before proceeding to Phase 2.

---

## Phase 2: `mappings:` block routing (literal key lookup)

### Overview

Extend the dispatcher to consult the `mappings:` block as a literal-key lookup. After Phase 1, the dispatcher only checks `cfg.Models` for exact match. Phase 2 adds the `mappings:` lookup: if the request's model name appears as a key in `mappings:` (verbatim string match), route through the mapped provider/model. End of Phase 2: a config with only `mappings:` works for users who write `model: opus` (or any other family name) in their Claude Code request. Family pattern matching is Phase 3.

### Changes Required:

#### 1. Add the `mappings:` lookup to the dispatcher

**File**: `proxy/proxy.go`

**Intent**: After the `cfg.Models[req.Model]` lookup at line 76, add a fallback to `cfg.Mappings[req.Model]`. If the key exists, use it as the dispatch target (same as if it had been in `models:`).

**Contract**:
```go
m, ok := d.Cfg.Models[req.Model]
if !ok {
    m, ok = d.Cfg.Mappings[req.Model]
    if !ok {
        d.Logger.Debug("no match for model", "model", req.Model)
        d.writeJSON(w, http.StatusNotFound, map[string]string{"status": "no_match"})
        return
    }
}
```

The `mappings:` entry is treated identically to a `models:` entry for the rest of `ServeHTTP` — the matched-headers, the registry lookup, the adapter call. The only difference is the source map.

Phase 3 will insert a family-pattern match between the `models` exact match and the `mappings` exact-key match. Phase 2's structure is intentionally ordered: `models` first (S-01 power-user path), `mappings` second (S-02 family path).

#### 2. Validate `mappings:` entries with the same per-entry rules as `models:`

**File**: `config/config.go`

**Intent**: Run the same per-entry validation loop over `cfg.Mappings` as over `cfg.Models`. The user shouldn't be able to slip an invalid `mappings:` entry past the loader.

**Contract**: After the existing `for name, m := range cfg.Models` loop, add `for name, m := range cfg.Mappings { ... }` with the same validation rules. The error message format is `"config: config file at <path>: mapping %q has ..."` (uses "mapping" instead of "model" to distinguish the source).

#### 3. Update tests

**File**: `config/config_test.go`, `proxy/proxy_test.go`

**Intent**: Add coverage for the `mappings:` literal-key lookup and the per-entry validation.

**Contract**:
- `config/config_test.go:TestLoad`: add cases — valid `mappings:` with one entry (passes), `mappings:` with `provider: openai` and no `base_url` (error), `mappings:` with invalid `base_url` scheme (error), `mappings:` with `api_key_env` containing `=` (error), empty `mappings:` block (passes — empty map is valid), empty both `models:` and `mappings:` (error), `models:` empty but `mappings:` non-empty (passes — the "at least one" rule).
- `proxy/proxy_test.go:TestServeHTTP`: add cases — POST with a model name that exactly matches a `mappings:` key returns 200 (or upstream error) with the mapped provider's response, POST with a model name that is not in `models:` and not in `mappings:` returns 404 (unchanged from F-01), POST with a model name that is in both `models:` and `mappings:` prefers `models:` (S-01 power-user path wins).

### Success Criteria:

#### Automated Verification:

- `make ci` is green
- `go test -race -cover ./config/...` — coverage ≥ 90%
- `go test -race -cover ./proxy/...` — coverage ≥ 85%
- `go vet ./...` — clean
- `go build ./...` — succeeds
- New test cases for: valid `mappings:` block, `mappings:` with invalid `provider` (error), `mappings:` with `provider: openai` and no `base_url` (error), `mappings:` with invalid `base_url` scheme (error), `mappings:` with `api_key_env` containing `=` (error), empty both blocks (error), `models:` empty but `mappings:` non-empty (passes), dispatch routes through `mappings:` key match, dispatch returns 404 when neither `models` nor `mappings` matches, dispatch prefers `models:` over `mappings:` when both have the key

#### Manual Verification:

- With a config like:
  ```yaml
  mappings:
    opus:
      provider: custom
      model: sonnet-shim-v1
      base_url: https://my-shim.example.com/v1/messages
      api_key_env: CUSTOM_API_KEY
  ```
  `curl -X POST http://127.0.0.1:8080/v1/messages -d '{"model":"opus"}'` routes to the custom adapter. `curl -d '{"model":"claude-opus-4-1"}'` returns 404 (family pattern matching is Phase 3; this is correct — `claude-opus-4-1` is not a literal key in `mappings:`).
- A config with `models: claude-opus-4-1: { provider: nim, model: foo }` and `mappings: claude-opus-4-1: { provider: custom, model: bar, base_url: ... }`: `curl -d '{"model":"claude-opus-4-1"}'` routes through `models:` (the S-01 power-user path wins). This is the documented escape-hatch behavior.
- `go test -race -cover ./...` shows the coverage targets met.

**Implementation Note**: After completing this phase and all automated verification passes, pause here for manual confirmation that the manual testing was successful before proceeding to Phase 3.

---

## Phase 3: Family patterns + tests

### Overview

Add the hardcoded family pattern table in `proxy/families.go`. The dispatcher runs family pattern matching against the request's model name when neither `models` nor `mappings` (literal key) matched. The pattern table is the source of truth for which model-name substrings map to which family. End of Phase 3: a user's five-line `mappings:` config works as designed — `claude-opus-4-1` matches `opus`, `claude-sonnet-4-6` matches `sonnet`, etc.

### Changes Required:

#### 1. Add `proxy/families.go` with the family pattern table

**File**: `proxy/families.go` (new)

**Intent**: The single source of truth for family names and their patterns. The dispatcher's family lookup uses this table.

**Contract**:
```go
package proxy

import "regexp"

type familyPattern struct {
    name    string
    pattern *regexp.Regexp
}

var knownFamilies = []familyPattern{
    {name: "opus", pattern: regexp.MustCompile(`(?i)opus`)},
    {name: "sonnet", pattern: regexp.MustCompile(`(?i)sonnet`)},
    {name: "haiku", pattern: regexp.MustCompile(`(?i)haiku`)},
    {name: "auto", pattern: regexp.MustCompile(`(?i)auto`)},
    {name: "default", pattern: regexp.MustCompile(``)}, // matches anything (catch-all)
}

func extractFamily(modelName string) (string, bool) {
    for _, f := range knownFamilies {
        if f.pattern.MatchString(modelName) {
            return f.name, true
        }
    }
    return "", false
}
```

The `default` family has an empty pattern (matches everything). The user's `mappings:` block must include a `default:` entry for the catch-all to be active; otherwise unmatched model names 404 (per the dispatcher lookup chain).

The slice order is the priority order: `opus` > `sonnet` > `haiku` > `auto` > `default`. A model name like `claude-haiku-sonnet-2024` matches `sonnet` first (higher priority), then `haiku` — the slice order resolves the ambiguity.

#### 2. Add family-pattern lookup to the dispatcher

**File**: `proxy/proxy.go`

**Intent**: Insert a family-pattern match between the `mappings` literal-key lookup and the 404. If the model name matches a family pattern AND the user has a `mappings:` entry for that family, route through the mapped provider/model.

**Contract**:
```go
m, ok := d.Cfg.Models[req.Model]
if !ok {
    m, ok = d.Cfg.Mappings[req.Model]
    if !ok {
        if family, found := extractFamily(req.Model); found {
            m, ok = d.Cfg.Mappings[family]
        }
    }
}
if !ok {
    d.Logger.Debug("no match for model", "model", req.Model)
    d.writeJSON(w, http.StatusNotFound, map[string]string{"status": "no_match"})
    return
}
```

The `extractFamily` function returns the highest-priority family whose pattern matches. The `mappings[family]` lookup is the same as Phase 2's `mappings[req.Model]` lookup, just with a different key.

A user with `mappings: opus: { provider: go, model: deepseek-v4-pro }` and a request for `claude-opus-4-1` gets routed to `go` / `deepseek-v4-pro`. A user with no `mappings: opus:` entry and a request for `claude-opus-4-1` falls through to 404.

#### 3. Update `config.example.yaml` to demonstrate the family mappings

**File**: `config.example.yaml`

**Intent**: Replace the Phase 2 example (which only used literal keys) with a realistic five-line family mapping that shows the S-02 mental model.

**Contract**:
```yaml
mappings:
  opus:    { provider: go,  model: deepseek-v4-pro }
  sonnet:  { provider: go,  model: deepseek-v4-flash }
  haiku:   { provider: nim, model: step-3.5 }
  auto:    { provider: nim, model: step-3.5 }
  default: { provider: nim, model: step-3.5 }
```

This is the "after S-02" example from the research. Five lines, every Claude Code model name routes correctly.

#### 4. Add tests for `extractFamily` and the dispatcher family-pattern lookup

**File**: `proxy/families_test.go` (new), `proxy/proxy_test.go` (additions)

**Intent**: Cover the family pattern table and the dispatcher's family-pattern lookup.

**Contract**:
- `proxy/families_test.go`: table-driven `TestExtractFamily` covering:
  - Each family pattern matches the canonical examples from the research (`claude-opus-4-1` → `opus`, `claude-sonnet-4-6` → `sonnet`, `claude-haiku-3-5` → `haiku`, `auto` → `auto`)
  - Case-insensitive matching (`CLAUDE-OPUS-4-1` → `opus`, `Claude-Sonnet-4-6` → `sonnet`)
  - Priority resolution (`claude-haiku-sonnet-2024` → `sonnet` because `sonnet` is higher priority than `haiku`)
  - `default` family matches anything when present (`claude-future-model-2026` → `default`)
  - Unmatched model returns `("", false)` when no `default` family is in scope (note: `extractFamily` itself doesn't know about `mappings:` — it always returns the highest-priority match; the dispatcher checks if `mappings[family]` exists)
- `proxy/proxy_test.go:TestServeHTTP`: add cases — POST with `model: claude-opus-4-1` and `mappings: opus:` configured routes through the `opus` mapping, POST with `model: claude-opus-4-1` and no `mappings: opus:` (and no `default:`) returns 404, POST with `model: claude-unknown-2026` and `mappings: default:` configured routes through the `default` mapping, POST with `model: claude-opus-4-1` matches `mappings: opus:` even when `mappings: auto:` is listed first in the YAML (priority is fixed by code order, not YAML order).

### Success Criteria:

#### Automated Verification:

- `make ci` is green
- `go test -race -cover ./config/...` — coverage ≥ 90%
- `go test -race -cover ./proxy/...` — coverage ≥ 85% (the new family lookup path)
- `go vet ./...` — clean
- `go build ./...` — succeeds
- `govulncheck ./...` — no new vulnerabilities
- New test cases for: all 5 family patterns (opus, sonnet, haiku, auto, default), case-insensitive matching, priority resolution (sonnet beats haiku), unmatched model with no default returns 404, unmatched model with default configured routes through default, priority is independent of YAML order, exact-match `models:` still wins over family match (regression test)

#### Manual Verification:

- With the new `config.example.yaml` (five-line family mapping), run a real `claude-code` session with `ANTHROPIC_BASE_URL=http://127.0.0.1:8080`:
  - Make a request that triggers `opus` (e.g., explicitly request `claude-opus-4-1` in the Claude Code prompt). Confirm the request routes through the `go` provider and gets a response.
  - Make a request that triggers `sonnet`. Confirm routing.
  - Make a request that triggers `haiku`. Confirm routing.
  - Make a request that triggers `auto` (Claude Code's default model). Confirm routing.
  - Make a request with a model name that doesn't match any specific family (e.g., `claude-future-2026`). Confirm the `default` mapping catches it.
  - Confirm tool use, streaming, and multi-turn all work.
- With a config that has `mappings: opus:` but no `default:`: a request for `claude-unknown-2026` returns 404. The dispatcher does not invent a default.
- With a config that has both `models: claude-opus-4-1:` and `mappings: opus:`: a request for `claude-opus-4-1` routes through `models:` (the S-01 power-user path). A request for `claude-opus-4-5` (a different opus version not in `models:`) routes through `mappings: opus:`.
- `go test -race -cover ./...` shows the coverage targets met.
- `test-manual.sh` smoke tests pass (the script needs Phase 3 updates for the new schema; see Testing Strategy below).

**Implementation Note**: After completing this phase and all automated verification passes, S-02 is done. The next step is `/10x-impl-review provider-and-mapping` to audit the implementation against this plan, then `/10x-archive` to close the change. The `context/foundation/lessons.md` file should be created (if it doesn't exist) with at least one entry: the SSE footguns (json.Encoder newline, bufio.Scanner 64KB cap) captured for future reference.

---

## Testing Strategy

### Unit Tests

- `config/config_test.go` — extends `TestLoad` and `TestKnownProviders` for the new `Mappings` field, the extended 6-name `KnownProviders` set, the in-binary defaults, the `custom` → `anthropic` alias rewrite, the "at least one of models/mappings" rule, and the per-entry validation for both `models:` and `mappings:` blocks.
- `proxy/proxy_test.go` — extends `newTestDispatcher` to pass a `*Registry`; adds the `mappings:` literal-key lookup, the family-pattern lookup, and the `models:`-wins-over-`mappings:` precedence tests.
- `proxy/families_test.go` (new) — table-driven `TestExtractFamily` covering all 5 patterns, case-insensitivity, priority resolution, and unmatched model behavior.
- `proxy/provider_test.go` (new) — covers `NewRegistry` (panics on nil entries) and `Lookup` (returns the right adapter, returns `(_, false)` for unknown names).
- `proxy/errors_test.go` (new) — covers `forwardUpstreamError` (status/headers/body passthrough) and `freediusErrorHandler` (silent on `context.Canceled`, writes 502 on other errors).
- `proxy/custom_test.go` (new) — 5-7 cases for the Anthropic passthrough (text, streaming, upstream 401/500, missing env var, missing `base_url`, client disconnect).
- `proxy/anthropic_compat_test.go` (new) — same shape as `custom_test.go`. May share test code with `custom_test.go` via a helper.
- `proxy/nim_test.go` (new) — 5-7 cases for the NIM-as-preset wrapper.
- `proxy/openai_compat_test.go` (new) — 5-7 cases for the OpenAI translation adapter (text, streaming, tool-use, upstream 401/429, transport error, client disconnect).
- `proxy/translate/anthropic_openai_test.go` (new) — golden-file tests for `TranslateRequest` and `TranslateStream` per the S-01 design (text-only, single tool, parallel tools, error mid-stream, `[DONE]` only, usage chunk, content filter).
- `proxy/translate/testdata/` (new) — JSON and SSE fixtures for the translation tests.

### Integration Tests

- None in CI (per S-01 design). The `httptest.NewServer`-based tests are the integration tests — they exercise the full path from the dispatcher through the adapter to a mock upstream.
- Manual smoke tests against real NIM, a real custom Anthropic-compatible shim, and a real `claude-code` session are the "true" integration tests, deferred to the user per the per-phase Manual Verification sections.

### Manual Testing Steps

Each phase has a per-phase Manual Verification section that lists the specific `curl` commands and real `claude-code` sessions the user runs to confirm the slice works end-to-end. The Phase 3 manual verification is the most rigorous: it requires a real `claude-code` session routed through freedius.

`test-manual.sh` (existing, F-01 smoke tests) needs Phase 3 updates:
- Update the F-01 501/404/400 assertions for the new schema (the `X-Freedius-Matched-Provider: nim` header still appears, but the status is 200/upstream-error from a real adapter, not 501)
- Add a new section for the family-pattern lookup: with the new `config.example.yaml`, `curl` with `model: claude-opus-4-1` and verify the response routes through the `go` provider
- Add a new section for the "at least one of models/mappings" rule: a config with only `mappings:` and an unknown model returns 404
- Add a new section for the `custom` alias: a config with `provider: custom` is accepted and routes through the `anthropic` adapter

## Performance Considerations

- **NFR-Latency ("imperceptible overhead")**: the inline single-goroutine SSE translation adds at most one `bufio.ReadBytes` + one `json.Marshal` + one `fmt.Fprintf` per chunk. Per-chunk overhead is in the low microseconds; the dominant latency is the upstream LLM call. The Anthropic passthrough adapter adds effectively zero overhead (`httputil.ReverseProxy` is an `io.Copy` with header rewriting).
- **NFR-Multi-agent**: `*http.Client` and `*httputil.ReverseProxy` are both safe for concurrent use. Adapters are constructed once at startup and shared. Per-request state (the `bufio.Reader`, the `anthropicEmitter` instance) is allocated locally per `Handle` call. No locks, no atomics.
- **NFR-Resource-footprint (sub-50MB idle, negligible CPU)**: the new adapter files (`openai_compat.go`, `anthropic_compat.go`) add maybe 2KB of static memory. The `bufio.Reader` allocated per request is freed when `Handle` returns. The `httputil.ReverseProxy` per-request construction in `AnthropicCompatibleAdapter.Handle` is intentional — a `*ReverseProxy` is ~100 bytes, freed when the goroutine returns.
- **NFR-Privacy (no body logging)**: existing F-01 behavior carries over. None of the new adapter files log request or response bodies. The dispatcher logs only the matched provider/model (no payload).
- **NFR-Error-handling (provider errors forwarded visibly)**: verbatim upstream passthrough is the design. The dispatcher adds 502 only for transport-level failures (when the upstream is unreachable), not for upstream 4xx/5xx.
- **Dispatch lookup is O(1) for `models` exact match + O(1) for `mappings` literal-key lookup + O(F) for family pattern match** where F = 5 (the `knownFamilies` slice length). In practice, the family pattern match runs 5 regex matches, each against a short substring — well under a microsecond. Negligible overhead.

## References

- Research: `context/changes/provider-and-mapping/research.md` (S-02 entry point; bundles Follow-ups #2, #3, #4 from the S-03 research)
- Detailed designs (in the S-03 research): `context/changes/zen-go-adapters/research.md`
  - Follow-up #1: Unified `OPENCODE_API_KEY` (inherited; no S-02 work)
  - Follow-up #2: Two-tier provider model
  - Follow-up #3: In-binary defaults
  - Follow-up #4: Family-aware mapping
- S-01 plan (folded into Phase 1): `context/changes/first-call-routed/plan.md`
- S-01 research (folded into Phase 1): `context/changes/first-call-routed/research.md`
- F-01 plan (foundation): `context/changes/proxy-skeleton/plan.md`
- F-01 review (hardening inherited): `context/changes/proxy-skeleton/reviews/impl-review.md`
- PRD: `context/foundation/prd.md` (FR-001, FR-003, FR-004, FR-009, NFR-Error-handling)
- Roadmap: `context/foundation/roadmap.md` (S-02 row, the "Open Questions" section)
- AGENTS.md: `/home/pawel/code/freedius/AGENTS.md` (no comments, `gofumpt`, env-var config, test conventions, Go 1.22+ patterns)
- Go stdlib docs (Context7 query for `httputil.ReverseProxy`): `https://pkg.go.dev/net/http/httputil#ReverseProxy`

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles. See `references/progress-format.md`.

### Phase 1: Schema + provider foundation (S-01 work + S-02 schema/defaults/alias/compat)

#### Automated

- [x] 1.1 Add `BaseURL` and `APIKeyEnv` to `config.Model`; add `Mappings map[string]Model` to `Config` (`config/config.go`) — d06211d
- [x] 1.2 Extend `KnownProviders` to `{nim, zen, go, custom, openai, anthropic}`; extend per-entry validation (`config/config.go`) — d06211d
- [x] 1.3 Add "at least one of models/mappings" rule to `config.Load` (`config/config.go`) — d06211d
- [x] 1.4 Add `config/defaults.go` with `knownProviderDefaults` map and `(*Config).applyDefaults()` (alias rewrite + defaults merge) — d06211d
- [x] 1.5 Add `(*Config).UsesProvider(name) bool` method (`config/config.go`) — d06211d
- [x] 1.6 Add `proxy/provider.go` with `Provider` interface, `Registry` type, `NewRegistry`, `Lookup` — d06211d
- [x] 1.7 Add `proxy/errors.go` with `forwardUpstreamError` and `freediusErrorHandler` — d06211d
- [x] 1.8 Add `proxy/anthropic_compat.go` with `AnthropicCompatibleAdapter` — d06211d
- [x] 1.9 Add `proxy/openai_compat.go` with `OpenAICompatibleAdapter` — d06211d
- [x] 1.10 Add `proxy/translate/` package with `TranslateRequest`, `TranslateStream`, `anthropicEmitter` (`types.go`, `anthropic_openai.go`) — d06211d
- [x] 1.11 Add `proxy/translate/testdata/` with golden-file fixtures — d06211d
- [x] 1.12 Add `proxy/translate/anthropic_openai_test.go` with golden-file tests for `TranslateRequest` and `TranslateStream` — d06211d
- [x] 1.13 Add `proxy/custom.go` with `CustomAdapter` (thin wrapper over `AnthropicCompatibleAdapter`) — d06211d
- [x] 1.14 Add `proxy/nim.go` with `NIMAdapter` (thin wrapper over `OpenAICompatibleAdapter`) — d06211d
- [x] 1.15 Update `Dispatcher` struct + `NewDispatcher` to take a `*Registry`; replace 501 stub with Registry lookup (`proxy/proxy.go`) — d06211d
- [x] 1.16 Update `main.go` to build the Registry, run the eager env-var check loop, register `nim`/`custom`/`openai`/`anthropic` — d06211d
- [x] 1.17 Update `config.example.yaml` with the new schema (`base_url`, `api_key_env`, new providers) — d06211d
- [x] 1.18 Add config test cases: extended `KnownProviders` (6 names), `provider=openai`/`anthropic` without `base_url` (error), invalid `base_url` scheme (error), invalid `api_key_env` (error), valid `nim` with defaults (passes), valid `custom` alias rewrite (passes), empty both blocks (error) — d06211d
- [x] 1.19 Update `newTestDispatcher` to construct a Registry; add "provider not registered" 500 test case — d06211d
- [x] 1.20 Add `proxy/provider_test.go` with `NewRegistry` and `Lookup` tests — d06211d
- [x] 1.21 Add `proxy/errors_test.go` with `forwardUpstreamError` and `freediusErrorHandler` tests — d06211d
- [x] 1.22 Add `proxy/custom_test.go` with the 5-7 Anthropic passthrough cases — d06211d
- [x] 1.23 Add `proxy/anthropic_compat_test.go` with the 5-7 Anthropic compat adapter cases — d06211d
- [x] 1.24 Add `proxy/openai_compat_test.go` with the 5-7 OpenAI translation cases — d06211d
- [x] 1.25 Add `proxy/nim_test.go` with the 5-7 NIM wrapper cases — d06211d
- [x] 1.26 Run `make ci` — all green, coverage ≥ 90% config / ≥ 85% proxy / ≥ 90% translate — d06211d
- [x] 1.27 Run `govulncheck ./...` — no new vulnerabilities — d06211d
- [x] 1.28 Add `main_test.go` unit test for `checkRequiredEnvVars` helper: missing preset env var (e.g. `NIM_API_KEY` unset with `provider: nim` referenced) returns the expected error string; missing per-model `api_key_env` (e.g. `OPENAI_API_KEY` unset on a `provider: openai` model) returns the expected error string; happy path with all vars set returns nil; case where the config references a preset provider that has no default env var (e.g. `provider: custom` is rewritten to `anthropic` which has no default — pass through silently). Use `t.Setenv` so the test is hermetic — d06211d

#### Manual

- [x] 1.29 Verify the new schema + adapters + alias via `curl` per Phase 1 Manual Verification — commit sha
- [x] 1.30 Verify eager env-var check (missing NIM_API_KEY, missing per-model api_key_env) — commit sha
- [x] 1.31 Verify `provider: zen` and `provider: go` return 500 "provider not registered" — commit sha

### Phase 2: `mappings:` block routing (literal key lookup)

#### Automated

- [x] 2.1 Add `mappings:` literal-key lookup to `Dispatcher.ServeHTTP` (`proxy/proxy.go`) — af38103
- [x] 2.2 Extend per-entry validation to `cfg.Mappings` (`config/config.go`) — af38103
- [x] 2.3 Add config test cases: valid `mappings:`, `mappings:` with invalid provider/error, `mappings:` with `provider: openai` and no `base_url` (error), `mappings:` with invalid `base_url` scheme (error), `mappings:` with `api_key_env` containing `=` (error), empty both blocks (error), `models:` empty but `mappings:` non-empty (passes) — af38103
- [x] 2.4 Add dispatcher test cases: dispatch routes through `mappings:` key match, dispatch returns 404 when neither matches, dispatch prefers `models:` over `mappings:` when both have the key — af38103
- [x] 2.5 Run `make ci` — all green, coverage ≥ 90% config / ≥ 85% proxy — af38103

#### Manual

- [x] 2.6 Verify `mappings:` literal-key routing via `curl` per Phase 2 Manual Verification — commit sha
- [x] 2.7 Verify `models:`-wins-over-`mappings:` precedence (S-01 power-user escape hatch) — commit sha
- [x] 2.8 Verify `claude-opus-4-1` returns 404 (family pattern matching is Phase 3) — commit sha

### Phase 3: Family patterns + tests

#### Automated

- [x] 3.1 Add `proxy/families.go` with `knownFamilies` slice and `extractFamily` function — commit sha
- [x] 3.2 Add family-pattern lookup to `Dispatcher.ServeHTTP` (`proxy/proxy.go`) — commit sha
- [x] 3.3 Update `config.example.yaml` to demonstrate the five-line family mapping — commit sha
- [x] 3.4 Add `proxy/families_test.go` with `TestExtractFamily` (5+ cases per pattern, case-insensitivity, priority resolution, unmatched) — commit sha
- [x] 3.5 Add dispatcher test cases: family pattern routes through `mappings: opus:`, unmatched model with no `default:` returns 404, unmatched model with `default:` routes through it, priority is independent of YAML order, POST with a model present in `models:` AND a matching family in `mappings:` routes through `models:` (Phase 2 precedence regression holds after family patterns land) — commit sha
- [x] 3.6 Run `make ci` — all green, coverage ≥ 90% config / ≥ 85% proxy — commit sha
- [x] 3.7 Run `govulncheck ./...` — no new vulnerabilities — commit sha

#### Manual

- [x] 3.8 Verify family-pattern routing with a real `claude-code` session per Phase 3 Manual Verification — commit sha
- [x] 3.9 Verify `default:` opt-in behavior (no `default:` → 404; `default:` → routes through it) — commit sha
- [x] 3.10 Verify `models:`-wins-over-family-match precedence (S-01 power-user escape hatch still works after Phase 3) — commit sha
- [x] 3.11 Update `test-manual.sh` for the new schema (replace F-01 501/404 assertions, add family-pattern section, add `custom` alias section) — commit sha
- [x] 3.12 Create `context/foundation/lessons.md` with the SSE footguns (json.Encoder newline, bufio.Scanner 64KB cap) and the `custom` → `anthropic` rewrite-in-`applyDefaults` lesson — commit sha

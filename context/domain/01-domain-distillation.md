---
title: "Domain Distillation — freedius"
created: 2026-08-10
type: domain-distillation
---

# Domain Distillation: freedius

> A local HTTP proxy that routes LLM API requests from AI coding agents (Claude Code, OpenCode) to upstream providers — with fallback chains, model-name mapping, and a live web dashboard.

---

## STEP 0 — Project Context

### Source Documents Found

| Document | Path | Role |
|----------|------|------|
| PRD | `context/foundation/prd.md` | Product requirements, functional requirements FR-001–FR-009, non-functional requirements, non-goals |
| Tech Stack | `context/foundation/tech-stack.md` | Stack rationale (Go stdlib) |
| Roadmap | `context/foundation/roadmap.md` | Current state, shipped features, active work |
| Shape Notes | `context/foundation/shape-notes.md` | Original shaping conversation decisions |
| README | `README.md` | User-facing surface, quickstart, config reference |
| Lessons | `context/foundation/lessons.md` | Learned patterns from implementation |
| Provider Spec | `providers.yaml` | Single source of truth for provider metadata |

### Repo Structure (Business Logic Layers)

| Layer | Directory | Responsibility |
|-------|-----------|----------------|
| Entry point | `cmd/freedius/` | CLI flags, server wiring, env-injection |
| Core routing | `proxy/` | `Dispatcher`, adapters, middleware, fallback chain |
| Translation | `proxy/translate/` | Anthropic ↔ OpenAI wire-format conversion |
| Web UI | `proxy/web/` | Embedded dashboard (HTML/JS/CSS via `embed.go`) |
| Config | `config/` | YAML load/validate/persist, provider defaults |
| Persistence | `config/` (Save/Load) | Atomic YAML writes with backup |
| Telemetry | `proxy/eventbus.go`, `proxy/stats_collector.go` | Live event streaming, aggregate counters |
| Codegen | `config/gen.go`, `internal/genproviders/` | `go generate` from `providers.yaml` |

### Stack

Go 1.x, standard library only (`net/http`, `httputil.ReverseProxy`), `goccy/go-yaml` for config, embedded web UI (no framework), `mage` for builds.

---

## STEP 1 — Ubiquitous Language

Each term: **definition**, **source quote**, **code location**.

| Term | Definition | Source Quote | Code Location |
|------|-----------|--------------|---------------|
| **Provider** | A named upstream LLM endpoint with a behavior class, base URL, and API key env var. Many mappings can share one provider. | `config/config.go:58-81` — "Provider describes a single upstream LLM endpoint" | `config/config.go:60-81` (struct `Provider`) |
| **Mapping** | Binds a freedius-facing model name to a Provider plus the upstream model string to request. Holds an ordered fallback list. | `config/config.go:89-102` — "Mapping binds a freedius-facing name to an upstream Provider" | `config/config.go:97-102` (struct `Mapping`) |
| **Behavior** | The wire-protocol class of a provider: `openai`, `anthropic`, or `mix`. Selects which adapter handles the request. | `providers.yaml:8` — "behavior: openai \| anthropic \| mix — runtime adapter class" | `providers.yaml:8` (field docs), `config/config.go:61` |
| **Fallback Chain** | An ordered list of alternate `{ProviderName, ModelString}` targets tried when the primary fails (config error, transport failure, or upstream 4xx/5xx). | `README.md:130-145` — "When the primary fails, freedius tries each fallback in order" | `proxy/proxy.go:289-303` (chain construction + loop) |
| **Adapter** | A backend implementation that serves a request end-to-end (builds upstream request, copies/streams response). One per behavior class. | `proxy/provider.go:11-19` — "Provider is a single backend implementation that can serve a freedius request end-to-end" | `proxy/provider.go:12-19` (interface `Provider`) |
| **MixAdapter** | Protocol-detecting adapter that routes to Anthropic or OpenAI sub-adapter based on `Provider.Protocol` field or URL path suffix. | `proxy/mix.go:14-19` — "MixAdapter routes each request to either the Anthropic or OpenAI code path" | `proxy/mix.go:20-103` (struct + Handle) |
| **OpenAICompatibleAdapter** | Translates Anthropic-format requests to OpenAI Chat Completions format, streams translated SSE response back. | `proxy/openai_compat.go:18-24` — "translates Anthropic-format requests into the OpenAI Chat Completions format" | `proxy/openai_compat.go:20-207` |
| **AnthropicCompatibleAdapter** | Forwards requests to Anthropic-API-compatible upstream, returns `upstreamError` on 4xx/5xx for fallback eligibility. | `proxy/anthropic_compat.go:23-31` — "forwards requests to an Anthropic-API-compatible upstream" | `proxy/anthropic_compat.go:27-222` |
| **Dispatcher** | Top-level HTTP handler that resolves a request to a configured model, looks up the Provider, and forwards via the Registry. | `proxy/proxy.go:39-46` — "Dispatcher is the top-level HTTP handler that resolves a freedius request to a configured model" | `proxy/proxy.go:47-66` |
| **Registry** | Maps provider names to concrete Provider implementations. | `proxy/provider.go:22-26` — "Registry maps provider names to their concrete Provider implementation" | `proxy/provider.go:24-44` |
| **Request ID** | Unique 16-byte hex identifier assigned per request, propagated via `X-Freedius-Request-ID` header and `context.Context`. | `proxy/proxy.go:524-551` — RequestIDMiddleware + generateRequestID | `proxy/proxy.go:524-551` |
| **Config (freedius.yaml)** | YAML file containing providers and mappings. Resolution: `--config` flag → cwd → `~/.config/freedius/` → embedded starter. | `README.md:95-107` — config resolution order | `config/config.go:30-40`, `config/defaults.go:52-61` |
| **Starter Config** | Embedded default config loaded when no config file is found. NIM-primary with per-tier fallback chains. | `README.md:101-107` — "When no config file is found, freedius loads the embedded starter" | `cmd/freedius/templates/starter.yaml` (referenced in README) |
| **Provider Defaults** | Generated metadata (`providerDefaults` map) merged into user's Providers to fill empty fields. Codegen from `providers.yaml`. | `config/providers_gen.go:5-11` — "providerDefaults is the metadata table for known providers" | `config/providers_gen.go:12+`, `config/defaults.go:16-50` |
| **Model Rewriting** | Echoing the client's original model name in the response (stable across fallbacks). | `proxy/anthropic_compat.go:255-275` — rewriteAnthropicModelField | `proxy/anthropic_compat.go:185, 257-275` |
| **Fallback Timeout** | Shared budget for entire fallback chain = `fallbackTimeoutMultiplier × streamTimeout`. Default 2×. | `proxy/proxy.go:52-57` — "fallbackTimeoutMultiplier scales the per-attempt stream timeout" | `proxy/proxy.go:298-300` |
| **UpstreamError** | Classified upstream HTTP error (4xx/5xx) with Anthropic error type. Returned by adapters so dispatcher can fallback. | `proxy/errors.go:33-37` — "classified result of an upstream HTTP error response" | `proxy/errors.go:85-131` (classifyUpstreamError) |
| **ConfigError** | Adapter pre-flight configuration error (missing base_url, missing API key). | `proxy/errors.go:22-25` — "adapter pre-flight configuration error with an Anthropic error.type string" | `proxy/errors.go:22-25` |
| **Privacy Rule** | No request/response payload logged to disk or transmitted beyond target provider. Metadata only. | `proxy/proxy.go:1-6` — "DO NOT log request or response bodies in this file" | Enforced by convention; `proxy/proxy.go:1-6` comment |
| **Provenance (`added_at`)** | Optional free-form string on a mapping indicating when it was added. Rendered on dashboard mapping card. | `README.md:147-160` — "Mappings accept an optional `added_at` free-form string" | `config/config.go:101` |
| **Last Responder** | Per-mapping index of the most recently successful responder step (0 = primary, 1+ = fallback index). TTL-bound (60s). | `proxy/lastresponder.go:13-22` — "per-mapping responder index aggregator" | `proxy/lastresponder.go:26-106` |
| **Token Counting** | Local BPE-based counting for `/v1/messages/count_tokens` when upstream doesn't support it. | `proxy/count_tokens_local.go:24-28` — "runs the local BPE-based counter" | `proxy/count_tokens_local.go:29-67` |
| **NIM Sanitize** | Pre-send hook stripping boolean subschemas from tool `parameters` for NVIDIA NIM compatibility. | `proxy/nim_sanitize.go:5-7` — sanitizeNIMBody | `providers.yaml:21` (pre_send_hook: sanitizeNIMBody), `proxy/nim_sanitize.go:5-72` |
| **EventBus** | Publish/subscribe channel for `RequestEvent` metadata. Ring-buffered (10k) for replay. | `proxy/eventbus.go:30-35` — "decoupled publish/subscribe channel for request metadata events" | `proxy/eventbus.go:34-226` |
| **LogSink** | Bounded channel of pre-rendered log entries (`LogEntry`) for live log streaming. Ring-buffered. | `proxy/logtee.go:26-39` — "bounded channel of pre-rendered log entries" | `proxy/logtee.go:26-278` |
| **StatsCollector** | Subscribes to EventBus, maintains per-mapping/per-provider aggregate counters (request count, error rate, fallback count). | `proxy/stats_collector.go:32-37` — "maintains per-mapping and per-provider aggregate counters" | `proxy/stats_collector.go:38-266` |
| **SSE Stream Translation** | Real-time conversion of OpenAI SSE chunks to Anthropic SSE events via the `emitter`. | `proxy/translate/anthropic_openai.go:394-435` — Stream function | `proxy/translate/anthropic_openai.go:399-435` |
| **Mapping Resolution** | Process: exact model-name match → regex-based mapping-key match (most-specific-first) → `default` catch-all. | `proxy/proxy.go:132-148` — resolveMapping | `proxy/proxy.go:132-148`, `config/config.go:212-239` (BuildMatchers) |
| **Aggregate Provider Fail** | When all fallback entries fail, an aggregated error listing every failed `provider/model (err_type)` is returned. | `proxy/proxy.go:442-471` — writeAggregatedFallbackError | `proxy/proxy.go:442-471` |

---

## STEP 2 — Subdomain Classification

| Concept / Area | Subdomain | Justification |
|---------------|-----------|---------------|
| Mapping resolution (exact + regex + default catch-all) | **Core** | The product's raison d'être: "map any Claude Code model name to any provider model transparently" (PRD FR-003). This is what distinguishes freedius from direct Anthropic usage. |
| Fallback chain execution | **Core** | Explicit success criterion: "fallback chains" are named in the product tagline (README:1-9). Survives provider outages — the key value proposition. |
| Anthropic ↔ OpenAI protocol translation | **Core** | The technical mechanism enabling the product vision (PRD Business Logic:95-99). Without translation, the gateway cannot route to OpenAI-format providers. |
| MixAdapter protocol detection | **Core** | Enables zero-config routing for `mix` providers — key to "dead-simple" setup promise (shape-notes.md:38). |
| Model name rewriting in responses | **Core** | Required for the "transparent to Claude Code" guarantee (PRD Guardrails:40-41). Without it, Claude Code sees wrong model names after fallback. |
| Config resolution (file → env → starter) | **Supporting** | Necessary for UX but not the product's differentiator. Could be replaced without changing the domain. |
| Provider defaults / codegen | **Supporting** | Convenience layer that bootstraps the provider table. Not a product advantage — a build-time optimization. |
| Error classification & redactation | **Supporting** | Required for good UX (NFR: error handling) but not unique to freedius. Standard proxy concern. |
| Request ID generation | **Generic** | Cross-cutting infrastructure. Same pattern in any HTTP service. |
| EventBus / LogSink (telemetry pub-sub) | **Supporting** | Powers the dashboard and observability. Valuable for the "live web dashboard" feature but secondary to routing. |
| StatsCollector (aggregate counters) | **Supporting** | Derived telemetry. Supports dashboard rendering. Not core routing logic. |
| Web dashboard UI | **Supporting** | The PRD explicitly defers web UI to v2 (prd.md:107). README promotes it but it's not the product's primary value. |
| Local token counting | **Supporting** | Feature for OpenAI-protocol upstreams that lack `/count_tokens`. Convenience, not core. |
| NIM sanitize hook | **Generic** | Provider-specific workaround. Not domain-specific; a compatibility patch. |
| Atomic config Save with backup | **Supporting** | Persistence mechanism. Important for config CRUD via UI but not core domain. |
| Env auto-injection (`freedius configure`) | **Supporting** | Convenience feature for wiring up Claude Code. Secondary success criterion (prd.md:36). |

---

## STEP 3 — Aggregate Candidates & Invariants

| # | Aggregate Candidate | Invariant (business rule that MUST hold) | Source Quote | Code Status |
|---|--------------------|----------------------------------------|--------------|-------------|
| 1 | **Mapping + Fallback Chain** | A mapping must always resolve to at least one reachable provider-target; if the primary fails, fallbacks are tried in order; if all fail, a descriptive aggregate error is returned. | `README.md:130-145` — "When the primary fails, freedius tries each fallback in order" | **ENFORCED** — `proxy/proxy.go:289-414` (chain loop + aggregation) |
| 2 | **Config (Providers + Mappings)** | Every mapping must reference a known provider; no orphan mappings. Config errors must not crash the gateway. | `prd.md:41-42` — "Config errors produce a clear error message but do not crash the gateway" | **ENFORCED** — `config/config.go:382-473` (validateMapping references known providers) |
| 3 | **Provider Registry** | Every behavior class referenced by a provider must have a registered adapter; a missing adapter must surface as `provider_not_registered`. | `proxy/proxy.go:326-343` — fallback behavior lookup + error | **ENFORCED** — `proxy/proxy.go:326-343` |
| 4 | **Request → Mapping → Provider chain** | Every accepted request must produce exactly one matched mapping; matched mapping must resolve to exactly one provider; ambiguity is resolved by most-specific-first matching. | `prd.md:67` — "the mapping is transparent to Claude Code" | **ENFORCED** — `proxy/proxy.go:132-148` (resolveMapping) + `config/config.go:212-239` |
| 5 | **Privacy (no body logging)** | Request/response payloads (message content, tool arguments, tool results, API responses) must never be logged. Metadata (model, provider, status, request_id) is allowed. | `prd.md:90` — "no request or response payload is logged to disk or transmitted beyond the target provider" | **DECLARED** — File-level comment `proxy/proxy.go:1-6` + `proxy/translate/anthropic_openai.go:1-5`. No static enforcement; relies on convention. |
| 6 | **Single-user access** | No auth, no multi-tenancy. Gateway accepts any API key (dummy). Real keys live in local env vars. | `prd.md:101-103` — "Single user; no auth... One flat config — no roles, no multi-user" | **ENFORCED** — No auth middleware on proxy port; dashboard optionally gated by `FREEDIUS_UI_TOKEN` |
| 7 | **Fallback chain non-recursion** | Fallback entries cannot themselves have fallbacks. The chain is strictly one level deep. | `config/config.go:94-95` — "the Fallback field on fallback entries is always nil (no recursive chaining)" | **ENFORCED** — Struct design (`Mapping.Fallback []Mapping` but fallback entries loaded without nested Fallback) |
| 8 | **Model field stability** | The `model` field in the response must always reflect the client's requested model, regardless of which fallback provider served the request. | `proxy/anthropic_compat.go:163-164` — originalModel from context; `proxy/translate/anthropic_openai.go:399` — modelOverride | **ENFORCED** — `proxy/anthropic_compat.go:185, 257-275` + `proxy/translate/anthropic_openai.go:399, 486-494` |
| 9 | **Concurrent session isolation** | Multiple concurrent Claude Code sessions must not interfere, leak state, or mix requests. | `prd.md:89` — "freedius handles concurrent Claude Code sessions without interference, state leak, or request mixing" | **ENFORCED** — Stateless adapters; per-request context carries model. RWMutex on shared Config. |
| 10 | **Error type fidelity** | Provider errors must be classified into Anthropic error types (`rate_limit_error`, `overloaded_error`, `api_error`, `authentication_error`, `invalid_request_error`) and forwarded visibly. | `prd.md:88` — "provider errors are forwarded to Claude Code as descriptive messages" | **ENFORCED** — `proxy/errors.go:100-123` (classifyUpstreamError switch) |

---

## STEP 4 — Model vs Code Discrepancies

| # | Document Says | Code Does | Evidence |
|---|--------------|-----------|----------|
| 1 | "Config errors do not crash the gateway" (prd.md:41-42) | Config `Load()` returns an error that propagates to `main()` and exits — the process does not start. | `config/config.go:105-124` — `Load()` returns error on validate failure; `cmd/freedius/main.go` (typical pattern) exits. No "serve partial config" fallback. |
| 2 | "The web UI is a v2 concern" / "No web UI in v1" (prd.md:107) | A full embedded web UI exists with CRUD, live logs, dashboard, mapping visualization. | `proxy/web/` directory with 20+ files. README promotes dashboard prominently. PRD is stale relative to implementation. |
| 3 | "Dev configures provider credentials via environment variables and model mappings in a config file" (prd.md:69) | API keys are read from env vars at request time, but `default_api_key_env` is also stored in generated defaults and merged at load. The env var name effectively becomes part of config state. | `config/defaults.go:33-35` — `p.DefaultAPIKeyEnv = defaults.DefaultAPIKeyEnv`. The boundary between "config" and "env" is blurred. |
| 4 | "freedius tries each fallback in order" (README.md:131) | The fallback chain loop breaks early when `ww.wroteHeader` is true — meaning if an adapter writes headers but then fails, the chain does not continue. | `proxy/proxy.go:352-370` — `if err == nil \|\| ww.wroteHeader { return }`. A partial write commits the response. |
| 5 | "All requests proxy transparently" / "Claude Code cannot tell the difference" (prd.md:65-66, 40-41) | Body size is capped at 10MB (`MaxBodyBytes`). Large Anthropic requests (e.g., long context) get a 413 instead of transparent proxying. | `proxy/proxy.go:29-30` — `MaxBodyBytes = 10 * 1024 * 1024`. PRD has no body-size limit mentioned. |
| 6 | "No request or response payload is logged to disk" (prd.md:90) | The LogSink captures all structured logs including error messages from upstream (`ErrorMessage` field in `RequestEvent`). Error message content may contain snippets of upstream responses. | `proxy/errors.go:89` — `msg := sanitizePrintable(snippet[:n])`. Error message body snippet (256 bytes) is captured in logs. Privacy rule says no payload, but error messages may contain payload fragments. |
| 7 | "provider not registered" error returned when adapter missing (proxy/proxy.go:260-267) | The dispatcher checks `providerFound` (exists in config map) and `behavior` registered in Registry. But there is no validation at startup that all providers in config have registered adapters — the check is deferred to request time. | `proxy/proxy.go:326-343` — check happens per-request. A config with an unregistered behavior will pass `validate()` but fail at request time. |
| 8 | Mapping key regex is `(?i).*key.*` — substring match (config/config.go:205-239) | The README says "family prefix match (e.g. `claude-sonnet-4-...` → `claude-sonnet-4`)" but the code does substring containment, not prefix matching. `claude-sonnet-4-20250514` matches `sonnet` not `claude-sonnet-4` because regex is `.*sonnet.*`. | `config/config.go:222` — `regexp.MustCompile("(?i).*" + regexp.QuoteMeta(name) + ".*")`. Most-specific-first tiebreak helps but substring != prefix. |
| 9 | "Freedius auto-injects Claude Code environment variables" (prd.md:36, secondary criterion) | The `configure` command writes `~/.claude/settings.json`, overriding the entire file (with backup). It does not merge — it replaces. | `cmd/freedius/configure.go` — overwrites settings.json. README says "overwrites the file with freedius's env block only" (README.md:82-84). |
| 10 | Providers are "first-class entries: there is no alias rewriting at load time" (providers_gen.go:10-11) | The `providerDefaults` map injects providers missing from user config (if they have a `DefaultBaseURL`), effectively creating aliases/defaults that appear without user declaration. | `config/defaults.go:21-28` — `if defaults.DefaultBaseURL != "" { c.Providers[name] = defaults }`. Providers auto-inject. |
| 11 | "custom providers must present an Anthropic-compatible API. The gateway is a pass-through proxy, not a universal translator." (prd.md:83) | `custom` provider uses `behavior: mix` and goes through the MixAdapter, which auto-detects protocol and translates. So custom providers DO get translation, not just pass-through. | `providers.yaml:47-49` — custom is `behavior: mix`. MixAdapter translates. Contradicts PRD statement. |

---

## STEP 5 — Refactoring Ranking

Ranked by **value** (how core the invariant is to the product promise) × **risk** (how poorly it is currently enforced/expressed).

| # | Aggregate Candidate | Value (1-5) | Risk (1-5) | Composite | Notes |
|---|--------------------|-------------|------------|-----------|-------|
| 1 | **Privacy (no body logging)** | 5 | 5 | **25** | Core NFR (PRD:90). Currently enforced by file-level comments only — no static analysis, no test that fails if someone adds `slog` with body content. Easy to violate accidentally. High user-trust impact. |
| 2 | **Mapping resolution correctness** | 5 | 3 | **15** | Core to product. Current regex substring match can surprise users (discrepancy #8). Most-specific-first helps but the matching semantics don't match user mental model ("prefix match"). |
| 3 | **Config validation completeness** | 4 | 4 | **16** | Adapter-registration check is deferred to runtime. A typo in behavior field → 500 at request time instead of startup. Could be caught at load. |
| 4 | **Error classification fidelity** | 4 | 2 | **8** | Well-implemented switch. Low risk. Minor: error type strings are scattered across `classifyUpstreamError` and `writeAnthropicError` — no single source of truth. |
| 5 | **Fallback chain semantics** | 5 | 2 | **10** | Core feature. The `wroteHeader` early-exit (discrepancy #4) is a real edge case but rare in practice. |
| 6 | **Model field stability** | 5 | 1 | **5** | Well enforced via context propagation. Low risk. |
| 7 | **Concurrent session isolation** | 4 | 1 | **4** | Stateless design. Well handled. |

### #1 Recommendation: Privacy Invariant

**Why #1:** The privacy rule ("no request or response payload is logged to disk or transmitted beyond the target provider") is a non-negotiable NFR stated in the PRD (`prd.md:90`) and repeated in file-level comments. Yet it is enforced only by convention — a developer could add `slog.Info("body", "content", string(body))` in any file and no test or lint would catch it. The risk is high because:

1. The codebase has multiple translation/adapter files where body content is in scope (`anthropic_compat.go`, `openai_compat.go`, `translate/anthropic_openai.go`).
2. Error message snippets capture 256 bytes of upstream response body — a potential privacy leak path (discrepancy #6).
3. The `LogSink` and `EventBus` carry `ErrorMessage` fields that may propagate body fragments.

**Suggested enforcement direction:** Introduce a linter rule or a test that greps for body-logging patterns in proxy package files, OR architecturally separate body-content types from loggable metadata types so the compiler prevents accidental logging.

---

## Summary

This artifact contains: (1) a complete ubiquitous language table of 28 domain terms extracted from documents and code with source citations; (2) a subdomain classification mapping each concept to Core/Supporting/Generic with PRD-based justifications; (3) ten aggregate candidates with their invariants, source quotes, and enforcement status; (4) eleven model-vs-code discrepancies where documented intent diverges from implementation; and (5) a refactoring ranking by value×risk.

**The most important conclusion:** freedius's core domain — routing LLM requests through model mappings with fallback chains and protocol translation — is well-expressed in the code and largely matches the PRD. However, the single highest-risk gap is the privacy NFR: the "no body logging" rule that is central to the product's trust contract is enforced only by convention and file-level comments, with no static or test-based enforcement. A second critical finding is that the PRD itself is stale — it declares "no web UI in v1" and lists only 4 providers, while the codebase ships 19 providers and a full dashboard, suggesting the domain model has evolved past its original specification without a corresponding PRD update.

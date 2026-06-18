---
id: first-call-routed
title: First call routed — NIM adapter + custom passthrough
status: impl_reviewed
created: 2026-06-16
updated: 2026-06-17
roadmap_id: S-01
prd_refs:
  - US-01
  - FR-001
  - FR-002
  - FR-006
  - FR-009
  - NFR-Latency
  - NFR-Error-handling
  - NFR-Privacy
---

## Scope Addendum (post-S-01 review, 2026-06-17)

The implementation shipped by the S-01 branch exceeded the plan's
"Changes Required" scope in the following ways. This addendum records
the drift for future reviewers and agents.

### Files added beyond the plan

- `proxy/anthropic_compat.go` — Anthropic-compatible passthrough
  adapter (`x-api-key` + `anthropic-version: 2023-06-01`). Plan
  L62 said the custom adapter would use `Authorization: Bearer`;
  the implementation correctly used `x-api-key` because the Anthropic
  API does not accept Bearer.
- `proxy/openai_compat.go` — OpenAI-compatible SSE-translating
  adapter used by NIM, plus a `--stream-timeout` flag
  (default 5m). Plan L61 said "no http.Client.Timeout, no
  context.WithTimeout"; the implementation added a per-request
  `context.WithTimeout(r.Context(), streamTimeout)` as defensive
  hardening. The 5-minute default is well above any real LLM call.
- `proxy/mix.go` — adapter that routes to anthropic_compat or
  openai_compat based on the upstream path. Used by `provider: zen`
  and `provider: go` entries (post-rewrite by `applyDefaults`).
- `proxy/families.go` — `extractFamily(opus/sonnet/haiku/auto/
  default)` and a `Mappings[family]` lookup. Plan L127-128 described
  the dispatcher as consulting only `cfg.Models`; the implementation
  added a per-family fallback so a single mapping can cover all
  Claude models in a family.
- `config/defaults.go` — `knownProviderDefaults`, `applyDefaults`,
  `applyEntryDefaults` (rewrites `custom`→`anthropic` and
  `zen`/`go`→`mix`), `readConfigFile`, `yamlUnmarshalStrict`. Plan
  L66 explicitly excluded a top-level `providers:` block; this file
  introduces provider-level defaults via a different mechanism
  (post-rewrite, not a top-level block) with the same intent.

### Plan items superseded

- L62 "Anthropic-version header on custom adapters — only
  Authorization: Bearer" → superseded. Custom adapter uses
  `x-api-key` + `anthropic-version: 2023-06-01` because that is
  what the Anthropic API accepts. Recorded as a project lesson.
- L61 "Total upstream-call timeout — none" → superseded. The
  OpenAI-compatible adapter wraps the outbound request in
  `context.WithTimeout(r.Context(), streamTimeout)` with a
  `--stream-timeout` flag. Cancellation still propagates.
- L67-68 "Provider-specific adapter files for `zen` and `go` —
  they still return 501" → superseded. `zen`/`go` are routed
  through `mix.go` to the OpenAI-compatible adapter (post-rewrite).
- L66 "Top-level `providers:` block in YAML" → partly superseded.
  No top-level block was added, but `config/defaults.go` introduces
  the same idea as a post-rewrite defaults map.

### Registry entries that shipped

- `nim` (was in plan)
- `custom` (was in plan; delegates to `AnthropicCompatibleAdapter`)
- `anthropic` (added; same code path as `custom`)
- `openai` (added; used by `mix`)
- `mix` (added; routes between anthropic_compat and openai_compat)

### S-02 work pre-baked here

The S-02 work (zen/go adapters) was effectively done in this
branch. The S-02 change folder can either reuse these adapters
or be archived as "covered by S-01 scope expansion".

---

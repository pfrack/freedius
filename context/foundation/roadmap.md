---
project: freedius
version: 1
status: draft
created: 2026-06-16
updated: 2026-06-17
prd_version: 1
main_goal: speed
top_blocker: time
---

# Roadmap: freedius

> Derived from `context/foundation/prd.md` (v1) + auto-researched codebase baseline.
> Edit-in-place; archive when superseded.
> Slices below are listed in dependency order. The "At a glance" table is the index.

## Vision recap

A developer using Claude Code wants to route LLM calls to cheaper or free providers instead of paying Anthropic's rates. Existing solutions are production gateways (overkill for a solo dev's laptop) or apps with clunky UIs. Freedius replaces them with a single config file and a local process — a few lines of config, not a project.

## North star

**S-01: first-call-routed — NIM + custom routing** — the north star: the smallest end-to-end slice whose successful delivery would prove the core product hypothesis. The NIM adapter validates Anthropic→OpenAI translation and streaming; the custom passthrough validates that the proxy handles arbitrary Anthropic-compatible endpoints without modification. Together, they prove the entire concept — routing works, translation works, Claude Code cannot tell the difference.

## At a glance

| ID   | Change ID          | Outcome (user can …)                                           | Prerequisites | PRD refs                                        | Status   |
| ---- | ------------------ | -------------------------------------------------------------- | ------------- | ----------------------------------------------- | -------- |
| F-01 | proxy-skeleton     | (foundation) HTTP server listens, config loaded, dispatch stub | —             | FR-001, FR-003, FR-004, FR-005, NFR-Multi-agent, NFR-Resource-footprint | done     |
| S-01 | first-call-routed  | route a Claude Code call through freedius to NIM or a custom Anthropic-compatible provider — streaming and tool use work identically | F-01          | US-01, FR-001, FR-002, FR-006, FR-009, NFR-Latency, NFR-Error-handling, NFR-Privacy | done     |
| S-02 | provider-and-mapping | route by semantic family name (`opus`/`sonnet`/`haiku`/`auto`/`default`); use agnostic compat providers (`openai`/`anthropic`) for any compatible endpoint; known providers ship with in-binary defaults | S-01          | FR-001, FR-003, FR-004, FR-009, NFR-Error-handling | proposed |
| S-03 | zen-go-adapters    | route calls to Opencode Zen or Opencode Go via multi-format adapters (Anthropic-format and OpenAI-format per model) | S-01, S-02    | FR-007, FR-008, NFR-Error-handling               | proposed |
| S-04 | error-hardening    | get clear error messages on config mistakes and provider failures; freedius auto-injects Claude Code env vars; `freedius init` generates a starter config template | S-01          | FR-004, Success-Criteria-Secondary, NFR-Error-handling | proposed |
| S-05 | opencode-nim-fixes | route calls to OpenCode Go Anthropic-format models (MiniMax, Qwen on `/v1/messages`) without 401; NIM streaming delivers reasoning content instead of empty/malformed SSE responses | S-03          | FR-006, FR-007, FR-008, NFR-Error-handling       | proposed |
| S-06 | provider-codegen   | add a new provider by adding one entry to `providers.yaml` and running `go generate` — all boilerplate (adapters, config maps, registry, validation) is generated | S-05          | FR-003, FR-004                                   | proposed |

## Baseline

What's already in place in the codebase as of `2026-06-16` (auto-researched + user-confirmed).
Foundations below assume these are present and do NOT re-scaffold them.

- **Frontend:** N/A — CLI tool, no UI per PRD §Non-Goals ("no web UI in v1")
- **Backend / API:** Absent — go.mod exists (module github.com/user/freedius, Go 1.26.1), no source files. Tech-stack.md: Go stdlib net/http + httputil.ReverseProxy.
- **Data:** N/A — in-memory proxy, no persistence per PRD.
- **Auth:** Absent (by design) — single user, no sign-up per PRD §Access Control.
- **Deploy / infra:** Absent — no CI workflow, no Dockerfile. Tech-stack.md: GitHub Actions + self-host.
- **Observability:** Absent — no logging/metrics infrastructure.

## Foundations

### F-01: Proxy skeleton + config

- **Outcome:** (foundation) HTTP server listens on a local port, YAML config file with model→provider mappings is loaded at startup, incoming requests are dispatched to a provider dispatch stub (not yet performing translation) — the request loop is wired end-to-end.
- **Change ID:** proxy-skeleton
- **PRD refs:** FR-001, FR-003, FR-004, FR-005, NFR-Multi-agent, NFR-Resource-footprint
- **Unlocks:** S-01 (first-call-routed — connects the dispatch stub to real NIM and custom-provider adapters)
- **Prerequisites:** —
- **Parallel with:** —
- **Blockers:** —
- **Unknowns:** —
- **Risk:** Minimal — Go's stdlib net/http + httputil.ReverseProxy make this straightforward. The main risk is over-engineering the foundation: keep it to the smallest loop that can receive a request, parse a config, and dispatch to a no-op handler. Anything more belongs in S-01.
- **Status:** ready

## Slices

### S-01: First call routed — NIM + custom

- **Outcome:** user can configure a NIM model mapping and a custom Anthropic-compatible provider, run `claude-code`, and the request proxies through freedius to either provider — streaming, tool use, and multi-turn conversations all work as if talking directly to Anthropic.
- **Change ID:** first-call-routed
- **PRD refs:** US-01, FR-001, FR-002, FR-006, FR-009, NFR-Latency, NFR-Error-handling, NFR-Privacy
- **Prerequisites:** F-01
- **Parallel with:** —
- **Blockers:** —
- **Unknowns:**
  - NIM free-tier streaming format — does NIM's SSE streaming match the OpenAI format the adapter targets? Owner: user. Block: no (partial streaming support is acceptable per FR-002 Socrates resolution).
  - Claude Code API base override — does `CLAUDE_CODE_API_BASE` (or equivalent env var) accept a localhost proxy transparently? Owner: user. Block: no (trivially verifiable before implementation).
- **Risk:** The north star. The dual-provider scope increases the exposure — if NIM's API surprises us (rate limits, format quirks), the custom passthrough serves as a fallback validation path. The slice still ships if one provider path works.
- **Status:** proposed

### S-02: Provider-and-mapping — family mapping + compat providers + in-binary defaults

- **Outcome:** user can route by semantic model family (`opus`/`sonnet`/`haiku`/`auto`/`default`) instead of enumerating every Claude Code model name; user can point at any OpenAI- or Anthropic-compatible endpoint using agnostic `provider: openai` / `provider: anthropic` strings; known providers (`nim`/`zen`/`go`) ship with sensible default values in the binary so config stays short; `provider: custom` is an alias for `provider: anthropic`.
- **Change ID:** provider-and-mapping
- **PRD refs:** FR-001, FR-003, FR-004, FR-009, NFR-Error-handling
- **Prerequisites:** S-01
- **Parallel with:** S-03, S-04
- **Blockers:** —
- **Unknowns:** —
- **Risk:** This is an architectural refactor (schema + dispatch + config-load). Three independent concerns bundled into one slice because they all touch the same code surface (config + dispatch + adapters). The S-02 planner may further decompose into S-02a/b/c if implementation reveals they should ship separately. S-03 (zen-go-adapters) depends on S-02's compat adapter code as the underlying implementation.
- **Status:** proposed

### S-03: Zen + Go adapters

- **Outcome:** user can configure Opencode Zen and Opencode Go model mappings and route Claude Code calls to either provider through multi-format adapters (Anthropic-format and OpenAI-format endpoints are auto-detected from `base_url`).
- **Change ID:** zen-go-adapters
- **PRD refs:** FR-007, FR-008, NFR-Error-handling
- **Prerequisites:** S-01, S-02 (the compat adapter code from S-02 is the underlying implementation; S-03's `ZenAdapter` and `GoAdapter` are thin multi-format routers on top)
- **Parallel with:** S-04
- **Blockers:** —
- **Unknowns:**
  - Does Zen's `/v1/messages` endpoint require the `anthropic-version` header? (Verify with `curl`.) Block: no.
  - Does Zen's `/v1/chat/completions` honor `stream_options: {include_usage: true}`? Block: no (output_tokens falls back to 0 if not).
- **Risk:** The two providers are multi-format gateways (Zen: 4 wire formats; Go: 2 wire formats). S-03 only supports the two most common (Anthropic + OpenAI Chat Completions); OpenAI Responses (GPT) and Google (Gemini) are deferred.
- **Status:** proposed

### S-04: Error hardening + env injection + config template

- **Outcome:** user gets clear error messages on config mistakes (missing keys, invalid YAML) and provider failures — no silent crashes; freedius auto-injects Claude Code environment variables so manual env setup is unnecessary; `freedius init` generates a starter config template.
- **Change ID:** error-hardening
- **PRD refs:** FR-004, Success-Criteria-Secondary, NFR-Error-handling
- **Prerequisites:** S-01
- **Parallel with:** S-02, S-03
- **Blockers:** —
- **Unknowns:** —
- **Risk:** The auto-inject-env-vars feature (secondary Success Criterion) is low risk (write to a shell config or emit instructions). The config template is a simple file write. The real work is hardening — ensuring every failure path in the proxy produces a user-readable message rather than a crash or a silent timeout.
- **Status:** proposed

### S-05: OpenCode Go 401 + NIM SSE fixes

- **Outcome:** user can route Claude Code calls to OpenCode Go Anthropic-format models (MiniMax, Qwen on `/v1/go/messages`) without 401 errors; NIM streaming delivers reasoning content to Claude Code instead of empty/malformed SSE responses. The `AnthropicCompatibleAdapter` auth scheme is universally corrected from `Authorization: Bearer` to `x-api-key` + `anthropic-version` for all Anthropic-compatible endpoints.
- **Change ID:** opencode-nim-fixes
- **PRD refs:** FR-006, FR-007, FR-008, NFR-Error-handling
- **Prerequisites:** S-03 (builds on the `mix` adapter and `AnthropicCompatibleAdapter` from S-03; the auth fix affects all Anthropic-format providers universally)
- **Parallel with:** S-04
- **Blockers:** —
- **Unknowns:**
  - Does OpenCode Go return 200 with an SSE error chunk for unsupported request fields (e.g. `stream_options.include_usage`)? The "feature not supported" stream-translation log indicates upstream rejection of a request field. Verify with `curl` and adapt `TranslateRequest` to omit unsupported fields when targeting OpenCode endpoints.
- **Risk:** The auth change (`x-api-key` instead of `Authorization: Bearer`) is a universal shift for all Anthropic-compatible providers. Any custom provider configured with `provider: custom` or `provider: anthropic` that relied on the old `Authorization: Bearer` header will break — but those providers were already violating the Anthropic spec, so this is a correctness fix, not a regression. The S-03 research explicitly flagged the `anthropic-version` header as an unknown (see S-03 Unknowns item #1); this slice resolves it. NIM SSE fixes only affect NIM, which currently produces no useful output — this is purely additive.
- **Status:** proposed

### S-06: Provider codegen — `go:generate` from `providers.yaml`

- **Outcome:** adding a new provider requires only a one-entry addition to `providers.yaml` + `go generate ./...` — all boilerplate (thin adapter wrappers, `KnownProviders` map, `knownProviderDefaults`, rewrite rules, `base_url` validation list, registry construction) is generated at compile time. The three core adapters (`openai_compat.go`, `anthropic_compat.go`, `mix.go`) remain hand-written.
- **Change ID:** provider-codegen
- **PRD refs:** FR-003, FR-004
- **Prerequisites:** S-05 (needs auth scheme (`x-api-key` vs `Bearer`), NIM body sanitization, and SSE stream translation patterns stabilized before extracting them into a codegen template — without S-05, the generated code would embed the wrong auth header for Anthropic-format providers)
- **Parallel with:** S-04
- **Blockers:** —
- **Unknowns:** —
- **Risk:** Low — this is a pure internal refactor with no user-facing behavior change. The generated code is deterministic and reviewable. Risk is over-abstracting: the template must stay simple enough that reading a generated file is trivial.
- **Status:** proposed

## Backlog Handoff

| Roadmap ID | Change ID          | Suggested issue title                      | Ready for `/10x-plan` | Notes |
| ---------- | ------------------ | ------------------------------------------ | --------------------- | ----- |
| F-01       | proxy-skeleton     | Proxy skeleton — HTTP server + config loading + dispatch stub | yes                   | No prerequisites. Done. |
| S-01       | first-call-routed  | First call routed — NIM adapter + custom passthrough | no                    | Needs F-01. North star. Done. |
| S-02       | provider-and-mapping | Family-aware mapping + compat providers + in-binary defaults | no                    | Needs S-01. Architectural refactor; bundles three concerns. S-03 depends on S-02's compat adapter code. |
| S-03       | zen-go-adapters    | Opencode Zen + Go multi-format adapters   | no                    | Needs S-01 and S-02. Runs parallel with S-04. The `ZenAdapter`/`GoAdapter` are thin routers on S-02's compat adapters. |
| S-04       | error-hardening    | Error hardening + env auto-injection + config template | no                    | Needs S-01. Runs parallel with S-02 and S-03. |
| S-05       | opencode-nim-fixes | OpenCode Go Anthropic-format 401 fix + NIM SSE reasoning fixes | no                    | Needs S-03. Auth change (x-api-key) applies universally to all Anthropic-compat providers. |
| S-06       | provider-codegen   | Provider codegen — go:generate boilerplate from providers.yaml | no                    | Needs S-05. Auth scheme and SSE patterns must be stable before codegen extraction. |

## Open Roadmap Questions

1. **What is the expected peak queries-per-second (QPS)?** — Owner: user. Block: no (scale refines resource-footprint NFR; ballpark is implicit for single-user local tool).
2. **What is the expected request/response payload size range?** — Owner: user. Block: no (refines memory and latency NFRs).
3. **Additional user stories for FR-002 through FR-009?** — Owner: user. Block: no (FRs are fully specified; user stories add scenario texture for implementation planning, not roadmap-sequencing input).
4. **Additional non-goals beyond "no web UI in v1"?** — Owner: user. Block: no (monitoring dashboard, usage analytics, billing integration, Windows support — none gate the roadmap but may affect later slices).

## Parked

- **Web UI** — Why parked: v1 is config-file-only per PRD §Non-Goals. v2 concern.
- **Monitoring dashboard** — Why parked: not in v1 scope; listed as potential non-goal in Open Question #4.
- **Usage analytics / billing integration** — Why parked: not in v1 scope; single-user local tool.
- **Windows support** — Why parked: not in v1 scope; listed as potential non-goal in Open Question #4. Linux + macOS only for v1.

## Done

- F-01: proxy-skeleton — merged via PR #1
- S-01: first-call-routed — merged via PR #2

---
project: freedius
version: 1
status: draft
created: 2026-06-16
updated: 2026-07-06
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
| S-06 | custom-to-mix-protocol | use `provider: custom` with any endpoint — freedius auto-detects protocol from URL or user sets explicit `protocol: openai\|anthropic` field | S-03          | FR-003, FR-009                                   | proposed |
| S-07 | provider-codegen   | add a new provider by adding one entry to `providers.yaml` and running `go generate` — all boilerplate (adapters, config maps, registry, validation) is generated | S-05, S-06    | FR-003, FR-004                                   | proposed |
| S-08 | openai-count-tokens | `POST /v1/messages/count_tokens` returns a useful `input_tokens` estimate when routed to OpenAI-protocol upstreams (NIM, OpenCode Go, custom OpenAI-compat) — no more 501 for these providers | S-01, count-tokens-passthrough | (new capability — local token counting)        | proposed |
| V-01 | tui-dashboard       | (superseded — see V-02) | — | Replaced by V-02 on 2026-07-03. Original research at `context/changes/tui-dashboard/research.md`. | superseded |
| V-02 | web-ui              | monitor live request stream, provider health, and usage stats from a browser dashboard at :8083 — works in Docker / headless; replaces the TUI | v1 complete (S-01–S-08) | research + plan in `context/changes/web-ui/`. Drops TUI + Unix-socket IPC + charm.land deps. | done |
| V-02a | provider-model-discovery | fetch a provider's available models from its upstream /v1/models endpoint, pick from a clickable list when configuring a mapping | V-02 | `context/changes/provider-model-discovery/`. Mapping-modal-only "Fetch models" button with click-to-fill; no Providers-page UI. | done |
| V-02b | web-ui-redesign | modernized web UI with zinc dark palette, responsive sidebar, design system tokens | V-02 | Archived 2026-07-05. Visual refresh of layout, CSS variables, mobile hamburger nav. | done |
| V-02c | provider-fallback-routing | configure fallback provider/model chains — freedius tries alternatives when primary fails (transport error, 4xx/5xx) | V-02 | `context/changes/provider-fallback-routing/`. Config schema `fallback:` array on mappings + dispatcher retry logic. | done |
| V-02d | mapping-graph-visualization | visualize mapping routing as breadcrumb-chain cards instead of a flat table — each mapping shows its pipeline left-to-right | V-02c | `context/changes/mapping-graph-visualization/`. Pure CSS + template change, no new deps. | planned |

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

### S-06: Custom → mix + protocol field

- **Outcome:** user can use `provider: custom` with any endpoint (OpenAI-compatible or Anthropic-compatible) — freedius auto-detects the protocol from `base_url` path (existing `mix` behavior) or the user explicitly sets `protocol: openai` or `protocol: anthropic` to override when URLs are ambiguous. The `CustomAdapter` struct is removed; `custom` is rewritten to `mix` in `applyDefaults()` following the same pattern as `zen`/`go`.
- **Change ID:** custom-to-mix-protocol
- **PRD refs:** FR-003, FR-009
- **Prerequisites:** S-03 (depends on `MixAdapter` being the proven multi-format router)
- **Parallel with:** S-05
- **Blockers:** —
- **Unknowns:** —
- **Risk:** Low — follows the exact rewrite pattern already used for `zen`/`go` → `mix`. The `protocol` field is additive (optional, validated at config load). Existing `custom` configs with `/v1/messages` URLs keep working because `mix` already routes them to the Anthropic adapter. The only risk is users with ambiguous URLs who relied on the implicit "always Anthropic" behavior — they'd need to add `protocol: anthropic`.
- **Status:** proposed

### S-07: Provider codegen — `go:generate` from `providers.yaml`

- **Outcome:** adding a new provider requires only a one-entry addition to `providers.yaml` + `go generate ./...` — all boilerplate (thin adapter wrappers, `KnownProviders` map, `knownProviderDefaults`, rewrite rules, `base_url` validation list, registry construction) is generated at compile time. The three core adapters (`openai_compat.go`, `anthropic_compat.go`, `mix.go`) remain hand-written.
- **Change ID:** provider-codegen
- **PRD refs:** FR-003, FR-004
- **Prerequisites:** S-05, S-06 (needs auth scheme (`x-api-key` vs `Bearer`), NIM body sanitization, SSE stream translation patterns, and the `protocol` field all stabilized before extracting them into a codegen template)
- **Parallel with:** —
- **Blockers:** —
- **Unknowns:** —
- **Risk:** Low — this is a pure internal refactor with no user-facing behavior change. The generated code is deterministic and reviewable. Risk is over-abstracting: the template must stay simple enough that reading a generated file is trivial.
- **Status:** proposed

### S-08: Local token counting for OpenAI-protocol upstreams

- **Outcome:** Claude Code's `/v1/messages/count_tokens` probe returns a useful `input_tokens` estimate when the request is routed to OpenAI-protocol upstreams (NIM, OpenCode Go, custom OpenAI-compatible endpoints like DeepSeek via opencode-go). The user no longer sees 501 for these providers. Implementation: a local counter (character-based heuristic or `github.com/pkoukk/tiktoken-go` with `cl100k_base` encoding) runs in the dispatcher before the 501 rejection path; on success the response shape is identical to Anthropic's upstream `{"input_tokens": N, ...}`.
- **Change ID:** openai-count-tokens
- **PRD refs:** (new capability — local token counting extends FR-001 / FR-006 / FR-007 / FR-008 by making `count_tokens` usable across all provider protocols)
- **Prerequisites:** S-01 (first-call-routed — proves the routing/dispatch path is stable enough to add a non-trivial branch), count-tokens-passthrough (the routing logic that currently emits 501 for OpenAI-protocol upstreams; the new counter replaces the rejection with a useful response)
- **Parallel with:** —
- **Blockers:** —
- **Unknowns:**
  - **Counter approach: tiktoken-go vs character-based heuristic.** Owner: user. Block: no. tiktoken-go is more accurate (~within a few percent of upstream tokenizers) but adds a dependency; character heuristic has zero new deps but is coarser (~within 20% of upstream). Both satisfy the use case (pre-flight estimation, prompt trimming) per the reference pattern in `free-claude-code` (`core/anthropic/tokens.py`).
  - **Does the local counter need to match the upstream's tokenizer exactly?** Block: no. count_tokens is for estimation only; Anthropic's own docs describe it as a "best effort" estimate. As long as the count is in the right ballpark, Claude Code's pre-flight behavior is preserved.
  - **What encoding for tiktoken?** Likely `cl100k_base` (matches GPT-4 / most OpenAI-style tokenizers and the `free-claude-code` reference). Owner: user. Block: no.
- **Risk:** Medium — adds a new dependency (tiktoken-go) if we go that route; char-heuristic has zero new deps. The token count will not match the upstream's count exactly, but Claude Code uses it for pre-flight estimation only, so accuracy within ~10–20% is fine. The dispatcher-level integration follows the same pattern as the existing count_tokens 501 check (single capability branch in `proxy/capabilities.go`), keeping the change small.
- **Status:** proposed

### V-01: TUI dashboard — live monitoring terminal UI

- **Outcome:** user runs `freedius tui` and gets a live terminal dashboard showing: request stream (model, provider, latency, token count), provider health (up/down, avg response time, error rate), active config summary (model mappings, endpoints), usage stats (requests/min, total tokens), and recent error log. Zero context switch from the terminal.
- **Change ID:** tui-dashboard
- **PRD refs:** v2 scope (PRD §Non-Goals: "no web UI in v1")
- **Prerequisites:** v1 complete (S-01–S-08) — the proxy event flow, provider registry, and config model must be stable before adding a UI layer that observes them.
- **Parallel with:** —
- **Blockers:** —
- **Unknowns:**
  - **TUI scope for MVP:** Full dashboard or just a live log viewer? Owner: user. Block: no (either ships value; full dashboard is the recommended approach per research).
  - **Event bus persistence:** In-memory ring buffer or file-backed store for historical view? Owner: user. Block: no (ring buffer is simpler; file store enables historical charts).
  - **Subcommand vs flag:** `freedius tui` (separate command) vs `freedius --tui` (flag on main process)? Owner: user. Block: no (separate command is the Bubble Tea convention).
  - **Web UI timing:** Add web dashboard (htmx+templ) in the same change or defer to V-02? Owner: user. Block: no (research recommends hybrid: TUI first, web later).
- **Risk:** Low-medium. Bubble Tea is mature and well-proven in Go CLI tools (lazygit, k9s). The main risk is coupling the proxy core to a UI event bus — the design should use a decoupled subscriber pattern so the proxy functions identically with or without the TUI attached. Research at `context/changes/tui-dashboard/research.md` covers architecture patterns and dependency choices.
- **Status:** superseded by V-02 (2026-07-03) — TUI dropped in favor of embedded web UI; see V-02 for the replacement.

### V-02: Web UI dashboard — embedded browser UI replacing the TUI

- **Outcome:** user runs `freedius` (no flags) and gets: plain-text request + log lines streaming to stderr (visible via `docker logs` or in the terminal), the proxy listening on `:8082` for Claude Code traffic, and a browser dashboard at `http://localhost:8083/` showing live SSE event/log streams plus full provider/mapping CRUD via htmx forms. Runs cleanly in Docker / headless environments — no TTY required.
- **Change ID:** web-ui
- **PRD refs:** v2 scope (PRD §Non-Goals: "no web UI in v1" — promoted from parked list 2026-07-03)
- **Prerequisites:** v1 complete (S-01–S-08) — proxy event flow, provider registry, and config model must be stable before adding a UI layer that observes them.
- **Parallel with:** —
- **Blockers:** —
- **Unknowns:**
  - **Phase ordering:** Read-only first (Phase 2), writeback later (Phase 3). Owner: planner. Block: no (locked in plan).
  - **Auth scope:** Token gates all routes when set (logs may leak upstream API keys via error messages). Owner: planner. Block: no (locked in plan).
  - **Docker base image:** Distroless `static-debian12:nonroot`. Owner: planner. Block: no (locked in plan).
- **Risk:** Medium. Three integration points (log fan-out, event bus, config mutation) all need discipline; the rollback pattern from the TUI's `submitForm` MUST be preserved exactly. Docker base image choice affects reproducibility. The breaking change (TUI removal in Phase 4) affects anyone using `--tui`, `--fg`, `--daemon`, or `attach`.
- **Status:** done

## Backlog Handoff

| Roadmap ID | Change ID          | Suggested issue title                      | Ready for `/10x-plan` | Notes |
| ---------- | ------------------ | ------------------------------------------ | --------------------- | ----- |
| F-01       | proxy-skeleton     | Proxy skeleton — HTTP server + config loading + dispatch stub | yes                   | No prerequisites. Done. |
| S-01       | first-call-routed  | First call routed — NIM adapter + custom passthrough | no                    | Needs F-01. North star. Done. |
| S-02       | provider-and-mapping | Family-aware mapping + compat providers + in-binary defaults | no                    | Needs S-01. Architectural refactor; bundles three concerns. S-03 depends on S-02's compat adapter code. |
| S-03       | zen-go-adapters    | Opencode Zen + Go multi-format adapters   | no                    | Needs S-01 and S-02. Runs parallel with S-04. The `ZenAdapter`/`GoAdapter` are thin routers on S-02's compat adapters. |
| S-04       | error-hardening    | Error hardening + env auto-injection + config template | no                    | Needs S-01. Runs parallel with S-02 and S-03. |
| S-05       | opencode-nim-fixes | OpenCode Go Anthropic-format 401 fix + NIM SSE reasoning fixes | no                    | Needs S-03. Auth change (x-api-key) applies universally to all Anthropic-compat providers. |
| S-06       | custom-to-mix-protocol | Custom → mix + protocol field — remove CustomAdapter, add protocol config field | no                    | Needs S-03. Runs parallel with S-05. Small refactor following existing zen/go pattern. |
| S-07       | provider-codegen   | Provider codegen — go:generate boilerplate from providers.yaml | no                    | Needs S-05, S-06. Auth scheme, SSE patterns, and protocol field must be stable before codegen extraction. |
| S-08       | openai-count-tokens | Local token counting for OpenAI-protocol upstreams (tiktoken-go or char-heuristic) | no                    | Needs S-01 + count-tokens-passthrough. Replaces the 501 path with a useful `input_tokens` estimate. Counter approach (tiktoken vs char-heuristic) is an open question for the planner. |
| V-01       | tui-dashboard       | TUI dashboard — live terminal monitoring UI (Bubble Tea) | —                     | Superseded by V-02 on 2026-07-03. Research preserved at `context/changes/tui-dashboard/research.md`. |
| V-02       | web-ui              | Web UI dashboard — embedded browser UI replacing the TUI | yes                   | Research at `context/changes/web-ui/research.md`; plan at `context/changes/web-ui/plan.md`. Phases 1–4 shippable independently. |

## Open Roadmap Questions

1. **What is the expected peak queries-per-second (QPS)?** — Owner: user. Block: no (scale refines resource-footprint NFR; ballpark is implicit for single-user local tool).
2. **What is the expected request/response payload size range?** — Owner: user. Block: no (refines memory and latency NFRs).
3. **Additional user stories for FR-002 through FR-009?** — Owner: user. Block: no (FRs are fully specified; user stories add scenario texture for implementation planning, not roadmap-sequencing input).
4. **Additional non-goals beyond "no web UI in v1"?** — Owner: user. Block: no (monitoring dashboard, usage analytics, billing integration, Windows support — none gate the roadmap but may affect later slices).

## Parked

- **Web UI** — *promoted to V-02 on 2026-07-03.* Removed from parked list (now tracked in At a glance).
- **Monitoring dashboard** — Why parked: not in v1 scope; listed as potential non-goal in Open Question #4.
- **Usage analytics / billing integration** — Why parked: not in v1 scope; single-user local tool.
- **Windows support** — Why parked: not in v1 scope; listed as potential non-goal in Open Question #4. Linux + macOS only for v1.

## Done

- F-01: proxy-skeleton — merged via PR #1
- S-01: first-call-routed — merged via PR #2
- **V-02: monitor live request stream, provider health, and usage stats from a browser dashboard at :8083 — works in Docker / headless; replaces the TUI** — Archived 2026-07-05 → `context/archive/2026-07-02-web-ui/`. Lesson: —.
- **V-02a: provider-model-discovery** — Mapping-modal-only "Fetch models" button with click-to-fill list. Backend: removed cache-only GET route, simplified refresh handler. UI: stripped Providers page to 7 columns, added explicit fetch button + clickable model list in mapping modal. Reviewed 2026-07-05 → `context/changes/provider-model-discovery/`.
- **V-02b: web-ui-redesign** — Modernized web UI with zinc dark palette, responsive sidebar, design system tokens, mobile hamburger nav. Archived 2026-07-05 → `context/archive/2026-07-05-web-ui-redesign/`.
- **V-02c: provider-fallback-routing** — Config schema `fallback:` array on mappings + dispatcher retry logic. When primary provider fails (transport error, 4xx/5xx before any response bytes), freedius tries configured fallback providers in order. Impl reviewed 2026-07-06 → `context/changes/provider-fallback-routing/`.

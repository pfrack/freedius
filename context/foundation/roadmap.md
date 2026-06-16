---
project: freedius
version: 1
status: draft
created: 2026-06-16
updated: 2026-06-16
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
| F-01 | proxy-skeleton     | (foundation) HTTP server listens, config loaded, dispatch stub | —             | FR-001, FR-003, FR-004, FR-005, NFR-Multi-agent, NFR-Resource-footprint | ready    |
| S-01 | first-call-routed  | route a Claude Code call through freedius to NIM or a custom Anthropic-compatible provider — streaming and tool use work identically | F-01          | US-01, FR-001, FR-002, FR-006, FR-009, NFR-Latency, NFR-Error-handling, NFR-Privacy | proposed |
| S-02 | zen-go-adapters    | configure Opencode Zen and Opencode Go model mappings and route calls to either provider | S-01          | FR-007, FR-008, NFR-Error-handling               | proposed |
| S-03 | error-hardening    | get clear error messages on config mistakes and provider failures; freedius auto-injects Claude Code env vars; `freedius init` generates a starter config template | S-01          | FR-004, Success-Criteria-Secondary, NFR-Error-handling | proposed |

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

### S-02: Zen + Go adapters

- **Outcome:** user can configure Opencode Zen and Opencode Go model mappings and route Claude Code calls to either provider.
- **Change ID:** zen-go-adapters
- **PRD refs:** FR-007, FR-008, NFR-Error-handling
- **Prerequisites:** S-01
- **Parallel with:** S-03
- **Blockers:** —
- **Unknowns:**
  - Zen API format — Anthropic-compatible, OpenAI-compatible, or custom? Owner: user. Block: no (PRD says translation layer handles per-provider format).
  - Go API format — same question. Owner: user. Block: no.
- **Risk:** Both providers follow the adapter pattern established in S-01, so this is incremental — one new adapter per provider. The unknown API formats mean the first 30 minutes of implementation are discovery, not coding.
- **Status:** proposed

### S-03: Error hardening + env injection + config template

- **Outcome:** user gets clear error messages on config mistakes (missing keys, invalid YAML) and provider failures — no silent crashes; freedius auto-injects Claude Code environment variables so manual env setup is unnecessary; `freedius init` generates a starter config template.
- **Change ID:** error-hardening
- **PRD refs:** FR-004, Success-Criteria-Secondary, NFR-Error-handling
- **Prerequisites:** S-01
- **Parallel with:** S-02
- **Blockers:** —
- **Unknowns:** —
- **Risk:** The auto-inject-env-vars feature (secondary Success Criterion) is low risk (write to a shell config or emit instructions). The config template is a simple file write. The real work is hardening — ensuring every failure path in the proxy produces a user-readable message rather than a crash or a silent timeout.
- **Status:** proposed

## Backlog Handoff

| Roadmap ID | Change ID          | Suggested issue title                      | Ready for `/10x-plan` | Notes |
| ---------- | ------------------ | ------------------------------------------ | --------------------- | ----- |
| F-01       | proxy-skeleton     | Proxy skeleton — HTTP server + config loading + dispatch stub | yes                   | No prerequisites. Start here. |
| S-01       | first-call-routed  | First call routed — NIM adapter + custom passthrough | no                    | Needs F-01. North star. |
| S-02       | zen-go-adapters    | Opencode Zen + Go adapters                | no                    | Needs S-01. Runs parallel with S-03. |
| S-03       | error-hardening    | Error hardening + env auto-injection + config template | no                    | Needs S-01. Runs parallel with S-02. |

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

(Empty on first generation. `/10x-archive` appends an entry here — and flips that item's `Status` to `done` — when a change whose `Change ID` matches the item is archived. Do NOT pre-populate.)

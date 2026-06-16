---
project: "freedius"
context_type: greenfield
created: 2026-06-16
updated: 2026-06-16
product_type: cli
target_scale:
  users: small
timeline_budget:
  mvp_weeks: 2
  hard_deadline: null
  after_hours_only: true
checkpoint:
  current_phase: 8
  phases_completed: [1, 2, 3, 4, 5, 6, 7]
  gray_areas_resolved:
    - topic: "pain category"
      decision: "Workflow friction — switching providers should be a config, not a project."
    - topic: "insight"
      decision: "Existing gateway tools target production infra, not solo-dev tooling. Nobody's built the dead-simple version."
    - topic: "persona scope"
      decision: "Individual developers — solo devs, one person, one machine."
    - topic: "access model"
      decision: "Flat — one user, one local config file. No roles, no multi-user."
    - topic: "v1 scope"
      decision: "Config file only, no web UI. 4 providers: Nvidia NIM, Opencode Zen, Opencode Go, custom. Web UI in v2."
    - topic: "mvp timeline"
      decision: "2 weeks."
  frs_drafted: 9
  quality_check_status: accepted
---

# Shape Notes

Seed: "I want to write gateway that routes llm calls from claude-code to other providers and is easy as fuck to configure. At the begining I want free nvidia nim, openrouter, opencode zen, opencode go etc"

## Vision & Problem Statement

A developer using Claude Code wants to route LLM calls to cheaper or free providers (Nvidia NIM, OpenRouter, Opencode Zen/Go) instead of paying Anthropic's rates. Existing solutions are either full-blown production gateways (overkill for a solo dev's laptop) or apps with clunky UIs that break the terminal-native flow. Switching providers should be dead-simple — a few lines of config, not a project.

The insight: existing gateway tools (LiteLLM, Langfuse, etc.) target production infrastructure — multi-user, multi-model, observability-heavy. Nobody has built the single-config-file, single-user, "just route my Claude Code calls to OpenRouter" tool.

## User & Persona

**Primary persona**: Solo developer using Claude Code for its harness/tools (agent loop, file editing, bash execution). They want cheaper inference than Anthropic's direct pricing. One person, one machine. Terminal-native — they live in the CLI and want configuration to live there too, not in a web UI.

## Success Criteria

### Primary
- Dev installs freedius, writes a config file (or freedius generates a template), points Claude Code at freedius, and makes a Claude Code request. The call routes through freedius to the configured provider (Nvidia NIM, Opencode Zen, Opencode Go, or custom). Claude Code functions normally.

### Secondary
- Freedius auto-injects Claude Code environment variables so the dev doesn't have to manually set `CLAUDE_CODE_API_BASE` or equivalent.

### Guardrails
- Claude Code cannot tell the difference — tool use, streaming, file editing, bash execution all work identically.
- Config errors (missing keys, invalid YAML) produce a clear error message but do not crash the gateway.

## Access Control

Single user; no auth. The gateway accepts any API key from Claude Code (dummy key). Real provider API keys live in a local config file on the developer's machine. One flat config — no roles, no multi-user, no profiles. The gateway is a local process; no sign-up, no sign-in.

## Functional Requirements

### Gateway core
- FR-001: Dev can use Claude Code without interruptions — all requests proxy transparently through freedius. Priority: must-have
  > Socrates: Counter-argument considered: "'No interruptions' hides failures — the dev should see provider errors, not get silent timeouts." Resolution: kept; gateway must forward provider errors visibly rather than swallowing them.
- FR-002: Dev can use Claude Code in auto/agent mode — tool calls, streaming, multi-turn conversations all work. Priority: must-have
  > Socrates: Counter-argument considered: "Some free providers don't support Anthropic-format tool-use streaming." Resolution: kept; partial streaming support is acceptable where the provider doesn't support it natively.
- FR-003: Dev can map any Claude Code model name to any provider model — the mapping is transparent to Claude Code. Priority: must-have
  > Socrates: Counter-argument considered: "Mapping Claude Opus to a free 8B model will silently produce garbage — dev needs feedback about capability gaps." Resolution: kept; acknowledged risk. Dev is responsible for sensible mappings.
- FR-004: Dev configures provider credentials via environment variables and model mappings in a config file. Priority: must-have
  > Socrates: Counter-argument considered: "Plaintext API keys in a config file is a security risk." Resolution: revised — credentials sourced from env vars, not config file. Model mappings stay in config file.
- FR-005: Freedius starts as a local process and listens for Claude Code HTTP requests. Priority: must-have
  > Socrates: Counter-argument considered: "Port conflicts — if the default port is taken, the dev has to debug why Claude Code can't connect." Resolution: kept; gateway must produce a clear error on port conflict.

### Providers
- FR-006: Dev can route requests to Nvidia NIM. Priority: must-have
  > Socrates: Counter-argument considered: "NIM free tier may change or disappear." Resolution: kept; acknowledged risk. If NIM vanishes, the adapter is dropped.
- FR-007: Dev can route requests to Opencode Zen. Priority: must-have
  > Socrates: Counter-argument considered: "Zen's API may not be Anthropic-compatible — every non-Anthropic provider needs a translation layer." Resolution: kept; the gateway handles per-provider translation where needed.
- FR-008: Dev can route requests to Opencode Go. Priority: must-have
  > Socrates: Counter-argument considered: "Same as Zen — API incompatibility means every provider is a custom adapter." Resolution: kept; same translation-layer approach.
- FR-009: Dev can route requests to a user-defined custom provider (endpoint + key). Priority: must-have
  > Socrates: Counter-argument considered: "Custom providers can have any API format — the gateway can't translate arbitrary APIs." Resolution: kept; custom providers must present an Anthropic-compatible API. The gateway is a pass-through proxy, not a universal translator.

## User Stories

### US-01: Dev routes first Claude Code call through freedius

- **Given** freedius is installed and a config file maps a model to a provider (e.g., Nvidia NIM)
- **When** the dev runs any Claude Code command
- **Then** the request passes through freedius, routes to the configured provider, and Claude Code responds with normal tool-use behavior

#### Acceptance Criteria
- Claude Code session starts and completes without error
- Tool calls (file read/write, bash, etc.) work identically to direct Anthropic API
- Streaming responses work

## Business Logic

Freedius translates Anthropic-format requests to OpenAI-compatible format and routes them to the configured provider based on model name mappings from the config file.

Inputs: the model name in the Claude Code request (Anthropic model ID), the config file's model→provider mapping, and the provider-specific endpoint + credentials from environment variables.
Output: a translated request sent to the matched provider, and the provider's response translated back to Anthropic format and returned to Claude Code.
The dev encounters this rule transparently — they send a Claude Code request and get a response. The translation and routing happen without the dev's awareness.

## Non-Functional Requirements

- Latency: freedius adds imperceptible overhead — the dev cannot feel the proxy between Claude Code and the provider.
- Error handling: provider errors are forwarded to Claude Code as descriptive messages; freedius itself does not crash or drop requests on config or provider errors.
- Multi-agent: freedius handles concurrent Claude Code sessions (multiple agents running in parallel) without interference, state leak, or request mixing.
- Privacy: no request or response payload is logged to disk or transmitted beyond the target provider. Data lives in-flight only.
- Resource footprint: freedius runs as a lightweight local process with sub-50MB idle memory and negligible CPU.

## Non-Goals

- No web UI in v1. Config file only. Web UI is a v2 concern.

## Forward: tech-stack

- User preference for Rust as the implementation language. To be evaluated during stack selection.

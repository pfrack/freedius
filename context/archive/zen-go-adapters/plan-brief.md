# Zen + Go Adapters — Plan Brief

> Full plan: `context/changes/zen-go-adapters/plan.md`
> Research: `context/changes/zen-go-adapters/research.md`

## What & Why

Add support for Opencode Zen and Opencode Go as providers. Both are multi-format model gateways exposing Anthropic-format and OpenAI-format endpoints behind one API key. Users need to route Claude Code calls through these gateways to access cheaper models (DeepSeek, MiniMax, GLM, Kimi, etc.).

## Starting Point

The codebase has `OpenAICompatibleAdapter` and `AnthropicCompatibleAdapter` as generic compat adapters. `NIMAdapter` wraps the OpenAI one; `CustomAdapter` wraps the Anthropic one. `zen` and `go` are in `KnownProviders` with defaulted `APIKeyEnv` but no adapter is registered for them — requests to zen/go models hit "provider not registered".

## Desired End State

A user configures `provider: zen` or `provider: go` with a `base_url` pointing at either `/v1/messages` (Anthropic) or `/v1/chat/completions` (OpenAI). The adapter auto-detects the format from the URL and delegates to the appropriate compat adapter. A new `provider: mix` is available for any third-party multi-format gateway.

## Key Decisions Made

| Decision | Choice | Why (1 sentence) | Source |
| --- | --- | --- | --- |
| Adapter architecture | Single `MixAdapter` type, not per-provider types | Zen and Go are identical in behavior; avoids code duplication | Plan |
| Provider naming | `mix` as user-facing compat provider | Users with non-Zen/Go multi-format gateways can use it directly | Plan |
| zen/go handling | Rewrite to `mix` in `applyDefaults` | Mirrors established `custom` → `anthropic` pattern; simpler than separate registry entries | Plan |
| Format detection | URL path suffix (`/v1/messages` → Anthropic, else → OpenAI) | No hardcoded model list; evolvable when Opencode adds models | Research |
| `base_url` requirement | Required for zen/go/mix at config load time | Multi-format gateway has no single sensible default | Research |
| `anthropic-version` header | Don't inject; pass through from Claude Code | Claude Code already sends it; matches existing adapter behavior | Plan |
| Out-of-scope formats | OpenAI Responses + Google excluded | Scope discipline; two formats cover 90%+ of Zen/Go models | Research |

## Scope

**In scope:**
- `MixAdapter` with URL-based format routing
- `mix` added to `KnownProviders`
- `zen`/`go` → `mix` rewrite in `applyDefaults`
- `base_url` validation for zen/go/mix
- Registry wiring in `main.go`
- Tests and example config

**Out of scope:**
- OpenAI Responses format (`/v1/responses`)
- Google Generative AI format
- Auto-fetching model lists
- Per-provider error messages (errors say "mix adapter")

## Architecture / Approach

```
config.yaml → applyDefaults(zen→mix, go→mix) → validate(mix needs base_url)
                                                          ↓
request → dispatcher → registry.Lookup("mix") → MixAdapter.Handle
                                                          ↓
                                          URL suffix check on m.BaseURL
                                         /                          \
                              /v1/messages                      otherwise
                                   ↓                               ↓
                    AnthropicCompatibleAdapter        OpenAICompatibleAdapter
                        (passthrough)                    (translate)
```

## Phases at a Glance

| Phase | What it delivers | Key risk |
| --- | --- | --- |
| 1. Config | `mix` in KnownProviders, zen/go→mix rewrite, base_url validation + tests | Rewrite changes error messages (says "mix" not "zen") — acceptable per decision |
| 2. MixAdapter + wiring | Working adapter, registry entry, tests, example config | URL suffix check is the load-bearing logic — a bug here routes to wrong format |

**Prerequisites:** S-02 (provider-and-mapping) complete — ✓ already archived
**Estimated effort:** ~1 session, 2 phases

## Open Risks & Assumptions

- Zen's `/v1/messages` endpoint assumed to be standard Anthropic wire format (verify with real API during manual testing)
- `stream_options: {include_usage: true}` assumed to work on Zen/Go OpenAI endpoint (graceful degradation if not — usage shows 0)
- Error messages reference `mix` instead of original provider name (`zen`/`go`) — accepted tradeoff

## Success Criteria (Summary)

- `make ci` passes with all new tests
- A zen Anthropic-format model routes through passthrough correctly
- A zen OpenAI-format model routes through translation correctly
- Misconfigured `base_url` (missing) fails at startup with clear error

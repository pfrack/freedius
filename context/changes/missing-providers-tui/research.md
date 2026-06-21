---
date: 2026-06-21T12:00:00+02:00
researcher: opencode
git_commit: 51878d7f50b9366bb1b99a7863d4eeb153a204e2
branch: tui-themes
repository: freedius
topic: "Why are popular providers missing from the TUI?"
tags: [research, codebase, providers, tui, codegen]
status: complete
last_updated: 2026-06-21
last_updated_by: opencode
---

# Research: Missing Popular Providers in TUI

**Date**: 2026-06-21T12:00:00+02:00  
**Researcher**: opencode  
**Git Commit**: 51878d7f50b9366bb1b99a7863d4eeb153a204e2  
**Branch**: tui-themes  
**Repository**: freedius

## Research Question

Why don't I see all providers in the TUI? I lost popular providers somewhere in a commit.

## Summary

The `anthropic` provider was removed from `providers.yaml` in commit `1a1bf3a` ("Tui dashboard (#13)"). However, the **generated Go files are stale** — `config/providers_gen.go` and `proxy/adapters_gen.go` still contain the `anthropic` entry. Running `go generate ./...` would synchronize the generated files with `providers.yaml`, removing `anthropic` from the runtime.

Other popular providers (Google, Mistral, Cohere, DeepSeek, Groq, Together, Fireworks) were **never present** in the repository.

## Detailed Findings

### 1. Current State of `providers.yaml`

**File**: [`providers.yaml`](providers.yaml) (lines 23-53)

The source of truth contains **6 providers**:

| Provider | Behavior | Default Base URL | API Key Env |
|----------|----------|------------------|-------------|
| `nim` | openai | `https://integrate.api.nvidia.com/v1/chat/completions` | `NVIDIA_NIM_API_KEY` |
| `zen` | mix | _(requires base_url)_ | `OPENCODE_API_KEY` |
| `go` | mix | _(requires base_url)_ | `OPENCODE_API_KEY` |
| `custom` | mix | _(requires base_url)_ | _(none)_ |
| `openai` | openai | _(requires base_url)_ | _(none)_ |
| `mix` | mix | _(requires base_url)_ | _(none)_ |

**Missing**: `anthropic` was removed in commit `1a1bf3a`.

### 2. Stale Generated Files

The generated files are **out of sync** with `providers.yaml`:

**`config/providers_gen.go`** (lines 12-53): Still contains `anthropic` entry:
```go
"anthropic": {
    Behavior:            "anthropic",
    DefaultAPIKeyEnv:    "ANTHROPIC_API_KEY",
    RequireBaseURL:      true,
    SupportsCountTokens: true,
},
```

**`proxy/adapters_gen.go`** (line 53): Still wires anthropic adapter:
```go
"anthropic": NewAnthropicCompatibleAdapter(logger, verboseErrors),
```

### 3. Git History

| Commit | Message | Change |
|--------|---------|--------|
| `b75db78` | Provider codegen (#9) | Created `providers.yaml` with 7 providers including `anthropic` |
| `1a1bf3a` | Tui dashboard (#13) | **Removed `anthropic`** from `providers.yaml` |

### 4. How Providers Appear in TUI

The TUI provider list comes from two sources:

1. **Providers Tab** ([`proxy/tui/views.go:236-259`](proxy/tui/views.go#L236-L259)): Reads from `cfg.ProvidersSnapshot()` which returns the runtime config's `Providers` map.

2. **Provider Picker** ([`proxy/tui/picker.go:109-116`](proxy/tui/picker.go#L109-L116)): Also reads from `cfg.ProvidersSnapshot()`.

The runtime config is populated by merging:
- User's `freedius.yaml` config file
- `providerDefaults` from `config/providers_gen.go` (via `applyDefaults()` at [`config/defaults.go:14-37`](config/defaults.go#L14-L37))

### 5. The Mismatch

Because the generated files haven't been regenerated:
- `anthropic` still appears in `providerDefaults` (from `providers_gen.go`)
- If user has `anthropic` in their `freedius.yaml`, it will show in TUI
- If user runs `go generate ./...`, `anthropic` will be removed from generated files

### 6. Other Popular Providers

No commits found adding or removing providers like:
- Google/Gemini
- Mistral
- Cohere
- DeepSeek
- Groq
- Together
- Fireworks
- Llama

These providers were **never in the repository**.

## Code References

- [`providers.yaml:23-53`](providers.yaml#L23-L53) — Current provider definitions (6 providers)
- [`config/providers_gen.go:12-53`](config/providers_gen.go#L12-L53) — Generated provider defaults (stale, still has anthropic)
- [`proxy/adapters_gen.go:44-60`](proxy/adapters_gen.go#L44-L60) — Generated registry wiring (stale, still has anthropic)
- [`proxy/tui/views.go:236-259`](proxy/tui/views.go#L236-L259) — TUI provider collection from config
- [`proxy/tui/picker.go:109-116`](proxy/tui/picker.go#L109-L116) — Provider picker population
- [`config/defaults.go:14-37`](config/defaults.go#L14-L37) — Merging providerDefaults into runtime config
- [`internal/genproviders/main.go:289-300`](internal/genproviders/main.go#L289-L300) — YAML loading logic

## Architecture Insights

1. **Build-time code generation**: Providers are defined in `providers.yaml` and code-generated into Go files via `go generate ./...`. This is a build-time pipeline, not runtime loading.

2. **Behavior-based adapter wiring**: The registry wires adapters by behavior class (`openai`, `anthropic`, `mix`), not by individual provider name. All providers sharing a behavior class use the same adapter constructor.

3. **Stale generated files risk**: If `providers.yaml` is modified without running `go generate ./...`, the generated files become stale and the runtime behavior doesn't match the source of truth.

## Historical Context

- `context/changes/` and `context/archive/` — No prior changes found related to provider management or TUI provider display.

## Related Research

- No other research artifacts found under `context/changes/**/research.md` or `context/archive/**/research.md`.

## Open Questions

1. **Should `anthropic` be restored to `providers.yaml`?** The generated files still reference it, suggesting it may have been removed accidentally.

2. **Should popular providers be added?** If the user expects Google, Mistral, etc., these would need to be added to `providers.yaml` with appropriate behavior class and defaults.

3. **Should `go generate ./...` be run?** This would synchronize the generated files with `providers.yaml`, but would remove `anthropic` from runtime if it's not restored to the YAML first.

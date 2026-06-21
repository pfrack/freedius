---
date: 2026-06-21T15:30:00+02:00
researcher: opencode
git_commit: b1946c1ed24e1d74d9e9ad099b621366e2ae5ec1
branch: providers
repository: freedius
topic: "How to improve config for mixed providers"
tags: [research, config, mix, providers, protocol, codegen]
status: complete
last_updated: 2026-06-21
last_updated_by: opencode
---

# Research: Improving Config for Mixed Providers

**Date**: 2026-06-21T15:30:00+02:00
**Researcher**: opencode
**Git Commit**: b1946c1ed24e1d74d9e9ad099b621366e2ae5ec1
**Branch**: providers
**Repository**: freedius

## Research Question

How to improve config for mixed providers — the `mix` behavior class that routes to either OpenAI or Anthropic sub-adapters based on URL path sniffing.

## Summary

The `mix` adapter is freedius's most flexible behavior class, composing both `OpenAICompatibleAdapter` and `AnthropicCompatibleAdapter` internally. However, its config surface has five concrete gaps: (1) a planned `protocol` field was approved but never merged, (2) URL-path sniffing is the only routing mechanism and is fragile, (3) `SupportsCountTokens` is a static codegen-time flag that ignores user overrides, (4) the TUI has no concept of mix-specific UX, and (5) missing edge-case test coverage. The S-06 plan (`context/archive/custom-to-mix-protocol/`) addresses gap #1 and was fully implemented and approved but the code was never merged to main.

## Detailed Findings

### 1. The `protocol` Field Gap (HIGH priority)

The S-06 plan (`context/archive/custom-to-mix-protocol/plan.md`) added an optional `protocol: openai|anthropic` field to the `Provider` struct. The implementation was reviewed and approved (`context/archive/custom-to-mix-protocol/reviews/impl-review.md:7` — "APPROVED", 0 critical findings). All automated tests passed. **But the code was never merged to main.**

Current state on `providers` branch:
- `Provider` struct (`config/config.go:37-47`) has no `Protocol` field
- `MixAdapter` (`proxy/mix.go:58`) routes purely by `strings.HasSuffix(parsedURL.Path, "/v1/messages")`
- Users with ambiguous URLs (e.g., a proxy at `https://my-gateway.example.com/api`) cannot force a specific protocol

The S-06 implementation added:
- `Protocol string` YAML field on `Provider` with validation (`""`, `"openai"`, `"anthropic"`)
- Protocol-first routing in `MixAdapter.Handle` before falling back to URL sniffing
- `custom` provider rewrite from `→ anthropic` to `→ mix`
- Full test coverage for protocol routing

**Source**: `config/config.go:37-47`, `proxy/mix.go:44-63`, `context/archive/custom-to-mix-protocol/plan.md`

### 2. URL-Path Sniffing Fragility (MEDIUM priority)

`MixAdapter.Handle` (`proxy/mix.go:58`) uses a single check:
```go
if strings.HasSuffix(parsedURL.Path, "/v1/messages")
```

This means:
- `/v1/messages` → Anthropic sub-adapter (no translation, `x-api-key` auth)
- Everything else → OpenAI sub-adapter (full request/response translation, `Bearer` auth)

Failure modes:
- A provider at `/v1/complete` (non-standard) silently goes through OpenAI adapter
- A provider at `/v1/messages/v2` (versioned) incorrectly matches Anthropic
- No way to override without the `protocol` field

**Source**: `proxy/mix.go:51-63`

### 3. `SupportsCountTokens` Static Flag (MEDIUM priority)

The `SupportsCountTokens` flag is computed at codegen time (`internal/genproviders/main.go:83-98`) based on whether the provider's default URL ends in `/v1/messages`. It's a static boolean in `providers_gen.go`.

The problem (`config/defaults.go:37-41` — comment explicitly acknowledges this):
> If a user overrides DefaultBaseURL to a /v1/messages-suffixed URL on a mix provider whose generated SupportsCountTokens is false, the dispatcher will use local counting even though MixAdapter would route /v1/messages/count_tokens upstream.

This means a user who configures `zen` with a `/v1/messages` URL gets local token counting instead of upstream passthrough — a silent behavioral downgrade.

**Source**: `config/defaults.go:37-41`, `internal/genproviders/main.go:83-98`

### 4. TUI Has No Mix-Specific UX (LOW priority)

The TUI presents identical forms for all provider types:
- Behavior picker (`proxy/tui/picker.go:60-79`): 3 choices — `openai`, `anthropic`, `mix`
- Provider form (`proxy/tui/model.go:663-679`): same 5 fields for all
- Validation (`proxy/tui/model.go:724-729`): same checks for all

There is no guidance that `mix` providers need a `base_url`, no protocol picker, and no hint about URL-path-based routing. A user creating a `mix` provider in the TUI has no way to set `protocol` (since the field doesn't exist yet).

**Source**: `proxy/tui/picker.go:60-79`, `proxy/tui/model.go:663-679`

### 5. Missing Edge-Case Tests (LOW priority)

`config/config_test.go` (817 lines, 22 subtests) has good coverage but gaps:
- No test for mix providers with ambiguous URLs (non-`/v1/messages` Anthropic endpoints)
- No test for concurrent config mutation (RWMutex exercised only in production)
- No test for auto-injected providers referenced by mappings (only tested indirectly)
- No test for duplicate mapping names (last-wins semantics)
- No negative test for `anthropic_version` field

**Source**: `config/config_test.go`

### 6. No Config Hot-Reload (LOW priority)

Config is loaded once at startup (`cmd/freedius/main.go:165-168`). The TUI can mutate in-memory config and save to disk via `config.Save()`, but the file is never re-read. External edits require restart.

**Source**: `cmd/freedius/main.go:165-168`

## Code References

- `config/config.go:37-47` — `Provider` struct definition (missing `Protocol` field)
- `config/config.go:189-236` — `validateProvider` function
- `config/defaults.go:16-43` — `applyDefaults` function
- `config/defaults.go:37-41` — comment about `SupportsCountTokens` static flag issue
- `proxy/mix.go:19-63` — `MixAdapter` struct and `Handle` method (URL sniffing)
- `proxy/mix.go:28-40` — `NewMixAdapter` (hardcoded `NoStreamUsage: true`)
- `proxy/openai_compat.go:59-163` — OpenAI adapter (translation layer)
- `proxy/anthropic_compat.go:39-98` — Anthropic adapter (reverse proxy)
- `proxy/proxy.go:250` — behavior-based dispatch (`Registry.Lookup(provider.Behavior)`)
- `proxy/tui/picker.go:60-79` — behavior picker (3 choices, no protocol)
- `proxy/tui/model.go:663-679` — provider form (no mix-specific fields)
- `internal/genproviders/main.go:83-98` — `supportsCountTokens()` codegen logic
- `providers.yaml:23-117` — all 16 provider definitions
- `config.example.yaml:1-15` — example config with mix providers

## Architecture Insights

1. **Two-level routing**: The dispatcher does behavior-based dispatch (`openai`/`anthropic`/`mix`), then `MixAdapter` does URL-based sub-routing. This is a clean composite pattern but the second level has no config escape hatch.

2. **Protocol = adapter selection**: In freedius, "protocol" means "which wire format the upstream expects." The `mix` adapter is the only one that supports both. Adding `protocol` to config is the natural extension point.

3. **Codegen as source of truth**: `providers.yaml` → `go generate` → `providers_gen.go` + `adapters_gen.go`. Static flags like `SupportsCountTokens` are computed at this stage. Dynamic user overrides can't influence them without changing the architecture.

4. **Config-write, no config-read**: The TUI writes config via `config.Save()` but the proxy never re-reads the file. This is a deliberate simplicity choice, not a bug.

## Historical Context

- `context/archive/custom-to-mix-protocol/plan.md` — S-06 plan: add `protocol` field, rewrite `custom → mix`, delete `CustomAdapter`. **Approved but never merged.**
- `context/archive/custom-to-mix-protocol/reviews/impl-review.md` — Implementation review: APPROVED, 0 critical, 1 warning, 3 observations (all fixed).
- `context/changes/add-popular-providers/` — Added 9 popular providers (Google, Mistral, DeepSeek, etc.) with `behavior: openai` and default URLs. Auto-injection via `applyDefaults` for providers with `DefaultBaseURL`.
- `context/foundation/lessons.md:57-69` — Lesson: "Adding New Providers: Auto-Inject + Env-Var Scope" — auto-inject only providers with default URLs; check env vars only for providers referenced by mappings.
- `context/foundation/roadmap.md:135-145` — S-06 roadmap item: "Custom → mix + protocol field"

## Recommended Improvements (prioritized)

### 1. Merge the S-06 `protocol` field (HIGH)

The implementation exists, was reviewed and approved, and all tests passed. It just needs to be merged. This single change resolves the URL-sniffing fragility issue by giving users an explicit override.

**Effort**: Low (code already written)
**Impact**: High (unblocks ambiguous URL configs, removes `CustomAdapter` complexity)

### 2. Make `SupportsCountTokens` runtime-aware (MEDIUM)

Instead of a static codegen flag, compute `SupportsCountTokens` at runtime based on the effective `DefaultBaseURL` (after user overrides). Options:
- **A**: Check `strings.HasSuffix(provider.DefaultBaseURL, "/v1/messages")` in `applyDefaults`
- **B**: Add a `supports_count_tokens: true|false` YAML field that overrides the codegen default
- **C**: Remove the static flag entirely and always check the URL at dispatch time

Option A is simplest and follows the existing pattern. Option B gives users full control. Option C is the most correct but requires changes to the dispatcher.

**Effort**: Low-Medium
**Impact**: Medium (fixes silent behavioral downgrade for overridden URLs)

### 3. Add protocol-aware TUI hints (LOW)

Once the `protocol` field exists:
- Add a `protocol` picker to the provider form (for `mix` behavior only)
- Show protocol auto-detection hint when `mix` is selected
- Display effective protocol in the providers tab

**Effort**: Low
**Impact**: Low (UX improvement for TUI users)

### 4. Add missing edge-case tests (LOW)

Priority test gaps:
- Mix provider with ambiguous URL + explicit protocol
- Auto-injected provider referenced by a mapping
- Config round-trip with protocol field

**Effort**: Low
**Impact**: Low (defense-in-depth)

### 5. Consider config hot-reload (DEFER)

File watching (`fsnotify`) or SIGHUP-based reload. Not critical for single-user local tool. Only worth implementing if users report friction from restarting after config edits.

**Effort**: Medium
**Impact**: Low

## Open Questions

1. **Was the S-06 implementation intentionally not merged, or was it an oversight?** The plan archive shows all automated tests passed and the review was approved. The `providers` branch may have diverged.

2. **Should `protocol` be on `Provider` or `Mapping`?** The S-06 plan puts it on `Provider`, meaning all mappings for a mix provider share the same protocol. If a user needs different protocols for different models on the same provider, they'd need separate provider entries. This seems acceptable for the current architecture.

3. **Should the `MixAdapter` support more than two sub-adapters?** Currently it's OpenAI or Anthropic. If a third protocol (e.g., Google Gemini native) is needed, the adapter would need extension. The `protocol` field design should accommodate this (e.g., `protocol: google`).

## Related Research

- `context/changes/missing-providers-tui/research.md` — Why popular providers were missing from TUI (stale generated files)

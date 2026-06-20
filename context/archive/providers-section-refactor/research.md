---
date: "2026-06-19T00:00:00+02:00"
researcher: opencode
git_commit: b16cd8b1a01e0c6e09c3339877f4ae885ff9a517
branch: tui-dashboard
repository: freedius
topic: "Move provider settings to providers section, simplify mappings to name→provider+model"
tags: [research, config, providers, mappings, refactor, breaking-change]
status: complete
last_updated: 2026-06-19
last_updated_by: opencode
---

# Research: Provider/Mapping Structural Refactor

**Date**: 2026-06-19T00:00:00+02:00
**Researcher**: opencode
**Git Commit**: b16cd8b1a01e0c6e09c3339877f4ae885ff9a517
**Branch**: tui-dashboard
**Repository**: freedius

## Research Question

Move provider settings (url, type, api_key) into a dedicated `providers` section. Mappings should only contain name → provider_name + model (freetext). Models don't need separate setup in any other place than mappings.

## Summary

The current architecture conflates two concerns into one `config.Model` struct: provider identity/configuration and model routing. Both `Config.Models` and `Config.Mappings` use the same `config.Model` type with 7 fields (`Provider`, `Model`, `BaseURL`, `APIKeyEnv`, `AnthropicVersion`, `Protocol`, `OriginalProvider`). This means every mapping entry must redundantly specify `base_url`, `api_key_env`, etc. even when multiple mappings share the same provider.

The proposed refactor splits this into:
- **`Providers`**: A new `map[string]Provider` with `Behavior`, `DefaultBaseURL`, `DefaultAPIKeyEnv`, `RequireBaseURL`
- **`Mappings`**: A simplified `map[string]Mapping` with only `ProviderName string` + `ModelString string`

Alias rewriting (`custom`/`zen`/`go` → `mix`) is eliminated — aliases become first-class provider entries with `Behavior: "mix"` directly. The two-phase rewrite logic in `applyEntryDefaults` is removed entirely. This affects ~20 files across config, proxy, TUI, tests, generators, and templates.

## Detailed Findings

### 1. Current Config Data Model

**File**: `config/config.go:16-30`

```go
type Config struct {
    Models   map[string]Model `yaml:"models"`
    Mappings map[string]Model `yaml:"mappings,omitempty"`
}

type Model struct {
    Provider         string // yaml:"provider"
    Model            string // yaml:"model" — upstream model name
    BaseURL          string // yaml:"base_url,omitempty"
    APIKeyEnv        string // yaml:"api_key_env,omitempty"
    AnthropicVersion string // yaml:"anthropic_version,omitempty"
    Protocol         string // yaml:"protocol,omitempty"
    OriginalProvider string // yaml:"-" — runtime-only, captures alias name before rewrite
}
```

Both `Models` and `Mappings` are `map[string]Model` — same type. This is the root of the conflation.

### 2. Config Loading and Defaults Pipeline

**Current flow** (`config/config.go:33-58`, `config/defaults.go:25-32`, `config/providers_gen.go:71-95`):

```
Load (read YAML → strict unmarshal)
  → applyDefaults (iterates Models + Mappings)
    → applyEntryDefaults per entry:
      Phase A: capture OriginalProvider, custom→mix rewrite
      Phase B: fill BaseURL/APIKeyEnv from knownProviderDefaults
               then zen→mix, go→mix rewrites
  → validate (iterates Models + Mappings)
    → validateModel per entry (checks provider known, model non-empty,
      base_url valid URL, requiresBaseURL satisfied, api_key_env safe,
      protocol valid)
```

**Proposed flow**:
```
Load (read YAML → strict unmarshal into new Config shape)
  → applyDefaults:
    → applyProviderDefaults: merge gen'd defaults into Providers map
    → applyMappingDefaults: no-op (mappings have no defaults)
  → validate:
    → validateProvider (behavior valid, base_url valid, api_key_env safe)
    → validateMapping (ProviderName exists in Providers, ModelString safe)
```

**Key deletions**:
- The `OriginalProvider` field and all alias rewriting (two-phase rewrite pattern)
- `applyEntryDefaults()` function — replaced by `applyProviderDefaults`
- `ProviderInfo()` hardcoded switch (`config/config.go:63-82`) — provider metadata now lives in the `Provider` struct
- `sortedKnownProviders()`, `PresetProviders()`, `originalProviderName()` functions

### 3. Per-Field Usage Trace (Model fields → consumers)

| Field | Consumers | Proposed home |
|---|---|---|
| `Provider` | `Registry.Lookup`, `supportsCountTokens`, `originalOr`, validate | **Mapping.ProviderName** (references a Providers key) |
| `Model` | `translate.Request` (upstream model name), debug logging | **Mapping.ModelString** |
| `BaseURL` | `AnthropicCompat.Handle`, `OpenAICompat.Handle`, `MixAdapter.Handle`, `supportsCountTokens` | **Provider.DefaultBaseURL** |
| `APIKeyEnv` | `AnthropicCompat.Handle`, `OpenAICompat.Handle`, `main.checkRequiredEnvVars` | **Provider.DefaultAPIKeyEnv** |
| `AnthropicVersion` | `AnthropicCompat.Handle` only | **Provider** (or remain per-model override) |
| `Protocol` | `MixAdapter.Handle` routing, `supportsCountTokens` | **Determined by Provider.Behavior** |
| `OriginalProvider` | `originalOr()` for logging/errors | **Deleted** — providers use their real names |

### 4. Adapter Handle Signature Impact

All 4 adapters (NIM, OpenAI, Anthropic, Mix) receive `m config.Model` via the `Provider` interface (`proxy/provider.go:13`). Every adapter reads `m.BaseURL` and `m.APIKeyEnv`. Every adapter reads `m.Provider` (directly or via `originalOr`).

**Option A**: Change `Handle` signature to accept both `Provider` and `Mapping`:
```go
Handle(w, r, provider config.Provider, mapping config.Mapping, body)
```

**Option B**: Resolver struct — dispatcher resolves everything and passes a combined value:
```go
type ResolvedModel struct {
    ModelString string
    BaseURL     string
    APIKey      string
    Protocol    string
}
Handle(w, r, resolved ResolvedModel, body)
```

**Recommendation**: Option A preserves clear separation. The adapter knows both "what provider to talk to" and "what model to ask for".

### 5. Alias Rewriting Elimination

**Current**: `zen`/`go`/`custom` are aliases that rewrite to `mix` at config-load time. The `OriginalProvider` field captures the original name for error messages and YAML round-tripping.

**Proposed**: Each becomes a full `Provider` entry with `Behavior: "mix"`:
```yaml
providers:
  zen:    { behavior: mix, default_api_key_env: OPENCODE_API_KEY, require_base_url: true }
  go:     { behavior: mix, default_api_key_env: OPENCODE_API_KEY, require_base_url: true }
  custom: { behavior: mix, require_base_url: true }
```

No runtime rewriting. `Registry.Lookup` looks up the adapter by `Behavior`, not by provider name. All 3 route to the same `MixAdapter` instance.

### 6. Generator Changes (internal/genproviders/main.go)

**Current genproviders output**:
- `config/providers_gen.go`: `KnownProviders`, `knownProviderDefaults`, `requireBaseURL`, `PresetProviders`, `applyEntryDefaults`
- `proxy/adapters_gen.go`: thin adapter wrappers + `NewDefaultRegistry`

**Proposed output**:
- `config/providers_gen.go`: `providerDefaultsMap map[string]Provider` — a literal map of all providers with their defaults from `providers.yaml`, used by `applyProviderDefaults`
- `proxy/adapters_gen.go`: unchanged (still generates thin adapter wrappers + `NewDefaultRegistry`)

**`providers.yaml` changes**:
- Remove `rewrite_to` field
- `zen`, `go`, `custom` are now standalone entries with `behavior: mix`
- No pre/post-lookup rewrite logic needed

### 7. Validation Changes

**Current** `validateModel` (per entry, 65 lines, `config/config.go:113-197`): validates 7 fields uniformly, checks provider against `KnownProviders`, checks `requiresBaseURL` map, validates protocol.

**Proposed split**:

**`validateProvider`**:
- `Behavior` must be one of `{openai, anthropic, mix}`
- `DefaultBaseURL` if set must be valid HTTP(S) URL
- `DefaultAPIKeyEnv` if set must not contain `\r\n=`
- If `RequireBaseURL && DefaultBaseURL == ""` → error

**`validateMapping`**:
- `ProviderName` must exist in `Providers` map
- `ModelString` must be non-empty, no `\r\n:`

The `requireBaseURL` field moves from a compile-time set (`requireBaseURL map[string]struct{}`) to a field on the `Provider` struct.

### 8. TUI Changes

**Current**: Single 7-field form for both models and mappings. No way to add mappings via TUI (`openAddForm` hardcodes `formKind = "model"`).

**Required changes** (comprehensive list from sub-agent 4):

1. **`proxy/tui/model.go`**:
   - New form modes: `formEditProvider`, `formAddProvider`, `formEditMapping`, `formAddMapping`
   - `fieldLabel()` split into `providerFieldLabels()` (6 fields) and `mappingFieldLabels()` (3 fields)
   - `openEditForm()` branches on entry kind → different form layouts
   - `openAddForm()` split into `openAddProviderForm()` and `openAddMappingForm()`
   - `validateForm()` split per form kind
   - `submitForm()` split — builds `Provider` or `Mapping` struct
   - `handleDeleteConfirmKeyPress()` adds `formKind == "provider"` branch
   - New key binding: `p` to add provider

2. **`proxy/tui/views.go`**:
   - `renderForm()` renders different field layouts based on `formMode`
   - `renderConfigTab()` shows two sections: Providers + Mappings
   - `renderProvidersTab()` reads directly from `cfg.Providers`
   - `collectProvidersFromConfig()` simplified — no `OriginalProvider` logic
   - `collectAllModels()` split into `collectAllProviders()` + `collectAllMappings()`

3. **`proxy/tui/picker.go`**:
   - Mapping form picker: lists keys from `cfg.Providers`, not `KnownProviders`
   - Provider type picker (new): for selecting `Behavior` when adding/editing providers

### 9. main.go Changes

**`checkRequiredEnvVars()`** (`main.go:276-301`): Simplified — iterates `cfg.Providers` only. No longer iterates mappings (env vars are provider-level). `originalProviderName()` deleted.

**`resolveConfigPath()`**: No change (path resolution is format-agnostic).

### 10. Files NOT Changing

- `proxy/adapters_gen.go` — generated registry wiring is unchanged
- `proxy/translate/` — operates on raw bytes + target model string, no config.Model dependency
- `proxy/count_tokens_local.go` — only reads `m.Model` for debug logging
- `proxy/nim_sanitize.go` — operates on raw body bytes, no config dependency
- `proxy/eventbus.go` — `RequestEvent` is a separate struct
- `proxy/middleware_test.go` — no config dependency
- `internal/envinject/` — reads env vars directly, no config dependency

## Code References

Key files requiring changes (ordered by impact):

- `config/config.go:16-30` — `Config` and `Model` struct definitions (root of all changes)
- `config/config.go:33-258` — `Load`, `ProviderInfo`, `UsesProvider`, `validate`, `validateModel`, `Marshal`, `Save`
- `config/defaults.go:10-32` — `modelDefaults`, `ProviderEnvVar`, `applyDefaults`
- `config/providers_gen.go:1-95` — generated code; must be regenerated
- `providers.yaml:32-70` — remove `rewrite_to`, make aliases standalone
- `internal/genproviders/main.go:40-95,334-417` — `Provider` struct, templates, rewrite logic
- `main.go:276-308` — `checkRequiredEnvVars`, `originalProviderName`
- `proxy/proxy.go:151-158,200,263-268` — dispatcher lookup, `originalOr`
- `proxy/provider.go:13` — `Provider` interface `Handle` signature
- `proxy/anthropic_compat.go:39-93` — reads `m.BaseURL`, `m.APIKeyEnv`, `m.AnthropicVersion`
- `proxy/openai_compat.go:59-142` — reads `m.BaseURL`, `m.APIKeyEnv`, `m.Model`
- `proxy/mix.go:44-73` — reads `m.Protocol`, `m.BaseURL` for routing
- `proxy/capabilities.go:33-53` — `supportsCountTokens` reads `m.Provider`, `m.Protocol`, `m.BaseURL`
- `proxy/tui/model.go:367-534` — form handling, validation, submit
- `proxy/tui/views.go:117-233` — config rendering, provider collection
- `proxy/tui/picker.go:26-86` — provider picker
- `templates/starter.yaml` — template content
- `config.example.yaml:1-11` — example content
- `config/config_test.go:1-808` — ~26 test cases, most rewritten
- `config/original_provider_test.go` — deleted (alias tests no longer needed)
- `main_test.go` — checkRequiredEnvVars tests rewritten
- `proxy/*_test.go` (15+ files) — fixture construction with new types

## Architecture Insights

1. **Conflation pattern**: The current `Model` struct serves as both "provider configuration" and "model routing" — this is the single problem driving all complexity. Every mapping entry redundantly carries provider settings.

2. **Alias rewriting as complexity sink**: The two-phase rewrite (`custom` before defaults lookup, `zen`/`go` after) requires `OriginalProvider` tracking, round-trip preservation in `Marshal`, and scattered `originalOr()` calls for error messages. Eliminating this removes ~150 lines of ceremony.

3. **Adapter pattern is sound**: The `Provider` interface and `Registry` pattern need minimal changes — adapters already receive a `config.Model` and extract what they need. The change is in *how* those values arrive, not in the adapter logic.

4. **TUI form is the most affected component**: The single 7-field form is deeply baked into the TUI. Splitting into provider form (6 fields) and mapping form (3 fields) touches ~200 lines in `model.go` alone, plus field validation, submission, rendering, and tests.

5. **Tests will need the most typing**: ~30 test files construct `config.Model` literals. Most will need new struct shapes, but the test *logic* (what's being tested) often remains the same.

## Historical Context

No prior research or plans exist for this specific refactor. The `providers-section-refactor` change folder was created fresh for this work. Related prior work:
- `context/changes/tui-config-setup/` — added the TUI config editing form; form structure derives from the current `Model` struct
- `context/changes/error-code-collapse/` — touched error handling in adapters but didn't change config structure

## Related Research

- `context/changes/tui-dashboard/research.md` — TUI architecture research; relevant for understanding form patterns
- `context/changes/tui-config-setup/research.md` — TUI config editing research; shows how the 7-field form was designed

## Open Questions

1. **AnthropicVersion placement**: Currently a per-model field. Should it stay per-model (different models may need different versions) or move to provider level? Most likely provider-level since it's a property of the upstream API endpoint.

2. **Protocol override**: Currently `m.Protocol` allows overriding the mix adapter's URL-sniffing. With `Behavior` on the provider, is a per-mapping protocol override still needed? Probably not — routing is now determined by provider behavior + URL path suffix.

3. **Backward compatibility**: Should `freedius.yaml` files in the old format be auto-migrated? The `init` subcommand writes the starter template; existing user configs would break. A migration tool or a warning message with migration instructions may be needed.

4. **Adapter Handle signature**: Option A (two params) vs Option B (resolved struct). Option A preserves clarity but changes the `Provider` interface; Option B is less intrusive to adapter code but adds a new type. Recommendation: Option A.

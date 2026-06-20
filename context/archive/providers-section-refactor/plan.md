# Provider/Mapping Structural Refactor — Implementation Plan

## Overview

Split the conflated `config.Model` struct (7 fields serving both provider identity and model routing) into two separate types: `Provider` (behavior, base_url, api_key, protocol routing) and `Mapping` (name → provider_name + freetext model string). Eliminate the two-phase alias rewriting system (`custom`/`zen`/`go` → `mix`) by making aliases first-class provider entries. Hard-break old config format.

## Current State Analysis

**`config.Model`** (`config/config.go:22-30`) carries 7 fields (`Provider`, `Model`, `BaseURL`, `APIKeyEnv`, `AnthropicVersion`, `Protocol`, `OriginalProvider`) and is used as the value type for **both** `Config.Models` and `Config.Mappings` maps. This means every mapping redundantly specifies provider settings.

**Alias rewriting** (`config/providers_gen.go:71-95`): `custom`/`zen`/`go` rewrite to `mix` at load time via a two-phase `applyEntryDefaults`. The `OriginalProvider` field preserves the original name for error messages and YAML round-tripping. This is ~150 lines of ceremony across config, proxy, and TUI.

**Adapter interface** (`proxy/provider.go:13`): `Handle(w, r, m config.Model, body)` — every adapter receives the full `Model` and extracts what it needs. `OpenAICompatibleAdapter` reads `m.BaseURL`, `m.APIKeyEnv`, `m.Model`; `AnthropicCompatibleAdapter` reads all three plus `m.AnthropicVersion`; `MixAdapter` reads `m.Protocol` for routing.

**TUI** (`proxy/tui/model.go:367-534`): A single 7-field form handles both models and mappings. `openAddForm()` hardcodes `formKind = "model"` — there's no way to add a mapping via TUI. `collectAllModels()` merges both maps into one flat list.

**Generator** (`internal/genproviders/main.go`): Emits `KnownProviders`, `knownProviderDefaults`, `requireBaseURL`, `PresetProviders`, and the two-phase `applyEntryDefaults` function from `providers.yaml`.

### Key Discoveries:

- `config/config.go:63-82` — `ProviderInfo()` is a hardcoded switch returning behavior/apiKeyEnv/baseURL for each known provider. This function is deleted; provider metadata lives in the `Provider` struct.
- `config/providers_gen.go:71-95` — `applyEntryDefaults` saves `OriginalProvider`, runs pre/post-lookup rewrites, applies defaults. Replaced by a simple merge of generated provider defaults into user's `Providers` map.
- `config/config.go:210-230` — `Marshal()` clones maps and restores `OriginalProvider` for round-trip fidelity. Simplifies to direct YAML marshal since there's nothing to sanitize.
- `proxy/proxy.go:151-158` — dispatcher does 3-tier lookup: `Models`, then `Mappings`, then family-fallback. `Models` map is gone; dispatch is `Mappings` → family fallback only.
- `proxy/tui/model.go:419` — `openAddForm` hardcodes `formKind = "model"`. Must split into `openAddProviderForm` and `openAddMappingForm`.

## Desired End State

A `freedius.yaml` where provider configuration lives under `providers:` (behavior, base_url, api_key_env, protocol routing) and model routing lives under `mappings:` (only name → provider_name + model_string). No alias rewriting — `zen`, `go`, `custom` are standalone provider entries. The TUI has separate forms for adding/editing providers (`p` key) and mappings (`a` key). Config loading validates that mapping `ProviderName` references a defined provider in the `providers` map.

### Verification:

- `go test ./...` passes with updated tests
- `go vet ./...` clean
- `go build -o freedius .` succeeds
- `freedius init` writes a config with the new `providers:` / `mappings:` format
- Old-format configs fail with clear YAML strict-unmarshal errors
- TUI shows separate Provider and Mapping sections with distinct add/edit forms

## What We're NOT Doing

- Auto-migration of old config format
- Supporting `models:` as a YAML key (renamed to `providers:`)
- Per-mapping base_url or api_key_env overrides (pure mapping only)
- `Protocol` as a per-mapping override field
- Changing family-pattern matching in the dispatcher (kept as-is)
- Changing the event bus or middleware
- Modifying `proxy/translate/` package (no `config.Model` dependency)
- Modifying `internal/envinject/` (no config dependency)

### Addenda

- **Local token counting** (`proxy/count_tokens_local.go`): Added during implementation — provides local BPE-based token counting via `translate.CountInputTokens` for providers where `SupportsCountTokens` is false. Integrated into dispatcher at `proxy.go:209` as `d.serveLocalCountTokens`. Not in original plan; emerged as a natural complement to the static `SupportsCountTokens` flag since providers without upstream count-tokens support now silently fail instead of returning a clear error.

## Implementation Approach

**Phased, data-model-first**: Define new types → update adapters to accept them → split TUI → update generator. Each phase is testable at its boundary. Phase 1 produces a compilable config package with the new types; Phase 2 updates adapters so the proxy compiles; Phase 3 fixes TUI; Phase 4 regenerates code.

**Hard break on config format**: Old YAML with `models:` key or inline `base_url`/`api_key_env` in mappings fails with strict YAML parse errors. No compatibility path.

**No per-mapping overrides**: Mappings are `ProviderName + ModelString` only. If a user needs different base_url/api_key for the same provider behavior, they create separate provider entries.

## Critical Implementation Details

- **Marshal simplification**: `Marshal()` (`config/config.go:210`) no longer needs to clone maps or restore `OriginalProvider`. Direct `yaml.Marshal(c)` is sufficient since all user-facing fields are tagged with yaml struct tags and there are no runtime-only fields to strip.

- **Adapter signature ordering**: `Handle(w, r, provider config.Provider, mapping config.Mapping, body)` — Provider first (the "who"), Mapping second (the "what"). The dispatcher resolves both before calling `adapter.Handle`. This touches every adapter's `Handle` method and all mock adapters in tests.

- **TUI cursor tracking**: After splitting `collectAllModels()` into `collectAllProviders()` + `collectAllMappings()`, the single `configCursor int` in `Dashboard` must become two cursors (one per list) or track which list is active. The simplest approach: render providers first, then mappings, with a single cursor that spans both lists. Form entry type (`provider` vs `mapping`) is determined by the index relative to the boundary between the two lists.

## Phase 1: New Config Data Model

### Overview

Define `Provider` and `Mapping` structs. Update `Config` to use them. Rewrite `Load`, validation, `Save`, and `Marshal`. Update `checkRequiredEnvVars` in main.go. Update starter template and example config. Update all config package tests.

### Changes Required:

#### 1.1 Config Structs

**File**: `config/config.go`

**Intent**: Replace the single `Model` struct with two structs — `Provider` (provider-level settings) and `Mapping` (routing entry). Update `Config` to hold `Providers map[string]Provider` and `Mappings map[string]Mapping`.

**Contract**:

```go
type Config struct {
    Providers map[string]Provider `yaml:"providers"`
    Mappings  map[string]Mapping  `yaml:"mappings,omitempty"`
}

type Provider struct {
    Behavior            string `yaml:"behavior"`
    DefaultBaseURL      string `yaml:"default_base_url,omitempty"`
    DefaultAPIKeyEnv    string `yaml:"default_api_key_env,omitempty"`
    AnthropicVersion    string `yaml:"anthropic_version,omitempty"`
    RequireBaseURL      bool   `yaml:"-"`
    SupportsCountTokens bool   `yaml:"-"`
}

type Mapping struct {
    ProviderName string `yaml:"provider_name"`
    ModelString  string `yaml:"model_string"`
}
```

Remove the old `Model` struct entirely. Remove all alias-related fields: `OriginalProvider`, `Protocol` (was per-model override). `SupportsCountTokens` and `RequireBaseURL` are runtime-only (`yaml:"-"`) — set by `applyDefaults` from generated provider metadata.

#### 1.2 Validation

**File**: `config/config.go`

**Intent**: Split `validateModel()` into `validateProvider()` and `validateMapping()`. Remove alias rewriting and `requireBaseURL` map lookups. `validateMapping` checks that `m.ProviderName` exists in `c.Providers`.

**Contract**:

- `validateProvider(name, p)` — `Behavior` must be one of `{"openai", "anthropic", "mix"}`; if `DefaultBaseURL` is set, must be valid HTTP(S) URL; if `DefaultAPIKeyEnv` is set, must not contain `\r\n=`.
- `validateMapping(name, m, providers)` — `m.ProviderName` must exist as a key in `providers` map; `m.ModelString` must be non-empty and not contain `\r\n:`.
- `validate()` iterates `c.Providers` calling `validateProvider`, then `c.Mappings` calling `validateMapping` with `c.Providers` as the lookup map.

#### 1.3 Load, Save, Marshal

**File**: `config/config.go`

**Intent**: `Load()` reads new YAML structure, calls `applyDefaults`, validates. `Marshal()` simplifies to direct `yaml.Marshal(c)` since there are no runtime-only fields to sanitize. `Save()` is unchanged (format-agnostic).

**Contract**:

- `Load()`: empty check becomes `len(cfg.Providers) == 0 && len(cfg.Mappings) == 0`.
- `Marshal()`: `return yaml.Marshal(c)` — no clone, no `OriginalProvider` restoration.
- `Save()`: calls `c.validate(path)` then `c.Marshal()` then writes, same backup/rollback logic.

#### 1.4 Delete ProviderInfo and Aliases

**File**: `config/config.go`

**Intent**: Remove `ProviderInfo()` (hardcoded switch), `sortedKnownProviders()`, `originalProviderName()` (in `main.go`). Remove `UsesProvider()` or rewrite to check `c.Providers` keys + `c.Mappings` entries.

**Contract**:

- `ProviderInfo()` deleted — `proxy/tui/picker.go` and `proxy/tui/model.go` will read from `cfg.Providers` directly.
- `UsesProvider()` rewritten to check `_, ok := c.Providers[name]` and iterate `c.Mappings` checking `m.ProviderName`.

#### 1.5 Defaults

**File**: `config/defaults.go`

**Intent**: `applyDefaults()` now merges generated provider defaults (from genproviders) into the user's `Providers` map. `applyEntryDefaults()` is deleted. `modelDefaults` struct deleted. `ProviderEnvVar()` now reads from the providers map.

**Contract**:

- `applyDefaults()`: for each provider in `c.Providers`, if it matches a generated default entry, fill empty `DefaultBaseURL`, `DefaultAPIKeyEnv`, and set `RequireBaseURL`/`SupportsCountTokens` runtime fields.
- `ProviderEnvVar(name string) string` — rewritten to accept providers map parameter or deleted if callers look up directly.
- The generated defaults map is imported from `providers_gen.go` as a package-level `var providerDefaults`.

#### 1.6 Starter Template & Example

**File**: `templates/starter.yaml`, `config.example.yaml`

**Intent**: Rewrite to new format with `providers:` and `mappings:` sections.

**Contract**:

```yaml
providers:
  nim: { behavior: openai, default_api_key_env: NVIDIA_NIM_API_KEY }
  go:  { behavior: mix, default_base_url: https://opencode.ai/zen/go/v1/chat/completions, default_api_key_env: OPENCODE_API_KEY }
  zen: { behavior: mix, default_base_url: https://opencode.ai/zen/v1/messages, default_api_key_env: OPENCODE_API_KEY }

mappings:
  opus:    { provider_name: go, model_string: deepseek-v4-pro }
  sonnet:  { provider_name: go, model_string: deepseek-v4-flash }
  haiku:   { provider_name: zen, model_string: claude-sonnet-4-6 }
  auto:    { provider_name: nim, model_string: step-3.5 }
  default: { provider_name: nim, model_string: step-3.5 }
```

Zero `models:` section. No `base_url` or `api_key_env` in mappings. `model_string` is freetext.

#### 1.7 main.go Updates

**File**: `main.go`

**Intent**: `checkRequiredEnvVars` simplified to iterate `cfg.Providers` only (env vars are provider-level). `originalProviderName()` deleted.

**Contract**:

- `checkRequiredEnvVars(cfg)`: iterate `cfg.Providers`, for each with non-empty `DefaultAPIKeyEnv`, check `os.Getenv` is set. Return descriptive error if missing.
- Delete `originalProviderName()` function entirely.
- Remove `slices.Contains(presets, ...)` logic — no more `PresetProviders()` call.

#### 1.8 Config Tests

**Files**: `config/config_test.go`, `config/original_provider_test.go`, `config/testhelpers_test.go`

**Intent**: Update all test fixtures to use new types. Delete alias-specific tests. Add tests for provider validation and mapping-to-provider reference validation.

**Contract**:

- `config/config_test.go`: rewrite ~26 test cases. YAML fixtures use `providers:` and `mappings:` with new field names. Assertions check `Provider.Behavior`, `Mapping.ProviderName`, `Mapping.ModelString` instead of `Model.Provider`, `Model.Model`. Delete tests for: alias rewriting (`custom→mix`, `zen→mix`, `go→mix`), `Protocol` field validation, `requireBaseURL` as map lookup. Add tests: mapping references non-existent provider, provider has invalid Behavior, mapping ModelString unsafe characters.
- `config/original_provider_test.go`: **deleted** — no `OriginalProvider` field.
- `config/testhelpers_test.go`: update any helpers that construct `Model` values.

### Success Criteria:

#### Automated Verification:

- `go test ./config/...` passes with new test cases
- `go vet ./config/...` clean
- `go build ./config/...` succeeds

#### Manual Verification:

- `freedius init` produces a `freedius.yaml` with separate `providers:` and `mappings:` sections
- Loading the starter config succeeds without error
- `freedius serve` starts and validates provider env vars correctly

---

## Phase 2: Adapter Signature Change

### Overview

Change the `Provider` interface's `Handle` signature to accept `Provider` + `Mapping` instead of `Model`. Update all 4 adapters. Update dispatcher lookup (remove `Models` map). Update `supportsCountTokens` to use `Provider.SupportsCountTokens`. Update all proxy tests.

### Changes Required:

#### 2.1 Provider Interface

**File**: `proxy/provider.go`

**Intent**: Change `Handle` signature from `(w, r, m config.Model, body)` to `(w, r, provider config.Provider, mapping config.Mapping, body)`.

**Contract**:

```go
type Provider interface {
    Handle(w http.ResponseWriter, r *http.Request, provider config.Provider, mapping config.Mapping, body []byte) error
}
```

#### 2.2 Dispatcher

**File**: `proxy/proxy.go`

**Intent**: Remove `d.Cfg.Models` map lookup from dispatch. Resolve mapping only. After matching, look up the provider from `d.Cfg.Providers[mapping.ProviderName]`. Delete `originalOr()`.

**Contract**:

- Dispatch flow: `d.Cfg.Mappings[req.Model]` → family fallback. No `d.Cfg.Models` lookup.
- After matching a Mapping, look up `provider, ok := d.Cfg.Providers[mapping.ProviderName]`. If not found, return 500 "provider_not_registered".
- Pass `provider` and `mapping` to `adapter.Handle(ww, r, provider, mapping, body)`.
- Delete `originalOr()` function. All logging/error messages that used `originalOr(m)` switch to `mapping.ProviderName`.
- Response headers (`X-Freedius-Matched-Provider`, `X-Freedius-Matched-Model`): set to `mapping.ProviderName` and `mapping.ModelString`.

#### 2.3 OpenAICompatibleAdapter

**File**: `proxy/openai_compat.go`

**Intent**: Update `Handle` signature. Read base URL, API key from `provider`; read model string from `mapping`.

**Contract**:

- `Handle(w, r, provider config.Provider, mapping config.Mapping, body)`:
  - `baseURL := provider.DefaultBaseURL` (was `m.BaseURL`)
  - `apiKey := os.Getenv(provider.DefaultAPIKeyEnv)` (was `m.APIKeyEnv`)
  - `targetModel := mapping.ModelString` (was `m.Model`)
  - `translate.Request(body, mapping.ModelString, ...)` — no change to translate call.

#### 2.4 AnthropicCompatibleAdapter

**File**: `proxy/anthropic_compat.go`

**Intent**: Same signature change. Read provider fields.

**Contract**:

- `Handle(w, r, provider config.Provider, mapping config.Mapping, body)`:
  - `baseURL := provider.DefaultBaseURL` (was `m.BaseURL`)
  - `apiKey := os.Getenv(provider.DefaultAPIKeyEnv)` (was `m.APIKeyEnv`)
  - `anthropicVersion := provider.AnthropicVersion; if empty → "2023-06-01"` (was `m.AnthropicVersion`)
  - Error messages reference `mapping.ProviderName` instead of `originalOr(m)`.

#### 2.5 MixAdapter

**File**: `proxy/mix.go`

**Intent**: Remove `m.Protocol` switch. Route based on provider's base URL path suffix only.

**Contract**:

- `Handle(w, r, provider config.Provider, mapping config.Mapping, body)`:
  - Remove `switch m.Protocol { case "anthropic": ... case "openai": ... }`.
  - Sniff `provider.DefaultBaseURL` path: if ends with `/v1/messages` → `a.anthropic.Handle(...)`, else → `a.openai.Handle(...)`.
  - Pass `provider` and `mapping` through to sub-adapters.

#### 2.6 NIMAdapter (generated)

**File**: `proxy/adapters_gen.go`

**Intent**: Update the generated `NIMAdapter.Handle` signature to match the new interface. No other logic changes — it's a thin wrapper that delegates to `OpenAICompatibleAdapter`.

**Contract**:

- Signature matches new `Provider` interface. Delegates `a.inner.Handle(w, r, provider, mapping, body)`.

#### 2.7 Capabilities

**File**: `proxy/capabilities.go`

**Intent**: `supportsCountTokens` now reads `Provider.SupportsCountTokens` (set at defaults-merge time) instead of duplicating routing logic.

**Contract**:

- `supportsCountTokens(provider config.Provider) bool` — returns `provider.SupportsCountTokens`.
- Callers in `proxy/proxy.go` pass the resolved provider instead of the old `m config.Model`.
- Delete the old function body's routing logic.

#### 2.8 Proxy Tests

**Files**: `proxy/proxy_test.go`, `proxy/provider_test.go`, `proxy/capabilities_test.go`, `proxy/adapter_errors_test.go`, `proxy/anthropic_compat_test.go`, `proxy/openai_compat_test.go`, `proxy/mix_test.go`, `proxy/nim_test.go`, `proxy/error_contract_test.go`, `proxy/error_propagation_test.go`

**Intent**: Update all `config.Model` literals in test fixtures to use `config.Provider` and `config.Mapping`. Update mock `Provider` implementations to match new `Handle` signature. Update `supportsCountTokens` test cases.

**Contract**:

- Test config fixtures: replace `Models: map[string]config.Model{...}` with `Providers: map[string]config.Provider{...}` + `Mappings: map[string]config.Mapping{...}`.
- Mock adapter `Handle` signatures: update to `Handle(w, r, config.Provider, config.Mapping, body)`.
- `capabilities_test.go`: test `supportsCountTokens` with `config.Provider{SupportsCountTokens: true/false}` instead of constructing `config.Model` with routing info.
- `originalOr` tests in `error_contract_test.go`: deleted (function removed).

### Success Criteria:

#### Automated Verification:

- `go test ./proxy/...` passes
- `go vet ./proxy/...` clean
- `go build ./proxy/...` succeeds

#### Manual Verification:

- Start freedius with a new-format config, send a request — it routes to the correct adapter
- Count tokens endpoint works for anthropic providers
- Mix adapter routes correctly based on base_url path suffix
- Error responses contain the correct provider name in logs

---

## Phase 3: TUI Split

### Overview

Split the single 7-field form into separate provider form (6 fields: name, behavior, base_url, api_key_env, protocol-type, anthropic_version) and mapping form (3 fields: name, provider_name, model_string). New key bindings: `a` to add mapping, `p` to add provider. Update views to render two sections. Update picker to list from `cfg.Providers`.

### Changes Required:

#### 3.1 Form Modes and Key Bindings

**File**: `proxy/tui/model.go`

**Intent**: Add new form mode constants. Split `fieldLabel()` into provider and mapping variants. Split `openEditForm()`, `openAddForm()`, `validateForm()`, `submitForm()` per entry kind. Update key bindings.

**Contract**:

- New form modes: `formEditProvider`, `formAddProvider`, `formEditMapping`, `formAddMapping` (add to existing `formNone`, `formEdit`, `formAdd`, `formDeleteConfirm`).
- `providerFieldLabels()` returns: `["name", "behavior", "base_url", "api_key_env", "protocol", "anthropic_version"]`. `protocol` here refers to the mix routing protocol type, not a per-mapping override.
- `mappingFieldLabels()` returns: `["name", "provider", "model"]`.
- `openEditForm()`: if `entry.kind == "provider"` → populate 6-field provider form with `cfg.Providers[name]` values, set `formMode = formEditProvider`. If `entry.kind == "mapping"` → populate 3-field mapping form with `cfg.Mappings[name]` values, set `formMode = formEditMapping`.
- `openAddForm()` split: `openAddProviderForm()` (6 empty fields, `formMode = formAddProvider`) and `openAddMappingForm()` (3 empty fields, `formMode = formAddMapping`).
- Key bindings in `handleTabModeKeyPress`: `p` → `openAddProviderForm()`, `a` → `openAddMappingForm()`, `e` → `openEditForm()` (unchanged, branches on entry kind).
- `validateForm()`: branches on `formMode`. Provider validation checks `Behavior` is one of `{openai, anthropic, mix}`, `DefaultBaseURL` if set is valid URL, `DefaultAPIKeyEnv` safe. Mapping validation checks `ProviderName` is a key in `d.config.Providers`, `ModelString` non-empty and no `\r\n:`.
- `submitForm()`: branches on `formMode`. Provider submit constructs `config.Provider{...}` and writes to `d.config.Providers`. Mapping submit constructs `config.Mapping{...}` and writes to `d.config.Mappings`.
- `handleDeleteConfirmKeyPress`: add `if d.formKind == "provider" { delete(d.config.Providers, ...) }` branch.

#### 3.2 Config Rendering

**File**: `proxy/tui/views.go`

**Intent**: `renderConfigTab()` shows two sections (Providers, then Mappings) with a single cursor spanning both. `renderProvidersTab()` reads from `cfg.Providers` directly. `collectAllModels()` split. `collectProvidersFromConfig()` simplified.

**Contract**:

- `renderConfigTab()`: render providers section first (header "Providers"), then mappings section (header "Mappings"). Cursor spans both — items 0..len(providers)-1 are providers, items len(providers).. are mappings. Highlight the active entry.
- `renderForm()`: branches on `formMode`. Provider modes render 6 fields with provider labels. Mapping modes render 3 fields with mapping labels.
- `collectProvidersFromConfig()`: iterate `cfg.Providers` only (no Models/Mappings aggregation). Return provider name + model count (count `cfg.Mappings` entries referencing this provider).
- `collectAllModels()`: **deleted**. Replaced by `collectAllProviders()` (iterates `cfg.Providers`, returns provider entries with `kind = "provider"`) and `collectAllMappings()` (iterates `cfg.Mappings`, returns mapping entries with `kind = "mapping"`). Both are called and concatenated in order (providers first, then mappings) to build the cursor list.
- `modelEntry` struct: rename field `model config.Model` → split into `provider *config.Provider` and `mapping *config.Mapping` (one is nil, the other set). Or use a `kind string` + interface. The simplest: keep `kind string` + add `provider config.Provider` and `mapping config.Mapping` fields.

#### 3.3 Provider Picker

**File**: `proxy/tui/picker.go`

**Intent**: The picker for mapping forms lists keys from `d.config.Providers` (the user's configured providers), not `config.KnownProviders`. For provider forms, the behavior field uses a fixed picker with `{openai, anthropic, mix}`.

**Contract**:

- `newProviderPicker()`: accepts a `[]string` of provider names (keys from `cfg.Providers`). Lists them as selectable items.
- `newBehaviorPicker()`: new picker with fixed items `[{openai}, {anthropic}, {mix}]` for the behavior field in provider forms.
- TUI form key handling: when `fieldLabel(focus) == "provider"` in mapping form → open provider picker. When `fieldLabel(focus) == "behavior"` in provider form → open behavior picker.

#### 3.4 TUI Tab Bar Label

**File**: `proxy/tui/views.go`

**Intent**: Update the config tab label to show new key bindings.

**Contract**:

- `renderTabs()`: change config tab label from `"[3] Config (e=edit a=add d=del)"` to `"[3] Config (e=edit a=+map p=+prov d=del)"`.

#### 3.5 TUI Tests

**Files**: `proxy/tui/model_test.go`, `proxy/tui/picker_test.go`

**Intent**: Update tests for separate forms. Add tests for provider form and mapping form. Add tests for behavior picker.

**Contract**:

- `model_test.go`: tests that previously checked `len(d.formFields) == 7` now check for 6 (provider form) or 3 (mapping form) depending on the test. Tests for `openAddProviderForm` and `openAddMappingForm` added. Tests for provider validation (behavior required, valid behavior set) and mapping validation (provider must exist in config). Test that `p` key opens provider form, `a` key opens mapping form.
- `picker_test.go`: update `TestProviderPicker_Selection` to accept a custom name list. Add `TestBehaviorPicker_Selection` testing the fixed behavior set.

### Success Criteria:

#### Automated Verification:

- `go test ./proxy/tui/...` passes
- `go vet ./proxy/tui/...` clean
- `go build ./proxy/tui/...` succeeds

#### Manual Verification:

- TUI starts and shows Providers tab with correct provider list and model counts
- Config tab shows providers section first, then mappings section, with cursor navigation
- Pressing `p` opens provider add form with 6 fields; behavior field opens picker
- Pressing `a` opens mapping add form with 3 fields; provider field opens picker with configured providers
- Editing and deleting providers and mappings works correctly
- Form validation catches invalid behaviors, missing provider references, unsafe model strings

---

## Phase 4: Generator Update & Cleanup

### Overview

Update `internal/genproviders/main.go` to emit a `providerDefaults` map (replacing `KnownProviders`, `knownProviderDefaults`, `requireBaseURL`, `PresetProviders`, and `applyEntryDefaults`). Update `providers.yaml` to remove `rewrite_to`. Regenerate `config/providers_gen.go` and `proxy/adapters_gen.go`. Update `main_test.go`. Final integration test.

### Changes Required:

#### 4.1 providers.yaml

**File**: `providers.yaml`

**Intent**: Remove `rewrite_to` field from all entries. `zen`, `go`, `custom` become standalone entries with `behavior: mix`. The generator no longer needs alias logic.

**Contract**:

```yaml
providers:
  nim:
    behavior: openai
    default_base_url: https://integrate.api.nvidia.com/v1/chat/completions
    default_api_key_env: NVIDIA_NIM_API_KEY
    require_base_url: false
    openai:
      no_stream_usage: true
      pre_send_hook: sanitizeNIMBody

  zen:
    behavior: mix
    default_api_key_env: OPENCODE_API_KEY
    require_base_url: true

  go:
    behavior: mix
    default_api_key_env: OPENCODE_API_KEY
    require_base_url: true

  custom:
    behavior: mix
    require_base_url: true

  openai:
    behavior: openai
    require_base_url: true

  anthropic:
    behavior: anthropic
    default_api_key_env: ANTHROPIC_API_KEY
    require_base_url: true

  mix:
    behavior: mix
    require_base_url: true
```

Key changes: `rewrite_to` removed from `zen`, `go`, `custom`. `anthropic` behavior implies `SupportsCountTokens: true` (set by generator, not explicit in YAML).

#### 4.2 Generator Code

**File**: `internal/genproviders/main.go`

**Intent**: Replace the config template to emit a `providerDefaults` map of `config.Provider` values instead of separate `KnownProviders`, `knownProviderDefaults`, `requireBaseURL`, `PresetProviders`, and `applyEntryDefaults`. Remove all rewrite-related fields and logic. Keep the proxy template unchanged (still emits thin adapters + `NewDefaultRegistry`).

**Contract**:

- `Provider` struct in genproviders: remove `RewriteTo`. `DefaultBaseURL` and `DefaultAPIKeyEnv` are now fields on the generated `Provider` entries.
- `configTmplData`: remove `PreLookupRewrites`, `PostLookupRewrites`. Replace with `ProviderDefaults []providerDefaultEntry` where each entry has: `Name`, `Behavior`, `DefaultBaseURL`, `DefaultAPIKeyEnv`, `RequireBaseURL`, `SupportsCountTokens`.
- `configTemplate`: emits a `var providerDefaults = map[string]config.Provider{...}` literal. No `applyEntryDefaults` function generated. No `KnownProviders` map generated. No `requireBaseURL` map. No `PresetProviders` function.
- `GenerateConfig()`: populates `ProviderDefaults` from spec entries. `SupportsCountTokens` is true when `Behavior == "anthropic"` or (`Behavior == "mix"` and `DefaultBaseURL` path ends with `/v1/messages`).
- `proxyTmplData` and `proxyTemplate`: **unchanged** (still generates `NewDefaultRegistry` wiring the 4 adapters). Thin wrapper logic (`NIMAdapter`) unchanged.
- Remove `Provider.hasDefaults()`, `isPreLookupRewrite()`, `isPostLookupRewrite()`, `hasAPIKeyEnvDefault()` methods.

#### 4.3 Regenerate

**Files**: `config/providers_gen.go`, `proxy/adapters_gen.go`

**Intent**: Run `go generate ./...` to produce fresh generated files from updated `providers.yaml` and genproviders.

**Contract**:

- `config/providers_gen.go`: contains `var providerDefaults = map[string]config.Provider{...}`. No `KnownProviders`, no `knownProviderDefaults`, no `requireBaseURL`, no `PresetProviders`, no `applyEntryDefaults`.
- `proxy/adapters_gen.go`: unchanged from current output (new `Handle` signature was already applied in Phase 2; regenerated code matches).

#### 4.4 main_test.go

**File**: `main_test.go`

**Intent**: Update tests for `checkRequiredEnvVars` to use new `Config` type. Remove `originalProviderName` tests.

**Contract**:

- Test fixtures: `Config{Providers: map[string]config.Provider{...}, Mappings: map[string]config.Mapping{...}}`.
- `TestCheckRequiredEnvVars`: checks that missing env vars for providers with `DefaultAPIKeyEnv` set produce errors. Checks that providers without `DefaultAPIKeyEnv` are skipped.
- Delete any `originalProviderName` tests.

#### 4.5 Genproviders Tests

**File**: `internal/genproviders/main_test.go`

**Intent**: Update assertions that check generated code output to match new template format.

**Contract**:

- Assert generated config code contains `var providerDefaults =` and `config.Provider{`.
- Assert no `applyEntryDefaults` function in generated output.
- Assert no `rewrite_to` references in generated output.

#### 4.6 Final Integration

**Intent**: Verify the full build, test suite, and end-to-end flow work correctly.

**Contract**:

- `go generate ./...` runs cleanly.
- `go build -o freedius .` succeeds.
- `go test ./... -count=1` passes all tests.
- `go vet ./...` clean.
- Manual: `freedius init` → `freedius serve` → send a test request → verify routing, response headers, and logging.

### Success Criteria:

#### Automated Verification:

- `go generate ./...` produces no diff (generated files match source of truth)
- `go test ./...` passes
- `go vet ./...` clean
- `go build -o freedius .` succeeds

#### Manual Verification:

- Full end-to-end: init → serve → request → verify correct routing
- TUI starts and displays correct provider/mapping info
- Error messages reference correct provider names (no stale `mix` aliases)

---

## Testing Strategy

### Unit Tests:

- Config validation: provider behavior validity, mapping provider reference, unsafe model strings, base_url validation
- Adapter Handle: correct field extraction from Provider + Mapping types
- Dispatcher: correct lookup flow (Mappings → family fallback, no Models)
- Capabilities: `SupportsCountTokens` flag derived correctly for each behavior type
- TUI: separate form field counts, validation per form kind, key bindings

### Integration Tests:

- Config round-trip: write → load → marshal → load (verify no data loss)
- Dispatcher end-to-end: config with providers + mappings → request → adapter receives correct values
- TUI form submit: provider form saves to `cfg.Providers`, mapping form saves to `cfg.Mappings`

### Manual Testing Steps:

1. `freedius init` — verify output format has `providers:` and `mappings:` sections
2. `freedius serve` with new config — verify startup and env var checking
3. Send a POST request matching a mapping name — verify `X-Freedius-Matched-*` headers
4. TUI: press `p` to add provider, fill form, submit, verify it appears in config tab
5. TUI: press `a` to add mapping, select provider from picker, submit, verify it appears
6. TUI: delete a provider entry, verify mappings referencing it are orphaned (this is expected — no referential integrity enforcement on delete)
7. Test with old-format config — verify clear YAML parse error message

## Performance Considerations

None — this is a data model refactor at load time. No runtime performance impact. The `providerDefaults` map is looked up once at config load during `applyDefaults`; adapter dispatch performs one extra map lookup (`d.Cfg.Providers[mapping.ProviderName]`) per request — negligible overhead.

## Migration Notes

This is a **hard break**. Existing `freedius.yaml` files with `models:` key or inline `base_url`/`api_key_env` in mappings will fail to load with strict YAML unmarshal errors. Users must rewrite their configs to the new format. The `freedius init` command writes the new format.

No auto-migration is provided. No backward compatibility alias for `models:` key.

## References

- Research: `context/changes/providers-section-refactor/research.md`
- Current `Model` struct: `config/config.go:22-30`
- Alias rewriting: `config/providers_gen.go:71-95`
- Dispatcher lookup: `proxy/proxy.go:151-158`
- TUI form: `proxy/tui/model.go:367-534`
- Generator: `internal/genproviders/main.go`
- providers.yaml source of truth: `providers.yaml:32-70`

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles.

### Phase 1: New Config Data Model

#### Automated

- [x] 1.1 `go test ./config/...` passes with new test cases — c68719d
- [x] 1.2 `go vet ./config/...` clean — c68719d
- [x] 1.3 `go build ./config/...` succeeds — c68719d

#### Manual

- [x] 1.4 `freedius init` produces correct new-format config — c68719d
- [x] 1.5 Loading starter config succeeds without error — c68719d
- [x] 1.6 `freedius serve` validates provider env vars correctly — c68719d

### Phase 2: Adapter Signature Change

#### Automated

- [x] 2.1 `go test ./proxy/...` passes — c68719d
- [x] 2.2 `go vet ./proxy/...` clean — c68719d
- [x] 2.3 `go build ./proxy/...` succeeds — c68719d

#### Manual

- [x] 2.4 Request routes to correct adapter with new-format config — c68719d
- [x] 2.5 Count tokens endpoint works for anthropic providers — c68719d
- [x] 2.6 Mix adapter routes based on base_url path suffix — c68719d
- [x] 2.7 Error logs contain correct provider names — c68719d

### Phase 3: TUI Split

#### Automated

- [x] 3.1 `go test ./proxy/tui/...` passes — c68719d
- [x] 3.2 `go vet ./proxy/tui/...` clean — c68719d
- [x] 3.3 `go build ./proxy/tui/...` succeeds — c68719d

#### Manual

- [x] 3.4 TUI Providers tab shows correct provider list and model counts — c68719d
- [x] 3.5 Config tab shows providers + mappings sections with cursor navigation — c68719d
- [x] 3.6 `p` opens provider add form, `a` opens mapping add form — c68719d
- [x] 3.7 Provider picker in mapping form lists configured providers — c68719d
- [x] 3.8 Behavior picker in provider form lists valid behaviors — c68719d
- [x] 3.9 Editing and deleting providers/mappings works — c68719d

### Phase 4: Generator Update & Cleanup

#### Automated

- [x] 4.1 `go generate ./...` produces no diff — c68719d
- [x] 4.2 `go test ./...` full suite passes — c68719d
- [x] 4.3 `go vet ./...` clean — c68719d
- [x] 4.4 `go build -o freedius .` succeeds — c68719d

#### Manual

- [x] 4.5 Full end-to-end: init → serve → request → verify routing — c68719d
- [x] 4.6 TUI displays correct info with regenerated code — c68719d

# SWE Provider Registry Expansion — Implementation Plan

## Overview

Expand freedius's provider registry so users can route to strong SWE/coding
models from Minimax and Xiaomi (DeepSeek already present), and confirm the
starter template continues to use only `nim`/`kilo`/`nous` models. The
`ensureDefaultMapping` empty-mappings guard is confirmed correct and is left
unchanged.

## Current State Analysis

- `providers.yaml` is the single source of truth; `go generate ./...` emits
  `config/providers_gen.go` and `proxy/adapters_gen.go` (config.go:16-17,
  gen.go directives).
- The registry already contains `deepseek` but not `minimax` or `xiaomi`.
- `templates/starter.yaml` (embedded via `//go:embed` at main.go:48) defines
  opus/sonnet/haiku/default tiers using only `nim`/`nous`/`kilo`.
- `ensureDefaultMapping` (config.go:182) injects a `default` catch-all that
  copies the alphabetically-first mapping when none is explicit; it returns
  early when `len(c.Mappings) == 0` (config.go:189). That guard is **not** a
  defect: with zero mappings `BuildMatchers` omits the catch-all
  (config.go:232) and `resolveMapping` returns `mapped=false`, which the
  dispatcher turns into a clean `no_match` JSON error (proxy.go:231-239) — not
  a 500. Injecting a `default` with nothing to copy would 500 on every request.
- The OpenAI adapter POSTs directly to `provider.DefaultBaseURL`
  (proxy/openai_compat.go:142), so a provider's `default_base_url` must be the
  full `…/chat/completions` endpoint.
- Starter tiers must stay NIM-first with `nous` before `kilo`, `kilo` last
  (enforced by `TestStarterTemplate_FallbackChainOrdering`,
  cmd/freedius/main_test.go:516).

### Key Discoveries:

- Xiaomi's API base URL is `https://token-plan-sgp.xiaomimimo.com/v1`
  (user-provided), so the full OpenAI endpoint is
  `https://token-plan-sgp.xiaomimimo.com/v1/chat/completions`.
- Minimax's OpenAI-compatible base is `https://api.minimax.com/v1/chat/completions`
  and its current frontier coding model is `MiniMax-M3`.
- `TestProviderDefaults` (config/config_test.go:587) enumerates the exact
  provider set and must be updated when the registry grows.

## Desired End State

- `minimax` and `xiaomi` are first-class providers available to any config
  (registry + generated tables), alongside the existing `deepseek`.
- The starter template is unchanged in behavior — it still only references
  `nim`/`kilo`/`nous` models — satisfying the requirement that starter models
  come from nim/kilo and providers are a separate concern.
- The empty-mappings guard remains as-is; a config with no mappings yields a
  clean `no_match`, not a crash.
- `mage test` is green and `mage lint`/`mage build` pass.

## What We're NOT Doing

- Not injecting `minimax`/`deepseek`/`xiaomi` model references into the starter
  tiers (starter stays nim/kilo/nous only).
- Not changing `ensureDefaultMapping`'s empty-mappings guard.
- Not adding a dedicated `coder` mapping to the starter (out of scope per user).
- Not modifying fallback-ordering or the NIM→nous→kilo contract.

## Implementation Approach

Treat this as a data/registry change, not a code change. Add two provider
entries to `providers.yaml`, regenerate the Go tables, and update the single
test that asserts the provider count. No dispatcher, adapter, or starter logic
is touched.

## Phase 1: Register minimax + xiaomi providers

### Overview

Add `minimax` and `xiaomi` to `providers.yaml` as `openai`-behavior providers,
regenerate the generated tables, and update `TestProviderDefaults` to include
them.

### Changes Required:

#### 1. Provider registry entries

**File**: `providers.yaml`

**Intent**: Declare `minimax` (base `https://api.minimax.com/v1/chat/completions`,
env `MINIMAX_API_KEY`) and `xiaomi` (base
`https://token-plan-sgp.xiaomimimo.com/v1/chat/completions`, env
`XIAOMI_API_KEY`) as `openai` providers so they merge into `providerDefaults`
like every other provider.

**Contract**: Two new entries under the `providers:` map, mirroring the
existing `deepseek`/`groq` shape (`behavior: openai`, `default_base_url`,
`default_api_key_env`, `require_base_url: false`).

#### 2. Regenerate provider tables

**File**: `config/providers_gen.go`, `proxy/adapters_gen.go`

**Intent**: Run `go generate ./...` so the new providers appear in
`providerDefaults` and the adapter switch.

**Contract**: Generated files must list `minimax` and `xiaomi`; no manual edits
to generated files.

#### 3. Update provider-count test

**File**: `config/config_test.go`

**Intent**: Add `"minimax"` and `"xiaomi"` to the expected slice in
`TestProviderDefaults` so the assertion (currently 18 → 20 entries) passes.

**Contract**: The `expected` string slice in `TestProviderDefaults`
(config/config_test.go:588) gains two entries; length check and membership
loop remain unchanged.

### Success Criteria:

#### Automated Verification:

- `go generate ./...` regenerates both generated files without diff noise beyond the two new providers.
- `go test ./config/...` passes, including `TestProviderDefaults` (20 entries).
- `go build ./...` succeeds.

#### Manual Verification:

- A config referencing `provider_name: minimax` / `xiaomi` loads without
  `provider_not_registered`.
- Starter template still boots with only `NVIDIA_NIM_API_KEY` set.

## Phase 2: Confirm starter scope + guard behavior

### Overview

Verify the starter template still uses only nim/kilo/nous and that the
empty-mappings guard produces a clean `no_match`.

### Changes Required:

#### 1. Starter template unchanged (verification only)

**File**: `cmd/freedius/templates/starter.yaml`

**Intent**: Confirm no minimax/deepseek/xiaomi references were added; tiers
remain NIM-first with nous→kilo ordering.

**Contract**: `TestStarterTemplate_FallbackChainOrdering` (main_test.go:516)
still passes; no edits required to the file.

#### 2. Empty-mappings guard (verification only)

**File**: `config/config.go` (`ensureDefaultMapping`)

**Intent**: Confirm the `len(c.Mappings) == 0` early return is intentional and
that a zero-mapping config yields `no_match`, not a 500.

**Contract**: No code change. Documented as correct in this plan and frame.md.

### Success Criteria:

#### Automated Verification:

- `go test ./cmd/freedius/...` passes (`TestStarterTemplate_*`).
- `mage test` green across the repo.

#### Manual Verification:

- A config with `mappings: {}` returns a structured `no_match` error for an
  arbitrary model (no panic / 500).

## Testing Strategy

### Unit Tests:

- `TestProviderDefaults` — registry now contains minimax + xiaomi (config_test.go:587).
- `TestStarterTemplate_FallbackChainOrdering` — starter still NIM-first, nous→kilo (main_test.go:516).

### Integration Tests:

- Existing proxy/adapter tests cover openai-behavior routing; no new integration
  test needed since no adapter logic changed.

### Manual Testing Steps:

1. Start freedius with the embedded starter and confirm it serves nim/kilo/nous.
2. Add a mapping using `provider_name: minimax` (model `MiniMax-M3`) and confirm
   a request routes without `provider_not_registered`.
3. Add a mapping using `provider_name: xiaomi` against
   `https://token-plan-sgp.xiaomimimo.com/v1/chat/completions` and confirm it
   routes with `XIAOMI_API_KEY` set.

## Performance Considerations

None — pure config/registry change, no runtime hot path affected.

## Migration Notes

None — additive provider entries; existing configs are unaffected.

## References

- Frame brief: `context/changes/swe-starter-models/frame.md`
- Provider source of truth: `providers.yaml`
- Generated tables: `config/providers_gen.go`, `proxy/adapters_gen.go`
- Guard logic: `config/config.go:182-203`, `config/config.go:212-239`
- No-match path: `proxy/proxy.go:230-239`
- Adapter URL use: `proxy/openai_compat.go:142`

## Progress

### Phase 1: Register minimax + xiaomi providers

#### Automated

- [x] 1.1 Add minimax + xiaomi entries to `providers.yaml` — fefc9de
- [x] 1.2 Run `go generate ./...` and confirm regenerated tables — fefc9de
- [x] 1.3 Update `TestProviderDefaults` expected slice (config_test.go:588) — fefc9de
- [x] 1.4 `go test ./config/...` and `go build ./...` pass — fefc9de

#### Manual

- [x] 1.5 Config referencing minimax/xiaomi loads without `provider_not_registered` — fefc9de

### Phase 2: Confirm starter scope + guard behavior

#### Automated

- [x] 2.1 `go test ./cmd/freedius/...` and `mage test` pass

#### Manual

- [ ] 2.2 Zero-mapping config returns clean `no_match` (no 500)

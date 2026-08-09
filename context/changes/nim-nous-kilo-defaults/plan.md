# Modernize Default Config: NIM Tiers + Nous + Kilo Implementation Plan

## Overview

The default freedius config (`cmd/freedius/templates/starter.yaml`) is currently
NIM-only with a single hard-coded fallback on the `opus` tier. This plan
modernizes it so a fresh install works out of the box with one NVIDIA key, while
adding resilience: each tier gets a **per-tier NIM fallback chain** (best NIM
model → cheaper NIM models), and **Nous Research** + **Kilo** are added as
openai providers that appear as the **last-resort** fallback entries in every
chain.

## Current State Analysis

- Default config = the embedded starter at `cmd/freedius/templates/starter.yaml`
  (loaded via `//go:embed` in `cmd/freedius/main.go:48`). It is NIM-only:
  `nim` provider with `sanitizeNIMBody` + `no_stream_usage`, and 5 mappings
  (`opus`, `sonnet`, `haiku`, `default`, `auto`) all pointing at NIM. Only
  `opus` has a fallback (`deepseek-ai/deepseek-v4-pro`).
- Provider metadata lives in `providers.yaml` → generated into
  `providers_gen.go` via `go generate ./...`. There is **no** `nous` or `kilo`
  entry today, so any mapping referencing them would fail validation
  (`config/config.go:332` "references unknown provider").
- `warnMissingEnvVars` (`cmd/freedius/main.go:413`) only logs — missing
  `NOUS_API_KEY` / `KILO_API_KEY` will not block startup, and a fallback to a
  key-less provider only fails at request time if NIM itself fails.
- `TestStarterTemplate_ValidConfig` (`cmd/freedius/main_test.go:501`) is the
  regression guard: the new starter must parse and define ≥1 provider/mapping.
- Fallback entries are full `{provider_name, model_string}` structs
  (`config/config.go:64`), so cross-provider chains (NIM → Nous → Kilo) work
  with the existing dispatcher — no code change needed in the proxy.

### Key Discoveries:

- `providers.yaml` fields: `behavior`, `default_base_url`, `default_api_key_env`,
  `require_base_url`, and `openai: {no_stream_usage, pre_send_hook}`
  (`providers.yaml:7-21`).
- NIM's `pre_send_hook: sanitizeNIMBody` is per-provider (`providers.yaml:31`),
  so Nous/Kilo requests are **not** sanitized — correct, they speak plain
  OpenAI.
- Adding `nous`/`kilo` to `providers.yaml` and regenerating is the only way to
  make them referenceable in mappings without hitting the "unknown provider"
  validation error.

## What We're NOT Doing

- Not changing the proxy dispatch, fallback timeout logic, or `sanitizeNIMBody`.
- Not making Nous/Kilo the **primary** target of any tier (NIM stays primary so
  a single `NVIDIA_NIM_API_KEY` is still sufficient for normal operation).
- Not adding a TUI/UI for editing fallback chains — this is a config-only change.
- Not wiring `anthropic`/`mix` behaviors — both new providers are openai-class.
- Not changing how missing keys are handled (still warn-at-boot, fail-at-request).

## Implementation Approach

1. Declare `nous` and `kilo` in `providers.yaml` as `behavior: openai` with their
   default base URLs and key envs; regenerate `providers_gen.go`.
2. Rewrite `starter.yaml` so every tier is NIM-first with a descending NIM
   fallback, then `nous`, then `kilo` as the terminal fallback.
3. Keep the starter single-key friendly: primary targets are all NIM, so only
   `NVIDIA_NIM_API_KEY` is required for the happy path.

## Critical Implementation Details

- **NIM model IDs must be refreshed against the live catalog.** The existing
  starter uses `nvidia/nemotron-3-ultra-550b-a55b`, `deepseek-ai/deepseek-v4-pro`,
  `deepseek-ai/deepseek-v4-flash`. Confirm these still exist (or pick current
  equivalents) at build.nvidia.com before writing `model_string` values. Use
  three classes: flagship reasoning (`opus`), strong mid (`sonnet`),
  fast/cheap (`haiku`/`default`/`auto`). The implementer — not the plan — chooses
  the exact IDs, but they MUST be valid catalog IDs or the starter will route to
  a non-existent model.
- **Kilo is now pinned** (from the operator's opencode config): base URL
  `https://api.kilo.ai/api/gateway/chat/completions`, key env `KILO_API_KEY`,
  fallback model `kilo-auto/free` (its auto-routing free model). The
  openai-compatible gateway follows the same `/chat/completions` suffix
  convention as the other openai providers in `providers.yaml`.
- **Nous base URL** is `https://api.nousresearch.com/v1/chat/completions` with
  `NOUS_API_KEY` (confirmed by user). The Nous fallback model is **`hy3:free`**
  (user-specified, sonnet-equivalent) — use it for the `nous` entry in every
  tier's fallback chain.
- **Ordering matters**: `nous` must come before `kilo` in every fallback chain
  (Kilo is the final last-resort), and NIM models must descend in capability
  within each tier's chain.

## Phase 1: Add Nous + Kilo Provider Metadata

### Overview

Register the two new providers so they can be referenced by name in mappings,
then regenerate the generated metadata table.

### Changes Required:

#### 1. Provider declarations

**File**: `providers.yaml`

**Intent**: Add `nous` and `kilo` as `behavior: openai` providers with their
default base URLs, key envs, and `require_base_url: false` so the starter can
reference them without the user overriding the base URL. Mirror the existing
`google`/`mistral` entries for shape.

**Contract**: Append two entries following the established schema
(`providers.yaml:23-117`). Suggested shape:

```yaml
  nous:
    behavior: openai
    default_base_url: https://api.nousresearch.com/v1/chat/completions
    default_api_key_env: NOUS_API_KEY
    require_base_url: false
    openai:
      no_stream_usage: true

  kilo:
    behavior: openai
    default_base_url: https://api.kilo.ai/api/gateway/chat/completions
    default_api_key_env: KILO_API_KEY
    require_base_url: false
    openai:
      no_stream_usage: true
```

#### 2. Regenerate metadata

**File**: `providers_gen.go` (generated — do not edit by hand)

**Intent**: Run code generation so `nous` and `kilo` appear in `providerDefaults`
with `RequireBaseURL: false` and `SupportsCountTokens: false`, matching every
other openai provider.

**Contract**: `go generate ./...` regenerates `providers_gen.go`. Verify the
generated `var providerDefaults` now contains `nous` and `kilo` entries.

### Success Criteria:

#### Automated Verification:

- `go generate ./...` runs cleanly and `git diff` shows `nous`/`kilo` added to `providers_gen.go`
- `go build ./...` succeeds
- `mage lint` (vet + staticcheck + golangci-lint) passes

#### Manual Verification:

- `providers_gen.go` contains both `nous` and `kilo` with `RequireBaseURL: false`

**Implementation Note**: After completing this phase and all automated verification passes, pause here for manual confirmation from the human that the manual testing was successful before proceeding to the next phase. Phase blocks use plain bullets — the corresponding `- [ ]` checkboxes for these items live in the `## Progress` section at the bottom of the plan.

---

## Phase 2: Restructure Starter Mappings with Per-Tier Fallback

### Overview

Rewrite `cmd/freedius/templates/starter.yaml` so each tier is NIM-first, falls
back through cheaper NIM models within the tier, then through `nous`, then `kilo`
as the terminal last-resort.

### Changes Required:

#### 1. Provider block

**File**: `cmd/freedius/templates/starter.yaml`

**Intent**: Declare `nim`, `nous`, and `kilo` in the starter's `providers:` block
so all three are available to mappings. Keep `nim` as-is.

**Contract**: `providers:` block lists `nim` (existing), `nous`
(`behavior: openai, default_api_key_env: NOUS_API_KEY`), and `kilo`
(`behavior: openai, default_api_key_env: KILO_API_KEY`).

#### 2. Mappings with fallback chains

**File**: `cmd/freedius/templates/starter.yaml`

**Intent**: For each tier (`opus`, `sonnet`, `haiku`, `default`, `auto`), set the
NIM primary model, an ordered NIM fallback list (descending capability), then a
`nous` entry, then a `kilo` entry as the final fallback. Keep NIM as the primary
so a single `NVIDIA_NIM_API_KEY` still drives normal traffic.

**Contract**: Each mapping uses the schema
(`config/config.go:64`):

```yaml
mappings:
  opus:
    provider_name: nim
    model_string: <CONFIRM: current NIM flagship reasoning model>
    fallback:
      - provider_name: nim
        model_string: <CONFIRM: current NIM strong-mid model>
      - provider_name: nim
        model_string: <CONFIRM: current NIM fast model>
      - provider_name: nous
        model_string: hy3:free
      - provider_name: kilo
        model_string: kilo-auto/free

  sonnet:
    provider_name: nim
    model_string: <CONFIRM: current NIM strong-mid model>
    fallback:
      - provider_name: nim
        model_string: <CONFIRM: current NIM fast model>
      - provider_name: nous
        model_string: hy3:free
      - provider_name: kilo
        model_string: kilo-auto/free

  haiku:
    provider_name: nim
    model_string: <CONFIRM: current NIM fast model>
    fallback:
      - provider_name: nous
        model_string: hy3:free
      - provider_name: kilo
        model_string: kilo-auto/free

  default:
    provider_name: nim
    model_string: <CONFIRM: current NIM fast model>
    fallback:
      - provider_name: nous
        model_string: hy3:free
      - provider_name: kilo
        model_string: kilo-auto/free

  auto:
    provider_name: nim
    model_string: <CONFIRM: current NIM fast model>
    fallback:
      - provider_name: nous
        model_string: hy3:free
      - provider_name: kilo
        model_string: kilo-auto/free
```

The `<CONFIRM: ...>` placeholders are resolved by the implementer against the
live NIM catalog and the confirmed Nous/Kilo specs (see Critical Implementation
Details). `opus` has the longest chain (flagship → mid → fast → nous → kilo);
`sonnet`/`haiku`/`default`/`auto` have shorter, sensible chains.

### Success Criteria:

#### Automated Verification:

- `mage test` passes, including `TestStarterTemplate_ValidConfig` (main_test.go:501)
- `go run ./cmd/freedius --help` (or `mage build` + a dry config load) confirms the starter parses
- `mage lint` passes

#### Manual Verification:

- Loading the starter with only `NVIDIA_NIM_API_KEY` set works for all tiers
- With NIM key unset (or forced failure), requests fall back to `nous`, then `kilo`
- No startup failure due to missing `NOUS_API_KEY`/`KILO_API_KEY` (only warnings)

**Implementation Note**: After completing this phase and all automated verification passes, pause here for manual confirmation from the human that the manual testing was successful before proceeding to the next phase. Phase blocks use plain bullets — the corresponding `- [ ]` checkboxes for these items live in the `## Progress` section at the bottom of the plan.

---

## Phase 3: Verification & Docs

### Overview

Run the full quality gate and update the starter's header comment to reflect the
new default behavior and required keys.

### Changes Required:

#### 1. Starter header comment

**File**: `cmd/freedius/templates/starter.yaml`

**Intent**: Update the top comment to explain NIM-first with per-tier fallback
and that Nous/Kilo are opt-in last-resort fallbacks (only need keys if NIM is
down), replacing the old "NIM-only" narrative.

**Contract**: Comment at the top of `starter.yaml` (currently lines 1-21) — no
schema change, documentation only.

#### 2. Full quality gate

**File**: repo-wide

**Intent**: Run the complete CI-equivalent gate to confirm no regressions from
the config + generated-code change.

**Contract**: `mage ci` (or `mage test` + `mage lint` + `mage govulncheck`).

### Success Criteria:

#### Automated Verification:

- `mage ci` passes (test + lint + govulncheck)
- `go generate ./...` is a no-op (generated code already in sync — `git diff` empty after regen)

#### Manual Verification:

- README "Configuration" / Quickstart still accurate (note optional Nous/Kilo keys)
- Fresh `freedius` boot with one NVIDIA key shows only benign missing-key warnings

## Testing Strategy

### Unit Tests:

- `TestStarterTemplate_ValidConfig` — starter parses, ≥1 provider/mapping.
- `TestConfig_MappingFallback_*` (config_test.go:926+) — unchanged, must still pass (fallback ordering/duplicate/unknown-provider rules).
- Add a focused test asserting the starter's fallback chains end in `kilo` and
  contain `nous` before `kilo` (guards the ordering contract from Critical
  Implementation Details).

### Integration Tests:

- (Optional) A request-time test forcing NIM failure and asserting the
  dispatcher falls back to `nous`, then `kilo` — only if existing e2e harness
  makes this cheap; otherwise manual.

### Manual Testing Steps:

1. Boot `freedius` with only `NVIDIA_NIM_API_KEY` → all tiers serve from NIM.
2. Unset `NVIDIA_NIM_API_KEY` (or block NIM) → requests fall back to `nous`, then `kilo`.
3. Confirm startup logs only warnings for missing `NOUS_API_KEY`/`KILO_API_KEY`, no abort.

## Performance Considerations

- Fallback chains add at most 2 extra upstream attempts per failing request
  (Nous, then Kilo). The existing `FREEDIUS_FALLBACK_TIMEOUT_MULTIPLIER`
  (main.go:399) already bounds fallback latency; no change needed.

## Migration Notes

- This changes only the **embedded default** starter. Existing user configs on
  disk are untouched (freedius only loads the starter when no config file is
  present). No migration of user data required.

## References

- Starter template: `cmd/freedius/templates/starter.yaml`
- Provider metadata source: `providers.yaml`, generated `config/providers_gen.go`
- Fallback schema: `config/config.go:56-69`
- Env-var warning (non-fatal): `cmd/freedius/main.go:408-424`
- Starter validity test: `cmd/freedius/main_test.go:501`

## Progress

### Phase 1: Add Nous + Kilo Provider Metadata

#### Automated

- [ ] 1.1 `go generate ./...` clean; `providers_gen.go` adds `nous` + `kilo`
- [ ] 1.2 `go build ./...` succeeds
- [ ] 1.3 `mage lint` passes

#### Manual

- [ ] 1.4 `providers_gen.go` contains `nous` and `kilo` with `RequireBaseURL: false`

### Phase 2: Restructure Starter Mappings with Per-Tier Fallback

#### Automated

- [ ] 2.1 `mage test` passes (incl. `TestStarterTemplate_ValidConfig`)
- [ ] 2.2 Starter parses via dry config load
- [ ] 2.3 `mage lint` passes

#### Manual

- [ ] 2.4 Single `NVIDIA_NIM_API_KEY` serves all tiers
- [ ] 2.5 NIM failure falls back to `nous`, then `kilo`
- [ ] 2.6 No startup abort on missing `NOUS_API_KEY`/`KILO_API_KEY`

### Phase 3: Verification & Docs

#### Automated

- [ ] 3.1 `mage ci` passes
- [ ] 3.2 `go generate ./...` is a no-op after regen

#### Manual

- [ ] 3.3 README Quickstart still accurate
- [ ] 3.4 Fresh boot shows only benign missing-key warnings

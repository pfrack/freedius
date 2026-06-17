# Custom → Mix + Protocol Field Implementation Plan

## Overview

Replace the `custom` provider's hardcoded Anthropic-only routing with the `mix` adapter's auto-detection logic, and add an optional `protocol` config field so users can explicitly declare the wire protocol for ambiguous URLs. Users keep writing `provider: custom` — the internal rewrite changes from `custom → anthropic` to `custom → mix`.

## Current State Analysis

- `CustomAdapter` (`proxy/custom.go`) is 11 lines — a struct wrapping `AnthropicCompatibleAdapter`
- `applyEntryDefaults` in `config/defaults.go` rewrites `custom → anthropic`
- `zen` and `go` already rewrite to `mix` via the same function — proven pattern
- `MixAdapter.Handle` sniffs `base_url` path: `/v1/messages` → anthropic, else → openai
- `config.Model` has no `Protocol` field
- Registry in `main.go` registers `"custom"` as a separate adapter entry
- Tests in `config_test.go` and `original_provider_test.go` assert `custom → anthropic`

### Key Discoveries:

- `proxy/custom.go:17` — `CustomAdapter.Handle` just calls `a.inner.Handle`
- `config/defaults.go:47` — `if m.Provider == "custom" { m.Provider = "anthropic" }`
- `config/defaults.go:56-57` — `zen`/`go` rewrite: `if m.Provider == "zen" || m.Provider == "go" { m.Provider = "mix" }`
- `proxy/mix.go:34` — URL sniffing: `strings.HasSuffix(parsedURL.Path, "/v1/messages")`
- `main.go:169` — registry entry: `"custom": proxy.NewCustomAdapter(logger, verboseErrors)`
- `config/config.go:103` — validation requires `base_url` for `openai`/`anthropic`/`mix`

## Desired End State

- `provider: custom` rewrites to `mix` internally
- `MixAdapter` checks `m.Protocol` field first; falls back to URL path sniffing when empty
- `config.Model` has `Protocol string` field, validated as `""`, `"anthropic"`, or `"openai"`
- `CustomAdapter` struct and file are deleted
- All existing tests pass (updated expectations)
- Existing user configs with `provider: custom` + `/v1/messages` URLs work identically

## What We're NOT Doing

- Not removing `"custom"` from `KnownProviders` — it remains a valid alias
- Not making `protocol` field mandatory — URL sniffing is the default
- Not changing behavior for `zen`/`go`/`nim` providers
- Not adding `protocol` validation that restricts which providers can use it

## Implementation Approach

Follow the exact pattern used for `zen`/`go` → `mix` rewrite. Add `Protocol` to the config struct, wire it through validation, update the `MixAdapter` to read it, delete the now-unnecessary `CustomAdapter`.

## Phase 1: Config — Protocol Field + Validation + Rewrite

### Overview

Add `Protocol` field to `config.Model`, validate its value, change `custom` rewrite target from `anthropic` to `mix`.

### Changes Required:

#### 1. Add Protocol field to Model struct

**File**: `config/config.go`

**Intent**: Add `Protocol string` YAML field to the `Model` struct so users can explicitly declare the wire protocol.

**Contract**: `Protocol string \`yaml:"protocol,omitempty"\`` field on `Model`. Validation in `validateModel` rejects values other than `""`, `"anthropic"`, `"openai"`.

#### 2. Change custom rewrite target

**File**: `config/defaults.go`

**Intent**: Change the `custom` rewrite from `→ anthropic` to `→ mix`, aligning it with the `zen`/`go` pattern.

**Contract**: The line `m.Provider = "anthropic"` in the `if m.Provider == "custom"` block becomes `m.Provider = "mix"`. Move the `custom` check to be adjacent to the `zen`/`go` check (after defaults are applied, before the `zen`/`go` rewrite, so `custom` also rewrites to `mix`).

#### 3. Add base_url requirement for custom (post-rewrite)

**File**: `config/config.go`

**Intent**: Since `custom` now rewrites to `mix`, it already falls under the existing `mix` base_url validation. No new code needed — just verify this works.

**Contract**: Existing validation `if (m.Provider == "openai" || m.Provider == "anthropic" || m.Provider == "mix") && m.BaseURL == ""` already covers post-rewrite `custom`.

### Success Criteria:

#### Automated Verification:

- `go build ./...` passes
- `go test ./config/...` passes (after test updates in Phase 4)
- Config with `provider: custom, base_url: https://x/v1/messages` loads and model has `Provider == "mix"`
- Config with `provider: custom, protocol: openai` loads without error
- Config with `provider: custom, protocol: invalid` rejects at validation

#### Manual Verification:

- Existing `freedius.yaml` configs with `provider: custom` still load correctly

---

## Phase 2: MixAdapter — Protocol-Aware Routing

### Overview

Update `MixAdapter.Handle` to check `m.Protocol` before falling back to URL path sniffing.

### Changes Required:

#### 1. Protocol-first routing in MixAdapter

**File**: `proxy/mix.go`

**Intent**: When `m.Protocol` is set, route directly to the correct inner adapter without URL sniffing.

**Contract**: `MixAdapter.Handle` checks `m.Protocol` — if `"anthropic"`, route to `a.anthropic`; if `"openai"`, route to `a.openai`; if empty, fall through to existing URL sniffing logic.

### Success Criteria:

#### Automated Verification:

- `go test ./proxy/...` passes
- New test: `MixAdapter` with `Protocol: "anthropic"` routes to anthropic adapter regardless of URL
- New test: `MixAdapter` with `Protocol: "openai"` routes to openai adapter regardless of URL
- New test: `MixAdapter` with empty `Protocol` uses URL sniffing (existing behavior)

#### Manual Verification:

- `provider: custom` with ambiguous URL + `protocol: anthropic` routes correctly

---

## Phase 3: Delete CustomAdapter + Deregister

### Overview

Remove the `CustomAdapter` code and its registry entry — `custom` now routes through `mix` via config rewrite.

### Changes Required:

#### 1. Delete CustomAdapter files

**File**: `proxy/custom.go`

**Intent**: Remove the file entirely — it's dead code after the rewrite change.

**Contract**: File deleted.

#### 2. Remove custom from registry

**File**: `main.go`

**Intent**: Remove the `"custom": proxy.NewCustomAdapter(...)` entry from the registry map. After config rewrite, no model will ever have `Provider == "custom"` at dispatch time.

**Contract**: Delete the `"custom"` line from the `proxy.NewRegistry(map[string]proxy.Provider{...})` call.

### Success Criteria:

#### Automated Verification:

- `go build ./...` passes (no dangling references to `CustomAdapter`)
- `go test ./...` passes

#### Manual Verification:

- End-to-end: `provider: custom` config entry routes through mix adapter correctly

---

## Phase 4: Update Tests

### Overview

Fix test assertions that expect `custom → anthropic` rewrite, replace with `custom → mix`. Delete `proxy/custom_test.go`.

### Changes Required:

#### 1. Update config test assertions

**File**: `config/config_test.go`

**Intent**: Change assertions that check `custom` rewrites to `anthropic` — they should now assert `mix`.

**Contract**: Tests "valid two models" and "valid custom alias rewrite" change expected `Provider` from `"anthropic"` to `"mix"`. The unknown provider error substring test stays unchanged (KnownProviders still lists `custom`).

#### 2. Update original_provider_test.go

**File**: `config/original_provider_test.go`

**Intent**: Update the `custom` test case to expect `wantProv: "mix"` instead of `wantProv: "anthropic"`. Update `TestLoad_SetsOriginalProviderThroughPipeline` similarly.

**Contract**: `custom rewrites Provider but preserves OriginalProvider` case: `wantProv` changes from `"anthropic"` to `"mix"`. Pipeline test: `opus.Provider` assertion changes from `"anthropic"` to `"mix"`.

#### 3. Delete custom adapter tests

**File**: `proxy/custom_test.go`

**Intent**: Remove the file — `CustomAdapter` no longer exists. The equivalent behavior is tested through mix adapter tests.

**Contract**: File deleted.

#### 4. Add protocol field tests

**File**: `config/config_test.go`

**Intent**: Add test cases for the new `protocol` field: valid values pass, invalid values reject.

**Contract**: New test cases in `TestLoad`: `protocol: openai` passes, `protocol: anthropic` passes, `protocol: grpc` rejects with error substring about invalid protocol.

#### 5. Add protocol routing tests

**File**: `proxy/mix_test.go`

**Intent**: Add tests verifying `MixAdapter` protocol-first routing.

**Contract**: Tests that pass `config.Model{Protocol: "anthropic"}` with an openai-style URL still route to anthropic, and vice versa.

### Success Criteria:

#### Automated Verification:

- `go test ./...` passes with zero failures
- `go vet ./...` clean
- No references to `CustomAdapter` or `proxy/custom.go` remain

#### Manual Verification:

- Review test coverage: all protocol routing paths exercised

---

## Testing Strategy

### Unit Tests:

- Config validation: empty/valid/invalid `protocol` values
- Config rewrite: `custom → mix` with `OriginalProvider` preserved
- MixAdapter routing: explicit protocol vs URL sniffing fallback

### Integration Tests:

- Full Load→Dispatch path with `provider: custom, protocol: openai`
- Full Load→Dispatch path with `provider: custom` (no protocol, URL sniffing)

### Manual Testing Steps:

1. Use existing `freedius.yaml` with `provider: custom` entry — verify it works
2. Add `protocol: anthropic` to a custom entry with ambiguous URL — verify routing
3. Try `protocol: invalid` — verify rejection at startup

## References

- Existing pattern: `config/defaults.go:56-57` — `zen`/`go` → `mix` rewrite
- Mix routing logic: `proxy/mix.go:34` — URL path sniffing
- Roadmap item: S-06 in `context/foundation/roadmap.md`

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles. See `references/progress-format.md`.

### Phase 1: Config — Protocol Field + Validation + Rewrite

#### Automated

- [x] 1.1 `go build ./...` passes — e771bb3
- [x] 1.2 `go test ./config/...` passes — e771bb3

#### Manual

- [ ] 1.3 Existing `freedius.yaml` with `provider: custom` loads correctly

### Phase 2: MixAdapter — Protocol-Aware Routing

#### Automated

- [x] 2.1 `go test ./proxy/...` passes — e771bb3
- [x] 2.2 Protocol-first routing tests added and passing — e771bb3

#### Manual

- [ ] 2.3 Custom + ambiguous URL + explicit protocol routes correctly

### Phase 3: Delete CustomAdapter + Deregister

#### Automated

- [x] 3.1 `go build ./...` passes (no dangling references) — e771bb3
- [x] 3.2 `go test ./...` passes — e771bb3

#### Manual

- [ ] 3.3 End-to-end custom entry routes through mix

### Phase 4: Update Tests

#### Automated

- [x] 4.1 `go test ./...` passes with zero failures — e771bb3
- [x] 4.2 `go vet ./...` clean — e771bb3
- [x] 4.3 No references to CustomAdapter remain — e771bb3

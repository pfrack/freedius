# Zen + Go Adapters Implementation Plan

## Overview

Add a `MixAdapter` — a multi-format URL-routing adapter that inspects `base_url` path suffix to delegate to either `AnthropicCompatibleAdapter` (for `/v1/messages`) or `OpenAICompatibleAdapter` (for everything else). Register `mix` as a new compat provider. Rewrite `zen` and `go` to `mix` in `applyDefaults`, mirroring the `custom` → `anthropic` pattern. Add config validation requiring `base_url` for `zen`/`go`/`mix`.

## Current State Analysis

- S-02 (provider-and-mapping) is complete and archived. The codebase has:
  - `OpenAICompatibleAdapter` and `AnthropicCompatibleAdapter` as the two compat adapters
  - `NIMAdapter` delegates to `OpenAICompatibleAdapter`; `CustomAdapter` delegates to `AnthropicCompatibleAdapter`
  - `applyDefaults()` rewrites `custom` → `anthropic` and fills in-binary defaults for known providers
  - `KnownProviders = {nim, zen, go, custom, openai, anthropic}` — 6 names
  - Family-aware mapping with `mappings:` block and `extractFamily`
  - Config validation requires `base_url` for `openai`/`anthropic` compat providers
- `zen` and `go` entries in `knownProviderDefaults` only set `APIKeyEnv: "OPENCODE_API_KEY"` (no `BaseURL`)
- The registry in `main.go` registers 4 adapters: `nim`, `custom`, `openai`, `anthropic`
- `zen` and `go` are NOT registered in the registry — any request to a zen/go model currently hits "provider not registered"

### Key Discoveries:

- `proxy/nim.go` — thin wrapper pattern: struct with `inner *OpenAICompatibleAdapter`, delegates `Handle`
- `proxy/custom.go` — same pattern: struct with `inner *AnthropicCompatibleAdapter`
- `config/defaults.go:40-43` — `applyEntryDefaults` rewrites `custom` → `anthropic`; S-03 adds `zen` → `mix` and `go` → `mix`
- `config/config.go:80-83` — validation requires `base_url` for `openai`/`anthropic`; S-03 extends to include `mix`
- `main.go:89-94` — registry construction; S-03 adds `"mix"` entry

## Desired End State

A user configures Opencode Zen or Go models with `provider: zen` (or `go` or `mix`) and a `base_url` pointing at the right endpoint. The adapter auto-detects the wire format from the URL:

```yaml
mappings:
  opus:   { provider: zen, model: deepseek-v4-pro,   base_url: https://opencode.ai/zen/v1/chat/completions }
  sonnet: { provider: go,  model: minimax-m3,        base_url: https://opencode.ai/zen/go/v1/messages }
  haiku:  { provider: mix, model: glm-5.1,           base_url: https://custom-gateway.com/v1/chat/completions, api_key_env: MY_KEY }
```

Verification: `make ci` passes. A request through a `provider: zen` mapping dispatches to the correct compat adapter based on the `base_url` path suffix.

## What We're NOT Doing

- OpenAI Responses format (`/v1/responses`) — out of scope
- Google Generative AI format — out of scope
- Auto-fetching model lists from Zen/Go
- Per-provider error messages (zen/go rewrite to mix; error messages say "mix adapter")
- Hardcoded default `base_url` for zen/go (multi-format gateway has no single default)
- `anthropic-version` header injection (Claude Code already sends it)

## Implementation Approach

Three touches:
1. **Config layer** — add `mix` to `KnownProviders`, rewrite `zen`/`go` → `mix` in `applyDefaults`, require `base_url` for `mix`
2. **Adapter layer** — new `MixAdapter` in `proxy/mix.go` that routes by URL path suffix
3. **Wiring** — register `MixAdapter` in `main.go`'s registry

## Phase 1: Config — mix provider + zen/go rewrite + validation

### Overview

Extend the config layer to recognize `mix` as a provider, rewrite `zen`/`go` to `mix` at load time, and validate that `base_url` is present for all three.

### Changes Required:

#### 1. Add `mix` to KnownProviders

**File**: `config/config.go`

**Intent**: Add `"mix"` to the `KnownProviders` map so config validation accepts it.

**Contract**: `KnownProviders` grows from 6 to 7 entries. The set becomes `{nim, zen, go, custom, openai, anthropic, mix}`.

#### 2. Rewrite `zen`/`go` → `mix` in applyDefaults

**File**: `config/defaults.go`

**Intent**: In `applyEntryDefaults`, after `custom` → `anthropic` rewrite, add `zen` → `mix` and `go` → `mix` rewrites. This means post-`applyDefaults`, no model entry has `Provider == "zen"` or `Provider == "go"` — they're all `"mix"`.

**Contract**: `applyEntryDefaults` maps `{"custom" → "anthropic", "zen" → "mix", "go" → "mix"}`. The `knownProviderDefaults` map entries for `zen`/`go` continue to provide `APIKeyEnv` before the rewrite happens.

#### 3. Require `base_url` for `mix` provider

**File**: `config/config.go`

**Intent**: Extend the existing validation rule that requires `base_url` for `openai`/`anthropic` to also require it for `mix`.

**Contract**: The condition becomes `m.Provider == "openai" || m.Provider == "anthropic" || m.Provider == "mix"`.

#### 4. Update config tests

**File**: `config/config_test.go`

**Intent**: Add test cases for zen/go/mix validation and rewrite behavior.

**Contract**: New table entries in `TestLoad`:
- `"valid zen model"` — `provider: zen` with `base_url` → passes, `Provider` field is `"mix"` after load
- `"valid go model"` — same
- `"valid mix model"` — `provider: mix` with `base_url` → passes
- `"zen without base_url"` → error containing `"provider=mix but no base_url"` (error fires post-rewrite)
- `"go without base_url"` → same
- `"mix without base_url"` → same

Update `TestKnownProviders` to assert 7 entries (add `"mix"`).

### Success Criteria:

#### Automated Verification:

- Tests pass: `make test`
- Vet passes: `make vet`
- `TestKnownProviders` asserts 7 entries
- `TestLoad` new cases pass (zen/go rewrite + base_url requirement)

#### Manual Verification:

- None for this phase

**Implementation Note**: After completing this phase and all automated verification passes, proceed to Phase 2.

---

## Phase 2: MixAdapter + registry wiring

### Overview

Create the `MixAdapter` and register it in `main.go`.

### Changes Required:

#### 1. Create MixAdapter

**File**: `proxy/mix.go` (new)

**Intent**: A multi-format adapter that inspects `m.BaseURL` path suffix. If it ends in `/v1/messages`, delegate to `AnthropicCompatibleAdapter`. Otherwise, delegate to `OpenAICompatibleAdapter`.

**Contract**: 
```go
type MixAdapter struct {
    anthropic *AnthropicCompatibleAdapter
    openai    *OpenAICompatibleAdapter
}
func NewMixAdapter(logger *slog.Logger) *MixAdapter
func (a *MixAdapter) Handle(w http.ResponseWriter, r *http.Request, m config.Model, body []byte) error
```
The routing check: `strings.HasSuffix(parsedURL.Path, "/v1/messages")`.

#### 2. Register in main.go

**File**: `main.go`

**Intent**: Add `"mix": proxy.NewMixAdapter(logger)` to the registry map.

**Contract**: Registry grows from 4 to 5 entries.

#### 3. Add MixAdapter tests

**File**: `proxy/mix_test.go` (new)

**Intent**: Test both code paths (Anthropic passthrough and OpenAI translation) plus error cases.

**Contract**: Table-driven test cases:
- Anthropic passthrough: `base_url` ending `/v1/messages` → request forwarded verbatim, response passed through
- OpenAI translation: `base_url` ending `/v1/chat/completions` → request translated to OpenAI format, response SSE translated back to Anthropic format
- Upstream 401 on Anthropic path → 401 forwarded
- Upstream 401 on OpenAI path → 401 forwarded
- Missing env var → error returned
- Missing base_url → error returned

#### 4. Update config.example.yaml

**File**: `config.example.yaml`

**Intent**: Add zen/go examples showing both Anthropic-format and OpenAI-format base_urls.

**Contract**: Example entries demonstrating the two URL patterns for zen and go.

### Success Criteria:

#### Automated Verification:

- Full CI passes: `make ci`
- `proxy/mix_test.go` covers both code paths
- Existing tests unchanged and passing (NIM, custom, openai_compat, anthropic_compat)

#### Manual Verification:

- Review that `config.example.yaml` is clear and demonstrates both URL patterns

**Implementation Note**: After completing this phase and all automated verification passes, pause here for manual confirmation from the human that the manual testing was successful before proceeding to the next phase.

---

## Testing Strategy

### Unit Tests:

- `config/config_test.go` — zen/go/mix validation rules, rewrite behavior, KnownProviders count
- `proxy/mix_test.go` — both format routing paths, error cases, upstream error forwarding

### Integration Tests:

- `proxy/proxy_test.go` — existing dispatcher tests continue to pass (zen/go models now resolve through mix adapter)

### Manual Testing Steps:

1. With a real `OPENCODE_API_KEY`, configure a zen Anthropic-format model and verify streaming passthrough
2. Configure a zen OpenAI-format model and verify translation works end-to-end
3. Misconfigure `base_url` (omit it) and verify startup fails with clear error

## Performance Considerations

None — the `MixAdapter` adds one `url.Parse` + one `strings.HasSuffix` call per request. Negligible.

## References

- Research: `context/changes/zen-go-adapters/research.md`
- S-02 archive: `context/archive/provider-and-mapping/`
- NIM adapter pattern: `proxy/nim.go`
- Custom adapter pattern: `proxy/custom.go`
- applyDefaults rewrite: `config/defaults.go:40-43`

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles. See `references/progress-format.md`.

### Phase 1: Config — mix provider + zen/go rewrite + validation

#### Automated

- [x] 1.1 Tests pass: `make test` — ab35abd
- [x] 1.2 Vet passes: `make vet` — ab35abd
- [x] 1.3 `TestKnownProviders` asserts 7 entries — ab35abd
- [x] 1.4 `TestLoad` new cases pass (zen/go rewrite + base_url requirement) — ab35abd

### Phase 2: MixAdapter + registry wiring

#### Automated

- [x] 2.1 Full CI passes: `make ci` — 0ccbf2b
- [x] 2.2 `proxy/mix_test.go` covers both code paths — 0ccbf2b
- [x] 2.3 Existing tests unchanged and passing — 0ccbf2b

#### Manual

- [x] 2.4 Review that `config.example.yaml` is clear and demonstrates both URL patterns — 0ccbf2b

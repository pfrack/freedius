# Config-Driven Adapter Options — Implementation Plan

## Overview

The `nim-nous-kilo-defaults` impl-review (2026-08-09) closed as APPROVED but
flagged a follow-up: `NewDefaultRegistry` (proxy/adapters_gen.go:116) keys the
adapter registry by *behavior* (`nim`/`openai`/`anthropic`/`mix`), so the
generated per-provider wrappers `GoogleAdapter`, `LmstudioAdapter`,
`OllamaAdapter`, `NIMAdapter` are **dead code** — and the `no_stream_usage` /
`pre_send_hook` flags declared in `providers.yaml` are therefore **silently
ignored** for every provider except `nim` (whose behavior key happens to be its
own wrapper).

This plan makes those options **config-driven and resolved per-request** from
`provider.OpenAI`, then deletes the dead wrappers and the codegen path that
emits them. Behavior is preserved exactly because the YAML data already exists
(`providers.yaml:19-21`, `:29-31`, `:66-67`, `:109-110`, `:116-117`) — it just
never reached the request path.

Decisions locked with the user:
- **Config-only**: drop construction-time adapter defaults; behavior is 100%
  driven by `provider.OpenAI`. (Consequence: `mix`'s currently-hardcoded
  `NoStreamUsage: true` must move into `providers.yaml`.)
- **Unknown `pre_send_hook` → error out** with a clear `configError`.
- **Collapse `nim`** into the generic openai registry entry; its
  `NoStreamUsage` + `sanitizeNIMBody` now come from the `nim` config at request
  time.

## Current State Analysis

- `config.Provider` (config/config.go:37) has **no** `OpenAI` field. The
  generated `providerDefaults` (config/providers_gen.go:12) carries only
  behavior/base-url/key/flags — so `openai.no_stream_usage` /
  `openai.pre_send_hook` are **dropped on load**.
- Codegen only consumed those flags via `needsThinWrapper()`
  (internal/genproviders/main.go:62) to emit per-provider wrappers.
- Those wrappers are never registered: `NewDefaultRegistry` registers only the
  four behavior keys (adapters_gen.go:122-127). `OpenAICompatibleAdapter.Handle`
  (proxy/openai_compat.go:90) uses the fixed `a.translateOpts`, so `google`/
  `ollama`/`lmstudio` get `NoStreamUsage: false` regardless of YAML.
- `NIMAdapter` is the only one that works, purely because `"nim"` is a behavior
  key.
- `mix` forces `NoStreamUsage: true` on its inner openai adapter at construction
  (proxy/mix.go:36) — this is the only place the flag is live outside NIM.
- `pre_send_hook` resolution has no runtime mechanism; the only hook
  (`sanitizeNIMBody`) was baked into `NIMAdapter` (adapters_gen.go:72).

### Key Discoveries:

- The bug is **three layers deep**: config struct lacks the field → defaults
  table drops it → generated wrappers that would apply it are unreachable.
- `providers.yaml` already declares `no_stream_usage`/`pre_send_hook` for the
  right providers; the data is correct, the wiring is missing.
- References to the wrappers are contained to `internal/genproviders/main.go`
  and `internal/genproviders/main_test.go` — no production code calls them.
- Going config-only means `mix`'s forced flag (mix.go:36) must be re-expressed
  in `providers.yaml` or production `zen`/`go`/`custom`/`mix` openai paths would
  regress.

## Desired End State

- `config.Provider` carries `OpenAI *OpenAIOptions` (mirroring the YAML), and
  `providerDefaults` embeds it for known providers.
- `OpenAICompatibleAdapter.Handle` builds `translate.Opts` and resolves the
  `preSendHook` **from `provider.OpenAI` on every request**; an unknown
  `pre_send_hook` returns an `invalid_request_error`.
- `proxy/adapters_gen.go` contains **only** `NewDefaultRegistry` wiring the four
  behavior adapters (with `nim` → generic openai ctor); no per-provider wrapper
  types exist.
- `no_stream_usage: true` is honored for google/ollama/lmstudio/nim; user-supplied
  `freedius.yaml` providers with the flag also work for the first time.
- All tests pass; `go generate ./...` output is md5-stable across reruns.

## What We're NOT Doing

- Switching to `go:embed` of `providers.yaml` (runtime YAML load) — out of scope;
  codegen stays the mechanism.
- Keying the registry by provider name (Option B) — rejected; it explodes the
  registry and breaks `mix`.
- Changing `anthropic`/`mix` adapter logic beyond moving `mix`'s forced flag into
  config.
- Adding new providers or new hook functions beyond `sanitizeNIMBody`.

## Implementation Approach

Treat the per-provider YAML options as first-class runtime config. Load them into
`config.Provider`, read them in `OpenAICompatibleAdapter.Handle`, and strip the
codegen that produced the unreachable wrappers. Keep `go generate ./...` as the
single source-of-truth step; it now also propagates the `openai:` block into
`providerDefaults`.

## Critical Implementation Details

- **`mix` regression trap**: under config-only, removing `mix.go:36` means `zen`/
  `go`/`custom`/`mix` lose `NoStreamUsage: true` unless their generated defaults
  carry `openai: {no_stream_usage: true}`. Add those four blocks to
  `providers.yaml` and regenerate, or the mix openai path will start sending
  `stream_options` to upstreams that reject it.
- **Hook registry is a name→fn map**, not a codegen emission. Keep
  `sanitizeNIMBody` (proxy/nim_sanitize.go) as the sole registered hook; the map
  lives next to `OpenAICompatibleAdapter` and is keyed by the YAML string.

## Phase 1: Carry OpenAI options through config generation

### Overview

Extend the config model so `no_stream_usage`/`pre_send_hook` survive load and
reach `providerDefaults`, and re-home `mix`'s forced flag into YAML.

### Changes Required:

#### 1. Add `OpenAIOptions` to the config model

**File**: `config/config.go`

**Intent**: Let a `Provider` carry its openai-behavior tweaks, mirroring the
YAML shape already used by the codegen `Provider` struct
(internal/genproviders/main.go:57-60).

**Contract**: Add to `config.Provider` (after line 48):
```go
// OpenAI holds openai-behavior-specific adapter tweaks. It is populated from
// the YAML openai: block (user config or generated providerDefaults) and read
// per-request by OpenAICompatibleAdapter.Handle.
OpenAI *OpenAIOptions `yaml:"openai,omitempty"`
```
and a new exported type:
```go
// OpenAIOptions are openai-behavior-specific adapter tweaks.
type OpenAIOptions struct {
	NoStreamUsage bool   `yaml:"no_stream_usage,omitempty"`
	PreSendHook   string `yaml:"pre_send_hook,omitempty"`
}
```

#### 2. Propagate `OpenAI` through generation and defaults merge

**File**: `internal/genproviders/main.go`

**Intent**: Include the `openai:` block in the generated `providerDefaults` so it
is embedded in the binary.

**Contract**: Add `OpenAI *OpenAIOptions` to `providerDefaultEntry` (struct at
main.go:106) and populate it in `GenerateConfig` (the loop at main.go:141-151):
`OpenAI: p.OpenAI`. Extend `configTemplate` (main.go:304) to emit the block
conditionally inside each `Provider{...}` literal:
```go
<% if .OpenAI -%>
	OpenAI: &OpenAIOptions{
		NoStreamUsage: <% .OpenAI.NoStreamUsage %>,
<% if .OpenAI.PreSendHook -%>
		PreSendHook:   "<% .OpenAI.PreSendHook %>",
<% end -%>
	},
<% end -%>
```

#### 3. Merge `OpenAI` in `applyDefaults`

**File**: `config/defaults.go`

**Intent**: Inject generated `OpenAI` options when the user's provider omits
them, exactly like `DefaultBaseURL`/`DefaultAPIKeyEnv`.

**Contract**: In `applyDefaults` (defaults.go:30-42), after the
`DefaultAPIKeyEnv` block, add:
```go
if p.OpenAI == nil {
	p.OpenAI = defaults.OpenAI
}
```
inside the branch where the provider already exists (`if p, ok := ...; ok`).

#### 4. Re-home `mix`'s forced `NoStreamUsage` into YAML

**File**: `providers.yaml`

**Intent**: Preserve `mix`'s current behavior once code defaults are dropped.

**Contract**: Add `openai: {no_stream_usage: true}` to the `zen`, `go`,
`custom`, and `mix` provider entries (the four `behavior: mix` providers). Do
**not** add it to `openai`/`anthropic` (no openai sub-path) and leave
`nim`/`google`/`ollama`/`lmstudio` as-is (they already set it).

#### 5. Regenerate and verify config output

**File**: `config/providers_gen.go` (generated)

**Intent**: Produce the embedded defaults including the new `OpenAI` blocks.

**Contract**: Run `go generate ./...`; confirm `nim`, `google`, `ollama`,
`lmstudio`, `zen`, `go`, `custom`, `mix` entries now contain an `OpenAI` field.

### Success Criteria:

#### Automated Verification:

- `go build ./...` succeeds.
- `go test ./config/...` passes (update any `providerDefaults` struct-literal
  comparisons in `config/config_test.go` that now include `OpenAI`).
- `go generate ./...` is md5-stable across two consecutive reruns.
- `config/providers_gen.go` contains `OpenAI: &OpenAIOptions{...}` for the
  providers listed above.

#### Manual Verification:

- Inspecting `config/providers_gen.go` shows `nim` carries
  `NoStreamUsage: true, PreSendHook: "sanitizeNIMBody"` and the four mix
  providers carry `NoStreamUsage: true`.

---

## Phase 2: Make `OpenAICompatibleAdapter.Handle` config-driven

### Overview

Read `provider.OpenAI` on every request to build `translate.Opts` and resolve
the `preSendHook`, replacing the fixed construction-time fields. Remove the
forced flag in `mix.go`.

### Changes Required:

#### 1. Replace fixed fields with per-request resolution

**File**: `proxy/openai_compat.go`

**Intent**: The adapter becomes a pure behavior-class handler; all
provider-specific tweaks come from config at request time.

**Contract**: Remove the `translateOpts` and `preSendHook` fields from
`OpenAICompatibleAdapter` (openai_compat.go:24-25) and from
`NewOpenAICompatibleAdapterWithTimeout` (the struct literal at lines 40-54). In
`Handle`, replace `a.translateOpts` (line 90) and the `a.preSendHook` block
(lines 101-113) with:
```go
opts := translate.Opts{}
var hook func([]byte) ([]byte, error)
if provider.OpenAI != nil {
	opts.NoStreamUsage = provider.OpenAI.NoStreamUsage
	if provider.OpenAI.PreSendHook != "" {
		h, ok := preSendHooks[provider.OpenAI.PreSendHook]
		if !ok {
			return &configError{
				err: fmt.Errorf(
					"%s adapter (openai-compat): unknown pre_send_hook %q",
					mapping.ProviderName, provider.OpenAI.PreSendHook,
				),
				errType: "invalid_request_error",
			}
		}
		hook = h
	}
}
upstreamBody, err := translate.Request(body, mapping.ModelString, opts)
...
if hook != nil {
	upstreamBody, err = hook(upstreamBody)
	... (existing error handling)
}
```

#### 2. Add the `preSendHook` name→fn registry

**File**: `proxy/openai_compat.go`

**Intent**: Resolve the YAML hook string to the actual function without codegen.

**Contract**: Add a package-level map near the top of the file:
```go
// preSendHooks maps provider-declared pre_send_hook names to functions.
var preSendHooks = map[string]func([]byte) ([]byte, error){
	"sanitizeNIMBody": sanitizeNIMBody,
}
```
(`sanitizeNIMBody` is defined in proxy/nim_sanitize.go, same package.)

#### 3. Drop `mix`'s forced flag

**File**: `proxy/mix.go`

**Intent**: `mix` openai behavior is now driven by the provider's `OpenAI`
config (carried by `zen`/`go`/`custom`/`mix` from Phase 1).

**Contract**: Delete `openai.translateOpts = translate.Opts{NoStreamUsage: true}`
(mix.go:36). Update the `NewMixAdapter` doc comment (mix.go:27-29) to note the
openai sub-path honors `provider.OpenAI.NoStreamUsage`. The `anthropic` sub-adapter
is unchanged.

### Success Criteria:

#### Automated Verification:

- `go build ./...` succeeds (no remaining references to removed struct fields).
- `go vet ./proxy/...` clean.
- `go test ./proxy/...` passes after the Phase 4 test updates land.

#### Manual Verification:

- A request to a `google`/`ollama`/`lmstudio` mapping with `stream: true` no
  longer sends `stream_options` to the upstream (verifiable via a local mock or
  `ollama` instance).

---

## Phase 3: Remove the dead generated wrappers

### Overview

Stop codegen from emitting per-provider wrappers; collapse `nim` into the generic
openai registry entry; regenerate.

### Changes Required:

#### 1. Stop emitting per-provider wrappers

**File**: `internal/genproviders/main.go`

**Intent**: The wrappers are unreachable and now redundant (Phase 2 makes the
generic adapter config-driven).

**Contract**: In `GenerateProxy` (main.go:158-188):
- Delete the `for name, p := range spec.Providers { if !p.needsThinWrapper()...}`
  loop (lines 161-175) and the following `sort.Slice` (lines 173-175).
- Remove the `Adapters` field from `proxyTmplData` (main.go:116-119) and the
  `adapterEntry` type (main.go:121-126).
- Delete `needsThinWrapper` (main.go:62-73) and `providerTypeName`
  (main.go:215-223) — they are now unused.
- Change the `nim` registry entry (main.go:181) to
  `NewOpenAICompatibleAdapterWithTimeout(logger, streamTimeout)`.

#### 2. Trim the proxy template

**File**: `internal/genproviders/main.go`

**Intent**: Remove the wrapper type/method block from generated output.

**Contract**: In `proxyTemplate` (main.go:332-394), delete the
`<% range .Adapters -%> ... <% end -%>` block (lines 345-373). Keep the
`NewDefaultRegistry` block and its `range .RegistryEntries`.

#### 3. Regenerate the proxy adapters

**File**: `proxy/adapters_gen.go` (generated)

**Intent**: Produce the cleaned registry with no wrapper types.

**Contract**: Run `go generate ./...`; `proxy/adapters_gen.go` must contain only
`NewDefaultRegistry` wiring `nim`/`openai`/`anthropic`/`mix` to the four generic
ctors — no `GoogleAdapter`/`LmstudioAdapter`/`OllamaAdapter`/`NIMAdapter`.

### Success Criteria:

#### Automated Verification:

- `go build ./...` succeeds.
- `grep -r "GoogleAdapter\|LmstudioAdapter\|OllamaAdapter\|NIMAdapter" proxy/`
  returns only `adapters_gen.go` (regenerated) — no other source references
  them.
- `go generate ./...` is md5-stable across two consecutive reruns.

#### Manual Verification:

- Reading `proxy/adapters_gen.go` shows the four behavior keys and no wrapper
  type declarations.

---

## Phase 4: Update and extend tests

### Overview

Migrate the NIM/mix tests to the config-driven model and add coverage proving
the previously-dead flags now take effect.

### Changes Required:

#### 1. Rewrite `nim_test.go` against the generic adapter + NIM config

**File**: `proxy/nim_test.go`

**Intent**: These tests exercised `NIMAdapter`; they now prove the generic
openai adapter honors NIM's config.

**Contract**: Change `newNIMAdapter` to return
`NewOpenAICompatibleAdapterWithTimeout(logger, 5*time.Minute)` (drop the
`*NIMAdapter` type). In every `a.Handle(...)` call, add
`OpenAI: &config.OpenAIOptions{NoStreamUsage: true, PreSendHook: "sanitizeNIMBody"}`
to the `config.Provider` literal. All existing assertions (no `stream_options`,
`additionalProperties` stripped, 401/429/tool-use/parallel behavior) must still
hold. Keep the test function names.

#### 2. Fix `mix_test.go` openai-path assertion

**File**: `proxy/mix_test.go`

**Intent**: The test relied on `mix.go:36`'s forced flag.

**Contract**: In `TestMixAdapter_OpenAIPathOmitsStreamOptions` (mix_test.go:189),
add `OpenAI: &config.OpenAIOptions{NoStreamUsage: true}` to the provider literal
so the assertion at mix_test.go:212 still passes.

#### 3. Rewrite the codegen tests

**File**: `internal/genproviders/main_test.go`

**Intent**: Assertions referenced `NIMAdapter` emission and wiring.

**Contract**:
- Replace `TestGenerateProxy_EmitsNIMAdapter` (main_test.go:234) with
  `TestGenerateProxy_OmitsPerProviderWrappers` asserting the generated proxy
  source does **not** contain `type GoogleAdapter struct`,
  `type LmstudioAdapter struct`, `type OllamaAdapter struct`,
  `type NIMAdapter struct`.
- Update the registry-entry assertion (main_test.go:269) to expect
  `"nim":NewOpenAICompatibleAdapterWithTimeout(logger,streamTimeout)`.
- Remove the `NIM NoStreamUsage=true not wired` assertion block
  (main_test.go:249-253); instead assert the config output *does* contain
  `OpenAI: &OpenAIOptions{` for `nim` (optionally a second test for the mix
  providers carrying `NoStreamUsage: true`).

#### 4. Add new behavior tests

**File**: `proxy/openai_compat_test.go` (new) — or extend `nim_test.go`

**Intent**: Prove config-only options take effect and unknown hooks error.

**Contract**: Add:
- `TestOpenAIAdapter_ConfigNoStreamUsageOmitsStreamOptions` — table over
  providers `{google, ollama, lmstudio, nim}` with their real `OpenAI` config;
  assert the captured upstream body has no `stream_options` key.
- `TestOpenAIAdapter_ConfigStreamUsageIncludesStreamOptions` — a provider with
  `OpenAI: &config.OpenAIOptions{}` (or nil) asserts `stream_options` IS present
  (the default for openai/groq/deepseek).
- `TestOpenAIAdapter_UnknownPreSendHook_Errors` — provider with
  `OpenAI: &config.OpenAIOptions{PreSendHook: "does-not-exist"}` asserts `Handle`
  returns a `*configError` with `errType: "invalid_request_error"`.

### Success Criteria:

#### Automated Verification:

- `go test ./...` passes (full suite; update `config/config_test.go` struct
  comparisons if they enumerate `providerDefaults` fields).
- `mage lint` reports 0 issues.
- `mage govulncheck` reports no vulnerabilities.
- `go generate ./...` md5-stable.

#### Manual Verification:

- `mage build` produces a single static binary; a `stream:true` request to a
  locally-running `ollama` mapping shows no `stream_options` in the upstream
  request (confirm via ollama logs or a mitmproxy-style mock).
- A provider configured with `openai: {pre_send_hook: bogus}` returns an
  `invalid_request_error` rather than silently proceeding.

## Testing Strategy

### Unit Tests:

- Per-request `NoStreamUsage` resolution for each flag-bearing provider
  (google/ollama/lmstudio/nim) and the default off-state.
- `preSendHook` resolution success (`sanitizeNIMBody`) and failure (unknown
  name → error).
- `mix` openai path still omits `stream_options` via the new YAML config.
- Codegen: no wrapper types emitted; config output carries `OpenAI` blocks.

### Integration Tests:

- Full `go test ./...` after regeneration (the existing 716-test suite must stay
  green — this change touches the request path used by every openai-behavior
  provider).

### Manual Testing Steps:

1. `mage build && ./freedius` with the shipped starter; send a streaming request
   to a `google`/`ollama`/`nim` mapping and confirm `stream_options` handling
   matches the flag.
2. Temporarily set `pre_send_hook: bogus` on a provider and confirm a clear
   `invalid_request_error`.
3. Confirm `go generate ./...` is a no-op on a clean tree (md5-stable).

## Performance Considerations

None material. The per-request change replaces a fixed struct field read with a
nil-check + map lookup — negligible. The `preSendHooks` map is package-level and
immutable after init.

## Migration Notes

No data migration. Existing `providers.yaml` already declares the flags; the
only YAML edit is adding `openai: {no_stream_usage: true}` to the four mix
providers, which is required to preserve current behavior (Phase 1, step 4).

## References

- Impl-review that surfaced this: `context/changes/nim-nous-kilo-defaults/reviews/impl-review.md` (F5 side-effect note + post-triage follow-up).
- Registry keyed by behavior: `proxy/adapters_gen.go:116` (`NewDefaultRegistry`).
- Request-path lookup: `proxy/proxy.go:324` (`d.Registry.Lookup(p.Behavior)`).
- Current silenced flag: `proxy/openai_compat.go:90`.
- Mix forced flag: `proxy/mix.go:36`.

## Progress

### Phase 1: Carry OpenAI options through config generation

#### Automated

- [x] 1.1 `go build ./...` succeeds after config model + generation changes — 76ce3cc
- [x] 1.2 `go test ./config/...` passes (providerDefaults comparisons updated) — 76ce3cc
- [x] 1.3 `go generate ./...` md5-stable across two consecutive reruns — 76ce3cc
- [x] 1.4 `config/providers_gen.go` contains `OpenAI` blocks for nim/google/ollama/lmstudio/zen/go/custom/mix — 76ce3cc

#### Manual

- [ ] 1.5 Inspect `config/providers_gen.go`: nim has NoStreamUsage+PreSendHook; mix providers have NoStreamUsage:true

### Phase 2: Make OpenAICompatibleAdapter.Handle config-driven

#### Automated

- [x] 2.1 `go build ./...` succeeds (removed fields unreferenced) — 5485559
- [x] 2.2 `go vet ./proxy/...` clean — 5485559
- [x] 2.3 `go test ./proxy/...` passes (after Phase 4 test updates) — 5485559

#### Manual

- [ ] 2.4 Streaming request to google/ollama/lmstudio omits `stream_options` upstream

### Phase 3: Remove the dead generated wrappers

#### Automated

- [x] 3.1 `go build ./...` succeeds — 5485559
- [x] 3.2 No source references GoogleAdapter/LmstudioAdapter/OllamaAdapter/NIMAdapter outside regenerated adapters_gen.go — 5485559
- [x] 3.3 `go generate ./...` md5-stable — 5485559

#### Manual

- [ ] 3.4 `proxy/adapters_gen.go` shows only the 4 behavior keys, no wrapper types

### Phase 4: Update and extend tests

#### Automated

- [x] 4.1 `go test ./...` passes (full suite, config_test comparisons updated) — 5485559
- [x] 4.2 `mage lint` 0 issues — 5485559
- [x] 4.3 `mage govulncheck` no vulnerabilities — 5485559
- [x] 4.4 `go generate ./...` md5-stable — 5485559
- [x] 4.5 New tests present: config NoStreamUsage on/off + unknown hook errors + codegen omits wrappers — 5485559

#### Manual

- [ ] 4.6 `mage build` binary: streaming request to local ollama shows no `stream_options`; bogus `pre_send_hook` returns `invalid_request_error`

# Provider Codegen Implementation Plan

## Overview

Replace hand-maintained provider boilerplate with a `go:generate`-based code generator. A single `providers.yaml` declaration becomes the source of truth; running `go generate ./...` emits all mechanical maps, thin adapter wrappers, rewrite rules, validation sets, and registry construction. The three core adapters (`openai_compat.go`, `anthropic_compat.go`, `mix.go`) remain hand-written.

## Current State Analysis

Adding a provider today touches 5+ locations:

1. `config/config.go:30-38` — `KnownProviders` map (7 entries)
2. `config/defaults.go:15-29` — `knownProviderDefaults` map (4 entries: nim, zen, go, anthropic)
3. `config/defaults.go:50-71` — `applyEntryDefaults` rewrite rules (`custom→mix` first, `zen→mix` and `go→mix` after defaults)
4. `config/config.go:154-155` — base_url requirement condition (`openai || anthropic || mix`)
5. `proxy/nim.go` — thin wrapper adapter (NIMAdapter, pure delegation to OpenAICompatibleAdapter)
6. `main.go:210-215` — registry construction with 4 adapter entries
7. `main.go:277-299` — `checkRequiredEnvVars` iterates `cfg.Models`/`cfg.Mappings` directly; uses `OriginalProvider` in error messages

All of this is deterministic boilerplate derivable from provider metadata.

### Key Discoveries:

- `applyEntryDefaults` executes rewrites in two phases: `custom→mix` runs BEFORE the `knownProviderDefaults` lookup (because `custom` has no defaults entry), while `zen→mix` and `go→mix` run AFTER defaults are applied (so they inherit `OPENCODE_API_KEY`). Order matters — see `context/archive/custom-to-mix-protocol/plan.md:68`.
- `checkRequiredEnvVars` no longer uses a hardcoded preset list. It iterates `cfg.Models` and `cfg.Mappings` directly, checking each model's `APIKeyEnv`. Error messages use `OriginalProvider` (when set) with fallback to `Provider`.
- No existing `//go:generate` directives in the codebase — this will be the first.
- Thin wrapper (`nim.go`) is 38 lines with zero logic beyond delegation to `OpenAICompatibleAdapter`. The former `proxy/custom.go` was deleted in S-06; `custom` now rewrites to `mix` and uses `MixAdapter`.
- `TestKnownProviders` asserts exactly 7 entries by name — generated code must produce the same set.
- `proxy/phase2_test.go:120-219` enforces critical error-message format contract: `"%s adapter (%s): %s"` using `originalOr(m)` for the provider name.

## Desired End State

After this plan is complete:

1. `providers.yaml` at repo root is the single source of truth for all provider metadata
2. `go generate ./...` produces all boilerplate files (marked `// Code generated ... DO NOT EDIT.`)
3. Adding a new provider = one YAML entry + `go generate ./...` + `make ci`
4. Providers needing custom logic use `manual: true` to skip adapter generation
5. CI runs `go generate ./...` && `git diff --exit-code` to catch stale generated files
6. All existing tests pass unchanged — behavior is identical

Verification: `make ci` passes. `go generate ./... && git diff --exit-code` shows no changes.

## What We're NOT Doing

- Changing any user-facing behavior or config format
- Touching the core adapters (`openai_compat.go`, `anthropic_compat.go`, `mix.go`)
- Changing the `Provider` interface or `Registry` type
- Adding new providers (that's the payoff, not the scope of this change)
- Generating tests — tests remain hand-written

## Implementation Approach

Three phases:
1. **Spec + generator tool** — create `providers.yaml` and `internal/genproviders/main.go` that reads it and emits Go files via `text/template`
2. **Replace hand-written code with generated equivalents** — delete originals, add `//go:generate` directives, run generator, verify `make ci` still passes
3. **CI golden-file check** — add a `make generate-check` target that ensures generated files are up to date

## Phase 1: Generator tool + providers.yaml

### Overview

Create the `providers.yaml` declaration and the generator program that reads it and produces Go source files for both `config/` and `proxy/` packages.

### Changes Required:

#### 1. Provider declaration file

**File**: `providers.yaml`

**Intent**: Single source of truth for all provider metadata — name, behavior class, defaults, rewrite rules, and flags.

**Contract**: YAML structure with a `providers` key containing a map of provider specs. Each spec has fields: `behavior` (openai|anthropic|mix), `rewrite_to` (optional), `default_base_url` (optional), `default_api_key_env` (optional), `require_base_url` (bool), `manual` (bool, for adapters needing hand-written logic). Must reproduce the current 7 providers exactly.

#### 2. Generator program

**File**: `internal/genproviders/main.go`

**Intent**: A `go run`-able program that reads `providers.yaml` and emits generated Go files into target package directories.

**Contract**: Accepts flags `-spec <path>` (YAML file) and `-pkg <config|proxy>` (which package to generate for). Emits files with `// Code generated by genproviders from providers.yaml. DO NOT EDIT.` header. Uses `text/template` + `go/format` for output. Exit 1 on any error.

For `config` package, emits one file containing:
- `KnownProviders` map
- `knownProviderDefaults` map (type `modelDefaults` stays hand-written)
- `applyEntryDefaults` function with rewrite rules and default application
- `requireBaseURL` set (used by `validateModel`)
- `PresetProviders()` function returning provider names that have a `default_api_key_env` (for `checkRequiredEnvVars`)

For `proxy` package, emits one file containing:
- Thin adapter structs + constructors + Handle methods for non-manual providers (currently: `nim`)
- `NewDefaultRegistry(logger, overrides map[string]Provider) *Registry` function that wires all adapters

#### 3. Go generate directives

**File**: `config/gen.go` (new)

**Intent**: Wire `go generate` to invoke the generator for the config package.

**Contract**: `//go:generate go run ../internal/genproviders -spec ../providers.yaml -pkg config`

**File**: `proxy/gen.go` (new)

**Intent**: Wire `go generate` to invoke the generator for the proxy package.

**Contract**: `//go:generate go run ../internal/genproviders -spec ../providers.yaml -pkg proxy`

### Success Criteria:

#### Automated Verification:

- Generator builds: `go build ./internal/genproviders`
- Generator runs without error: `go generate ./config/ ./proxy/`
- Generated files compile: `go build ./...`
- Generated `KnownProviders` matches current 7-entry set
- Generated `applyEntryDefaults` produces same rewrite behavior

#### Manual Verification:

- Generated files are readable and clearly marked DO NOT EDIT

**Implementation Note**: After completing this phase and all automated verification passes, proceed to Phase 2.

---

## Phase 2: Replace hand-written code with generated equivalents

### Overview

Delete the hand-written maps, rewrite function, thin wrappers, and registry construction. Replace with generated code + thin call sites that use the generated functions.

### Changes Required:

#### 1. Remove hand-written config boilerplate

**File**: `config/config.go`

**Intent**: Remove `KnownProviders` variable declaration (now generated — same name is shadowed). Replace the hardcoded `base_url` requirement condition with a lookup into the generated `requireBaseURL` set. Add `PresetProviders()` to the config package's exported API (used by `main.go` in Phase 2).

**Contract**: `validateModel` calls `requireBaseURL[m.Provider]` instead of `m.Provider == "openai" || m.Provider == "anthropic" || m.Provider == "mix"`. The `KnownProviders` var is removed from this file (lives in generated file now). The generated file emits `knownProviderDefaults` (same name — clean shadow on delete).

#### 2. Remove hand-written defaults boilerplate

**File**: `config/defaults.go`

**Intent**: Remove `knownProviderDefaults` map (generated version shadows the same name) and `applyEntryDefaults` function (now generated). Keep `modelDefaults` type, `readConfigFile`, `yamlUnmarshalStrict`, and `ProviderEnvVar` (which references the generated map).

**Contract**: `ProviderEnvVar` stays hand-written but references the generated `knownProviderDefaults` map (same name — no rename needed). The `applyDefaults` method on `Config` stays (it just calls `applyEntryDefaults` per entry — that function is now generated).

#### 3. Remove thin wrapper adapter file

**File**: `proxy/nim.go`

**Intent**: Delete this file — its equivalent is now in the generated proxy file.

**Contract**: `NIMAdapter` type with identical signature exists in the generated file instead.

#### 4. Replace registry construction and env-var check in main.go

**File**: `main.go`

**Intent**: Replace the hand-written `proxy.NewRegistry(map[string]Provider{...})` with `proxy.NewDefaultRegistry(logger, nil)`. Update `checkRequiredEnvVars` to use `config.PresetProviders()` as the authoritative lookup for which provider names have preset env vars, replacing the implicit `APIKeyEnv != ""` guard. The per-model iteration contract (only check providers actually in config) is preserved.

**Contract**: `main.go` calls `proxy.NewDefaultRegistry(logger, nil)`. `checkRequiredEnvVars` still iterates `cfg.Models`/`cfg.Mappings` but calls `slices.Contains(config.PresetProviders(), m.OriginalProvider)` to decide which providers need env-var checks, instead of relying on `m.APIKeyEnv != ""`. The `originalProviderName(m)` helper stays. Error messages must still use `OriginalProvider` where set, with fallback to `Provider`.

### Success Criteria:

#### Automated Verification:

- Full CI passes: `make ci`
- All existing tests pass unchanged (config_test.go, proxy_test.go, main_test.go, etc.)
- `go vet ./...` clean
- No compilation errors

#### Manual Verification:

- None — this is a behavior-preserving refactor validated by existing tests

**Implementation Note**: After completing this phase and all automated verification passes, proceed to Phase 3.

---

## Phase 3: CI golden-file check + cleanup

### Overview

Add a Makefile target that ensures generated files stay in sync with `providers.yaml`, and clean up any dead code.

### Changes Required:

#### 1. Add generate-check target

**File**: `Makefile` and `.github/workflows/ci.yml`

**Intent**: Add a `generate-check` target that runs `go generate ./...` and asserts no diff. Wire it into both the local `make ci` target and the GitHub Actions CI workflow so stale generated files are caught on every push.

**Contract**: `generate-check` runs `go generate ./...` then `git diff --exit-code -- '*.go'`. The `ci` target becomes `ci: vet generate-check test build`. CI workflow (`.github/workflows/ci.yml`) gets a new step after `go build` that runs `go generate ./...` then `git diff --exit-code`.

#### 2. Remove dead test assertions if any

**File**: `config/config_test.go`

**Intent**: `TestKnownProviders` should still pass as-is since the generated map has the same entries. If the test imports `KnownProviders` directly, no change needed. Verify and leave alone if passing.

**Contract**: No change expected — existing tests validate the generated output matches the original.

#### 3. Update config.example.yaml comment

**File**: `config.example.yaml`

**Intent**: Add a comment noting that supported providers are defined in `providers.yaml`.

**Contract**: One-line comment at top: `# Supported providers are defined in providers.yaml`

### Success Criteria:

#### Automated Verification:

- `make ci` passes (now includes `generate-check`)
- `make generate-check` passes on clean checkout
- Modifying `providers.yaml` without re-running generate causes `make generate-check` (and CI workflow) to fail

#### Manual Verification:

- Adding a dummy provider entry to `providers.yaml` + running `go generate ./...` produces correct new files

---

## Testing Strategy

### Unit Tests:

- Existing `TestKnownProviders` validates the generated map has correct entries
- Existing `TestLoad` cases validate rewrite and validation behavior
- Existing `TestCheckRequiredEnvVars_*` cases validate env var checking
- Existing `proxy/nim_test.go` validates adapter behavior (it instantiates by type name — generated type has same name)

### Integration Tests:

- `make ci` is the integration gate — all existing tests must pass identically
- `proxy/proxy_test.go` dispatcher tests exercise full request routing through generated registry

### Manual Testing Steps:

1. Add a fake provider to `providers.yaml` with `behavior: openai`, run `go generate ./...`, verify new adapter file appears
2. Remove the fake entry, re-run, verify file disappears
3. Edit `providers.yaml` without running generate, verify `make generate-check` fails

## Performance Considerations

None — code generation happens at development time only. Runtime behavior is identical to current hand-written code.

## References

- Research: `context/changes/provider-codegen/research.md`
- Prior art: `proxy/nim.go` (thin wrapper pattern to replicate)
- Prior art: `config/defaults.go` (rewrite + defaults pattern to replicate)
- S-03 plan: `context/archive/zen-go-adapters/plan.md` (introduced mix adapter)
- S-06 plan: `context/archive/custom-to-mix-protocol/plan.md` (custom→mix rewrite, Protocol field)
- Error format contract: `proxy/phase2_test.go:120-219`

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles. See `references/progress-format.md`.

### Phase 1: Generator tool + providers.yaml

#### Automated

- [x] 1.1 Generator builds: `go build ./internal/genproviders`
- [x] 1.2 Generator runs without error: `go generate ./config/ ./proxy/`
- [x] 1.3 Generated files compile: `go build ./...`
- [x] 1.4 Generated `KnownProviders` matches current 7-entry set
- [x] 1.5 Generated `applyEntryDefaults` produces same rewrite behavior

#### Manual

- [ ] 1.6 Generated files are readable and clearly marked DO NOT EDIT

### Phase 2: Replace hand-written code with generated equivalents

#### Automated

- [ ] 2.1 Full CI passes: `make ci`
- [ ] 2.2 All existing tests pass unchanged
- [ ] 2.3 `go vet ./...` clean

### Phase 3: CI golden-file check + cleanup

#### Automated

- [ ] 3.1 `make ci` passes (includes generate-check)
- [ ] 3.2 `make generate-check` passes on clean checkout
- [ ] 3.3 Modifying providers.yaml without re-running generate causes `make generate-check` (and CI) to fail

#### Manual

- [ ] 3.4 Adding a dummy provider to providers.yaml + running go generate produces correct files

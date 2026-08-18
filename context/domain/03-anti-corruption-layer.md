---
title: "Anti-Corruption Layer — goccy/go-yaml dependency leak"
created: 2026-08-10
type: refactor-plan
---

# Anti-Corruption Layer: goccy/go-yaml dependency leak

> Refactor plan only — no production code modified.

---

## STEP 0 — Discovery

### Source Documents

| Document | Path | Key Insight |
|----------|------|-------------|
| PRD | `context/foundation/prd.md` | FR-009: custom providers "must present an Anthropic-compatible API" — implies the config/persistence format is NOT the contract |
| README | `README.md:95-103` | Config resolution order is a UX concern, not a domain concern |
| Roadmap | `context/foundation/roadmap.md` | "Plugin system" listed as a future direction — swappable components are an intent |
| Domain Distillation | `context/domain/01-domain-distillation.md` | Config classified as **Supporting** subdomain — explicitly NOT core |
| Tech Stack | `context/foundation/tech-stack.md` | "Go's net/http and httputil.ReverseProxy are built into the standard library, making the core routing logic nearly zero-dependency" |

### Stack

Go 1.26.5, standard library (`net/http`, `httputil/ReverseProxy`, `encoding/json`), `goccy/go-yaml` for config serialization, `tiktoken-go` for BPE token counting.

### External Dependencies (from `go.mod`)

| Dependency | Type | Role |
|------------|------|------|
| `github.com/goccy/go-yaml v1.19.2` | direct | YAML marshal/unmarshal of config |
| `github.com/magefile/mage v1.17.2` | direct | Build tooling (magefiles only) |
| `github.com/pkoukk/tiktoken-go v0.1.8` | direct | BPE token counting in translate layer |
| `github.com/pkoukk/tiktoken-go-loader v0.0.2` | direct | Offline BPE dictionary loader |

### Code Layers

| Layer | Directory | Responsibility |
|-------|-----------|---------------|
| Entry point | `cmd/freedius/` | CLI flags, server wiring, env-injection |
| Core routing | `proxy/` | Dispatcher, adapters, middleware, fallback chain |
| Translation | `proxy/translate/` | Anthropic ↔ OpenAI wire-format conversion, token counting |
| Web UI | `proxy/web/` | Embedded dashboard |
| Config / Domain | `config/` | YAML load/validate/persist, provider defaults |
| Codegen | `internal/genproviders/` | `go generate` from `providers.yaml` |

---

## STEP 1 — Leaking Dependencies Identified

### Leak #1: `goccy/go-yaml`

The same YAML serialization library is referenced in **4 production files + 2 test/codegen files**, and — critically — its serialization annotations (`yaml:"..."` struct tags) are **embedded directly in the domain model types**.

| File:Line | How it knows the dependency |
|-----------|---------------------------|
| `config/config.go:16` | `import "github.com/goccy/go-yaml"` |
| `config/config.go:32-34` | `yaml:"providers"`, `yaml:"mappings,omitempty"`, `yaml:"theme,omitempty"` on the `Config` struct (domain aggregate root) |
| `config/config.go:61-75` | 7× `yaml:"..."` tags on `Provider` struct |
| `config/config.go:85-86` | 2× `yaml:"..."` tags on `OpenAIOptions` struct |
| `config/config.go:98-102` | 4× `yaml:"..."` tags on `Mapping` struct |
| `config/config.go:488` | `yaml.Marshal(c)` — direct call to marshal the domain aggregate |
| `config/defaults.go:7` | `import "github.com/goccy/go-yaml"` |
| `config/defaults.go:65` | `yaml.UnmarshalWithOptions(data, cfg, yaml.Strict())` |
| `config/defaults.go:66` | `yaml.FormatError(err, true, false)` |
| `config/config_test.go:10` | `import "github.com/goccy/go-yaml"` |
| `config/config_test.go:684` | `yaml.Unmarshal(data, &parsed)` |
| `internal/genproviders/main.go:32` | `import "github.com/goccy/go-yaml"` |
| `internal/genproviders/main.go:47-53` | 6× `yaml:"..."` tags on a duplicate `Provider` struct (codegen's own copy of the domain type) |
| `internal/genproviders/main.go:256` | `yaml.Unmarshal(data, &spec)` |

**Cross-boundary signal:** The `Provider` and `Mapping` structs in `config/config.go` ARE the domain model (used by `proxy/`, `proxy/web/`, and `cmd/`). Their fields carry `yaml:"..."` struct tags — a serialization-library concern baked into domain types. This means the domain model cannot be compiled, tested, or reasoned about without the YAML library being present.

### Leak #2: `tiktoken-go`

The tokenizer library is confined to one package but its types leak into function signatures throughout that package.

| File:Line | How it knows the dependency |
|-----------|---------------------------|
| `proxy/translate/count.go:13` | `import "github.com/pkoukk/tiktoken-go"` |
| `proxy/translate/count.go:15` | `tiktoken_loader "github.com/pkoukk/tiktoken-go-loader"` |
| `proxy/translate/count.go:36` | `tiktoken.SetBpeLoader(...)` — **global side-effect in `init()`** |
| `proxy/translate/count.go:39-62` | `*tiktoken.Tiktoken` type in `encoderCache` struct + `getEncoder()` signature |
| `proxy/translate/count.go:173` | `countSystem(enc *tiktoken.Tiktoken, ...)` — library type in internal function signature |
| `proxy/translate/count.go:195` | `countMessages(enc *tiktoken.Tiktoken, ...)` |
| `proxy/translate/count.go:203` | `countContent(enc *tiktoken.Tiktoken, ...)` |
| `proxy/translate/count.go:251` | `countTools(enc *tiktoken.Tiktoken, ...)` |

**Cross-boundary signal:** `*tiktoken.Tiktoken` appears in **5 function signatures** within the package. However, the **public** API (`CountInputTokens(body []byte) (int, error)`) returns only `(int, error)` — primitives. The proxy layer calls only the public function and never sees a `tiktoken` type. The leak is contained within the `translate` package.

### Leak #3: `magefile/mage`

Used only in `magefiles/` directory for build tooling. No production code imports it. **Not a leak.**

---

## STEP 2 — Classification & Choice of #1

### Axes Assessment

| Dependency | (a) Layers/files affected | (b) Risk/cost of swapping today | (c) Intent-vs-code mismatch? | Composite |
|------------|--------------------------|--------------------------------|------------------------------|-----------|
| **goccy/go-yaml** | **4** production layers (config, proxy, web, codegen), 6+ files | **LOW** — swap to `gopkg.in/yaml.v3` or `sigs.k8s.io/yaml` is mechanical; BUT struct tags are duplicated across files so the change is error-prone without an ACL | **YES** — domain types carry serialization tags; the distillation classifies Config as Supporting (swappable), but code embeds the format INTO the domain aggregate root | **#1** |
| tiktoken-go | 1 package (translate), but 5 internal function signatures | **HIGH** — no drop-in replacement; BPE dictionaries are tiktoken-specific | **WEAK** — token counting is infrastructure, no document declares it swappable | #2 |
| magefile/mage | 0 production files | N/A — not a leak | N/A | — |

### Choice: goccy/go-yaml

**Why:** The domain model (`Config`, `Provider`, `Mapping`) is annotated with `yaml:"..."` struct tags. These types are the **ubiquitous language** of the application — they flow through every layer. Yet they carry an infrastructure concern (YAML serialization format) baked into their definition. The distillation correctly classifies Config as a **Supporting** subdomain (`01-domain-distillation.md:93`), meaning it SHOULD be swappable without affecting core domain logic. The code fails to honor this: the YAML library is not just *used by* the config layer, it is *referenced in the type definitions* that every other layer imports. This is the textbook "leaking dependency" — a persistence-format concern polluting the domain model.

The second factor is **duplication**: `internal/genproviders/main.go:46-53` defines its own `Provider` struct with its own `yaml:"..."` tags — a separate copy of the domain type that must be kept in sync. An ACL eliminates this duplication by having one domain type and one adapter that knows the wire format.

The third factor is **practical replaceability**: the README (`README.md:95-103`) describes config as "a YAML config file" but the domain model has no inherent dependency on YAML — it's just Go structs with fields. Swapping to TOML, JSON, or a database-backed store should be possible without touching `Provider`, `Mapping`, or `Config` field definitions.

---

## STEP 3 — Diagnosis

### The Duplication

The `Provider` type is defined **twice** with separate `yaml:"..."` tags:

**`config/config.go:60-81`** (the domain model):
```go
type Provider struct {
	Behavior         string `yaml:"behavior"`
	DefaultBaseURL   string `yaml:"default_base_url,omitempty"`
	DefaultAPIKeyEnv string `yaml:"default_api_key_env,omitempty"`
	AnthropicVersion string `yaml:"anthropic_version,omitempty"`
	Protocol         string `yaml:"protocol,omitempty"`
	OpenAI *OpenAIOptions `yaml:"openai,omitempty"`
	RequireBaseURL      bool `yaml:"-"`
	SupportsCountTokens bool `yaml:"-"`
}
```

**`internal/genproviders/main.go:46-53`** (the codegen copy):
```go
type Provider struct {
	Behavior         string         `yaml:"behavior"`
	DefaultBaseURL   string         `yaml:"default_base_url,omitempty"`
	DefaultAPIKeyEnv string         `yaml:"default_api_key_env,omitempty"`
	RequireBaseURL   bool           `yaml:"require_base_url"`
	Manual           bool           `yaml:"manual,omitempty"`
	OpenAI           *OpenAIOptions `yaml:"openai,omitempty"`
}
```

These two types share field names and YAML tags but live in different packages with different field sets. They are already drifting apart (the codegen version has `Manual` and a different `RequireBaseURL` type). This is a **dependency leak enabling type duplication**.

### The Cross-Boundary Leak

The `yaml.Marshal` and `yaml.UnmarshalWithOptions` calls live INSIDE the config package functions that operate on domain types:

**`config/config.go:487-489`** — the domain aggregate knows how to serialize itself:
```go
func (c *Config) Marshal() ([]byte, error) {
	return yaml.Marshal(c)
}
```

**`config/defaults.go:64-67`** — the loader calls the YAML library directly:
```go
func yamlUnmarshalStrict(path string, data []byte, cfg *Config) error {
	if err := yaml.UnmarshalWithOptions(data, cfg, yaml.Strict()); err != nil {
		return fmt.Errorf("config: %s: %s: %w", path, yaml.FormatError(err, true, false), err)
	}
	return nil
}
```

The `Config.Marshal()` method on the domain aggregate root means every caller of `Marshal()` transitively depends on goccy/go-yaml — even though the proxy and web layers never import it directly. The dependency is hidden behind a method call.

### Intent-vs-Code Mismatch

The domain distillation (`01-domain-distillation.md:93`) classifies Config as a **Supporting** subdomain:

> "Config resolution (file → env → starter) — Supporting — Necessary for UX but not the product's differentiator. **Could be replaced without changing the domain.**"

But the code embeds YAML struct tags in the domain types themselves. You cannot "replace config" without touching `Provider`, `Mapping`, and `Config` field definitions — because the YAML serialization format is compiled into the domain model.

---

## STEP 4 — ACL Design

### Design Goal

`goccy/go-yaml` should appear in **exactly one file** (the adapter). Domain types (`Config`, `Provider`, `Mapping`) carry no `yaml:"..."` tags. Marshaling and unmarshaling go through a narrow port (interface).

### Domain Value Objects (Pure — No Serialization Tags)

```go
// config/types.go — pure domain model, no yaml tags

package config

type Provider struct {
	Behavior            string
	DefaultBaseURL      string
	DefaultAPIKeyEnv    string
	AnthropicVersion    string
	Protocol            string
	OpenAI              *OpenAIOptions
	RequireBaseURL      bool
	SupportsCountTokens bool
}

type OpenAIOptions struct {
	NoStreamUsage bool
	PreSendHook   string
}

type Mapping struct {
	ProviderName string
	ModelString  string
	Fallback     []Mapping
	AddedAt      string
}

type Config struct {
	Providers map[string]Provider
	Mappings  map[string]Mapping
	Theme     string
}
```

### Wire DTOs (Adapter-Owned — Carry the Format Tags)

```go
// config/yamlio/dto.go — owned by the adapter layer

package yamlio

type providerDTO struct {
	Behavior            string         `yaml:"behavior"`
	DefaultBaseURL      string         `yaml:"default_base_url,omitempty"`
	DefaultAPIKeyEnv    string         `yaml:"default_api_key_env,omitempty"`
	AnthropicVersion    string         `yaml:"anthropic_version,omitempty"`
	Protocol            string         `yaml:"protocol,omitempty"`
	OpenAI              *openAIOptsDTO `yaml:"openai,omitempty"`
	RequireBaseURL      bool           `yaml:"-"`
	SupportsCountTokens bool           `yaml:"-"`
}

type openAIOptsDTO struct {
	NoStreamUsage bool   `yaml:"no_stream_usage,omitempty"`
	PreSendHook   string `yaml:"pre_send_hook,omitempty"`
}

type mappingDTO struct {
	ProviderName string        `yaml:"provider_name"`
	ModelString  string        `yaml:"model_string"`
	Fallback     []mappingDTO  `yaml:"fallback,omitempty"`
	AddedAt      string        `yaml:"added_at,omitempty"`
}

type configDTO struct {
	Providers map[string]providerDTO `yaml:"providers"`
	Mappings  map[string]mappingDTO  `yaml:"mappings,omitempty"`
	Theme     string                `yaml:"theme,omitempty"`
}
```

### The Port (Domain Interface)

```go
// config/codec.go — narrow port that the domain depends on

package config

// Serializer marshals/unmarshals Config to/from a wire format.
// The domain depends only on this interface, never on the concrete YAML library.
type Serializer interface {
	Marshal(cfg Config) ([]byte, error)
	Unmarshal(data []byte) (Config, error)
}
```

### The Adapter (Only File That Knows goccy/go-yaml)

```go
// config/yamlio/yamlio.go — the ACL adapter

package yamlio

import (
	"github.com/goccy/go-yaml"
	"github.com/pfrack/freedius/config"
)

// YamlAdapter implements config.Serializer using goccy/go-yaml.
type YamlAdapter struct{}

func New() *YamlAdapter { return &YamlAdapter{} }

func (a *YamlAdapter) Marshal(cfg config.Config) ([]byte, error) {
	dto := toDTO(cfg)
	data, err := yaml.Marshal(dto)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (a *YamlAdapter) Unmarshal(data []byte) (config.Config, error) {
	var dto configDTO
	if err := yaml.UnmarshalWithOptions(data, &dto, yaml.Strict()); err != nil {
		return config.Config{}, err
	}
	return fromDTO(dto)
}
```

### Conversion Functions (Between DTO and Domain)

```go
// config/yamlio/convert.go — DTO ↔ domain mapping

package yamlio

import "github.com/pfrack/freedius/config"

func toDTO(cfg config.Config) configDTO {
	dto := configDTO{Theme: cfg.Theme}
	if len(cfg.Providers) > 0 {
		dto.Providers = make(map[string]providerDTO, len(cfg.Providers))
		for name, p := range cfg.Providers {
			dto.Providers[name] = providerDTO{
				Behavior:            p.Behavior,
				DefaultBaseURL:      p.DefaultBaseURL,
				DefaultAPIKeyEnv:    p.DefaultAPIKeyEnv,
				AnthropicVersion:    p.AnthropicVersion,
				Protocol:            p.Protocol,
				RequireBaseURL:      p.RequireBaseURL,
				SupportsCountTokens: p.SupportsCountTokens,
			}
			if p.OpenAI != nil {
				dto.Providers[name].OpenAI = &openAIOptsDTO{
					NoStreamUsage: p.OpenAI.NoStreamUsage,
					PreSendHook:   p.OpenAI.PreSendHook,
				}
			}
		}
	}
	if len(cfg.Mappings) > 0 {
		dto.Mappings = make(map[string]mappingDTO, len(cfg.Mappings))
		for name, m := range cfg.Mappings {
			dto.Mappings[name] = mappingDTO{
				ProviderName: m.ProviderName,
				ModelString:  m.ModelString,
				AddedAt:      m.AddedAt,
				Fallback:     make([]mappingDTO, len(m.Fallback)),
			}
			for i, fb := range m.Fallback {
				dto.Mappings[name].Fallback[i] = mappingDTO{
					ProviderName: fb.ProviderName,
					ModelString:  fb.ModelString,
					AddedAt:      fb.AddedAt,
				}
			}
		}
	}
	return dto
}

func fromDTO(dto configDTO) (config.Config, error) {
	cfg := config.Config{Theme: dto.Theme}
	if len(dto.Providers) > 0 {
		cfg.Providers = make(map[string]config.Provider, len(dto.Providers))
		for name, p := range dto.Providers {
			prov := config.Provider{
				Behavior:            p.Behavior,
				DefaultBaseURL:      p.DefaultBaseURL,
				DefaultAPIKeyEnv:    p.DefaultAPIKeyEnv,
				AnthropicVersion:    p.AnthropicVersion,
				Protocol:            p.Protocol,
				RequireBaseURL:      p.RequireBaseURL,
				SupportsCountTokens: p.SupportsCountTokens,
			}
			if p.OpenAI != nil {
				prov.OpenAI = &config.OpenAIOptions{
					NoStreamUsage: p.OpenAI.NoStreamUsage,
					PreSendHook:   p.OpenAI.PreSendHook,
				}
			}
			cfg.Providers[name] = prov
		}
	}
	if len(dto.Mappings) > 0 {
		cfg.Mappings = make(map[string]config.Mapping, len(dto.Mappings))
		for name, m := range dto.Mappings {
			mapping := config.Mapping{
				ProviderName: m.ProviderName,
				ModelString:  m.ModelString,
				AddedAt:      m.AddedAt,
				Fallback:     make([]config.Mapping, len(m.Fallback)),
			}
			for i, fb := range m.Fallback {
				mapping.Fallback[i] = config.Mapping{
					ProviderName: fb.ProviderName,
					ModelString:  fb.ModelString,
					AddedAt:      fb.AddedAt,
				}
			}
			cfg.Mappings[name] = mapping
		}
	}
	return cfg, nil
}
```

### Refactored Config Methods

```go
// config/config.go — after refactor

type Config struct {
	// ... same fields, no yaml tags
}

// Marshal delegates to the injected serializer.
func (c *Config) Marshal(s Serializer) ([]byte, error) {
	return s.Marshal(*c)
}
```

### Wiring (Composition Root)

```go
// cmd/freedius/main.go — wire the adapter at the composition root

import "github.com/pfrack/freedius/config/yamlio"

func loadConfig(cfgPath, explicitFlag string, s config.Serializer) (*config.Config, error) {
	data, err := readConfigFile(cfgPath)
	if err != nil {
		if os.IsNotExist(err) && explicitFlag == "" {
			return config.LoadFromBytes([]byte(starterTemplate), s)
		}
		return nil, err
	}
	return config.LoadFromBytes(data, s)
}

// In main():
serializer := yamlio.New()
cfg, err := loadConfig(cfgPath, configFlag, serializer)
```

### The genproviders Duplicate — Resolution

The codegen tool (`internal/genproviders/main.go`) currently maintains its own `Provider` with yaml tags. After the refactor, genproviders uses the same yamlio adapter:

```go
// internal/genproviders/main.go — after refactor
// Instead of defining its own Provider struct, it uses config.Provider
// and the yamlio adapter for serialization.
```

**Decision:** The codegen tool's `Provider` type is removed. It uses `config.Provider` from the domain package. The DTO conversion in `yamlio` handles the wire format. This eliminates the duplication permanently.

---

## STEP 5 — Isolation Proof + Before/After

### Isolation Proof

After the refactor, swapping to `sigs.k8s.io/yaml` (or `gopkg.in/yaml.v3`, or a JSON serializer) touches **only one file**:

| Concern | Before | After |
|---------|--------|-------|
| Domain types | Carry `yaml:"..."` tags in 3 structs | **No tags** — pure Go types |
| Marshaling | `yaml.Marshal(c)` inside `Config.Marshal()` | `Serializer.Marshal()` — delegated to adapter |
| Unmarshaling | `yaml.UnmarshalWithOptions(...)` inside `yamlUnmarshalStrict()` | `Serializer.Unmarshal()` — delegated to adapter |
| Tests | `yaml.Unmarshal(data, &parsed)` in `config_test.go` | Test the adapter, or test via `Serializer` interface |
| Codegen | Duplicate `Provider` with its own yaml tags | Uses `config.Provider` + adapter |
| **Files that know goccy/go-yaml** | **6 files** (config.go, defaults.go, config_test.go, genproviders/main.go + 2 DTO copies) | **1 file** (`config/yamlio/yamlio.go`) |

### Before/After for Each Leaking Spot

| Location | Before | After |
|----------|--------|-------|
| `config/config.go:30-40` — Config struct | 3 fields carry `yaml:"..."` tags | **Tags removed** — pure domain type |
| `config/config.go:60-81` — Provider struct | 7 fields carry `yaml:"..."` tags | **Tags removed** — pure domain type |
| `config/config.go:85-86` — OpenAIOptions struct | 2 fields carry `yaml:"..."` tags | **Tags removed** — pure domain type |
| `config/config.go:97-102` — Mapping struct | 4 fields carry `yaml:"..."` tags | **Tags removed** — pure domain type |
| `config/config.go:487-489` — `Config.Marshal()` | `yaml.Marshal(c)` direct call | `s.Marshal(*c)` via `Serializer` interface |
| `config/defaults.go:64-67` — `yamlUnmarshalStrict()` | `yaml.UnmarshalWithOptions(...)` direct call | `s.Unmarshal(data)` via `Serializer` interface |
| `config/config_test.go:684` | `yaml.Unmarshal(data, &parsed)` | Use adapter or mock `Serializer` |
| `internal/genproviders/main.go:46-53` | Duplicate `Provider` struct with yaml tags | **Deleted** — uses `config.Provider` |
| `internal/genproviders/main.go:256` | `yaml.Unmarshal(data, &spec)` | Uses `yamlio.New().Unmarshal(data)` |

### What the UI Layer Receives (Before vs After)

**Before:** The web handlers already receive domain types (`config.Provider`, `config.Mapping`), not raw YAML. But those types carry yaml tags — so importing `config` transitively imports goccy/go-yaml. The web handler compiles only because the YAML library is in the module graph.

**After:** The web handlers receive the same domain types — but the types no longer carry yaml tags. The `config` package no longer imports goccy/go-yaml at all. The dependency is fully severed at the source level. The UI layer's data flow is unchanged (it still gets `config.Provider` with the same fields), but the dependency graph is clean.

### Open Questions Resolved

| Question | Resolution | Where encoded |
|----------|-----------|---------------|
| Should `Config.Marshal()` take a `Serializer` parameter, or should it be a struct field? | **Struct field** — `Config` holds a `Serializer` reference set at construction. This avoids changing every call site of `Marshal()`. | `config/config.go` — `Config` struct gets a `serializer Serializer` field |
| Who owns the DTO types? | The **adapter package** (`config/yamlio/`). The domain package never sees them. | `config/yamlio/dto.go` |
| What about `providerDefaults`? | It's populated from generated code (`providers_gen.go`). The generated code populates `config.Provider` directly (no yaml tags needed). The DTO conversion handles serialization at marshal time. | `config/providers_gen.go` — unchanged, but now populates pure domain types |
| Does this affect `genproviders`? | Yes — it eliminates the duplicate `Provider` type. Genproviders uses `config.Provider` + `yamlio` adapter. | `internal/genproviders/main.go` |

---

## STEP 6 — Verification and Plan

### Success Criterion

After the refactor:
```bash
$ grep -rl "goccy/go-yaml" --include="*.go" .
./config/yamlio/yamlio.go
```

Only ONE file imports goccy/go-yaml: the adapter.

### Files That Know the Dependency Today vs After

| File | Knows goccy/go-yaml today? | Knows it after? |
|------|---------------------------|-----------------|
| `config/config.go` | YES (import + struct tags + Marshal) | **NO** |
| `config/defaults.go` | YES (import + UnmarshalWithOptions) | **NO** |
| `config/config_test.go` | YES (import + Unmarshal) | **NO** (uses adapter or mock) |
| `config/providers_gen.go` | NO (generated, no yaml tags on Go side) | NO (unchanged) |
| `internal/genproviders/main.go` | YES (import + struct tags + Unmarshal) | **NO** (uses adapter) |
| `proxy/count_tokens_local.go` | NO (doesn't import yaml) | NO |
| `proxy/openai_compat.go` | NO | NO |
| `proxy/web/handlers.go` | NO | NO |
| `cmd/freedius/main.go` | NO | NO (wires adapter but doesn't import library) |
| `config/yamlio/yamlio.go` | — (doesn't exist) | **YES** (the adapter) |

### Phased Plan

**Phase 1 — Extract DTOs + adapter (test-first).**
1. Create `config/yamlio/dto.go` with the wire-format structs (yaml-tagged copies).
2. Create `config/yamlio/convert.go` with `toDTO` / `fromDTO` functions.
3. Create `config/yamlio/yamlio.go` implementing `Serializer`.
4. Write table-driven tests for `Marshal`/`Unmarshal` round-trip (pure domain → DTO → bytes → DTO → domain, lossless).

**Phase 2 — Remove yaml tags from domain types.**
1. Strip all `yaml:"..."` tags from `Config`, `Provider`, `OpenAIOptions`, `Mapping` in `config/config.go`.
2. Change `Config.Marshal()` to delegate to a `Serializer`.
3. Replace `yaml.UnmarshalWithOptions(...)` in `yamlUnmarshalStrict` with `Serializer.Unmarshal()`.
4. Run `mage test` — all existing tests must pass.

**Phase 3 — Remove duplicate type from genproviders.**
1. Delete the duplicate `Provider` struct from `internal/genproviders/main.go`.
2. Genproviders uses `config.Provider` + `yamlio.New()` for spec loading.
3. Run `go generate ./...` and CI's "no diff" check.

**Phase 4 — Test cleanup.**
1. Replace direct `yaml.Unmarshal` in `config_test.go` with the adapter or a mock `Serializer`.
2. Add a test that verifies swapping the serializer (e.g., to a JSON-based one) requires NO changes outside `config/yamlio/`.

**Phase 5 — Verify.**
1. `grep -rl "goccy/go-yaml" --include="*.go" .` returns exactly `config/yamlio/yamlio.go`.
2. `mage ci` passes.
3. `mage e2e` passes.

### Consistent with Project Conventions

- Follows existing convention: tests live next to code (`yamlio/yamlio_test.go`).
- Uses `httptest`-style table tests for adapter round-trips (matching `config_test.go` style).
- Conventional commits: `refactor(config): introduce ACL for YAML serialization`.
- No external dependencies added (goccy/go-yaml remains, just isolated).

---

## Summary

The `goccy/go-yaml` library leaks across 4 layers and 6+ files, most critically by embedding serialization struct tags directly in the domain model types (`Config`, `Provider`, `Mapping`). This creates a duplication (genproviders maintains its own `Provider` copy with its own yaml tags) and violates the intent expressed in the domain distillation that Config is a Supporting subdomain that should be swappable. The fix is a classic Anti-Corruption Layer: pure domain types with no serialization tags, a narrow `Serializer` port (interface), and a single adapter (`config/yamlio/yamlio.go`) that owns all YAML knowledge including the wire-format DTOs and the DTO↔domain conversion functions. After the refactor, grepping for `goccy/go-yaml` returns exactly one file — the adapter — and swapping the serialization format (to JSON, TOML, or a different YAML library) touches only that file. The duplicate `Provider` type in genproviders is eliminated by having codegen use the same domain type.

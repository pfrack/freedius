---
date: 2026-06-16T19:10:00+02:00
researcher: opencode
git_commit: 4d88ef1ee705d31c9aa71a31168876816bcb2cac
branch: main
repository: pfrack/freedius
topic: "S-02 provider-and-mapping — family-aware mapping, compat providers, in-binary defaults"
tags: [research, s-02, provider-and-mapping, family-aware-mapping, compat-providers, in-binary-defaults, two-tier-providers, refactor, approved]
status: complete
last_updated: 2026-06-16
last_updated_by: opencode
last_updated_note: "Follow-ups #3 (in-binary defaults) and #4 (family mapping) APPROVED 2026-06-16 19:15. Renamed from provider-model-v2 to provider-and-mapping 2026-06-16 19:20. S-02 (this slice) reorders ahead of S-03 (zen-go-adapters) because S-03 depends on S-02's compat adapter code. All major design decisions resolved; ready for /10x-plan."
---

# Research: S-02 provider-and-mapping

**Date**: 2026-06-16 19:10 CEST (renamed and reordered 2026-06-16 19:20)
**Researcher**: opencode
**Git Commit**: `4d88ef1ee705d31c9aa71a31168876816bcb2cac` (main, post-merge of S-01 PR #2)
**Branch**: `main`
**Repository**: `pfrack/freedius`

## Research Question

What changes to freedius's provider and configuration model would best serve a solo developer who wants to route Claude Code calls to many different model families (opus, sonnet, haiku, auto) without enumerating every Claude Code model version in the config, and who wants to be able to point freedius at any OpenAI- or Anthropic-compatible endpoint without registering a new provider?

## Summary

Three architectural changes are bundled into S-02:

1. **Family-aware mapping** (Follow-up #4 from the S-03 research): replace (or supplement) the current exact-match `models:` map with a `mappings:` block that routes by semantic family name (`opus`/`sonnet`/`haiku`/`auto`/`default`). The router knows the patterns internally — the user doesn't write regex.

2. **Two-tier provider model** (Follow-up #2): `KnownProviders` stays as a closed set of **convenience presets** (`nim`/`zen`/`go`) with default URLs and env-var names; new **compatibility providers** (`openai`/`anthropic`) let the user point at any compatible endpoint without registering a vendor. `custom` becomes an alias for `anthropic`.

3. **In-binary defaults** (Follow-up #3): known providers ship with default `base_url` and `api_key_env` values compiled into the binary. The config loader fills in missing values at load time. This dramatically shortens the example config and reduces the "wall of YAML" problem.

All three are independently useful but share the same code surface (config + dispatch + adapters). Bundling them in one slice keeps the implementation coherent; the S-02 planner may further decompose into S-02a/b/c if needed.

**Resulting config example**:

```yaml
mappings:
  opus:   { provider: go,  model: deepseek-v4-pro }
  sonnet: { provider: go,  model: deepseek-v4-flash }
  haiku:  { provider: nim, model: step-3.5 }
  auto:   { provider: nim, model: step-3.5 }
  default: { provider: nim, model: step-3.5 }
```

Five lines. The same setup with the S-01 design would be 50+ lines (every Claude Code model name enumerated). The user's mental model "opus goes here, sonnet goes there" is now the literal config syntax.

**Source design discussion**: this research is a summary of Follow-ups #2, #3, #4 from `context/changes/zen-go-adapters/research.md` (the S-03 research artifact, created 2026-06-16 in the same conversation). The detailed design, code examples, test cases, and rationale for each piece live there. This document is the S-02 entry point; the S-03 research is the S-02 appendix.

## Detailed Findings

### 1. Family-aware mapping (Follow-up #4 — detailed in S-03 research)

The `mappings:` block in the config declares a semantic family name → model mapping. The router extracts the family from the incoming model name using a built-in pattern table.

**Schema**:
```yaml
mappings:
  <family_name>:
    provider: <string>
    model: <string>
    base_url: <string>     # optional for presets, required for compat
    api_key_env: <string>  # optional for presets, required for compat
```

**Built-in family patterns** (hardcoded in the router, in `proxy` or `config` package):

| Family | Pattern | Matches examples |
|---|---|---|
| `opus` | `(?i)opus` | `claude-opus-4-1`, `claude-opus-4-5`, `claude-opus-4-7`, `claude-3-opus-20240229` |
| `sonnet` | `(?i)sonnet` | `claude-sonnet-4-6`, `claude-3-5-sonnet-latest` |
| `haiku` | `(?i)haiku` | `claude-haiku-3-5`, `claude-haiku-4-5` |
| `auto` | `(?i)auto` | `auto`, `claude-auto`, the default Claude Code model |
| `default` | (no pattern — matches anything unmatched) | catch-all for unmatched model names |

**Lookup order in the dispatcher** (per request):

1. `models:` exact match (S-01 power-user path, unchanged)
2. `mappings:` family match (new S-02 default path)
3. 404 not found

**Key design decisions** (from the S-03 triage):

- **Priority order is fixed by the built-in `knownFamilies` slice**; user YAML order doesn't matter for family matching. More specific families (`opus`) win over more general (`auto`) only if `opus` appears earlier in the `knownFamilies` slice.
- **`default:` is a special family** that catches anything not matched by another family or by exact match. The user opts in by including `default:` in their `mappings:` block.
- **Exact match in `models:` always wins** over family match. This is the documented escape hatch for power users and overrides.

See S-03 research Follow-up #4 for: full design, validation rules, test cases, risks, open questions.

### 2. Two-tier provider model (Follow-up #2 — detailed in S-03 research)

`KnownProviders` (closed set, validated at startup) becomes a list of convenience presets. New compatibility providers add format-explicit agnostic adapters.

**Final `KnownProviders` set**: `{nim, zen, go, custom, openai, anthropic}` — six names, two of which (`custom` and `anthropic`) resolve to the same adapter.

| Provider | Tier | Wire format | URL source | Env var source | Eager check |
|---|---|---|---|---|---|
| `nim` | preset | OpenAI Chat Completions | const default + user override | const `NIM_API_KEY` + user override | yes |
| `zen` | preset | multi-format (Anthropic or OpenAI, from URL) | user-supplied `base_url` | const `OPENCODE_API_KEY` + user override | yes |
| `go` | preset | multi-format (Anthropic or OpenAI, from URL) | user-supplied `base_url` | const `OPENCODE_API_KEY` + user override | yes |
| `openai` | compat | OpenAI Chat Completions | user-supplied `base_url` (required) | user-supplied `api_key_env` (required) | no |
| `anthropic` | compat | Anthropic Messages | user-supplied `base_url` (required) | user-supplied `api_key_env` (required) | no |
| `custom` | compat (alias) | Anthropic Messages | user-supplied `base_url` (required) | user-supplied `api_key_env` (required) | no |

**Adapter factoring**:
- `proxy/openai_compat.go` — `OpenAICompatibleAdapter` (new in S-02)
- `proxy/anthropic_compat.go` — `AnthropicCompatibleAdapter` (new in S-02; supersedes S-01's `CustomAdapter` which stays as alias target)
- `proxy/zen.go` / `proxy/go.go` — thin multi-format routers that delegate to the compat adapters
- S-01's `proxy/nim.go` and `proxy/custom.go` continue to exist; their behavior matches the new compat adapters' internals (no rename needed)

**S-01 implications**: zero. S-03 is the add-on (per triage); S-01 lands unchanged.

See S-03 research Follow-up #2 for: dispatch logic, factory table, alias resolution, single-source principle, full design.

### 3. In-binary defaults (Follow-up #3 — detailed in S-03 research)

A new file `config/defaults.go` defines a `knownProviderDefaults` map. After strict YAML parse (and before validation), the loader fills in missing fields from the defaults table.

**`config/defaults.go`** (new in S-02):

```go
package config

type modelDefaults struct {
    BaseURL   string
    APIKeyEnv string
}

var knownProviderDefaults = map[string]modelDefaults{
    "nim": {
        BaseURL:   "https://integrate.api.nvidia.com/v1/chat/completions",
        APIKeyEnv: "NIM_API_KEY",
    },
    "zen": {
        // No default base_url — multi-format gateway; user must specify per model.
        APIKeyEnv: "OPENCODE_API_KEY",
    },
    "go": {
        APIKeyEnv: "OPENCODE_API_KEY",
    },
    // custom, openai, anthropic: no defaults — user provides everything.
}

func (c *Config) applyDefaults() {
    for name, m := range c.Models {
        d, ok := knownProviderDefaults[m.Provider]
        if !ok {
            continue
        }
        if m.BaseURL == "" {
            m.BaseURL = d.BaseURL
        }
        if m.APIKeyEnv == "" {
            m.APIKeyEnv = d.APIKeyEnv
        }
        c.Models[name] = m
    }
}
```

**In `config.Load`** (modified by S-02):

```go
// After strict YAML parse, before validation
cfg.applyDefaults()
// ... existing validation as before
```

**Eager startup check (Option B, recommended)**: iterate merged models, check each `m.APIKeyEnv`:

```go
for name, m := range cfg.Models {
    if m.APIKeyEnv != "" && os.Getenv(m.APIKeyEnv) == "" {
        return failf("freedius: %s env var required (config model %q references it)", m.APIKeyEnv, name)
    }
}
```

One loop, no preset knowledge, catches per-model overrides. Matches the single-source principle (F-01 review F7).

**Validation update**: `provider: nim` no longer requires `base_url` or `api_key_env` (defaulted); `provider: zen`/`go` no longer require `api_key_env` (still require `base_url`); `provider: openai`/`anthropic`/`custom` require both (no defaults).

**Adapter simplification**: S-01's `NIMAdapter` was going to have a hardcoded `https://integrate.api.nvidia.com/v1/chat/completions` const default. With the merge-at-load approach, the adapter doesn't need a fallback — it just reads `m.BaseURL`. The const goes away.

See S-03 research Follow-up #3 for: full design, test cases, risks, Option A vs B discussion.

## Architecture Insights

1. **The S-01 design was a closed, exact-match system.** The user's pivot to family-aware mapping + agnostic compat providers + in-binary defaults is a more flexible architecture. It's the right design for a solo-dev local tool: short configs, semantic mental models, evolvable as new vendors appear.

2. **All three follow-ups are independent but touch the same code surface.** S-02 can ship all three together, or decompose into S-02a/b/c if implementation reveals that. The natural decomposition:
   - S-02a: compat providers + alias (small, clean)
   - S-02b: in-binary defaults (small, isolated to config)
   - S-02c: family-aware mapping (larger, touches dispatch)

3. **The S-01 power-user path (`models:` exact match) continues to work.** S-02 is purely additive. No breaking change for existing users.

4. **The router code is the load-bearing piece.** `extractFamily` is a 10-line function that defines the user-visible mental model. Get it right and the rest falls into place. Get it wrong (e.g., "auto" matches when user didn't intend it to) and the user has no escape hatch except the exact-match `models:` block.

5. **In-binary defaults are a UX multiplier, not a feature.** The same config schema with and without defaults is dramatically different in user experience. The cost is one small `defaults.go` file.

6. **The compat adapter factoring is DRY but the routing layer is not.** `ZenAdapter.handleOpenAI` and `OpenAICompatibleAdapter.Handle` are functionally identical, but the routing in `ZenAdapter` (URL path → format) is what makes it a Zen adapter. The DRY structure is: shared code in compat adapters, routing in preset adapters. This is the right tradeoff.

7. **The user's intuition about model families is correct.** Claude Code does send `claude-opus-4-1`, `claude-opus-4-5`, `claude-opus-4-7` with predictable patterns. The "family" abstraction is real, not invented. Anthropic's API also returns the model name in every response, so the client knows what it asked for.

## Historical Context (from prior changes)

- `context/foundation/roadmap.md:32-33` — S-03 and S-02 were originally scoped differently; this research drove the re-scoping on 2026-06-16.
- `context/foundation/roadmap.md:79-90` — Original S-02 scope ("Zen + Go adapters") is now S-03 (zen-go-adapters); the S-02 research grew to include provider-model work which is now this S-02 slice.
- `context/changes/zen-go-adapters/research.md` — Source design discussion for all three S-02 follow-ups. Follow-ups #2, #3, #4 contain the full design, code examples, test cases, risks.
- `context/changes/zen-go-adapters/research.md:Follow-up #1` — Unified `OPENCODE_API_KEY` env var. S-02 inherits this; no separate work needed in S-02.
- `context/changes/first-call-routed/plan.md` — S-01 establishes the `Provider` interface, `Registry`, dispatcher. S-02 is fully compatible with S-01 (no S-01 changes).
- `context/changes/first-call-routed/research.md:404` — S-01 research flagged per-model `BaseURL` as the S-01 default; S-02 extends this with family mapping and in-binary defaults.
- `context/changes/proxy-skeleton/reviews/impl-review.md:88-91` — F-01 review F7 established the single-source principle; S-02 follows it (one defaults map, one factory table, one family pattern table).

## Related Research

- `context/changes/zen-go-adapters/research.md` — Full source design discussion
  - Follow-up #1: Unified `OPENCODE_API_KEY`
  - Follow-up #2: Two-tier provider model
  - Follow-up #3: In-binary defaults
  - Follow-up #4: Family-aware mapping
- `context/changes/first-call-routed/research.md` — S-01 research; S-02 inherits the `Provider` seam

## Status

**All major design decisions APPROVED (2026-06-16 19:15 triage):**

- ✅ Two-tier provider model (Follow-up #2 from S-03 research) — `KnownProviders` as presets + `openai`/`anthropic` compat + `custom` alias
- ✅ In-binary defaults (Follow-up #3) — `config/defaults.go` + `applyDefaults()` merge + Option B single-loop eager check
- ✅ Family-aware mapping (Follow-up #4) — `mappings:` block with `opus`/`sonnet`/`haiku`/`auto`/`default`; exact-match `models:` still wins
- ✅ Unified `OPENCODE_API_KEY` (Follow-up #1) — single env var for Zen and Go

**Ready for `/10x-plan provider-and-mapping`.**

## Follow-up Research

(None — this is the initial research artifact for S-02. Detailed designs are in the linked S-03 research.)

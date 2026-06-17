# Provider Codegen — Plan Brief

> Full plan: `context/changes/provider-codegen/plan.md`

## What & Why

Replace hand-maintained provider boilerplate with a `go:generate`-based code generator. Today, adding a provider requires touching 5+ files across config and proxy packages with repetitive, mechanical code. A single `providers.yaml` declaration + `go generate` eliminates this ceremony entirely.

## Starting Point

The codebase has 7 providers spread across hand-written maps (`KnownProviders`, `knownProviderDefaults`), a rewrite function (`applyEntryDefaults`), thin wrapper adapters (`nim.go`, `custom.go`), registry construction in `main.go`, and a hardcoded env-var check list. All are deterministic boilerplate derivable from provider metadata. Three core adapters (`openai_compat.go`, `anthropic_compat.go`, `mix.go`) contain real logic and stay hand-written.

## Desired End State

Adding a new provider is a one-line entry in `providers.yaml` + `go generate ./...`. CI validates generated files are in sync. Providers that need custom logic use `manual: true` to skip adapter generation while still appearing in config maps. All existing behavior is preserved — this is a pure internal refactor.

## Key Decisions Made

| Decision | Choice | Why (1 sentence) |
|---|---|---|
| What gets generated | Config maps + thin wrappers + registry; core adapters stay hand-written | Maximizes elimination while keeping real logic debuggable, with `manual: true` escape hatch for future customization. |
| Generator location | `internal/genproviders/main.go` | Standard Go convention; single binary generates for both packages. |
| Declaration format | `providers.yaml` at repo root | Non-Go contributors can read it; same format as user-facing config; `go-yaml` already in deps. |
| Escape hatch | `manual: true` skips adapter gen, keeps config maps | Clean separation: config metadata always generated, only adapter wiring is opt-out. |
| Migration | Delete-and-replace in one phase | Tests are the safety net; no transitional duplication state. |
| Env var checks | Generated `PresetProviders()` replaces hardcoded list | Eliminates last hardcoded provider list that would go stale. |
| CI validation | Golden-file: `go generate` + `git diff --exit-code` | Standard Go pattern; generated files are committed, reviewable in PRs. |

## Scope

**In scope:**
- `providers.yaml` declaration file
- `internal/genproviders/` generator program
- Generated files for `config/` and `proxy/` packages
- Replacing hand-written boilerplate with generated equivalents
- CI check for stale generated files

**Out of scope:**
- Changing user-facing config format or behavior
- Touching core adapter logic
- Adding new providers
- Generating tests

## Architecture / Approach

```
providers.yaml  ──→  internal/genproviders/main.go  ──→  config/providers_gen.go
                                                    ──→  proxy/adapters_gen.go
```

The generator reads YAML, applies `text/template` + `go/format`, writes to target packages. `//go:generate` directives in `config/gen.go` and `proxy/gen.go` invoke it. Generated files are committed and CI-validated.

## Phases at a Glance

| Phase | What it delivers | Key risk |
|---|---|---|
| 1. Generator tool + providers.yaml | Working generator that produces correct Go files alongside existing code | Template bugs producing non-compiling code |
| 2. Replace hand-written with generated | Delete originals, wire generated code, all tests pass | Subtle behavior difference in rewrite ordering |
| 3. CI golden-file check + cleanup | `make ci` catches stale generated files | None — mechanical |

**Prerequisites:** S-03 (zen-go-adapters) complete — all three behavior classes stable.
**Estimated effort:** ~1-2 sessions across 3 phases.

## Open Risks & Assumptions

- Assumes `applyEntryDefaults` rewrite ordering (rewrites before defaults lookup) is correctly captured in template logic
- Assumes existing tests are sufficient to catch any behavioral drift from generated code

## Success Criteria (Summary)

- `make ci` passes with zero behavior change
- `go generate ./... && git diff --exit-code` shows no changes on clean checkout
- Adding a provider to `providers.yaml` + `go generate` produces working code without touching any other file

# Modernize Default Config: NIM Tiers + Nous + Kilo — Plan Brief

> Full plan: `context/changes/nim-nous-kilo-defaults/plan.md`

## What & Why

The default freedius config (the embedded starter) is NIM-only with a single
hard-coded fallback. We want a fresh install to keep working with one NVIDIA key,
but gain resilience: each model tier gets a **per-tier NIM fallback chain**, and
**Nous Research** + **Kilo** are added as openai providers used as the
**last-resort** fallbacks in every chain.

## Starting Point

`cmd/freedius/templates/starter.yaml` is loaded on first run when no config file
exists. Today it declares only the `nim` provider and 5 mappings
(`opus`/`sonnet`/`haiku`/`default`/`auto`) all pointing at NIM, with `opus` the
only mapping that has a fallback. Provider metadata is generated from
`providers.yaml` into `providers_gen.go`; `nous` and `kilo` do not exist yet, so
any mapping referencing them would fail validation with "references unknown
provider".

## Desired End State

A fresh `freedius` boot with one `NVIDIA_NIM_API_KEY` serves every tier from NIM
as before. If NIM fails for a tier, requests automatically fall back through
cheaper NIM models, then to Nous, then to Kilo — with no startup failure when the
Nous/Kilo keys are absent (only warnings).

## Key Decisions Made

| Decision                       | Choice                                       | Why (1 sentence)                                                              | Source |
| ------------------------------ | -------------------------------------------- | ----------------------------------------------------------------------------- | ------ |
| Fallback shape                 | Per-tier NIM fallback → Nous → Kilo          | Keeps NIM primary (single key) while adding cross-provider resilience.        | Plan   |
| NIM model IDs                  | Refresh to latest catalog at build time      | Catalog IDs change; implementer must verify against build.nvidia.com.         | Plan   |
| Nous integration               | `openai` provider, `api.nousresearch.com/v1` | Matches existing openai-provider pattern; user-confirmed endpoint.            | User   |
| Kilo integration               | `openai` provider, last-resort in mappings  | Real provider (api.kilo.ai/api/gateway), `kilo-auto/free` terminal fallback. | User   |
| Nous/Kilo as primary?          | No — NIM stays primary                       | Preserves the single-key Quickstart experience.                               | Plan   |

## Scope

**In scope:**
- `providers.yaml` + regenerated `providers_gen.go`: add `nous`, `kilo`.
- `starter.yaml`: NIM-first tiers with per-tier NIM fallback, then `nous`, then `kilo`.
- Starter header comment update; full CI gate.

**Out of scope:**
- Proxy dispatch / fallback timeout / `sanitizeNIMBody` changes.
- Making Nous/Kilo primary targets.
- TUI for editing fallback chains.
- `anthropic`/`mix` behavior wiring.

## Architecture / Approach

Providers are declared once in `providers.yaml` (generated metadata) and consumed
by name in `starter.yaml` mappings. Each mapping's `fallback:` list is an ordered
sequence of `{provider_name, model_string}` entries — the existing dispatcher
tries them in order, so NIM→Nous→Kilo works with zero proxy code changes.
Missing keys only warn at boot (`warnMissingEnvVars`, main.go:413), so the last
two providers are true opt-in fallbacks.

## Phases at a Glance

| Phase | What it delivers                              | Key risk                                  |
| ----- | --------------------------------------------- | ----------------------------------------- |
| 1     | `nous` + `kilo` in generated provider metadata | Wrong/placeholder base URL for Kilo      |
| 2     | Restructured starter with per-tier fallbacks  | Stale NIM model IDs → routes to 404 model |
| 3     | Docs + full `mage ci` gate                    | README Quickstart drift                   |

**Prerequisites:** `NVIDIA_NIM_API_KEY` for the happy path; live NIM catalog
access to confirm current model IDs. Nous (`hy3:free`) and Kilo
(`kilo-auto/free` @ `api.kilo.ai/api/gateway`) specs are pinned.
**Estimated effort:** ~1 session across 3 phases (mostly YAML + one regen).

## Open Risks & Assumptions

- **NIM model IDs** are volatile; the plan deliberately leaves exact IDs as a
  build-time confirmation to avoid baking in stale strings. The implementer must
  verify them against build.nvidia.com before writing `model_string` values.
- All other specs (Nous `hy3:free`, Kilo `kilo-auto/free`) are pinned from the
  operator's config.

## Success Criteria (Summary)

- Fresh boot with one NVIDIA key serves all tiers from NIM.
- NIM failure transparently falls back to Nous, then Kilo.
- `mage ci` green; `go generate` in sync; starter parses (regression test passes).

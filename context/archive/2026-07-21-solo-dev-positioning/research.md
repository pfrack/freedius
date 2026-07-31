---
date: 2026-07-22T00:00:00Z
researcher: opencode
git_commit: cf029c56beed8736f7c293381b7d03df0786df1c
branch: auto-review
repository: freedius
topic: "Mapping provenance visualization — current state of breadcrumb-chain cards"
tags: [research, web-ui, provenance, mapping, breadcrumb, env-status]
status: complete
last_updated: 2026-07-22
last_updated_by: opencode
---

# Research: Mapping provenance visualization — current state of breadcrumb-chain cards

**Date**: 2026-07-22T00:00:00Z
**Researcher**: opencode
**Git Commit**: cf029c56beed8736f7c293381b7d03df0786df1c
**Branch**: auto-review
**Repository**: freedius

## Research Question

The frame (`context/changes/solo-dev-positioning/frame.md`) identifies a gap: the
mapping-graph breadcrumb-chain cards show data shape (model → provider → upstream)
but not decision provenance (when/why/env status). This research verifies what is
**already implemented** vs. what is **still missing** in the current codebase,
so the plan can extend rather than re-architect.

## Summary

The breadcrumb-chain visualization shipped by `2026-07-06-mapping-graph-visualization`
**already implements two of the three provenance signals** the frame identifies as
missing:

1. **Added-at timestamp** — `AddedAt` field on `config.Mapping` (config/config.go:68),
   rendered in the card header (mappings-table.html:21) and populated by
   `buildMappingRows` (handlers.go:342).
2. **Env-var presence status** — `EnvPresent` computed in `buildMappingRows`
   (handlers.go:317-324) via `os.Getenv(p.DefaultAPIKeyEnv)`, rendered as a
   `status-dot` with `--present`/`--missing` variants (app.css:929-948).

The frame's Hypothesis #2 ("Provider env lookup lives in `config/providers.go`
(`DefaultAPIKeyEnv`) and is never rendered as a status on the card") is **partially
outdated** — the status IS rendered, but the `checkRequiredEnvVars` function
(main.go:376-393) whose result is discarded at main.go:134 is a separate,
startup-time check that returns an error (not a per-mapping status).

The **truly missing signal** is **fallback rationale** — there is no field or
mechanism to capture *why* a fallback chain exists. The `Fallback` field on
`config.Mapping` (config/config.go:67) is a `[]Mapping` with only
`ProviderName`, `ModelString`, and `AddedAt` — no rationale text.

## Detailed Findings

### 1. Card structure (already implemented)

The breadcrumb-chain card layout was shipped in `2026-07-06-mapping-graph-visualization`
and is currently live in:

- **Template**: `proxy/web/templates/mappings-table.html:1-74` — defines
  `{{define "mappings-table"}}` with a `.mappings-grid` containing `.route-card`
  elements. Each card has a header (`.route-card__header`) with name, meta,
  status-dot, family badge, and depth indicator, plus a `.route-chain` flex
  container with `.route-step` pills.
- **CSS**: `proxy/web/static/app.css:825-1051` — full styling for
  `.mappings-grid`, `.route-card`, `.route-chain`, `.route-step`,
  `.route-step--primary` (green border), `.route-step--fallback` (amber
  border), `.status-dot--present`/`--missing`, `.badge--family`,
  `.route-step--responder` with pulse animation.
- **Types**: `proxy/web/types.go:55-76` — `fallbackEntry` struct (ProviderName,
  Model, Protocol, BaseURL) and `mappingRow` struct (Name, ProviderName, Model,
  Protocol, BaseURL, Responder, HasResponder, Fallbacks, AddedAt, EnvPresent,
  Family).
- **Handler**: `proxy/web/handlers.go:268-349` — `buildMappingRows` constructs
  `[]mappingRow` from `cfg.MappingsSnapshot()` and `cfg.ProvidersSnapshot()`.

### 2. Added-at timestamp (already implemented)

- `config.Mapping.AddedAt` field: `config/config.go:68` — `yaml:"added_at,omitempty"`,
  a free-form string.
- Populated in `buildMappingRows`: `handlers.go:342` — `AddedAt: m.AddedAt`.
- Rendered in template: `mappings-table.html:21` —
  `{{if .AddedAt}}{{.AddedAt}}{{else}}added: unknown{{end}}`.
- Documented in README: `README.md:82-93` — "Mappings accept an optional
  `added_at` free-form string shown on the card."
- The frame suggests sourcing from `git blame` annotations — currently it is
  purely user-supplied via YAML. No git-blame integration exists.

### 3. Env-var presence status (already implemented)

- `config.Provider.DefaultAPIKeyEnv` field: `config/config.go:40` —
  `yaml:"default_api_key_env,omitempty"`.
- Computed in `buildMappingRows`: `handlers.go:317-324`:
  ```go
  envPresent := false
  if p, ok := providers[m.ProviderName]; ok {
      proto = p.Protocol
      url = p.DefaultBaseURL
      if p.DefaultAPIKeyEnv != "" {
          envPresent = os.Getenv(p.DefaultAPIKeyEnv) != ""
      }
  }
  ```
- Rendered in template: `mappings-table.html:22` —
  `<span class="status-dot {{if .EnvPresent}}status-dot--present{{else}}status-dot--missing{{end}}" title="...">`.
- CSS: `app.css:929-948` — green dot for present, hollow amber dot for missing.
- The `checkRequiredEnvVars` function (main.go:376-393) is a **separate** startup-time
  check that returns an error if ANY required env var is missing. Its result is
  discarded at `main.go:134` (`_ = checkRequiredEnvVars(cfg)`). This is NOT the
  same as the per-mapping `EnvPresent` field rendered on cards.

### 4. Fallback rationale (NOT implemented — the actual gap)

- `config.Mapping.Fallback` is `[]Mapping` (config/config.go:67). Each fallback
  entry has `ProviderName`, `ModelString`, `AddedAt`, and `Fallback` (nil for
  fallback entries per the struct comment at config.go:63-69).
- There is **no field** for rationale text — no "why this fallback exists",
  no "credentials rationale", no "fallback reason".
- The card renders fallback steps as pills with provider/model/protocol but no
  explanatory text. See `mappings-table.html:58-69`.
- The fallback dispatch logic in `proxy/proxy.go` (referenced at
  context/changes/solo-dev-positioning/frame.md:117) handles transport failures
  and upstream 4xx/5xx but does not record or surface *why* a particular fallback
  was chosen.

### 5. Family / model binding (partially implemented, weak concern)

- `proxy/families.go:10-16` — `knownFamilies` with regex patterns for opus,
  sonnet, haiku, auto, and a default catch-all.
- `ExtractFamily` (families.go:22-29) returns the first matching family keyword.
- The card shows a family badge: `mappings-table.html:23` —
  `{{if .Family}}<span class="badge badge--family">{{.Family}}</span>{{end}}`.
- Computed in `buildMappingRows`: `handlers.go:325-328` —
  `family, _ := proxy.ExtractFamily(name)` (uses the mapping *key* name, not
  the model string).
- The frame's Hypothesis #3 ("Mapping-key ↔ client-model binding") is **WEAK**
  per the frame itself. The family badge shows which family the mapping key
  belongs to, but does not enumerate which client model strings would resolve
  to this mapping (e.g., `claude-opus-4-5-…` → `opus` mapping). This would
  require either a reverse-lookup table or a comment field on the mapping.

### 6. checkRequiredEnvVars (discarded at startup)

- `cmd/freedius/main.go:134` — `_ = checkRequiredEnvVars(cfg)` — the error is
  discarded. This function (main.go:376-393) iterates all mappings, looks up
  the provider, and returns an error if `DefaultAPIKeyEnv` is set but the env
  var is empty.
- The frame suggests "un-discarding its result at `cmd/freedius/main.ts:134`"
  (note: typo in frame — should be `main.go`, not `main.ts`). Un-discarding
  would make freedius **fail to start** if any configured mapping's env var
  is missing, which may be too aggressive — the per-mapping `EnvPresent` status
  on cards is more useful for the maintainer's actual need (knowing which
  mappings will fail at request time without blocking startup).
- The `EnvPresent` field already provides the per-mapping status the frame
  describes as missing. The gap is that `checkRequiredEnvVars` does NOT feed
  into the card data — it is a separate startup check.

### 7. README provenance coverage

- `README.md:15-21` — "Reading the system state" section describes the dashboard
  as primary, mapping cards showing "routing shape plus provenance (when added,
  whether the API key is present right now, family badge)."
- `README.md:103-106` — "Mapping cards — routing shape plus provenance: when
  added (`added_at`), a green/amber dot for whether the API key is in the
  environment right now, and a family badge."
- The README already documents the provenance features that the frame identifies
  as missing. The frame's claim that "the README doesn't anchor any of this"
  (frame.md:75-78) is **outdated** — the README was rewritten in commit
  `be00154 docs(solo-dev-positioning): README Rewrite (p3)` which is part of
  this same change.

## Code References

- `proxy/web/templates/mappings-table.html:1-74` — breadcrumb-chain card template
- `proxy/web/handlers.go:268-349` — `buildMappingRows` constructs mapping rows
- `proxy/web/handlers.go:317-324` — env presence check (`os.Getenv`)
- `proxy/web/handlers.go:342` — `AddedAt` field populated
- `proxy/web/handlers.go:325-328` — family extraction
- `proxy/web/types.go:55-76` — `fallbackEntry` and `mappingRow` structs
- `proxy/web/static/app.css:825-1051` — card, chain, status-dot, badge CSS
- `config/config.go:40` — `Provider.DefaultAPIKeyEnv` field
- `config/config.go:64-69` — `Mapping` struct (ProviderName, ModelString, Fallback, AddedAt)
- `config/config.go:67` — `Fallback []Mapping` field (no rationale)
- `proxy/families.go:10-29` — family pattern matching
- `cmd/freedius/main.go:134` — discarded `checkRequiredEnvVars` result
- `cmd/freedius/main.go:376-393` — `checkRequiredEnvVars` function
- `README.md:15-21` — "Reading the system state" section
- `README.md:82-93` — provenance annotation documentation
- `README.md:103-106` — mapping cards provenance description
- `context/archive/2026-07-06-mapping-graph-visualization/plan.md` — original card implementation plan
- `context/archive/2026-07-07-routing-visibility/frame.md:34` — dashboard routing context gap

## Architecture Insights

1. **The card renderer is already provenance-aware** — The `mappingRow` struct
   carries `AddedAt`, `EnvPresent`, and `Family` fields, and the template
   renders all three. The frame's premise that the card "does not show" these
   signals is outdated relative to the current codebase state.

2. **Two separate env-check mechanisms exist** — `checkRequiredEnvVars`
   (startup-time, returns error, discarded) and `EnvPresent` (per-mapping,
   rendered on cards). The frame conflates them. The per-mapping status is
   the one that matters for the maintainer's need.

3. **Fallback rationale is the true gap** — No field exists anywhere in the
   data model to capture *why* a fallback chain exists. Adding a `rationale`
   or `reason` field to `config.Mapping` (or a new `FallbackEntry` struct)
   would be the minimal change to address this.

4. **Git blame integration is not present** — The frame suggests sourcing
   added-at from git blame, but currently `AddedAt` is a user-supplied
   free-form string. Git blame integration would require either a build-time
   code generation step or a runtime `git blame` subprocess call.

5. **The README was already rewritten** — Commit `be00154` (part of this same
   change) rewrote the README to include provenance documentation. The frame's
   claim that the README doesn't cover provenance is outdated.

## Historical Context (from prior changes)

- `context/archive/2026-07-06-mapping-graph-visualization/plan.md` — Original
  plan that shipped the breadcrumb-chain card layout. Phase 1 restructured
  `mappingRow` to carry `[]fallbackEntry` (structured fallbacks). Phase 2
  replaced the table with card-based chain. Phase 3 integrated the edit dialog.
  The plan does NOT mention provenance fields (AddedAt, EnvPresent, Family) —
  these were added later, likely as part of the solo-dev-positioning change.
- `context/archive/2026-07-07-routing-visibility/frame.md` — Earlier framing
  that identified routing visibility gaps across the entire web UI. This is
  the predecessor to the current frame, which narrows the scope to provenance
  specifically.
- `context/changes/solo-dev-positioning/plan.md` — The current change's plan,
  which includes phases for Data & Types (p1), Card Rendering (p2), and README
  Rewrite (p3). The provenance fields were added in p1 (commit `fa1109f`).

## Related Research

- `context/archive/2026-07-06-mapping-graph-visualization/plan.md` — Original
  card implementation plan (no provenance fields mentioned)
- `context/archive/2026-07-07-routing-visibility/frame.md` — Predecessor
  framing for routing visibility
- `context/changes/solo-dev-positioning/plan.md` — Current change plan with
  provenance field implementation

## Open Questions

1. **Should `checkRequiredEnvVars` be un-discarded?** — Un-discarding would
   make freedius fail to start if any mapping's env var is missing. The
   per-mapping `EnvPresent` status on cards already provides this information
   at runtime. The question is whether startup failure is desirable or if
   the card status is sufficient.

2. **How should fallback rationale be stored?** — Options:
   - Add a `rationale string` field to `config.Mapping` (applies to the
     primary→fallback relationship, not per-fallback).
   - Add a `FallbackEntry` struct with `ProviderName`, `ModelString`,
     `Rationale` fields (replaces `[]Mapping` with `[]FallbackEntry`).
   - Add a free-form `comment` field to each fallback entry.

3. **Should git blame integration be added?** — Currently `AddedAt` is
   user-supplied. Git blame would provide automatic timestamps but requires
   a subprocess call or build-time codegen. The frame suggests this as a
   possibility but it is not strictly necessary if `AddedAt` is sufficient.

4. **Is the family badge sufficient for the "name binding" concern?** — The
   frame's Hypothesis #3 (marked WEAK) asks whether the mapping key guarantees
   correct resolution. The family badge shows which family the key belongs to,
   but does not enumerate which client model strings resolve to this mapping.
   This may be derivative of the provenance work and can be deferred.

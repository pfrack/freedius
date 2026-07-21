# Mapping Card Provenance + README Rewrite — Implementation Plan

## Overview

Enrich the mapping-graph breadcrumb cards with decision-provenance signals (when added, env-var presence, family resolution) so the maintainer can reconstruct why each mapping exists and whether it will forward successfully. Then rewrite the README from scratch to answer "what is this for / how do I read its current state" in under 30 seconds.

## Current State Analysis

- `buildMappingRows` (`proxy/web/handlers.go:267-337`) translates `config.Mapping` → `mappingRow` (`proxy/web/types.go:64-73`). The `mappingRow` struct carries only routing data (name, provider, model, protocol, baseURL, responder, fallbacks). **No provenance fields exist.**
- `checkRequiredEnvVars` (`cmd/freedius/main.go:376-393`) iterates mappings and checks `os.Getenv(provider.DefaultAPIKeyEnv)`, but its return value is **discarded** at `main.go:134` (`_ = checkRequiredEnvVars(cfg)`). The env-presence knowledge is computed and thrown away.
- `extractFamily()` (`proxy/families.go:18-25`) exists and compiles but has **zero production callers** — it's dead code from an archived plan (`context/archive/2026-07-07-routing-visibility/plan-2.md`).
- The card template (`proxy/web/templates/mappings-table.html:18-68`) renders `.route-card__header` with name + fallback-count pill + actions, then a `.route-chain` of `.route-step` pills. The edit dialog (`mappings.html:95-118`) reconstructs state purely from `data-*` attributes on the Edit button — display-only signals added to the card won't couple to the dialog.
- README (`README.md`, 218 lines) opens with "what it connects" (a list of 16 providers), then Quickstart, Docker, Config table, CLI flags. There is **zero** content on "what it is for / who maintains it / how to read the system's current state."

### Key Discoveries

- `mappingRow` enrichment happens in Go (struct fields), not templates — the template just renders whatever the struct carries. Adding signals = extend struct + populate in `buildMappingRows`.
- The renderer already resolves `providers[m.ProviderName]` at `handlers.go:316-319` to pull `Protocol` and `DefaultBaseURL`. Env presence can be computed inline at the same point: `os.Getenv(p.DefaultAPIKeyEnv) != ""`.
- `Provider.DefaultAPIKeyEnv` (`config/config.go:40`) is the canonical env-var name. It's populated either from user config or generated defaults (`config/defaults.go:33-35`).
- CSS has a `.route-card__depth` pill (`app.css:915-922`) and a `.route-step--responder::before` status-dot pattern (`app.css:924-935`) — both reusable language for new badges.
- The edit dialog's `data-*` attributes (`mappings-table.html:23-29`) are read-only reconstruction inputs; card-head display signals don't need to propagate into them.

## Desired End State

After this plan:
1. Each mapping card shows: (a) an "added" date (from config annotation), (b) a green/amber env-var-present dot, (c) a family badge (opus/sonnet/haiku).
2. A maintainer returning after a gap reads the README in 30 seconds and understands: what freedius does, who it's for, how to read the system state from the dashboard (including the new card signals), and honest packaging (no "no setup" gloss).
3. `extractFamily()` is no longer dead code.

### Verification

- `mage test` passes — `buildMappingRows` is unit-testable; new fields can be asserted.
- `mage lint` passes — no new deps, no unsafe patterns.
- Cards render the three new signals when served at `http://localhost:8083/mappings`.
- README opens with purpose/audience, not a provider list.

## What We're NOT Doing

- **Git blame integration** — rejected in favour of config annotation (no git coupling in the binary).
- **Runtime first-seen timestamp** — rejected (resets on reload, can't retroact).
- **Mapping-key ↔ client-model binding resolution** — the frame scored this WEAK; we surface family via the existing regex classifier rather than building model enumeration logic.
- **Env presence precomputed at startup / stored on Handlers** — rejected; inline check at render time is simpler and reflects the live process env.
- **"Why it exists" free-text rationale** — the config annotation (`added_at`) is the only structured provenance signal. A free-text "rationale" field was considered but rejected: no source of truth exists, and manual prose per mapping doesn't pay for itself at current mapping counts.
- **Changing the `config.Provider` struct** — provenance lives on mappings, not providers.
- **Touching the edit dialog or fallback chain JS** — new signals are read-only card-head display only.

## Implementation Approach

Extend the read path only: add fields to `mappingRow`, populate them in `buildMappingRows` using already-available data (config annotation, `os.Getenv` inline, `extractFamily`), render in the template, add minimal CSS. Then rewrite README independently. No new types, no new dependencies, no new endpoints, no schema migrations — config annotation is additive (blank = unknown).

## Phase 1: Data & Types

### Overview

Extend the `mappingRow` struct with three provenance fields and populate them in `buildMappingRows` using data already available in that function's scope. Add an optional `added_at` field to the `config.Mapping` YAML schema. Wire up the existing dead-code `extractFamily()`.

### Changes Required

#### 1. Add `AddedAt` to the mapping config schema

**File**: `config/config.go` (struct `Mapping` at line 64-68)

**Intent**: Allow users to annotate when a mapping was added, so cards can render an "added" date. Optional — blank means unknown (no validation error).

**Contract**: Add a field `AddedAt string \`yaml:"added_at,omitempty"\`` to the `Mapping` struct. It's a free-form string (e.g. `2026-07-06` or `2026-07-06 — added after opus routing broke`). No regex/validation — loose so users can write whatever's useful. The field round-trips through YAML but isn't validated in `validateMapping` (it's metadata, not routing-critical).

#### 2. Extend `mappingRow` with provenance fields

**File**: `proxy/web/types.go` (struct `mappingRow` at line 64-73)

**Intent**: Carry the three new signals from `buildMappingRows` to the template renderer. Read-only display data.

**Contract**: Add three fields to `mappingRow`:
- `AddedAt string` — the config annotation (may be empty)
- `EnvPresent bool` — whether `DefaultAPIKeyEnv` resolves in the process environment
- `Family string` — extracted family keyword (opus/sonnet/haiku/auto), empty if no match

#### 3. Populate new fields in `buildMappingRows`

**File**: `proxy/web/handlers.go` (function `buildMappingRows`, lines 314-333)

**Intent**: Populate `AddedAt`, `EnvPresent`, and `Family` using data already resolved in the loop body. No new lookups beyond what's already happening at lines 316-319.

**Contract**: After resolving `proto`/`url` from `providers[m.ProviderName]` (existing lines 316-319), compute:
- `AddedAt: m.AddedAt` (copy from the config `Mapping`; empty string if unset)
- `envPresent := false` then `if p, ok := providers[m.ProviderName]; ok && p.DefaultAPIKeyEnv != "" { envPresent = os.Getenv(p.DefaultAPIKeyEnv) != "" }` — note the short-circuit: when `DefaultAPIKeyEnv` is empty (provider requires no key), `envPresent` stays `false` but the signal is suppressed in the template (no badge rendered for keyless providers).
- `Family: extractFamily(name)` — call the existing `proxy.families.extractFamily` with the mapping's card name (the `name` variable from the outer loop, line 280).

Populate all three in the `mappingRow{...}` literal at lines 324-333.

Add `"os"` import if not already present.

#### 4. Wire up `extractFamily` (dead code activation)

**File**: `proxy/web/handlers.go` (import block + call site from item 3)

**Intent**: Connect the existing unused `extractFamily` function into the rendering path so it's no longer dead code.

**Contract**: In the import block, add `"github.com/pfrack/freedius/proxy"` if not already imported (handlers.go already imports the proxy package at line 10 — verify; if already present, no import change). Call `extractFamily(name)` where described in item 3. No modification to `extractFamily` itself — it's already correct.

### Success Criteria

#### Automated Verification

- `mage test` passes — including any new or extended tests for `buildMappingRows` asserting the three new fields populate correctly.
- `mage lint` passes — no new lint violations from added fields/calls.
- `go vet ./...` passes.
- `go build ./...` passes.

#### Manual Verification

- Start the server with a config where one mapping has `added_at: 2026-07-06` and another does not; confirm the first card shows the date and the second shows "unknown" or nothing.
- Unset the env var for one provider and confirm the card shows the missing-state indicator; set it and confirm the present-state indicator.

**Implementation Note**: After completing this phase and all automated verification passes, pause here for manual confirmation from the human that the manual testing was successful before proceeding to the next phase.

---

## Phase 2: Card Rendering

### Overview

Render the three new provenance signals on each mapping card in `mappings-table.html` and add minimal CSS matching existing badge/dot patterns. The edit dialog stays untouched — new fields are read-only card-only signals.

### Changes Required

#### 1. Render provenance signals in the card header

**File**: `proxy/web/templates/mappings-table.html` (card header, lines 19-43)

**Intent**: Display the three new signals in the `.route-card__header` row so the maintainer can read them at a glance without opening anything.

**Contract**: Inside `.route-card__header` (after the name `<h3>` and alongside/after `.route-card__depth` at line 21), add three read-only signals:

- **Added-at**: `<span class="route-card__meta">{{if .AddedAt}}{{.AddedAt}}{{else}}added: unknown{{end}}</span>` — only render the `added: unknown` fallback if `.AddedAt` is empty, so it's honest rather than showing a blank.
- **Env dot**: `<span class="status-dot {{if .EnvPresent}}status-dot--present{{else}}status-dot--missing{{end}}" title="{{if .EnvPresent}}API key present{{else}}API key missing — requests to this mapping will fail{{end}}"></span>` — a 6px circle, green when present, amber ring when missing. The title attribute provides the consequence explanation on hover.
- **Family badge**: `{{if .Family}}<span class="badge badge--family">{{.Family}}</span>{{end}}` — only rendered when `extractFamily` matched. Reuses the `.badge` base class.

The signals sit between the name and the actions group, or inline with the existing `.route-card__depth` pill. The existing `{{if ...}}` blocks in the template already handle empty cases — new signals follow the same pattern.

No `data-*` attribute changes on the Edit button — the new signals are display-only and don't propagate to the dialog.

#### 2. Add CSS for new card signals

**File**: `proxy/web/static/app.css`

**Intent**: Style the three new signal elements to match the existing design tokens and badge/dot patterns.

**Contract**: Add rules:

- `.route-card__meta` — reuse the `.route-card__depth` token set (`app.css:915-922`): `font-size:0.75rem; color:var(--text-muted);` — sits alongside the depth pill.
- `.status-dot` — modeled on `.route-step--responder::before` (`app.css:924-928`): `display:inline-block; width:6px; height:6px; border-radius:50%;`
  - `.status-dot--present` — `background:var(--color-success);` (green dot, matches primary/fallback step color)
  - `.status-dot--missing` — `background:transparent; border:1px solid var(--color-warning);` (amber ring — deliberately not filled, to distinguish from "present" without requiring color perception)
- `.badge--family` — modeled on `.badge--protocol` (`app.css:709`): `background:var(--accent-subtle); color:var(--accent);` — reuses the accent tint rather than introducing a new hue.

Add `@media (prefers-reduced-motion: reduce)` overrides only if any animation is introduced (the responder `::before` pulses — the env dot should NOT pulse, so no override needed).

Add light-mode overrides only if the new rules use tokens that lack light-mode definitions in `app.css:74-108` (they shouldn't — all reference existing tokens).

### Success Criteria

#### Automated Verification

- `mage test` passes — template-rendering tests (if any exist in `handlers_test.go` or similar) still pass.
- `mage lint` passes.
- `go build ./...` passes.

#### Manual Verification

- Open `http://localhost:8083/mappings` and verify each card shows all three signals.
- Unset an env var, refresh, confirm the dot turns amber ring; re-set, refresh, confirm green.
- Resize to mobile width (<768px) — signals should not overflow or overlap the actions group.
- Open the Edit dialog — confirm it's unchanged and still works.

**Implementation Note**: After completing this phase and all automated verification passes, pause here for manual confirmation from the human that the manual testing was successful before proceeding to the next phase.

---

## Phase 3: README Rewrite

### Overview

Rewrite `README.md` from scratch. Lead with purpose/audience/state-reading. Ground every claim in the current system state. Cut "what it connects" to a reference section at the bottom. The maintainer is the only end user — no cold-start persona, no alternative-tool pitch.

### Changes Required

#### 1. Rewrite README from scratch

**File**: `README.md`

**Intent**: Replace the current 218-line "what it connects" document with one that answers "what is this for / who is it for / how do I read its current state" in under 30 seconds.

**Contract**: The new structure (replace the file, not append):

```
# freedius

[2-3 sentence tagline: what it does, who it's for, how it's packaged.
Mention "solo dev maintainer," "local HTTP proxy," "single static binary."]

## What it does

[1 short paragraph: routes LLM API requests from coding agents to upstreams,
with fallback chains, via model-name mapping.]

## Reading the system state

[Short section pointing the maintainer at the web dashboard: mapping cards show
routing shape + provenance (when added, env-var presence, family). Logs stream
live via SSE. The "last-used responder" highlight tells you which fallback
actually fired on the last request.]

## Quickstart

[Keep the existing curl example — it's good. Trim the build flag details.]

## Configuration

[Keep: resolution order, example YAML, mapping resolution, fallback chains.
Move the full provider table to a reference section or drop it — the table is
16 rows of "what it connects" with no actionability. Link to providers.yaml
instead.]

## Web Dashboard

[Keep: what the dashboard shows, the auth token note. Point to the mapping
card signals (Phase 1-2) as the primary state-reading surface.]

## CLI & Environment Variables

[Keep the flag table and env var table — these are reference material the
maintainer already uses. One consolidated section instead of two.]

## Development

[Keep the mage commands — these are muscle memory for the maintainer.]

## Reference

[Provider table moved here. API response headers. Built-in endpoints. The
things you only look up occasionally — out of the way at the bottom.]
```

**Constraints**:
- Every system claim must match the actual implementation (no "no web UI in v1" stale claims, no "no setup required" — there IS a credential gate and it's now surfaced on cards).
- The "added_at" annotation and env-dot should be described concretely: "Each mapping card shows when it was added and whether its API key is in the environment right now."
- Length target: 90-140 lines (roughly half the current 218).
- No meta-commentary, no marketing voice. Short declarative sentences.
- The maintainer IS the end user — write for one person returning to the project after a week away, not for a cold audience.

### Success Criteria

#### Automated Verification

- No broken internal links: grep the README for any `[...](...)` link targets and verify each exists (e.g. `providers.yaml` path).
- `mage test` passes (README change is inert to tests — verification is just "didn't break the build").
- Spellcheck pass: run `go run github.com/client9/misspell/...` or `typos` if available in the toolchain; else manual read-through.

#### Manual Verification

- A maintainer reading from the top understands what freedius is and where to look for system state within 30 seconds.
- No stale claims ("no web UI", "no setup required", etc.).
- All config examples still parse as valid YAML (they're preserved from current README).

**Implementation Note**: After completing this phase and all automated verification passes, pause here for manual confirmation from the human that the manual testing was successful before proceeding to the next phase.

---

## Testing Strategy

### Unit Tests

- If `buildMappingRows` has existing tests in `handlers_test.go`, extend them to assert the three new fields. Key assertions: `AddedAt` copies from config input; `EnvPresent` is true when env var set, false when unset; `Family` matches `extractFamily` output for known family names.
- If no existing tests, add a table-driven test for `buildMappingRows` covering: empty config, mapping with all three signals present, mapping with `added_at` blank, keyless provider (no env dot rendered).

### Integration Tests

- Render the `mappings-table.html` template with a `mappingRow` carrying known `AddedAt`/`EnvPresent`/`Family` values and assert the output HTML contains the expected class names (`status-dot--present`, `badge--family`) and text.

### Manual Testing Steps

1. Run `mage run`, open `http://localhost:8083/mappings`.
2. Confirm every card shows added-date (or "unknown"), env dot, and family badge (if matched).
3. Toggle an env var, refresh, confirm dot state changes.
4. Open and use the Edit dialog — confirm it still works unaffected.
5. Read the README top-to-bottom as if returning after a week — confirm purpose is clear in 30 seconds.
6. Check mobile viewport — cards and signals do not overflow.

## Performance Considerations

- `buildMappingRows` already runs on every HTMX refresh of the mappings panel. Adding one `os.Getenv` call per mapping per render is negligible (env reads are ns-range; the panel renders tens of mappings at most).
- No new allocations beyond the three new fields per `mappingRow` (two one-word strings, one bool).
- No caching concerns — env presence is read fresh each render, which is the desired behavior (status reflects the live process env).

## Migration Notes

- **Config files are backward compatible**: the `added_at` field is `omitempty` with no validation. Existing configs load unchanged (field defaults to empty string → card shows "unknown").
- **No database, no schema migration** — the config is YAML, fields are additive.
- **README is a full replacement** — `git diff` will show a large deletion+addition. That's expected and correct.

## References

- Frame brief: `context/changes/solo-dev-positioning/frame.md` (authoritative problem framing; HIGH confidence).
- Archived mapping-graph plan: `context/archive/2026-07-06-mapping-graph-visualization/plan.md` (card structure, template naming).
- Archived routing-visibility plan: `context/archive/2026-07-07-routing-visibility/plan-2.md` (family field planned but never shipped).
- Existing env-check: `cmd/freedius/main.go:376-393` (discarded — we reuse its logic inline rather than un-discarding).
- CSS design tokens: `proxy/web/static/app.css:7-29`, `.badge` at `:686-712`, `.route-step--responder::before` at `:924-935`.

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles. See `references/progress-format.md`.

### Phase 1: Data & Types

#### Automated

- [x] 1.1 `mage test` passes with extended/new `buildMappingRows` assertions — fa1109f
- [x] 1.2 `mage lint` passes — fa1109f
- [x] 1.3 `go build ./...` passes — fa1109f

#### Manual

- [ ] 1.4 Start server with mixed `added_at` annotations — cards show date vs "unknown"
- [ ] 1.5 Toggle env var — env-present state reflects live process env

### Phase 2: Card Rendering

#### Automated

- [ ] 2.1 `mage test` passes
- [ ] 2.2 `mage lint` passes
- [ ] 2.3 `go build ./...` passes

#### Manual

- [ ] 2.4 Open `/mappings` — all three signals render on each card
- [ ] 2.5 Toggle env var, refresh — dot color changes green ↔ amber
- [ ] 2.6 Mobile viewport (<768px) — signals don't overflow
- [ ] 2.7 Edit dialog still works, no regression

### Phase 3: README Rewrite

#### Automated

- [ ] 3.1 No broken internal links (grep validation)
- [ ] 3.2 `mage test` passes (build unaffected)
- [ ] 3.3 Spellcheck passes

#### Manual

- [ ] 3.4 Read README top-to-bottom — purpose clear in 30 seconds
- [ ] 3.5 No stale claims remain ("no web UI", "no setup required", etc.)
- [ ] 3.6 Config examples still valid YAML

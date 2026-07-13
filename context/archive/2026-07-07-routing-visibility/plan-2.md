# Routing Visibility — Plan 2: Richer Per-Step Metadata

## Overview

Surface the routing-relevant metadata that the chain cards currently hide:
provider Behavior class (openai/anthropic/mix), effective BaseURL (post-
normalization), API Key Env var, and family matching visibility. This is the
second of three plans identified by the routing-visibility frame brief.

The frame investigation found that the breadcrumb chain shows only
Provider/Model + Protocol (when set), collapsing ~7 routing decision points
into 2 visible dimensions. This plan closes that gap for static config data
(the remaining runtime dimension is Plan 3).

## Current State Analysis

- **Chain cards** (`mappings-table.html:44-67`): Each `.route-step` shows
  `ProviderName / Model` + conditional Protocol badge + conditional last-
  responder chevron. Tooltip shows configured BaseURL (not effective).
- **`fallbackEntry`** (`types.go:54-59`): Carries `ProviderName`, `Model`,
  `Protocol`, `BaseURL`. Missing: `Behavior`, `APIKeyEnv`, `EffectiveURL`.
- **`mappingRow`** (`types.go:62-71`): Same fields as primary step. Missing:
  same three fields.
- **`mix.go:84-101`**: `normalizeBaseURL` appends `/v1/messages` or
  `/v1/chat/completions` when `Protocol` is set. The effective URL differs
  from the configured one — invisible in the UI.
- **`families.go:10-16`**: `extractFamily` matches mapping names against
  `opus|sonnet|haiku|auto|default` regexes. Completely invisible in the UI.
  When a mapping named "opus" exists, requests for `claude-opus-4-20250514`
  match it — users have no idea.
- **`handlers.go:217-251`**: `buildMappingRows` (from Plan 1) populates
  `Protocol` and `BaseURL` from provider lookup. Does not populate
  `Behavior`, `APIKeyEnv`, or compute effective URL.

### Key Discoveries:

- `normalizeBaseURL` is a method on `*MixAdapter` (`mix.go:84`), not a
  standalone function. The handler layer cannot call it directly. Extracting
  the logic into a package-level helper (or reimplementing in the handler)
  is needed. The logic is simple: if Protocol is "anthropic", append
  `/v1/messages`; if "openai", append `/v1/chat/completions`. A 10-line
  helper in `handlers.go` suffices.
- Family matching is a **mapping-level** concept (the mapping name matches a
  family regex), not a step-level concept. The badge belongs in the card
  header, not on individual steps.
- The Providers page already shows `Behavior` and `APIKeyEnv` columns
  (`providers-table.html:18-19`). The mapping cards should surface the same
  data inline to avoid cross-page lookups.

## Desired End State

Each `.route-step` pill in the chain shows:
- **Provider / Model** (existing)
- **Protocol badge** (existing, conditional)
- **Behavior label** (new): small text "openai", "anthropic", or "mix"
- **API Key Env** (new): small muted text showing the env var name (e.g.,
  `ANTHROPIC_API_KEY`)
- **Tooltip** (updated): shows effective BaseURL (post-normalization) instead
  of configured URL

Each mapping card header shows:
- **Mapping name** (existing)
- **Family badge** (new, conditional): when the mapping name matches a known
  family (`opus`, `sonnet`, `haiku`, `auto`, `default`), show a small badge
  indicating "family match"
- **Fallback count** (existing)

Verification: load `/mappings` — each step shows Behavior + API Key Env.
Hover a step — tooltip shows effective URL. A mapping named "opus" shows a
"family" badge in its header.

## What We're NOT Doing

- **No runtime stats** (error rates, fallback triggers) — Plan 3.
- **No changes to the Dashboard** — Plan 1.
- **No cross-page links** — Plan 1.
- **No config schema changes** — all changes in `proxy/web/` types and
  handlers.
- **No new backend endpoints** — purely data-model + template changes.
- **No changes to `mix.go`** — the effective URL logic is reimplemented as a
  simple handler-layer helper (10 lines), not extracted from MixAdapter.

## Implementation Approach

Add `Behavior`, `APIKeyEnv`, and `EffectiveURL` fields to `fallbackEntry` and
`mappingRow`. Compute effective URL in the handler using a small helper that
mirrors `normalizeBaseURL`'s suffix-appending logic. Add family matching
detection to `buildMappingRows` using the `extractFamily` function from
`families.go`. Update the template to render the new fields.

## Critical Implementation Details

- **`extractFamily` is in package `proxy`** (`families.go:18`). The web
  handler package (`proxy/web`) already imports `proxy` (for `LogSink`,
  `EventBus`, etc.). Calling `proxy.ExtractFamily` (if exported) or
  reimplementing the regex check in the handler is straightforward. The
  function is 7 lines; reimplementing avoids coupling to the proxy package's
  internal family list. Decision: reimplement as a `knownFamilies` map in
  `handlers.go` — the families are a UI concern (what to display), not a
  proxy-internal concern.
- **Effective URL computation** depends on Protocol. When Protocol is empty,
  the effective URL is the configured URL (path-sniffing happens at runtime
  and can't be predicted statically). When Protocol is set, the suffix is
  deterministic: `"anthropic"` → append `/v1/messages`; `"openai"` → append
  `/v1/chat/completions`. The helper only needs these two cases.
- **`buildMappingRows` is shared** between `handleMappings` and
  `renderMappingsTable` (extracted in Plan 1). Both callers benefit from the
  new fields automatically once `buildMappingRows` populates them.

---

## Phase 1: Data model + handler changes

### Overview

Add `Behavior`, `APIKeyEnv`, and `EffectiveURL` fields to the web data types.
Update `buildMappingRows` (from Plan 1) to populate them. Add family matching
detection.

### Changes Required:

#### 1. Extend `fallbackEntry` and `mappingRow` with new fields

**File**: `proxy/web/types.go`

**Intent**: Add `Behavior string`, `APIKeyEnv string`, and `EffectiveURL string`
to `fallbackEntry` (lines 54-59) and `mappingRow` (lines 62-71). These carry
the provider's behavior class, API key env var, and effective base URL (post-
normalization) for each step.

**Contract**: Three new exported fields on each struct. No JSON tags needed
(templates access via Go field names). No changes to `config.Mapping` or
`config.Provider`.

#### 2. Add `computeEffectiveURL` helper

**File**: `proxy/web/handlers.go`

**Intent**: Pure function that computes the effective base URL given a
configured URL and protocol. Mirrors the suffix-appending logic from
`mix.go:84-101` without importing MixAdapter.

**Contract**:

```go
func computeEffectiveURL(baseURL, protocol string) string
```

When `protocol` is `"anthropic"`, ensure the URL path ends with `/v1/messages`.
When `protocol` is `"openai"`, ensure the URL path ends with
`/v1/chat/completions`. When `protocol` is empty, return `baseURL` unchanged
(path-sniffing is a runtime decision). Use `net/url.Parse` for URL
manipulation.

#### 3. Add family matching detection

**File**: `proxy/web/handlers.go`

**Intent**: Detect when a mapping name matches a known family pattern. Add a
`Family string` field to `mappingRow`. Populate it in `buildMappingRows` by
checking the mapping name against a local `knownFamilies` map.

**Contract**: Add `Family string` to `mappingRow` in `types.go`. In
`buildMappingRows`, check the mapping name against `knownFamilies =
map[string]*regexp.Regexp{"opus": ..., "sonnet": ..., "haiku": ..., "auto":
..., "default": ...}`. Set `Family` to the matched family name, or empty if
no match. Reimplement the regexes locally (don't import from `proxy` package)
— the family list is a UI concern.

#### 4. Update `buildMappingRows` to populate new fields

**File**: `proxy/web/handlers.go`

**Intent**: In the existing `buildMappingRows` function (from Plan 1), populate
`Behavior`, `APIKeyEnv`, and `EffectiveURL` from the provider lookup. Populate
`Family` from the family matching check.

**Contract**: In the per-mapping loop, after looking up the provider:
- Set `Behavior` from `p.Behavior`.
- Set `APIKeyEnv` from `p.DefaultAPIKeyEnv`.
- Set `EffectiveURL` from `computeEffectiveURL(p.DefaultBaseURL, p.Protocol)`.
- Set `Family` from the family matching check.
Same for each fallback entry.

### Success Criteria:

#### Automated Verification:

- [ ] 1.1 `go build ./...` succeeds
- [ ] 1.2 `mage test` passes — all new + existing tests
- [ ] 1.3 `mage lint` clean
- [ ] 1.4 `TestMappingRow_PopulatesBehavior` — create provider with
  `Behavior: "mix"`; assert `mappingRow.Behavior == "mix"`
- [ ] 1.5 `TestMappingRow_PopulatesAPIKeyEnv` — create provider with
  `DefaultAPIKeyEnv: "ANTHROPIC_API_KEY"`; assert field is populated
- [ ] 1.6 `TestComputeEffectiveURL_Anthropic` — URL `https://api.example.com/v1`
  with protocol `"anthropic"` → `https://api.example.com/v1/messages`
- [ ] 1.7 `TestComputeEffectiveURL_OpenAI` — URL `https://api.example.com/v1`
  with protocol `"openai"` → `https://api.example.com/v1/chat/completions`
- [ ] 1.8 `TestComputeEffectiveURL_AlreadyCorrect` — URL already ends with
  `/v1/messages` → unchanged
- [ ] 1.9 `TestComputeEffectiveURL_EmptyProtocol` — empty protocol → URL
  unchanged
- [ ] 1.10 `TestFamilyMatching_Opus` — mapping named `"opus"` → `Family == "opus"`
- [ ] 1.11 `TestFamilyMatching_NoMatch` — mapping named `"claude-custom"` →
  `Family == ""`
- [ ] 1.12 `TestFamilyMatching_CaseInsensitive` — mapping named `"Sonnet"` →
  `Family == "sonnet"`

#### Manual Verification:

- [ ] 1.13 Load `/mappings` — each step shows Behavior label
- [ ] 1.14 Each step shows API Key Env as muted text
- [ ] 1.15 Hover a step — tooltip shows effective URL (not configured URL)
- [ ] 1.16 Mapping named "opus" — card header shows family badge

**Implementation Note**: After completing this phase and all automated
verification passes, pause for manual confirmation before proceeding to Phase 2.

---

## Phase 2: Template + CSS rendering

### Overview

Update the chain card template to render the new fields. Add CSS for the
Behavior label, API Key Env text, and family badge.

### Changes Required:

#### 1. Template: render Behavior, API Key Env, effective URL, family badge

**File**: `proxy/web/templates/mappings-table.html`

**Intent**: Update each `.route-step` to show Behavior and API Key Env. Update
the tooltip to show `EffectiveURL`. Add a family badge to the card header.

**Contract**:

For each step pill (primary and fallback), after the Protocol badge:
```html
{{if .Behavior}}<span class="route-step__behavior">{{.Behavior}}</span>{{end}}
{{if .APIKeyEnv}}<span class="route-step__apikey text-muted">{{.APIKeyEnv}}</span>{{end}}
```

Update the `title` attribute to use `EffectiveURL` instead of `BaseURL`:
```html
title="{{.EffectiveURL}}{{if .Protocol}} ({{.Protocol}}){{end}}"
```

In the card header, after the mapping name, add a conditional family badge:
```html
{{if .Family}}<span class="route-card__family badge">{{.Family}}</span>{{end}}
```

#### 2. CSS: Behavior label, API Key Env, family badge

**File**: `proxy/web/static/app.css`

**Intent**: Add styles for the new inline elements. Reuse existing design
tokens only.

**Contract**: New rules in the breadcrumb-chain block (after line 847):

- `.route-step__behavior` — small uppercase label, `font-size: 0.7rem`,
  `color: var(--text-muted)`, `text-transform: uppercase`, `letter-spacing:
  0.05em`.
- `.route-step__apikey` — small muted text, `font-size: 0.7rem`,
  `font-family: var(--font-mono)`.
- `.route-card__family` — small badge in the card header, reusing
  `--badge--protocol` styles with `--color-warning` border.

### Success Criteria:

#### Automated Verification:

- [ ] 2.1 `go build ./...` succeeds
- [ ] 2.2 `mage test` passes
- [ ] 2.3 `mage lint` clean
- [ ] 2.4 New regex test on rendered `mappings-table.html`: each `.route-step`
  with a Behavior field contains `class="route-step__behavior"`
- [ ] 2.5 New regex test: mapping named `"opus"` card header contains
  `class="route-card__family`

#### Manual Verification:

- [ ] 2.6 Load `/mappings` — each step shows Behavior label (e.g., "mix",
  "openai") and API Key Env (e.g., "ANTHROPIC_API_KEY")
- [ ] 2.7 Hover a step — tooltip shows effective URL (e.g.,
  `https://api.anthropic.com/v1/messages (anthropic)`)
- [ ] 2.8 Mapping named "opus" — card header shows "family" badge
- [ ] 2.9 `prefers-reduced-motion` — no visual change (no new animations)

**Implementation Note**: After completing this phase and all automated
verification passes, the plan is ready for `/10x-impl-review`.

---

## Testing Strategy

### Unit Tests:

- Phase 1: Handler-level tests for `buildMappingRows` populating new fields.
  Pure function tests for `computeEffectiveURL` covering all protocol cases.
  Regex tests for family matching.
- Phase 2: Template-rendering tests asserting new CSS classes and content
  appear in rendered HTML.

### Integration Tests:

- Existing `handlers_test.go` `TestPageHandlers` covers page status codes.
  No new integration tests needed — this plan adds display fields, not new
  routes.

### Manual Testing Steps:

Load `/mappings` → verify each step shows Behavior + API Key Env → hover to
check effective URL tooltip → check family badge on family-named mappings.

## Performance Considerations

- `computeEffectiveURL` is called once per step per page render. It parses a
  URL and checks a suffix — negligible cost.
- Family matching is a regex check per mapping name — O(1) per mapping.
- No new API endpoints or data aggregation.

## Migration Notes

- None. No config schema changes. No data migration.

## References

- Frame brief: `context/changes/routing-visibility/frame.md`
- Prior plan: `context/changes/routing-visibility/plan.md` (Plan 1)
- Prior research: `context/changes/web-ui-friendliness/research.md`
- Foundation lessons: `context/foundation/lessons.md`

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a
> step lands. Do not rename step titles. See `references/progress-format.md`.

### Phase 1: Data model + handler changes

#### Automated

- [ ] 1.1 `go build ./...` succeeds
- [ ] 1.2 `mage test` passes — all new + existing tests
- [ ] 1.3 `mage lint` clean
- [ ] 1.4 `TestMappingRow_PopulatesBehavior`
- [ ] 1.5 `TestMappingRow_PopulatesAPIKeyEnv`
- [ ] 1.6 `TestComputeEffectiveURL_Anthropic`
- [ ] 1.7 `TestComputeEffectiveURL_OpenAI`
- [ ] 1.8 `TestComputeEffectiveURL_AlreadyCorrect`
- [ ] 1.9 `TestComputeEffectiveURL_EmptyProtocol`
- [ ] 1.10 `TestFamilyMatching_Opus`
- [ ] 1.11 `TestFamilyMatching_NoMatch`
- [ ] 1.12 `TestFamilyMatching_CaseInsensitive`

#### Manual

- [ ] 1.13 Each step shows Behavior label
- [ ] 1.14 Each step shows API Key Env as muted text
- [ ] 1.15 Tooltip shows effective URL
- [ ] 1.16 Family-named mapping shows family badge

### Phase 2: Template + CSS rendering

#### Automated

- [ ] 2.1 `go build ./...` succeeds
- [ ] 2.2 `mage test` passes
- [ ] 2.3 `mage lint` clean
- [ ] 2.4 Regex: each step contains `route-step__behavior`
- [ ] 2.5 Regex: family-named mapping contains `route-card__family`

#### Manual

- [ ] 2.6 Steps show Behavior + API Key Env
- [ ] 2.7 Tooltip shows effective URL
- [ ] 2.8 Family badge visible on family-named mappings
- [ ] 2.9 No visual regressions

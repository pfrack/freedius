# Regex model→mapping matching + protected default catch-all

## Overview

Today `resolveMapping` (`proxy/proxy.go:132`) routes a model only when its name
*exactly* equals a mapping key or contains one of four hardcoded family keywords
(`opus|sonnet|haiku|auto`); everything else 404s. We generalize matching to a
compiled, case-insensitive regex over **all** mapping keys (each auto-wrapped as
`(?i).*key.*`), with `default` exempted as an always-last catch-all that is
auto-injected when missing and protected from deletion. The hardcoded family
routing (and its cosmetic UI badge) is retired.

## Current State Analysis

- `resolveMapping` (`proxy/proxy.go:132-146`): exact `Mappings[model]` lookup,
  then `ExtractFamily` (`proxy/families.go:22`) for 4 keywords, then `default`.
- `default` catch-all already exists via the empty-regex `default` family
  (`proxy/families.go:15`) — but only because the family mechanism still fires.
- `handleDeleteMapping` (`proxy/web/handlers.go:1270`) deletes any mapping
  unconditionally — `default` is deletable today.
- `Mapping` (`config/config.go:75`) has no regex field; we will NOT add one
  (keys are auto-wrapped).
- `ExtractFamily` is also used purely for a cosmetic UI badge
  (`handlers.go:739` → `mappingRow.Family` → `mappings-routing-table.html:59`)
  and has 3 tests. Decision: drop it entirely.
- `applyDefaults` (`config/defaults.go:16`) already injects missing *providers*;
  we mirror that pattern to inject a missing `default` *mapping*.
- Starter (`cmd/freedius/templates/starter.yaml:67-94`) defines `auto` +
  `default`; `auto` becomes redundant and will be removed.

### Key Discoveries:

- A catch-all already existed via the family layer — the genuine new work is
  (a) generalizing loose matching to every key and (b) guaranteeing an
  undeletable `default`.
- `ExtractFamily` is routing-irrelevant for the UI; dropping it is safe and
  cleaner (user decision).
- `buildMatchers` must be separable from `ensureDefaultMapping` so unit tests
  that hand-build a `Config` and expect "no match" are not polluted by an
  injected `default`.

## Desired End State

- Any model name routes to the **most specific** matching mapping key
  (exact `model==key` wins; else the longest key among regex matches, ties
  broken by ascending key name); `default` catches everything else, last.
- A `default` mapping always exists at runtime (injected in-memory if absent)
  and cannot be deleted via API or UI.
- Matching is O(1) exact + a single precompiled regex scan per request.
- No `auto` mapping; the family badge and `ExtractFamily` are gone.

Verification: requesting `claude-OPUS-4` → `opus` mapping; `gpt-4o` → a
user mapping keyed `gpt-4o` (or more specific); an unmatched model → `default`;
`DELETE /v1/mappings/default` → 409 and mapping persists.

## What We're NOT Doing

- Not adding a user-facing `regex` config field (keys are auto-wrapped only).
- Not persisting the injected `default` to disk (in-memory only).
- Not changing `Mapping`'s YAML schema (no new fields).
- Not altering provider resolution or fallback chains.

## Implementation Approach

Replace the exact+family routing with a compiled matcher list stored on
`Config`. At load (and after every runtime mutation) we (1) ensure a `default`
mapping exists, then (2) compile one regex per non-`default` key as
`(?i).*<QuoteMeta(key)>.*`, sorted by key length descending (ties by key
ascending), with a catch-all `default` matcher (`regexp.MustCompile("")`
matches everything) appended last. `resolveMapping` does the exact lookup,
then scans the list. Retire `ExtractFamily`.

## Critical Implementation Details

- **Two-step separation (load vs test correctness)**: `ensureDefaultMapping()`
  (injects a missing `default` mirroring the first mapping by sorted key) is
  called ONLY in the `Load`/`LoadFromBytes` pipeline. `buildMatchers()` compiles
  matchers from whatever keys currently exist and is the reusable step called by
  both `Load` and the runtime mutation handlers. This keeps hand-built `Config`s
  in unit tests (e.g. `TestServeHTTPNeitherMatch`) free of an injected `default`
  so negative "no match" assertions stay valid. `resolveMapping` reads
  `c.matchers`; tests that exercise it on a direct `Config` must call
  `cfg.buildMatchers()` in setup.
- **Double-lock rebuild**: mutation handlers (`handleDeleteMapping` ~1270, the
  add/create paths ~1168/1222) finish their own Lock/Unlock + save, then call
  `cfg.buildMatchers()` (which takes its own Lock). `default` cannot be deleted,
  so it is always present for the runtime rebuild.
- **ReDoS safety**: auto-wrap uses `regexp.QuoteMeta(key)`, so user mapping names
  can never inject catastrophic patterns. No user-supplied regex exists.
- **Most-specific sort**: sort compiled matchers by `len(key)` desc, then `key`
  asc. Exact `model==key` is checked before the scan, so it always wins.

## Phase 1: Compiled matcher core

### Overview

Add the matcher list to `Config` and rewrite `resolveMapping` to use it; retire
`ExtractFamily` from routing.

### Changes Required:

#### 1. Config matcher field + builders

**File**: `config/config.go`

**Intent**: Hold a compiled, ordered matcher list on `Config` and provide
`ensureDefaultMapping()` + `buildMatchers()` (see Critical Implementation
Details). Preserve concurrent access via the existing `RWMutex`.

**Contract**: Add unexported `matchers []mappingMatcher` to `Config`; define
`type mappingMatcher struct { name string; re *regexp.Regexp }`. Add methods:
- `ensureDefaultMapping()`: if `Mappings["default"]` absent, set it to a
  `Mapping` copying `ProviderName`/`ModelString` from the first key (sorted
  ascending); no fallback chain.
- `buildMatchers()`: Lock; for each key != "default" compile
  `regexp.MustCompile(`(?i).*` + regexp.QuoteMeta(key) + `.*`)`; collect; sort by
  `len(key)` desc then `key` asc; append a catch-all
  `mappingMatcher{"default", regexp.MustCompile("")}`; store `c.matchers`.
- `resolveMapping` (in `proxy/proxy.go`) changes in Phase 1.3.

#### 2. Wire build into the load pipeline

**File**: `config/config.go` (`LoadFromBytes` / `loadFromUnmarshaled`)

**Intent**: After `applyDefaults()` and `validate()`, call
`cfg.ensureDefaultMapping()` then `cfg.buildMatchers()` so every loaded config
has matchers and a guaranteed `default`.

**Contract**: Insert the two calls before `return &cfg` in `LoadFromBytes` (and
mirror in `Load` if it shares the path). No signature changes.

#### 3. Rewrite resolveMapping

**File**: `proxy/proxy.go` (`resolveMapping`, lines 132-146)

**Intent**: Replace exact+family logic with exact short-circuit then compiled
scan; drop the `ExtractFamily` call entirely.

**Contract**:
```go
func (d *Dispatcher) resolveMapping(model string) (string, config.Mapping, config.Provider, bool, bool) {
    d.Cfg.RLock()
    defer d.Cfg.RUnlock()
    if m, ok := d.Cfg.Mappings[model]; ok {            // exact wins
        p, pok := d.Cfg.Providers[m.ProviderName]
        return model, m, p, true, pok
    }
    for _, mm := range d.Cfg.Matchers() {              // most-specific first, default last
        if mm.re.MatchString(model) {
            name := mm.name
            m := d.Cfg.Mappings[name]
            p, pok := d.Cfg.Providers[m.ProviderName]
            return name, m, p, true, pok
        }
    }
    return "", config.Mapping{}, config.Provider{}, false, false
}
```
Expose matchers via a `Matchers()` accessor (RLock-free read of the slice
pointer is safe because it is only swapped under `Lock`).

#### 4. Retire ExtractFamily

**File**: `proxy/families.go`, `proxy/web/handlers.go:739-742`,
`proxy/web/types.go` (`mappingRow.Family`), `mappings-routing-table.html:59`,
`proxy/families_test.go`, `proxy/web/handlers_mapping_test.go:100,129`

**Intent**: Remove the family routing/label machinery per the user decision.

**Contract**: Delete `families.go` and `ExtractFamily`; remove `Family` from
`mappingRow` and the badge template line; delete the two dependent tests.

### Success Criteria:

#### Automated Verification:

- `mage test` passes (after dependent tests updated)
- `mage lint` passes
- `go vet ./...` clean

#### Manual Verification:

- `go run ./cmd/freedius` starts; a request for `claude-OPUS-4` routes to `opus`.

**Implementation Note**: After completing this phase and all automated
verification passes, pause here for manual confirmation before proceeding.

---

## Phase 2: Default protection + runtime rebuild

### Overview

Guard `default` from deletion and rebuild matchers after any runtime mutation.

### Changes Required:

#### 1. Delete guard

**File**: `proxy/web/handlers.go` (`handleDeleteMapping`, ~1255-1295)

**Intent**: Reject deletion of the `default` mapping so the catch-all is always
available.

**Contract**: After `pathName` resolves `name`, if `name == "default"` return
`writeJSONError(w, http.StatusConflict, "protected_mapping",
"the default mapping cannot be deleted")` and return (before locking). Keep the
existing rollback-on-save-failure behavior for other names.

#### 2. Rebuild after mutation

**File**: `proxy/web/handlers.go` (add/create paths ~1168, ~1222;
`handleDeleteMapping` after successful save)

**Intent**: Keep `c.matchers` in sync after add/delete.

**Contract**: After a successful mutation + `SaveData`, call
`cfg.buildMatchers()`. (No injection here — `default` is already protected and
present.)

### Success Criteria:

#### Automated Verification:

- `mage test` passes
- New test: `DELETE /v1/mappings/default` → 409 and mapping still present.
- New test: adding a mapping then routing by its key works (matchers rebuilt).

#### Manual Verification:

- In the UI, deleting `default` is refused (see Phase 3 for the button state).

---

## Phase 3: Starter cleanup + TUI

### Overview

Remove the redundant `auto` mapping, fix the stale comment, and show `default`
as protected in the UI.

### Changes Required:

#### 1. Starter template

**File**: `cmd/freedius/templates/starter.yaml` (lines 67-94)

**Intent**: Drop `auto`; keep `default` as the sole catch-all; rewrite the
comment that explained the old family fallthrough.

**Contract**: Remove the `auto:` block; replace the comment (lines 67-73) with
one line: "`default` is the always-present catch-all for any unmatched model."

#### 2. Starter validation test

**File**: `cmd/freedius/main_test.go` (`TestStarterTemplate_FallbackChainOrdering`, ~516-525)

**Intent**: Reflect `auto` removal and the retired family rationale.

**Contract**: Drop `"auto"` from the asserted name list; replace the stale
comment (lines 522-524) with a note that `default` is the injected catch-all.

#### 3. Protected UI state

**File**: `proxy/web/templates/mappings-routing-table.html`,
`proxy/web/templates/mapping-drawer.html`, `proxy/web/templates/mappings.html`
(`confirmDeleteMapping`, ~150)

**Intent**: Mark `default` as protected and prevent delete.

**Contract**: Render a "protected" badge on the `default` row (mirror the
existing badge style). In `confirmDeleteMapping(btn)`, read the row name; if it
is `"default"`, show a small inline message / `return` without setting
`hx-delete`. Also guard the drawer delete control if it exposes one.

### Success Criteria:

#### Automated Verification:

- `mage test` passes (starter test updated)
- New test: `buildMappingRows` marks `default` row as protected (badge/data
  attribute present).

#### Manual Verification:

- Mappings table shows a "protected" badge on `default`; its delete action is
  disabled and explains why.

---

## Phase 4: Comprehensive tests

### Overview

Lock in precedence, case-insensitivity, injection, and guard behavior; preserve
the original family-priority expectation via the new matcher.

### Changes Required:

#### 1. Test suite

**File**: `proxy/proxy_test.go`, `config/config_test.go`,
`proxy/web/handlers_mapping_test.go`, `proxy/web/handlers_write_test.go`

**Intent**: Cover the new behavior; keep `TestServeHTTPFamilyPriority…`
(`proxy_test.go:412`) and `TestServeHTTPFamilyMatchWinsOverUnrelatedExact`
(`:444`) valid under most-specific matching.

**Contract** add cases:
- most-specific wins: keys `gpt` + `gpt-4`; model `gpt-4-x` → `gpt-4`.
- case-insensitive: `CLAUDE-OPUS` → `opus`.
- exact beats regex: model exactly `opus` (when also a substring of another
  key) routes to `opus`.
- default last: unmatched model → `default` (when default present).
- inject: `config.LoadFromBytes` on a mappings set without `default` yields a
  `default` mapping mirroring the first mapping; the source YAML is unchanged on
  disk.
- guard: `DELETE /v1/mappings/default` → 409; mapping persists (Phase 2).
- UI: `default` row flagged protected (Phase 3).
Update any test that hand-builds a `Config` and calls `resolveMapping`/`ServeHTTP`
to call `cfg.buildMatchers()` in setup.

### Success Criteria:

#### Automated Verification:

- `mage test` passes with the new cases
- `mage lint` and `mage govulncheck` pass

#### Manual Verification:

- End-to-end smoke: route several model names (exact, substring, unmatched) and
  confirm correct mapping selection in logs/response.

## Testing Strategy

### Unit Tests:

- precedence (most-specific + exact-beats-regex), case-insensitivity, default
  last, inject-on-load, delete guard, protected UI flag.

### Integration Tests:

- `TestServeHTTPFamilyPriorityIndependentOfYAMLOrder` preserved; add
  most-specific scenario through `ServeHTTP`.

### Manual Testing Steps:

1. Start `freedius`; `curl` a request with model `claude-OPUS-4` → lands on `opus`.
2. Add a mapping keyed `gpt-4o`; request model `gpt-4o-mini` → routes to it.
3. Request a garbage model → routes to `default`.
4. Attempt to delete `default` in UI and via API → refused.

## Performance Considerations

Per-request cost is O(1) exact lookup + one `MatchString` per mapping (small N,
compiled once at load). No per-request compilation. This matches the prior
family-scan cost class.

## Migration Notes

Existing configs without a `default` mapping get one injected **in memory** at
load (mirroring the first mapping's provider/model). The on-disk YAML is never
rewritten. `auto` removal only affects the starter template, not user configs.

## References

- `proxy/proxy.go:132` resolveMapping (rewritten)
- `proxy/families.go:10-29` ExtractFamily (retired)
- `config/config.go:75` Mapping struct (unchanged)
- `config/defaults.go:16` applyDefaults (injection pattern to mirror)
- `proxy/web/handlers.go:1270` handleDeleteMapping (guard added)
- `cmd/freedius/templates/starter.yaml:67-94` auto/default (auto removed)
- `proxy/web/templates/mappings-routing-table.html:59` family badge (removed)
- `proxy_test.go:412,444` family-priority tests (preserved)

## Progress

### Phase 1: Compiled matcher core

#### Automated

- [x] 1.1 Add `matchers`/`mappingMatcher` + `ensureDefaultMapping`/`buildMatchers` to `config/config.go` — 8c5b620
- [x] 1.2 Wire `ensureDefaultMapping`+`buildMatchers` into the load pipeline — 8c5b620
- [x] 1.3 Rewrite `resolveMapping` to exact + compiled scan; add `Matchers()` accessor — 8c5b620
- [x] 1.4 Retire `ExtractFamily` (delete families.go, `mappingRow.Family`, badge, 2 tests) — 8c5b620
- [x] 1.5 `mage test`, `mage lint`, `go vet` pass — 8c5b620

#### Manual

- [ ] 1.6 Start server; `claude-OPUS-4` routes to `opus`

### Phase 2: Default protection + runtime rebuild

#### Automated

- [x] 2.1 Add `default` delete guard (409) in `handleDeleteMapping` — 2c3ab77
- [x] 2.2 Call `buildMatchers()` after add/delete mutations — 2c3ab77
- [x] 2.3 Test: `DELETE /v1/mappings/default` → 409, mapping persists — 2c3ab77
- [x] 2.4 Test: add mapping then route by its key — 2c3ab77
- [x] 2.5 `mage test` passes — 2c3ab77

#### Manual

- [ ] 2.6 Confirm default deletion refused in UI flow

### Phase 3: Starter cleanup + TUI

#### Automated

- [x] 3.1 Remove `auto` from starter.yaml; fix comment — 
- [x] 3.2 Update `TestStarterTemplate_FallbackChainOrdering` (drop `auto`, new comment) — 
- [x] 3.3 Render protected badge + disable delete for `default` (table + drawer) — 
- [x] 3.4 Test: `default` row flagged protected — 
- [x] 3.5 `mage test` passes — 

#### Manual

- [ ] 3.6 Verify protected badge + disabled delete in UI

### Phase 4: Comprehensive tests

#### Automated

- [ ] 4.1 Add most-specific / case-insensitive / exact-beats-regex / default-last tests
- [ ] 4.2 Add inject-on-load test (in-memory, disk unchanged)
- [ ] 4.3 Update hand-built `Config` tests to call `buildMatchers()`
- [ ] 4.4 `mage test`, `mage lint`, `mage govulncheck` pass

#### Manual

- [ ] 4.5 End-to-end smoke across exact/substring/unmatched models

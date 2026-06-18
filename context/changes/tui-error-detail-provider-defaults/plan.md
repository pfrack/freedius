---
date: 2026-06-18T12:00:00+02:00
planner: opencode
git_commit: ba9725e
branch: main
repository: pfrack/freedius
topic: "TUI — Error Detail Panel + Provider-Level Defaults"
tags: [implementation, tui, error-display, provider-defaults, config-editing, bubble-tea]
status: planned
last_updated: 2026-06-18
last_updated_by: opencode
---

# TUI — Error Detail Panel + Provider-Level Defaults Implementation Plan

## Overview

Two independent TUI enhancements: (1) an error detail panel on the Requests tab — select a log entry with j/k and press Enter to see full `RequestEvent` metadata (error message no longer truncated to 80 chars), and (2) provider-level defaults (`base_url`, `api_key_env`) stored in a top-level `providers:` YAML section, editable via e/a/d on the Providers tab, with per-entry-wins override semantics.

## Current State Analysis

### Error detail

- Error messages are **already captured** in `RequestEvent.ErrorMessage` and `ErrorType` via `EventBusMiddleware` (`proxy/proxy.go:477-478`) reading `X-Freedius-Error-*` response headers set by `writeErrorJSON` (`proxy/proxy.go:290-291`) and `writeAnthropicError` (`proxy/errors.go:34-35`).
- The Requests tab (`proxy/tui/views.go:33-88`) renders each entry as one line, truncating the error message to 80 chars at line 74: `truncate(e.ErrorMessage, 80)`.
- There is **no request selection cursor** on the Requests tab — no `requestCursor` field on `Dashboard`, no j/k binding for Requests, no expand/detail view.
- The `Dashboard` struct (`proxy/tui/model.go:67-89`) has no `showDetail` or `selectedRequest` fields.

### Provider defaults

- Provider-level defaults exist only in generated code: `knownProviderDefaults` at `config/providers_gen.go:21-35` (compiled-in Go map, not user YAML). Only `nim` has a `BaseURL` default; `anthropic`, `go`, `zen` have `APIKeyEnv` defaults; `openai`, `mix`, `custom` have no defaults.
- The `Config` struct (`config/config.go:16-19`) has only `Models` and `Mappings` — no `Providers` field.
- `applyEntryDefaults` (`config/providers_gen.go:71-95`) does a blank-fill merge: if user didn't set `BaseURL`/`APIKeyEnv` on an entry, fill from `knownProviderDefaults`. No user provider defaults tier exists.
- Per-entry e/a/d editing on the Config tab (`proxy/tui/model.go:393-535`) already works — this stays unchanged.
- The Providers tab (`proxy/tui/views.go:90-115`) is a read-only summary table — no cursor, no selection, no editing.

### Key patterns to follow

- **Cursor pattern**: `configCursor` on Config tab (`model.go:87`) — incremented/decremented with j/k, bounded by `collectAllModels()` length, highlighted with `activeTabStyle` in `renderConfigTab` (`views.go:124-127`). Requests and Providers tabs get analogous cursors.
- **Form pattern**: `formMode` constants (`styles.go:61-65`), `formFields []textinput.Model`, `formKind` determines which map gets mutated (`"model"` vs `"mapping"`), `submitForm()` then `config.Save(cfgPath)` then `resetForm()`. Provider forms reuse the same `updateFormFocus()`/`handleFormKeyPress` infrastructure but with `formKind = "provider"` and a smaller field set (3 fields instead of 7).
- **Marshal pattern**: `config/config.go:210-230` — clones the config, restores `OriginalProvider` for alias entries (zen to mix, go to mix, custom to mix), clears `OriginalProvider`, marshals with `yaml.Marshal`. The new `Providers` map needs analogous clone logic.
- **Alias lesson**: `lessons.md:15-19` — `custom` rewrites to `mix` in Phase A (before defaults lookup), `zen`/`go` rewrite in Phase B (after inheriting defaults). Provider defaults editor must handle aliases: if user sets defaults for `zen`, the runtime `Provider` field is `mix`, but the YAML must store `zen`.

## Desired End State

After this plan is complete:

- **Error detail panel**: On the Requests tab, j/k navigates a selection cursor. Pressing Enter on an entry with `Status >= 400` opens a detail panel showing all `RequestEvent` fields (timestamp, status, model, provider, latency, matched provider/model, error type, full error message, request ID). Pressing Esc closes the panel and returns to the list. Pressing Enter on a non-error entry (Status < 400) is a no-op.
- **Provider-level defaults**: `freedius.yaml` gains a top-level `providers:` section (parallel to `models:` and `mappings:`). Each key is a provider name (e.g., `anthropic`, `nim`, `openai`), value is `{base_url, api_key_env}`. When loading config, user provider defaults are applied as a blank-fill tier above generated defaults but below per-entry values. Provider defaults are editable in the TUI: Providers tab gains a cursor (j/k), e opens a 3-field edit form, a opens a 3-field add form, d deletes a provider default (reverting to generated defaults).
- **Per-entry-wins override semantics**: When a per-entry `base_url`/`api_key_env` is explicitly set, it always takes priority over the provider default.
- **Live effect**: Provider default changes take effect immediately for new requests without restart (the `*config.Config` pointer is shared between TUI goroutine and proxy dispatcher).
- **Backward compatibility**: Existing `freedius.yaml` files without a `providers:` section load and behave identically.

## What We're NOT Doing

- **No full request/response body capture** — error detail panel shows existing metadata only.
- **No request body logging** — privacy NFR (`proxy/proxy.go:1-5`) remains intact.
- **No detail expansion for successful requests** — Enter on a 2xx entry is a no-op.
- **No per-entry editing changes** — the existing e/a/d on the Config tab stays exactly as-is.
- **No provider-level `anthropic_version` or `protocol` defaults** — provider defaults are limited to `base_url` and `api_key_env`.
- **No dynamic provider health data** — Providers tab remains a config viewer/editor, not a live monitoring surface.
- **No config file hot-reload from disk** — changes apply via `config.Save()`; external file edits not picked up until restart.

## Implementation Approach

1. **Two independent features, ordered by dependency**: Error detail panel (Phase 1) is pure TUI display. Provider defaults (Phases 2-3) span config layer and TUI.
2. **Cursor pattern reuse**: `configCursor` pattern copied for `requestCursor` and `providerCursor`. Same j/k binding, same `activeTabStyle` highlighting, same bounds checking.
3. **Form infrastructure reuse**: Provider editing reuses existing form state machine (`formMode`, `formFields`, `formFocus`, `fieldErrors`). New form modes added to enum; existing `handleFormKeyPress`, `updateFormFocus`, `resetForm` work unchanged.
4. **Config layer remains backward-compatible**: `providers:` YAML section with `omitempty` means existing configs load unchanged. `applyDefaults` gains one new step — `applyEntryDefaults` untouched.
5. **No generated code changes**: `providers_gen.go` is not modified. User provider defaults applied in `applyDefaults()` (in `defaults.go`) before `applyEntryDefaults`.

## Critical Implementation Details

- **applyDefaults merge order**: User provider defaults applied BEFORE `applyEntryDefaults`. `applyEntryDefaults` does blank-fill only, so user defaults take priority: if user sets `providers.anthropic.base_url`, all blank per-entry `base_url` fields get filled with it, then `applyEntryDefaults` fills any remaining blanks from `knownProviderDefaults`. Per-entry explicitly-set values win because neither tier overwrites non-empty fields.
- **Provider alias handling in Providers map**: User YAML stores provider names as user-facing (e.g., `zen`). Runtime `m.Provider` is `mix` after alias rewrite. When looking up `c.Providers[m.Provider]`, key would be `"mix"` — but user stored under `"zen"`. Solution: `applyDefaults()` looks up user provider defaults by `m.OriginalProvider` (if set) first, falls back to `m.Provider`. TUI provider editor stores/edits by `OriginalProvider` name.
- **Marshal alias round-trip for Providers**: `OriginalProvider` is only on `Model`, not on provider map keys. `Providers` map keys are user-facing names (e.g., `zen`) and never rewritten — no alias restoration needed in `Marshal()`. Validation of provider names in `Providers` map must accept alias names (zen, go, custom).

## Phase 1: Request Selection Cursor + Error Detail Panel

### Overview

Add j/k cursor navigation to the Requests tab and an Enter-to-expand detail panel showing full `RequestEvent` metadata for error entries. Pure TUI display change.

### Changes Required

#### 1. Dashboard struct — add request selection state

**File**: `proxy/tui/model.go`

**Intent**: Track which request is selected and whether the detail panel is open.

**Contract**:
- Add `requestCursor int` field — index into `d.eventLog.all()`, default 0
- Add `showDetail bool` field — true when the detail panel is visible, default false

#### 2. Dashboard handleTabModeKeyPress — add Requests tab bindings

**File**: `proxy/tui/model.go`

**Intent**: j/k moves cursor on Requests tab, Enter opens detail panel for error entries, Esc closes.

**Contract**:
- Before the existing switch in `handleTabModeKeyPress`, check if `showDetail` and msg is `"esc"` — close panel
- Add j/k cases for `activeTab == tabRequests`: decrement/increment `requestCursor`, clamp to `[0, len(all)-1]`
- Add `"enter"` case for `activeTab == tabRequests`: get entry at cursor; if `Status >= 400`, set `showDetail = true`

#### 3. Dashboard View() — route to detail panel or normal tab

**File**: `proxy/tui/model.go`

**Intent**: When `showDetail` is true on Requests tab, render detail panel instead of list.

**Contract**:
- In `View()`, before `switch d.activeTab`, check `d.activeTab == tabRequests && d.showDetail` — render `renderDetailPanel(d, ev, width, bodyHeight)`

#### 4. Detail panel rendering

**File**: `proxy/tui/views.go`

**Intent**: A bordered detail panel showing all RequestEvent fields with full (untruncated) error message.

**Contract**:
- `func renderDetailPanel(d *Dashboard, ev proxy.RequestEvent, width, height int) string`
- Bordered box titled "Error Detail". Fields displayed:
  - Status (color-coded), Timestamp (full), Model, Provider, Matched Provider, Matched Model, Latency
  - Error Type, Error Message (full, wrapped to panel width), Request ID
- Footer: "Esc=Close"
- Uses `windowStyle` border, `configKeyStyle` labels, `configValueStyle` values, `statusErrorStyle` for error message
- No scrolling — detail panel shows what fits in the available height

#### 5. Requests tab rendering — cursor highlight

**File**: `proxy/tui/views.go`

**Intent**: When no detail panel, highlight the selected entry row.

**Contract**:
- `renderRequestsTab` gains `cursor int` parameter
- When `i == cursor`, apply `activeTabStyle` (Bold + Underline) to the entire line
- When cursor is outside visible window, adjust `start` to include it

### Success Criteria

#### Automated Verification

- Unit tests: `TestDashboard_Update_RequestCursor` — j/k navigation, bounds clamping
- Unit tests: `TestDashboard_Update_DetailPanel` — Enter opens panel on error, no-op on success, Esc closes
- Unit tests: `TestRenderDetailPanel_Fields` — all RequestEvent fields appear in output
- All existing TUI tests pass: `go test ./proxy/tui/ -v`
- Compiles: `go build ./proxy/tui/`
- Linting: `go vet ./proxy/tui/`

#### Manual Verification

- `freedius tui` — send error request — see red entry. j/k navigates cursor. Enter opens detail panel with full error message. Esc closes. Enter on green entry does nothing.

---

## Phase 2: Provider Defaults — Config Layer

### Overview

Add `Providers` map to `Config` struct. Wire merge into `applyDefaults()` (user defaults before generated, per-entry-wins). Update `Marshal()`, `validate()`.

### Changes Required

#### 1. Config struct — add ProviderDefaults type and Providers field

**File**: `config/config.go`

**Intent**: Extend config schema for user-defined provider-level defaults.

**Contract**:
- New exported type: `type ProviderDefaults struct { BaseURL string; APIKeyEnv string }` with `yaml:"base_url,omitempty"` and `yaml:"api_key_env,omitempty"` tags
- Add `Providers map[string]ProviderDefaults` field to `Config` with `yaml:"providers,omitempty"` tag

#### 2. applyDefaults — merge user provider defaults before generated

**File**: `config/defaults.go`

**Intent**: Apply user provider defaults as a blank-fill tier between per-entry and generated.

**Contract**:
- Modify `func (c *Config) applyDefaults()`:
  - Before `applyEntryDefaults(m)` call, look up user defaults via `c.Providers`:
    - Key: `m.OriginalProvider` if set, else `m.Provider`
    - If found, fill `m.BaseURL` from `pd.BaseURL` only if `m.BaseURL == ""`
    - Fill `m.APIKeyEnv` from `pd.APIKeyEnv` only if `m.APIKeyEnv == ""`
- `applyEntryDefaults()` runs AFTER user defaults (fills remaining blanks from generated defaults)
- Same for both `c.Models` and `c.Mappings` loops

#### 3. validate — validate Providers entries

**File**: `config/config.go`

**Intent**: Provider names and field values must pass validation.

**Contract**:
- In `validate()`, loop over `c.Providers`:
  - Provider name must be in `KnownProviders` (aliases zen/go/custom accepted)
  - If `pd.BaseURL != ""`, it must be valid HTTP/HTTPS URL
  - `pd.APIKeyEnv` must not contain CR, LF, or `=`

#### 4. Marshal — include Providers in YAML output

**File**: `config/config.go`

**Intent**: Serialize the Providers map alongside Models and Mappings.

**Contract**:
- In `Marshal()`, add `clone.Providers` initialization and copy entries
- No alias restoration needed — provider names in Providers map are user-facing, never rewritten

#### 5. ProviderInfo — verify accessible for form validation

**File**: `config/config.go` (no change needed)

**Intent**: `config.ProviderInfo()` already exists and is imported by `proxy/tui/model.go` (used at line 476). Provider form in Phase 3 calls it.

### Success Criteria

#### Automated Verification

- Unit tests: `TestConfig_ProvidersRoundTrip` — Load-Marshal-Load semantic equality
- Unit tests: `TestConfig_ProviderDefaultsMerge` — user default wins over generated
- Unit tests: `TestConfig_ProviderDefaultsPerEntryWins` — per-entry wins over user default
- Unit tests: `TestConfig_ProvidersValidation` — invalid name, bad URL, bad env var
- Unit tests: `TestConfig_SaveProviders` — save creates backup, round-trip
- Existing config tests pass: `go test ./config/ -v`
- Compiles: `go build ./config/`
- Linting: `go vet ./config/`

#### Manual Verification

- Create `freedius.yaml` with `providers:` section, send request with blank base_url entry — verify proxy routes to provider default URL. Add per-entry override — verify it takes priority.

---

## Phase 3: Provider Defaults — TUI Editor

### Overview

Rework the Providers tab from read-only to editable. Add cursor (j/k), e=edit, a=add, d=delete for provider defaults. Reuse existing form infrastructure with new `formProviderEdit`/`formProviderAdd` modes.

### Changes Required

#### 1. Dashboard struct — add provider cursor

**File**: `proxy/tui/model.go`

**Intent**: Track which provider row is selected.

**Contract**: Add `providerCursor int` field, default 0.

#### 2. Add form mode constants

**File**: `proxy/tui/styles.go`

**Intent**: New form modes for provider editing.

**Contract**: Add `formProviderEdit = 5` and `formProviderAdd = 6`.

#### 3. Dashboard handleTabModeKeyPress — Providers tab bindings

**File**: `proxy/tui/model.go`

**Intent**: j/k moves cursor, e/a/d open provider forms.

**Contract**:
- Extend j/k binding for `activeTab == tabProviders`: decrement/increment `providerCursor`, clamp to list length
- Add "e" case: get selected provider from `collectProvidersFromConfig`, call `openProviderEditForm(name)`
- Add "a" case: call `openProviderAddForm()`
- Add "d" case: set `formKind = "provider"`, `formEntryName = providerName`, `formMode = formDeleteConfirm`

#### 4. Dashboard — openProviderEditForm, openProviderAddForm

**File**: `proxy/tui/model.go`

**Intent**: Open 3-field form for provider defaults.

**Contract**:
- `openProviderEditForm(providerName string)`: 3 fields — [0] provider name (pre-filled, read-only), [1] base_url (pre-filled), [2] api_key_env (pre-filled). `formKind = "provider"`, `formMode = formProviderEdit`
- `openProviderAddForm()`: 3 fields — all empty, fully editable. `formKind = "provider"`, `formMode = formProviderAdd`

#### 5. Dashboard fieldLabel — extend for provider fields

**File**: `proxy/tui/model.go`

**Intent**: Return correct labels for 3-field provider form.

**Contract**: When `formKind == "provider"`, return `"provider"`, `"base_url"`, `"api_key_env"` for indices 0-2.

#### 6. Dashboard handleFormKeyPress — skip picker for provider forms

**File**: `proxy/tui/model.go`

**Intent**: The provider field in provider forms is a text input, not a picker. Disable picker trigger.

**Contract**: In the `"enter"` handler for `fieldLabel == "provider"`, add `&& d.formKind != "provider"` guard.

#### 7. Dashboard validateForm — provider field validation

**File**: `proxy/tui/model.go`

**Intent**: Validate provider form fields.

**Contract**: For `formKind == "provider"`:
- [0] non-empty, in `config.KnownProviders`
- [1] if non-empty, valid URL; required if `ProviderInfo(name).requiresBaseURL`
- [2] no CR, LF, or `=`

#### 8. Dashboard submitForm — provider form submission

**File**: `proxy/tui/model.go`

**Intent**: Write provider defaults to `d.config.Providers` and save.

**Contract**: For `formKind == "provider"`: build `config.ProviderDefaults` from fields [1] and [2]. Edit mode: `delete(old)` then `insert(new)`. Add mode: `insert(new)`. Call `d.config.Save(d.cfgPath)`. On error, set `formError`.

#### 9. Providers tab rendering — cursor highlight

**File**: `proxy/tui/views.go`

**Intent**: Highlight selected provider row.

**Contract**: `renderProvidersTab` gains `cursor int` parameter. When `i == cursor`, apply `activeTabStyle` to provider name.

#### 10. Delete confirmation for providers

**File**: `proxy/tui/model.go`

**Intent**: Delete confirmation already handled generically. When `formMode == formDeleteConfirm` and user presses `"y"`: delete `d.config.Providers[d.formEntryName]`, call `Save()`, `resetForm()`.

**Contract**: Extend `handleDeleteConfirmKeyPress` to handle `formKind == "provider"`: `delete(d.config.Providers, d.formEntryName)`.

### Success Criteria

#### Automated Verification

- Unit tests: `TestDashboard_Update_ProviderCursor` — j/k navigation, bounds
- Unit tests: `TestDashboard_Update_ProviderForm` — edit/add/submit/cancel/delete
- Unit tests: `TestDashboard_ProviderFormValidation` — invalid name, missing URL, bad env
- Unit tests: `TestDashboard_ProviderSave` — save to Providers map, delete removes entry
- All existing TUI tests pass: `go test ./proxy/tui/ -v`
- Compiles: `go build ./proxy/tui/`
- Linting: `go vet ./proxy/tui/`

#### Manual Verification

- Providers tab: j/k cursor, e opens edit form (pre-filled, provider name read-only), edit and Enter saves. a opens add form (all fields editable). d+confirm deletes. Send request with blank base_url — verifies provider default picked up.

---

## Phase 4: Integration, Polish, and Test Coverage

### Overview

Wire full pipeline end-to-end. Ensure all tests pass, linting clean.

### Changes Required

#### 1. Wire new Dashboard fields in NewDashboard

**File**: `proxy/tui/model.go`

**Intent**: Initialize new cursor and state fields.

**Contract**: In `NewDashboard()`, explicitly set `requestCursor = 0`, `showDetail = false`, `providerCursor = 0`.

#### 2. Ensure View() handles all new states

**File**: `proxy/tui/model.go`

**Intent**: Verify all render paths work.

**Contract**: `showDetail` check before switch. `formMode != formNone` check already covers `formProviderEdit` and `formProviderAdd`. Providers tab passes `providerCursor` to `renderProvidersTab`. Requests tab passes `requestCursor` to `renderRequestsTab`.

#### 3. Test coverage

**Files**: `proxy/tui/model_test.go`, `config/config_test.go`

**Intent**: Fill coverage for all new code paths.

**Contract**:
- TUI tests: request cursor navigation, detail panel open/close, provider cursor navigation, provider form lifecycle, provider validation, provider save, provider delete
- Config tests: providers round-trip, merge priority, validation

#### 4. Full suite verification

**Intent**: Ensure no regressions.

**Contract**:
- `go test -race ./...` — all tests pass
- `go vet ./...` — no issues
- `go build -o freedius .` — compiles

### Success Criteria

#### Automated Verification

- Full build: `go build -o freedius .`
- All tests: `go test -race ./...`
- CI check: `go vet ./... && go test ./... && go build .`
- Module graph: `go mod tidy && go mod verify`

#### Manual Verification

1. Error detail: send error request in TUI, j/k navigate, Enter opens detail panel with full error, Esc closes
2. Provider defaults edit: Providers tab, e on provider, edit base_url, Enter saves, file updated
3. Provider defaults add: a on Providers tab, fill fields, Enter saves
4. Provider defaults delete: d on provider, y confirms, entry removed
5. Provider defaults effect: send request with blank base_url entry using edited provider — picks up provider default
6. Per-entry override: set base_url on entry AND on provider — entry value wins
7. Backward compat: start TUI with config that has no `providers:` section — no errors, all existing features work

---

## Testing Strategy

### Unit Tests

- `proxy/tui/model_test.go`: RequestCursor (j/k nav, bounds), DetailPanel (Enter opens on error, no-op on success, Esc closes), ProviderCursor (j/k nav, bounds), ProviderForm (edit/add/submit/cancel/delete lifecycle, validation)
- `config/config_test.go`: ProvidersRoundTrip, ProviderDefaultsMerge, ProviderDefaultsPerEntryWins, ProvidersValidation, SaveProviders

### Integration Tests

- None required — both features are display/config logic without cross-system interactions.

### Manual Testing Steps

1. Error detail panel: full lifecycle (j/k select, Enter expand, Esc close, no-op on success)
2. Provider defaults edit: open, edit, save, verify in file
3. Provider defaults add: open, fill, save, verify routing
4. Provider defaults delete: confirm, verify entry removed
5. Override semantics: per-entry > provider default > generated default
6. Backward compatibility: existing config loads without `providers:` section

## Performance Considerations

- Detail panel rendering is a one-shot string build from existing ring buffer data — no new allocations beyond the panel string.
- `applyDefaults` now does one additional map lookup per Model/Mapping entry — O(1) per entry, negligible.
- Provider cursor navigation builds `collectProvidersFromConfig` list (~7 items) on each j/k press — trivial.
- No new dependencies or allocations in the hot request path.

## Migration Notes

- No existing data migration needed — `providers:` YAML section is `omitempty`, absent from existing configs.
- Users with existing `freedius.yaml` can add a `providers:` section manually or via the TUI.
- `NewDashboard()` signature unchanged — new fields are internal state.
- Provider alias names (zen, go, custom) work as provider keys in the `providers:` section.

## References

- Frame brief: `context/changes/tui-error-detail-provider-defaults/frame.md`
- Original TUI plan: `context/changes/tui-dashboard/plan.md`
- TUI config setup plan: `context/changes/tui-config-setup/plan.md`
- Lessons: `context/foundation/lessons.md:15-19` — alias rewrite (zen/go/custom to mix)
- `proxy/tui/model.go:67-89` — Dashboard struct
- `proxy/tui/model.go:87` — configCursor pattern
- `proxy/tui/model.go:393-535` — edit/add/submit form lifecycle
- `proxy/tui/views.go:33-88` — renderRequestsTab (error truncation at line 74)
- `proxy/tui/views.go:90-115` — renderProvidersTab
- `proxy/tui/views.go:117-144` — renderConfigTab (cursor highlighting)
- `proxy/tui/views.go:252-301` — renderForm
- `proxy/tui/styles.go:8-12` — activeTabStyle definition
- `proxy/tui/styles.go:61-65` — form mode constants
- `proxy/proxy.go:477-478` — EventBusMiddleware error header reads
- `proxy/proxy.go:290-291` — writeErrorJSON error headers
- `proxy/errors.go:34-35` — writeAnthropicError error headers
- `config/config.go:16-19` — Config struct
- `config/config.go:210-230` — Marshal (clone and alias restore)
- `config/config.go:235-258` — Save (validate, backup, write)
- `config/providers_gen.go:21-35` — knownProviderDefaults
- `config/providers_gen.go:71-95` — applyEntryDefaults
- `config/defaults.go:25-32` — applyDefaults loop
- Investigation tasks: ses_1243d8e5cffeRrHl0VnAAYOm0C, ses_1243d7de3ffeCEnZ6j2w6GteQv, ses_1241d25c4ffeLlam8f5xvYyN2z

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles.

### Phase 1: Request Selection Cursor + Error Detail Panel

#### Automated

- [ ] 1.1 Unit tests: TestDashboard_Update_RequestCursor
- [ ] 1.2 Unit tests: TestDashboard_Update_DetailPanel
- [ ] 1.3 Unit tests: TestRenderDetailPanel_Fields
- [ ] 1.4 All existing TUI tests pass: `go test ./proxy/tui/ -v`
- [ ] 1.5 Compiles: `go build ./proxy/tui/`
- [ ] 1.6 Linting: `go vet ./proxy/tui/`

#### Manual

- [ ] 1.7 Error detail panel: j/k navigate, Enter expand, Esc close, no-op on success

### Phase 2: Provider Defaults — Config Layer

#### Automated

- [ ] 2.1 Unit tests: TestConfig_ProvidersRoundTrip
- [ ] 2.2 Unit tests: TestConfig_ProviderDefaultsMerge
- [ ] 2.3 Unit tests: TestConfig_ProviderDefaultsPerEntryWins
- [ ] 2.4 Unit tests: TestConfig_ProvidersValidation
- [ ] 2.5 Unit tests: TestConfig_SaveProviders
- [ ] 2.6 All existing config tests pass: `go test ./config/ -v`
- [ ] 2.7 Compiles: `go build ./config/`
- [ ] 2.8 Linting: `go vet ./config/`

#### Manual

- [ ] 2.9 Provider defaults merge: user default wins over generated default
- [ ] 2.10 Per-entry override takes priority over provider default
- [ ] 2.11 Providers section survives Save round-trip

### Phase 3: Provider Defaults — TUI Editor

#### Automated

- [ ] 3.1 Unit tests: TestDashboard_Update_ProviderCursor
- [ ] 3.2 Unit tests: TestDashboard_Update_ProviderForm
- [ ] 3.3 Unit tests: TestDashboard_ProviderFormValidation
- [ ] 3.4 Unit tests: TestDashboard_ProviderSave
- [ ] 3.5 All existing TUI tests pass: `go test ./proxy/tui/ -v`
- [ ] 3.6 Compiles: `go build ./proxy/tui/`
- [ ] 3.7 Linting: `go vet ./proxy/tui/`

#### Manual

- [ ] 3.8 Provider edit: e opens form, edit, Enter saves
- [ ] 3.9 Provider add: a opens form, fill, Enter saves
- [ ] 3.10 Provider delete: d + y confirms, entry removed
- [ ] 3.11 Provider default picked up by blank entry

### Phase 4: Integration, Polish, and Test Coverage

#### Automated

- [ ] 4.1 Full build: `go build -o freedius .`
- [ ] 4.2 All tests: `go test -race ./...`
- [ ] 4.3 CI check: `go vet ./... && go test ./... && go build .`
- [ ] 4.4 Module graph clean: `go mod tidy && go mod verify`

#### Manual

- [ ] 4.5 Error detail panel full lifecycle in running TUI
- [ ] 4.6 Provider defaults full lifecycle (edit/add/delete) + routing verification
- [ ] 4.7 Backward compatibility: config without providers section loads cleanly

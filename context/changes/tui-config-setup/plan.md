---
date: 2026-06-19T09:00:00+02:00
planner: kiro
git_commit: 996b96d09a674f2bcdcdf70ff8646f342e35b8bf
branch: tui-dashboard
repository: pfrack/freedius
topic: "Extend TUI with mapping/model setup and plain error display"
tags: [implementation, tui, config-editing, error-display, bubble-tea, yaml]
status: planned
last_updated: 2026-06-19
last_updated_by: kiro
---

# Extend TUI with Mappings/Models Setup and Plain Error Display — Implementation Plan

## Overview

Add interactive config editing (CRUD for mappings and models) and plain error message display to the freedius TUI dashboard. Users will edit config entries through modal overlay forms with a provider list picker, with changes saved to `freedius.yaml` via new YAML serialization. Simultaneously, error messages from upstream failures will be captured in `RequestEvent` and displayed alongside status codes in the requests tab.

## Current State Analysis

### What exists today

- **TUI Dashboard** (`proxy/tui/model.go:58-70`): 3-tab layout (Requests, Providers, Config) using Bubble Tea v2. Flat state machine model — no nested models, no form/input components.
- **Event Bus** (`proxy/eventbus.go:13-22`): `RequestEvent` carries `Status int` but no error message or error type field.
- **EventBusMiddleware** (`proxy/proxy.go:448-474`): Emits after `next.ServeHTTP()` — reads status code from `wroteHeaderResponseWriter`, but doesn't capture error body text.
- **Config System** (`config/config.go:30-55`): `Load()` pipeline (read → unmarshal → defaults → validate). No Save/Marshal path exists. `Model.OriginalProvider` (`yaml:"-"`) preserves pre-alias-rewrite provider name.
- **Provider definitions** (`providers.yaml:32-70`): 7 known providers with behaviors, defaults, and alias rewrites. Generated code at `config/providers_gen.go` and `proxy/adapters_gen.go`.
- **Error generation** (`proxy/proxy.go:269-301`, `proxy/errors.go:29-43`): Two error envelope formats — freedius-format (`writeErrorJSON`) and Anthropic-format (`writeAnthropicError`). Upstream error body (256 bytes) is read in `translateUpstreamError` (`proxy/errors.go:52-54`) but discarded after response write.
- **Dependencies**: `charm.land/bubbletea/v2`, `charm.land/lipgloss/v2`, `goccy/go-yaml`. No `charm.land/bubbles/v2` (input widgets).

### Key discoveries

- The original TUI plan (`context/changes/tui-dashboard/plan.md:61`) explicitly deferred config editing: "No config editing in the TUI — the Config tab is read-only display." This was scope discipline, not a technical blocker.
- No form/input widgets exist anywhere in the codebase. Adding text inputs requires `go get charm.land/bubbles/v2`.
- `wroteHeaderResponseWriter` (`proxy/proxy.go:335-365`) already captures `code int` and `wroteHeader bool` — a natural extension point for error body capture.
- Provider aliases (zen→mix, go→mix, custom→mix) rewrite at load time; `OriginalProvider` preserves the user's intent. Config editing must use `OriginalProvider` when writing back.
- The TUI's `runTUI()` (`tui.go:118`) already discards `checkRequiredEnvVars` errors — the TUI is permissive about missing env vars at startup.

### What's missing

- No YAML serialization for `Config` — marshal round-trip must be built from scratch.
- No text input widgets — missing `charm.land/bubbles/v2` dependency.
- No list-picker component for selecting from 7 known providers.
- No modal overlay rendering infrastructure in the TUI.
- No error message capture in `RequestEvent` or `EventBusMiddleware`.
- No error message column in `renderRequestsTab`.
- No form validation-to-field mapping.

## Desired End State

After this plan is complete:

- **Error messages visible in TUI**: The requests tab shows an error message and error type alongside status codes. Users can see at a glance *what* went wrong (e.g., "no configured mapping for model 'gpt-4'"), not just the status code.
- **Config editing via modal overlay**: Pressing `e` on the Config tab opens a modal form showing the selected mapping/model's fields. Users navigate fields with Tab, edit with keyboard, and confirm with Enter or cancel with Esc.
- **Provider list picker**: Selecting the `provider` field opens a scrollable list of the 7 known providers (anthropic, custom, go, mix, nim, openai, zen) with context info (behavior, default env var, requires base_url).
- **Full CRUD**: Users can add new mappings/models, edit existing ones, and delete entries (with confirmation dialog).
- **Save to disk with backup**: On confirm, config is validated, serialized to YAML, and written to `freedius.yaml`. The previous file is backed up as `freedius.yaml.bak` on each save.
- **In-memory live update**: Mutations to the in-memory `*config.Config` pointer are visible to the running proxy dispatcher immediately — no restart needed.
- **Inline field validation errors**: If a field fails validation (e.g., invalid base URL), the error message appears next to the problematic field and the form stays open.
- **Error message + type in events**: `RequestEvent` gains `ErrorMessage` and `ErrorType` fields, populated by `EventBusMiddleware` for all non-2xx responses.

## What We're NOT Doing

- **No web UI for config editing** — the TUI is the sole config editing interface; no separate web dashboard.
- **No config hot-reload from file** — in-memory mutations take effect immediately, but there's no file watcher to pick up external edits.
- **No multi-line text areas for model fields** — single-line `textinput` for all fields; model/config values are short strings.
- **No provider addition beyond KnownProviders** — the 7 providers from `providers.yaml` are the fixed set; no UI for adding custom providers.
- **No config template generation** — `freedius init` is the CLI for that; the TUI edits existing populated configs.
- **No undo/redo stack** — edits are confirmed or cancelled; no multi-step undo.
- **No request log filtering/search** — the error message column is displayed but there's no filter UI for the requests tab.

## Implementation Approach

1. **Bottom-up dependency order**: Error capture first (Phase 1 — small, self-contained change to event pipeline). Then YAML serialization (Phase 2 — pure config package, testable in isolation). Then UI components (Phase 3 — provider picker). Then forms (Phase 4 — modal + text inputs). Then CRUD wiring (Phase 5). Then polish (Phase 6).
2. **Reuse existing patterns**: `EventBusMiddleware` already captures response metadata after `next.ServeHTTP` — extend it rather than creating new hooks. `validateModel()` already validates config entries — call it directly from form submit logic. `wroteHeaderResponseWriter` already tracks status — add error body capture there.
3. **New dependency**: `charm.land/bubbles/v2` provides `textinput.Model` and list components. Added once, used by Phases 3+.
4. **YAML round-trip strategy**: Serialize using `goccy/go-yaml` (already in go.mod). Before marshal, swap `Provider` → `OriginalProvider` for alias entries so `zen`/`go`/`custom` survive round-trip. Respect `omitempty` tags on optional fields. Validated before write.
5. **Modal overlay pattern**: A new `formMode` enum on `Dashboard` (none/add/edit). When active, `Update()` delegates keystrokes to the focused input widget instead of tab-switching. `View()` renders the form as an overlay on top of the dimmed Config tab.

## Critical Implementation Details

- **OriginalProvider round-trip**: When saving, the `Config.Marshal()` function must iterate all entries and replace `Provider` with `OriginalProvider` where they differ before serialization. Skipping this causes user-written `provider: zen` to become `provider: mix` in the file — breaking the config's alias semantics on next load.
- **EventBusMiddleware error capture order**: The middleware fires after `next.ServeHTTP(ww, r)` — at this point the response is fully written. Error text cannot be read from the response body (already flushed). Instead, the `wroteHeaderResponseWriter` or a header-based approach must capture the error message *during* handler execution. The `Dispatcher.writeErrorJSON` already constructs the message string before writing — a response header or a context value is the cleanest conduit.

## Phase 1: Error Message Capture in Event Pipeline

### Overview

Add `ErrorMessage` and `ErrorType` fields to `RequestEvent`, populate them in `EventBusMiddleware` for non-2xx responses, and display them in the requests tab.

### Changes Required

#### 1. RequestEvent struct

**File**: `proxy/eventbus.go`

**Intent**: Add two string fields so error metadata flows through the event bus to the TUI.

**Contract**:
- Add `ErrorMessage string` field — human-readable error description (e.g. "no configured mapping for model 'gpt-4'")
- Add `ErrorType string` field — machine-readable error code (e.g. "no_match", "api_error", "rate_limit_error")
- Both fields zero-value is `""` — existing event consumers (nil-safe) are unaffected

#### 2. EventBusMiddleware error capture

**File**: `proxy/proxy.go`

**Intent**: After `next.ServeHTTP(ww, r)`, when `ww.code >= 400`, extract the error message and type from the response to populate the new `RequestEvent` fields.

**Contract**:
- After `next.ServeHTTP` and status determination, read `X-Freedius-Error-Type` and `X-Freedius-Error-Message` response headers if `ww.code >= 400`
- Populate `ev.ErrorType` and `ev.ErrorMessage` in the `bus.Emit()` call

#### 3. Dispatcher error header injection

**File**: `proxy/proxy.go`

**Intent**: When `Dispatcher.ServeHTTP` writes an error response via `writeErrorJSON`, inject the error code and message as response headers so `EventBusMiddleware` can read them.

**Contract**:
- In `writeErrorJSON`, add `w.Header().Set("X-Freedius-Error-Type", code)` and `w.Header().Set("X-Freedius-Error-Message", message)` before writing the JSON body
- The `writeAnthropicError` calls in `translateUpstreamError` and `freediusErrorHandler` must also set these headers — extract `errType` and `message` values from the Anthropic envelope

#### 4. Requests tab rendering

**File**: `proxy/tui/views.go`

**Intent**: Add an error message column to the requests tab display when errors are present.

**Contract**:
- In `renderRequestsTab`, after the latency column, render `e.ErrorMessage` for entries where `e.Status >= 400`
- Truncate error message to 40 characters with `truncate()`
- Style error messages with `statusErrorStyle` or a new `errorMessageStyle` (dimmed/red)

### Success Criteria

#### Automated Verification

- Unit tests: `go test ./proxy/ -run TestEventBus -race` — verify new fields serialize correctly
- Unit tests: `go test ./proxy/ -run TestMiddleware` — verify headers are set and read correctly
- Unit tests: `go test ./proxy/tui/ -v` — verify new fields render in requests tab
- Compiles: `go build ./...`
- Linting: `go vet ./...`

#### Manual Verification

- Start TUI with `freedius tui`, send a request with an unknown model — see error message like "no configured mapping for model 'unknown'" in requests tab instead of just red "404"

---

## Phase 2: Config YAML Serialization

### Overview

Add `Config.Marshal()` to serialize config back to YAML, including `OriginalProvider` alias recovery and a `Save(path)` method that validates before writing with backup.

### Changes Required

#### 1. Marshal and Save functions

**File**: `config/config.go`

**Intent**: Build the YAML round-trip path — marshal `Config` to bytes and write to disk.

**Contract**:
- `func (c *Config) Marshal() ([]byte, error)` — serializes to YAML bytes
  - Before marshal, clones the Config (or walks in-place) and replaces `m.Provider` with `m.OriginalProvider` where they differ (for alias entries: zen, go, custom)
  - Marshals using `goccy/go-yaml` (already in go.mod)
  - Respects `omitempty` struct tags — empty optional fields are omitted from output
- `func (c *Config) Save(path string) error` — validates, marshals, writes to file
  - Calls `c.validate(path)` before serialization
  - Creates backup: if `path` exists, `os.Rename(path, path+".bak")`
  - Writes with `os.WriteFile(path, data, 0o644)` (same permissions as `freedius init`, `init.go:70`)
  - On write failure, attempts to restore backup

#### 2. Marshal tests

**File**: `config/config_test.go`

**Intent**: Verify YAML round-trip preserves all fields through Load → Marshal → Load cycle.

**Contract**:
- `TestConfig_RoundTrip` — load a config with all field types, marshal, unmarshal result, assert semantic equality
- `TestConfig_MarshalOriginalProvider` — verify `provider: zen` survives round-trip as `zen` (not rewritten to `mix`)
- `TestConfig_MarshalOmitEmpty` — verify `omitempty` fields absent in output when empty
- `TestConfig_SaveBackup` — verify save creates .bak file, restores on write failure

#### 3. Marshal integration with validation

**File**: `config/config.go`

**Intent**: Ensure `Save()` validates before writing, producing errors compatible with the form validation display in Phase 4.

**Contract**:
- `Save()` returns a structured error (or wraps validation errors) so callers can identify which field failed
- Use `fmt.Errorf` wrapping: validation errors from `validateModel` are already formatted with field context

### Success Criteria

#### Automated Verification

- Unit tests pass: `go test ./config/ -v -run "RoundTrip|Marshal|Save"`
- Round-trip test: Load starter.yaml → Marshal → Load again → no semantic changes
- Linting: `go vet ./config/`

#### Manual Verification

- Manual check: write a Go test that creates a `Config`, marshals it, and visually inspect the YAML output for correct formatting and alias preservation

---

## Phase 3: Provider List Picker Component

### Overview

Add `charm.land/bubbles/v2` dependency and build a list-picker component that lets users select from the 7 known providers with context display.

### Changes Required

#### 1. Dependency

**File**: `go.mod`

**Intent**: Add input widget library used by Phases 3-6.

**Contract**: Run `go get charm.land/bubbles/v2@latest` — adds `textinput`, `list`, and other widget packages.

#### 2. Provider picker component

**File**: `proxy/tui/picker.go` (new)

**Intent**: A scrollable list widget that displays the 7 providers from `config.KnownProviders` with context (behavior, default API key env, requires base_url), wrapping a bubbles `list.Model`.

**Contract**:
- `type providerPicker struct` — wraps `list.Model`
- `func newProviderPicker() *providerPicker` — creates list with 7 items:
  - Title: provider name (e.g. "nim", "zen", "openai")
  - Description: behavior + defaults info (e.g. "openai · NVIDIA_NIM_API_KEY · base_url: auto")
- Uses `readKnownProviders()` helper that returns sorted provider names + metadata from `config.KnownProviders`, `config.knownProviderDefaults`, `config.requireBaseURL`, and `providers.yaml` behavior annotations
- `Update(msg tea.Msg)` delegates to `list.Model.Update`
- `View()` renders the list with `windowStyle` borders
- Exposes `SelectedProvider() string` returning the chosen provider name

#### 3. Provider metadata accessor

**File**: `config/config.go` or `config/providers_gen.go`

**Intent**: Expose provider metadata (behavior, defaults) for the picker to display.

**Contract**:
- `func ProviderInfo(name string) (behavior, apiKeyEnv, baseURL string, requiresBaseURL bool)` — returns metadata for a given provider name
- Falls back to empty strings for unknown providers

### Success Criteria

#### Automated Verification

- Compiles with `charm.land/bubbles/v2`: `go build ./...`
- Unit test: `TestProviderPicker_Selection` — select a provider, verify `SelectedProvider()` returns correct name
- Unit test: `TestProviderInfo` — verify returns correct metadata for each of the 7 providers
- Linting: `go vet ./proxy/tui/`

#### Manual Verification

- Not independently testable — picker is visible only when integrated into the form in Phase 4

---

## Phase 4: Model/Mapping Editor Forms (Modal Overlay)

### Overview

Build the modal overlay form infrastructure in the TUI Dashboard: text input fields for all `Model` fields, provider list picker, inline field validation errors, and Enter/Esc for confirm/cancel.

### Changes Required

#### 1. Form model on Dashboard

**File**: `proxy/tui/model.go`

**Intent**: Add fields to `Dashboard` for form state — the active form, its inputs, and focus management.

**Contract**:
- Add to `Dashboard` struct:
  - `formMode int` — enum: `formNone`, `formEdit`, `formAdd` (new constants in `styles.go`)
  - `formFields []textinput.Model` — one textinput per field (name, provider, model, base_url, api_key_env, protocol)
  - `formFocus int` — currently focused field index (0 to len(formFields)-1)
  - `formKind string` — "model" or "mapping" (which map is being edited)
  - `formEntryName string` — the key being edited (for edit mode; empty for add)
  - `fieldErrors map[int]string` — validation error messages per field index
  - `showPicker bool` — true when provider list picker is active (overrides text input)
- New form-specific message types:
  - `type providerSelectedMsg string` — emitted when user picks a provider
  - `type formSubmittedMsg struct{}` — emitted when user confirms the form

#### 2. Form lifecycle: open, navigate, submit, cancel

**File**: `proxy/tui/model.go`

**Intent**: Handle form state transitions in `Update()`.

**Contract**:
- **Open form (edit)**: On `tea.KeyPressMsg` with `"e"` when `activeTab == tabConfig`, populate `formFields` from the selected config entry, set `formMode = formEdit`
- **Open form (add)**: On `"a"` when `activeTab == tabConfig`, create empty `formFields`, set `formMode = formAdd`
- **Focus navigation**: When `formMode != formNone`, `tab`/`shift+tab` cycles `formFocus` between fields instead of switching tabs
- **Provider picker toggle**: When `formFocus` is on the provider field and `Enter` is pressed, set `showPicker = true`
- **Field editing**: Forward non-navigation keystrokes to `formFields[formFocus].Update(msg)` (bubbles textinput handles typing, backspace, etc.)
- **Submit**: On `Enter` when not on provider field and not showing picker, validate all fields, if valid emit `formSubmittedMsg`, if errors set `fieldErrors`
- **Cancel**: On `Esc` when form is active, reset all form fields to zero, set `formMode = formNone`

#### 3. Form rendering (modal overlay)

**File**: `proxy/tui/views.go`

**Intent**: Render the form as a modal overlay in `View()`.

**Contract**:
- `func renderForm(d *Dashboard, width, height int) string` — new rendering function
  - Renders a dimmed background of the Config tab content
  - Overlays a bordered form box centered in the viewport
  - For each field:
    - Label (e.g. "Model:", "Provider:", "Base URL:") using `configKeyStyle`
    - Rendered `formFields[i].View()` for the text input
    - If `showPicker` and this is the provider field, render `picker.View()` instead
    - If `fieldErrors[i]` is set, render error text below the field using `statusErrorStyle`
  - Bottom bar: "Enter=Save  Esc=Cancel  Tab=Next Field  Ctrl+D=Delete"

#### 4. Inline field validation

**File**: `proxy/tui/model.go`

**Intent**: Run config validation and map errors to specific form fields.

**Contract**:
- `func (d *Dashboard) validateForm() map[int]string` — validates current form fields
  - Constructs a temporary `config.Model` from form field values
  - Calls `validateModel` (by constructing a validation context)
  - Maps `validateModel` error messages to field indices:
    - "model" field → index 2
    - "provider" field → index 1
    - "base_url" field → index 3
    - "api_key_env" → index 4
    - "protocol" → index 5
  - Also validates the entry name (index 0) is non-empty and contains no invalid chars

#### 5. Delete confirmation

**File**: `proxy/tui/views.go`

**Intent**: Show a confirmation dialog before deleting a config entry.

**Contract**:
- `func renderDeleteConfirm(d *Dashboard, width int) string` — simple centered confirmation box
  - Text: "Delete {kind} '{name}'? [y/N]"
  - `y` confirms, any other key cancels
- Add delete message handling in `Update()`: on `ctrl+d` when in edit mode, set `formMode = formDeleteConfirm`

### Success Criteria

#### Automated Verification

- Unit tests: `go test ./proxy/tui/ -v -run "Form"` — test form open/close, field navigation, submit with validation errors, cancel
- Unit tests: `TestDashboard_Update_FormLifecycle` — table-driven: open form, edit fields, submit valid, submit invalid, cancel, delete confirm
- Compiles: `go build ./proxy/tui/`
- Linting: `go vet ./proxy/tui/`

#### Manual Verification

- `freedius tui` → press `3` (Config) → press `e` on a mapping → form opens with populated fields
- Tab through fields → each field gains focus cursor
- Press Enter on provider field → provider list picker appears → select with arrow keys + Enter
- Edit fields → press Enter → form submits → Config tab refreshes with new values
- Press Esc → form closes without saving
- Press `ctrl+d` on edit form → delete confirmation → `y` deletes entry

---

## Phase 5: CRUD Operations + Save to Disk

### Overview

Wire form submit/delete actions to config mutation and file save. Add "Add" keybinding on Config tab. Ensure in-memory mutation is visible to the running proxy.

### Changes Required

#### 1. Config mutation on form submit

**File**: `proxy/tui/model.go`

**Intent**: On form confirmation, mutate the in-memory `*config.Config` and write to disk.

**Contract**:
- In `Update()`, on `formSubmittedMsg`:
  - Build `config.Model` from form fields
  - For edit: update `d.config.Models[entryName]` or `d.config.Mappings[entryName]`
  - For add: insert into `d.config.Models[entryName]` or `d.config.Mappings[entryName]`
  - Call `d.config.Save(cfgPath)` — needs config file path stored on Dashboard
  - On save error: show error in a form-level error field (not field-specific), log the detail
  - Reset form state to `formNone`

#### 2. Config mutation on delete

**File**: `proxy/tui/model.go`

**Intent**: On delete confirmation, remove the entry and save.

**Contract**:
- On `"y"` key in delete confirm mode:
  - `delete(d.config.Models, entryName)` or `delete(d.config.Mappings, entryName)`
  - `d.config.Save(cfgPath)`
  - Reset to tab view

#### 3. Config path on Dashboard

**File**: `proxy/tui/model.go`

**Intent**: Dashboard needs the config file path to call `Save()`.

**Contract**:
- Add `cfgPath string` field to `Dashboard` struct
- `NewDashboard()` gains a `cfgPath string` parameter
- `runTUI()` in `tui.go` passes `cfgPath` (already resolved at `tui.go:87`)

#### 4. Dashboard constructor update

**File**: `proxy/tui/model.go`, `tui.go`

**Intent**: Wire the new `cfgPath` parameter through.

**Contract**:
- `NewDashboard(events <-chan proxy.RequestEvent, cfg *config.Config, reg *proxy.Registry, cfgPath string) *Dashboard`
- At `tui.go:147`, change to `tui.NewDashboard(bus.Subscribe(), cfg, registry, cfgPath)`

### Success Criteria

#### Automated Verification

- Unit tests: `TestDashboard_SaveConfig` — mock save, verify file contents
- Unit tests: verify delete removes entry from in-memory maps
- Unit tests: verify add inserts entry into correct map (Models vs Mappings)
- All existing tests pass after signature change: `go test ./proxy/tui/ -v`
- Compiles: `go build ./...`
- Linting: `go vet ./...`

#### Manual Verification

- Edit a mapping → change model field → save → quit TUI → `cat freedius.yaml` → verify change persisted
- Add a new model → save → send a request using that model name → proxy routes correctly
- Delete a model → save → send request using deleted model → get "no match" error
- Check `freedius.yaml.bak` exists after save

---

## Phase 6: Integration, Polish, and Test Coverage

### Overview

Verify the complete pipeline: error display + config editing + save + live proxy routing. Add keyboard shortcut hints to the UI. Fill remaining test coverage gaps.

### Changes Required

#### 1. UI polish

**File**: `proxy/tui/views.go`

**Intent**: Show keyboard shortcuts in the tab bar and form footer.

**Contract**:
- Update `renderTabs` to show hints: `[1] Requests  [2] Providers  [3] Config (e=edit a=add)`
- The form footer already shows `Enter=Save  Esc=Cancel  Tab=Next Field  Ctrl+D=Delete` from Phase 4

#### 2. Keybinding hints on Config tab

**File**: `proxy/tui/views.go`

**Intent**: Show available actions on the Config tab when no form is active.

**Contract**:
- Add a footer line below the config entries: `e=edit selected  a=add new  d=delete selected` (dimmed style)
- The "selected" entry can be tracked by a new `configFocus int` field, or by using the same `formFocus` as a selection cursor in read-only mode

#### 3. Config entry selection cursor

**File**: `proxy/tui/model.go`

**Intent**: Users need to select which entry to edit/delete on the Config tab.

**Contract**:
- Add `configCursor int` field to `Dashboard` — index into `collectAllModels(cfg)` output
- In Config tab mode (no form active), `j`/`k` or `up`/`down` arrow keys move cursor up/down
- Highlight the currently selected entry with `activeTabStyle`
- `e` opens edit form for the selected entry; `d` shows delete confirmation; `a` opens add form

#### 4. Test coverage

**File**: `proxy/tui/model_test.go`, `config/config_test.go`, `proxy/eventbus_test.go`

**Intent**: Fill coverage for all new code paths.

**Contract**:
- `TestDashboard_Update_ConfigCursor` — up/down navigation through config entries
- `TestDashboard_Update_FormLifecycle` — full form lifecycle (open → edit → submit → close)
- `TestDashboard_Update_ProviderPicker` — picker open/select/close
- `TestDashboard_Update_DeleteConfirm` — delete confirmation flow
- `TestConfig_SaveRoundTrip` — full Load → Marshal → Save → Load cycle
- `TestEventBus_ErrorMessage` — verify ErrorMessage and ErrorType populate correctly

#### 5. Integration test

**File**: `main_test.go`

**Intent**: End-to-end test of the full pipeline in TUI mode.

**Contract**:
- `TestTUI_ConfigEditFlow` — start TUI with test config, programmatically simulate key presses: `3` (Config), `j` (select entry), `e` (edit), edit fields, `Enter` (save), verify file changed
- Note: Bubble Tea has programmatic test support via `tea.NewProgram` with test options — this is a functional test, not a rendering test

### Success Criteria

#### Automated Verification

- All tests pass: `go test -race ./...`
- Full build: `go build -o freedius .`
- CI check: `go vet ./... && go test ./... && go build .`
- Module graph clean: `go mod tidy && go mod verify`

#### Manual Verification

1. **Error display**: Send malformed request → see error message in requests tab (e.g. "no configured mapping for model 'gpt-4'")
2. **Edit flow**: Tab 3 → select entry with j/k → press e → form opens → edit fields → Enter saves → file updated
3. **Add flow**: Tab 3 → press a → fill form → Enter saves → new entry appears in list and file
4. **Delete flow**: Select entry → press d → confirm with y → entry removed from list and file
5. **Provider picker**: Edit form → focus provider field → Enter → picker opens → select with arrows → Enter → field populated
6. **Validation**: Edit form → enter invalid base_url → Enter → see inline error → fix → Enter → saves
7. **Live routing**: Edit a model's upstream model name → save → send request → proxy routes to the new upstream model
8. **Quit/resume**: Quit TUI → restart → config reflects all saved edits
9. **Backup**: After saving, `freedius.yaml.bak` exists with previous version

---

## Testing Strategy

### Unit Tests

- `proxy/eventbus_test.go`: verify ErrorMessage and ErrorType fields in RequestEvent, nil-safe defaults
- `proxy/proxy_test.go` (or middleware_test.go): verify X-Freedius-Error-* headers are set by writeErrorJSON and writeAnthropicError; verify EventBusMiddleware reads and populates them
- `config/config_test.go`: YAML round-trip (Load→Marshal→Load), OriginalProvider preservation, omitempty behavior, Save with backup/restore
- `proxy/tui/model_test.go`: form lifecycle (open/edit/submit/cancel/delete), cursor navigation, provider picker state, field validation error mapping
- `proxy/tui/picker_test.go` (new): provider picker selection, item ordering, metadata display

### Integration Tests

- `main_test.go`: end-to-end TUI test simulating config edit flow with programmatic key events
- Manual verification steps listed in each phase

### Manual Testing Steps

1. Full error display: send bad request → check error message appears
2. Full edit flow: edit → save → verify file → restart → verify persistence
3. Full add flow: add → save → send request with new model name → verify routing
4. Full delete flow: delete → save → send request with deleted model → verify "no match"
5. Provider picker UX: open, navigate, select, verify correct provider in form
6. Validation UX: trigger each validation rule → verify inline error message

## Performance Considerations

- YAML serialization of a config with ~20 entries is sub-millisecond — negligible
- `textinput.Model` from bubbles/v2 has minimal overhead (~2KB per field, 6 fields = ~12KB)
- Provider picker list with 7 items has negligible rendering cost
- EventBusMiddleware header read is O(1) — no measurable latency impact
- In-memory config mutation via shared pointer requires no locking for reads (the dispatcher reads config once per request from the same map reference; map mutations happen on the TUI goroutine between requests)

## Migration Notes

- No existing data migration needed — config format is unchanged
- Users with existing `freedius.yaml` can edit it with the TUI immediately — no schema changes
- First save creates `freedius.yaml.bak` as a safety net
- `NewDashboard()` signature change: callers (`tui.go`) must pass `cfgPath string` — single call site, trivial update

## References

- Research: `context/changes/tui-config-setup/research.md`
- Original TUI plan: `context/changes/tui-dashboard/plan.md`
- Original TUI research: `context/changes/tui-dashboard/research.md`
- Lessons: `context/foundation/lessons.md` — custom→mix rewrite (lessons.md:15-19), Adapter Return Contract (lessons.md:33-43)
- `proxy/tui/model.go:58-70` — Dashboard struct
- `proxy/tui/model.go:98-135` — Update() message dispatch
- `proxy/tui/views.go:112-135` — renderConfigTab
- `proxy/eventbus.go:13-22` — RequestEvent struct
- `proxy/proxy.go:448-474` — EventBusMiddleware
- `proxy/proxy.go:269-301` — writeErrorJSON and WithDetail
- `proxy/errors.go:29-43` — writeAnthropicError
- `config/config.go:86-170` — validateModel rules
- `config/providers_gen.go:61-95` — alias rewrite logic
- `providers.yaml:32-70` — provider definitions

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles.

### Phase 1: Error Message Capture in Event Pipeline

#### Automated

- [x] 1.1 Unit tests: `go test ./proxy/ -run TestEventBus -race` — new fields serialize correctly — b05756c
- [x] 1.2 Unit tests: `go test ./proxy/ -run TestMiddleware` — headers set and read correctly — b05756c
- [x] 1.3 Unit tests: `go test ./proxy/tui/ -v` — new fields render in requests tab — b05756c
- [x] 1.4 Compiles: `go build ./...` — b05756c
- [x] 1.5 Linting: `go vet ./...` — b05756c

#### Manual

- [ ] 1.6 Send unknown-model request in TUI — see error message instead of just red status

### Phase 2: Config YAML Serialization

#### Automated

- [x] 2.1 Unit tests: `go test ./config/ -v -run "RoundTrip|Marshal|Save"` — f2bb08e
- [x] 2.2 Round-trip test: Load → Marshal → Load — no semantic changes — f2bb08e
- [x] 2.3 Linting: `go vet ./config/` — f2bb08e

#### Manual

- [ ] 2.4 Visual inspection of marshaled YAML output for correct formatting and alias preservation

### Phase 3: Provider List Picker Component

#### Automated

- [x] 3.1 Compiles with `charm.land/bubbles/v2`: `go build ./...` — 78b0850
- [x] 3.2 Unit tests: `TestProviderPicker_Selection` — 78b0850
- [x] 3.3 Unit tests: `TestProviderInfo` — returns correct metadata for all 7 providers — 78b0850
- [x] 3.4 Linting: `go vet ./proxy/tui/` — 78b0850

### Phase 4: Model/Mapping Editor Forms (Modal Overlay)

#### Automated

- [x] 4.1 Unit tests: `go test ./proxy/tui/ -v -run "Form"` — e881cbf
- [x] 4.2 Unit tests: form lifecycle — open, edit, submit valid/invalid, cancel, delete confirm — e881cbf
- [x] 4.3 Compiles: `go build ./proxy/tui/` — e881cbf
- [x] 4.4 Linting: `go vet ./proxy/tui/` — e881cbf

#### Manual

- [ ] 4.5 Edit flow: Tab 3 → select entry → e → form opens with populated fields → Tab through fields → edit → Enter saves
- [ ] 4.6 Provider picker: focus provider field → Enter → list appears → select → field populated
- [ ] 4.7 Cancel: Esc closes form without saving
- [ ] 4.8 Delete confirm: Ctrl+D → confirmation → y deletes entry

### Phase 5: CRUD Operations + Save to Disk

#### Automated

- [x] 5.1 Unit tests: save config, verify file contents — 21a3341
- [x] 5.2 Unit tests: delete removes entry from in-memory maps — 21a3341
- [x] 5.3 Unit tests: add inserts entry into correct map — 21a3341
- [x] 5.4 All existing tests pass: `go test ./proxy/tui/ -v` — 21a3341
- [x] 5.5 Compiles: `go build ./...` — 21a3341
- [x] 5.6 Linting: `go vet ./...` — 21a3341

#### Manual

- [ ] 5.7 Edit → save → quit → `cat freedius.yaml` — change persisted
- [ ] 5.8 Add new model → save → send request using new name → proxy routes correctly
- [ ] 5.9 Delete model → save → send request → "no match" error
- [ ] 5.10 Backup file exists: `freedius.yaml.bak`

### Phase 6: Integration, Polish, and Test Coverage

#### Automated

- [ ] 6.1 All tests pass: `go test -race ./...`
- [ ] 6.2 Full build: `go build -o freedius .`
- [ ] 6.3 CI check: `go vet ./... && go test ./... && go build .`
- [ ] 6.4 Module graph clean: `go mod tidy && go mod verify`

#### Manual

- [ ] 6.5 Error display: malformed request → error message in requests tab
- [ ] 6.6 Config cursor: j/k navigate entries, highlighted selection
- [ ] 6.7 Live routing: edit upstream model → save → proxy routes to new model
- [ ] 6.8 Quit/resume: restart TUI → all edits persisted

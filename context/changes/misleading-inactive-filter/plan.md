# Align Mappings Status Vocabulary Implementation Plan

## Overview

The mappings page uses contradictory words for the same derived condition: the
status filter offers an **"Inactive"** option while the matching rows are badged
**"Key Missing"**. "Inactive" also falsely implies a user-controllable on/off
state that the `Mapping` model does not have. This plan makes the vocabulary
honest and consistent — one term, **"No API key"**, for both the filter option
and the status badge — and adds a tooltip explaining the cause. No data-model or
handler change: the filter still keys off the internal `value="inactive"`.

## Current State Analysis

- The status filter `<select>` renders an "Inactive" option
  (`proxy/web/templates/mappings-routing-table.html:18`). Its `value="inactive"`
  is what `handleMappings`/`buildMappingRows` actually switch on
  (`proxy/web/handlers.go:588–593`, `:696–706`).
- The Status column badge renders `Active` when the provider env key is present
  and `Key Missing` otherwise (`proxy/web/templates/mappings-routing-table.html:71–72`).
- "Inactive" (filter) and "Key Missing" (badge) describe the same condition
  (env API key absent) but say different things, and neither matches the other.
- `Mapping` has no active/enabled flag (`config/config.go:64`), so "Inactive"
  cannot mean a user-disabled state — it only ever means "key not set".
- A CSS comment references "Key Missing" as a label-length example
  (`proxy/web/static/app.css:2074`).
- No test asserts the badge/filter text, so the change introduces no test breakage.

### Key Discoveries:

- The filter *value* (`"inactive"`) is an internal key, independent of the
  displayed label — relabeling the option does **not** require touching handler
  logic.
- `Active` (filter option + badge) is already consistent and stays unchanged.
- Tooltip pattern already exists in this template (`title="{{.ProviderName}} / {{.Model}}"` at `:60`), so a `title` on the badge follows convention.

## Desired End State

On the mappings page, selecting the status filter shows an option labeled
**"No API key"**, and every row whose provider env key is unset is badged
**"No API key"** with a tooltip "No API key set in the environment". Filtering
by that option returns exactly those rows. The words agree end-to-end and no
longer imply a controllable state.

### Verification:

- `mage test` and `mage lint` pass.
- Manual: open `/mappings`, confirm the filter option and badge both read
  "No API key" and the badge shows the tooltip on hover.

## What We're NOT Doing

- Not adding a real user-controlled enable/disable state (no `Mapping` field,
  persist/writeback, or row toggle). That is a separate, larger change.
- Not renaming the internal filter `value="inactive"` or changing handler logic.
- Not touching the providers page (it has no status vocabulary).

## Implementation Approach

A pure labeling change confined to the mappings table template and one CSS
comment. Keep the internal `value="inactive"` so `buildMappingRows` filtering
keeps working unchanged. Add a `title` attribute to the status badge for the
clarifying tooltip, mirroring the existing `title` usage in the same template.

## Phase 1: Align status vocabulary

### Overview

Relabel the filter option and status badge to "No API key" and add the tooltip;
update the stale CSS comment.

### Changes Required:

#### 1. Mappings table template

**File**: `proxy/web/templates/mappings-routing-table.html`

**Intent**: Make the status filter option and the status badge use the same
honest term, and explain the badge with a tooltip. Keeps the internal
`value="inactive"` so filtering is unaffected.

**Contract**:
- Line 18: change the option's displayed text from `Inactive` to `No API key`.
  Keep `value="inactive"` and the `{{if eq .StatusFilter "inactive"}}selected{{end}}` guard.
- Lines 71–72: change the `{{else}}` branch badge text from `Key Missing` to
  `No API key`, and add `title="No API key set in the environment"` to that
  `<span class="badge badge--status-warn">` element (mirroring the `title=`
  pattern already used at line 60).

#### 2. CSS comment accuracy

**File**: `proxy/web/static/app.css`

**Intent**: Update the label-length comment so it no longer references the old
badge wording.

**Contract**: Line 2074 — replace the `"OK" vs "Key Missing"` example with
`"Active" vs "No API key"` (or equivalent), keeping the surrounding point about
constant badge width.

### Success Criteria:

#### Automated Verification:

- Lint passes: `mage lint`
- Build compiles: `mage build`
- Existing tests pass: `mage test`

#### Manual Verification:

- On `/mappings`, the status filter dropdown shows an option labeled "No API key".
- Rows with an unset provider env key are badged "No API key".
- Hovering that badge shows the tooltip "No API key set in the environment".
- Selecting the "No API key" filter returns exactly those rows (behavior unchanged).

**Implementation Note**: After completing this phase and all automated
verification passes, pause here for manual confirmation from the human that the
manual testing was successful before considering the change done.

## Testing Strategy

### Unit Tests:

- No new unit tests required: no logic changed and no existing test asserts the
  badge/filter text. (If desired, a template-render assertion that the badge
  contains "No API key" could be added, but it is optional.)

### Integration Tests:

- None required beyond the existing `mage test` suite, which exercises
  `?status=inactive` filtering (`proxy/web/handlers_phase4_test.go`).

### Manual Testing Steps:

1. Run `mage run`, open `http://localhost:8080/mappings`.
2. Open the "Filter by status" dropdown — confirm the option reads "No API key".
3. With a mapping whose provider env key is unset, confirm its Status badge reads
   "No API key" and shows the tooltip on hover.
4. Select the "No API key" filter — confirm it returns the same rows as before
   (semantics unchanged).

## Performance Considerations

None — text-only template and comment changes.

## Migration Notes

None — no schema, config, or API change. The internal filter `value="inactive"`
is preserved, so bookmarked `?status=inactive` URLs keep working.

## References

- Frame brief: `context/changes/misleading-inactive-filter/frame.md`
- Filter option: `proxy/web/templates/mappings-routing-table.html:18`
- Status badge: `proxy/web/templates/mappings-routing-table.html:71–72`
- Derived-status logic: `proxy/web/handlers.go:692–706`
- Mapping model (no active flag): `config/config.go:64`
- CSS comment: `proxy/web/static/app.css:2074`

## Progress

### Phase 1: Align status vocabulary

#### Automated

- [ ] 1.1 Lint passes (`mage lint`)
- [ ] 1.2 Build compiles (`mage build`)
- [ ] 1.3 Existing tests pass (`mage test`)

#### Manual

- [ ] 1.4 Filter option reads "No API key" on `/mappings`
- [ ] 1.5 Rows with unset env key badged "No API key" with tooltip on hover
- [ ] 1.6 "No API key" filter returns the same rows as before (semantics unchanged)

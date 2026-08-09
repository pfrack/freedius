# Align Mappings Status Vocabulary — Plan Brief

> Full plan: `context/changes/misleading-inactive-filter/plan.md`
> Frame brief: `context/changes/misleading-inactive-filter/frame.md`

## What & Why

The mappings page says two different things about the same condition: the status
filter offers **"Inactive"** while the matching rows are badged **"Key Missing"**,
and "Inactive" falsely implies a user-controllable on/off state the `Mapping`
model doesn't have. We make the vocabulary honest and consistent — one term,
**"No API key"**, for both — and add a tooltip explaining the cause.

> Reframed problem (from frame): the "Inactive" status filter is misleading
> because its label and its derived meaning (API key env var absent) disagree
> with the Status badge it surfaces ("Key Missing"), and imply a controllable
> state the data model does not support.

## Starting Point

Today the filter option (`mappings-routing-table.html:18`) reads "Inactive" and
the status badge (`:72`) reads "Key Missing"; both derive from env-key absence
(`handlers.go:692–706`). "Inactive" is only an internal `value`, not a stored
state (`config.go:64` has no active flag).

## Desired End State

On `/mappings`, the status filter option and every unset-key status badge both
read **"No API key"**, the badge carries a tooltip ("No API key set in the
environment"), and filtering by that option returns the same rows as before. The
words agree end-to-end and no longer imply a controllable state.

## Key Decisions Made

| Decision          | Choice                    | Why (1 sentence)                                                              | Source |
| ----------------- | ------------------------- | ----------------------------------------------------------------------------- | ------ |
| Scope             | Align labels only         | Fixes the misleading behavior with no data-model change; enable/disable is a separate change. | Plan   |
| Standardized term | "No API key"              | Honest and unambiguous — matches the actual derived condition (key not set). | Plan   |
| Tooltip           | Add on status badge       | Prevents re-confusion cheaply, following the existing `title` pattern.        | Plan   |
| Internal value    | Keep `value="inactive"`   | Filtering logic keys off it; relabeling display text needs no handler change. | Frame  |

## Scope

**In scope:** relabel the filter option + status badge to "No API key"; add a
tooltip on the badge; update one stale CSS comment.

**Out of scope:** a real user-controlled enable/disable state (no `Mapping`
field, persist/writeback, or row toggle); renaming the internal filter value;
changes to the providers page.

## Architecture / Approach

Pure labeling change in the mappings table template. The internal
`value="inactive"` is preserved so `buildMappingRows` filtering
(`handlers.go:696–706`) is unaffected. Tooltip reuses the `title` attribute
pattern already present in the same template.

## Phases at a Glance

| Phase | What it delivers                          | Key risk                    |
| ----- | ----------------------------------------- | --------------------------- |
| 1     | Relabel filter + badge, add tooltip, fix CSS comment | Low — text-only; semantics unchanged |

**Prerequisites:** none (no new deps, no schema).
**Estimated effort:** ~1 short session, single phase.

## Open Risks & Assumptions

- Assumption: "No API key" is the preferred wording; if you'd rather keep
  "Key Missing", only the filter option needs to change instead.
- No tests assert badge text, so none break — but a future test could; the
  change is text-only and low-risk.

## Success Criteria (Summary)

- `mage test`, `mage lint`, `mage build` all pass.
- On `/mappings`, the filter option and unset-key badges both read "No API key"
  with a tooltip, and filtering returns the same rows as before.

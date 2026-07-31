# Review Fix Follow-ups

Queued from `context/changes/mapping-first-ui-refactor/reviews/impl-review.md`.

## F8 — Design-system scope split (applied Fix A)

**Action**: Split the shipped work. The mapping-first structural changes
(sidebar reorder, stats grid, mapping-centric card hierarchy, filter UI) are
accepted in `mapping-first-ui-refactor`. The concurrent design-system rewrite
(color palette, font stack, spacing/radii, shadows, animations, card/table/
form/dialog styling) should be absorbed by the pre-existing
`context/changes/web-ui-design-upgrade/` change.

**No code change required here** — the work is already shipped and tests pass.
This is a planning/scope-accounting action item for the next `web-ui-design-upgrade`
cycle.

**Recommended next step**: Open/confirm `context/changes/web-ui-design-upgrade/`
and update its plan.md to record that the following were already implemented as
of 2026-07-31 and should be treated as done (not re-implemented):
- Geist font via CDN (`layout.html:9-11`)
- New color palette, spacing variables, radii, and shadow system in `app.css`
- Redesigned card, table, form, and dialog styling in `app.css`
- Noise-effect and animation-system additions in `app.css`

Mark this follow-up as resolved once `web-ui-design-upgrade` has acknowledged the
already-shipped design work.

# Frame Brief: Misleading "Inactive" status filter on mappings page

> Framing step before /10x-plan. Captures what is *actually* at issue,
> separated from what was initially assumed.

## Reported Observation

"inactive button not visible on UI, either we dont have inactive state or
we should add the button" — followed by the clarification: the "Inactive"
option *is* present in the status filter dropdown, but it is **misleading**.

The literal observable effect: an "Inactive" filter option exists in the
mappings UI, but it does not correspond to an intuitive "inactive" concept,
and the surrounding UI contradicts it.

## Initial Framing (preserved)

- **User's stated cause or approach**: either the codebase has no "inactive"
  state at all, or the UI is simply missing the button.
- **User's proposed direction**: add the button.
- **Pre-dispatch narrowing**: user confirmed the "Inactive" filter option
  *does* exist in the dropdown; the real complaint is that it is misleading,
  not absent. (Outcome question left "not sure yet" — the issue is the
  misleading semantics, not a specific desired end-state.)

## Dimension Map

The observation could originate at any of these dimensions:

1. **Missing UI control** — the literal "button" the user first assumed was
   absent. → RULED OUT: the "Inactive" <option> exists at
   `mappings-routing-table.html:18`; per-row actions are Edit/Delete only
   (`mappings-routing-table.html:74–89`), so no toggle button exists, but
   the user confirmed the filter option is present.
2. **Vocabulary mismatch (filter ↔ badge)** — the filter is labeled
   "Inactive" but the matching Status badge reads "Key Missing"
   (`mappings-routing-table.html:71–72`). A user who filters "Inactive"
   expects to see rows also tagged "Inactive", and does not.  ← reframed problem
3. **Semantic confusion (derived vs controllable state)** — "Inactive" is
   derived purely from env-key absence (`handlers.go:692–706`), not a
   user-controlled on/off flag. There is no stored active/inactive field on
   `Mapping` (`config/config.go:64`). So "Inactive" implies a disabled
   mapping the user can re-enable, when it really means "API key not set".
4. **Missing stored inactive state** — if the *intent* was a real enable/
   disable capability, the data model has no field to hold it and would need
   one before any button could work. Plausible only if the desired outcome
   is user-controlled toggling, which the user did not confirm.

## Hypothesis Investigation

| Hypothesis | Evidence | Verdict |
| --- | --- | --- |
| Missing UI "Inactive" button/filter | `mappings-routing-table.html:18` clearly renders an "Inactive" option; user confirmed it is visible | NONE |
| Filter label vs badge vocabulary mismatch | Filter says "Inactive" (`line 18`); badge says "Key Missing" (`lines 71–72`); no "Inactive" badge anywhere | STRONG |
| "Inactive" is derived from env-key absence, not a real state | `handlers.go:692–706` derives status from `os.Getenv(p.DefaultAPIKeyEnv)`; `Mapping` struct (`config/config.go:64`) has no active flag | STRONG |
| User wanted a controllable enable/disable toggle | No stored flag in model; user left outcome "not sure yet"; only confirmed it is "misleading" | WEAK |

## Narrowing Signals

- User explicitly answered "but there in inactive in filter?" — confirms the
  control exists; the issue is not absence.
- User's follow-up "then it is missleadin[g]" — pins the actual complaint to
  *semantics/labels*, not a missing control.
- Cross-check: the providers page (`providers-table.html`,
  `providers.html`) uses **no** status/inactive vocabulary at all, so the
  "Active/Inactive" terminology is unique to the mappings filter and has no
  consistent sibling convention to lean on.

## Cross-System Convention

No other page in the UI models an "active/inactive" entity state, so there is
no established convention to copy. The mappings filter invented the term
"Inactive" for a derived runtime condition (missing env key) and labelled the
resulting badge "Key Missing" — an internal inconsistency within one page,
not a project-wide pattern. The leading hypothesis (misleading label/semantics)
matches the only evidence present and contradicts the original "add a button"
framing.

## Reframed (or Confirmed) Problem Statement

> **The actual problem to plan around is**: the mappings "Inactive" status
> filter is misleading because its label ("Inactive") and its derived meaning
> (API key env var absent) disagree with the Status badge it surfaces
> ("Key Missing"), and imply a user-controllable on/off state that the data
> model does not support.

This is a labeling/semantics problem, not a missing-control problem. Adding a
button (the original proposed direction) would not fix it — and if the
intent were a *real* enable/disable capability, that requires a new stored
state on `Mapping` plus persistence and handlers first. The mismatch between
the filter's promise and what the user sees is what makes the UI feel broken.

## Confidence

- **HIGH** — strong, directly-cited evidence (template + handler + config
  struct) for both the label mismatch and the derived-state semantics; user
  confirmed the control exists and called it misleading; no competing
  hypothesis with comparably strong evidence.

## What Changes for /10x-plan

The plan should be about making the status vocabulary honest and consistent
(e.g. align the filter option, the Status badge, and any tooltip to one
coherent term such as "Key Missing" / "No API key"), and about deciding
whether a *real* user-controlled active/inactive state is in scope (which
would be a separate, larger data-model change). Do **not** start by "adding a
button" — that addresses a non-problem.

## References

- Filter option: `proxy/web/templates/mappings-routing-table.html:18`
- Status badge: `proxy/web/templates/mappings-routing-table.html:71–72`
- Derived status logic: `proxy/web/handlers.go:692–706`
- Status filter parse/types: `proxy/web/handlers.go:588–593`, `proxy/web/types.go:207`
- Mapping model (no active flag): `config/config.go:64`
- Sibling page (no status vocab): `proxy/web/templates/providers-table.html`

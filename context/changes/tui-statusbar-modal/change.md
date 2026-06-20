---
change_id: tui-statusbar-modal
title: "Pin stats bar to top, show tabs below it, add `?` keyboard shortcuts modal"
status: implemented
created: 2026-06-20
updated: 2026-06-20
---

## Summary

Move the existing bottom stats bar (uptime, requests, errors, error rate, transient
status message) to the top of the dashboard so it is always visible. The tab
switcher stays a single row directly below the stats bar; tab content fills the
remaining height.

Add a keyboard shortcuts help modal opened with `?` (and dismissed with `?` or
`Esc`). The modal lists every current keybinding grouped by context (Global,
Tab switching, Log, Providers, Config, Form, Delete confirm, Picker).

## Scope (decided with user)

- Layout: **stats on top, then tabs, then content** (no tab-bar merge, no panel
  stack — keeps the existing single-tab-switcher shape, just moved).
- Modal key: **`?`**, standard TUI convention.
- Modal content: **all current shortcuts grouped by context**.

## Out of scope (potential follow-ups, flagged during research)

- The form footer says `Ctrl+D=Delete` at `proxy/tui/views.go:370`, but
  `ctrl+d` is not bound anywhere. The form does not support in-form delete at
  all. Decide whether to fix the footer (drop the line) or add a real
  `ctrl+d` binding as a separate small change.
- The providerPicker lets `q` / `Ctrl+C` fall through to the bubble list's
  default Quit keymap, which would quit the TUI from inside the picker. This
  is a pre-existing leak unrelated to this change — note for a follow-up.
- Translating shortcut descriptions, or making the table sortable / filterable.

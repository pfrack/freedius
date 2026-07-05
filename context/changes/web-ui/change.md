---
id: web-ui
title: Replace Bubble Tea TUI with embedded web UI
status: impl_reviewed
created: 2026-07-02
updated: 2026-07-05
type: feature
---

# Replace Bubble Tea TUI with embedded web UI

Replace the in-process Bubble Tea dashboard with a Go stdlib web server
(`html/template` + vendored htmx) bound to a separate port, so freedius
runs cleanly inside Docker / headless environments while still showing
live request and log data through a browser.

Scope derived from research at `research.md`. Full plan at `plan.md`.
Brief at `plan-brief.md`.

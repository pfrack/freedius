---
change_id: tui-all-logs-level-filter
title: "TUI Log Tab: all slog lines + cycle-level filter"
status: implementing
created: 2026-06-20
updated: 2026-06-20
---

## Summary

Route every `slog` line into the TUI's first tab (in addition to the current stderr sink) and render the tab without lipgloss styling. Add a single cycling key (`L`) to filter the tab by level: `All → Debug → Info → Warn → Error → All`. Keep the existing access-log lines; merge slog lines chronologically.
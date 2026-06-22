---
id: tui-providers-mappings-split
title: "Split TUI Config tab into separate Providers and Mappings tabs"
status: archived
created: 2026-06-22
updated: 2026-06-22
archived_at: 2026-06-22T11:52:15Z
---

# Change: Split TUI Config tab into separate Providers and Mappings tabs

UI reorg: decompose the current `tabConfig` (which conflates providers and mappings in one list) into two distinct surfaces — providers get editing via an overlay modal launched from the Providers tab, and mappings get their own dedicated tab (replacing the old Config tab).

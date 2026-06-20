---
change_id: unified-server-logs-tab
title: "Unified mode: server-log tab + single binary entry point"
status: impl_reviewed
created: 2026-06-20
updated: 2026-06-20
---

## Summary

Replace the TUI's first tab (Requests table) with raw server access logs (matching `AccessLogMiddleware` output in serve mode). Collapse the multi-subcommand dispatch into a single entry point where `freedius` always starts the TUI+proxy.

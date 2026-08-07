---
id: web-ui-polish
roadmap_id: V-02k
title: Web UI Polish — Remainder & Dead-Code Cleanup
status: implementing
created: 2026-08-04
updated: 2026-08-07
---

Complete the 3 features left over from the superseded web-ui-polish audit
(skeleton loaders, body-text max-width, back-to-top button) and clean up dead
code discovered by a fresh audit. CSS-first, scoped to
`proxy/web/templates/*.html` and `proxy/web/static/app.css` + `app.js`.
No Go changes, no test changes.

The original 17-finding audit was fully delivered by the archived
`2026-08-01-ui-design-polish` change (PR #40). This plan covers only what
remains, plus newly discovered dead code.

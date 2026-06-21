---
id: go-package-layout
title: "Move main package to cmd/freedius/ (Go convention)"
status: impl_reviewed
created: 2026-06-21
updated: 2026-06-21
---

# Change: go-package-layout

Mechanical relocation of the application entry point into `cmd/freedius/` to
align with Go community convention (stdlib, Kubernetes, Moby, Zalando skipper).
Supersedes the prior rejection of `cmd/` documented at
`context/archive/error-hardening/research.md:261` — that rejection was about
adding a *second* binary; this change keeps one binary, just at the
conventional path.

## Closeout

**Commit**: `a5a8d53` — `refactor(go-package-layout): move main package to cmd/freedius/ (p1)`

**Deferred follow-ups**:
- `internal/genproviders/` → `cmd/genproviders/` (package main under internal/ anomaly)
- `proxy/translate/` + `proxy/tui/` → `internal/proxy/` (tighten public surface)
- AGENTS.md project-structure list refresh (document missing directories)

**Drift from plan**:
- `test-manual.sh` required extensive fixes beyond the planned build-path swap: (a) Bubble Tea TTY wrapper via `script(1)` to keep the server alive in non-interactive mode, (b) all ~14 YAML config snippets updated from the old `provider`/`model`/`base_url`/`api_key_env` schema to the current `provider_name`/`model_string`/`providers:` schema. Both are pre-existing issues unpinned by this change but fixed in transit.
- `.gitignore` needed `freedius` → `/freedius` anchor to avoid matching the new `cmd/freedius/` directory.

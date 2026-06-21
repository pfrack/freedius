---
id: go-package-layout
title: "Move main package to cmd/freedius/ (Go convention)"
status: implementing
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

`internal/genproviders/` → `cmd/genproviders/` is **deferred** to a follow-up
change (anomaly worth fixing but not bundled).

See `research.md` for the full citation set and `plan.md` for the step list.

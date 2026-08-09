---
id: claude-settings-injection
title: Safe Claude Code settings injection (backup + freedius env)
status: preparing
created: 2026-08-09
updated: 2026-08-09
phase: 0
tags:
  - cli
  - envinject
  - settings
---

# Frame Brief: Safe Claude Code settings injection

> Framing step before /10x-plan. This document captures what is *actually*
> at issue, separated from what was initially assumed.

## Reported Observation

The user wants to run `claude` (Claude Code CLI) through freedius as a local
proxy. They already have a custom `~/.claude/settings.json`. freedius today
only prints an env-var snippet to stderr on startup (`cmd/freedius/main.go:151`
→ `envinject.Snippet`) and does **not** write or back up `settings.json`. The
user is undecided whether the "point Claude Code at freedius" capability should
be a CLI argument/subcommand (`freedius login`) or a button in the web dashboard.
Their stated biggest worry is **losing the custom settings.json**.

## Initial Framing (preserved)

- **User's stated cause or approach**: They need an argument that backs up
  `settings.json` and overwrites it with freedius's settings.
- **User's proposed direction**: Make a `login` subcommand/flag that backs up
  and overwrites `settings.json` with freedius's env.
- **Pre-dispatch narrowing**: User is unsure *where* they trigger it ("nie wiem"
  on terminal vs UI); only they use it (localhost, single user); the dominant
  concern is losing the custom `settings.json` (hence backup matters).

## Dimension Map

The observation could originate at any of these dimensions:

1. **CLI arg/subcommand** — freedius is a terminal tool; the user runs it in the
   same shell where they'll run `claude`. A `login` subcommand is invoked in
   that same context. Natural fit, net-new dispatch needed.
2. **Web UI dashboard action** — the `:8083` dashboard already has CRUD handlers
   that persist config to disk, so a "Configure Claude Code" button is feasible.
   ← initial framing leaned here as an alternative.
3. **Write semantics (overwrite vs merge + backup)** — the actual substance of
   the "don't lose my custom settings" worry, surface-independent.
4. **Implicit auto-write on startup vs explicit trigger** — freedius already
   *has* `WriteSettingsJSON` (unused); auto-on-start is an alternative to an
   explicit arg/button, but the user wants an explicit, backup-first action.

## Hypothesis Investigation

| Hypothesis | Evidence | Verdict |
| --- | --- | --- |
| CLI subcommand is a natural, low-cost home | No subcommand dispatch exists (`run()` is flat-flag only, `main.go:57`); `WriteSettingsJSON` has zero callers; `envinject.Snippet` already prints to the same terminal. Adding `login` is net-new but coherent. | STRONG |
| UI button is feasible | `web.NewServer` + `mux.HandleFunc` CRUD pattern persists config to disk (`proxy/web/handlers.go:347-386`); file-write precedent exists. | STRONG (feasible) |
| UI button crosses a boundary | Writing into `$HOME/.claude/settings.json` from a web handler is unusual for a proxy even on localhost (single user). No such home-dir write exists today. | WEAK (red flag, not blocker) |
| Safe form = backup + merge | `WriteSettingsJSON` already **merges** (preserves other keys, only sets `"env"`, `settings.go:52`); a backup step before write directly answers "utracenie custom settings". | STRONG |

## Narrowing Signals

- User confirmed: **only them, localhost** → remote/multi-user attack surface is
  not a deciding factor; the UI boundary-crossing is a code-smell, not a
  security emergency.
- User confirmed: **biggest worry is losing custom settings** → the decisive
  requirement is backup first; the original stays restorable.
- "Nie wiem gdzie" on terminal vs UI → the surface choice is genuinely a
  packaging preference, not forced by the problem.
- **Write semantics refined**: user prefers **backup + overwrite with only
  freedius's needs** (no merge). Backup *scope* (single `settings.json` vs whole
  `~/.claude/` directory) is deferred — user said "nie wiem"; default to a
  timestamped `settings.json.bak.<ts>` in planning, directory backup optional.

## Cross-System Convention

freedius is a CLI-first tool: it prints its client-setup snippet to stderr
(`envinject.Snippet`) and expects the operator to paste it. The codebase has an
unused `WriteSettingsJSON` that *merges*, signalling the intended behavior was
always "inject our env, keep the rest." No existing path writes into
`$HOME/.claude` from the web layer. A CLI subcommand keeps the config-write in
the operator's own process context rather than reaching from the web server into
the user's agent config — consistent with the existing terminal-first design.

## Reframed (or Confirmed) Problem Statement

> **The actual problem to plan around is**: give the operator a *safe,
> backup-first* way to write freedius's env block into `~/.claude/settings.json`
> **without destroying their existing custom settings** — and pick the surface
> (CLI subcommand vs UI button) that fits freedius's terminal-first design.

The UI-vs-arg question is real but secondary: both are viable, and the substance
(backup + write-only-freedius-needs) is identical either way. The evidence favors
a **CLI subcommand** (`freedius configure` / `--write-settings`): it lives in the
same terminal the user already uses for `mage run` and `claude`, needs no new web
surface, and avoids the web-server-writing-into-$HOME/.claude boundary crossing.

**Write semantics (refined):** the user prefers **backup + overwrite with only
what freedius needs** — i.e. after backing up, write a fresh `settings.json`
containing freedius's env block, NOT a merge that preserves the old keys. The
original is fully restorable from the backup, so non-destructive intent is met
without keeping stale custom keys around. (This reverses the earlier "merge"
lean — `WriteSettingsJSON` currently merges at `settings.go:52`; the plan must
write the freedius-only block instead, or add a `merge bool` flag.)

## Confidence

- **MEDIUM-HIGH** — strong evidence that a CLI subcommand is the coherent surface
  and that merge+backup is the right semantics; the only open point is packaging
  preference (CLI vs UI), which the user should confirm.

## What Changes for /10x-plan

Plan a CLI subcommand (e.g. `freedius configure`, or a `--write-settings` flag)
that: (1) backs up the current `~/.claude/settings.json` to
`settings.json.bak.<timestamp>`, (2) writes freedius's env block via the existing
`WriteSettingsJSON` (merge, not blind overwrite), and (3) reuses `envinject`'s
host/port addressing. The UI-button alternative should be noted as a fallback,
not the primary, due to the web-into-$HOME boundary crossing. Decide explicitly
between subcommand vs flag during planning.

**Naming constraint (user feedback):** do NOT call it `login` or `claude` —
freedius is provider-agnostic and may front non-Claude clients in the future.
Use a generic verb (e.g. `configure`, `client`, `setup`) or the descriptive
`--write-settings` flag. The injected env vars are currently Anthropic-shaped
(`ANTHROPIC_BASE_URL` etc.), but the *action* is "make the local AI client use
freedius as its base URL", which is client-neutral.

## References

- Client-setup snippet: `cmd/freedius/main.go:151`, `internal/envinject/snippet.go`
- Unused merge writer: `internal/envinject/settings.go:25` (`WriteSettingsJSON`)
- CLI entry / flat flags: `cmd/freedius/main.go:51-91`
- Web server + CRUD/file-write precedent: `cmd/freedius/main.go:185`,
  `proxy/web/handlers.go:347-386`

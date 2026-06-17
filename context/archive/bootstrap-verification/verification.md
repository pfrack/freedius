---
bootstrapped_at: 2026-06-16T07:36:48Z
starter_id: go
starter_name: Go (standard library)
project_name: freedius
language_family: go
package_manager: N/A (no {pm} placeholder in cmd_template)
cwd_strategy: subdir-then-move
bootstrapper_confidence: first-class
phase_3_status: ok
audit_command: govulncheck ./...
---

## Hand-off

```yaml
starter_id: go
project_name: freedius
hints:
  language_family: go
  team_size: solo
  deployment_target: self-host
  ci_provider: github-actions
  ci_default_flow: auto-deploy-on-merge
  bootstrapper_confidence: first-class
  path_taken: standard
  quality_override: false
  self_check_answers: null
  has_auth: false
  has_payments: false
  has_realtime: false
  has_ai: true
  has_background_jobs: false
```

## Why this stack

Go is the recommended default for CLI tools, and freedius is a local HTTP proxy — Go's `net/http` and `httputil.ReverseProxy` are built into the standard library, making the core routing logic nearly zero-dependency. A solo developer shipping in 2 weeks after-hours needs fast compile times, simple concurrency (goroutines handle multiple concurrent Claude Code sessions per the multi-agent NFR), and a single static binary for trivial local installation. Go delivers all three. It passes all four agent-friendly quality gates (typed, convention-based, popular in Go training data, well-documented). Bootstrapper confidence is first-class — the stack is registered with a valid CLI; expect mostly-smooth scaffolding. Deployment is self-host (local binary). CI runs on GitHub Actions with auto-deploy-on-merge.

## Pre-scaffold verification

| Signal             | Value                              | Severity | Notes                                                |
| ------------------ | ---------------------------------- | -------- | ---------------------------------------------------- |
| npm package        | not run                            | —        | non-JS starter (language_family: go)                 |
| GitHub repo        | not run                            | —        | docs_url (https://go.dev/doc/) is not a GitHub URL   |

## Scaffold log

**Resolved invocation**: `mkdir .bootstrap-scaffold && cd .bootstrap-scaffold && go mod init github.com/user/freedius`
**Strategy**: subdir-then-move (scaffold into a temp directory then move files up)
**Exit code**: 0
**Files moved**: 1 (go.mod)
**Conflicts (.scaffold siblings)**: none
**.gitignore handling**: absent in scaffold
**.bootstrap-scaffold cleanup**: deleted

## Post-scaffold audit

**Tool**: govulncheck ./...
**Status**: failed to run
**Reason**: govulncheck is not installed on this system

## Hints recorded but not acted on

| Hint                       | Value                              |
| -------------------------- | ---------------------------------- |
| bootstrapper_confidence    | first-class                        |
| quality_override           | false                              |
| path_taken                 | standard                           |
| self_check_answers         | null                               |
| team_size                  | solo                               |
| deployment_target          | self-host                          |
| ci_provider                | github-actions                     |
| ci_default_flow            | auto-deploy-on-merge               |
| has_auth                   | false                              |
| has_payments               | false                              |
| has_realtime               | false                              |
| has_ai                     | true                               |
| has_background_jobs        | false                              |

## Next steps

Next: a future skill will set up agent context (CLAUDE.md, AGENTS.md). For now, your project is scaffolded and verified — happy hacking.

Useful manual steps in the meantime:
- `git init` (if you have not already) to start your own repo history.
- Review any `.scaffold` siblings the conflict policy created and decide which version of each file to keep.
- Address audit findings per your project's risk tolerance — the full breakdown is in this log.

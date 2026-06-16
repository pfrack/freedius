---
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
---

## Why this stack

Go is the recommended default for CLI tools, and freedius is a local HTTP proxy — Go's `net/http` and `httputil.ReverseProxy` are built into the standard library, making the core routing logic nearly zero-dependency. A solo developer shipping in 2 weeks after-hours needs fast compile times, simple concurrency (goroutines handle multiple concurrent Claude Code sessions per the multi-agent NFR), and a single static binary for trivial local installation. Go delivers all three. It passes all four agent-friendly quality gates (typed, convention-based, popular in Go training data, well-documented). Bootstrapper confidence is first-class — the stack is registered with a valid CLI; expect mostly-smooth scaffolding. Deployment is self-host (local binary). CI runs on GitHub Actions with auto-deploy-on-merge.

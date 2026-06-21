---
id: missing-providers-tui
title: "Investigate missing providers in TUI"
status: complete
created: 2026-06-21
updated: 2026-06-21
---

# Change: Investigate missing providers in TUI

## Problem

User reports not seeing all expected providers in the TUI. The `anthropic` provider was removed from `providers.yaml` in commit `1a1bf3a` ("Tui dashboard (#13)"), but the generated Go files are stale and still include it.

## Root Cause

The `providers.yaml` file is the single source of truth for provider metadata. The generated files (`config/providers_gen.go` and `proxy/adapters_gen.go`) are produced by `go generate ./...` but have not been regenerated since the `anthropic` entry was removed.

## Impact

- If `go generate ./...` is run, `anthropic` will disappear from both generated files
- The TUI reads from runtime config which merges `providerDefaults` (from generated file) with user config
- Other popular providers (Google, Mistral, Cohere, etc.) were never in the repository

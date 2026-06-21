---
id: add-popular-providers
title: "Add popular AI providers to providers.yaml"
status: implemented
created: 2026-06-21
updated: 2026-06-21
---

# Change: Add Popular AI Providers

## Problem

Users expect to see popular AI providers (Anthropic, Google, Mistral, DeepSeek, etc.) in the TUI. Currently only 6 providers exist, and `anthropic` was accidentally removed.

## Solution

Add 10 new providers to `providers.yaml`:
- **Priority 1** (cloud, no auth changes): anthropic, google, mistral, deepseek, groq, together, fireworks, cohere
- **Priority 2** (local, needs auth skip): ollama, lmstudio

## Steps

1. Add `no_auth` support to OpenAI adapter
2. Update `providers.yaml` with all new providers
3. Update tests
4. Run `go generate ./...`
5. Verify with `go test ./...` and `go vet ./...`

## Files

- `providers.yaml` — add provider entries
- `proxy/openai_compat.go` — auth skip for local providers
- `internal/genproviders/main.go` — add `NoAuth` field
- `internal/genproviders/main_test.go` — update test assertions
- `config/providers_gen.go` — generated
- `proxy/adapters_gen.go` — generated

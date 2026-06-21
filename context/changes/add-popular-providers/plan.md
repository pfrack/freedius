---
id: add-popular-providers
title: "Add popular AI providers to providers.yaml"
status: planning
created: 2026-06-21
updated: 2026-06-21
---

# Plan: Add Popular AI Providers

## Context

The user wants to add popular AI providers to freedius. Currently, `providers.yaml` has 6 providers (`nim`, `zen`, `go`, `custom`, `openai`, `mix`), but is missing `anthropic` (removed in commit `1a1bf3a`) and other widely-used providers.

## Providers to Add

### Priority 1: Restore + Cloud Providers (no auth changes needed)

| Provider | Behavior | Default Base URL | API Key Env | Special |
|----------|----------|------------------|-------------|---------|
| `anthropic` | `anthropic` | `https://api.anthropic.com/v1/messages` | `ANTHROPIC_API_KEY` | Restore removed entry |
| `google` | `openai` | `https://generativelanguage.googleapis.com/v1beta/openai/chat/completions` | `GEMINI_API_KEY` | `no_stream_usage: true` |
| `mistral` | `openai` | `https://api.mistral.ai/v1/chat/completions` | `MISTRAL_API_KEY` | — |
| `deepseek` | `openai` | `https://api.deepseek.com/chat/completions` | `DEEPSEEK_API_KEY` | — |
| `groq` | `openai` | `https://api.groq.com/openai/v1/chat/completions` | `GROQ_API_KEY` | — |
| `together` | `openai` | `https://api.together.xyz/v1/chat/completions` | `TOGETHER_API_KEY` | — |
| `fireworks` | `openai` | `https://api.fireworks.ai/inference/v1/chat/completions` | `FIREWORKS_API_KEY` | — |
| `cohere` | `openai` | `https://api.cohere.com/compatibility/v1/chat/completions` | `COHERE_API_KEY` | — |

### Priority 2: Local Providers (need auth-skip logic)

| Provider | Behavior | Default Base URL | API Key Env | Special |
|----------|----------|------------------|-------------|---------|
| `ollama` | `openai` | `http://localhost:11434/v1/chat/completions` | _(none)_ | `no_stream_usage: true`, needs auth skip |
| `lmstudio` | `openai` | `http://localhost:1234/v1/chat/completions` | _(none)_ | `no_stream_usage: true`, needs auth skip |

## Blocker: Auth Skip for Local Providers

`proxy/openai_compat.go:75-85` fails if `DefaultAPIKeyEnv` is empty. For local providers (Ollama, LM Studio), the adapter needs to skip auth when `DefaultAPIKeyEnv` is empty.

### Solution

Add `no_auth` field to `OpenAIOptions` struct:
1. `internal/genproviders/main.go:57-60` — add `NoAuth bool` to `OpenAIOptions`
2. `proxy/openai_compat.go:75-85` — skip auth check when `provider.DefaultAPIKeyEnv == ""`
3. `providers.yaml` — set `no_auth: true` for local providers

## Implementation Steps

### Step 1: Add `no_auth` support to OpenAI adapter

**Files to modify:**
- `proxy/openai_compat.go:75-85` — skip auth when `DefaultAPIKeyEnv` is empty
- `internal/genproviders/main.go:57-60` — add `NoAuth` field to `OpenAIOptions`

### Step 2: Update `providers.yaml`

Add all providers listed above. The file should grow from 6 to 16 providers.

### Step 3: Update tests

**Files to modify:**
- `internal/genproviders/main_test.go:14-56` — update `fullSpec()` with new providers
- `internal/genproviders/main_test.go:246` — update provider count assertion (7 → 16)
- `internal/genproviders/main_test.go:77-84` — update provider name assertions
- `internal/genproviders/main_test.go:264-270` — update provider name assertions

### Step 4: Run code generation

```bash
go generate ./...
```

This updates:
- `config/providers_gen.go` — adds entries to `providerDefaults`
- `proxy/adapters_gen.go` — updates `NewDefaultRegistry` (no new thin wrappers needed unless providers have custom options)

### Step 5: Verify

```bash
go test ./...
go vet ./...
```

## Progress

### Phase 1: Add providers and auth skip

#### Automated
- [x] 1.1 Skip auth when DefaultAPIKeyEnv is empty in openai_compat.go
- [x] 1.2 Add 9 new providers to providers.yaml (google, mistral, deepseek, groq, together, fireworks, cohere, ollama, lmstudio)
- [x] 1.3 Fix anthropic require_base_url to false
- [x] 1.4 Update test assertions (fullSpec, provider count, name lists)
- [x] 1.5 Run go generate ./...
- [x] 1.6 Run go test ./... and go vet ./...

#### Manual
- [ ] 1.7 TUI shows all providers in the Providers tab
- [ ] 1.8 Local providers (Ollama, LM Studio) work without API key

## Verification Criteria

- [ ] `providers.yaml` contains all 16 providers
- [ ] `go generate ./...` succeeds without errors
- [ ] `go test ./...` passes
- [ ] `go vet ./...` passes
- [ ] TUI shows all providers in the Providers tab
- [ ] Provider picker shows all providers when adding a mapping
- [ ] Local providers (Ollama, LM Studio) work without API key

## Open Questions

1. Should `anthropic` use `require_base_url: false` (since it has a default) or `true` (current behavior)?
   - **Decision**: `false` — it has a sensible default, like `nim`

2. Should local providers be Priority 1 or Priority 2?
   - **Decision**: Priority 2 — they need auth-skip logic first

3. Should we add `pre_send_hook` for any new providers?
   - **Decision**: No — none of the new providers need body sanitization like NIM does

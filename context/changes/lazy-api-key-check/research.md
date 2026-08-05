---
date: 2026-07-31T23:25:01+02:00
researcher: kiro
git_commit: efd5a66602dbce6192a5e04d6dd93ad5143bfd75
branch: dashboard-redesign
repository: freedius
topic: "Lazy/proxy API key checking — move env var validation from startup to first-use"
tags: [research, codebase, startup, env-vars, providers, web-ui]
status: complete
last_updated: 2026-07-31
last_updated_by: kiro
---

# Research: Lazy/Proxy API Key Checking

**Date**: 2026-07-31T23:25:01+02:00
**Researcher**: kiro
**Git Commit**: efd5a66602dbce6192a5e04d6dd93ad5143bfd75
**Branch**: dashboard-redesign
**Repository**: freedius

## Research Question

Can we move API key env var checking from startup (fatal) to first-use (lazy), scoped specifically to providers only used by the web dashboard? What's the effort and risk?

## Summary

**The runtime already has lazy checking built in.** Both adapters (`openai_compat.go`, `anthropic_compat.go`) independently validate API keys at request time, returning a `configError{errType: "authentication_error"}` that feeds into the fallback loop. The startup `checkRequiredEnvVars` function is a **redundant safety net** that kills the process before it can serve any traffic.

Removing or relaxing the startup check is a **trivial, low-risk change**: delete or comment out the 15-line function call. The web UI already shows "Key Missing" badges in real-time. The only behavioral difference: freedius would start successfully even if a mapped provider's API key is absent, and fail gracefully on first request to that provider (with fallback to other providers if configured).

## Detailed Findings

### 1. Startup Check: `checkRequiredEnvVars` (cmd/freedius/main.go:397-411)

```go
func checkRequiredEnvVars(cfg *config.Config) error {
    providers := cfg.ProvidersSnapshot()
    for name, m := range cfg.MappingsSnapshot() {
        p, ok := providers[m.ProviderName]
        if !ok {
            continue
        }
        if p.DefaultAPIKeyEnv != "" && os.Getenv(p.DefaultAPIKeyEnv) == "" {
            return fmt.Errorf(
                "%s env var required (mapping %q references provider %q)",
                p.DefaultAPIKeyEnv, name, m.ProviderName,
            )
        }
    }
    return nil
}
```

**Behavior**: Iterates only providers referenced by at least one mapping. If any referenced provider's env var is empty, returns a fatal error (exit code 1). This was already improved from the original version that checked ALL providers — see lessons.md entry "Adding New Providers: Auto-Inject + Env-Var Scope".

**Called at**: `main.go:141`, after config load, before server startup.

### 2. Runtime Adapter Checks (Already Lazy)

**Anthropic adapter** (`proxy/anthropic_compat.go:82-90`):
```go
apiKey := os.Getenv(provider.DefaultAPIKeyEnv)
if apiKey == "" {
    return &configError{
        err:     fmt.Errorf("%s adapter (anthropic-compat): env var %s is not set", ...),
        errType: "authentication_error",
    }
}
```

**OpenAI adapter** (`proxy/openai_compat.go:74-83`):
```go
if provider.DefaultAPIKeyEnv != "" {
    apiKey = os.Getenv(provider.DefaultAPIKeyEnv)
    if apiKey == "" {
        return &configError{
            err:     fmt.Errorf("%s adapter (openai-compat): env var %s is not set", ...),
            errType: "authentication_error",
        }
    }
}
```

Both return `configError` which the dispatcher (proxy.go:260-275) classifies with `status: 500` and `errType` from the error. When fallbacks are configured, the chain continues to the next provider. **This is already the lazy/first-use pattern.**

### 3. Web UI: Real-Time `EnvPresent` Indicator

**`proxy/web/handlers.go:334-339`**:
```go
envPresent := false
if p, ok := providers[m.ProviderName]; ok {
    proto = p.Protocol
    url = p.DefaultBaseURL
    if p.DefaultAPIKeyEnv != "" {
        envPresent = os.Getenv(p.DefaultAPIKeyEnv) != ""
    }
}
```

**Template** (`proxy/web/templates/mappings-table.html:22`):
```html
{{if .EnvPresent}}<span class="badge badge--status-ok">Active</span>
{{else}}<span class="badge badge--status-warn">Key Missing</span>{{end}}
```

This is read fresh on every page load — no startup dependency. If a user sets an env var and refreshes the dashboard, the badge updates immediately.

### 4. What "Web-Only Providers" Means

The question mentions "scoped to web-only providers" — in this codebase, **all providers serve the same dual purpose**: they're available for proxy traffic AND displayed in the web UI. There's no concept of "web-only" providers. The auto-injected providers (those with `DefaultBaseURL` in `providers.yaml` that aren't in the user's config) are injected by `applyDefaults()` in `config/defaults.go:17-37`, and they appear in the web dashboard's provider list. But they only trigger the startup check if a mapping references them.

The "not setup" state the user sees in the web dashboard is the `Key Missing` badge — it's purely informational, already lazy, and has no startup impact.

## Code References

- `cmd/freedius/main.go:141` - Call site of `checkRequiredEnvVars`
- `cmd/freedius/main.go:397-411` - The startup check function
- `proxy/anthropic_compat.go:82-90` - Runtime API key check in Anthropic adapter
- `proxy/openai_compat.go:74-83` - Runtime API key check in OpenAI adapter
- `proxy/proxy.go:247-275` - Dispatcher fallback handling of `configError`
- `proxy/web/handlers.go:334-339` - Real-time `EnvPresent` resolution
- `proxy/web/templates/mappings-table.html:22` - Active/Key Missing badge
- `config/defaults.go:17-37` - Auto-injection of providers with `DefaultBaseURL`
- `cmd/freedius/main_test.go:24-65` - Tests for `checkRequiredEnvVars`

## Architecture Insights

1. **Defense in depth already exists**: The startup check is a UX convenience (fail-fast), not a safety requirement. The adapters are already hardened against missing keys.

2. **Fallback chain handles auth failures gracefully**: A `configError{errType: "authentication_error"}` is treated like any other pre-write failure — the dispatcher tries the next fallback. This means even with the startup check removed, a multi-provider fallback chain degrades gracefully.

3. **The change is essentially one line**: Remove or conditionally skip `main.go:141`. The rest of the system already works correctly without it.

4. **Possible middle ground — warn instead of fail**: Replace the fatal `return err` with `logger.Warn(...)` to surface the issue without blocking startup. This preserves discoverability while removing the hard gate.

## Proposed Approaches

### Option A: Remove `checkRequiredEnvVars` entirely
- Delete the function and its call at `main.go:141`
- Delete tests in `main_test.go:24-65`
- Risk: None functional. Users lose the early "you forgot to set X" error on startup, but the web UI already shows "Key Missing" badges and the first request will get a clear error.

### Option B: Downgrade to a warning log
- Keep the function but replace `return fmt.Errorf(...)` with `logger.Warn("API key not set", ...)`
- Pros: Users still see the problem in startup logs; process starts anyway
- Cons: Log noise if intentional (e.g., provider only used as fallback-of-last-resort)

### Option C: Only check when there are NO fallbacks
- Only fatal-error if the primary provider of a mapping is missing a key AND it has no fallback chain. If fallbacks exist, skip the check (the chain will handle it).
- Pros: Preserves fail-fast for single-provider setups where the error would be confusing
- Cons: More complex logic; still prevents starting when the user might want to set the key later via the dashboard

**Recommendation**: Option B is the smallest useful change — one line from `return` to `logger.Warn`. It removes the hard startup gate while preserving operator visibility. Option A is fine for a more opinionated "everything is lazy" stance.

## Historical Context (from prior changes)

- `context/foundation/lessons.md` — "Adding New Providers: Auto-Inject + Env-Var Scope" lesson documents the prior evolution: `checkRequiredEnvVars` was narrowed from ALL providers to only those referenced by mappings. This change would be the logical next step in that trajectory.

## Related Research

No prior research documents address this specific topic.

## Open Questions

1. **Should `checkRequiredEnvVars` consider fallback chains?** If a mapping has fallbacks, is a missing primary key still worth warning about?
2. **Should the web dashboard offer a "set API key" affordance?** Currently it only shows status. A text field to paste a key (stored in env or config) would close the UX loop.
3. **Does the starter template (no config file) scenario change anything?** When freedius boots from the embedded starter template, it has no mappings — `checkRequiredEnvVars` is a no-op. The question only matters for users who have already configured mappings.

---
date: 2026-06-18T16:41:04Z
researcher: kiro
git_commit: 996b96d09a674f2bcdcdf70ff8646f342e35b8bf
branch: tui-dashboard
repository: pfrack/freedius
topic: "Extend TUI with mapping/model setup and plain error display"
tags: [research, codebase, tui, config-editing, error-display, bubble-tea, forms]
status: complete
last_updated: 2026-06-18
last_updated_by: kiro
---

# Research: Extend TUI with mapping/model setup and plain error display

**Date**: 2026-06-18T16:41:04Z
**Researcher**: kiro
**Git Commit**: 996b96d09a674f2bcdcdf70ff8646f342e35b8bf
**Branch**: tui-dashboard
**Repository**: pfrack/freedius

## Research Question

How to extend the freedius TUI dashboard with:
1. Interactive setup of **mappings** (family-based model routing, e.g. `opus` → `provider: go, model: deepseek-v4-pro`)
2. Interactive setup of **models** (exact-match model configurations)
3. Display of **plain error messages** (upstream error body text) alongside request status codes

## Summary

The TUI can be extended with interactive config editing and error display, but it requires building three pieces from scratch: a YAML serialization layer for `Config`, a text-input form UI (adding the `charm.land/bubbles/v2` dependency for input widgets), and error message capture in the event pipeline. The existing TUI plan (`tui-dashboard/plan.md:61`) explicitly deferred config editing to a future web UI, but nothing in the codebase architecture blocks adding it to the TUI now. The simplest path: add config editing forms as new tab 4 (Mode Selector) and tab 5 (Provider Selector) or as modal overlays on the existing Config tab.

## Detailed Findings

### 1. Current TUI Architecture

The Dashboard model ([`proxy/tui/model.go:58-70`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/proxy/tui/model.go#L58-L70)) is a flat state machine with no nested models:

| Field | Type | Purpose |
|---|---|---|
| `activeTab` | `int` | Current tab (0=Requests, 1=Providers, 2=Config) |
| `events` | `<-chan proxy.RequestEvent` | Event bus subscription channel |
| `eventLog` | `*ringBuffer` | Last 1000 request events |
| `config` | `*config.Config` | Read-only config snapshot |
| `registry` | `*proxy.Registry` | Stored but never rendered |
| `stats` | `statsData` | Aggregated request counters |
| `width`, `height` | `int` | Terminal dimensions |

**Tab constants** ([`proxy/tui/styles.go:51-54`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/proxy/tui/styles.go#L51-L54)):
```go
tabRequests  = 0
tabProviders = 1
tabConfig    = 2
```

**Update() message handling** ([`proxy/tui/model.go:98-135`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/proxy/tui/model.go#L98-L135)) uses a type switch on `tea.Msg` with no nested model delegation. Key press `"1"`/`"2"`/`"3"` switch tabs; tab/shift+tab cycle.

**View() dispatch** ([`proxy/tui/model.go:138-174`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/proxy/tui/model.go#L138-L174)) switches on `activeTab` to call the appropriate renderer.

**No form/input widgets exist** anywhere in the codebase. A grep for `TextInput`, `TextArea`, `textarea`, `textinput`, `form` across all `.go` files returns zero matches. The codebase uses `charm.land/bubbletea/v2` and `charm.land/lipgloss/v2` but **not** `charm.land/bubbles/v2` — which is where `textinput.Model`, `textarea.Model`, and other input widgets live.

**Entry point wiring** in [`tui.go:120-148`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/tui.go#L120-L150) creates the Dashboard with `tui.NewDashboard(bus.Subscribe(), cfg, registry)`. The `cfg` pointer is never written back to disk — the TUI has no save flow.

### 2. Config System — What Must Be Preserved for Editing

The `Config` struct ([`config/config.go:13-16`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/config/config.go#L13-L16)):
```go
type Config struct {
    Models   map[string]Model `yaml:"models"`
    Mappings map[string]Model `yaml:"mappings,omitempty"`
}
```

The `Model` struct ([`config/config.go:19-27`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/config/config.go#L19-L27)):
```go
type Model struct {
    Provider         string `yaml:"provider"`
    Model            string `yaml:"model"`
    BaseURL          string `yaml:"base_url,omitempty"`
    APIKeyEnv        string `yaml:"api_key_env,omitempty"`
    AnthropicVersion string `yaml:"anthropic_version,omitempty"`
    Protocol         string `yaml:"protocol,omitempty"`
    OriginalProvider string `yaml:"-"`  // NOT serialized — set at load time
}
```

**Critical: There is no Save/Marshal function.** The codebase only reads config (`Load()`) — never writes it. Building config editing requires creating YAML serialization from scratch.

**Validation rules** that must be enforced when saving ([`config/config.go:86-170`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/config/config.go#L86-L170)):

| Rule | Detail |
|---|---|
| `model` field required | Cannot be empty |
| `provider` field required | Cannot be empty |
| Provider in `KnownProviders` | Must be one of: anthropic, custom, go, mix, nim, openai, zen |
| No CR/LF/colon in model name | Security/validity check |
| BaseURL valid URL | Must parse, scheme must be http/https |
| BaseURL required for some providers | All providers except `nim` require `base_url` |
| APIKeyEnv no CR/LF/`=` | Security check |
| Protocol only "anthropic" or "openai" | Or empty (auto-detected) |

**Alias rewrites** — users write `provider: zen` but it rewrites to `mix` at load time ([`config/providers_gen.go:61-95`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/config/providers_gen.go#L61-L95)):
- `custom` → `mix` (pre-lookup, custom has no defaults)
- `zen` → `mix` (post-lookup, inherits `OPENCODE_API_KEY` first)
- `go` → `mix` (post-lookup, inherits `OPENCODE_API_KEY` first)
- `OriginalProvider` preserves the user's original choice for `checkRequiredEnvVars`

When writing config back, you **must** use `OriginalProvider` in the `provider:` field so aliases survive round-trip.

**Provider defaults** ([`config/providers_gen.go:21-35`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/config/providers_gen.go#L21-L35)) — nim gets auto-filled `base_url` + `api_key_env`. anthropic gets auto-filled `api_key_env`. go/zen get auto-filled `api_key_env`. custom/openai/mix get nothing.

**Env var check** ([`main.go:276-301`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/main.go#L276-L301)) — `checkRequiredEnvVars` looks for missing env vars and returns error. The TUI currently discards this error ([`tui.go:118`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/tui.go#L118): `_ = checkRequiredEnvVars(cfg)`) instead of blocking. This is the right behavior for a TUI-based config editor — missing env vars can be shown as warnings.

**KnownProviders** are generated from `providers.yaml` ([`providers.yaml:32-70`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/providers.yaml#L32-L70)). The 7 providers:

| Provider | Behavior | Has defaults? | Requires base_url? | Alias? |
|---|---|---|---|---|
| `nim` | openai | BaseURL + APIKeyEnv | No | No |
| `zen` | mix | APIKeyEnv only | Yes | → `mix` |
| `go` | mix | APIKeyEnv only | Yes | → `mix` |
| `custom` | mix | None | Yes | → `mix` |
| `openai` | openai | None | Yes | No |
| `anthropic` | anthropic | APIKeyEnv only | Yes | No |
| `mix` | mix | None | Yes | No |

### 3. Error Propagation — Current Gaps

The `RequestEvent` struct ([`proxy/eventbus.go:13-22`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/proxy/eventbus.go#L13-L22)) has **no error message field**:

```go
type RequestEvent struct {
    RequestID       string
    Model           string        // user-requested model name from body
    Provider        string        // matched provider
    Status          int           // HTTP status (defaults 200)
    Latency         time.Duration
    MatchedProvider string
    MatchedModel    string
    Timestamp       time.Time
}
```

Only `Status int` is captured. The error body text from `writeErrorJSON` and `writeAnthropicError` is lost.

**Error generation sites** — there are two formats:

1. **freedius-format** via `writeErrorJSON` ([`proxy/proxy.go:269-301`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/proxy/proxy.go#L269-L301)): `{"error":"<code>", "message":"<human>", "request_id":"<id>", "detail":"<optional>"}`

   Called from `Dispatcher.ServeHTTP` at 10 locations for: method_not_allowed, unsupported_content_type, body_too_large, body_unreadable, empty_body, invalid_json, missing_model, no_match, provider_not_registered.

2. **Anthropic-format** via `writeAnthropicError` ([`proxy/errors.go:29-43`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/proxy/errors.go#L29-L43)): `{"type":"error", "error":{"type":"<errType>", "message":"<msg>"}}`

   Used for: adapter failures (transport error → "upstream not reachable"), upstream error translation (via `translateUpstreamError` at [`proxy/errors.go:50-90`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/proxy/errors.go#L50-L90)), and freediusErrorHandler transport errors.

**Upstream error body** — `translateUpstreamError` reads the first 256 bytes of the upstream response body to use as the error message ([`proxy/errors.go:52-54`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/proxy/errors.go#L52-L54)). This is exactly the "plain errors" the user wants shown — but it's discarded after being written to the response.

**VerboseErrors gating** — the `WithDetail` pattern ([`proxy/proxy.go:287-289`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/proxy/proxy.go#L287-L289)) only includes detail in the response when `VerboseErrors=true`. No detail is ever captured in events regardless of the flag.

**TUI display gap** — the [`renderRequestsTab`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/proxy/tui/views.go#L33-L83) shows only: timestamp, color-coded status, model, provider, latency. No error message column.

**Minimal change to capture errors**: Add an `ErrorMessage string` field to `RequestEvent`, then in `EventBusMiddleware` ([`proxy/proxy.go:448-474`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/proxy/proxy.go#L448-L474)), after `next.ServeHTTP`, check `ww.code >= 400` and read/sniff the error body from the wrapped response — or, more cleanly, pass the error message alongside the status in a new `wroteHeaderResponseWriter` field or via a response header (`X-Freedius-Error-Message`).

### 4. Historical Context — Prior Decisions

The existing TUI dashboard plan ([`context/changes/tui-dashboard/plan.md:57-64`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/context/changes/tui-dashboard/plan.md#L57-L64)) explicitly **deferred config editing**:
> - **No config editing in the TUI** — the Config tab is read-only display.
> - Config editing was assigned to a future **web dashboard** (v3) in the research decision matrix.

The TUI research ([`context/changes/tui-dashboard/research.md:151-162`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/context/changes/tui-dashboard/research.md#L151-L162)) proposed config editing as a **web UI** feature:
> - Config editing → ✅ Web column only

The roadmap ([`context/foundation/roadmap.md:214`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/context/foundation/roadmap.md#L214)) parks web UI in v2/v3:
> - **Web UI** — "v1 is config-file-only per PRD §Non-Goals. v2 concern."

The PRD ([`context/foundation/prd.md:107`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/context/foundation/prd.md#L107)):
> - No web UI in v1. Config file only. Web UI is a v2 concern.

Despite these historical decisions, **nothing technical blocks** adding config editing to the TUI. The original deferral was based on scope discipline, not architectural constraints. The TUI already holds a `*config.Config` pointer, has access to the config file path, and uses Bubble Tea — the only missing pieces are input widgets and YAML serialization.

**Lessons from `context/foundation/lessons.md` that apply:**
- `custom` → `mix` rewrite ([`lessons.md:15-19`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/context/foundation/lessons.md#L15-L19)): config editing must handle alias rewrites correctly — use `OriginalProvider` when writing back.
- Adapter Return Contract ([`lessons.md:33-43`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/context/foundation/lessons.md#L33-L43)): events emit after response — safe for error capture.

## Code References

### TUI Model & Views
- [`proxy/tui/model.go:58-70`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/proxy/tui/model.go#L58-L70) — Dashboard struct with all fields
- [`proxy/tui/model.go:98-135`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/proxy/tui/model.go#L98-L135) — Update() message type switch
- [`proxy/tui/model.go:138-174`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/proxy/tui/model.go#L138-L174) — View() tab dispatch
- [`proxy/tui/styles.go:51-54`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/proxy/tui/styles.go#L51-L54) — Tab constants (tabRequests=0, tabProviders=1, tabConfig=2)
- [`proxy/tui/views.go:33-83`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/proxy/tui/views.go#L33-L83) — renderRequestsTab — no error message column
- [`proxy/tui/views.go:112-135`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/proxy/tui/views.go#L112-L135) — renderConfigTab — read-only model display
- [`proxy/tui/views.go:137-156`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/proxy/tui/views.go#L137-L156) — renderStatsBar — shows error count/rate

### Entry Point & Wiring
- [`tui.go:120-150`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/tui.go#L120-L150) — runTUI — wires config/registry/bus to Dashboard
- [`tui.go:118`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/tui.go#L118) — checkRequiredEnvVars error is discarded in TUI mode
- [`tui.go:38-42`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/tui.go#L38-L42) — flag.FlagSet for `tui` subcommand
- [`main.go:74-75`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/main.go#L74-L75) — dispatch: `case "tui": return runTUI(args)`

### Config System
- [`config/config.go:13-27`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/config/config.go#L13-L27) — Config and Model structs
- [`config/config.go:30-55`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/config/config.go#L30-L55) — Load() pipeline (read → unmarshal → defaults → validate)
- [`config/config.go:86-170`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/config/config.go#L86-L170) — validateModel() — all validation rules
- [`config/providers_gen.go:8-16`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/config/providers_gen.go#L8-L16) — KnownProviders map
- [`config/providers_gen.go:40-47`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/config/providers_gen.go#L40-L47) — requireBaseURL map
- [`config/providers_gen.go:21-35`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/config/providers_gen.go#L21-L35) — knownProviderDefaults
- [`config/providers_gen.go:61-95`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/config/providers_gen.go#L61-L95) — applyEntryDefaults alias rewrite logic
- [`config/defaults.go:25-32`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/config/defaults.go#L25-L32) — applyDefaults iteration
- [`main.go:276-301`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/main.go#L276-L301) — checkRequiredEnvVars

### Error Flow
- [`proxy/eventbus.go:13-22`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/proxy/eventbus.go#L13-L22) — RequestEvent struct (no ErrorMessage field)
- [`proxy/proxy.go:269-301`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/proxy/proxy.go#L269-L301) — writeErrorJSON and WithDetail
- [`proxy/proxy.go:448-474`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/proxy/proxy.go#L448-L474) — EventBusMiddleware — emit site
- [`proxy/proxy.go:71-247`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/proxy/proxy.go#L71-L247) — Dispatcher.ServeHTTP — all error paths
- [`proxy/errors.go:29-43`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/proxy/errors.go#L29-L43) — writeAnthropicError
- [`proxy/errors.go:50-90`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/proxy/errors.go#L50-L90) — translateUpstreamError — reads 256 bytes of upstream body

### Historical Context
- [`context/changes/tui-dashboard/plan.md:57-64`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/context/changes/tui-dashboard/plan.md#L57-L64) — "What We're NOT Doing" — explicit no-config-editing
- [`context/changes/tui-dashboard/research.md:151-165`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/context/changes/tui-dashboard/research.md#L151-L165) — Config editing assigned to Web UI only
- [`context/foundation/roadmap.md:214`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/context/foundation/roadmap.md#L214) — Web UI parked in v2/v3
- [`context/foundation/lessons.md:15-19`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/context/foundation/lessons.md#L15-L19) — custom→mix rewrite lesson
- [`providers.yaml:32-70`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/providers.yaml#L32-L70) — Provider metadata source of truth

## Architecture Insights

### Pattern 1: Flat state machine — no nested models

The Dashboard is a single flat model with a type-switch dispatcher. Adding config editing requires either:
- **Modal overlay approach**: Add fields to `Dashboard` for an active form (e.g., `editingField string`, `editMode bool`, `textInput textinput.Model`), and when a form is active, forward keystrokes to it in `Update()` and render it as an overlay in `View()`.
- **New tab approach**: Add new tab constants (`tabSetupMapping = 3`, `tabSetupModel = 4`) with dedicated render/update functions. Each gets its own text input model.

The modal overlay approach is preferred — it preserves existing tab navigation and feels more natural for quick edits.

### Pattern 2: No config save path

The config flows through: `os.ReadFile → yaml.Unmarshal → applyDefaults → validate → use read-only`. There is no reverse path. Building one requires:
1. **YAML serialization**: Use `goccy/go-yaml` (already in go.mod) to marshal `Config` back
2. **OriginalProvider recovery**: Before marshal, replace `Model.Provider` with `Model.OriginalProvider` for alias entries (custom, zen, go)
3. **Omit empty optional fields**: Use `omitempty` tags (already present) — the YAML marshaler respects them
4. **Validation before save**: Call `validate()` to ensure the edited config is valid before writing

### Pattern 3: Error messages are discarded after writing to response

Both `writeErrorJSON` and `writeAnthropicError` write directly to `http.ResponseWriter`. The error text exists transiently in `message`/`msg` variables but is never captured. The `EventBusMiddleware` fires after `next.ServeHTTP()` returns but only reads the status code from `wroteHeaderResponseWriter`.

The cleanest capture approach: pass error messages through response headers or a dedicated error field on `wroteHeaderResponseWriter`. The `Dispatcher.writeErrorJSON` already uses `WithDetail` as an error metadata mechanism — this same pattern could feed into the event bus.

### Pattern 4: Bubble Tea v2 input requires `bubbles` dependency

The codebase currently has `charm.land/bubbletea/v2` and `charm.land/lipgloss/v2` but not `charm.land/bubbles/v2`. Adding text input requires `go get charm.land/bubbles/v2`. Bubble Tea v2's key press handling is through `tea.KeyPressMsg` (not `tea.KeyMsg` from v1), and input widgets receive keystrokes via their own `Update(tea.Msg)` method.

## Historical Context (from prior changes)

- [`context/changes/tui-dashboard/plan.md:61`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/context/changes/tui-dashboard/plan.md#L61) — "No config editing in the TUI — the Config tab is read-only display." This was a scope decision, not a technical blocker.
- [`context/changes/tui-dashboard/research.md:151-152`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/context/changes/tui-dashboard/research.md#L151-L152) — Hybrid approach: TUI for monitoring, Web for config editing. Config editing moved to TUI now overrides this.
- [`context/foundation/roadmap.md:28-39`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/context/foundation/roadmap.md#L28-L39) — V-01 `tui-dashboard` is listed as proposed (but actual change.md says implemented).
- [`context/foundation/lessons.md:15-19`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/context/foundation/lessons.md#L15-L19) — custom→mix rewrite: config editing must preserve alias semantics.

## Related Research

- [`context/changes/tui-dashboard/research.md`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/context/changes/tui-dashboard/research.md) — Original TUI UI approach decision (TUI vs Web vs GUI)
- [`context/changes/tui-dashboard/plan.md`](https://github.com/pfrack/freedius/blob/996b96d09a674f2bcdcdf70ff8646f342e35b8bf/context/changes/tui-dashboard/plan.md) — Phase-by-phase implementation plan for the existing TUI

## Open Questions

1. **Scope of config editing**: Should the TUI support editing only `models` and `mappings` entries, or also global settings (provider defaults, protocol overrides)? The existing `Config` struct only has `Models` and `Mappings`.
2. **Provider-selector UX**: How should the TUI present the 7 known providers? A simple list picker, or a multi-field form showing each provider's behavior, default env var, and whether it's an alias?
3. **Save vs Apply**: Should config edits be saved to disk immediately, or applied in-memory only for the current session? In-memory-only would avoid the YAML serialization challenge but changes would be lost on restart.
4. **Registry reload on config change**: If config is edited in-memory, the `Dispatcher` and `Registry` would need to be updated to reflect the new mappings. Currently the `Dispatcher` holds a `*config.Config` pointer — changing the fields on the shared pointer would take effect immediately. But if a model is *added* or *removed*, the map mutation must be visible to the dispatcher too.
5. **Error message deduplication**: If two requests hit the same error within seconds (e.g., rate limiting), the TUI could show duplicate error messages. Should there be deduplication or aggregation of similar errors?

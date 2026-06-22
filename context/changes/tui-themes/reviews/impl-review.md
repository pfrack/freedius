<!-- IMPL-REVIEW-REPORT -->
# Implementation Review: TUI Themes

- **Plan**: context/changes/tui-themes/plan.md
- **Scope**: Phases 1-3 (full plan)
- **Date**: 2026-06-22
- **Verdict**: NEEDS ATTENTION
- **Findings**: 0 critical, 3 warnings, 7 observations

## Verdicts

| Dimension | Verdict |
|-----------|---------|
| Plan Adherence | PASS |
| Scope Discipline | PASS |
| Safety & Quality | WARNING |
| Architecture | PASS |
| Pattern Consistency | PASS |
| Success Criteria | PASS |

Automated checks: `go build` ✅, `go test` ✅, `go vet` ✅, `go test ./...` ✅.

## Findings

### W1 — `NewAttachDashboard` silently ignores the user's configured theme

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Safety & Quality
- **Location**: proxy/tui/model.go:190; cmd/freedius/attach.go:26-32
- **Detail**: In-process TUI (`NewDashboard`) honors `cfg.Theme`. Attach TUI (`NewAttachDashboard`) hardcodes `theme := resolveTheme("")`, so a user with `theme: zenburn` in their `freedius.yaml` sees the default theme when running `freedius attach`. The attach entry point does not pass `cfgPath`, so the theme can't currently be recovered. The plan only wired `cfg.Theme` through `NewDashboard` (Phase 3); attach-mode behavior is undocumented and asymmetric.
- **Fix A ⭐ Recommended**: Thread the resolved theme name from the daemon to the attach client over IPC (analogous to how `cfgPath` and host/port are already passed). Update `NewAttachDashboard` signature to accept `themeName string` and pass `d.styles` (or re-resolve locally if config is available).
  - Strength: Removes the silent inconsistency; user's `theme: zenburn` would apply in both modes.
  - Tradeoff: Requires a small IPC payload change; tests for `NewAttachDashboard` need updating.
  - Confidence: HIGH — pattern is established for cfgPath/host/port.
  - Blind spot: Backward compat with older daemons that don't send the theme.
- **Fix B**: Document the limitation at `attach.go:29` next to the `""` literal and move on. Add a comment so future maintainers see the split.
  - Strength: Cheapest; no behavior change.
  - Tradeoff: User-facing inconsistency persists; only a paper trail.
  - Confidence: MEDIUM — depends on whether this matters to users.
  - Blind spot: Doesn't fix the UX surprise.
- **Decision**: FIXED

### W2 — `resolveTheme` fallback path is untested

- **Severity**: ⚠️ WARNING
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Safety & Quality
- **Location**: proxy/tui/styles.go:123-130; absence in proxy/tui/model_test.go
- **Detail**: Plan §Phase 3 success criterion 3.6 calls for `theme: nonexistent` to fall back to default silently. Only `TestDashboard_CycleTheme` exists, which only walks the registered themes. A typo like `theme: zenburrn` is silently swallowed with no signal to the user.
- **Fix**: Add a small test:
  ```go
  func TestResolveTheme_UnknownFallsBackToDefault(t *testing.T) {
      th := resolveTheme("nonexistent")
      if th.Name != "default" {
          t.Errorf("resolveTheme(\"nonexistent\") = %q, want \"default\"", th.Name)
      }
  }
  ```
  - Strength: 5-line test; closes documented gap.
  - Tradeoff: None.
  - Confidence: HIGH
  - Blind spot: Doesn't address the underlying UX problem (typo invisibility).
- **Decision**: FIXED

### W3 — `NewStyles` doesn't validate palette completeness

- **Severity**: ⚠️ WARNING
- **Impact**: 🔎 MEDIUM — real tradeoff; pause to reason through it
- **Dimension**: Safety & Quality
- **Location**: proxy/tui/styles.go:135-197
- **Detail**: `NewStyles` blindly feeds every `AdaptiveColor` slot through `lipgloss.LightDark`. A future theme added to `themeRegistry` with a missing slot would produce a `lipgloss.Style` with `nil` foreground/background, and lipgloss v2 tolerates nil silently — the failure is purely visual (missing colors). All four currently-registered themes populate all 14 slots (7 light + 7 dark), so this is a latent footgun, not an active bug.
- **Fix**: Add a `validate(p Palette) error` step that returns an error if any `AdaptiveColor` has both fields nil. Call it at the top of `NewStyles` (or at registry init).
  - Strength: Cheap insurance against future theme additions.
  - Tradeoff: Adds a small validation path; defensive only.
  - Confidence: HIGH — pattern used in other init paths.
  - Blind spot: Could be over-eager (e.g., refusing light terminals where one of light/dark should be nil intentionally — but current themes never do this).
- **Decision**: FIXED

### O1 — `Styles` struct has 20 fields; plan text said 16 (then 17)

- **Severity**: 👁️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: proxy/tui/styles.go:37-58 vs context/changes/tui-themes/plan.md:60
- **Detail**: Plan's Phase 1 contract said "Styles struct with 16 exported fields … plus OverlayBgStyle" (i.e., 17). Implementation has 20: the planned OverlayBgStyle plus `LogInfoStyle` and `LogDebugStyle`, which are functionally required for log-level rendering in `renderLogTab`. Additions are documented in commit messages ("fix overlayModal outlier" and "theme-visible text, stats bar, and log coloring") but the plan doc itself is stale.
- **Fix**: Update plan §Phase 1 contract text to say "20 fields" (or "≥16 fields, including OverlayBgStyle, LogInfoStyle, LogDebugStyle"). Or just acknowledge the doc drift and leave the implementation as-is.
  - Strength: Accurate plan.
  - Tradeoff: None.
  - Confidence: HIGH
  - Blind spot: None.
- **Decision**: FIXED

### O2 — `Theme.Name` stutters; rest of `Config` avoids this

- **Severity**: 👁️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern Consistency
- **Location**: proxy/tui/styles.go:30-33
- **Detail**: `Provider.Behavior`, `Provider.DefaultBaseURL`, `Mapping.ProviderName` all avoid repeating the struct name. `Theme.Name` follows a different convention (name is unambiguous in context). Defensible — the plan explicitly chose `Name`.
- **Fix**: Leave as-is (intentional design choice acknowledged by plan).
  - Strength: Stable, plan-approved.
  - Tradeoff: Minor stylistic divergence.
  - Confidence: HIGH
  - Blind spot: None.
- **Decision**: FIXED

### O3 — `themeRegistry` pointer aliasing is fragile

- **Severity**: 👁️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Architecture
- **Location**: proxy/tui/styles.go:81, 123-130; proxy/tui/model.go:583, 589
- **Detail**: `resolveTheme` and `cycleTheme` return/store `*Theme` pointers into the backing array of the package-level `themeRegistry` slice. The slice is never mutated today, and the comment at styles.go:80 documents the immutability contract. A future "load themes from disk" feature would silently dangle previously-returned pointers.
- **Fix**: Either (a) change `resolveTheme` to return a `Theme` value or `*Theme` to a heap-allocated copy, or (b) convert `themeRegistry` to a `[N]Theme` array. Both eliminate the aliasing risk without changing call sites.
  - Strength: Future-proof.
  - Tradeoff: Minor API change (callers may need updates).
  - Confidence: MEDIUM — current usage is safe; risk is purely future-looking.
  - Blind spot: None today.
- **Decision**: FIXED

### O4 — `HasDarkBackground` can silently pick the wrong branch on terminals without OSC 11 support

- **Severity**: 👁️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Safety & Quality
- **Location**: proxy/tui/model.go:149, 189
- **Detail**: `lipgloss.HasDarkBackground` falls back to `false` (light) on terminals that don't support OSC 11. For the default theme, this means Light variants (brighter ANSI codes) are used — slightly different from the pre-theme TUI. Non-default themes (Light == Dark) are unaffected. Pre-existing limitation of lipgloss, not a regression in this change, but the plan's "Visual output identical to before" (Phase 1 §1.4) is contingent on the user's terminal supporting OSC 11.
- **Fix**: Add a startup log line reporting the detected `isDark` value, or document the requirement in a comment near the `HasDarkBackground` call.
  - Strength: Diagnosability.
  - Tradeoff: None.
  - Confidence: HIGH
  - Blind spot: Doesn't actually fix the rendering; just surfaces the cause.
- **Decision**: FIXED

### O5 — `cycleTheme`/`resolveTheme` linear scan is `O(n)`

- **Severity**: 👁️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Architecture
- **Location**: proxy/tui/styles.go:123-130; proxy/tui/model.go:579-592
- **Detail**: With 4 themes, the linear scan is irrelevant. Plan's "What We're NOT Doing" caps the registry at ~5–10 themes without a modal picker. A map keyed by name would be `O(1)`.
- **Fix**: Leave as-is. Flag if/when the registry crosses ~8 entries.
  - Strength: Matches current scale.
  - Tradeoff: None.
  - Confidence: HIGH
  - Blind spot: None.
- **Decision**: FIXED

### O6 — `renderForm` takes `*Dashboard` while siblings take `styles Styles`

- **Severity**: 👁️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Pattern Consistency
- **Location**: proxy/tui/views.go:310
- **Detail**: Plan called this out explicitly as an intentional choice. `renderForm` reads from `d.styles` because it's tightly coupled to the dashboard (form fields, focus state). Sibling renderers take `styles Styles` as a separate parameter. Slight inconsistency in API shape, but justified.
- **Fix**: Leave as-is (plan acknowledged).
  - Strength: Stable, plan-approved.
  - Tradeoff: Minor API divergence.
  - Confidence: HIGH
  - Blind spot: None.
- **Decision**: FIXED

### O7 — Help modal entry ordering: `Ctrl+T` between `L` and `Ctrl+S`

- **Severity**: 👁️ OBSERVATION
- **Impact**: 🏃 LOW — quick decision; fix is obvious and narrowly scoped
- **Dimension**: Plan Adherence
- **Location**: proxy/tui/help.go:22-23
- **Detail**: Plan said insert "after L, before form-specific entries". Both `Ctrl+T` and `Ctrl+S` are global (non-form), so placing `Ctrl+T` between `L` and `Ctrl+S` is internally consistent. No user impact.
- **Fix**: None.
  - Strength: —
  - Tradeoff: —
  - Confidence: HIGH
  - Blind spot: None.
- **Decision**: FIXED

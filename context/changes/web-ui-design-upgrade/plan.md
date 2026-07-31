# Web UI Design Upgrade — Implementation Plan

## Overview

Upgrade the freedius admin dashboard from a generic AI-template aesthetic to a premium, intentional design. The existing UI has solid foundations (responsive sidebar, empty states, focus-visible, HTMX-powered interactions) but uses every default pattern that marks AI-generated interfaces: system font stack, indigo accent, pure-black shadows, uppercase everywhere, no texture, no pressed states.

This plan applies targeted visual upgrades without rewriting functionality, following the redesign audit's priority order: font → color → states → layout → components → typography polish.

## Current State Analysis

- **Stack**: Go `html/template` + vanilla CSS custom properties + HTMX + vanilla JS
- **Pages**: Dashboard (stat cards + mapping summary + providers chips), Mappings (route cards with fallback chains), Providers (CRUD table), Logs (SSE streaming)
- **Styling**: Single `app.css` file (~750 lines), CSS custom properties for theming, dark-mode-first with `prefers-color-scheme: light` override
- **Existing strengths**: Semantic HTML, focus-visible, empty states, staggered card animations, reduced-motion support, responsive mobile sidebar

### Key Discoveries:

- Font: `-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto` — zero character
- Accent: `#6366f1` (indigo) — the #1 "AI purple" fingerprint
- Shadows: `rgba(0,0,0,0.3/0.4/0.5)` — untinted pure black
- `h1` only `1.5rem` — lacks display presence
- No `max-width` on `<main>` — content stretches infinitely on wide monitors
- Fallback fieldset uses inline `style="border:1px solid #555;..."` — code smell
- Toast uses `z-index: 9999` — no controlled z-index scale
- No favicon, no meta description, no skip-to-content link
- Table headers and badges all use `text-transform: uppercase` — overused
- No `:active` state on buttons — clicks feel dead
- No `scroll-behavior: smooth`
- No texture or grain — completely flat surfaces

## Desired End State

The dashboard looks and feels intentional rather than template-generated. Geist font gives it a modern developer-tool identity. Teal accent reads as "infrastructure tool" rather than "AI product." Every interactive element provides tactile feedback. The layout respects wide monitors. Accessibility gaps (skip-link, favicon) are closed. Code quality improves: no inline styles, proper z-index scale, semantic structure.

Verification: load the dashboard in a browser and confirm — teal accent visible, Geist font rendering, pressing buttons shows scale feedback, main content has a max-width container, subtle grain texture visible on dark backgrounds.

## What We're NOT Doing

- Migrating to Tailwind or any CSS framework
- Changing the HTML template structure beyond the `<head>` additions
- Adding new pages or features
- Changing HTMX behavior or JavaScript logic beyond the fallback-row DOM creation
- Redesigning the sidebar navigation pattern (it's appropriate for this admin UI)
- Adding animations beyond the existing stagger + the new `:active` feedback

## Implementation Approach

Work with the existing vanilla CSS custom properties system. All visual changes happen through variable swaps and targeted rule additions in `app.css`. Template changes are limited to `<head>` meta tags and removing inline styles. This keeps the diff small, reviewable, and zero-risk to functionality.

## Phase 1: Font and color foundation

### Overview

Swap the font stack and accent color — the two highest-impact, lowest-risk changes.

### Changes Required:

#### 1. Font swap

**File**: `proxy/web/static/app.css`

**Intent**: Replace the generic system font stack with Geist Sans/Mono. Geist is a modern, geometrically clean typeface with excellent legibility at small sizes — ideal for a data-heavy admin dashboard.

**Contract**: `--font-sans` variable changes to `'Geist', -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif`; `--font-mono` changes to `'Geist Mono', 'Fira Code', 'Cascadia Code', monospace`. System fonts remain as fallbacks.

#### 2. Font CDN import

**File**: `proxy/web/templates/layout.html`

**Intent**: Load Geist font from jsDelivr CDN with preconnect for performance.

**Contract**: Add `<link rel="preconnect" href="https://cdn.jsdelivr.net" crossorigin>` and two `<link rel="stylesheet">` tags for `geist-sans/style.css` and `geist-mono/style.css` before `app.css`.

#### 3. Accent color swap

**File**: `proxy/web/static/app.css`

**Intent**: Replace indigo (`#6366f1`) with teal (`#0d9488`) across both dark and light mode. Teal reads as "infrastructure tool" rather than "AI product" while maintaining sufficient contrast ratios.

**Contract**: Dark mode: `--accent: #0d9488`, `--accent-hover: #2dd4bf`, `--accent-subtle: rgba(13,148,136,0.12)`. Light mode: `--accent: #0f766e`, `--accent-hover: #0d9488`, `--accent-subtle: rgba(15,118,110,0.1)`.

### Success Criteria:

#### Automated Verification:

- Templates parse without error: `go test ./proxy/web/ -run TestEmbed`
- Full test suite passes: `go test ./...`

#### Manual Verification:

- Geist font renders in the browser (check DevTools → Computed → font-family)
- Teal accent visible on active nav link, primary buttons, focus rings
- Light mode accent is slightly darker teal for contrast

**Implementation Note**: After completing this phase and all automated verification passes, pause here for manual confirmation from the human that the manual testing was successful before proceeding to the next phase.

---

## Phase 2: Interaction polish and layout

### Overview

Add pressed-state feedback on buttons, constrain main content width, enable smooth scroll, and tint shadows.

### Changes Required:

#### 1. Button active state

**File**: `proxy/web/static/app.css`

**Intent**: Add `:active` pseudo-class to `.btn` so clicks provide tactile feedback.

**Contract**: `.btn:active { transform: translateY(1px) scale(0.98); }` — placed immediately after the `.btn--primary:hover` rule.

#### 2. Main content max-width

**File**: `proxy/web/static/app.css`

**Intent**: Prevent content from stretching infinitely on wide screens.

**Contract**: Add `max-width: 1200px` to the `main` rule.

#### 3. Smooth scroll

**File**: `proxy/web/static/app.css`

**Intent**: Enable smooth scrolling for anchor navigation.

**Contract**: Add `html { scroll-behavior: smooth; }` before the `body` rule. Add `@media (prefers-reduced-motion: reduce) { html { scroll-behavior: auto; } }` override.

#### 4. Tinted shadows

**File**: `proxy/web/static/app.css`

**Intent**: Replace pure-black shadows with zinc-tinted shadows that blend with the dark background.

**Contract**: `--shadow-sm: 0 1px 2px rgba(9,9,11,0.5)`, `--shadow-md: 0 4px 12px rgba(9,9,11,0.6)`, `--shadow-lg: 0 8px 24px rgba(9,9,11,0.7)`.

#### 5. Z-index scale

**File**: `proxy/web/static/app.css`

**Intent**: Replace arbitrary z-index values with a controlled custom-property scale.

**Contract**: Add variables `--z-overlay: 50`, `--z-sidebar: 100`, `--z-hamburger: 150`, `--z-dialog: 200`, `--z-toast: 300`. Replace hardcoded values: sidebar `100` → `var(--z-sidebar)`, hamburger `200` → `var(--z-hamburger)`, overlay `90` → `var(--z-overlay)`, toast `9999` → `var(--z-toast)`.

### Success Criteria:

#### Automated Verification:

- Full test suite passes: `go test ./...`

#### Manual Verification:

- Clicking a primary button shows slight downward shift
- Main content stops at ~1200px on a wide monitor
- Anchor clicks scroll smoothly
- Shadows blend with background (not a separate black layer)

**Implementation Note**: After completing this phase and all automated verification passes, pause here for manual confirmation from the human that the manual testing was successful before proceeding to the next phase.

---

## Phase 3: Typography and component refinement

### Overview

Increase headline presence, add tabular figures, reduce uppercase overuse, remove inline styles, add noise texture.

### Changes Required:

#### 1. Headline sizing

**File**: `proxy/web/static/app.css`

**Intent**: Make h1 larger and heavier to create proper visual hierarchy.

**Contract**: `h1` changes to `font-size: 1.75rem`, `letter-spacing: -0.03em`, `line-height: 1.2`, plus `text-wrap: balance`. `h2` adds `letter-spacing: -0.02em` and `text-wrap: balance`.

#### 2. Tabular figures on stat values

**File**: `proxy/web/static/app.css`

**Intent**: Numbers in stat cards should use fixed-width digits so they don't jump when values change.

**Contract**: Add `font-variant-numeric: tabular-nums` to `.card--stat .card-value`.

#### 3. Reduce uppercase

**File**: `proxy/web/static/app.css`

**Intent**: Table headers currently use uppercase which is overused alongside badge uppercase and card-label uppercase. Switch table headers to sentence case.

**Contract**: Remove `text-transform: uppercase` and `letter-spacing: 0.05em` from `th`. Replace with `font-size: 0.75rem` and `letter-spacing: 0.01em`.

#### 4. Noise texture overlay

**File**: `proxy/web/static/app.css`

**Intent**: Break the perfectly flat digital surfaces with subtle grain.

**Contract**: Add `body::before` pseudo-element with `position: fixed; inset: 0; pointer-events: none; z-index: 9999; opacity: 0.025` and an inline SVG `feTurbulence` noise pattern as `background-image`.

#### 5. Remove inline styles from fallback fieldset

**File**: `proxy/web/templates/mappings.html`

**Intent**: Replace hardcoded `style="border:1px solid #555;..."` with CSS classes.

**Contract**: Fieldset gets `class="fallback-fieldset"`, legend loses inline styles. New CSS classes `.fallback-fieldset`, `.fallback-row`, `.fallback-row__inner` in `app.css`. JS `addFallbackRow()` updated to use `className` assignments instead of `.style.*` properties. Remove button's `removeBtn` listener uses `.closest('.fallback-row')` instead of `div[style*="margin-bottom"]`.

### Success Criteria:

#### Automated Verification:

- Full test suite passes: `go test ./...`

#### Manual Verification:

- h1 text is visibly larger and tighter-spaced
- Stat card numbers use fixed-width digits
- Table headers no longer screaming uppercase
- Subtle grain texture visible (zoom to 200% to confirm)
- Adding/removing fallback rows in mapping dialog still works correctly

**Implementation Note**: After completing this phase and all automated verification passes, pause here for manual confirmation from the human that the manual testing was successful before proceeding to the next phase.

---

## Phase 4: Accessibility and meta

### Overview

Add skip-to-content link, favicon, meta description — closing the remaining audit gaps.

### Changes Required:

#### 1. Skip-to-content link

**Files**: `proxy/web/templates/layout.html`, `proxy/web/static/app.css`

**Intent**: Add a hidden skip-link for keyboard navigation that becomes visible on focus.

**Contract**: `<a href="#main-content" class="skip-link">Skip to content</a>` as first child of `<body>`. `<main>` gets `id="main-content"`. CSS `.skip-link` positions off-screen, `.skip-link:focus` brings it visible at top-left.

#### 2. Favicon

**File**: `proxy/web/templates/layout.html`

**Intent**: Add a branded favicon so the browser tab isn't blank.

**Contract**: Inline SVG data URI favicon: `<link rel="icon" href="data:image/svg+xml,...">` using a hexagon (⬡) character.

#### 3. Meta description

**File**: `proxy/web/templates/layout.html`

**Intent**: Add proper meta description for the page.

**Contract**: `<meta name="description" content="freedius — LLM proxy dashboard for managing providers, mappings, and fallback chains">`.

### Success Criteria:

#### Automated Verification:

- Full test suite passes: `go test ./...`

#### Manual Verification:

- Tab into the page with keyboard — "Skip to content" link appears
- Browser tab shows hexagon favicon
- View page source confirms meta description present

---

## Testing Strategy

### Unit Tests:

- Template parsing: `go test ./proxy/web/ -run TestEmbed` verifies all templates still parse after HTML changes
- Handler tests: existing handler tests verify template rendering with data

### Integration Tests:

- Full `go test ./...` confirms no Go compilation or template issues

### Manual Testing Steps:

1. Load dashboard at `localhost:<port>` — confirm Geist font rendering
2. Check accent color on nav, buttons, focus rings — should be teal
3. Click a button — observe slight scale/translate feedback
4. Resize browser wide — content constrained at ~1200px
5. Tab through page — skip-link appears, focus rings visible
6. Check browser tab — favicon present
7. Open mapping dialog → add fallback → remove fallback — works without error
8. Check DevTools console — no JS errors

## Performance Considerations

- Geist font loaded from CDN with `preconnect` — first paint may FOUT briefly, then render. Acceptable tradeoff for a local admin tool (not public-facing).
- Noise overlay uses SVG `feTurbulence` with `pointer-events: none` — zero interaction cost, negligible paint cost.
- No new JS dependencies added.

## References

- Geist font: https://vercel.com/font
- Redesign audit checklist: applied from the user-provided skill document
- Prior UI work: `context/archive/2026-07-05-web-ui-redesign/`

## Progress

> Convention: `- [ ]` pending, `- [x]` done. Append ` — <commit sha>` when a step lands. Do not rename step titles.

### Phase 1: Font and color foundation

#### Automated

- [x] 1.1 Templates parse without error
- [x] 1.2 Full test suite passes

#### Manual

- [ ] 1.3 Geist font renders in the browser
- [ ] 1.4 Teal accent visible on active nav, buttons, focus rings
- [ ] 1.5 Light mode accent darker teal for contrast

### Phase 2: Interaction polish and layout

#### Automated

- [x] 2.1 Full test suite passes

#### Manual

- [ ] 2.2 Button click shows slight downward shift
- [ ] 2.3 Main content stops at ~1200px on wide monitor
- [ ] 2.4 Anchor clicks scroll smoothly
- [ ] 2.5 Shadows blend with background

### Phase 3: Typography and component refinement

#### Automated

- [x] 3.1 Full test suite passes

#### Manual

- [ ] 3.2 h1 text visibly larger and tighter-spaced
- [ ] 3.3 Stat card numbers use fixed-width digits
- [ ] 3.4 Table headers no longer uppercase
- [ ] 3.5 Subtle grain texture visible
- [ ] 3.6 Adding/removing fallback rows still works

### Phase 4: Accessibility and meta

#### Automated

- [x] 4.1 Full test suite passes

#### Manual

- [ ] 4.2 Skip-to-content link appears on keyboard tab
- [ ] 4.3 Browser tab shows hexagon favicon
- [ ] 4.4 Meta description present in source

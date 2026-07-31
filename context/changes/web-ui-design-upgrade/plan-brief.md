# Web UI Design Upgrade — Plan Brief

> Full plan: `context/changes/web-ui-design-upgrade/plan.md`

## What & Why

Upgrade the freedius web dashboard's visual design from a generic AI-template aesthetic to a premium, intentional look. The UI had solid foundations (HTMX, semantic HTML, responsive layout) but every surface-level choice — system fonts, indigo accent, pure-black shadows, uppercase everywhere — marked it as auto-generated.

## Starting Point

A functional admin dashboard with 4 pages (Dashboard, Mappings, Providers, Logs), vanilla CSS custom properties, and good structural patterns. The problems were purely cosmetic and interaction-quality: no font character, AI-coded color, dead button clicks, no content width constraint, inline styles in JS, missing accessibility primitives.

## Desired End State

The dashboard looks like a deliberate developer tool, not a template. Geist font gives it identity. Teal accent reads "infrastructure" not "AI product." Every click provides tactile feedback. Content respects wide screens. Accessibility gaps (skip-link, favicon, meta) are closed. Code is cleaner: no inline styles, proper z-index scale.

## Key Decisions Made

| Decision | Choice | Why (1 sentence) |
|----------|--------|-------------------|
| Font | Geist Sans + Geist Mono via CDN | Modern geometric typeface with excellent small-size legibility, avoids the generic system stack |
| Accent color | Teal #0d9488 (dark) / #0f766e (light) | Kills the AI-purple fingerprint while reading as "infrastructure tool"; good contrast ratios |
| Shadow style | Zinc-tinted rgba(9,9,11,*) | Shadows blend with the dark zinc background instead of floating as separate black layers |
| Texture | SVG feTurbulence noise at 2.5% opacity | Breaks flat digital surfaces without performance cost |
| Inline styles | Replaced with CSS classes | Eliminates code smell in mappings template; makes styling auditable |
| z-index strategy | Custom property scale (50–300) | Removes magic numbers; makes stacking context predictable |

## Scope

**In scope:**
- Font swap (Geist via CDN)
- Accent color change (indigo → teal)
- Button `:active` pressed feedback
- `max-width` on main content
- Smooth scroll
- Tinted shadows
- Z-index scale cleanup
- Typography: larger h1, text-wrap balance, tabular-nums, reduce uppercase
- Noise texture overlay
- Remove inline styles from mappings template
- Skip-to-content link, favicon, meta description

**Out of scope:**
- Framework migration (no Tailwind, no CSS-in-JS)
- Template structural changes
- New pages or features
- JavaScript logic changes (beyond DOM class usage in `addFallbackRow`)
- Sidebar navigation redesign
- New animations

## Architecture / Approach

Pure CSS custom property swaps and targeted rule additions in the single `app.css` file. Template changes limited to `<head>` additions (font, meta, favicon, skip-link) and removing inline `style=` attributes. Zero functional changes — all existing HTMX behavior and Go handler logic untouched.

## Phases at a Glance

| Phase | What it delivers | Key risk |
|-------|-----------------|----------|
| 1. Font and color foundation | Geist font + teal accent | CDN dependency for font loading |
| 2. Interaction polish and layout | Active states, max-width, smooth scroll, tinted shadows, z-index scale | None — pure CSS additions |
| 3. Typography and component refinement | Larger headings, tabular-nums, reduced uppercase, noise texture, no inline styles | Fallback row JS change could break dialog |
| 4. Accessibility and meta | Skip-link, favicon, meta description | None — additive HTML only |

**Prerequisites:** None — existing codebase is ready.
**Estimated effort:** ~1 session, 4 phases executed sequentially.

## Open Risks & Assumptions

- Geist font loaded from CDN; if jsDelivr is unreachable, falls back to system fonts gracefully
- Noise overlay SVG uses `feTurbulence` which some very old browsers ignore (acceptable: dashboard targets modern browsers)

## Success Criteria (Summary)

- Dashboard looks intentionally designed, not template-generated (visual gut check)
- All `go test ./...` passes remain green
- No JavaScript errors in browser console after the changes

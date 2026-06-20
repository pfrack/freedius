# providers-section-refactor

- **created**: 2026-06-19
- **updated**: 2026-06-20
- **status**: implementing
- **research_complete**: true
- **plan_complete**: true
- **tags**: [config, providers, mappings, refactor, tui, breaking-change]

## Follow-ups

- **`init` / `serve` config-path asymmetry**: `freedius init` writes to `./freedius.yaml` in cwd, while `freedius serve` falls back to `$XDG_CONFIG_HOME/freedius/config.yaml` when no cwd config exists. A user running `freedius init` in dir A and `freedius serve` in dir B will silently miss the just-written config. Decide: should `init` default to xdg (matching `serve` fallback), should `serve` also look in xdg when an explicit cwd config is requested, or should `init` add an `--xdg` flag? Track as a separate change.

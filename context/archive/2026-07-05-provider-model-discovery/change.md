---
id: provider-model-discovery
title: Provider model discovery UI — fetch, cache, and refresh model lists
status: archived
created: 2026-07-05
updated: 2026-07-05
archived_at: 2026-07-05T20:30:00Z
type: feature
---

# Provider model discovery UI — fetch, cache, and refresh model lists

Add a web-dashboard affordance that fetches a provider's available models from
its upstream `/v1/models` endpoint (OpenAI-style `Authorization: Bearer` or
Anthropic-style `x-api-key` + `anthropic-version`), caches the result, and lets
the user refresh on demand. One surface only: an explicit "Fetch models" button
in the mapping modal that reveals a clickable list of model IDs to pick from —
the `model_string` field otherwise stays free-text. No fetch UI on the
Providers page.

Scope derived from research at `research.md` (see the 2026-07-05T19:08
follow-up section for the revision rationale).

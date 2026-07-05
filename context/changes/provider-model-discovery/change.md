---
id: provider-model-discovery
title: Provider model discovery UI — fetch, cache, and refresh model lists
status: implemented
created: 2026-07-05
updated: 2026-07-05
type: feature
---

# Provider model discovery UI — fetch, cache, and refresh model lists

Add a web-dashboard affordance that fetches a provider's available models from
its upstream `/v1/models` endpoint (OpenAI-style `Authorization: Bearer` or
Anthropic-style `x-api-key` + `anthropic-version`), caches the result, and lets
the user refresh on demand. Two surfaces: a per-provider "Fetch models" button on
the Providers page, and a `<datalist>` autocomplete on the mapping form's
`model_string` field so configuring mappings no longer means hand-typing model
IDs.

Scope derived from research at `research.md`.

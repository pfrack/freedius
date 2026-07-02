---
change_id: testing-proxy-integration
title: Testing proxy integration
status: implemented
created: 2026-07-02
updated: 2026-07-02
archived_at: null
---

## Notes

Open a change folder for rollout Phase 1 of context/foundation/test-plan.md: "Proxy integration — translation, routing, errors".
Risks covered: #1 (translation format confusion), #3 (silent misrouting), #4 (config validation crash), #5 (error swallowed), #6 (API key leakage).
Test types planned: integration + unit.
Risk response intent:
- #1: prove Anthropic endpoint always returns Anthropic-format response (headers, body shape, error format) regardless of upstream provider.
- #3: prove configured model mapping routes to the correct provider endpoint; wrong/missing mapping produces clear error, not silent fallback.
- #4: prove invalid config produces descriptive error without crashing the gateway.
- #5: prove provider 500/timeout/429 surfaces as descriptive error to Claude Code — not swallowed.
- #6: prove API keys and sensitive config never appear in logs, error responses, or TUI output.
After creating the folder, follow the downstream continuation rule.

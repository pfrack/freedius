---
id: provider-writeback-merge
status: implemented
objective: Stop dashboard provider create/update from silently destroying the generated openai: adapter-options block and both runtime-only flags, by merging form fields onto the existing Provider, re-applying generated defaults, and gating writes with validation.
created: 2026-10-12
today: 2026-10-12
updated: 2026-10-12
---

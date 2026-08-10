# Follow-ups from implementation review

Source: `context/changes/logs-ui-live-tail/reviews/impl-review.md` (triaged 2026-08-09)

## 1. Dedicated logs-ui commit for p3 + review fixes (F3)

Blocked on: the shared working tree still carries the `nim-nous-kilo-defaults`
starter.yaml edit, which fails `TestStarterTemplate_ValidConfig` (colon in
`hy3:free`) and trips the pre-commit gate. See F2.

Once the tree is clean, stage **only** the logs-ui files and commit:

- `proxy/web/templates/logs.html` — F5 fix (`sse-swap="log"` + `hx-swap="none"`,
  message-guard `detail.type` fix)
- `e2e/tests/logs-tail.spec.ts` — F1 fix (post-load probe + count assertion)
- `context/changes/logs-ui-live-tail/plan.md` — Phase 3 SHA write-back

Suggested message:

```
fix(logs-ui-live-tail): register sse-swap so the live tail actually streams (p3)
```

Then run the epilogue: set `change.md` → `implemented`.

## 2. Re-verify full gates after the tree is clean (F2)

`mage test` / `mage ci` could not be certified green for logs-ui because of the
parallel change's failure. Re-run both once `nim-nous-kilo-defaults` resolves
its `hy3:free` colon issue.

## 3. Plan source-URL correction (F4)

`plan.md` Phase 1.1 cites `https://unpkg.com/htmx-ext-sse@2.2.2/sse.min.js`,
which 404s. The vendored file came from `.../sse.js`. Update the plan so the
source of truth matches reality.

## 4. Re-check the Phase 1/2 manual criteria (F5 fallout)

The manual criteria asserting a working live tail were checked off while the
tail was in fact dead (the connection dot flipped to `live` on `htmx:sseOpen`
even though no messages were ever appended). Re-walk those manual items against
the fixed build rather than trusting the existing `[x]` marks.

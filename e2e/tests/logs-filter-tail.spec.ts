import { test, expect } from '@playwright/test';

test.describe('Logs live-tail filtering', () => {
  test('streamed lines that do not match the active filter are dropped', async ({ page }) => {
    // Regression guard for the client-side SSE filter (logLinePassesFilters in
    // logs.html). The server-side predicate in handleLogs filters the initial
    // render, but the SSE tail streams every line unfiltered — without the
    // client-side check, non-matching lines leak into #log and the operator
    // sees "filters not working". The Go tests only cover handleLogs, so this
    // is the only automated coverage of the actual fix.
    await page.goto('/logs?provider=freedius-filter-hit');
    await expect(page.locator('#provider-filter')).toHaveValue('freedius-filter-hit');

    // Wait for the live subscription before generating activity, otherwise the
    // probe lines could be emitted before the EventSource is open.
    await expect(page.locator('.log-live-dot')).toHaveAttribute('data-state', 'live', { timeout: 15000 });

    const lines = page.locator('#log pre');

    // Fire the non-matching probe FIRST, then the matching one. The proxy logs
    // the request path via AccessLogMiddleware, so the filter substring decides
    // which line survives. SSE preserves order, so once the matching line has
    // rendered we know the non-matching one was already streamed — and dropped,
    // if the client filter works.
    await fetch('http://127.0.0.1:8082/__freedius-filter-miss').catch(() => {});
    await fetch('http://127.0.0.1:8082/__freedius-filter-hit').catch(() => {});

    await expect
      .poll(async () => lines.filter({ hasText: '__freedius-filter-hit' }).count(), { timeout: 15000 })
      .toBeGreaterThan(0);

    // The access log is emitted after the response is written, so the miss line
    // can trail its own fetch by a hair. Settle before the negative assertion so
    // a pass means "filtered", never "not yet arrived".
    await page.waitForTimeout(500);
    await expect(lines.filter({ hasText: '__freedius-filter-miss' })).toHaveCount(0);
  });
});

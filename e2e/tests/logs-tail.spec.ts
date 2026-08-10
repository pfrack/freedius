import { test, expect } from '@playwright/test';

test.describe('Logs live tail', () => {
  test('streamed log lines appear in #log over SSE on load', async ({ page }) => {
    await page.goto('/logs');

    // The live tail appends <pre class="log-*"> lines streamed from /v1/logs.
    // Regression guard: without the htmx SSE extension loaded, sse-connect is
    // a no-op and #log never receives streamed entries. /logs also server-
    // renders the current ring snapshot on initial load, so an attached line
    // alone is NOT proof of a live tail — we capture the pre-load count and
    // assert a NEW line arrives after we generate proxy activity.
    const lines = page.locator('#log pre.log-info, #log pre.log-debug, #log pre.log-warn, #log pre.log-error');
    const before = await lines.count();

    // Generate proxy activity so the live SSE tail has something to stream.
    // The proxy logs every request through the ring handler (AccessLogMiddleware
    // emits "request complete" after the handler returns). We issue the request
    // from the test's Node context (which reaches :8082 directly) rather than
    // from the browser origin, then assert the resulting event arrives on the
    // browser's live SSE subscription — proving the tail actually streams. A
    // bogus path fails fast (405) and logs immediately, avoiding a slow upstream
    // call (e.g. /v1/chat/completions -> example.com) that would only log after
    // its timeout.
    await fetch('http://127.0.0.1:8082/__logs_tail_probe').catch(() => {});

    // A line must arrive beyond the initial snapshot — only possible if the
    // SSE subscription is live.
    await expect
      .poll(async () => lines.count(), { timeout: 15000 })
      .toBeGreaterThan(before);
  });

  test('connection dot reflects a live SSE stream', async ({ page }) => {
    await page.goto('/logs');

    // The dot starts "connecting" and flips to "live" once the htmx SSE
    // extension opens the EventSource to /v1/logs. If the extension script
    // is dropped, hx-ext="sse" never registers and the dot stays "connecting",
    // failing this assertion — locking in the fix.
    await expect(page.locator('.log-live-dot')).toHaveAttribute('data-state', 'live', { timeout: 15000 });
  });
});

import { test, expect } from '@playwright/test';

test.describe('Logs live tail', () => {
  test('streamed log lines appear in #log over SSE on load', async ({ page }) => {
    await page.goto('/logs');

    // The live tail appends <pre class="log-*"> lines streamed from /v1/logs.
    // Regression guard: without the htmx SSE extension loaded, sse-connect
    // is a no-op and #log never receives streamed entries.
    const lines = page.locator('#log pre.log-info, #log pre.log-debug, #log pre.log-warn, #log pre.log-error');
    await expect(lines.first()).toBeAttached({ timeout: 15000 });
    await expect(lines.first()).not.toHaveText('', { timeout: 15000 });
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

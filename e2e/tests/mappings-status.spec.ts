// Status-vocabulary regression guards for the mappings page.
//
// These pin the honest, consistent "No API key" wording end-to-end after the
// misleading-inactive-filter change: the status filter option and the unset-key
// status badge must agree, and the badge must explain its cause via a tooltip.
// The internal filter value stays "inactive", so filtering semantics are
// unchanged — asserted by requesting ?status=inactive directly.
//
// Fixture dependency: the "test-chat" mapping asserted below is seeded by
// e2e/fixtures/test-config.yaml, which playwright.config.ts loads via
// `go run ../cmd/freedius --config ../e2e/fixtures/test-config.yaml`. It is the
// only mapping in that fixture, so the "No API key" filter yields exactly one
// row.
import { test, expect } from '@playwright/test';

test('status filter option reads "No API key"', async ({ page }) => {
  await page.goto('/mappings');
  const opt = page.locator('select[name="status"] option[value="inactive"]');
  await expect(opt).toHaveText('No API key');
});

test('unset-key mapping is badged "No API key" with a cause tooltip', async ({ page }) => {
  await page.goto('/mappings');
  const row = page.locator('.mappings-table tbody tr', { hasText: 'test-chat' });
  const badge = row.locator('.badge--status-warn');
  await expect(badge).toHaveText('No API key');
  await expect(badge).toHaveAttribute('title', 'No API key set in the environment');
});

test('filtering by "No API key" returns the same rows (semantics unchanged)', async ({ page }) => {
  await page.goto('/mappings?status=inactive');
  // The internal value="inactive" still drives filtering.
  await expect(page.locator('select[name="status"]')).toHaveValue('inactive');
  // The unset-key mapping is still surfaced.
  const rows = page.locator('.mappings-table tbody tr');
  await expect(rows).toHaveCount(1);
  await expect(rows.filter({ hasText: 'test-chat' })).toBeVisible();
});

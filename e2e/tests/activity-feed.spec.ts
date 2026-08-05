import { test, expect } from '@playwright/test';

test.describe('SSE Activity Feed', () => {
  test('activity feed section renders on dashboard load', async ({ page }) => {
    await page.goto('/');
    await expect(page.locator('h1')).toContainText('Dashboard');
    // Activity feed section should exist.
    await expect(page.locator('#activity-feed')).toBeVisible();
  });

  test('dashboard shows routing table with test mapping', async ({ page }) => {
    await page.goto('/');
    // The test-config mapping should appear in the routing table as a strong element.
    await expect(page.locator('.routing-table strong', { hasText: 'test-chat' })).toBeVisible();
  });
});

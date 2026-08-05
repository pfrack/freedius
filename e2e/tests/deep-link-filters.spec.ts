import { test, expect } from '@playwright/test';

test.describe('Deep-Link Filters', () => {
  test('navigating to /logs?outcome=error pre-populates filter', async ({ page }) => {
    await page.goto('/logs?outcome=error');
    // The outcome dropdown should have "Error" selected.
    const outcomeSelect = page.locator('#outcome-filter');
    await expect(outcomeSelect).toBeVisible();
    await expect(outcomeSelect).toHaveValue('error');
  });

  test('navigating to /logs?fallback=true pre-populates filter', async ({ page }) => {
    await page.goto('/logs?fallback=true');
    // The fallback dropdown should have "Yes" selected.
    const fallbackSelect = page.locator('#fallback-filter');
    await expect(fallbackSelect).toBeVisible();
    await expect(fallbackSelect).toHaveValue('true');
  });

  test('logs page renders without errors', async ({ page }) => {
    await page.goto('/logs');
    await expect(page.locator('h1')).toContainText('Logs');
    // Filter controls should be present.
    await expect(page.locator('#level-filter')).toBeVisible();
    await expect(page.locator('#outcome-filter')).toBeVisible();
    await expect(page.locator('#fallback-filter')).toBeVisible();
  });
});

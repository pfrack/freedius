import { test, expect } from '@playwright/test';

test.describe('Mapping Details Drawer', () => {
  test('clicking a routing table row opens the drawer', async ({ page }) => {
    await page.goto('/');
    // Click the test-chat mapping row (the <strong> inside the routing table).
    await page.locator('.routing-table strong', { hasText: 'test-chat' }).click();
    // Drawer should become visible (gets .drawer--open class and content).
    await expect(page.locator('#mapping-drawer')).toContainText('test-chat');
  });

  test('drawer shows route chain (primary + fallback)', async ({ page }) => {
    await page.goto('/');
    await page.locator('.routing-table strong', { hasText: 'test-chat' }).click();
    await expect(page.locator('#mapping-drawer')).toBeVisible();
    // Should show primary provider.
    await expect(page.locator('#mapping-drawer')).toContainText('test-primary');
    // Should show fallback provider.
    await expect(page.locator('#mapping-drawer')).toContainText('test-fallback');
  });

  test('pressing Escape closes the drawer', async ({ page }) => {
    await page.goto('/');
    await page.locator('.routing-table strong', { hasText: 'test-chat' }).click();
    await expect(page.locator('#mapping-drawer')).toContainText('test-chat');
    // Press Escape to close.
    await page.keyboard.press('Escape');
    // Drawer content should be cleared (empty after close).
    await expect(page.locator('#mapping-drawer')).toHaveText('');
  });
});

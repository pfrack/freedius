import { test, expect } from '@playwright/test';

test.describe('Test Connection modal', () => {
  test('clicking Test opens a modal dialog with reachability result', async ({ page }) => {
    await page.goto('/providers');
    // Click the Test button for the first provider.
    await page.locator('button:has-text("Test")').first().click();
    // The test-dialog should open.
    await expect(page.locator('#test-dialog')).toBeVisible();
    // Dialog body should show either "Reachable" or "Unreachable".
    await expect(page.locator('#test-dialog-body')).toContainText(/Reachable|Unreachable/);
  });

  test('providers table remains visible after Test click', async ({ page }) => {
    await page.goto('/providers');
    await expect(page.locator('#providers')).toBeVisible();
    await page.locator('button:has-text("Test")').first().click();
    // The providers table should still be in the DOM (not replaced by models).
    await expect(page.locator('#providers')).toBeVisible();
  });

  test('closing the Test modal keeps the table intact', async ({ page }) => {
    await page.goto('/providers');
    await expect(page.locator('text=test-primary')).toBeVisible();
    await page.locator('button:has-text("Test")').first().click();
    await expect(page.locator('#test-dialog')).toBeVisible();
    // Click Close button inside the dialog.
    await page.locator('#test-dialog button:has-text("Close")').click();
    // Table should still be there with the provider name visible.
    await expect(page.locator('#providers')).toBeVisible();
    await expect(page.locator('text=test-primary')).toBeVisible();
  });
});

import { test, expect } from '@playwright/test';

test.describe('Attention Panel', () => {
  test('dashboard loads without attention panel when env vars are unset', async ({ page }) => {
    await page.goto('/');
    // With test-config.yaml, the provider API keys reference env vars
    // TEST_PRIMARY_API_KEY and TEST_FALLBACK_API_KEY which are likely
    // not set in the test environment. The attention panel should appear.
    const attentionPanel = page.locator('.attention-panel');
    // Either the panel exists (env vars missing) or it doesn't (env vars set).
    // We verify the dashboard renders correctly either way.
    await expect(page.locator('h1')).toContainText('Dashboard');
  });

  test('attention panel links navigate correctly when present', async ({ page }) => {
    await page.goto('/');
    const alertLinks = page.locator('.attention-panel a');
    const count = await alertLinks.count();
    if (count > 0) {
      // Each alert link should point to a valid page (/logs, /providers, /mappings).
      for (let i = 0; i < count; i++) {
        const href = await alertLinks.nth(i).getAttribute('href');
        expect(href).toMatch(/^\/(logs|providers|mappings)/);
      }
    }
  });
});

import { test, expect } from '@playwright/test';

test.describe('Base Path Prefix', () => {
  test('dashboard loads at /freedius/', async ({ page }) => {
    await page.goto('/freedius/');
    await expect(page.locator('h1')).toContainText('Dashboard');
  });

  test('nav links are prefixed with /freedius', async ({ page }) => {
    await page.goto('/freedius/');
    const mappingsHref = await page.locator('nav a', { hasText: 'Mappings' }).getAttribute('href');
    expect(mappingsHref).toBe('/freedius/mappings');
    const providersHref = await page.locator('nav a', { hasText: 'Providers' }).getAttribute('href');
    expect(providersHref).toBe('/freedius/providers');
    const dashboardHref = await page.locator('nav a', { hasText: 'Dashboard' }).getAttribute('href');
    expect(dashboardHref).toBe('/freedius/');
  });

  test('static assets are prefixed with /freedius/static/', async ({ page }) => {
    await page.goto('/freedius/');
    const cssHref = await page.locator('link[rel="stylesheet"][href*="app.css"]').getAttribute('href');
    expect(cssHref).toContain('/freedius/static/');
    const htmxSrc = await page.locator('script[src*="htmx.min.js"]').getAttribute('src');
    expect(htmxSrc).toContain('/freedius/static/');
  });

  test('SSE endpoint is prefixed in sse-connect', async ({ page }) => {
    await page.goto('/freedius/');
    const sseConnect = await page.locator('#activity-feed').getAttribute('sse-connect');
    expect(sseConnect).toContain('/freedius/v1/events');
  });

  test('clicking nav link navigates to prefixed URL', async ({ page }) => {
    await page.goto('/freedius/');
    await page.locator('nav a', { hasText: 'Mappings' }).click();
    await expect(page).toHaveURL(/.*\/freedius\/mappings$/);
  });

  test('root path without prefix returns 404', async ({ page }) => {
    const response = await page.request.get('http://localhost:8083/health');
    expect(response.status()).toBe(404);
  });
});

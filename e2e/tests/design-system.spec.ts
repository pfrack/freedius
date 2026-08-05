// Design-system regression guards for the ui-design-polish pass.
//
// These assert computed styles rather than pixels — there are no golden
// images, so nothing here flakes on font hinting or GPU differences. Each
// block pins a decision that is easy to revert by accident: the badge stripe,
// sentence-case labels, the radius hierarchy, the ring spinner, the log-line
// cap, the unified hexagon mark, the social meta, and the 404 treatment.
import { test, expect } from '@playwright/test';
import * as fs from 'fs';
import * as path from 'path';

// test-results/ is gitignored; screenshots are diagnostic output, not fixtures.
const OUT = path.join(__dirname, '..', 'test-results', 'shots');
fs.mkdirSync(OUT, { recursive: true });

const VIEWPORTS = [
  { name: '1280', width: 1280, height: 800 },
  { name: '768', width: 768, height: 1024 },
  { name: '480', width: 480, height: 800 },
];

const PAGES = [
  { name: 'dashboard', path: '/' },
  { name: 'mappings', path: '/mappings' },
  { name: 'providers', path: '/providers' },
  { name: 'logs', path: '/logs' },
  { name: '404', path: '/nope-not-a-route' },
];

for (const scheme of ['dark', 'light'] as const) {
  for (const vp of VIEWPORTS) {
    for (const pg of PAGES) {
      test(`smoke ${scheme} ${vp.name} ${pg.name}`, async ({ page }) => {
        await page.emulateMedia({ colorScheme: scheme });
        await page.setViewportSize({ width: vp.width, height: vp.height });
        const errors: string[] = [];
        page.on('pageerror', (e) => errors.push(String(e)));
        await page.goto(pg.path);
        await page.waitForTimeout(250);
        await page.screenshot({
          path: `${OUT}/${scheme}-${vp.name}-${pg.name}.png`,
          fullPage: true,
        });
        // The page must not be horizontally scrollable. Measure real
        // scrollability, not scrollWidth: Chrome inflates scrollWidth for
        // off-canvas position:fixed panels (the closed drawer) even though no
        // scrollbar exists and scrollX cannot move.
        const scrollX = await page.evaluate(() => {
          window.scrollTo(9999, 0);
          const x = window.scrollX;
          window.scrollTo(0, 0);
          return x;
        });
        expect(scrollX, `h-scroll on ${pg.name}`).toBe(0);
        expect(errors, `JS errors on ${pg.name}`).toEqual([]);
      });
    }
  }
}

test('p5: status badges render as square flags with left stripe', async ({ page }) => {
  await page.goto('/');
  const badge = page.locator('[class*="badge--status-"]').first();
  await expect(badge).toBeVisible();
  const s = await badge.evaluate((el) => {
    const c = getComputedStyle(el);
    return {
      radius: c.borderTopLeftRadius,
      leftWidth: c.borderLeftWidth,
      leftStyle: c.borderLeftStyle,
      leftColor: c.borderLeftColor,
      color: c.color,
    };
  });
  expect(s.radius).toBe('0px');
  expect(s.leftWidth).toBe('3px');
  expect(s.leftStyle).toBe('solid');
  // currentColor: stripe must equal the badge's own semantic colour.
  expect(s.leftColor).toBe(s.color);
});

test('p2: drawer + strip labels are sentence case', async ({ page }) => {
  await page.goto('/');
  for (const sel of ['.health-strip__key', '.health-strip__label']) {
    const tt = await page.locator(sel).first().evaluate((el) => getComputedStyle(el).textTransform);
    expect(tt, sel).toBe('none');
  }
  await page.locator('.routing-table strong', { hasText: 'test-chat' }).click();
  await expect(page.locator('#mapping-drawer')).toContainText('test-chat');
  for (const sel of ['.drawer__label', '.route-step__label', '.drawer__stats dt', '.drawer__section h3']) {
    const tt = await page.locator(sel).first().evaluate((el) => getComputedStyle(el).textTransform);
    expect(tt, sel).toBe('none');
  }
  // Table headers deliberately stay uppercase.
  const th = await page.locator('.routing-table th').first().evaluate((el) => getComputedStyle(el).textTransform);
  expect(th).toBe('uppercase');
});

test('p3: radius hierarchy + empty-state has no dashed border', async ({ page }) => {
  await page.goto('/');
  const wrap = await page.locator('.table-wrap').first().evaluate((el) => getComputedStyle(el).borderTopLeftRadius);
  expect(wrap).toBe('20px'); // --radius-xl 1.25rem
  await page.goto('/logs');
  const log = await page.locator('.log-container').evaluate((el) => getComputedStyle(el).borderTopLeftRadius);
  expect(log).toBe('20px');
});

test('p3: no translateY on hover for route-step', async ({ page }) => {
  await page.goto('/');
  await page.locator('.routing-table strong', { hasText: 'test-chat' }).click();
  await expect(page.locator('#mapping-drawer')).toContainText('test-chat');
  const step = page.locator('.route-step').first();
  await step.hover();
  await page.waitForTimeout(350);
  const t = await step.evaluate((el) => getComputedStyle(el).transform);
  expect(['none', 'matrix(1, 0, 0, 1, 0, 0)']).toContain(t);
});

test('p4: htmx indicator is a ring, hidden at rest', async ({ page }) => {
  await page.goto('/mappings');
  const ind = page.locator('.htmx-indicator').first();
  const s = await ind.evaluate((el) => {
    const c = getComputedStyle(el);
    return { display: c.display, radius: c.borderTopLeftRadius, top: c.borderTopColor, text: el.textContent };
  });
  expect(s.display).toBe('none');
  expect(s.radius).toBe('50%');
  expect(s.top).toBe('rgba(0, 0, 0, 0)'); // border-top-color: transparent
  expect((s.text || '').trim()).toBe('');
});

test('p4: log DOM caps at 500 lines under a 600-event burst', async ({ page }) => {
  await page.goto('/logs');
  await page.evaluate(() => {
    const el = document.getElementById('log')!;
    el.innerHTML = '';
    for (let i = 0; i < 600; i++) {
      document.dispatchEvent(
        new CustomEvent('htmx:sseMessage', {
          detail: { eventName: 'log', data: JSON.stringify({ level: 'info', line: 'line ' + i }) },
        }),
      );
    }
  });
  const count = await page.locator('#log > pre').count();
  expect(count).toBe(500);
  // Oldest trimmed, newest retained.
  const first = await page.locator('#log > pre').first().textContent();
  const last = await page.locator('#log > pre').last().textContent();
  expect(first).toBe('line 100');
  expect(last).toBe('line 599');
});

test('p5/p6: one hexagon mark across favicon, hamburger and header', async ({ page }) => {
  await page.goto('/');
  const hex = 'polygon[points="12 2 21 7 21 17 12 22 3 17 3 7 12 2"]';
  expect(await page.locator(`.hamburger ${hex}`).count()).toBe(1);
  expect(await page.locator(`.sidebar-header ${hex}`).count()).toBe(1);
  const icon = await page.locator('link[rel="icon"]').getAttribute('href');
  expect(icon).toContain('⬡');
  // All nav icons use the heavier custom stroke.
  const widths = await page.locator('.sidebar nav a svg').evaluateAll((els) =>
    els.map((e) => e.getAttribute('stroke-width')),
  );
  expect(widths.length).toBe(4);
  expect(widths.every((w) => w === '2.25')).toBe(true);
});

test('p6: social meta present and og:image is a usable SVG data URI', async ({ page }) => {
  await page.goto('/');
  for (const [attr, key] of [
    ['property', 'og:type'],
    ['property', 'og:title'],
    ['property', 'og:description'],
    ['property', 'og:image'],
    ['name', 'twitter:card'],
  ]) {
    await expect(page.locator(`meta[${attr}="${key}"]`)).toHaveCount(1);
  }
  const img = await page.locator('meta[property="og:image"]').getAttribute('content');
  expect(img).toMatch(/^data:image\/svg\+xml,/);
  // Must actually decode and load as an image.
  const ok = await page.evaluate(
    (src) =>
      new Promise<boolean>((res) => {
        const i = new Image();
        i.onload = () => res(i.naturalWidth === 1200 && i.naturalHeight === 630);
        i.onerror = () => res(false);
        i.src = src;
      }),
    img!,
  );
  expect(ok).toBe(true);
});

for (const scheme of ['dark', 'light'] as const) {
  test(`p5: 404 code is small and muted in ${scheme} mode`, async ({ page }) => {
    await page.emulateMedia({ colorScheme: scheme });
    await page.goto('/nope');
    const code = page.locator('.not-found__code');
    await expect(code).toBeVisible();
    const s = await code.evaluate((el) => {
      const c = getComputedStyle(el);
      return { size: parseFloat(c.fontSize), color: c.color, transform: c.transform };
    });
    expect(s.size).toBeLessThanOrEqual(64); // clamp max 4rem
    // --text-muted: #5c5c66 dark / #9a9aa0 light. Must not be the accent.
    expect(s.color).toBe(scheme === 'dark' ? 'rgb(92, 92, 102)' : 'rgb(154, 154, 160)');
    expect(s.transform).not.toBe('none'); // -2deg tilt
  });
}

test('a11y: focus ring is visible on interactive elements', async ({ page }) => {
  await page.goto('/');
  await page.keyboard.press('Tab');
  const s = await page.evaluate(() => {
    const el = document.activeElement as HTMLElement;
    const c = getComputedStyle(el);
    return { tag: el.tagName, width: c.outlineWidth, style: c.outlineStyle };
  });
  expect(s.style).not.toBe('none');
  expect(parseFloat(s.width)).toBeGreaterThanOrEqual(2);
});

const { test, expect } = require('@playwright/test');

test.describe('Public shell reactivity', () => {
  test('home page loads HTMX without console errors', async ({ page }) => {
    const errors = [];
    page.on('pageerror', (error) => errors.push(error.message));

    await page.goto('/');
    await expect(page).toHaveTitle(/Avandab/);
    await expect(page.locator('body.has-public-nav')).toBeAttached();
    await expect(page.locator('script[src*="htmx"]')).toBeAttached();
    await page.waitForFunction(() => Boolean(window.htmx));

    expect(errors).toHaveLength(0);
  });

  test('public pages do not reference the removed router bundle', async ({ page }) => {
    await page.goto('/');

    await expect(page.locator('script[src*="router.js"]')).toHaveCount(0);
    await expect(page.locator('script[src*="htmx.min.js"]')).toBeAttached();
    await expect(page.locator('a[href="/register"]').first()).toHaveCount(1);
  });
});

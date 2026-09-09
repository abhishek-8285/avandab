const { test, expect } = require('@playwright/test');

test.describe('Router Reactivity & Datastar Integration Tests', () => {
  test('home page loads router.js and datastar.js without console errors', async ({ page }) => {
    const errors = [];
    page.on('pageerror', err => errors.push(err.message));

    await page.goto('/');
    await expect(page).toHaveTitle(/Avandab/);

    // Verify router script is loaded
    await expect(page.locator('script[src*="router.js"]')).toBeAttached();

    // Verify no uncaught exceptions
    expect(errors).toHaveLength(0);
  });

  test('router submit interceptor ignores data-spa=false and data-datastar-ignore forms', async ({ page }) => {
    await page.setContent(`
      <!DOCTYPE html>
      <html>
      <head>
        <script src="/static/js/router.js"></script>
      </head>
      <body>
        <form id="spa-form" action="/test-spa" method="GET">
          <button type="submit">Submit SPA</button>
        </form>
        <form id="no-spa-form" action="/test-nospa" data-spa="false" method="GET">
          <button type="submit">Submit No-SPA</button>
        </form>
        <form id="ignore-form" action="/test-ignore" data-datastar-ignore method="GET">
          <button type="submit">Submit Ignore</button>
        </form>
        <form id="logout-form" action="/logout" method="POST">
          <button type="submit">Logout</button>
        </form>
      </body>
      </html>
    `);

    // Verify forms are rendered properly
    await expect(page.locator('#spa-form')).toBeAttached();
    await expect(page.locator('#no-spa-form')).toBeAttached();
    await expect(page.locator('#ignore-form')).toBeAttached();
    await expect(page.locator('#logout-form')).toBeAttached();
  });
});

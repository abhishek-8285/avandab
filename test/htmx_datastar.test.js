const { test, expect } = require('@playwright/test');

async function registerFreshUser(page) {
  for (let attempt = 0; attempt < 3; attempt++) {
    await page.goto('/register');
    const uid = `${Date.now()}${Math.floor(Math.random() * 100000)}`;
    await page.fill('input[name="name"]', 'PW Kharcha');
    await page.fill('input[name="email"]', `pwkharcha${uid}@test.com`);
    await page.fill('input[name="phone"]', '9876500000');
    await page.fill('input[name="password"]', 'TestPass123!');
    await page.fill('input[name="confirm_password"]', 'TestPass123!');
    await page.check('input[name="agree"]'); // DPDP explicit consent gate
    await page.click('button[type="submit"]');

    try {
      await page.waitForURL(/\/dashboard|\/company\/onboard/, {
        waitUntil: 'domcontentloaded',
        timeout: 15000,
      });
    } catch (error) {
      const body = await page.content().catch(() => '');
      if (body.includes('database table is locked') || body.includes('database is locked')) {
        await page.waitForTimeout(800 * (attempt + 1));
        continue;
      }
      throw error;
    }

    if (page.url().includes('/company/onboard')) {
      await page.fill('input[name="company_name"]', `PW Kharcha Fleet ${Date.now()}`);
      await page.fill('input[name="email"]', `ops${Date.now()}@test.com`);
      await page.fill('input[name="phone"]', '9876500000');
      await page.fill('textarea[name="address"]', 'Plot 42, Transport Nagar, Delhi 110042');
      await Promise.all([
        page.waitForURL('**/dashboard', { waitUntil: 'domcontentloaded' }),
        page.click('#onboarding-form button[type="submit"]'),
      ]);
    }

    await page.waitForURL('**/dashboard', { waitUntil: 'domcontentloaded' });
    return;
  }
  throw new Error('registerFreshUser failed after retries');
}

test.describe('Public HTMX integration', () => {
  test('home page loads successfully', async ({ page }) => {
    await page.goto('/');
    await expect(page).toHaveTitle(/Avandab/);
    await expect(page.locator('h1')).toContainText('Fleet operations managed with clarity and control');
    await expect(page.locator('script[src*="htmx"]')).toBeAttached();
  });

  test('unauthenticated kharcha redirects to login', async ({ page }) => {
    await page.goto('/kharcha');
    await expect(page).toHaveURL(/\/login/);
  });
});
test.describe('Authenticated Kharcha HTMX integration', () => {
  test.describe.configure({ mode: 'serial' });

  test.beforeEach(async ({ page }) => {
    await registerFreshUser(page);
    await page.goto('/kharcha');
    await page.waitForFunction(() => Boolean(window.htmx));
    await expect(page).toHaveTitle(/Kharcha Ledger/);
  });

  test('dashboard exposes the live HTMX polling attributes', async ({ page }) => {
    const livePill = page.locator('#queue-live-pill');
    await expect(livePill).toBeVisible();
    await expect(livePill).toHaveAttribute('hx-get', '/kharcha/pending');
    await expect(livePill).toHaveAttribute('hx-trigger', 'every 30s');
    await expect(livePill).toHaveAttribute('hx-target', '#kharcha-queue');
    await expect(livePill).toHaveAttribute('hx-swap', 'innerMorph');
  });

  test('reject expense modal wires its HTMX form on open', async ({ page }) => {
    await page.evaluate(() => openRejectModal('expense-e2e', 'Test Driver', '500.00'));
    await expect(page.locator('#reject-modal')).toBeVisible();
    await expect(page.locator('#reject-form')).toHaveAttribute('hx-post', '/kharcha/expense-e2e/reject');
    await expect(page.locator('#reject-form')).toHaveAttribute('hx-target', '#expense-row-expense-e2e');
    await expect(page.locator('#reject-form')).toHaveAttribute('hx-swap', 'outerMorph');
  });

  test('ledger trip filter uses HTMX on change', async ({ page }) => {
    const filterSelect = page.locator('select#ledger-trip-filter');
    await expect(filterSelect).toHaveAttribute('hx-get', '/kharcha/ledger');
    await expect(filterSelect).toHaveAttribute('hx-trigger', 'change');
    await expect(filterSelect).toHaveAttribute('hx-target', '#ledger-body');
    await expect(filterSelect).toHaveAttribute('hx-swap', 'innerMorph');
  });

  test('expense POST form is present and HTMX is loaded', async ({ page }) => {
    await expect(page.locator('#new-expense-form form[method="POST"]')).toBeAttached();
    await expect(page.locator('script[src*="htmx"]')).toBeAttached();
  });
});

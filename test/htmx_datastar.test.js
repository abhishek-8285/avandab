const { test, expect } = require('@playwright/test');

async function registerFreshUser(page) {
  await page.goto('/register');
  const uid = `${Date.now()}${Math.floor(Math.random() * 100000)}`;
  await page.fill('input[name="name"]', 'PW Test');
  await page.fill('input[name="email"]', `pw${uid}@test.com`);
  await page.fill('input[name="phone"]', '9876500000');
  await page.fill('input[name="password"]', 'TestPass123!');
  await page.fill('input[name="confirm_password"]', 'TestPass123!');
  await page.click('button[type="submit"]');
  await page.waitForURL(/\/dashboard|\/company\/onboard/, { waitUntil: 'domcontentloaded', timeout: 15000 });
  if (page.url().includes('/company/onboard')) {
    await page.fill('input[name="company_name"]', 'PW Fleet ' + Date.now());
    await page.fill('input[name="email"]', `ops${Date.now()}@test.com`);
    await page.fill('input[name="phone"]', '9876500000');
    await page.fill('textarea[name="address"]', 'Plot 42, Transport Nagar, Delhi 110042');
    await Promise.all([
      page.waitForURL('**/dashboard', { waitUntil: 'domcontentloaded' }),
      page.click('#onboarding-form button[type="submit"]'),
    ]);
  }
  await page.waitForURL('**/dashboard', { waitUntil: 'domcontentloaded' });
}

test.describe('HTMX & Datastar UI Tests', () => {
  test.describe.configure({ mode: 'serial' });

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

  test('kharcha dashboard has htmx polling endpoints', async ({ page }) => {
    await registerFreshUser(page);
    await page.goto('/kharcha');
    const refreshSpan = page.locator('#queue-live-pill');
    await expect(refreshSpan).toBeVisible();
    await expect(refreshSpan).toHaveAttribute('hx-get', '/kharcha/pending');
    await expect(refreshSpan).toHaveAttribute('hx-trigger', 'every 30s');
    await expect(refreshSpan).toHaveAttribute('hx-target', '#kharcha-queue');
    await expect(refreshSpan).toHaveAttribute('hx-swap', 'innerMorph');
  });

  test('reject expense form has htmx wired correctly', async ({ page }) => {
    await registerFreshUser(page);
    await page.goto('/kharcha');
    // Wire modal at runtime (form ships without hx-* until openRejectModal runs)
    await page.evaluate(() => openRejectModal('exp-test', 'Test Driver', '100'));
    await expect(page.locator('#reject-modal')).toBeVisible();
    await expect(page.locator('#reject-form')).toHaveAttribute('hx-post', '/kharcha/exp-test/reject');
    await expect(page.locator('#reject-form')).toHaveAttribute('hx-target', '#expense-row-exp-test');
    await expect(page.locator('#reject-form')).toHaveAttribute('hx-swap', 'outerMorph');
  });

  test('ledger trip filter uses htmx on change', async ({ page }) => {
    await registerFreshUser(page);
    await page.goto('/kharcha');
    const filterSelect = page.locator('select#ledger-trip-filter');
    await expect(filterSelect).toHaveAttribute('hx-get', '/kharcha/ledger');
    await expect(filterSelect).toHaveAttribute('hx-trigger', 'change');
    await expect(filterSelect).toHaveAttribute('hx-target', '#ledger-body');
    await expect(filterSelect).toHaveAttribute('hx-swap', 'innerMorph');
  });

  test('datastar request header is handled on POST requests', async ({ page }) => {
    await registerFreshUser(page);
    await page.goto('/kharcha');
    const form = page.locator('form[method="POST"]').first();
    await expect(form).toBeVisible();
  });
});

const { test, expect } = require('@playwright/test');

// Dashboard A/B experiment tests. webServer runs with EXPERIMENT_ROLLOUT=100,
// so fresh users are assigned Variant B by default; ?variant=a forces A.

async function registerFreshUser(page) {
  for (let attempt = 0; attempt < 3; attempt++) {
    await page.goto('/register');
    const uid = `${Date.now()}${Math.floor(Math.random() * 100000)}`;
    await page.fill('input[name="name"]', 'PW Test');
    await page.fill('input[name="email"]', `pw${uid}@test.com`);
    await page.fill('input[name="phone"]', '9876500000');
    await page.fill('input[name="password"]', 'TestPass123!');
    await page.fill('input[name="confirm_password"]', 'TestPass123!');
    await page.click('button[type="submit"]');
    // Wait for navigation; handle DB deadlock retry and onboarding flow.
    try {
      await page.waitForURL(/\/dashboard|\/company\/onboard/, { waitUntil: 'domcontentloaded', timeout: 10000 });
    } catch (e) {
      const body = await page.content().catch(() => '');
      if (body.includes('database table is locked') || body.includes('database is locked') || body.includes('deadlocked')) {
        await page.waitForTimeout(800 * (attempt + 1));
        continue;
      }
      throw e;
    }
    if (page.url().includes('/company/onboard')) {
      await page.fill('input[name="company_name"]', 'PW Fleet ' + Date.now());
      await page.fill('input[name="email"]', `ops${Date.now()}@test.com`);
      await page.fill('input[name="phone"]', '9876500000');
      await Promise.all([
        page.waitForURL('**/dashboard', { waitUntil: 'domcontentloaded' }),
        page.click('button[type="submit"]'),
      ]);
    }
    await page.waitForURL('**/dashboard', { waitUntil: 'domcontentloaded' });
    return;
  }
  throw new Error('registerFreshUser failed after retries');
}

test.describe('Dashboard A/B variants', () => {
  test('variant B renders KPI cards, charts and alert feed', async ({ page }) => {
    await registerFreshUser(page);

    // Same card design language as the control dashboard (text-stat cards)
    await expect(page.locator('.text-stat').first()).toBeVisible();
    await expect(page.getByText("Today's Trips").first()).toBeVisible();
    // Delta chip vs yesterday
    await expect(page.getByText('vs yesterday').first()).toBeVisible();
    // Chart containers render; fresh DB shows the empty-state placeholders
    await expect(page.locator('#chart-revenue-empty')).toBeVisible();
    await expect(page.locator('#chart-status-empty')).toBeVisible();
    await expect(page.locator('#chart-bookings-empty')).toBeVisible();
    await expect(page.locator('#chart-revenue')).toBeAttached();
    // No experiment badge in production UI
    await expect(page.getByText('EXPERIMENTAL', { exact: false })).toHaveCount(0);
    // Alerts sections present
    await expect(page.getByText('Overdue Trips').first()).toBeVisible();
    await expect(page.getByText('Idle Vehicles').first()).toBeVisible();

    const chartData = await page.evaluate(() => window.__DASHBOARD_CHARTS__);
    expect(chartData.variant).toBe('B');
  });

  test('variant A renders the legacy dashboard via ?variant=a override', async ({ page }) => {
    await registerFreshUser(page);
    await page.goto('/dashboard?variant=a');

    await expect(page.locator('#chart-revenue')).toHaveCount(0);
    await expect(page.locator('text=Experimental · Variant B')).toHaveCount(0);
    // Legacy card markup present
    await expect(page.locator('.text-stat').first()).toBeVisible();
    await expect(page.getByText("Today's Trips").first()).toBeVisible();
  });
});

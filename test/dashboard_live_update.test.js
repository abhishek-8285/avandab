const { test, expect } = require('@playwright/test');

// Live board refresh: the SSE snapshot must update KPIs and charts without
// a page reload. Drives a synthetic snapshot through the real shipped update
// path (window.__updateDashboardKpis / __updateDashboardCharts).

async function registerFreshUser(page) {
  for (let attempt = 0; attempt < 3; attempt++) {
    await page.goto('/register');
    const uid = `${Date.now()}${Math.floor(Math.random() * 100000)}`;
    await page.fill('input[name="name"]', 'PW Live');
    await page.fill('input[name="email"]', `pwlive${uid}@test.com`);
    await page.fill('input[name="phone"]', '9876500001');
    await page.fill('input[name="password"]', 'TestPass123!');
    await page.fill('input[name="confirm_password"]', 'TestPass123!');
    await page.click('button[type="submit"]');
    try {
      await page.waitForURL(/\/dashboard|\/company\/onboard/, { waitUntil: 'domcontentloaded', timeout: 15000 });
    } catch (e) {
      const body = await page.content().catch(() => '');
      if (body.includes('database table is locked') || body.includes('database is locked') || body.includes('deadlocked')) {
        await page.waitForTimeout(800 * (attempt + 1));
        continue;
      }
      throw e;
    }
    if (page.url().includes('/company/onboard')) {
      await page.fill('input[name="company_name"]', 'PW Live ' + Date.now());
      await page.fill('input[name="email"]', `ops${Date.now()}@test.com`);
      await page.fill('input[name="phone"]', '9876500001');
      // Onboarding requires a full address (server validates).
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

const SNAP = {
  TodaysTripsCount: 5, ActiveTripsCount: 3, CompletedTripsCount: 1,
  CancelledTripsCount: 1, AvailableVehiclesCount: 4, AvailableDriversCount: 2,
  PendingPaymentsCount: 2, MonthlyRevenue: 12345.6, DeltaYesterday: 1,
  statusCounts: { scheduled: 2, completed: 1 },
  StatusCounts: { scheduled: 2, completed: 1 },
  revenueByDay: [{ Day: '2026-09-04', Total: 100 }],
  RevenueByDay: [{ Day: '2026-09-04', Total: 100 }],
  bookingsByDay: [{ Day: '2026-09-04', Count: 3 }],
  BookingsByDay: [{ Day: '2026-09-04', Count: 3 }],
  Attention: {
    UnassignedBookings: 2, MaintenanceDue: 0, OpenWorkOrders: 0,
    GarageVehicles: 0, OpenAlerts: 0, ActiveDTCs: 0,
    ExpiringEwaybills: 0, PendingKharcha: 0, LowFastag: 0,
  },
};

test.describe('Dashboard live refresh', () => {
  // Fresh-user registration hits SQLite writes; run serially to avoid
  // lock contention between parallel contexts.
  test.describe.configure({ mode: 'serial' });
  test('KPI numbers, bars and charts update without reload', async ({ page }) => {
    await registerFreshUser(page);
    await page.goto('/dashboard?variant=B');
    await expect(page.locator('#kpi-today')).toBeVisible();

    await page.evaluate((snap) => {
      window.__updateDashboardKpis(snap);
      window.__updateDashboardCharts(snap);
    }, SNAP);

    await expect(page.locator('#kpi-today')).toHaveText('5');
    await expect(page.locator('#kpi-active')).toHaveText('3');
    await expect(page.locator('#kpi-completed')).toHaveText('1');
    await expect(page.locator('#kpi-vehicles')).toHaveText('4');
    await expect(page.locator('#kpi-revenue')).toHaveText('₹12,345.60');
    await expect(page.locator('#kpi-active-bar')).toHaveAttribute('style', /width: 60%/);
    await expect(page.locator('#kpi-cancel-pct')).toHaveText('20% of today');
    // Fresh tenant has no attention backlogs: updater must tolerate the
    // missing cards without throwing (evaluate would reject otherwise).
    // Charts leave their empty states once data flows.
    await expect(page.locator('#chart-revenue-empty')).toBeHidden();
    await expect(page.locator('#chart-status-empty')).toBeHidden();
    await expect(page.locator('#chart-bookings-empty')).toBeHidden();
  });

  test('SSE stream stays connected and refreshes the stamp', async ({ page }) => {
    await registerFreshUser(page);
    await page.goto('/dashboard?variant=B');
    const stamp = page.locator('#dash-live-stamp');
    await expect(stamp).toBeVisible();
    // One server tick (5s) must reset the stamp without any reload.
    await expect(stamp).toContainText(/just now|ago/, { timeout: 15000 });
  });

  test('tables swap server-rendered fragments without reload', async ({ page }) => {    await registerFreshUser(page);
    await page.goto('/dashboard?variant=B');
    await expect(page.locator('#upcoming-tbody')).toBeAttached();
    // Stub the fragment endpoint; the swap path itself is shipped code.
    await page.route('/dashboard/tables', (route) => route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        regions: {
          'upcoming-tbody': '<tr><td colspan="7">SWAPPED-ROWS</td></tr>',
          'feed-overdue': '<div>SWAPPED-FEED</div>',
        },
        badges: { 'badge-upcoming': 7 },
      }),
    }));
    await page.evaluate(() => window.__dashboardTables.refresh());
    await expect(page.locator('#upcoming-tbody')).toContainText('SWAPPED-ROWS');
    await expect(page.locator('#feed-overdue')).toContainText('SWAPPED-FEED');
    await expect(page.locator('#badge-upcoming')).toHaveText('7');
  });

  test('table signature detects status flips, not just membership', async ({ page }) => {
    await registerFreshUser(page);
    await page.goto('/dashboard?variant=B');
    const sigs = await page.evaluate(() => {
      const base = { UpcomingTrips: [{ ID: 't1', Status: 'scheduled' }], RecentBookings: [] };
      const flipped = { UpcomingTrips: [{ ID: 't1', Status: 'started' }], RecentBookings: [] };
      const grown = { UpcomingTrips: [{ ID: 't1', Status: 'scheduled' }, { ID: 't2', Status: 'scheduled' }], RecentBookings: [] };
      return {
        base: window.__dashboardTables.sig(base),
        same: window.__dashboardTables.sig({ UpcomingTrips: [{ ID: 't1', Status: 'scheduled' }], RecentBookings: [] }),
        flipped: window.__dashboardTables.sig(flipped),
        grown: window.__dashboardTables.sig(grown),
        empty: window.__dashboardTables.sig({}),
      };
    });
    expect(sigs.same).toBe(sigs.base);
    expect(sigs.flipped).not.toBe(sigs.base);
    expect(sigs.grown).not.toBe(sigs.base);
    expect(sigs.empty).not.toBe(sigs.base);
  });
});

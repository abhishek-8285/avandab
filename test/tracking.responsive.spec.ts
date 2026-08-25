import { test, expect } from '@playwright/test';

// Parallel registration writes deadlock the fresh SQLite server DB
// ("database is deadlocked") — run these viewport sweeps one at a time.
test.describe.configure({ mode: 'serial' });

// Throwaway responsive verification for the tracking page restyle.
// Mobile (390x844): drawer off-canvas, sheet docks to bottom, no overflow.
// Tablet (820x1180): same off-canvas behavior below lg.

const VEHICLES = [
  {
    vehicle_id: '11111111-1111-1111-1111-111111111111',
    vehicle_number: 'MH01AB1111',
    trip_id: '',
    status: 'running',
    speed: 42,
    heading: 45,
    lat: 28.6139,
    lng: 77.209,
    fuel_level: 74,
    odometer: 1204.5,
    ts: new Date().toISOString(),
  },
  {
    vehicle_id: '22222222-2222-2222-2222-222222222222',
    vehicle_number: 'DL02CD2222',
    trip_id: '',
    status: 'no_signal',
    speed: 0,
    heading: 0,
    lat: 28.7,
    lng: 77.3,
    ts: new Date().toISOString(),
  },
];

async function register(page: import('@playwright/test').Page) {
  const email = `pw-resp-${Date.now()}-${Math.floor(Math.random() * 1e6)}@test.local`;
  const resp = await page.request.post('/register', {
    form: {
      name: 'Playwright Resp',
      email,
      phone: '9999999999',
      password: 'Sup3rSecret!',
      confirm_password: 'Sup3rSecret!',
    },
    maxRedirects: 0,
  });
  expect([200, 303]).toContain(resp.status());
}

for (const vp of [{ w: 390, h: 844, label: 'mobile' }, { w: 820, h: 1180, label: 'tablet' }]) {
  test(`tracking responsive @ ${vp.label} (${vp.w}x${vp.h})`, async ({ page }) => {
    await page.setViewportSize({ width: vp.w, height: vp.h });
    await register(page);

    await page.route('**/api/v1/telemetry/live', (route) => route.fulfill({ json: VEHICLES }));
    await page.route('**/api/v1/telemetry/geofences**', (route) => route.fulfill({ json: [] }));
    await page.route('**/api/v1/telemetry/history**', (route) => route.fulfill({ json: [] }));
    await page.route('**/api/v1/telemetry/reverse_geocode**', (route) => route.fulfill({ json: { display_name: 'Test Addr' } }));
    await page.route('**/api/v1/trips/*/summary', (route) => route.fulfill({ status: 404, json: { error: 'trip not found' } }));

    page.on('pageerror', (err) => console.log('PAGEERROR:', err.message));
    page.on('console', (msg) => { if (msg.type() === 'error') { console.log('CONSOLE:', msg.text()); } });
    await page.goto('/tracking');
    await expect(page.locator('#fleet-list .fleet-row')).toHaveCount(2, { timeout: 15000 });

    // No horizontal overflow.
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(overflow, 'no horizontal page overflow').toBeLessThanOrEqual(0);

    // Drawer is off-canvas below lg; expand rail visible.
    const drawerState = await page.evaluate(() => {
      const d = document.getElementById('fleet-drawer');
      const r = d!.getBoundingClientRect();
      return { left: r.left, width: r.width, vw: window.innerWidth };
    });
    if (vp.w < 1024) {
      expect(drawerState.left, 'drawer starts off-canvas').toBeLessThanOrEqual(0);
      await expect(page.locator('#drawer-expand-rail')).toBeVisible();
    }

    // Top strip fits viewport width (.z-10 skips the hidden stale banner).
    const strip = await page.locator('#map-theater > div.absolute.top-3.z-10').first().boundingBox();
    expect(strip).not.toBeNull();
    expect(strip!.x).toBeGreaterThanOrEqual(0);
    expect(strip!.x + strip!.width).toBeLessThanOrEqual(vp.w + 1);

    // Open drawer, pick vehicle → sheet docks bottom (mobile) / right (desktop).
    if (vp.w < 1024) {
      await page.locator('#drawer-expand-rail').click();
    }
    await page.locator('.fleet-row', { hasText: 'MH01AB1111' }).click();
    await expect(page.locator('#intel-detail-panel')).toBeVisible();
    await expect(page.locator('#intel-vehicle-id')).toHaveText('MH01AB1111');
    // Let the 0.16s panel-pop animation finish before geometry assertions.
    await page.waitForTimeout(250);

    const sheet = await page.locator('#intel-detail-panel').boundingBox();
    const theater = await page.locator('#map-theater').boundingBox();
    expect(sheet).not.toBeNull();
    expect(theater).not.toBeNull();
    if (vp.w < 1024) {
      // Sheet is flush with the map theater's bottom edge (not the raw
      // viewport — layout chrome can shave a few px).
      expect(sheet!.y + sheet!.height, 'sheet flush with theater bottom').toBeCloseTo(theater!.y + theater!.height, 1);
      expect(sheet!.width, 'sheet is full-width on mobile').toBe(theater!.width);
      // Drawer must have stowed after the pick (0.22s transform transition).
      await expect
        .poll(() => page.evaluate(() => document.getElementById('fleet-drawer')!.getBoundingClientRect().left), { timeout: 3000 })
        .toBeLessThanOrEqual(0);
    } else {
      expect(sheet!.x + sheet!.width, 'sheet docks to right edge on desktop').toBe(theater!.x + theater!.width);
    }

    // Tabs usable at this width.
    await page.locator('[data-sheet-tab="trip"]').click();
    await expect(page.locator('#trip-summary-empty')).toBeVisible();
    await page.locator('[data-sheet-tab="history"]').click();
    await expect(page.locator('#history-empty')).toBeVisible();

    // Bottom status bar stays inside viewport.
    const bar = await page.locator('#map-theater > div.absolute.bottom-3').first().boundingBox();
    expect(bar).not.toBeNull();
    expect(bar!.x + bar!.width).toBeLessThanOrEqual(vp.w + 1);
  });
}

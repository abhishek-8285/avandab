import { test, expect } from '@playwright/test';

// Parallel registration writes deadlock the fresh SQLite server DB
// ("database is deadlocked") — run these viewport sweeps one at a time.
test.describe.configure({ mode: 'serial' });

// Responsive verification for the tracking React island.
// Mobile (390x844): registry off-canvas, detail drawer docks to bottom, no overflow.
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

  // Fresh registrants are org_admins without company settings; the
  // compliance gate would redirect /tracking to /company/onboard.
  // (Strict CSRF requires Origin/Referer on session-cookie POSTs.)
  await page.goto('/login');
  const origin = new URL(page.url()).origin;
  const onboard = await page.request.post('/company/onboard', {
    headers: { Origin: origin, Referer: `${origin}/company/onboard` },
    form: {
      company_name: 'Playwright Resp Fleet',
      address: 'MIDC Bhosari, Pune 411026',
      phone: '9999999999',
      email,
    },
    maxRedirects: 0,
  });
  expect([200, 303]).toContain(onboard.status());
}

for (const vp of [{ w: 390, h: 844, label: 'mobile' }, { w: 820, h: 1180, label: 'tablet' }]) {
  test(`tracking responsive @ ${vp.label} (${vp.w}x${vp.h})`, async ({ page }) => {
    await page.setViewportSize({ width: vp.w, height: vp.h });
    await register(page);

    await page.route('**/api/v1/telemetry/live', (route) => route.fulfill({ json: VEHICLES }));
    await page.route('**/api/v1/telemetry/geofences**', (route) => route.fulfill({ json: [] }));
    await page.route('**/api/v1/trips/*/summary', (route) => route.fulfill({ status: 404, json: { error: 'trip not found' } }));

    page.on('pageerror', (err) => console.log('PAGEERROR:', err.message));
    page.on('console', (msg) => { if (msg.type() === 'error') { console.log('CONSOLE:', msg.text()); } });

    await page.goto('/tracking');
    // Registry stowed off-canvas below lg; expand rail visible instead.
    await expect(page.locator('#drawer-expand-rail')).toBeVisible({ timeout: 15000 });
    const drawerState = await page.evaluate(() => {
      const d = document.getElementById('fleet-drawer');
      const r = d!.getBoundingClientRect();
      return { left: r.left, width: r.width, vw: window.innerWidth };
    });
    expect(drawerState.left, 'drawer starts off-canvas').toBeLessThanOrEqual(0);

    // No horizontal overflow.
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(overflow, 'no horizontal page overflow').toBeLessThanOrEqual(0);

    // Top bar fits viewport width.
    const bar = await page.locator('#map-theater .ti-topbar').boundingBox();
    expect(bar).not.toBeNull();
    expect(bar!.x).toBeGreaterThanOrEqual(0);
    expect(bar!.x + bar!.width).toBeLessThanOrEqual(vp.w + 1);

    // Open registry, pick vehicle → drawer docks to the theater bottom edge.
    await page.locator('#drawer-expand-rail').click();
    await expect(page.locator('#fleet-list .fleet-row')).toHaveCount(2, { timeout: 15000 });
    await page.locator('.fleet-row', { hasText: 'MH01AB1111' }).click();
    await expect(page.locator('#intel-detail-panel')).toBeVisible();
    await expect(page.locator('#intel-vehicle-id')).toHaveText('MH01AB1111');

    const sheet = await page.locator('#intel-detail-panel').boundingBox();
    const theater = await page.locator('#map-theater').boundingBox();
    expect(sheet).not.toBeNull();
    expect(theater).not.toBeNull();
    expect(sheet!.y + sheet!.height, 'sheet flush with theater bottom').toBeCloseTo(theater!.y + theater!.height, 1);
    expect(sheet!.width, 'sheet is full-width on small screens').toBe(theater!.width);
    // Registry stowed after the pick (0.22s transform transition).
    await expect
      .poll(() => page.evaluate(() => document.getElementById('fleet-drawer')!.getBoundingClientRect().left), { timeout: 3000 })
      .toBeLessThanOrEqual(0);
  });
}

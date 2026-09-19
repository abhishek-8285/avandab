import { test, expect } from '@playwright/test';
import { registerFreshUser } from './utils/register';
import { expectNoHorizontalOverflow, isBelowLg } from './utils/responsive';

// Parallel registration writes deadlock the fresh SQLite server DB
// ("database is deadlocked") — run these viewport sweeps one at a time.
test.describe.configure({ mode: 'serial' });

// Responsive verification for the tracking React island.
//
// The viewport now comes from the PROJECT (desktop 1440 / tablet 820 / mobile
// 390) rather than being set per-test, so every project gets the same
// assertions with no duplicated loop. Layout expectations branch twice:
//
//   < 768px  -> phone: full-width map + fleet bottom sheet (collapsed /
//                half / full). No side drawer, no expand rail.
//   768–1023 -> tablet: map + collapsible off-canvas registry, expand rail
//                re-opens it; vehicle detail sheet docks to the bottom of
//                the map theater at full width.
//   >= 1024  -> desktop: registry pinned open, no rail, detail sheet is a
//                side panel.

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

// Registration uses the shared full-UI helper (see ./utils/register): API-only
// registration was flaky — it sometimes produced no browser session, so
// /tracking redirected to /login under parallel workers.

test('tracking responsive layout', async ({ page }) => {
  await registerFreshUser(page, 'pw-resp');

  await page.route('**/api/v1/telemetry/live', (route) => route.fulfill({ json: VEHICLES }));
  await page.route('**/api/v1/telemetry/geofences**', (route) => route.fulfill({ json: [] }));
  await page.route('**/api/v1/trips/*/summary', (route) =>
    route.fulfill({ status: 404, json: { error: 'trip not found' } }),
  );

  page.on('pageerror', (err) => console.log('PAGEERROR:', err.message));
  page.on('console', (msg) => {
    if (msg.type() === 'error') {
      console.log('CONSOLE:', msg.text());
    }
  });

  await page.goto('/tracking');

  const vw = page.viewportSize()!.width;
  const isPhone = vw < 768;
  const compact = isBelowLg(page);

  if (isPhone) {
    // Bottom sheet, default collapsed; no drawer, no rail.
    await expect(page.locator('#fleet-sheet')).toBeVisible({ timeout: 15000 });
    await expect(page.locator('#fleet-sheet.collapsed')).toBeVisible();
    await expect(page.locator('#fleet-drawer')).toHaveCount(0);
    await expect(page.locator('#drawer-expand-rail')).toHaveCount(0);
    await expect(page.locator('#fleet-list')).toBeHidden();
    // Map owns the width: theater spans the viewport.
    const theaterBox = await page.locator('#map-theater').boundingBox();
    expect(theaterBox).not.toBeNull();
    expect(theaterBox!.x).toBeLessThanOrEqual(1);
    expect(theaterBox!.x + theaterBox!.width).toBeGreaterThanOrEqual(vw - 1);
    // Expand to half — search, filters, rows appear.
    await page.locator('.ti-sheet-summary').click();
    await expect(page.locator('#fleet-sheet.half')).toBeVisible();
    await expect(page.locator('#fleet-list')).toBeVisible();
  } else if (compact) {
    // Registry stowed off-canvas below lg; expand rail visible instead.
    await expect(page.locator('#drawer-expand-rail')).toBeVisible({ timeout: 15000 });
    const drawerState = await page.evaluate(() => {
      const d = document.getElementById('fleet-drawer');
      const r = d!.getBoundingClientRect();
      return { left: r.left, width: r.width, vw: window.innerWidth };
    });
    expect(drawerState.left, 'drawer starts off-canvas').toBeLessThanOrEqual(0);
  } else {
    // Desktop: registry is pinned open, no rail.
    await expect(page.locator('#drawer-expand-rail')).toHaveCount(0);
    await expect(page.locator('#fleet-drawer')).toBeVisible({ timeout: 15000 });
  }

  await expectNoHorizontalOverflow(page, '/tracking');

  // Top bar fits viewport width.
  const bar = await page.locator('#map-theater .ti-topbar').boundingBox();
  expect(bar).not.toBeNull();
  expect(bar!.x).toBeGreaterThanOrEqual(0);
  expect(bar!.x + bar!.width).toBeLessThanOrEqual(vw + 1);

  if (!isPhone && compact) {
    await page.locator('#drawer-expand-rail').click();
  }
  await expect(page.locator('#fleet-list .fleet-row')).toHaveCount(2, { timeout: 15000 });
  await page.locator('.fleet-row', { hasText: 'MH01AB1111' }).click();
  await expect(page.locator('#intel-detail-panel')).toBeVisible();
  await expect(page.locator('#intel-vehicle-id')).toHaveText('MH01AB1111');

  const sheet = await page.locator('#intel-detail-panel').boundingBox();
  const theater = await page.locator('#map-theater').boundingBox();
  expect(sheet).not.toBeNull();
  expect(theater).not.toBeNull();

  if (isPhone) {
    // Sheet docks to the theater's bottom edge at full width; the fleet
    // sheet drops back to collapsed so the centered marker stays visible.
    expect(sheet!.y + sheet!.height, 'sheet flush with theater bottom').toBeCloseTo(
      theater!.y + theater!.height,
      1,
    );
    expect(sheet!.width, 'sheet is full-width on small screens').toBe(theater!.width);
    await expect(page.locator('#fleet-sheet.collapsed')).toBeVisible();
  } else if (compact) {
    // Sheet docks to the theater's bottom edge at full width.
    expect(sheet!.y + sheet!.height, 'sheet flush with theater bottom').toBeCloseTo(
      theater!.y + theater!.height,
      1,
    );
    expect(sheet!.width, 'sheet is full-width on small screens').toBe(theater!.width);
    // Registry stowed after the pick (0.22s transform transition).
    await expect
      .poll(
        () => page.evaluate(() => document.getElementById('fleet-drawer')!.getBoundingClientRect().left),
        { timeout: 3000 },
      )
      .toBeLessThanOrEqual(0);
  } else {
    // Desktop: sheet is a side panel, so it is narrower than the theater.
    expect(sheet!.width, 'sheet is a side panel at lg+').toBeLessThan(theater!.width);
  }

  await expectNoHorizontalOverflow(page, '/tracking with detail sheet open');
});

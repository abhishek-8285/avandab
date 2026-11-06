import { test, expect, type Page } from '@playwright/test';
import { registerFreshUser } from './utils/register';
import { expectNoHorizontalOverflow } from './utils/responsive';

// Trip View mobile UX (internal/templates/trip_view.html, handlers/trips.go View):
// the page used to stack every section vertically at every width. Secondary
// sections are now <details class="vsec" data-m-open="0"> — rendered open for
// desktop and no-JS users, collapsed on phones by the layout script. Timeline,
// stops and the action sidebar must stay visible at all times.
test.describe.configure({ mode: 'serial' });

let tripSeq = 0;
function uniqueReg(tag: string): string {
  tripSeq += 1;
  return `MH${String(Date.now()).slice(-6)}${tag}${tripSeq}`.toUpperCase().replace(/[^A-Z0-9]/g, '').slice(0, 14);
}

function futureDate(): string {
  return new Date(Date.now() + 365 * 24 * 60 * 60 * 1000).toISOString().slice(0, 10);
}

async function seedTrip(page: Page): Promise<void> {
  const base = new URL(page.url()).origin;
  const headers = { Origin: base, Referer: `${base}/trips/new` };
  const reg = uniqueReg('TV');

  // A trip requires a route, so create one and read its id back from the list.
  const route = await page.request.post('/routes/new', {
    headers,
    form: { source: 'Delhi', destination: 'Jaipur', distance: '280', estimated_hours: '6', standard_fare: '12000' },
  });
  expect(route.ok(), 'seed route').toBeTruthy();
  await page.goto('/routes');
  const routeHref = await page.locator('#list-table a[href^="/routes/"]:not([href$="/edit"])').first().getAttribute('href');
  expect(routeHref, 'route href').toBeTruthy();
  const routeID = routeHref!.replace('/routes/', '');

  // The trip view renders a vehicle link when one is assigned, so seed both.
  const vehicle = await page.request.post('/vehicles/new', {
    headers,
    form: {
      registration_number: reg,
      vehicle_type: 'truck',
      capacity: '18000',
      fuel_type: 'diesel',
      insurance_expiry: futureDate(),
      fitness_expiry: futureDate(),
      permit_expiry: futureDate(),
    },
  });
  expect(vehicle.ok(), `seed vehicle ${reg}`).toBeTruthy();

  const trip = await page.request.post('/trips/new', {
    headers,
    form: {
      route_id: routeID,
      departure_time: new Date(Date.now() + 24 * 60 * 60 * 1000).toISOString().slice(0, 16),
      remarks: 'PW responsive fixture',
    },
  });
  expect(trip.ok(), 'seed trip').toBeTruthy();
}

/** Open the first trip row's View page; returns its URL. */
async function openFirstTrip(page: Page): Promise<string> {
  await page.goto('/trips');
  await expect(page.locator('table.rtable tbody tr, [data-trip-card]').first()).toBeVisible({ timeout: 15000 });
  const viewHref = await page.locator('table.rtable tbody tr a:has-text("View")').first().getAttribute('href');
  expect(viewHref, 'view link').toBeTruthy();
  await page.goto(viewHref!);
  await expect(page.locator('[data-trip-view]')).toBeVisible({ timeout: 15000 });
  return page.url();
}

const COLLAPSIBLE = ['fuel', 'shipment', 'pod', 'kharcha'];

test('trip view keeps the working surface visible on phones', async ({ page }) => {
  await registerFreshUser(page, 'pw-tripview');
  await seedTrip(page);
  await page.setViewportSize({ width: 390, height: 844 });
  await openFirstTrip(page);

  // Identity band, execution timeline and the action sidebar are never hidden.
  await expect(page.locator('[data-trip-view] h1')).toBeVisible();
  await expect(page.locator('[data-section="timeline"]')).toBeVisible();
  await expect(page.locator('aside').getByText('Actions', { exact: true })).toBeVisible();
});

test('trip view collapses secondary sections on phones and expands on tap', async ({ page }) => {
  await registerFreshUser(page, 'pw-tripviewcol');
  await seedTrip(page);
  await page.setViewportSize({ width: 390, height: 844 });
  await openFirstTrip(page);

  for (const sec of COLLAPSIBLE) {
    const sel = `details[data-section="${sec}"]`;
    await expect(page.locator(sel), `${sec} present`).toHaveCount(1);
    await expect(page.locator(sel), `${sec} collapsed`).toHaveJSProperty('open', false);
  }
  // Native details: tapping the summary opens it, second tap closes it.
  await page.locator('details[data-section="shipment"] > summary').click();
  await expect(page.locator('details[data-section="shipment"]')).toHaveJSProperty('open', true);
  await page.locator('details[data-section="shipment"] > summary').click();
  await expect(page.locator('details[data-section="shipment"]')).toHaveJSProperty('open', false);

  // The section content is still there once expanded (no data loss on collapse).
  await page.locator('details[data-section="shipment"] > summary').click();
  await expect(page.locator('details[data-section="shipment"]').getByText('Driver', { exact: true }).first()).toBeVisible();
  await expect(page.locator('details[data-section="shipment"]').getByText('Vehicle', { exact: true }).first()).toBeVisible();
});

test('trip view desktop stays fully expanded', async ({ page }) => {
  await registerFreshUser(page, 'pw-tripviewdesk');
  await seedTrip(page);
  await page.setViewportSize({ width: 1280, height: 900 });
  await openFirstTrip(page);

  for (const sec of COLLAPSIBLE) {
    await expect(page.locator(`details[data-section="${sec}"]`), `${sec} open on desktop`).toHaveJSProperty('open', true);
  }
  await expectNoHorizontalOverflow(page, 'trip view desktop');
});

for (const width of [288, 320, 360, 390, 412, 430]) {
  test(`trip view has no overflow at ${width}px`, async ({ page }) => {
    await registerFreshUser(page, `pw-tripview${width}`);
    await seedTrip(page);
    await page.setViewportSize({ width, height: 800 });
    await openFirstTrip(page);

    await expectNoHorizontalOverflow(page, `trip view ${width}px`);
    // Expanding a collapsed section must not introduce overflow.
    await page.locator('details[data-section="shipment"] > summary').click();
    await expectNoHorizontalOverflow(page, `trip view ${width}px shipment`);
    await page.locator('details[data-section="kharcha"] > summary').click();
    await expectNoHorizontalOverflow(page, `trip view ${width}px kharcha`);
  });
}

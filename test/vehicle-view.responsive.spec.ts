import { test, expect, type Page } from '@playwright/test';
import { registerFreshUser } from './utils/register';
import { expectNoHorizontalOverflow } from './utils/responsive';

// Vehicle View mobile UX (internal/templates/vehicle_view.html,
// handlers/vehicles.go View): header identity, primary summary, grouped
// odometer, collapsed secondary sections on phones, compact empty states,
// separated AV teleop deck — across 288–430px, plus desktop regression.
test.describe.configure({ mode: 'serial' });

let regSeq = 0;
function uniqueReg(tag: string): string {
  regSeq += 1;
  return `MH${String(Date.now()).slice(-6)}${tag}${regSeq}`.toUpperCase().replace(/[^A-Z0-9]/g, '').slice(0, 14);
}

function futureDate(): string {
  return new Date(Date.now() + 365 * 24 * 60 * 60 * 1000).toISOString().slice(0, 10);
}

async function seedVehicle(page: Page, reg: string, extra: Record<string, string> = {}): Promise<void> {
  const base = new URL(page.url()).origin;
  const resp = await page.request.post('/vehicles/new', {
    headers: { Origin: base, Referer: `${base}/vehicles/new` },
    form: {
      registration_number: reg,
      vehicle_type: 'truck',
      capacity: '18000',
      fuel_type: 'diesel',
      insurance_expiry: futureDate(),
      fitness_expiry: futureDate(),
      permit_expiry: futureDate(),
      ...extra,
    },
  });
  expect(resp.ok(), `seed ${reg}`).toBeTruthy();
}

/** Open the first vehicle row's View page; returns its URL. */
async function openFirstVehicle(page: Page): Promise<string> {
  await page.goto('/vehicles');
  await expect(page.locator('table.rtable tbody tr, [data-vehicle-card]').first()).toBeVisible({ timeout: 15000 });
  const viewHref = await page.locator('table.rtable tbody tr a:has-text("View")').first().getAttribute('href');
  expect(viewHref, 'view link').toBeTruthy();
  await page.goto(viewHref!);
  await expect(page.locator('[data-vehicle-view]')).toBeVisible({ timeout: 15000 });
  return page.url();
}

async function withinViewport(page: Page, selector: string, label: string) {
  const vw = page.viewportSize()!.width;
  const box = await page.locator(selector).first().boundingBox();
  expect(box, `${label} rendered`).not.toBeNull();
  expect(box!.x, `${label} left edge`).toBeGreaterThanOrEqual(-1);
  expect(box!.x + box!.width, `${label} right edge`).toBeLessThanOrEqual(vw + 1);
}

test('vehicle view header, summary and odometer formatting', async ({ page }) => {
  await registerFreshUser(page, 'pw-vehview');
  await seedVehicle(page, uniqueReg('ODO'), { current_mileage: '12345', standard_kmpl: '4.5' });
  await openFirstVehicle(page);

  // A. header: back link, identity, status, actions preserved.
  await expect(page.locator('a.back-link[href="/vehicles"]')).toBeVisible();
  await expect(page.locator('[data-vehicle-view] h1')).toBeVisible();
  await expect(page.locator('[data-vehicle-view] a[href$="/edit"]')).toBeVisible();

  // B. primary summary: all five operational fields visible.
  for (const label of ['Status', 'Fuel', 'Capacity', 'Odometer', 'Standard KMPL']) {
    await expect(page.locator('[data-summary]').getByText(label, { exact: true }).first()).toBeVisible();
  }

  // C. odometer: grouped km, never raw template syntax.
  const odo = page.locator('[data-odometer]');
  await expect(odo).toContainText('12,345 km');
  await expect(odo).not.toContainText('%');
  await expect(page.locator('[data-kmpl]')).toContainText('4.50');
  const bodyText = await page.locator('[data-vehicle-view]').innerText();
  expect(bodyText, 'no raw printf artifacts').not.toMatch(/%!|0x[0-9a-f]{4,}/);
});

test('vehicle view missing odometer renders dash, not zero', async ({ page }) => {
  await registerFreshUser(page, 'pw-vehviewdash');
  await seedVehicle(page, uniqueReg('DASH'));
  await openFirstVehicle(page);
  await expect(page.locator('[data-odometer]')).toHaveText('—');
});

test('vehicle view collapses secondary sections on phones', async ({ page }) => {
  await registerFreshUser(page, 'pw-vehviewcol');
  await seedVehicle(page, uniqueReg('COL'));
  await page.setViewportSize({ width: 390, height: 844 });
  await openFirstVehicle(page);

  // Summary + maintenance status + teleop stay visible.
  await expect(page.locator('[data-summary]')).toBeVisible();
  await expect(page.locator('[data-maintenance]')).toBeVisible();
  await expect(page.locator('[data-teleop]')).toBeVisible();

  // Secondary sections start collapsed…
  for (const sec of ['compliance', 'fleet', 'trips', 'measuring', 'jobcards', 'telemetry']) {
    await expect(page.locator(`details[data-section="${sec}"]`), `${sec} collapsed`).toHaveJSProperty('open', false);
  }
  // …but expand on tap (native details, no JS dependency).
  await page.locator('details[data-section="compliance"] > summary').click();
  await expect(page.locator('details[data-section="compliance"]')).toHaveJSProperty('open', true);
  // Fleet fields preserved (all eight + class/ownership + description).
  await page.locator('details[data-section="fleet"] > summary').click();
  await expect(page.locator('details[data-section="fleet"]')).toHaveJSProperty('open', true);
  for (const label of ['Fleet Class / Ownership', 'Description', 'Manufacturer / Model', 'Fleet Number', 'Facility ID', 'Cost Center', 'Chassis No.', 'Engine Serial No.']) {
    await expect(page.locator('details[data-section="fleet"]').getByText(label, { exact: true }).first()).toBeVisible();
  }

  // Compact empty states (no large cards).
  for (const hook of ['[data-empty-trips]', '[data-empty-points]', '[data-empty-jobcards]']) {
    const box = await page.locator(hook).boundingBox();
    expect(box, `${hook} compact`).not.toBeNull();
    expect(box!.height, `${hook} height`).toBeLessThan(60);
  }
});

test('vehicle view teleoperation deck stays separated and usable', async ({ page }) => {
  await registerFreshUser(page, 'pw-vehviewtel');
  await seedVehicle(page, uniqueReg('TEL'));
  await page.setViewportSize({ width: 360, height: 800 });
  await openFirstVehicle(page);

  const deck = page.locator('[data-teleop]');
  await expect(deck.getByText('AV Teleoperation Deck')).toBeVisible();
  // Command semantics untouched: three POST forms, exact command types,
  // E-STOP keeps its confirmation gate.
  for (const cmd of ['E_STOP', 'HOLD_POSITION', 'RESUME_MISSION']) {
    const form = deck.locator(`form[action$="/command"] input[value="${cmd}"]`);
    await expect(form, cmd).toHaveCount(1);
  }
  await expect(deck.locator('input[value="E_STOP"]').locator('..').locator('button[data-confirm]')).toHaveCount(1);
  // Buttons fit the viewport side by side without overlap.
  await withinViewport(page, '[data-teleop] button:has-text("E-STOP")', 'e-stop');
  await withinViewport(page, '[data-teleop] button:has-text("HOLD")', 'hold');
  await withinViewport(page, '[data-teleop] button:has-text("RESUME")', 'resume');
  const estop = (await page.locator('[data-teleop] button:has-text("E-STOP")').boundingBox())!;
  const hold = (await page.locator('[data-teleop] button:has-text("HOLD")').boundingBox())!;
  expect(hold.x, 'hold right of e-stop').toBeGreaterThanOrEqual(estop.x + estop.width - 1);
});

for (const width of [288, 320, 360, 390, 412, 430]) {
  test(`vehicle view has no overflow at ${width}px`, async ({ page }) => {
    await registerFreshUser(page, `pw-vehview${width}`);
    await seedVehicle(page, uniqueReg(`W${width}`), { current_mileage: '123456' });
    await page.setViewportSize({ width, height: 800 });
    await openFirstVehicle(page);

    await expectNoHorizontalOverflow(page, `vehicle view ${width}px`);
    await withinViewport(page, '[data-summary]', 'summary');
    await withinViewport(page, '[data-teleop]', 'teleop deck');
    await withinViewport(page, '[data-section="measuring"] summary', 'measuring header');
    // Measuring create controls fit when the section is opened.
    await page.locator('[data-section="measuring"] > summary').click();
    await withinViewport(page, 'input[name="annual_estimate"]', 'annual estimate input');
    await withinViewport(page, 'input[name="description"]', 'point description input');
    await withinViewport(page, '[data-section="measuring"] button:has-text("Create")', 'create point button');
  });
}

test('vehicle view desktop stays fully expanded', async ({ page }) => {
  await registerFreshUser(page, 'pw-vehviewdesk');
  await seedVehicle(page, uniqueReg('DESK'), { current_mileage: '12345' });
  await page.setViewportSize({ width: 1280, height: 900 });
  await openFirstVehicle(page);

  for (const sec of ['compliance', 'fleet', 'trips', 'measuring', 'jobcards', 'telemetry']) {
    await expect(page.locator(`details[data-section="${sec}"]`), `${sec} open on desktop`).toHaveJSProperty('open', true);
  }
  await expect(page.locator('[data-odometer]')).toContainText('12,345 km');
  await expectNoHorizontalOverflow(page, 'vehicle view desktop');
});

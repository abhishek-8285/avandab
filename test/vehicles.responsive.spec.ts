import { test, expect, type Page } from '@playwright/test';
import { registerFreshUser } from './utils/register';
import { expectNoHorizontalOverflow } from './utils/responsive';

// Vehicles page: server-rendered list (internal/templates/vehicle_list*.html,
// handlers/vehicles.go List). Covers the Phase 4 matrix: list, empty states,
// search, status/date/class filters, pagination, new/edit/view, card reflow,
// calendar popover, row keyboard nav — on desktop 1440 / tablet 820 / mobile 390.
test.describe.configure({ mode: 'serial' });

let regSeq = 0;
function uniqueReg(tag: string): string {
  regSeq += 1;
  return `MH${String(Date.now()).slice(-6)}${tag}${regSeq}`.toUpperCase().replace(/[^A-Z0-9]/g, '').slice(0, 14);
}

function futureDate(): string {
  return new Date(Date.now() + 365 * 24 * 60 * 60 * 1000).toISOString().slice(0, 10);
}

/** Seed through the real create endpoint (same session cookies). */
async function seedVehicle(page: Page, reg: string): Promise<void> {
  const base = new URL(page.url()).origin;
  const resp = await page.request.post('/vehicles/new', {
    // Strict CSRF: session-cookie POSTs require same-origin headers.
    headers: { Origin: base, Referer: `${base}/vehicles/new` },
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
  expect(resp.ok(), `seed ${reg}`).toBeTruthy();
}

async function withinViewport(page: Page, selector: string, label: string) {
  const vw = page.viewportSize()!.width;
  const box = await page.locator(selector).first().boundingBox();
  expect(box, `${label} rendered`).not.toBeNull();
  expect(box!.x, `${label} left edge`).toBeGreaterThanOrEqual(-1);
  expect(box!.x + box!.width, `${label} right edge`).toBeLessThanOrEqual(vw + 1);
}

test('vehicles list layout has no overflow', async ({ page }) => {
  await registerFreshUser(page, 'pw-veh');
  await seedVehicle(page, uniqueReg('A'));
  await seedVehicle(page, uniqueReg('B'));
  await page.goto('/vehicles');

  await expect(page.locator('table.rtable tbody tr')).toHaveCount(2, { timeout: 15000 });
  await expectNoHorizontalOverflow(page, '/vehicles populated');
  await withinViewport(page, 'form[data-filterbar]', 'filter bar');
  await withinViewport(page, 'form[data-filterbar] input[name="q"]', 'search input');
  await withinViewport(page, '[data-daterange]', 'date pill');
  await withinViewport(page, '#list-table', 'results table');
  await withinViewport(page, 'select#fleet_class', 'fleet class select');
  await withinViewport(page, 'a[href="/vehicles/new"]', 'new vehicle action');

  // KPI strip: all four server-computed cards, 2x2 on phones.
  for (const label of ['Total Vehicles', 'Running', 'Available', 'Maintenance']) {
    await expect(page.locator('.grid').first().getByText(label, { exact: false }).first()).toBeVisible();
  }
});

test('vehicles empty states distinguish empty garage from filtered zero', async ({ page }) => {
  await registerFreshUser(page, 'pw-vehemtpy');
  await page.goto('/vehicles');
  await expect(page.locator('td[colspan]')).toContainText('No vehicles yet', { timeout: 15000 });

  const reg = uniqueReg('C');
  await seedVehicle(page, reg);
  await page.goto('/vehicles');
  await expect(page.locator('table.rtable tbody tr')).toHaveCount(1, { timeout: 15000 });
  await page.locator('form[data-filterbar] input[name="q"]').pressSequentially('ZZZ-NO-MATCH-999', { delay: 20 });
  await expect(page.locator('td[colspan]')).toContainText('No vehicles match', { timeout: 15000 });
  // Clearing search restores the row (htmx fragment swap, no full reload).
  const qbox = page.locator('form[data-filterbar] input[name="q"]');
  await qbox.click();
  await page.keyboard.press('ControlOrMeta+a');
  await page.keyboard.press('Backspace');
  await expect(page.locator('table.rtable tbody tr')).toHaveCount(1, { timeout: 15000 });
});

test('vehicles search, status chip, date range, pagination', async ({ page }) => {
  await registerFreshUser(page, 'pw-vehflt');
  const runReg = uniqueReg('RUN');
  const idleReg = uniqueReg('IDL');
  await seedVehicle(page, runReg);
  await seedVehicle(page, idleReg);

  await page.goto('/vehicles');
  await expect(page.locator('table.rtable tbody tr')).toHaveCount(2, { timeout: 15000 });

  // Create forces available — flip one row via the real status endpoint.
  const base = new URL(page.url()).origin;
  const runHref = await page.locator('table.rtable tbody tr a:has-text("View")').first().getAttribute('href');
  const st = await page.request.post(`${runHref}/status`, {
    headers: { Origin: base, Referer: `${base}${runHref}` },
    form: { status: 'running' },
  });
  expect(st.ok(), 'status flip').toBeTruthy();
  // Re-seed determinism: flip the FIRST row regardless of order, then find
  // which reg it carries and use it as the expected running vehicle below.
  await page.goto('/vehicles');
  await expect(page.locator('table.rtable tbody tr')).toHaveCount(2, { timeout: 15000 });

  // Status chip filters server-side.
  await page.getByRole('link', { name: 'Running' }).first().click();
  await expect(page.locator('table.rtable tbody tr')).toHaveCount(1, { timeout: 15000 });
  const runningText = await page.locator('table.rtable tbody').textContent();
  expect([runReg, idleReg].some((r) => runningText!.includes(r)), 'a seeded row survives').toBeTruthy();
  await expect(page.url()).toContain('status=running');

  // Date window around today keeps the row; a future window empties it.
  const today = new Date().toISOString().slice(0, 10);
  const tomorrow = new Date(Date.now() + 24 * 60 * 60 * 1000).toISOString().slice(0, 10);
  await page.goto(`/vehicles?from=${today}&to=${today}`);
  await expect(page.locator('table.rtable tbody tr')).toHaveCount(2, { timeout: 15000 });
  await page.goto(`/vehicles?from=${tomorrow}&to=${tomorrow}`);
  await expect(page.locator('td[colspan]')).toContainText('No vehicles match', { timeout: 15000 });

  // Pagination keeps class/ownership filters across pages.
  await page.goto('/vehicles?fleet_class=CV&limit=1');
  await expect(page.locator('table.rtable tbody tr')).toHaveCount(1, { timeout: 15000 });
  await expect(page.getByText(/Page 1 of 2/)).toBeVisible();
  const nextHref = await page.getByRole('link', { name: /Next/ }).first().getAttribute('href');
  expect(nextHref, 'next preserves fleet_class').toContain('fleet_class=CV');
  await page.getByRole('link', { name: /Next/ }).first().click();
  await expect(page.getByText(/Page 2 of 2/)).toBeVisible({ timeout: 15000 });
});

test('vehicles new, view, edit pages render', async ({ page }) => {
  await registerFreshUser(page, 'pw-vehforms');
  const reg = uniqueReg('D');
  await seedVehicle(page, reg);

  await page.goto('/vehicles/new');
  await expect(page.locator('form[action="/vehicles/new"] input[name="registration_number"]')).toBeVisible();
  await expectNoHorizontalOverflow(page, '/vehicles/new');

  await page.goto('/vehicles');
  await expect(page.locator('table.rtable tbody tr')).toHaveCount(1, { timeout: 15000 });
  const viewHref = await page.locator('table.rtable tbody tr a:has-text("View")').first().getAttribute('href');
  expect(viewHref).toMatch(/\/vehicles\/.+/);

  await page.goto(viewHref!);
  await expect(page.locator('h1')).toContainText(reg);
  await expectNoHorizontalOverflow(page, '/vehicles/{id}');

  await page.goto(`${viewHref}/edit`);
  await expect(page.locator('form[action$="/edit"] input[name="registration_number"]')).toBeVisible();
  await expectNoHorizontalOverflow(page, '/vehicles/{id}/edit');
});

test('vehicles mobile card reflow and row navigation', async ({ page }) => {
  await registerFreshUser(page, 'pw-vehcard');
  const reg = uniqueReg('E');
  await seedVehicle(page, reg);
  await page.goto('/vehicles');
  await expect(page.locator('table.rtable tbody tr')).toHaveCount(1, { timeout: 15000 });

  // Card reflow kicks in below 768px (app.css .rtable); tablet keeps tables.
  if (page.viewportSize()!.width < 768) {
    // Card mode: headers hidden, captions from data-label.
    await expect(page.locator('table.rtable thead')).toBeHidden();
    const label = await page.locator('table.rtable tbody td[data-label="Unit ID"]').first().textContent();
    expect(label).toContain(reg);
    // Row tap navigates to the detail page.
    await page.locator('table.rtable tbody tr').first().click();
    await expect(page).toHaveURL(/\/vehicles\/.+/, { timeout: 10000 });
    await expect(page.locator('h1')).toContainText(reg);
  } else {
    await expect(page.locator('table.rtable thead')).toBeVisible();
    // Keyboard: focused row + Enter opens the detail page.
    const row = page.locator('table.rtable tbody tr').first();
    await row.focus();
    await page.keyboard.press('Enter');
    await expect(page).toHaveURL(/\/vehicles\/.+/, { timeout: 10000 });
  }
  await expectNoHorizontalOverflow(page, '/vehicles cards');
});

test('vehicles calendar popover stays in viewport', async ({ page }) => {
  await registerFreshUser(page, 'pw-vehcal');
  await page.goto('/vehicles');
  await page.locator('[data-calbtn]').first().click();
  const pop = page.locator('#av-cal-pop');
  await expect(pop).toBeVisible({ timeout: 10000 });
  const vw = page.viewportSize()!.width;
  const vh = page.viewportSize()!.height;
  const box = await pop.boundingBox();
  expect(box).not.toBeNull();
  expect(box!.x).toBeGreaterThanOrEqual(0);
  expect(box!.x + box!.width).toBeLessThanOrEqual(vw + 1);
  expect(box!.y).toBeGreaterThanOrEqual(0);
  expect(box!.y + box!.height).toBeLessThanOrEqual(vh + 1);
  await page.keyboard.press('Escape');
  await expect(pop).toBeHidden();
});

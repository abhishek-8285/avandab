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
  const regA = uniqueReg('A');
  const regB = uniqueReg('B');
  await seedVehicle(page, regA);
  await seedVehicle(page, regB);
  await page.goto('/vehicles');

  await expect(page.locator('table.rtable tbody tr')).toHaveCount(2, { timeout: 15000 }).catch(async (e) => {
    // Self-diagnosing: a shared parallel E2E DB once showed 20 rows here for
    // a fresh 2-vehicle tenant — dump the visible regs so the next CI failure
    // names the polluting rows instead of just a count.
    const regs = await page.locator('table.rtable tbody tr td:first-child').allTextContents().catch(() => []);
    console.log(`VEHICLES-OVERFLOW-DIAG seeded=[${regA}, ${regB}] visible=${JSON.stringify(regs.slice(0, 25))}`);
    throw e;
  });
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

  // Chip labels are vertically centered in their pills (inline anchors with
  // vertical padding rendered the text riding high).
  const offCenters = await page.locator('form[data-filterbar] a[hx-get]').evaluateAll((els) =>
    els.map((el) => {
      const range = document.createRange();
      range.selectNodeContents(el);
      const tr = range.getBoundingClientRect();
      const cr = el.getBoundingClientRect();
      return Math.abs((tr.top + tr.bottom) / 2 - (cr.top + cr.bottom) / 2);
    }),
  );
  expect(offCenters.length, 'chips present').toBeGreaterThan(0);
  for (const [i, off] of offCenters.entries()) {
    expect(off, `chip ${i} label centered`).toBeLessThan(2);
  }
});

for (const width of [288, 320, 360, 390, 412, 430]) {
  test(`vehicles filter controls never clip their text at ${width}px`, async ({ page }) => {
    await registerFreshUser(page, `pw-vehclip${width}`);
    await seedVehicle(page, uniqueReg(`CLIP${width}`));
    await page.setViewportSize({ width, height: 800 });
    await page.goto('/vehicles');
    await expect(page.locator('table.rtable tbody tr')).toHaveCount(1, { timeout: 15000 });

    // Real-device report: "All ownership" rendered "All ownersh", "All classes"
    // ran under the native arrow, date placeholders clipped to "dd-mm-y".
    // scrollWidth is useless here (empty inputs and selects never scroll), so
    // measure the longest rendered string with canvas and demand room for it.
    const clipped = await page.evaluate(() => {
      const ctx = document.createElement('canvas').getContext('2d')!;
      const out: string[] = [];
      const fits = (el: HTMLElement | null, text: string, extra: number, label: string) => {
        if (!el) { out.push(`${label} missing`); return; }
        const cs = getComputedStyle(el);
        ctx.font = `${cs.fontWeight} ${cs.fontSize} ${cs.fontFamily}`;
        const need = ctx.measureText(text).width + extra;
        if (el.clientWidth + 1 < need) {
          out.push(`${label}: "${text}" needs ${Math.round(need)}px, has ${el.clientWidth}px`);
        }
      };
      for (const id of ['fleet_class', 'ownership']) {
        const sel = document.querySelector(`select#${id}`) as HTMLSelectElement | null;
        // A closed select shows the SELECTED option, not the longest one
        // (dropdown options render in an overlay) — measure what is visible.
        const shown = sel?.options[sel?.selectedIndex]?.text ?? '';
        // px-3 padding (24) + native arrow (~24).
        fits(sel, shown, 48, `select#${id}`);
      }
      for (const key of ['from', 'to'] as const) {
        const inp = document.querySelector(`[data-daterange] input[data-${key}]`) as HTMLInputElement | null;
        fits(inp, inp?.placeholder ?? '', 8, `date ${key}`);
      }
      return out;
    });
    expect(clipped, `no clipped filter controls at ${width}px`).toEqual([]);
  });
}

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
  // Filtered-zero empty state stays centered: card-mode `td{display:flex;
  // justify-content:space-between}` + `td>*{text-align:right}` used to pin
  // the block left (real-device report, "No vehicles found" off-center).
  const centering = await page.locator('td[colspan]').evaluate((td) => {
    const cs = getComputedStyle(td);
    const child = td.firstElementChild as HTMLElement | null;
    const tr = td.getBoundingClientRect();
    const cr = child ? child.getBoundingClientRect() : tr;
    return {
      display: cs.display,
      childAlign: child ? getComputedStyle(child).textAlign : '',
      offset: Math.abs((cr.left + cr.right) / 2 - (tr.left + tr.right) / 2),
    };
  });
  if (page.viewportSize()!.width <= 768) {
    expect(centering.display, 'empty cell is block on mobile').toBe('block');
  }
  expect(centering.childAlign, 'empty content centered').toBe('center');
  expect(centering.offset, 'empty block horizontally centered').toBeLessThan(8);
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

  // Create forces available — flip one row via the real status endpoint.
  await page.goto('/vehicles');
  await expect(page.locator('table.rtable tbody tr')).toHaveCount(2, { timeout: 15000 });
  const base = new URL(page.url()).origin;
  const runHref = await page.locator('table.rtable tbody tr a:has-text("View")').first().getAttribute('href');
  const st = await page.request.post(`${runHref}/status`, {
    headers: { Origin: base, Referer: `${base}${runHref}` },
    form: { status: 'running' },
  });
  expect(st.ok(), 'status flip').toBeTruthy();
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

async function setStatus(page: Page, viewHref: string, status: string): Promise<void> {
  const base = new URL(page.url()).origin;
  const resp = await page.request.post(`${viewHref}/status`, {
    headers: { Origin: base, Referer: `${base}${viewHref}` },
    form: { status },
  });
  expect(resp.ok(), `status ${status}`).toBeTruthy();
}

test('vehicles filters behave as one state', async ({ page }) => {
  await registerFreshUser(page, 'pw-vehone');
  const reg = uniqueReg('ONE');
  await seedVehicle(page, reg);
  await page.goto('/vehicles');
  await expect(page.locator('table.rtable tbody tr')).toHaveCount(1, { timeout: 15000 });

  // Running chip on top of class+ownership preserves all three.
  await page.goto('/vehicles?fleet_class=CV&ownership=O');
  await page.getByRole('link', { name: 'Running' }).first().click();
  await expect(page.locator('td[colspan]')).toContainText('No vehicles match', { timeout: 15000 });
  expect(page.url()).toContain('status=running');
  expect(page.url()).toContain('fleet_class=CV');
  expect(page.url()).toContain('ownership=O');

  // Search preserves status + class + ownership + date.
  const today = new Date().toISOString().slice(0, 10);
  await page.goto(`/vehicles?status=available&fleet_class=CV&ownership=O&from=${today}&to=${today}`);
  await expect(page.locator('table.rtable tbody tr')).toHaveCount(1, { timeout: 15000 });
  await page.locator('form[data-filterbar] input[name="q"]').pressSequentially(reg.slice(-4), { delay: 20 });
  await expect(page.locator('table.rtable tbody tr')).toHaveCount(1, { timeout: 15000 });
  expect(page.url()).toContain('status=available');
  expect(page.url()).toContain('fleet_class=CV');
  expect(page.url()).toContain('ownership=O');
  expect(page.url()).toContain(`from=${today}`);

  // Class Clear drops only class+ownership, keeps the rest.
  await page.goto(`/vehicles?q=${reg.slice(-4)}&status=available&fleet_class=CV&ownership=O`);
  await expect(page.locator('table.rtable tbody tr')).toHaveCount(1, { timeout: 15000 });
  const clearHref = await page.getByRole('link', { name: 'Clear' }).first().getAttribute('href');
  expect(clearHref, 'clear drops class+ownership only').not.toContain('fleet_class');
  expect(clearHref, 'clear drops class+ownership only').not.toContain('ownership');
  expect(clearHref, 'clear keeps search').toContain(`q=${reg.slice(-4)}`);
  expect(clearHref, 'clear keeps status').toContain('status=available');
});

test('vehicles status chips match backend semantics; blocked stays unfiltered', async ({ page }) => {
  // Authoritative mapping lives in vehicle_list.html chips:
  // Active→available, Running→running, Maintenance→maintenance,
  // Out of Service→inactive. blocked has no chip anywhere in the repo.
  await registerFreshUser(page, 'pw-vehsts');
  const availReg = uniqueReg('AVL');
  const inaReg = uniqueReg('INA');
  const blkReg = uniqueReg('BLK');
  await seedVehicle(page, availReg);
  await seedVehicle(page, inaReg);
  await seedVehicle(page, blkReg);
  await page.goto('/vehicles');
  await expect(page.locator('table.rtable tbody tr')).toHaveCount(3, { timeout: 15000 });
  const hrefFor = async (reg: string) => {
    const row = page.locator('table.rtable tbody tr', { hasText: reg }).first();
    return (await row.locator('a:has-text("View")').first().getAttribute('href'))!;
  };
  await setStatus(page, await hrefFor(inaReg), 'inactive');
  await setStatus(page, await hrefFor(blkReg), 'blocked');
  await page.goto('/vehicles');
  await expect(page.locator('table.rtable tbody tr')).toHaveCount(3, { timeout: 15000 });

  // Out of Service = exactly inactive (blocked excluded by exact-match SQL).
  await page.goto('/vehicles?status=inactive');
  await expect(page.locator('table.rtable tbody tr')).toHaveCount(1, { timeout: 15000 });
  await expect(page.locator('table.rtable tbody')).toContainText(inaReg);

  // Active = exactly available.
  await page.goto('/vehicles?status=available');
  await expect(page.locator('table.rtable tbody tr')).toHaveCount(1, { timeout: 15000 });
  await expect(page.locator('table.rtable tbody')).toContainText(availReg);

  // Blocked surfaces only under All Units (and text search).
  await page.goto('/vehicles');
  await expect(page.locator('table.rtable tbody')).toContainText(blkReg);
});

test('vehicles widths 360-430 stay usable with long data', async ({ page }) => {
  if (page.viewportSize()!.width > 500) test.skip();
  await registerFreshUser(page, 'pw-vehwide');
  // 20-char plate, long fleet number + manufacturer: page.request skips the
  // HTML pattern gate, the server stores them verbatim.
  const longReg = `MH${'9'.repeat(14)}LONGA`.slice(0, 20);
  await seedVehicle(page, longReg);
  const base = new URL(page.url()).origin;
  const longNumResp = await page.request.post('/vehicles/new', {
    headers: { Origin: base, Referer: `${base}/vehicles/new` },
    form: {
      registration_number: uniqueReg('MFR'),
      vehicle_number: 'FLEETNUMBER-VERYLONG-01',
      vehicle_type: 'truck',
      capacity: '99999999',
      fuel_type: 'diesel',
      insurance_expiry: futureDate(),
      fitness_expiry: futureDate(),
      permit_expiry: futureDate(),
      manufacturer: 'SUPERLONGMANUFACTURERNAME-INDIA-PRIVATE-LIMITED',
    },
  });
  expect(longNumResp.ok(), 'long-field seed').toBeTruthy();

  for (const w of [360, 390, 412, 430]) {
    await page.setViewportSize({ width: w, height: 800 });
    await page.goto('/vehicles');
    await expect(page.locator('table.rtable tbody tr')).toHaveCount(2, { timeout: 15000 });

    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth);
    expect(overflow, `no page overflow at ${w}px`).toBeLessThanOrEqual(1);

    for (const [sel, label] of [
      ['form[data-filterbar]', 'filter bar'],
      ['form[data-filterbar] input[name="q"]', 'search'],
      ['[data-daterange]', 'date pill'],
      ['select#fleet_class', 'fleet class'],
      ['select#ownership', 'ownership'],
      ['a[href="/vehicles/new"]', 'new vehicle'],
      ['table.rtable tbody tr', 'first card'],
    ] as const) {
      const box = await page.locator(sel).first().boundingBox();
      expect(box, `${label} at ${w}px`).not.toBeNull();
      expect(box!.x + box!.width, `${label} fits at ${w}px`).toBeLessThanOrEqual(w + 1);
    }

    // Calendar popover fits the narrow viewport too.
    await page.locator('[data-calbtn]').first().click();
    const pop = page.locator('#av-cal-pop');
    await expect(pop).toBeVisible({ timeout: 10000 });
    const pbox = await pop.boundingBox();
    expect(pbox!.x + pbox!.width, `calendar fits at ${w}px`).toBeLessThanOrEqual(w + 1);
    await page.keyboard.press('Escape');
  }
});

test('vehicles filter hierarchy: one CTA, selects inside Filters, compact zero-state', async ({ page }) => {  await registerFreshUser(page, 'pw-vehhier');
  await page.goto('/vehicles');

  // Information hierarchy, top to bottom: header CTA > chips > date > search >
  // fleet class > ownership > results. centers() order, not just fit.
  const tops: number[] = [];
  for (const sel of [
    'a[href="/vehicles/new"]',
    'form[data-filterbar] a[href*="status=running"]',
    '[data-daterange]',
    'form[data-filterbar] input[name="q"]',
    'select#fleet_class',
    'select#ownership',
    '#list-table',
  ]) {
    const box = await page.locator(sel).first().boundingBox();
    expect(box, `${sel} rendered`).not.toBeNull();
    tops.push(box!.y + box!.height / 2);
  }
  for (let i = 1; i < tops.length; i++) {
    // Step 3 (search vs date) and step 5 (ownership vs fleet class): same-row
    // side-by-side is correct responsive behavior on wider viewports.
    if (i === 3 || i === 5) expect(tops[i], `order step ${i}`).toBeGreaterThanOrEqual(tops[i - 1] - 1);
    else expect(tops[i], `order step ${i}`).toBeGreaterThan(tops[i - 1]);
  }

  // Exactly one New Vehicle CTA on the page (header); none in filter bar or results.
  expect(await page.locator('a[href="/vehicles/new"]').count(), 'single CTA').toBe(1);
  expect(await page.locator('form[data-filterbar] a[href="/vehicles/new"]').count(), 'no CTA in filters').toBe(0);

  // Search placeholder is a real string, never a raw i18n key.
  const ph = await page.locator('form[data-filterbar] input[name="q"]').first().getAttribute('placeholder');
  expect(ph, 'placeholder text').not.toContain('common.search');
  expect(ph!.length, 'placeholder non-empty').toBeGreaterThan(8);

  // Filtered-zero state: correct copy, no creation CTA, compact height.
  await page.locator('form[data-filterbar] input[name="q"]').first().pressSequentially('ZZZ-NO-MATCH-999');
  await expect(page.locator('#list-table')).toContainText('No vehicles found', { timeout: 15000 });
  await expect(page.locator('#list-table')).toContainText('No vehicles match the selected criteria.');
  expect(await page.locator('#list-table a[href="/vehicles/new"]').count(), 'no CTA in zero-state').toBe(0);
  const emptyBox = await page.locator('#list-table td[colspan]').first().boundingBox();
  expect(emptyBox!.height, 'compact zero-state').toBeLessThan(300);
});

test('vehicles chip highlight follows the active filter over htmx', async ({ page }) => {
  await registerFreshUser(page, 'pw-vehchipsync');
  await seedVehicle(page, uniqueReg('S'));
  await page.goto('/vehicles');
  await expect(page.locator('table.rtable tbody tr')).toHaveCount(1, { timeout: 15000 });

  // Chips live outside the #list-table swap target: without the sync script
  // the highlight would freeze on the previously selected chip.
  const chipOn = async (label: string) =>
    await page.locator('form[data-filterbar] a[hx-get]', { hasText: label }).first().evaluate((el) =>
      el.className.includes('bg-primary'),
    );

  await expect.poll(() => chipOn('All Units'), { timeout: 10000 }).toBe(true);
  await page.locator('form[data-filterbar] a[hx-get]', { hasText: 'Out of Service' }).first().click();
  await expect(page.locator('#list-table')).toContainText('No vehicles found', { timeout: 15000 });
  await expect.poll(() => chipOn('Out of Service'), { timeout: 10000 }).toBe(true);
  expect(await chipOn('All Units')).toBe(false);

  await page.locator('form[data-filterbar] a[hx-get]', { hasText: 'All Units' }).first().click();
  await expect(page.locator('table.rtable tbody tr')).toHaveCount(1, { timeout: 15000 });
  await expect.poll(() => chipOn('All Units'), { timeout: 10000 }).toBe(true);
  expect(await chipOn('Out of Service')).toBe(false);
});

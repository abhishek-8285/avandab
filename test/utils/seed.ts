import { expect, type Page } from '@playwright/test';

/**
 * Seed one customer + one route + one booking through the real web forms.
 *
 * Why through the UI rather than the REST API: there is no REST endpoint for
 * customers or routes (only bookings has `POST /api/v1/bookings`, and it needs
 * both a customer_id and a route_id that must already exist). The forms are
 * therefore the only path, and they are also the path a real operator uses.
 *
 * The point is to make the responsive specs assert against a POPULATED table.
 * An empty state renders one full-width panel and can hide every column-width
 * and tap-target defect a real row exposes.
 */

/**
 * Submit and wait for the redirect. Server-side validation failures re-render
 * the same form with a FlashError instead of redirecting, so surface that
 * text — otherwise the only symptom is a URL timeout with no reason.
 */
async function submitForm(page: Page, actionPrefix: string, redirectTo: RegExp | string): Promise<void> {
  const form = page.locator(`form[action^="${actionPrefix}"]`);
  // Scope the click to the entity form — the app layout also renders submit
  // buttons (logout, language switcher) that would otherwise match first.
  await form.locator('button[type="submit"]').first().click();

  const navigated = await page
    .waitForURL(redirectTo, { waitUntil: 'domcontentloaded', timeout: 15000 })
    .then(() => true)
    .catch(() => false);

  if (!navigated) {
    const flash = (await page.locator('[class*="bg-status-alert"]').allTextContents())
      .map((t) => t.trim())
      .filter(Boolean);
    throw new Error(
      `Form ${actionPrefix} did not redirect (stayed at ${page.url()}). ` +
        `Server said: ${flash.join(' | ') || '(no flash error rendered)'}`,
    );
  }
}

/**
 * Phone is unique across the whole customers table, not per tenant, and the
 * Playwright DB is shared across runs — a fixed number collides the second
 * time the suite runs. 10 digits starting with 9 keeps it a plausible Indian
 * mobile number for any format validation downstream.
 */
function uniquePhone(): string {
  return '9' + String(Math.floor(Math.random() * 1_000_000_000)).padStart(9, '0');
}

export async function seedCustomer(page: Page, tag: string): Promise<void> {
  await page.goto('/customers/new');
  await page.fill('input[name="name"]', `${tag} Customer`);
  await page.fill('input[name="phone"]', uniquePhone());
  await page.fill('input[name="email"]', `${tag}@test.local`);
  await submitForm(page, '/customers', '**/customers*');
}

export async function seedRoute(page: Page, tag: string): Promise<void> {
  await page.goto('/routes/new');
  await page.fill('input[name="source"]', `${tag} Nagar`);
  await page.fill('input[name="destination"]', `${tag} Port`);
  await page.fill('input[name="distance"]', '420');
  // estimated_hours is validated server-side as > 0 even though the form
  // marks it optional, so leave it out and the create is rejected.
  await page.fill('input[name="estimated_hours"]', '9');
  await page.fill('input[name="standard_fare"]', '18500');
  await submitForm(page, '/routes', '**/routes*');
}

/** Pick a date 7 days out in yyyy-mm-dd (the format a date input expects). */
function pickupDate(): string {
  const d = new Date(Date.now() + 7 * 24 * 60 * 60 * 1000);
  return d.toISOString().slice(0, 10);
}

export async function seedBooking(page: Page): Promise<string> {
  await page.goto('/bookings/new');

  // The selects are server-populated from the tenant's own rows, so by the
  // time we get here the seeds above must have landed. Assert rather than
  // hope: a silently empty select would otherwise submit an empty customer_id
  // and the list page would stay empty — the exact false-green we are avoiding.
  const customerSelect = page.locator('select[name="customer_id"]');
  await expect(customerSelect.locator('option')).not.toHaveCount(1, { timeout: 10000 });
  const routeSelect = page.locator('select[name="route_id"]');
  await expect(routeSelect.locator('option')).not.toHaveCount(1, { timeout: 10000 });

  await customerSelect.selectOption({ index: 1 });
  await routeSelect.selectOption({ index: 1 });
  await page.fill('input[name="pickup_date"]', pickupDate());
  await page.fill('input[name="price"]', '18500');

  // A successful create redirects to the LIST, not to the detail page.
  await submitForm(page, '/bookings', '**/bookings*');

  // Recover the new booking's id from its row action link, so specs can open
  // the view/edit pages. Failing loudly here matters: a seed that silently
  // created nothing would leave every downstream assertion measuring an empty
  // page and passing for the wrong reason.
  const viewLink = page.locator('table tbody tr a[href^="/bookings/"]').first();
  await expect(viewLink, 'booking row action link after seed').toBeVisible({ timeout: 10000 });
  const href = await viewLink.getAttribute('href');
  if (!href) throw new Error('seedBooking: row action link has no href');
  return href.slice(href.lastIndexOf('/') + 1);
}

/** Create customer + route + booking. Safe to call once per serial run. */
export async function seedOneBooking(page: Page, tag = 'seed'): Promise<{ bookingId: string }> {
  const uid = `${tag}${Date.now()}`;
  await seedCustomer(page, uid);
  await seedRoute(page, uid);
  const bookingId = await seedBooking(page);
  return { bookingId };
}

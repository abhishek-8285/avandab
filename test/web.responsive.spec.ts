import { test, expect } from '@playwright/test';
import { registerFreshUser } from './utils/register';
import { seedOneBooking } from './utils/seed';
import { expectNoHorizontalOverflow, expectTapTargets, expectTableReflowsToCards } from './utils/responsive';

/**
 * Responsive regression sweep for the highest-traffic web flows.
 *
 * Runs on every project (desktop 1440 / tablet 820 / mobile 390) and asserts
 * the two things "responsive" actually has to mean here:
 *   1. the page never scrolls sideways
 *   2. every visible control is big enough to hit with a thumb
 *
 * Serial: every registration writes to one shared SQLite file and parallel
 * writers deadlock it.
 *
 * Pages are fully server-rendered Go templates, so there is no client-side
 * render to wait for — once goto() resolves the markup is final and measuring
 * is safe.
 */
test.describe.configure({ mode: 'serial' });

test('login page', async ({ page }) => {
  await page.goto('/login');
  await expect(page.locator('form')).toBeVisible();
  await expectNoHorizontalOverflow(page, '/login');
  await expectTapTargets(page, '/login');
});

test('dashboard', async ({ page }) => {
  await registerFreshUser(page, 'pw-dash');
  await page.goto('/dashboard');
  await expect(page.locator('#main-content')).toBeVisible();
  await expectNoHorizontalOverflow(page, '/dashboard');
  await expectTapTargets(page, '/dashboard');
});

test('booking list', async ({ page }) => {
  await registerFreshUser(page, 'pw-bl');
  await page.goto('/bookings');
  await expect(page.locator('main, #main-content').first()).toBeVisible();
  await expectNoHorizontalOverflow(page, '/bookings');
  await expectTapTargets(page, '/bookings');
});

test('booking list with rows', async ({ page }) => {
  await registerFreshUser(page, 'pw-bs');
  const { bookingId } = await seedOneBooking(page, 'pw-bs');
  expect(bookingId, 'seed produced a booking id').not.toBe('');

  await page.goto('/bookings');
  await expect(page.locator('table tbody tr')).not.toHaveCount(0);
  await expectNoHorizontalOverflow(page, '/bookings (populated)');
  await expectTapTargets(page, '/bookings (populated)');
  await expectTableReflowsToCards(page, '/bookings (populated)');
});

test('booking edit form', async ({ page }) => {
  await registerFreshUser(page, 'pw-be');
  const { bookingId } = await seedOneBooking(page, 'pw-be');

  await page.goto(`/bookings/${bookingId}/edit`);
  await expect(page.locator('select[name="customer_id"]')).toBeVisible();
  await expectNoHorizontalOverflow(page, '/bookings/:id/edit');
  await expectTapTargets(page, '/bookings/:id/edit');
});

test('invoice list', async ({ page }) => {
  await registerFreshUser(page, 'pw-inv');
  await page.goto('/invoices');
  await expect(page.locator('main, #main-content').first()).toBeVisible();
  await expectNoHorizontalOverflow(page, '/invoices');
  await expectTapTargets(page, '/invoices');
});

test('customer list', async ({ page }) => {
  await registerFreshUser(page, 'pw-cust');
  await page.goto('/customers');
  await expect(page.locator('main, #main-content').first()).toBeVisible();
  await expectNoHorizontalOverflow(page, '/customers');
  await expectTapTargets(page, '/customers');
});

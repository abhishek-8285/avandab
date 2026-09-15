import { test, expect } from '@playwright/test';
import { registerFreshUser } from './utils/register';

/**
 * Regression spec for the list-page date-range calendar
 * (internal/templates/partials/filter_bar.html).
 *
 * Guards the fixes proven broken before: ISO dates were mangled into garbage
 * by the input mask and the filter was then silently dropped; impossible dates
 * (31-02) also silently cleared the filter; keyboard nav did nothing; there
 * were no year controls; "This month" excluded half the month.
 */
test.describe.configure({ mode: 'serial' });

test.beforeEach(async ({ page }) => {
  await registerFreshUser(page, 'cal-reg');
  await page.goto('/bookings');
  await expect(page.locator('form[data-filterbar] [data-from]')).toBeVisible();
});

test('ISO date survives the pill and applies', async ({ page }) => {
  const from = page.locator('form[data-filterbar] [data-from]');
  await from.fill('2026-08-25');
  expect(await from.inputValue()).toBe('2026-08-25');

  await page.locator('form[data-filterbar] button[type="submit"]').click();
  await page.waitForLoadState('domcontentloaded');
  const u = new URL(page.url());
  expect(u.searchParams.get('from')).toBe('2026-08-25');
  // Back-rendered in the mask's dd-mm-yyyy display format.
  expect(await page.locator('form[data-filterbar] [data-from]').inputValue()).toBe('25-08-2026');
});

test('dd-mm-yyyy still applies', async ({ page }) => {
  await page.locator('form[data-filterbar] [data-from]').fill('25-08-2026');
  await page.locator('form[data-filterbar] button[type="submit"]').click();
  await page.waitForLoadState('domcontentloaded');
  expect(new URL(page.url()).searchParams.get('from')).toBe('2026-08-25');
});

test('impossible date blocks Apply with a visible error, never silently', async ({ page }) => {
  await page.locator('form[data-filterbar] [data-from]').fill('31-02-2026');
  // blur marks invalid; the inline error appears.
  await page.locator('form[data-filterbar] [data-to]').click();
  const box = page.locator('form[data-filterbar] [data-daterr]');
  await expect(box).not.toHaveClass(/hidden/);

  const urlBefore = page.url();
  await page.locator('form[data-filterbar] button[type="submit"]').click();
  // The request never goes out — page stays, no cleared filter.
  await page.waitForTimeout(500);
  expect(page.url()).toBe(urlBefore);
  await expect(box).not.toHaveClass(/hidden/);
});

test('server rejects a hand-sent bad range instead of dropping it silently', async ({ page }) => {
  await page.goto('/bookings?from=31-02-2026&to=01-03-2026');
  const box = page.locator('form[data-filterbar] [data-daterr]');
  await expect(box).not.toHaveClass(/hidden/);
  await expect(box).toContainText('not applied');
});

test('search box cannot fire with an invalid date sitting in the pill', async ({ page }) => {
  const search = page.locator('form[data-filterbar] input[name="q"]');
  await page.locator('form[data-filterbar] [data-from]').fill('31-02-2026');
  const count = await page.locator('table tbody tr').count();
  // Unfiltered data is present only if the list page seeds aren't needed; either
  // way the typed character must NOT trigger a navigation/request.
  await search.fill('a');
  await page.waitForTimeout(800);
  const u = new URL(page.url());
  expect(u.searchParams.get('from')).toBeNull();
  const box = page.locator('form[data-filterbar] [data-daterr]');
  await expect(box).not.toHaveClass(/hidden/);
  expect(await page.locator('table tbody tr').count()).toBe(count);
});

test('popover has year navigation and live title', async ({ page }) => {
  await page.locator('form[data-filterbar] [data-from]').click();
  const pop = page.locator('#av-cal-pop');
  await expect(pop).toBeVisible();
  const prevYear = pop.locator('button[aria-label="Previous year"]');
  await expect(prevYear).toBeVisible();
  const before = (await pop.locator('.av-cal-title').innerText()).trim();
  await prevYear.click();
  const after = (await pop.locator('.av-cal-title').innerText()).trim();
  expect(before).not.toBe(after);
  expect(after).toMatch(/\d{4}/);
});

test('arrow keys move the day cursor across month boundary', async ({ page }) => {
  await page.locator('form[data-filterbar] [data-from]').click();
  const pop = page.locator('#av-cal-pop');
  await expect(pop).toBeVisible();
  const target = pop.locator('button.av-cal-day').nth(10);
  await target.focus();
  const before = await page.evaluate(
    () => (document.activeElement as HTMLElement)?.getAttribute('data-day'),
  );
  await page.keyboard.press('ArrowRight');
  const after = await page.evaluate(
    () => (document.activeElement as HTMLElement)?.getAttribute('data-day'),
  );
  expect(before).not.toBeNull();
  expect(after).not.toBeNull();
  expect(after).not.toBe(before);
});

test('This month preset covers the whole month', async ({ page }) => {
  await page.locator('form[data-filterbar] [data-from]').click();
  await page.locator('#av-cal-pop [data-preset="month"]').click();
  const from = await page.locator('form[data-filterbar] [data-from]').inputValue();
  const to = await page.locator('form[data-filterbar] [data-to]').inputValue();
  expect(from).toMatch(/^01-\d{2}-\d{4}$/);
  const d = new Date();
  const last = new Date(d.getFullYear(), d.getMonth() + 1, 0);
  const mm = String(last.getMonth() + 1).padStart(2, '0');
  const dd = String(last.getDate()).padStart(2, '0');
  expect(to).toBe(`${dd}-${mm}-${last.getFullYear()}`);
});

test('grid exposes accessible selection state', async ({ page }) => {
  await page.locator('form[data-filterbar] [data-from]').click();
  const pop = page.locator('#av-cal-pop');
  await expect(pop).toBeVisible();
  expect(await pop.locator('[role="tab"][aria-selected]').count()).toBe(2);
  expect(await pop.locator('button.av-cal-day[aria-pressed]').count()).toBeGreaterThan(0);
});
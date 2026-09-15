import { expect, type Page } from '@playwright/test';

/**
 * Shared registration helper for the browser specs.
 *
 * Registers a brand-new tenant through the REAL UI (not the API), completes
 * onboarding, and lands on /dashboard with an authenticated browser session.
 *
 * Why not the API (`page.request.post('/register')`)? API-only registration
 * proved flaky under parallel workers — the session cookie sometimes never
 * reaches the page, so /tracking redirects to /login and the test fails on a
 * missing marker/fleet row. The full-UI flow always yields a real session and
 * lets us retry the transient SQLite lock contention ("database is locked" /
 * "deadlocked") that parallel registrations trigger.
 */
export async function registerFreshUser(page: Page, tag = 'pw'): Promise<void> {
  for (let attempt = 0; attempt < 3; attempt++) {
    const uid = `${Date.now()}${Math.floor(Math.random() * 100000)}`;
    const email = `${tag}${uid}@test.local`;

    await page.goto('/register');
    await page.fill('input[name="name"]', 'PW Test');
    await page.fill('input[name="email"]', email);
    await page.fill('input[name="phone"]', '9876500000');
    await page.fill('input[name="password"]', 'TestPass123!');
    await page.fill('input[name="confirm_password"]', 'TestPass123!');
    // DPDP explicit consent gate: /register requires agree=yes checkbox.
    await page.check('input[name="agree"]');
    await page.click('button[type="submit"]');

    try {
      await page.waitForURL(/\/dashboard|\/company\/onboard/, {
        waitUntil: 'domcontentloaded',
        timeout: 20000,
      });
    } catch (e) {
      const body = await page.content().catch(() => '');
      if (
        body.includes('database table is locked') ||
        body.includes('database is locked') ||
        body.includes('deadlocked')
      ) {
        await page.waitForTimeout(800 * (attempt + 1));
        continue; // transient SQLite contention — try a fresh user
      }
      throw e;
    }

    // Fresh org_admins have no company settings, so the compliance gate sends
    // them to onboarding first. Complete it to unlock /tracking.
    if (page.url().includes('/company/onboard')) {
      await page.fill('input[name="company_name"]', 'PW Fleet ' + Date.now());
      await page.fill('input[name="email"]', `ops${Date.now()}@test.com`);
      await page.fill('input[name="phone"]', '9876500000');
      // Server validates the address; scope the submit to the onboarding form
      // (the layout also renders logout submit buttons).
      await page.fill('textarea[name="address"]', 'Plot 42, Transport Nagar, Delhi 110042');
      await Promise.all([
        page.waitForURL('**/dashboard', { waitUntil: 'domcontentloaded' }),
        page.click('#onboarding-form button[type="submit"]'),
      ]);
    }

    await page.waitForURL('**/dashboard', { waitUntil: 'domcontentloaded' });
    return;
  }

  throw new Error('registerFreshUser failed after 3 attempts');
}

// Re-exported so specs that only need the status assertion keep using expect.
export { expect };

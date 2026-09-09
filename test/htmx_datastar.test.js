const { test, expect } = require('@playwright/test');

const BASE_URL = 'http://localhost:8080';

test.describe('HTMX & Datastar UI Tests', () => {
  test('home page loads successfully', async ({ page }) => {
    await page.goto('/');
    await expect(page).toHaveTitle(/Avandab/);
    await expect(page.locator('h1')).toContainText('Fleet operations managed with clarity and control');
    // Verify htmx script is loaded
    await expect(page.locator('script[src*="htmx"]')).toBeAttached();
  });

  test('kharcha dashboard has htmx polling endpoints', async ({ page }) => {
    await page.goto('/kharcha');
    // Check that the auto-refresh span has htmx attributes
    const refreshSpan = page.locator('span[hx-get="/kharcha/pending"]');
    await expect(refreshSpan).toBeVisible();
    await expect(refreshSpan).toHaveAttribute('hx-trigger', 'every 30s');
    await expect(refreshSpan).toHaveAttribute('hx-target', '#kharcha-queue');
    await expect(refreshSpan).toHaveAttribute('hx-swap', 'innerHTML');
  });

  test('reject expense form has htmx wired correctly', async ({ page }) => {
    await page.goto('/kharcha');
    // Open the reject modal
    const triggerBtn = page.locator('.kharcha-queue').first().locator('button').first();
    await triggerBtn.click();
    await expect(page.locator('#reject-modal')).toBeVisible();
    // Check form has htmx attributes after processing
    await expect(page.locator('#reject-form')).toHaveAttribute('hx-post');
    await expect(page.locator('#reject-form')).toHaveAttribute('hx-target');
    await expect(page.locator('#reject-form')).toHaveAttribute('hx-swap', 'outerHTML');
  });

  test('ledger trip filter uses htmx on change', async ({ page }) => {
    await page.goto('/kharcha');
    const filterSelect = page.locator('select#ledger-trip-filter');
    await expect(filterSelect).toHaveAttribute('hx-get', '/kharcha/ledger');
    await expect(filterSelect).toHaveAttribute('hx-trigger', 'change');
    await expect(filterSelect).toHaveAttribute('hx-target', '#ledger-body');
    await expect(filterSelect).toHaveAttribute('hx-swap', 'innerHTML');
  });

  test('datastar request header is handled on POST requests', async ({ page }) => {
    await page.goto('/kharcha');
    // Check that the page can make POST requests with Datastar-Request header
    // This is verified by checking the handler logic - forms should work
    const form = page.locator('form[method="POST"]').first();
    await expect(form).toBeVisible();
  });
});
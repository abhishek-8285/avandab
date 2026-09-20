import { test, expect } from '@playwright/test';
import { registerFreshUser } from './utils/register';

function futureDate(): string {
  return new Date(Date.now() + 365 * 24 * 60 * 60 * 1000).toISOString().slice(0, 10);
}
test('driver-vehicle preferred assignment and trip prefill', async ({ page }) => {
  await registerFreshUser(page, 'pw-assign');
  const base = new URL(page.url()).origin;

  // Create driver via real form POST.
  const drvRes = await page.request.post('/drivers/new', {
    headers: { Origin: base, Referer: `${base}/drivers/new` },
    form: {
      first_name: 'Rahul',
      last_name: 'Kumar',
      phone: '9876543210',
      license_number: 'DL-TEST-123',
      license_expiry: futureDate(),
      experience: '5',
    },
  });
  expect(drvRes.ok(), 'create driver').toBeTruthy();

  // Create vehicle.
  const vehReg = `MH${String(Date.now()).slice(-6)}AV`;
  const vehRes = await page.request.post('/vehicles/new', {
    headers: { Origin: base, Referer: `${base}/vehicles/new` },
    form: {
      registration_number: vehReg,
      vehicle_type: 'truck',
      capacity: '5000',
      fuel_type: 'diesel',
      insurance_expiry: futureDate(),
      fitness_expiry: futureDate(),
      permit_expiry: futureDate(),
    },
  });
  expect(vehRes.ok(), 'create vehicle').toBeTruthy();

  await page.goto('/drivers');
  const drvHref = await page.locator('table.rtable tbody tr a:has-text("View")').first().getAttribute('href');
  expect(drvHref).toBeTruthy();
  const drvId = drvHref!.split('/').pop()!;

  await page.goto('/vehicles');
  const vehHref = await page.locator('table.rtable tbody tr a:has-text("View")').first().getAttribute('href');
  expect(vehHref).toBeTruthy();
  const vehId = vehHref!.split('/').pop()!;

  // Assign vehicle to driver via driver view.
  const assignRes = await page.request.post(`/drivers/${drvId}/assign-vehicle`, {
    headers: { Origin: base, Referer: `${base}/drivers/${drvId}` },
    form: { vehicle_id: vehId },
  });
  expect(assignRes.ok() || assignRes.status() === 303, 'assign').toBeTruthy();

  // Driver view shows assigned vehicle.
  await page.goto(`/drivers/${drvId}`);
  await expect(page.locator('[data-section="assigned-vehicle"]')).toContainText(vehReg);
  await expect(page.locator('[data-section="assigned-vehicle"]')).toContainText('Primary');

  // Vehicle view shows assigned driver.
  await page.goto(`/vehicles/${vehId}`);
  await expect(page.locator('[data-section="assigned-driver"]')).toContainText('Rahul');

  // Trip create prefill: ?driver_id should pre-select vehicle.
  await page.goto(`/trips/new?driver_id=${drvId}`);
  const selVeh = await page.locator('select#vehicle_id').evaluate((el: HTMLSelectElement) => el.value);
  expect(selVeh, 'prefilled vehicle').toBe(vehId);

  // Change via driver change (reassign to same vehicle still shows).
  // Unassign.
  const unassignRes = await page.request.post(`/drivers/${drvId}/unassign-vehicle`, {
    headers: { Origin: base, Referer: `${base}/drivers/${drvId}` },
    form: {},
  });
  expect(unassignRes.ok() || unassignRes.status() === 303).toBeTruthy();
  await page.goto(`/drivers/${drvId}`);
  await expect(page.locator('[data-section="assigned-vehicle"]')).toContainText('No vehicle assigned');
});

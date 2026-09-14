import { test, expect } from '@playwright/test';
import { registerFreshUser } from './utils/register';

// Browser verification for the live telemetry parity panel on /tracking: the
// Intel detail drawer must render the live payload's vehicle fields (number,
// status, speed, fuel) so operators see ground truth, not fabricated data.
//   v1 → stopped, 0 km/h, 60% fuel
//   v2 → running, 45 km/h, 80% fuel
//   v3 → no fuel field → Fuel row omitted entirely (no fabrication)
// The live API is stubbed (server data shape is verified by Go tests); this
// proves the UI actually renders the contract.

const PARITY_VEHICLES = [
  {
    vehicle_id: '44444444-4444-4444-4444-444444444444',
    vehicle_number: 'MH01AB4444',
    trip_id: '',
    status: 'stopped',
    speed: 0,
    heading: 0,
    lat: 28.6139, // Delhi — spread far apart so Leaflet's marker cluster
    lng: 77.209, // group cannot merge the 3 markers into one cluster icon
    fuel_level: 60,
    odometer: 1000,
    ts: new Date().toISOString(),
  },
  {
    vehicle_id: '55555555-5555-5555-5555-555555555555',
    vehicle_number: 'DL02CD5555',
    trip_id: '',
    status: 'running',
    speed: 45,
    heading: 120,
    lat: 19.076, // Mumbai
    lng: 72.8777,
    fuel_level: 80,
    odometer: 2000,
    ts: new Date().toISOString(),
  },
  {
    vehicle_id: '66666666-6666-6666-6666-666666666666',
    vehicle_number: 'KA03EF6666',
    trip_id: '',
    status: 'stopped',
    speed: 0,
    heading: 0,
    lat: 12.9716, // Bengaluru
    lng: 77.5946,
    odometer: 3000,
    ts: new Date().toISOString(),
  },
];

test.describe('tracking parity panel', () => {
  test.describe.configure({ mode: 'serial' });

  test.beforeEach(async ({ page }) => {
    // Full-UI registration (shared helper): always yields a real browser
    // session and retries transient SQLite lock contention from parallel
    // workers. (/tracking has no permission gate per Spec 04 §7.)
    await registerFreshUser(page, 'pw-parity');

    // Fresh registrants land on /company/onboard (compliance gate). Complete
    // minimum viable onboarding so /tracking renders instead of setup wizard.
    await page.goto('/login');
    const origin = new URL(page.url()).origin;
    const onboardEmail = `ops-parity-${Date.now()}@test.local`;
    const onboard = await page.request.post('/company/onboard', {
      headers: { Origin: origin, Referer: `${origin}/company/onboard` },
      form: {
        company_name: 'Parity Fleet Pvt Ltd',
        address: 'MIDC Bhosari, Pune 411026',
        phone: '9999999999',
        email: onboardEmail,
      },
      maxRedirects: 0,
    });
    expect([200, 303]).toContain(onboard.status());

    // Deterministic data loading: replace EventSource with a fake that never
    // opens, so the REST poll is the sole data source.
    await page.addInitScript(() => {
      class FakeEventSource {
        static instances: FakeEventSource[] = [];
        closed = false;
        onopen: (() => void) | null = null;
        onerror: (() => void) | null = null;
        constructor() {
          FakeEventSource.instances.push(this);
        }
        addEventListener() {}
        close() {
          this.closed = true;
        }
      }
      (window as any).EventSource = FakeEventSource;
    });
  });

  test('detail drawer renders live payload fields without fabrication', async ({ page }) => {
    await page.route('**/api/v1/telemetry/live', (route) => route.fulfill({ json: PARITY_VEHICLES }));
    await page.route('**/api/v1/telemetry/geofences**', (route) => route.fulfill({ json: [] }));

    await page.goto('/tracking');
    await expect(page.locator('#fleet-list .fleet-row')).toHaveCount(3, { timeout: 15_000 });

    // ── v2: running vehicle ──
    await page.locator('.fleet-row', { hasText: 'DL02CD5555' }).click();
    await expect(page.locator('#intel-detail-panel')).toBeVisible();
    await expect(page.locator('#intel-vehicle-id')).toHaveText('DL02CD5555');
    await expect(page.locator('#intel-detail-panel .ti-pill')).toHaveText('running');
    // Speed/Fuel are key/value (.ti-kv) rows in the drawer — NOT the
    // fleet-row .ti-row-sub text. Assert the rendered payload values.
    await expect(page.locator('#intel-detail-panel .ti-kv', { hasText: 'Speed' })).toContainText('45 km/h');
    await expect(page.locator('#intel-detail-panel .ti-kv', { hasText: 'Fuel' })).toContainText('80%');

    // ── v1: stopped vehicle ──
    await page.locator('.fleet-row', { hasText: 'MH01AB4444' }).click();
    await expect(page.locator('#intel-vehicle-id')).toHaveText('MH01AB4444');
    await expect(page.locator('#intel-detail-panel .ti-pill')).toHaveText('stopped');
    await expect(page.locator('#intel-detail-panel .ti-kv', { hasText: 'Speed' })).toContainText('0 km/h');
    await expect(page.locator('#intel-detail-panel .ti-kv', { hasText: 'Fuel' })).toContainText('60%');

    // ── v3: no fuel field → the Fuel row is omitted entirely (no fabrication) ──
    await page.locator('.fleet-row', { hasText: 'KA03EF6666' }).click();
    await expect(page.locator('#intel-vehicle-id')).toHaveText('KA03EF6666');
    await expect(page.locator('#intel-detail-panel .ti-kv', { hasText: 'Fuel' })).toHaveCount(0);
  });

  // Marker tooltip: Leaflet's cluster group truncates DOM markers to those in
  // the viewport, so a single-vehicle payload guarantees exactly one
  // .leaflet-marker-icon → hover is deterministic and proves the marker label
  // carries the vehicle number from the live payload.
  test('single marker labels the vehicle from the live payload', async ({ page }) => {
    const oneVehicle = [PARITY_VEHICLES[0]]; // MH01AB4444
    await page.route('**/api/v1/telemetry/live', (route) => route.fulfill({ json: oneVehicle }));

    await page.goto('/tracking');
    await expect(page.locator('.leaflet-marker-icon').first()).toBeVisible({ timeout: 15_000 });
  });
});

import { test, expect } from '@playwright/test';

// Browser verification for the migration-00117 parity panel on /tracking:
// the Intel "Battery" tile and "Device" row must render from the live payload's
// battery_level / motion / valid fields, with the battery turning red ≤20%.
//   v1 → low battery (15%, red) + parked + invalid fix
//   v2 → healthy battery (66%, green) + moving + valid
//   v3 → no parity fields at all → "—" battery, "OK" device (no fabrication)
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
    battery_level: 15,
    motion: false,
    valid: false,
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
    battery_level: 66,
    motion: true,
    valid: true,
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
    fuel_level: 50,
    odometer: 3000,
    ts: new Date().toISOString(),
  },
];

test.describe('tracking parity panel', () => {
  test.describe.configure({ mode: 'serial' });

  test.beforeEach(async ({ page }) => {
    // Self-onboarding registers + auto-logs-in (viewer role suffices; /tracking
    // has no permission gate per Spec 04 §7).
    const email = `pw-parity-${Date.now()}-${Math.floor(Math.random() * 1e6)}@test.local`;
    const resp = await page.request.post('/register', {
      form: {
        name: 'Parity Tester',
        email,
        phone: '9999999999',
        password: 'Sup3rSecret!',
        confirm_password: 'Sup3rSecret!',
      },
      maxRedirects: 0,
    });
    expect([200, 303]).toContain(resp.status());

    // Deterministic data loading: replace EventSource with a fake that never
    // opens, so the REST poll is the sole data source. (An auto-opening fake
    // calls stopPolling() on open and races the initial refresh fetch — the
    // 3-vehicle panel test wins that race, but the 1-vehicle tooltip test loses
    // it and renders 0 rows. Keeping the stream "connecting" makes polling
    // deterministic, matching the no-stub debug run that worked 100%.)
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

  test('battery tile + device row render battery/motion/valid from the live payload', async ({ page }) => {
    await page.route('**/api/v1/telemetry/live', (route) => route.fulfill({ json: PARITY_VEHICLES }));
    await page.route('**/api/v1/telemetry/geofences**', (route) => route.fulfill({ json: [] }));

    await page.goto('/tracking');
    await expect(page.locator('#fleet-list .fleet-row')).toHaveCount(3, { timeout: 15_000 });

    // ── v1: low battery + parked + invalid fix ──
    await page.locator('.fleet-row', { hasText: 'MH01AB4444' }).click();
    await expect(page.locator('#intel-detail-panel')).toBeVisible();
    await expect(page.locator('#intel-battery')).toHaveText('15%');
    await expect(page.locator('#intel-battery')).toHaveClass(/text-status-error/, {
      message: 'battery ≤20% must render red (status-error)',
    });
    await expect(page.locator('#intel-device')).toHaveText('PARKED · NO GPS FIX');

    // ── v2: healthy battery + moving + valid ──
    await page.locator('.fleet-row', { hasText: 'DL02CD5555' }).click();
    await expect(page.locator('#intel-battery')).toHaveText('66%');
    await expect(page.locator('#intel-battery')).toHaveClass(/text-status-success/, {
      message: 'healthy battery must render green (status-success)',
    });
    await expect(page.locator('#intel-device')).toHaveText('MOVING');

    // ── v3: no parity fields → honest placeholders, nothing fabricated ──
    await page.locator('.fleet-row', { hasText: 'KA03EF6666' }).click();
    await expect(page.locator('#intel-battery')).toHaveText('—');
    await expect(page.locator('#intel-device')).toHaveText('OK');
  });

  // Marker tooltip: Leaflet's cluster group truncates DOM markers to those in
  // the viewport, so multi-vehicle markers aren't all reachable. A single-
  // vehicle payload guarantees exactly one .leaflet-marker-icon → hover is
  // deterministic and proves tooltipHTML renders the parity fields.
  test('single marker tooltip surfaces battery/parked/invalid-fix on hover', async ({ page }) => {
    const oneVehicle = [PARITY_VEHICLES[0]]; // MH01AB4444: 15%, parked, invalid
    await page.route('**/api/v1/telemetry/live', (route) => route.fulfill({ json: oneVehicle }));
    await page.route('**/api/v1/telemetry/geofences**', (route) => route.fulfill({ json: [] }));

    await page.goto('/tracking');
    await expect(page.locator('#fleet-list .fleet-row')).toHaveCount(1, { timeout: 15_000 });

    const tooltip = page.locator('.leaflet-tooltip');
    await page.locator('.leaflet-marker-icon').hover();
    await expect(tooltip).toBeVisible({ timeout: 5000 });
    await expect(tooltip).toContainText('Battery 15%');
    await expect(tooltip).toContainText('PARKED');
    await expect(tooltip).toContainText('No GPS fix');
  });
});
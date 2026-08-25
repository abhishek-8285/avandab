import { test, expect } from '@playwright/test';

// Browser-level verification for the tracking page rework:
//   1. Full-bleed layout (no double padding from layout <main>)
//   2. OSM attribution rendered AND not covered by overlay panels
//   3. Real OSM tile traffic
//   4. Telemetry ingestion renders registry rows, markers, counters
//   5. Healthy SSE pauses REST polling (no duplicate traffic)
//   6. Stream loss flips beacon to amber "Reconnecting…" and resumes polling
//   7. No fabricated data on the page

const VEHICLES = [
  {
    vehicle_id: '11111111-1111-1111-1111-111111111111',
    vehicle_number: 'MH01AB1111',
    trip_id: '',
    status: 'running',
    speed: 92,
    heading: 45,
    lat: 28.6139,
    lng: 77.209,
    fuel_level: 74,
    odometer: 1204.5,
    ts: new Date().toISOString(),
  },
  {
    vehicle_id: '22222222-2222-2222-2222-222222222222',
    vehicle_number: 'DL02CD2222',
    trip_id: 'TRIP-9001',
    status: 'stopped',
    speed: 0,
    heading: 0,
    lat: 19.076,
    lng: 72.8777,
    fuel_level: 41,
    odometer: 88221.2,
    ts: new Date().toISOString(),
  },
];

test.describe('tracking page', () => {
  test.beforeEach(async ({ page }) => {
    // Self-onboarding registers + auto-logs-in (viewer role suffices;
    // /tracking has no permission gate per Spec 04 §7).
    const email = `pw-track-${Date.now()}-${Math.floor(Math.random() * 1e6)}@test.local`;
    const resp = await page.request.post('/register', {
      form: {
        name: 'Playwright Tracker',
        email,
        phone: '9999999999',
        password: 'Sup3rSecret!',
        confirm_password: 'Sup3rSecret!',
      },
      maxRedirects: 0,
    });
    expect([200, 303]).toContain(resp.status());

    // Deterministic SSE: replace EventSource with a controllable stub so the
    // test decides exactly when the stream opens, emits, and dies. This
    // exercises the app's wiring (onopen/onerror/telemetry handlers,
    // start/stopPolling) without depending on transport timing.
    await page.addInitScript(() => {
      class FakeEventSource {
        static instances: FakeEventSource[] = [];
        url: string;
        closed = false;
        onopen: (() => void) | null = null;
        onerror: (() => void) | null = null;
        private telemetryCb: ((e: { data: string }) => void) | null = null;
        constructor(url: string) {
          this.url = url;
          FakeEventSource.instances.push(this);
          setTimeout(() => {
            if (!this.closed && this.onopen) this.onopen();
          }, 0);
        }
        addEventListener(type: string, cb: (e: { data: string }) => void) {
          if (type === 'telemetry') this.telemetryCb = cb;
        }
        close() {
          this.closed = true;
        }
        emit(data: unknown) {
          if (this.telemetryCb) this.telemetryCb({ data: JSON.stringify(data) });
        }
        fail() {
          if (!this.closed && this.onerror) this.onerror();
        }
      }
      (window as any).FakeEventSource = FakeEventSource;
      (window as any).EventSource = FakeEventSource;
    });
  });

  test('full-bleed layout, attribution visibility, tiles, telemetry, SSE lifecycle', async ({ page }) => {
    test.setTimeout(90_000);

    let liveCalls = 0;

    await page.route('**/api/v1/telemetry/live', async (route) => {
      liveCalls++;
      await route.fulfill({ json: VEHICLES });
    });

    await page.route('**/api/v1/telemetry/geofences**', (route) => route.fulfill({ json: [] }));

    let osmTileRequests = 0;
    await page.route(/tile\.openstreetmap\.org\//, async (route) => {
      osmTileRequests++;
      await route.continue();
    });

    await page.goto('/tracking');
    await expect(page).toHaveTitle(/Live Tracking|Avandab/i);

    // ── 1. Full-bleed: layout <main> must contribute zero padding here ──
    const pad = await page.evaluate(() => {
      const m = document.getElementById('main-content');
      return getComputedStyle(m).padding;
    });
    expect(pad, 'main padding must be 0 for the tracking route').toBe('0px');

    const mapBox = await page.locator('#live-map').boundingBox();
    expect(mapBox).not.toBeNull();
    // Full-bleed proof by composition: map + drawer + sidebar must tile the
    // viewport width exactly (no stray padding/margins between them).
    const chromeWidth = await page.evaluate(() => {
      const side = document.getElementById('sidebar');
      const drawer = document.getElementById('fleet-drawer');
      const w = (el: HTMLElement | null) => (el ? el.getBoundingClientRect().width : 0);
      return w(side as HTMLElement | null) + w(drawer);
    });
    const expectedMapW = 1280 - chromeWidth;
    expect(Math.abs(mapBox!.width - expectedMapW)).toBeLessThan(4);

    // ── 2. Attribution present, visible, and actually clickable-through ──
    const attribution = page.locator('.leaflet-control-attribution');
    await expect(attribution).toBeVisible();
    await expect(attribution).toContainText('OpenStreetMap');
    const uncovered = await page.evaluate(() => {
      const el = document.querySelector('.leaflet-control-attribution') as HTMLElement;
      if (!el) return false;
      const r = el.getBoundingClientRect();
      if (r.width === 0 || r.height === 0) return false;
      const top = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
      return !!top && el.contains(top);
    });
    expect(uncovered, 'attribution must not be covered by overlay chrome').toBe(true);

    // ── 3. Real tile traffic reaches OSM ──
    await page.waitForTimeout(1500);
    expect(osmTileRequests, 'expected OSM tile fetches').toBeGreaterThan(0);

    // ── 4. Poll snapshot ingested → registry, markers, counters ──
    await expect(page.locator('#fleet-list .fleet-row')).toHaveCount(2, { timeout: 15_000 });
    await expect(page.locator('#count-all')).toHaveText('2');
    // density-active counts running+stopped (everything with a live signal).
    await expect(page.locator('#density-active')).toHaveText('2');
    await expect(page.locator('#density-total')).toHaveText('2');
    await expect(page.locator('.leaflet-marker-icon').first()).toBeVisible();

    // Row content reflects payload (status label, speeding bolt).
    await expect(page.locator('.fleet-row', { hasText: 'MH01AB1111' })).toContainText('Moving');
    await expect(page.locator('.fleet-row', { hasText: 'MH01AB1111' })).toContainText('⚡');

    // Panel status tabs mirror the map chips (same state, second chip set).
    await expect(page.locator('#panel-count-all')).toHaveText('2');
    await expect(page.locator('#panel-count-running')).toHaveText('1');
    await expect(page.locator('#panel-count-stopped')).toHaveText('1');

    // Manual refresh + last-update clock chrome exist in the top strip.
    await expect(page.locator('#refresh-feed-btn')).toBeVisible();
    await expect(page.locator('#live-clock')).not.toHaveText('SYNCING...');

    // ── 4b. Docked detail sheet: open, overview stats, tabs, close ──
    await page.route('**/api/v1/trips/*/summary', (route) =>
      route.fulfill({
        json: {
          trip_id: 'TRIP-9001',
          trip_number: 'TRIP-9001',
          status: 'in_transit',
          origin: 'Delhi',
          destination: 'Gurgaon',
          route_km: 45,
        },
      }),
    );
    await page.route('**/api/v1/telemetry/history**', (route) => route.fulfill({ json: [] }));

    await page.locator('.fleet-row', { hasText: 'DL02CD2222' }).click();
    await expect(page.locator('#intel-detail-panel')).toBeVisible();
    await expect(page.locator('#intel-vehicle-id')).toHaveText('DL02CD2222');
    await expect(page.locator('#intel-speed')).toContainText('0');
    await expect(page.locator('#intel-fuel')).toContainText('41');

    // Trip tab renders the server summary — route names from the API only.
    await page.locator('[data-sheet-tab="trip"]').click();
    await expect(page.locator('#trip-summary-body')).toBeVisible();
    await expect(page.locator('#trip-sum-number')).toHaveText('TRIP-9001');
    await expect(page.locator('#trip-route-timeline')).toContainText('Delhi');
    await expect(page.locator('#trip-route-timeline')).toContainText('Gurgaon');

    // History tab: empty payload → honest empty state, no fabrication.
    await page.locator('[data-sheet-tab="history"]').click();
    await expect(page.locator('#history-empty')).toBeVisible();

    await page.locator('#close-intel-btn').click();
    await expect(page.locator('#intel-detail-panel')).toBeHidden();

    // ── 5. SSE opens → polling pauses ──
    await expect(page.locator('#conn-label')).toHaveText('Live Stream', { timeout: 10_000 });

    // Snapshot poll count once healthy; it must stay flat across more than a
    // full default poll interval (10s) while the stream is up.
    const before = liveCalls;
    await page.waitForTimeout(11_000);
    expect(liveCalls, 'polling must stop while SSE is healthy').toBe(before);

    // SSE patch ingests too: push a third vehicle through the stream only.
    const streamedIn = { ...VEHICLES[0], vehicle_id: '33333333-3333-3333-3333-333333333333', vehicle_number: 'KA03EF3333' };
    await page.evaluate((v) => {
      const es = (window as any).FakeEventSource.instances[0];
      es.emit(v);
    }, streamedIn);
    await expect(page.locator('#fleet-list .fleet-row')).toHaveCount(3, { timeout: 5_000 });
    await expect(page.locator('#count-all')).toHaveText('3');
    // Coalesced rerender must not have re-triggered polling either.
    expect(liveCalls).toBe(before);

    // ── 6. Stream dies → amber "Reconnecting…" + polling resumes ──
    await page.evaluate(() => {
      const es = (window as any).FakeEventSource.instances[0];
      es.fail();
    });
    await expect(page.locator('#conn-label')).toHaveText('Reconnecting…', { timeout: 10_000 });
    await expect(page.locator('#conn-beacon')).toHaveCSS('background-color', 'rgb(245, 158, 11)');
    await expect
      .poll(() => liveCalls, { timeout: 20_000, message: 'polling must resume after stream loss' })
      .toBeGreaterThan(before);

    // ── 7. Nothing fabricated ──
    await expect(page.getByText('Smart Allocation')).toHaveCount(0);
    await expect(page.getByText('Rajesh Kumar')).toHaveCount(0);
  });
});

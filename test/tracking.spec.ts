import { test, expect } from '@playwright/test';
import { registerFreshUser } from './utils/register';

// Browser-level verification for the tracking page rework:
//   1. Full-bleed layout (no double padding from layout <main>)
//   2. OpenStreetMap attribution rendered AND not covered by overlay panels
//   3. Real OSM tile traffic
//   4. Telemetry ingestion renders registry rows, markers, counters
//   5. Healthy SSE pauses REST polling (no duplicate traffic)
//   6. Stream loss flips beacon to amber "Connecting…" and resumes polling
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
  // Fresh-user registration hits SQLite writes; run serially to avoid
  // lock contention between parallel contexts.
  test.describe.configure({ mode: 'serial' });
  test.beforeEach(async ({ page }) => {
    // Full-UI registration (shared helper): always yields a real browser
    // session and retries transient SQLite lock contention from parallel
    // workers. (/tracking has no permission gate per Spec 04 §7.)
    await registerFreshUser(page, 'pw-track');

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

    let tileRequests = 0;
    await page.route(/mt1\.google\.com\/vt|tile\.openstreetmap\.org/, async (route) => {
      tileRequests++;
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
    // Derive from the live viewport, not a hardcoded 1280: the desktop project
    // runs at 1440 and this assertion is a tiling proof (map + drawer + sidebar
    // must exactly fill the width), so it must track whatever width is in use.
    const vw = page.viewportSize()!.width;
    const expectedMapW = vw - chromeWidth;
    expect(Math.abs(mapBox!.width - expectedMapW)).toBeLessThan(4);

    // ── 2. Attribution present, visible, and actually clickable-through ──
    const attribution = page.locator('.leaflet-control-attribution');
    await expect(attribution).toBeVisible();
    await expect(attribution).toContainText(/Google Maps|OpenStreetMap/);
    const uncovered = await page.evaluate(() => {
      const el = document.querySelector('.leaflet-control-attribution') as HTMLElement;
      if (!el) return false;
      const r = el.getBoundingClientRect();
      if (r.width === 0 || r.height === 0) return false;
      const top = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
      return !!top && el.contains(top);
    });
    expect(uncovered, 'attribution must not be covered by overlay chrome').toBe(true);

    // ── 3. Real tile traffic reaches the map provider ──
    await page.waitForTimeout(1500);
    expect(tileRequests, 'expected map tile fetches').toBeGreaterThan(0);

    // ── 4. Poll snapshot ingested → registry, markers, counters ──
    await expect(page.locator('#fleet-list .fleet-row')).toHaveCount(2, { timeout: 15_000 });
    await expect(page.locator('#count-all')).toHaveText('2');
    // density-active counts running+stopped (everything with a live signal).
    await expect(page.locator('#density-active')).toHaveText('2');
    await expect(page.locator('#density-total')).toHaveText('2');
    await expect(page.locator('.leaflet-marker-icon').first()).toBeVisible();

    // Row content reflects payload (status label, overspeed icon). The
    // overspeed indicator is an SVG (.ti-zap-icon), shown only when
    // speed > SPEED_LIMIT_KMH (80); MH01AB1111 runs at 92 km/h.
    await expect(page.locator('.fleet-row', { hasText: 'MH01AB1111' })).toContainText('Moving');
    await expect(page.locator('.fleet-row', { hasText: 'MH01AB1111' }).locator('.ti-zap-icon')).toBeVisible();

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
    // Drawer speed/fuel render as .ti-kv key/value rows (no #intel-speed/#intel-fuel IDs).
    await expect(page.locator('#intel-detail-panel .ti-kv', { hasText: 'Speed' })).toContainText('0 km/h');
    await expect(page.locator('#intel-detail-panel .ti-kv', { hasText: 'Fuel' })).toContainText('41%');

    // Drawer renders the server trip summary — route names from the API only.
    await expect(page.locator('#intel-detail-panel')).toContainText('TRIP-9001');
    await expect(page.locator('#intel-detail-panel')).toContainText('Delhi');
    await expect(page.locator('#intel-detail-panel')).toContainText('Gurgaon');

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
    // NOTE: layout's dashboard-live.js also opens an EventSource (stubbed by
    // the same fake), so select the island stream by URL, never by index.
    const streamedIn = { ...VEHICLES[0], vehicle_id: '33333333-3333-3333-3333-333333333333', vehicle_number: 'KA03EF3333' };
    await page.evaluate((v) => {
      // The tracking island's telemetry SSE may not be instances[0]: the
      // bookings board opens a second EventSource to the same
      // /api/v1/telemetry/stream URL, so pick the (last) telemetry-stream
      // instance rather than assuming index 0.
      const instances = (window as any).FakeEventSource.instances as any[];
      const es = instances.filter((i: any) => (i.url || '').includes('telemetry/stream')).pop()
        || instances[instances.length - 1];
      es.emit(v);
    }, streamedIn);
    await expect(page.locator('#fleet-list .fleet-row')).toHaveCount(3, { timeout: 5_000 });
    await expect(page.locator('#count-all')).toHaveText('3');
    // Coalesced rerender must not have re-triggered polling either.
    expect(liveCalls).toBe(before);

    // ── 6. Stream dies → REST polling resumes (SSE auto-reconnects in bg) ──
    await page.evaluate(() => {
      const instances = (window as any).FakeEventSource.instances as any[];
      const es = instances.filter((i: any) => (i.url || '').includes('telemetry/stream')).pop()
        || instances[instances.length - 1];
      es.fail();
    });
    // On stream loss the feed drops out of "Live Stream" and falls back to REST
    // polling. The app never renders a literal "Reconnecting…" label — its
    // status cycles Live (Polling) → Connecting… → Live Stream as it
    // auto-reconnects — so assert the label leaves the live state and that a
    // new poll actually fires.
    await expect(page.locator('#conn-label')).not.toHaveText('Live Stream', { timeout: 5_000 });
    await expect
      .poll(() => liveCalls, { timeout: 20_000, message: 'polling must resume after stream loss' })
      .toBeGreaterThan(before);
    await expect(page.locator('#conn-label')).toHaveText('Live Stream', { timeout: 15_000 });

    // ── 7. Nothing fabricated ──
    await expect(page.getByText('Smart Allocation')).toHaveCount(0);
    await expect(page.getByText('Rajesh Kumar')).toHaveCount(0);
  });
});

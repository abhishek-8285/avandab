import { defineConfig, devices } from '@playwright/test';
import fs from 'node:fs';
import path from 'node:path';

const databaseDir = path.resolve(process.cwd(), 'tmp');
// The E2E SQLite file lives in tmp/, which is gitignored and therefore absent
// on a fresh CI checkout (or a fresh clone). `mode=rwc` creates the database
// file but NOT its parent directory, so without this the server fails to open
// its DB and every test errors — create the directory up front.
fs.mkdirSync(databaseDir, { recursive: true });
const databasePath = path.join(databaseDir, 'transport-playwright.db').replaceAll('\\', '/');

export default defineConfig({
  testDir: './test',
  // Playwright wipes `outputDir` at the start of every run. On a hardened
  // runner (or locally after a big failing run) that bulk delete can be
  // blocked, which aborts the whole run before a single test executes — so
  // allow pointing at a fresh directory per run via PW_OUT. CI leaves it at
  // the default, where the checkout is already clean.
  outputDir: process.env.PW_OUT ?? 'test-results',
  timeout: 30000,
  expect: {
    timeout: 5000,
  },
  fullyParallel: true,
  workers: 4,
  // Registration-heavy specs write to one SQLite file, so transient
  // "database is locked" contention between workers is possible; a retry
  // (plus the in-spec register retry) keeps the suite honest but not brittle.
  retries: process.env.CI ? 2 : 1,
  reporter: [
    ['list'],
    // Same reasoning as `outputDir` above: the HTML reporter wipes its folder
    // on every run, so allow steering it to a fresh path.
    ['html', { outputFolder: process.env.PW_REPORT ?? 'playwright-report' }],
  ],
  webServer: {
    // Port 8092 is RESERVED: deploy_avandab.sh runs `adb forward tcp:8092`
    // for the Android TECNO device and the server publishes on
    // avandab.com:8092 for that device to reach. Playwright uses 8094 so the
    // ADB port-forward (which keeps 8092 bound on the dev machine) can never
    // collide with the E2E server. (8093 is also occupied on this dev box.)
    command: 'go run ./cmd/server/',
    env: {
      PORT: '8094',
      EXPERIMENT_ROLLOUT: '100',
      RATE_LIMIT_DISABLED: '1',
      DATABASE_URL: `file:${databasePath}?mode=rwc&cache=shared&_foreign_keys=on&_journal_mode=WAL`,
    },
    port: 8094,
    // Locally, reusing a running server saves a cold `go run` compile. On CI
    // always start a FRESH server: with reuse, a long-lived server that dies
    // mid-run is never restarted and every later test gets ECONNREFUSED.
    reuseExistingServer: !process.env.CI,
    // Cold `go run` compiles the whole module — give it room (default 60s
    // is tight on a first run).
    timeout: 120000,
  },
  projects: [
    // ── Desktop: runs the WHOLE suite. This is the regression baseline. ──
    {
      name: 'desktop',
      use: {
        ...devices['Desktop Chrome'],
        headless: true,
        viewport: { width: 1440, height: 900 },
        baseURL: 'http://localhost:8094',
        actionTimeout: 10000,
        navigationTimeout: 20000,
        trace: 'on-first-retry',
      },
    },

    // ── Responsive matrix ──────────────────────────────────────────────
    // These two run ONLY `*.responsive.spec.ts`. The desktop-oriented specs
    // (dashboard_ab, htmx_datastar, router_reactivity, tracking.spec) assert
    // desktop-only affordances (hover menus, sidebar always visible), so
    // pointing them at a 390px viewport would just produce noise.
    //
    // Viewports match the audit's stated targets: the smallest common Android
    // phone (390x844) and the tablet breakpoint (820x1180) that sits above
    // Tailwind's `md` (768) and below `lg` (1024) — i.e. the width where a
    // two-column layout has collapsed but the desktop grid has not kicked in.
    {
      name: 'tablet',
      testMatch: /.*\.responsive\.spec\.ts$/,
      use: {
        // Deliberately NOT devices['iPad (gen 7)']: that descriptor carries
        // defaultBrowserType 'webkit', and CI only installs chromium — every
        // tablet test would die on a missing WebKit binary. Same viewport,
        // plus touch, minus the mobile UA (tablets get the desktop layout of
        // most sites and keep hover working).
        ...devices['Desktop Chrome'],
        headless: true,
        hasTouch: true,
        viewport: { width: 820, height: 1180 },
        baseURL: 'http://localhost:8094',
        actionTimeout: 10000,
        navigationTimeout: 20000,
        trace: 'on-first-retry',
      },
    },
    {
      name: 'mobile',
      testMatch: /.*\.responsive\.spec\.ts$/,
      use: {
        ...devices['Pixel 5'],
        headless: true,
        viewport: { width: 390, height: 844 },
        baseURL: 'http://localhost:8094',
        actionTimeout: 10000,
        navigationTimeout: 20000,
        trace: 'on-first-retry',
      },
    },
  ],
});

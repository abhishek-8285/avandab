import { defineConfig, devices } from '@playwright/test';

// Serves the static expo web export (npx expo export --platform web) on :8099
// and runs the driver lifecycle spec against it with the Go backend fully
// mocked via page.route() — see driver-lifecycle.e2e.ts.
export default defineConfig({
  testDir: '.',
  testMatch: '*.e2e.ts',
  timeout: 60000,
  expect: {
    timeout: 10000,
  },
  fullyParallel: false,
  workers: 1,
  reporter: [['list']],
  webServer: {
    command: 'node static-server.cjs',
    url: 'http://localhost:8099',
    reuseExistingServer: !process.env.CI,
    timeout: 30000,
    cwd: __dirname,
    env: { E2E_ROOT: process.env.E2E_DIST_DIR || '../dist-web' },
  },
  use: {
    ...devices['Desktop Chrome'],
    headless: true,
    viewport: { width: 430, height: 932 },
    baseURL: 'http://localhost:8099',
    actionTimeout: 10000,
    navigationTimeout: 20000,
    trace: 'on-first-retry',
  },
});

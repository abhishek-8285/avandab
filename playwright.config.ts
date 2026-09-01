import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './test',
  timeout: 30000,
  expect: {
    timeout: 5000,
  },
  fullyParallel: true,
  workers: 4,
  reporter: [
    ['list'],
    ['html', { outputFolder: 'playwright-report' }],
  ],
  webServer: {
    // Port 8092 is RESERVED: deploy_avandab.sh runs `adb forward tcp:8092`
    // for the Android TECNO device and the server publishes on
    // avandab.com:8092 for that device to reach. Playwright uses 8093 so the
    // ADB port-forward (which keeps 8092 bound on the dev machine) can never
    // collide with the E2E server.
    command: 'PORT=8093 EXPERIMENT_ROLLOUT=100 DATABASE_URL="file:/tmp/transport-playwright.db?mode=rwc&cache=shared&_foreign_keys=on&_journal_mode=WAL" go run ./cmd/server/',
    port: 8093,
    reuseExistingServer: true,
    // Cold `go run` compiles the whole module — give it room (default 60s
    // is tight on a first run).
    timeout: 120000,
  },
  projects: [
    {
      name: 'Chromium',
      use: {
        ...devices['Desktop Chrome'],
        headless: true,
        viewport: { width: 1280, height: 720 },
        baseURL: 'http://localhost:8093',
        actionTimeout: 10000,
        navigationTimeout: 20000,
        trace: 'on-first-retry',
      },
    },
  ],
});

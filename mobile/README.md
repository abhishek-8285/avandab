# Avandab Driver App (mobile)

Expo SDK 52 / React Native 0.76 driver-facing app for the Avandab/MVTMS fleet
platform: trip dispatch, GPS telemetry, ePOD capture, offline-first expense
(kharcha) entry, document vault, and DPDP-style consent tracking.

## Architecture — autonomous loop

```
┌──────────────┐   MQTT over WebSocket (raw JWT as password)
│   Broker     │◄──────────────────────────────────────────────┐
│ :9001 ws /   │  avandab/trips/drivers/{id}/updates           │
│ :8883 wss    │  avandab/drivers/{id}/updates   (subscribe)   │
└──────────────┘                                               │
       ▲  avandab/telemetry/drivers/{id}/gps     (publish)     │
       │                                                       │
┌──────┴─────────┐   poller (tripPoller.ts, 30s default)  ┌────┴─────────┐
│ Go REST API    │◄──── GET /trips per driver ────────────│ syncEngine   │
│ /api/v1/**     │◄──── POST /telemetry/sync (batches) ───│ (syncEngine  │
└────────────────┘◄──── queued POD / expense flush ──────┤  .ts)        │
                                                         └──────┬───────┘
                                                                │ reads
                      ┌─────────────────────────────────────────▼───────┐
                      │ Local SQLite (expo-sqlite), two databases:      │
                      │ • avandab_offline.db: trips, offline_gps_logs,  │
                      │   consent_log                                   │
                      │ • offline_queue.db: queued_pods, queued_gps,    │
                      │   offline_expenses                              │
                      └─────────────────────────────────────────────────┘

GPS fixes: foreground via expo-location watchers; background task
AVANDAB_BACKGROUND_GPS (services/backgroundLocation.ts, deferred to SQLite,
flushed by SyncEngine). Poller interval defaults to 30s.
```

Offline behaviour: every POD, GPS batch, and expense is written to the queue
tables first; `syncEngine` drains them when connectivity returns
(`@react-native-community/netinfo`). MQTT failure falls back to HTTP telemetry.

## Setup

- Node.js **20** (CI pins `node-version: '20'`).
- `npm ci` inside `mobile/`.

### Environment variables

Copy `.env.example` → `.env.local` (git-ignored). All runtime knobs are
`EXPO_PUBLIC_*` with prod-ish defaults in `app.config.js`:

| Variable | Purpose | Default |
|---|---|---|
| `EXPO_PUBLIC_API_SCHEME` | REST scheme | `http` (dev) / `https` (prod) |
| `EXPO_PUBLIC_BACKEND_HOST` | API host | `127.0.0.1` / `api.avandab.com` |
| `EXPO_PUBLIC_API_PORT` | API port | `8080` / `443` |
| `EXPO_PUBLIC_MQTT_SCHEME` | Broker transport (`ws` dev, `wss` prod) | `ws` |
| `EXPO_PUBLIC_MQTT_BROKER_PORT` | Broker WS port | `9001` / `8883` |
| `EXPO_PUBLIC_POSTHOG_API_KEY` | Analytics key (console fallback when unset) | empty |
| `EXPO_PUBLIC_GSTN_API_KEY` | GSTN e-Way Bill API key for EWB generation (services/ewaybill.ts) | empty |
| `EXPO_PUBLIC_SENTRY_DSN` | Sentry crash reporting DSN (no-op when empty) | empty |
| `EXPO_PUBLIC_POSTHOG_KEY` | PostHog project key (console fallback when unset) | empty |

## Scripts

Run inside `mobile/`. CI merge gates are typecheck + lint + test-unit; see
[`.github/workflows/mobile.yml`](../.github/workflows/mobile.yml).

| Script | Command | Notes |
|---|---|---|
| start | `npm start` | expo dev server |
| build (native) | `npm run build` | `expo export --platform ios --platform android` |
| typecheck | `npm run typecheck` | `tsc --noEmit` (merge gate) |
| lint | `npm run lint` | eslint .ts/.tsx (merge gate) |
| test | `npm test` | jest unit tests (merge gate) |
| coverage | `npm run coverage` | jest + coverage report |
| mutation | `npm run mutation` | StrykerJS (quality signal; see stryker.config.json) |
| e2e | `npm run e2e` | Playwright against static web export (see below) |

## Local DB schema

`sqlite/migrations/001_init.sql`, `002_consent_log.sql`,
`003_documents_cache.sql` are the **canonical reference** of the tables the
app creates at runtime via `src/services/storage.ts`, `offlineQueue.ts`, and
`consentManager.ts`. Runtime services own creation/migration on device;
the SQL files exist so backend/engineers can audit the shape. NEVER hand-edit
an applied on-device database, and never edit an applied migration file.

- `001_init`: `queued_pods`, `queued_gps`, `offline_expenses` (offline_queue.db);
  `trips`, `offline_gps_logs` (avandab_offline.db)
- `002_consent_log`: `consent_log(purpose, user_response granted|denied, timestamp)`
- `003_documents_cache`: `documents_cache` (driver/vehicle doc metadata + expiry)

## DPDP compliance notes

- `consent_log` records each prompt purpose — `location`, `camera`,
  `microphone`, `data_processing` — with `granted`/`denied`.
- Retention: **3 years**, enforced by pruning older rows
  (`consentManager.ts`, `RETENTION_YEARS = 3`).
- `ConsentManager.exportConsentLog()` produces a portable export of the full
  consent trail.
- Data-rights request flows are covered by unit tests
  (`__tests__/dataRights.test.ts`, `__tests__/consentManager.test.ts`).

## OEM battery-optimization whitelist playbook

Background GPS dies when OEM task managers kill the app. Fleet managers must
walk every driver through whitelisting during onboarding (deployment
requirement):

1. **Xiaomi (MIUI)**: Settings → Apps → Manage apps → *Avandab Operations* →
   Battery saver → **No restrictions**; then Autostart → ON; then Recent apps
   → long-press card → lock (pin) the app.
2. **Huawei (EMUI)**: Settings → Battery → App launch → *Avandab Operations* →
   disable "Manage automatically" → enable all three (Auto-launch, Secondary
   launch, Run in background); Settings → Battery → More battery settings →
   turn off "Close apps after screen lock".
3. **Oppo (ColorOS)**: Settings → Battery → *Avandab Operations* → Allow
   background activity; App info → Battery usage → Allow background; recent-
   apps lock (pull down on card → Lock).
4. **Vivo (Funtouch OS)**: Settings → Battery → Background power consumption
   management → allow high background power for the app; App info → Battery →
   Background power consumption → enable.
5. **Realme (realme UI)**: same path as Oppo/ColorOS — Smart Control → app
   battery setting → "Allow background"; lock in recents.

Verify after setup: lock the device for 10 minutes on an active trip, then
confirm GPS points keep arriving (`offline_gps_logs` keeps growing while
offline).

## Known limitations

- **On-device STT**: voice expense input goes through a provider seam
  (`services/speech.ts`); no production speech provider is wired yet.
- **OCR provider seam**: receipt OCR is stubbed behind a service interface;
  provider integration pending.
- **Notifications adapter**: native push needs the notifications package
  installed for native builds (`POST_NOTIFICATIONS` permission is already
  declared in `app.config.js`); adapter is inert until then.
- **E2E web export currently BLOCKED**: `npx expo export --platform web`
  fails because `react-dom@18.3.1`, `react-native-web@~0.19.13`, and
  `@expo/metro-runtime@~4.0.1` are not installed (verbatim error):

  ```
  CommandError: It looks like you're trying to use web support but don't have the required
  dependencies installed.

  Please install react-dom@18.3.1, react-native-web@~0.19.13,
  @expo/metro-runtime@~4.0.1 by running:

  npx expo install react-dom react-native-web @expo/metro-runtime
  ```

  Required fix: install those three packages AND add a web-compatible map
  stub (Platform.OS guard around `react-native-maps` /
  `react-native-webview` imports in the map components — TODO for component
  owners) before web export can bundle. Until then `npm run e2e` exits green
  with **1 skipped** (graceful degradation in
  `e2e/driver-lifecycle.e2e.ts`; see `e2e/README.md`).

## Screens structure

Screen components live in `src/components/*Screen.tsx`. The `src/screens/`
directory is a pure structural barrel layer: one typed re-export file per
screen plus `index.ts` (zero logic, stable import surface):
`LoginScreen`, `RegisterScreen`, `ForgotPasswordScreen`, `GetStartedScreen`,
`SplashScreen`, `OnboardingOverviewScreen`, `BookingScheduleScreen`,
`EarningsOverviewScreen`, `FirstTimeSetupScreen`, `ActiveNavigationScreen`,
`DeliveryVerificationScreen`, `ExpenseScreen` (+ alias `ExpensesScreen`),
`ProfileScreen`, `IssuesScreen`.

## Release checklist

1. Version bump in `mobile/package.json` + `app.config.js` (`expo.version`).
2. Env audit: every `EXPO_PUBLIC_*` set for prod (wss/443, GSTN key, DSN,
   PostHog) and no dev hosts leaked into EAS secrets.
3. `npx expo export --platform ios --platform android` succeeds from clean tree.
4. Mutation score ≥ break-even threshold configured in `stryker.config.json`.
5. Jest coverage ≥ 80% (`npm run coverage`).
6. Bilingual parity test passes (en/hi locale keys — i18n parity suite).
7. Battery benchmark ≤ 3%/hr on Pixel 7 and iPhone 14 Pro during active trip.
8. FCM high-priority wake test: app responds within 90s of push in doze mode.
9. Airplane-mode drill ×5: toggle airplane mode five times mid-trip; queue
   auto-recovers and flushes fully on reconnect without duplicates
   (idempotency keys intact).
10. Store metadata ready: bilingual screenshots (en + hi) for both platforms.

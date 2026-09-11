# A10: Mobile stub inventory

Date: 2026-09-11. Method: every `*Screen` under `mobile/src`, route
registration in `mobile/App.tsx`, direct `/api/v1/*` strings + service-layer
imports per screen, zero-TODO check. ROADMAP's "only 2 .tsx" note is stale:
// `src/screens/*.ts` are one-line re-export shims; implementations live in
// `src/components/*Screen.tsx` (plus `src/features/*/screens/`).

## Result: 4 stubs, 13 wired

| Screen | Status | API binding |
|---|---|---|
| Splash | wired (nav only, no API needed) | — |
| GetStarted | wired (presentational intro, no API needed) | — |
| Login | wired | `POST /api/v1/auth/token`, `GET /api/v1/drivers/me` |
| Register | wired | `POST /api/v1/auth/register` |
| ForgotPassword | wired | `POST /api/v1/auth/forgot-password` |
| OnboardingOverview | **stub** (static mockup image, no data) | — |
| BookingSchedule | **stub** (static mockup image, no data) | — |
| EarningsOverview | **stub** (static mockup image, no data; not registered in `App.tsx`) | — |
| QRDemo | wired (device camera/QR tool, no API needed) | — |
| DriverOnboarding (`FirstTimeSetup` route) | wired (7 steps + `onboardingApi`) | `GET me/onboarding`, `POST me/license`, `POST me/vehicle-claims`, `POST me/payout-account`, `(+ documents, verification/submit)` |
| Trips (Main tab) | wired (`src/screens/TripsScreen.tsx`, 364 lines) | `GET /api/v1/trips` (+ stops/POD) |
| Dispatch (Main tab) | wired (`src/screens/DispatchScreen.tsx`) | `GET /api/v1/trips`, `GET me/offers` |
| Paisa (Main tab) | wired (via `driverMoney` service) | `/api/driver/balance|settlements|advances` |
| ActiveNavigation | wired (via `telemetry` service, 860 lines) | telemetry live/GPS path |
| DeliveryVerification | wired (1066 lines) | `POST /api/v1/trips/{id}/deliver-pod`, stops `reach|pod|complete` |
| Issues | wired | `GET/POST /api/v1/drivers/me/issues` |
| Profile | wired (920 lines) | `GET/PUT /api/v1/drivers/me`, `POST me/status` |
| Expenses | wired | `POST /api/v1/kharcha/expense` |

Notes:
- No TODO/FIXME in any screen file. Stub-word hits elsewhere are asset names
  (`mockupImage`) and copy, not code stubs.
- Every mobile-hit endpoint verified present in `openapi.yaml` AND on the
  chi router (`lifecycle_handlers.go:24-53`, `main.go:996-1018`) — mobile
  contract fully covered by B12 spec. No missing-route mobile breakage found.
- Document upload (`documentUploadApi`) hits `POST /api/v1/drivers/me/documents`
  (spec-covered, mounted).

## Per-feature mobile tickets
- OnboardingOverview / BookingSchedule / EarningsOverview: either wire to
  real endpoints (customer bookings, settlements/wallet) or delete — one
  ticket, no backend needed unless wiring.
- EarningsOverview is dead code (unregistered): delete or register.

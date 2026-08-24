# Feature Verification Audit — 2026-08-23

Code-path verification of external gap analysis vs actual implementation.
Method: parallel sub-agent exploration, file:line evidence, build/test runs.
Scope: code paths only; DB contents and mobile/ excluded.

## Confirmed gaps (external analysis was right)

- **Scorecard feeds**: 2 of 7 behaviour feeds have producers. Only
  `fuel_theft_suspicion` + `odometer_rollback` (internal/fuel/engine.go:635,704
  via insertBehaviour). `ScorecardService.WriteBehaviourEvent`
  (scorecard_service.go:180) has zero callers. Alert-kind constants for
  speeding/harsh_braking/harsh_accel/idling/night_driving exist in
  internal/telemetry/contracts.go:25-36 but are never referenced elsewhere.
  Event-less drivers render 100/tier-A via COALESCE(score,100) — misleadingly
  perfect, not zero.
- **Detection logic absent**: ingestion (internal/telemetry/ingest.go:82-236)
  emits positions/snapshots/outbox PositionEvent only. No speed-threshold or
  accel-delta calculation exists anywhere.
  TelemetryService.ProcessTelemetryStream (GPS-deviation/fuel-drop rules,
  internal/service/telemetry_service.go:50-151) is test-only, never wired.
- **SOS**: consumer chain complete (alerts/pipeline/sos.go — blocker alert,
  telegram fan-out, 10-min escalation). Producer stub:
  mqtt_ingest.go:131 logs TODO, never emits SOSEvent. No REST /sos endpoint,
  no mobile SOS button. Only tests publish SOSEvent.
- **Dashcam**: zero code anywhere.
- **Device adapters**: MockProvider only (providers/mock.go). Config vars
  WebhookSecretLocoNav/WheelsEye, WheelsEyeAccessToken/PollInterval loaded but
  dead (config.go:173-176,337-340). Spec'd webhook route unimplemented.
  Migration 00040 allows provider values 'own','loconav','wheelseye'.
- **Onboarding**: self-register assigns DefaultRoleID(RoleViewer)=id 4
  (auth.go:172; auth_handler.go:63). No owner role constant exists
  (roles: admin=1, org_admin=6, dispatcher=2, accountant=3, viewer=4,
  driver=5). Registration creates no tenant; tenant hardcoded
  shared.DefaultTenant="1". /company/onboard requires settings:update casbin
  AND session.Role=="admin" — unreachable for viewers. Redirect loop:
  dashboard.go:24-28 sends logged-in users to /company/onboard when company
  name empty; settings.go:36 bounces non-admins back to /dashboard forever.
  First admin only via BOOTSTRAP_ADMIN_EMAIL/PASSWORD env (main.go:1426-1454).
- **Multi-stop bookings**: bookings.route_id single FK (00006_bookings.sql:7).
  Zero waypoints/legs/stops tables across all migrations. Route optimizer's
  RouteLeg (route/optimizer/provider.go:53-64) is ad-hoc VRP JSON, no FK to
  bookings.
- **Locations free-text**: routes.source/destination TEXT (00005_routes.sql),
  "normalized" columns are LOWER(TRIM()) dedup only. Geocoding exists solely
  client-side in geofence_draw.js:345 (Nominatim map search), never persisted.
- **E-POD OTP/SMS**: server-side OTP fully operational+tested
  (trip_service.go:565-626, POD1-POD6 tests); SMS delivery unwired by design —
  operator relays verbally until SMS channel lands.
- **Notifications**: email=log-only stub, sms/push/webhook return errors,
  in-app=in-memory capped map (operations/notifications/service.go).
  Telegram (alerts/channels/telegram.go, telebot) is the only live external
  channel. No SMTP lib in repo at all.
- **Share links**: real security (SHA-256 token hash, salted PIN hash, HMAC
  cookie, lockout, rate limit). Delay notification absent — 30s client-poll
  only; no delay-event subscriber exists.
- **Razorpay**: real SDK client, HMAC webhook verify over raw body, restart-
  safe idempotency (webhook_event_id table + UNIQUE dedup), handles
  captured/paid/refund/failed. Caveat: reconciliation is event-driven only,
  no batch statement pull.

## Refuted claims (external analysis was wrong)

- **AI Assistant NOT demo-only**: real OpenAI-compatible HTTP calls
  (agent/client.go:103), real mutations via service layer (tools.go:157
  CreateBooking), approval gate default-on fail-closed
  (AGENT_REQUIRE_APPROVAL=true, config.go:327), RL episode recording,
  5 sub-agents. Keyless mode degrades to keyword routing honestly.
- **Self-host NOT "not started"**: multi-stage distroless Dockerfile
  (CGO_ENABLED=0, pure-Go modernc.org/sqlite), docker-compose.yml wires app +
  mosquitto MQTT, goose migrations auto-run at boot (main.go:160-178),
  deployment docs ARCHITECTURE.md §13. Turnkey gaps: .env population,
  DATABASE_URL must point under mounted volume.
- **Geofence→trip transitions EXIST**: dwell_worker.applyTransitions
  (:244-287) auto-reaches pickup, auto-starts transit; wired main.go:579-581;
  tested (dwell_worker_test.go:273-342). Gated behind company_config
  geofence.auto_reach_pickup / auto_start_transit, default false per spec
  Phase C (02-geofence-engine.md:461). delivered/completed remain manual.
  Dwell also opens/closes billable trip_detentions.
- **E-Way Bill NOT hard-broken**: builds, vets clean, 12/12 tests pass. Real
  issue: legacy worker.go has 3 column mismatches (part_b_updated_at,
  eway_bill_id/details, cancellation_reason) that silently self-disable via
  SchemaReady — zombie duplicate path. Service+monitor path healthy.
  worker_test.go fabricates schema instead of migrating → validates bugs as
  correct. NIC API mocked pending credentials.
- **FASTag NOT purely mock**: factory returns live NETC client when
  !UseMock && APIKey set (integration/fastag/factory.go:22); reconcile.go does
  provider pull + dedup + persistence (source='PROVIDER'). Mock is default;
  adapter shape unvalidated against real aggregator API.
- **REST API docs+rate limiting EXIST**: openapi.yaml served live at
  /openapi.yaml + /openapi.json; middleware/ratelimit.go sharded fixed-window
  + distributed cache INCR variant, fail-closed. Missing piece: static API-key
  management (auth = HMAC-signed bearer tokens only).
- **Customer portal not strictly view-only**: trip feedback submission works
  (customer_portal.go:621-651). True blocker: customer_users provisioning —
  zero INSERT paths outside migration 00073; portal role perms granted only
  to 'customer' role; needs manual SQL today.

## ETA reality

Hybrid heuristic, not haversine: odometer-delta vs manual routes.distance,
30-min rolling speed window, 0.7*telemetry+0.3*scheduled blend, confidence
bands, monotonic guard, ±15min window (eta/service.go). eta_history recorded
but PredictFromHistory has no production caller. Accuracy ceiling = manually
typed route distance.

## Historical playback reality

telemetry_positions persisted + indexed; GET /api/v1/telemetry/history
(vehicle_id, minutes≤1440, 500pt cap); tracking.html draws breadcrumb trail.
Missing: playback UX (scrubber/animation/trip-scoped replay/arbitrary range).

## P0 adjustments vs external list

- E-Way Bill item: delete-or-fix zombie worker.go + rewrite lying test with
  real goose migration; NIC adapter stays credential-blocked.
- FASTag item: validate existing adapter against real aggregator, not build
  from scratch.
- ADD: notification channel adapters (SMTP/SMS gateway/WhatsApp) — single
  blocker behind delay notifications, alert fan-out, password reset, POD OTP
  delivery.

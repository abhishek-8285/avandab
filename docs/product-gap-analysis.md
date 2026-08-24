# Product Gap Analysis — VERIFIED against codebase (2026-08-23 session)

Original homepage-based audit re-verified by four parallel code-tracing agents.
Every row below carries file:line evidence gathered this session. Rows marked
STALE were wrong in the original audit (feature since built or misjudged).

## Verdict tally
- STALE/FIXED (audit was wrong): 13 items
- CONFIRMED_GAP (still true): 21 items
- PARTIAL (nuance): rest

## STALE rows — do NOT treat as broken

| Original claim | Reality (verified) |
|---|---|
| E-Way Bill 🔴 DB-query bugs block generation | `internal/ewaybill/autogenerate.go:85` bug fixed + regression-tested (`worker_test.go:188`). Full Part-A/B/extend/cancel lifecycle (`service.go:129-447`, worker sweeps, migration 00047). Residual: worker pinned to stub client (`cmd/server/main.go:1381`) + hardcoded GSTINs (`worker.go:203`) → P2 wiring |
| Audit trail 🟡 weak | ~30 call sites across booking/trip/kharcha/payments/auth/telemetry-spoof; IP capture; RBAC viewer page. Nits only: fire-and-forget errors, no export |
| Fuel audit 🟡 manual-only | Full chain default-on (`TELEMETRY_ENABLED=true` → ingest snapshots → `internal/fuel/engine.go` events → 3-check audit → kharcha approval gate `kharcha_service.go:219`). Residue: device adapters mock-only |
| Kharcha 🟡 framework | Submit/approve/reject + mobile photo upload + auto settlement-deduction all wired (`kharcha_service.go:201-312`). Residues: no `settled` writer, web receipt URL-paste |
| PnL 🟡 no fuel modeling | Per-trip LivePnL uses odometer×fuel_prices with confidence bands (`pnl/service.go:60-106`). TRUE residues: fuel-kharcha double-count in margin; daily tenant PnL double-counts kharcha inside net_payout and ignores telemetry fuel |
| E-POD 🔴 OTP not operational | OTP generate/regenerate/48h-TTL/constant-time verify/pod_otp_verified write ALL work + tested (`trip_service.go:554-623`, `mobile_api_test.go:484`). Only SMS delivery stubbed |
| AI Assistant 🔴 demo | Real orchestrator + 19 tools + fail-closed approval gate + RL loop (`internal/agent/*`). Needs `AGENT_API_KEY`; keyword router works keyless |
| Self-host ⚫ none | Dockerfile + docker-compose + goose auto-migrations exist. Bugs: compose DB-volume path, no deploy README |
| REST API 🟡 undocumented | openapi.yaml served at /openapi.yaml(+json). Missing: Swagger UI, long-tail resources, api-key mgmt, resource-group rate limits |
| Rate limiting missing | Auth/webhooks/share ARE rate-limited; resource `/api/v1` group is not |
| Licensing missing | LICENSE (proprietary) + NOTICE ship in repo |
| Playback ⚫ missing | Breadcrumb endpoint + polyline trail shipped (`telemetry/history.go`, tracking Trail btn). True time-scrub playback still missing |
| Onboarding ⚫ no path | Bootstrap-first-admin env path (`main.go:1421-1454`), admin role-edit UI, org_admin role migration 00064. Gap is SELF-SERVE elevation ergonomics |

## CONFIRMED TRUE GAPS — ranked by severity

### P0-A Data-integrity hazard (fix immediately)
1. **FASTag mock fabricates money data**: empty table → synthesized plazas/₹85 txns (`fastag/client.go:221-235`) that reconcile into APPROVED driver expenses; balance falls back to hardcoded ₹2475.50 (`:129`); integration routes get client with NO db (`integration/handler.go:41`). Real NETC client exists behind env flags but default = fabrication.
2. **Fuel AlertEvents malformed** into canonical alert engine: founder-shape JSON keys → empty alert_type, dedup collisions (`fuel/engine.go:563` vs `alerts/pipeline/engine.go:96`).
3. **Daily PnL double-counts kharcha** (expenses include net_payout which already nets kharcha advances) while approved fuel claims ALSO stack on per-trip margin (`pnl/service.go:97-100`, `pnl_service.go:112`).

### P0-B Dead safety paths (built consumers, no producers)
4. Speeding / harsh-braking / harsh-accel / idling / night_driving: ZERO emitters anywhere — ingest has no motion rules at all (`telemetry/ingest.go:82-236`); contracts defined, never referenced (`telemetry/contracts.go:25-36`). Scorecard runs on fraud-events only → scores misleading.
5. SOS: MQTT parses flag but `onSOS` logs a TODO (`mqtt_ingest.go:126-133`); consumer pipeline complete (`alerts/pipeline/sos.go`) — unreachable from hardware.
6. gps_deviation rule is dead code: `ProcessTelemetryStream` has no production caller; callers must supply PlannedRoute coords nobody provides; `telemetry_alerts` table has no reader/UI.
7. DTC fault pipeline: full handling both sides, zero publishers.
8. Geofence breach events published to bus+outbox, ZERO subscribers (`dwell_worker.go:347`; absent from realtime forward list + alert subscriptions).

### P1 Functional gaps (real, ranked)
9. Notifications: email AND push are log-stubs like SMS; only Telegram + in-app deliver anything (`channels/stubs.go`, `operations/notifications/service.go`).
10. Geofence→trip auto-transitions triple-gated OFF (addon flag + company_config defaults false + only started→reached_pickup→in_transit covered).
11. ETA learning loop dead-ends: `eta_history` recorded nightly, `PredictFromHistory` has zero callers (`eta/history.go:34`); no traffic/weather inputs.
12. Customer portal truly view-only: no customer_users write path anywhere, no invoice download in portal, feedback is sole action.
13. Zero delay-notification triggers (no eta_slip type exists in alert domain).
14. Vendor settlements don't exist; payout execution is manual payment_ref text; TripDelivered auto-settlement uses hardcoded placeholders (advance 200/deduct 50) unless run manually (`service.go:229`).
15. Multi-stop bookings impossible (single route_id FK, no stop tables); VRP optimizer exists but feeds nothing back into booking model.
16. Locations free-text; Nominatim geocoding exists only inside geofence-draw JS, no Go-side service.
17. Mobile: single DriverStack; auth-store `role` never read for navigation; no owner screens at all. Voice expense mic opens text box (STT provider throws NoOp).
18. Marker teleport on every update (setLatLng jump; camera animates only in follow mode).
19. Resource `/api/v1` rate limiting absent; no api-key create/revoke/scope.
20. No planned-vs-actual distance reconciliation outside PnL fuel math.
21. Compose DB volume path bug + no deployment guide.

## Recommended attack order
Wave 1 (stop active harm): #1 guard FASTag fabrication + wire db into integration handler · #2 fix fuel alert mapping · #3 fix PnL double-counts.
Wave 2 (light up dead safety code): motion-rule evaluators in ingest (#4) → automatically feeds scorecard; SOS outbox emission (#5); deviation wiring (#6); breach subscriber (#8).
Wave 3: email provider, geofence gates, ETA feedback loop, portal actions, vendor settlements.

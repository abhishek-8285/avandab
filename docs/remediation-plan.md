# MVTMS Remediation Plan — verified fix-and-update doc

Date: 2026-08-23 · Repo: `Desktop/temux/basic` (Avandab/MVTMS) · HEAD `5259caa`
Inputs reconciled: (1) older homepage-based feature audit (the "54-item" list),
(2) `docs/FEATURE_AUDIT.md`, (3) `docs/product-gap-analysis.md`, (4) fresh
file-level verification performed this session across safety/compliance, ops,
and money paths — **including the uncommitted working tree**, which contains
partial Wave-1 fixes from the gap-analysis attack order.

Verdict method: every row below carries file:line evidence gathered this
session. Where the three documents disagree, this doc is authoritative.

---

## 1. Headline corrections to the older audit

The older homepage audit predates significant work. Its biggest errors:

| Old claim | Reality (verified) |
|---|---|
| Notifications email/SMS/push complete | **Most wrong claim in either document.** Real channels: Telegram (`alerts/channels/telegram.go:60-89`) + in-app only. Email/WhatsApp/SMS are log-stubs (`channels/stubs.go:34-41`); ops-service `SendSMS/SendPush/SendWebhook` return errors (`operations/notifications/service.go:73-83`). No FCM/APNs anywhere. Downgrade 🟢→🔴. |
| E-Way Bill 🔴 broken (DB bugs block generation) | Autogenerate query bug **fixed + regression-tested** (`ewaybill/autogenerate.go:88`, comment :85-87, `worker_test.go:188`). Full Part-A/B/extend/cancel lifecycle exists (`service.go:129-447`) with migration `00047`. Residual breakage is different and worse than documented — see §3-A. |
| E-POD OTP "not fully operational" | Server-side OTP fully works: generate/regenerate/48h-TTL/constant-time compare/`pod_otp_verified` write (`trip_service.go:554-626`), tested (`mobile_api_test.go:484-527`). Only SMS push of OTP is stubbed. |
| AI Assistant demo-only | Real orchestrator + **21** tools + fail-closed approval gate (default on, `config.go:327`) + online RL loop (`internal/agent/*`). Needs `AGENT_API_KEY`; keyword router works keyless. |
| Self-host not started | Dockerfile embeds goose auto-migrations (run at boot, `main.go:161-178`); compose includes mosquitto. Bugs remain (§3-G). |
| Driver scorecard "broken" | Accurate but imprecise: scores compute correctly off `driver_behaviour_events`; the defect is upstream — 5 of 7 event types have zero producers (§2 #4). |

Where the old audit was right and stays right: multi-stop missing, free-text
locations, no ML ETA, no smooth map animation, portal view-only, mobile
driver-only, dashcam absent, setup wizard absent.

---

## 2. Verified feature status (authoritative)

Legend: ✅ working · 🟡 partial (real residue) · 🔴 built-but-dead or hazardous · ⚫ absent

### Operations
| Feature | Status | Evidence / residue |
|---|---|---|
| Bookings CRUD | ✅ | handlers/bookings.go + coverage suites |
| Multi-stop routes | ⚫ | Booking & trip carry single `RouteID` (`domain/booking/entity.go:16-30`, `00006_bookings.sql:7`); VRP optimizer real (`route/optimizer/provider.go:10-91`, `/api/v1/routes/optimize` main.go:701) but **nothing writes sequences back** |
| Standardized locations | ⚫ | `routes.source/destination TEXT` (`00005_routes.sql:3-4`); Nominatim configured (`config.go:193,356`) never consumed in Go |
| Distance reconciliation | ⚫ | Only indirect use: fuel-audit Check B odometerΔ/route-distance fallback (`fuel_audit_service.go`) |
| Geofences + detention | ✅ | dwell worker real; entry/exit/dwell + detention billing |
| Auto trip transitions | 🟡 | Wired (`dwell_worker.go:248-279`, main.go:581) but gated by `company_config geofence.auto_reach_pickup/auto_start_transit` defaulting **false**; covers start→reached_pickup→in_transit only. Note: addon flag itself defaults ON (`features.go:47-48`) — one hard gate, not "triple-gated" |
| Geofence breach alert delivery | 🔴 | Emitted to outbox+bus atomically (`dwell_worker.go:328-351`) but **zero subscribers**: bus subs in main.go:217-232 omit it; realtime forward list omits it (`realtime/bus.go:18-26`). Dead-end |
| Live map | 🟡 | OSM-only post-`61092e7`; SSE 5s frames (`handlers/map.go:35-61`) + poll fallback; markers teleport (`setLatLng`), no tweening |
| Trail/playback | 🟡 | Breadcrumb endpoint ≤500 pts/≤24h (`telemetry/history.go:21-52`) + Trail polyline; no time-scrub |
| ETA | 🟡 | Hybrid heuristic 0.7·telemetry + 0.3·scheduled, ±15 min clamp, confidence bands (`eta/service.go:314-388`); learning loop `PredictFromHistory` has **zero callers** (`eta/history.go:34`); no traffic/weather |
| Route deviation (gps_deviation) | 🔴 | Rule real (`service/telemetry_service.go:50-100`) but `ProcessTelemetryStream` has **no production caller**; needs caller-supplied PlannedRoute coords nobody provides; `telemetry_alerts` table write-only, no reader/UI |
| Device adapters (LocoNav/WheelsEye) | ⚫ | Env placeholders only (`config.go:337-340`); providers dir = no-op MockProvider; real path = own-device HTTP/MQTT ingest |
| Setup wizard | ⚫ | Zero matches repo-wide |

### Safety
| Feature | Status | Evidence |
|---|---|---|
| Speeding / harsh-braking / harsh-accel / idling / night_driving emitters | ⚫ | Contracts declared, referenced nowhere else (`telemetry/contracts.go:25-36`); ingest steps 1-12 contain no motion evaluation (`ingest.go:82-236`); zero hits in mobile/, mqttservice/ |
| Scorecard | 🟡 misleading | Reads `driver_behaviour_events` (`scorecard_service.go:682-687`); 7 weighted types (:35-43); only producers = fuel fraud events (`fuel/engine.go:635,645,704`); intended motion producer `WriteBehaviourEvent` (:180-210) has **zero callers**. Scores currently reflect fraud-events only |
| SOS | 🔴 | Consumer pipeline complete (`alerts/pipeline/sos.go:17-129`: blocker alert, 10-min escalation, in_app+telegram; subscribed main.go:229-234). Producer side: MQTT parses `sos` flag → `onSOS` logs TODO only (`mqtt_ingest.go:126-133`); no HTTP endpoint, no mobile button, zero publishers of `SOSEvent` |
| Dashcam | ⚫ | Nothing exists (doc mention only) |

### Compliance
| Feature | Status | Evidence |
|---|---|---|
| E-Way Bill service layer | ✅ | Lifecycle complete + tested; manual/API generation routes live (`integration/handler.go:52-58` → main.go:289) |
| E-Way Bill automation | 🔴 | Three independent blockers — see §3-A/B/C |
| FASTag | 🟡 (fixed in working tree, uncommitted) | HEAD fabricates money data (§3-D); uncommitted diff gates synthesis behind `UseMock`, wires db, adds guards + tests; residual `tenantID := "1"` (`fastag/reconcile.go:26`) |
| GST invoicing | ✅ | CGST/SGST/IGST split by state-prefix (`generate_invoice.go:261-289`); HSN fail-closed validation; first-save drop fixed (commit `b3b385a`, ancestor of HEAD); e-invoice IRN mocked pending GSP creds (`gstn/einvoice.go:112-159`) |
| Audit trail | ✅ | ~30 call sites, IP capture, RBAC viewer page; nits: fire-and-forget errors, no export |

### Money
| Feature | Status | Evidence |
|---|---|---|
| Razorpay | ✅ (refund path dead) | Real SDK, HMAC constant-time verify checkout+webhook, idempotent webhooks (TTL cache + DB), public rate-limited route (`main.go:644`). **Refunds structurally dead**: `SetReversePaymentUseCase`/`SetEventBus` never called → `refund.processed` errors forever (retry loop), `payment.failed` silently no-ops (`razorpay_webhook.go:139-146,355-381`) |
| Kharcha ledger | ✅ (residues) | Submit/approve/reject transactional + GPS + offline idempotency (`kharcha_service.go:360-428,201-312`); mobile photo receipt real (`kharcha.go:126-198`); residues: no `settled` writer anywhere, web form URL-paste |
| Settlements + TDS 194C | ✅ (hazard §3-E) | Full state machine, PAN-based TDS 1%/2%, kharcha deduction lines with ref_ids (`driver_settlement_service.go:45-52,139-150,320-374`) |
| Fuel audit | ✅ | Chain default-on (`TELEMETRY_ENABLED` defaults `"true"` config.go:336): snapshots → engine (median smoothing, spike-hold, refill/theft/siphon classification `engine.go:606-680`) → 3-check audit (`fuel_audit_service.go:224-292`) → kharcha approval gate (`kharcha_service.go:216-229`) |
| PnL | 🟡 fixes incomplete | Per-trip formula/bands real (`pnl/service.go:60-118`). Original double-counts real. Uncommitted fixes directionally right but: toll double-count **still open** (§3-F), per-trip fix under-counts when telemetry partial, and `pnl/service_test.go` **does not compile** (`go vet ./internal/pnl`: `undefined: context` at :64,:89) — violates Prove-It protocol |
| Accounting sync | ✅ pipeline / 🔴 adapters | Consumer+idempotency real (`accounting/consumer.go:57-131,195-299`); tally/zoho/QB clients fabricate SUCCESS + synthetic ExternalIDs persisted as acked (`tally_client.go:20-51`) — opt-in but same fabrication class |

### Customer & extras
| Feature | Status | Evidence |
|---|---|---|
| Share links | ✅ | crypto/rand 32-byte token, hash-only storage, optional PIN (salted SHA-256 + signed cookie), TTL/revoke/lockout, per-route rate limits (`share.go:67-99,169-194,273,309`; main.go:964-966) |
| Customer portal | 🟡 + security hole | Four capabilities only (list bookings/invoices, tracking, feedback); no self-registration (`customer_users` table created `00073`, never populated). **Authz fallback hole**: strict lookup `ErrNoRows` falls back to tenant-wide query and serves the trip anyway (`customer_portal.go:356-391`, feedback `:605-613`) — any authenticated tenant user can view others' trips. Neither prior doc flagged this |
| Mobile app | 🟡 driver-only | All driver flows; role stored but never read for navigation (`authStore.ts:7`, analytics-only `App.tsx:278`); zero owner/approval screens |
| E-POD | ✅ (SMS stub) | See §1 |
| AI assistant | ✅ (keyed) | Orchestrator, 21 tools, approval gate fail-closed default-on, RL loop; admin decisions at `/agent-actions` |
| Experiments | ✅ | SHA1 sticky bucketing persisted (`experiments.go:28-51`, `experiments_service.go:362-409`), full lifecycle API + admin UI + dashboard A/B |
| REST API | 🟡 | openapi.yaml served; auth/webhook/share rate-limited; `/api/v1` resource group NOT; api-key mgmt missing; **spec 500s inside Docker** (§3-G) |
| Self-host | 🟡 | Exists; two bugs (§3-G) |
| Onboarding | 🟡 | Bootstrap-admin env path (`main.go:1421-1454`); `/register` → viewer is deliberate least-privilege (`auth.go:169-172`); elevation via admin Users CRUD; org_admin migration `00064`. Gap = self-serve elevation ergonomics, not absence |

---

## 3. Hazards found THIS session (not in any prior doc)

**A. EWB worker dead against real migrated DB.** `Worker.SchemaReady`
(`worker.go:110-118`) requires column `part_b_updated_at` — **no migration
creates it** (00047 creates `part_b_json` etc.; zero grep hits). `Tick` always
skips. Worker INSERTs also use it (`worker.go:221,449`). Tests pass only
because they hand-roll the schema (`worker_test.go:100,112`).

**B. EWB Part-A auto-trigger unreachable.** Subscribes
`TripConfirmedEvent`/`trip.confirmed` (`autogenerate.go:59-60`); nothing
publishes either. Booking confirm publishes `booking.confirmed`
(`booking_service.go:176-181`) which only auto-creates a trip. Only
`TripAssignedEvent` fires (`trip_service.go:253,340`) → Part-B attach works,
Part-A never auto-fires.

**C. EWB worker pinned to stub + hardcoded GSTINs.** `cmd/server/main.go:1381`
passes nil → stubClient fabricates `EWB-MOCK-*`; `worker.go:203-205` hardcodes
three GSTINs.

**D. FASTag fabricated money data (HEAD).** Empty table → synthesized plazas/
₹85 txns feeding `AutoKharcha=true` → fabricated APPROVED driver expenses;
balance fallback hardcoded ₹2475.50 reachable even db-less (committed
`client.go:123-132`); fake reconcile counts `Pulled:5 Matched:5`; db-less
client handed to integration routes (committed `handler.go:41`). Uncommitted
diff fixes gating+wiring+tests. Residual: `reconcile.go:26` hardcoded
`tenantID := "1"`. Trap: enabling integration without `USE_MOCK=false`
reactivates synthesis.

**E. Placeholder settlement lock-out.** TripDelivered auto-settlement writes
hardcoded fare 1200/advance 200/deduction 50, no lines (`service/service.go:229`
→ `:750-794`). `GenerateSettlement(force=false)` returns existing row
(`driver_settlement_service.go:114-120`) and handlers default force=false
(`settlement.go:128-155`) → real payout computation blocked until an operator
knows to send `force_recompute=true`. Silent wrong money.

**F. Toll double-count survives current fix.** FASTag-approved toll expenses
settle into `advances` (category toll included, `:323`) AND daily PnL sums
`fastag_transactions` separately (`pnl_service.go:91-96`). Uncommitted NOT
EXISTS filters category `'fuel'` only. Same rupee twice; worse while D was
live (fabricated tolls counted twice).

**G. Self-host defects.** (1) Volume mismatch: compose mounts `./data:/data`
but default `DATABASE_URL=file:transport.db` resolves to WORKDIR `/` → SQLite
lands outside mounted volume unless hand-edited (`docker-compose.yml:9`,
`config.go:250`). (2) No deploy README. (3) `openapi.yaml` not copied into
distroless image → `/openapi.yaml(.json)` returns 500 in containers
(`openapispec/handler.go:38-49`, Dockerfile:21-25).

**H. Portal authz fallback** — see customer-portal row above.

**I. Razorpay refund hooks never wired** — see money row; causes permanent
webhook retry loop on refunds.

**J. Uncommitted work violates Prove-It.** `internal/pnl/service_test.go`
fails `go vet` (`undefined: context`), seeds `tenant_id='1'` but calls
Calculate with empty tenant context → would ErrNoRows even after import fix.
Fuel-alert legacy mapping test passes on a fixture shape `buildAlert` never
emits (lowercase keys, top-level event_type) while the real producer sends
`category:"FUEL"` uppercase and nested `metadata.event_type`
(`fuel/engine.go:812-834`) → events still classify as `telemetry/FUEL/HIGH`,
fuel-source alert rules won't match.

**K. GST edge:** unknown customer GSTIN defaults intra-state
(`invoices.go:301`) → CGST/SGST charged where inter-state B2C may require
IGST.

---

## 4. Remediation waves (attack order)

Effort: S <1d · M 1-3d · L 3-10d

### Wave 0 — land & repair in-flight work (before anything new)
1. Fix `internal/pnl/service_test.go` compile + tenant-context seeding; add behavioral exclusion test for absorbed claims (fuel AND toll) in daily PnL. Extend NOT EXISTS filter to `category='fuel' OR 'toll'` or better: filter ALL settlement-absorbed categories via `ref_id` join. **M**
2. Finish fuel-alert mapping against the REAL wire format: lowercase `strings.ToLower(category)`, read `metadata.event_type`, normalize severity; add test using `buildAlert`'s actual shape. **S**
3. Commit FASTag anti-fabrication diff; remove `tenantID := "1"` in `fastag/reconcile.go:26` (derive from context). **S**
4. Acceptance: `go build ./... && go vet ./... && go test ./internal/...` green; `USE_MOCK` default flip decision documented.

### Wave 1 — dead-safety-path producers (P0-B, highest liability)
1. Motion-rule emitters in ingest: evaluate speed-vs-limit, accel/decel deltas between consecutive frames, idle windows, night window; emit the five missing behaviour kinds via `WriteBehaviourEvent` + alert kinds via existing contracts. Gate behind `company_config` + scorecard flag. **L**
2. SOS producer: implement `onSOS` → outbox `SOSEvent` (pipeline already subscribed); add `POST /api/v1/sos` (authed) + mobile SOS button. **M**
3. Wire gps_deviation: feed PlannedRoute coords from trip at snapshot time, or deprecate rule + `telemetry_alerts` table explicitly. Pick one; delete if deprecated. **M**
4. Wire geofence breach subscriber: add `GeofenceBreachEvent` to bus subscriptions + realtime forward list. **S**
5. Enable-by-default decision for `geofence.auto_*` company_config keys (document why off today). **S**

### Wave 2 — money integrity
1. Kill placeholder settlements: stop writing hardcoded 1200/200/50 rows (or mark them `draft` + make GenerateSettlement treat placeholder rows as non-existing unless explicitly accepted). **M**
2. Wire Razorpay refund hooks: construct reverse-payment use case + event bus, call setters in main; handle `payment.failed` observability. **S**
3. GSTIN unknown-state policy: require state or treat as inter-state per tax counsel; add validation on customer save. **S**
4. EWB automation: add migration creating `part_b_updated_at` (+ any other columns worker INSERTs expect); publish `TripConfirmedEvent` from booking-confirm→trip-create path; replace hardcoded GSTINs with company_config reads; mount real NIC client behind creds flag. **M**

### Wave 3 — close UX/completeness gaps
Portal authorization fix (remove ErrNoRows fallback; scope strictly) **S** ·
Kharcha `settled` writer on MarkPaid **S** · Telegram→email channel upgrade or explicit single-channel claim **M** ·
Self-host: fix DATABASE_URL/volume default, copy openapispec into image, write deploy README **S** ·
Self-serve elevation flow (request-role → admin approve) **M** ·
Multi-stop data model (waypoints table + optimizer write-back) **L** ·
Standardized location entity + Nominatim consumption **M** ·
Time-scrub playback UI on breadcrumb API **M**

### Explicitly deferred / do-not-build-now
Dashcam (zero foundation — decide roadmap first), ML ETA traffic/weather inputs
(heuristic adequate; learning loop unwired — wire or delete `PredictFromHistory`),
push notifications (Telegram covers ops alerting short-term), accounting adapter
truthfulness (opt-in; label clearly until real endpoints contracted).

---

## 5. Claims policy (until waves land)

Safe today: bookings, dispatch, drivers/vehicles, GST invoicing + PDF, Razorpay
checkout+payments (not refunds), kharcha approval + photo receipts, settlements+
TDS, fuel audit, share links (PIN), experiments, AI assistant (approval-gated),
audit trail, self-host (after §3-G fixes).

Claim as beta: live map (no animation), trail (no scrub), ETA (heuristic),
portal (view-only), E-Way Bill (manual/API generation only — NOT automation),
REST API (rate limits partial).

Do not claim: safety scorecard fairness (fraud-only inputs today), speeding/
harsh-event alerting, SOS, notifications breadth (email/SMS/push), EWB
auto-generation, dashcam, multi-stop, accounting sync accuracy (fabricated
adapters), FASTag reconciliation (until committed fix ships + NETC creds).

---

## 6. Reconciliation note

`docs/product-gap-analysis.md` rows are broadly accurate but understate four
items this session added (§3-A/B/E/H) and overstate one ("triple-gated"
geofences — actually one hard gate). `FEATURE_AUDIT.md` should be updated when
Wave 0 lands: FASTag row stays MOCKED (creds) but loses "fabricates data"
hazard; EWB automation row must reflect §3-A/B/C blockers, not "bug fixed".

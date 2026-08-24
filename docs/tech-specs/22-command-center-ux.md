# Command Center UX & Fleet OS Program — Implementation Spec v1
Status: ready
Depends-on: Spec 09 (event bus/tenancy), Spec 10 (auth/RBAC), Spec 05 (alert
pipeline @00045/00059), Spec 08 (settlement @00051, doc vault @00052),
Spec 07 (EWB @00047, FASTag @00049), Spec 16 (PNL @00058), Spec 13 (mobile),
Spec 12 (PWA), migrations 00075 (i18n), 00076 (expense idempotency),
00082 (expense geo), 00089 (feature flags)
Migration owner: db/migrations/00092_alert_inbox.sql, 00093_driver_advances.sql,
00094_kharcha_verification.sql (exactly these three, reserved in index)

## 0. Verified ground truth

Verified 2026-08-24 via `ls` / file existence checks:

- Migration head on disk = `00091_route_locations.sql`. Next free numbers:
  00092+. Index rows 00068–00072 and 00085 are RESERVED in
  `00-migration-ownership-index.md` but no `.sql` files exist for them —
  they stay reserved; never reuse.
- `internal/alerts/pipeline/engine.go` — alert pipeline engine exists.
- `internal/service/kharcha_service.go`, `driver_settlement_service.go`,
  `pnl_service.go` — money services exist.
- `internal/handlers/map.go`, `app.go` — map + app handlers exist (SSE live
  map per Spec 04).
- `internal/telemetry/ingest.go`, `internal/realtime/hub.go` — ingest +
  SSE hub exist.
- `internal/middleware/middleware.go` — tenant context injection
  (`shared.ContextWithTenantID` / `TenantIDFromContext`) per Spec 09.
- `mobile/src/screens/` — driver app screens exist: ActiveNavigation,
  Expense(s), EarningsOverview, DeliveryVerification, Issues, Profile…
  (Spec 13).
- `internal/templates/` — ~120 templates; core problem this spec fixes:
  daily owner work requires visiting many pages.
- `feature_flags` table exists (00089) — every step below ships dark behind
  a flag.
- No `022-*.md` spec existed before this file (number was free).

## 1. Overview / goal

One-screen "Command Center" for owners + zero-friction driver money flows.
Owners manage the whole fleet — see money, approve kharcha, extend e-way
bills, settle, share tracking — WITHOUT page navigation. Drivers get
balance transparency + advance requests. Design rules lifted from verified
competitor UX (Samsara Incident Center, Geotab Smart Sequence ranked
alerts, Fleetbase kanban + Ledger wallets, LocoNav zero-training bar).

**Non-goals:** ELD (US-only), EV management, dash cams (Spec 05/17 own
telemetry events; video is future), GraphQL/gRPC (Spec 14 deferred),
multi-region (future spec 23), rewriting existing pages that still serve
admin work (users, settings, migrations stay multi-page).

## 2. API contract

All routes behind `RequireAPIAuth` + `RequirePermission`. Tenant derived
from `auth.ContextUser` — NEVER from request body. Errors use existing
`internal/httpx` envelope.

### 2.1 Alert inbox (Step S1)
```
GET  /api/alerts/inbox?status=open|snoozed|acked|all&limit=50
     perm: alerts:read
     → 200 {"alerts":[
          {"id":"al_123","type":"vehicle_stopped_off_route","severity":"critical",
           "severity_rank":1,"title":"GJ01EF stopped 3h off-route",
           "vehicle_id":"veh_9","money_at_risk":4500.00,
           "created_at":"2026-08-24T09:12:00Z",
           "actions":["locate","call_driver","snooze"]}]}
POST /api/alerts/{id}/ack          perm: alerts:write  body {} → {"ok":true}
POST /api/alerts/{id}/snooze       perm: alerts:write
     body {"minutes":120} → {"ok":true,"snoozed_until":"2026-08-24T13:00:00Z"}
POST /api/alerts/snooze-all        perm: alerts:write
     body {"ids":["al_1","al_2"],"minutes":120} → {"ok":true,"count":2}
```

### 2.2 Money strip (S2)
```
GET  /api/dashboard/money-strip    perm: dashboard:read
     → 200 {"date":"2026-08-24","revenue":42300.00,"spent":18900.00,
            "receivables":620000.00,"open_alerts":7,"critical":2}
```

### 2.3 Fleet context panel (S3/S4)
```
GET  /api/fleet/{vehicleId}/context   perm: vehicles:read
     → 200 {"vehicle":{"id":"veh_9","number":"GJ01EF","status":"stopped"},
            "position":{"lat":22.3,"lng":73.2,"speed_kmph":0,"at":"2026-08-24T09:00:00Z"},
            "trip":{"id":"tr_5","route":"Pune→Nagpur","eta_at":"2026-08-24T17:40:00Z"},
            "driver":{"id":"drv_3","name":"Ravi K","phone":"+91-98…"},
            "pnl_km_today":9.20,
            "kharcha_pending":[{"id":"exp_8","amount":1250.00,"category":"fuel"}],
            "eway_bill":{"id":"ewb_2","expires_at":"2026-08-24T14:00:00Z"},
            "fastag_balance":340.00,
            "docs_expiring":[{"kind":"insurance","expires_on":"2026-09-05"}]}
```
Actions reuse existing endpoints (kharcha approve/reject, settlement
generate, share-link create, driver call = `tel:` link client-side). Only
NEW action endpoint:
```
POST /api/ewaybill/{id}/extend     perm: ewaybill:write
     body {"valid_upto_hours":4} → {"ok":true,"new_expiry":"2026-08-25T00:00:00Z"}
     (adapter-flagged; mock returns shifted expiry)
```

### 2.4 Kanban board (S5)
```
GET  /bookings/board   perm: bookings:read   (HTML fragment or JSON via ?format=json)
     → {"columns":[{"status":"new","cards":[{"id":"bk_1","vehicle":"MH12AB",
          "driver":"Ravi","freight":24000.00,"deadline":"2026-08-25T10:00:00Z"}]}, …]}
Status changes go through EXISTING booking status API (Spec 09 immutability +
status history @00054). No new mutation endpoint.
```

### 2.5 Universal search (S6)
```
GET  /api/search?q=mh12            perm: (result-scoped: caller sees only
     entities their role permits; service unions vehicles/drivers/bookings/
     invoices/ewaybill queries already tenant-scoped)
     → 200 {"vehicles":[…],"drivers":[…],"bookings":[…],"invoices":[…],"eway_bills":[…]}
```

### 2.6 Driver money (S7)
```
GET  /api/driver/balance           perm (driver self): driver:read-self
     → 200 {"running_balance":12500.50,"last_settlement_id":"st_7",
            "last_settlement_at":"2026-08-20T18:00:00Z","pending_advances":1}
GET  /api/driver/settlements       → {"settlements":[{"id":"st_7","period":"…",
            "gross":18200.00,"deductions":4300.00,"net":13900.00,
            "status":"paid","tds":200.00}]}
POST /api/driver/advances          perm: driver:write-self
     body {"trip_id":"tr_5","amount":4000.00,"reason":"tyre puncture"}
     → 201 {"id":"adv_1","status":"pending"}
GET  /api/driver/advances          → {"advances":[{"id":"adv_1","amount":4000.00,
            "status":"pending","requested_at":"2026-08-24T09:30:00Z"}]}
POST /api/driver/advances/{id}/decision   perm: kharcha:approve (admin side)
     body {"decision":"approved|rejected","note":"…"} → {"ok":true}
```

### 2.7 Kharcha verification (S8)
```
POST /api/expenses/{id}/ocr        perm: expenses:write (driver self or admin)
     body {} → {"ocr_amount":3000.00,"ocr_confidence":0.94}
     (server-side OCR adapter; receipt photo already on file)
GET  /api/expenses/flagged         perm: kharcha:approve
     → {"expenses":[{"id":"exp_9","flag_reason":"distance_from_route_km=42.1",…}]}
```

### 2.8 Compliance radar (S9)
```
GET  /api/compliance/radar         perm: compliance:read
     → 200 {"expiring_soon":[{"entity":"vehicle","id":"veh_9","kind":"insurance",
            "expires_on":"2026-09-05","days_left":12}],
            "ewaybill_expiring":[{"id":"ewb_2","expires_at":"…","hours_left":5}]}
Radar data itself = existing 00052 doc-vault tables + 00047 eway_bills.
Alerts flow through EXISTING alert pipeline rules (no new alert types).
```

### 2.9 WhatsApp channel (S10)
No new routes. Existing alert pipeline channel interface gains a
`whatsapp` sender; `notification_prefs` (@00045) gains channel value
`whatsapp`. Delivery logged to `notification_log` (@00058).

## 3. DB contract

### 00092_alert_inbox.sql
```sql
-- +goose Up
ALTER TABLE alert_events ADD COLUMN ack_status TEXT NOT NULL DEFAULT 'open'
  CHECK (ack_status IN ('open','snoozed','acked','resolved'));
ALTER TABLE alert_events ADD COLUMN severity_rank INTEGER NOT NULL DEFAULT 5;
ALTER TABLE alert_events ADD COLUMN money_at_risk REAL NOT NULL DEFAULT 0;
ALTER TABLE alert_events ADD COLUMN snoozed_until TIMESTAMP;
CREATE INDEX idx_alert_events_inbox
  ON alert_events(tenant_id, ack_status, severity_rank, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_alert_events_inbox;
ALTER TABLE alert_events DROP COLUMN snoozed_until;
ALTER TABLE alert_events DROP COLUMN money_at_risk;
ALTER TABLE alert_events DROP COLUMN severity_rank;
ALTER TABLE alert_events DROP COLUMN ack_status;
```

### 00093_driver_advances.sql
```sql
-- +goose Up
CREATE TABLE driver_advance_requests (
  id            TEXT PRIMARY KEY,
  tenant_id     TEXT NOT NULL,
  driver_id     TEXT NOT NULL,
  trip_id       TEXT,
  amount        REAL NOT NULL CHECK (amount > 0),
  reason        TEXT NOT NULL DEFAULT '',
  status        TEXT NOT NULL DEFAULT 'pending'
                CHECK (status IN ('pending','approved','rejected','paid')),
  requested_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  decided_by    TEXT,
  decided_at    TIMESTAMP
);
CREATE INDEX idx_advances_tenant_status
  ON driver_advance_requests(tenant_id, status, requested_at DESC);

-- +goose Down
DROP TABLE IF EXISTS driver_advance_requests;
```
NOTE: `driver_id` intentionally has NO FK (repo convention: free-form TEXT
ids, no cross-table FKs beyond what legacy tables already declare — same
rule the index applies to `tenant_id`). VERIFY `drivers.id` semantics at
implementation before adding any FK — do not add one by default.

### 00094_kharcha_verification.sql
```sql
-- +goose Up
ALTER TABLE driver_expenses ADD COLUMN verification_state TEXT NOT NULL DEFAULT 'manual'
  CHECK (verification_state IN ('manual','auto_verified','flagged'));
ALTER TABLE driver_expenses ADD COLUMN flag_reason TEXT NOT NULL DEFAULT '';
ALTER TABLE driver_expenses ADD COLUMN ocr_amount REAL;
ALTER TABLE driver_expenses ADD COLUMN ocr_confidence REAL;
CREATE INDEX idx_driver_expenses_verify
  ON driver_expenses(tenant_id, verification_state, created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_driver_expenses_verify;
ALTER TABLE driver_expenses DROP COLUMN ocr_confidence;
ALTER TABLE driver_expenses DROP COLUMN ocr_amount;
ALTER TABLE driver_expenses DROP COLUMN flag_reason;
ALTER TABLE driver_expenses DROP COLUMN verification_state;
```

RBAC seeds (via existing permission seeding path used by 00060/00061/00078):
`alerts:read`, `alerts:write`, `kharcha:approve`, `compliance:read`,
`driver:read-self`, `driver:write-self` — reuse any that already exist;
never double-insert (seed must be idempotent).

## 4. UI

RBAC resources: `command_center`, `bookings_board`, `driver_money`,
`kharcha_queue`, `compliance_radar`.

### 4.1 Owner Command Center (web, `/console` — becomes dashboard default when flag on)
Single page, three columns, no navigation for daily work:
```
┌ search (⌘K palette: vehicles/drivers/bookings/invoices/EWB) ──────────┐
│ money strip: today revenue | spent | receivables | alerts n🔴         │
├ fleet list ────┬ live map (SSE, existing hub) ──┬ context panel ──────┤
│ color-coded    │ selection-follows             │ trip/driver/PnL-km/ │
│ vehicle cards  │                               │ kharcha approve ▸/EWB│
│ +truck         │                               │ extend/docs/settle/  │
│                │                               │ share/call — inline  │
├ ranked alert inbox (severity_rank asc, batch ack/snooze) ─────────────┤
```
- Templates: `console.html` + partials `_fleet_strip.html`,
  `_context_panel.html`, `_alert_inbox.html`, `_money_strip.html`.
- JS: `static/js/console.js` (selection wiring, SSE subscribe, drawer,
  optimistic ack). Existing map JS reused.
- Menu collapses to 5: Console · Board · Money · Compliance · Admin.

### 4.2 Bookings kanban (`/bookings/board`)
Columns: new → assigned → en_route → delivered → settled. Card: vehicle,
driver, freight, deadline. Drag = existing status API; board subscribes to
event bus for other users' moves. Mobile: horizontal scroll columns.

### 4.3 Driver app (mobile/) — 4 bottom tabs
`Trip` (existing nav + e-POD) · `Kharcha` (existing + OCR confirm step +
offline queue) · `Paisa` (NEW screen: balance card, trip-wise earnings,
settlement history, advance request button + status chips) · `Docs`
(NEW screen: offline DL/RC/insurance/EWB copies). Replace current tab bar;
keep existing screens as pushes from these tabs.

### 4.4 Owner mobile/PWA
`console.html` responsive: columns stack, context panel becomes bottom
sheet. Service worker per Spec 12.

## 5. Business logic

### 5.1 Severity ranking (Geotab Smart-Sequence pattern)
`severity_rank` 1–5 assigned at emit-time in pipeline engine; inbox ORDER BY
severity_rank ASC, created_at DESC; snoozed rows hidden until
`snoozed_until < now()` (single worker sweep, reuses Spec 09 worker).
| rank | class | examples | money_at_risk |
|---|---|---|---|
| 1 | critical | off-route stop >2h, SOS, fuel-drain live, EWB <4h | event-specific estimate |
| 2 | urgent | EWB <12h, doc expiry ≤1d, kharcha flag high-confidence | fine estimate |
| 3 | money | kharcha flag low-confidence, kmpl drop >15% | claim amount |
| 4 | waste | idle >30m, harsh-driving burst | idle-fuel cost |
| 5 | info | geofence enter/exit, trip milestones | 0 |
`money_at_risk` formulas (documented, unit-tested): idle fuel = minutes ×
idle_lph × fuel_price; EWB = statutory penalty constant (config); kmpl drop =
lost_fuel_l × fuel_price; kharcha = claim amount. Constants in
`company_config` (@00042), tenant-editable.

### 5.2 Driver running balance
`running_balance = Σ paid settlements.net − Σ paid-out advances + Σ approved
unpaid advances` computed from settlement lines (@00051) + advances
(00093). Cached 60s per driver (existing `internal/cache`). Never negative
in UI without admin override note.

### 5.3 Kharcha auto-verify / auto-flag rules (S8)
Run async post-submit (event bus), never blocking driver sync:
1. `auto_verified` if: OCR confidence ≥0.90 AND amount within 20% of
   category median (last 90d, same tenant) AND distance-from-route ≤10km
   (uses @00082 geo + latest trip polyline).
2. `flagged` if ANY: distance-from-route >10km; duplicate (same driver,
   category, ±30min, ±5km); amount > 2× category median.
3. Else `manual`. Admin queue shows flagged+manual with evidence (photo,
   map pin). Decision events feed existing agent RL loop.

### 5.4 Compliance radar sweep
Nightly worker + on-write triggers: 30/7/1-day doc-expiry alerts, 12/4-hour
EWB expiry alerts — emitted through EXISTING alert pipeline as rank 2/1.
One-tap EWB extend → adapter (mock by default) → new expiry event.

### 5.5 Advance flow
request → status `pending` → admin decision (console panel or
`/agent-actions` if agent-suggested) → `approved` → linked into next
settlement run as advance line (existing engine @00051 already supports
advance lines; this table is the source of truth for pending ones).

## 6. Config / env

| var | default | purpose | reader |
|---|---|---|---|
| `COMMAND_CENTER_ENABLED` | false | flag: console page + redirect | handlers/app.go |
| `BOOKINGS_BOARD_ENABLED` | false | flag: kanban route | handlers/bookings.go |
| `DRIVER_MONEY_ENABLED` | false | flag: Paisa tab API | service/booking… driver handlers |
| `ALERT_INBOX_ENABLED` | false | flag: inbox UI+API | handlers/alerts* |
| `OCR_PROVIDER` | mock | mock\|tesseract\|http | integration/ocr (new) |
| `OCR_HTTP_URL` / `OCR_HTTP_KEY` | empty | HTTP OCR adapter | integration/ocr |
| `WHATSAPP_PROVIDER` | mock | mock\|gupshup\|meta | alerts/channels (new sender) |
| `WHATSAPP_*_CREDENTIALS` | empty | per-provider creds | alerts/channels |
| `VAHAN_PROVIDER` | mock | mock\|api | integration/vahan (new) |
| `EWB_EXTEND_ENABLED` | false | gate real extend calls | integration/ewaybill |

All external integrations = adapter + config-flagged mock (repo-wide scope
decision). No creds needed to build/test.

## 7. Tests

Per step, ALL must pass before merge (`go build ./...`, `go vet ./...`,
`go test ./internal/...`):
- Migration round-trip: goose up→down→up on clean SQLite (pattern per
  `test/helpers.go NewTestDB`; add migration-exists assertions for
  00092–00094).
- S1: rank assignment table-driven tests; inbox query tenant isolation
  (tenant A cannot see tenant B alerts); snooze expiry sweep test.
- S2: money-strip sums vs seeded fixtures (invoice+expense+pnl).
- S3: context endpoint — every sub-object from its real service, stubbed
  only at repo boundary; 404 + tenant-miss cases.
- S4: EWB extend — mock adapter shifts expiry; audit_log entry asserted.
- S5: board JSON mirrors booking status API; unauthorized drag rejected.
- S6: search — tenant scoping, permission-scoped result filtering.
- S7: balance formula incl. TDS + advances; advance lifecycle state test
  (pending→approved→paid idempotency); decision permissions.
- S8: OCR mock fixture returns canned amount; all 3 verification_states
  reachable via rule table tests; duplicate detection window test.
- S9: radar — doc 30/7/1 boundaries; EWB 12/4h boundaries.
- S10: whatsapp mock sender logs to notification_log; prefs honored.
- UI: Playwright (Spec 15) — console loads, select→panel updates, ack
  removes row, kanban drag (desktop + narrow viewport).
- Coverage: new packages ≥ existing floor (Spec 15 gate); no test skipped
  to pass.

## 8. Future / providers

- WhatsApp BSP choice (Gupshup vs Meta Cloud API) — §11.
- Vahan RC/DL verification API licensing — §11.
- Scale tiering (Postgres + Timescale for positions, read replicas,
  per-tenant ingest rate limits) — separate spec `23-scale-tiering.md`,
  do NOT bake into this one.
- Video telematics / dashcams — behind Spec 17 provider interface, later.
- Driver app voice-note kharcha — after OCR proves out.

## 9. Edge cases

1. Vehicle with no telemetry device → fleet card grey "no GPS", context
   panel still shows trip/kharcha/docs; no map follow.
2. SSE disconnect → console falls back to 30s poll (existing pattern,
   Spec 04); panel state preserved.
3. Offline driver submits kharcha → queued locally, idempotency key
   (@00076) dedupes on sync; OCR runs after sync, not on device.
4. OCR unavailable/timeout → expense stays `manual`, driver not blocked.
5. Advance on driver with unpaid previous advance → allowed but console
   warns (badge); policy cap in company_config (default ₹10k).
6. Alert storm (geofence flood) → existing storm batching (Spec 05) runs
   BEFORE inbox ranking; batch id groups to one row with count.
7. Snoozed alert re-fires same cause → increments existing row, keeps
   snoozed_until (no new row) — dedupe key = (type, vehicle_id, day).
8. Settlement generated while advance pending → pending advances NOT
   deducted (only approved/paid); listed as memo lines.
9. Booking dragged backwards (delivered→assigned) → rejected: status
   machine only allows forward + `cancelled` (Spec 09 history preserved).
10. Multi-tab/two admins ack same alert → second ack 200 but no-op
    (`WHERE ack_status='open'` guard), UI reconciles via SSE.
11. Tenant with 500 vehicles → fleet strip virtualized (render 30,
    search-filter rest); context fetch p95 <300ms target.
12. Driver phone number missing → call button hidden, chat/share still OK.

## 10. Phased rollout — THE step-by-step plan (dependency-ordered)

**Global invariants, every step, no exceptions:** build+vet+tests green ·
migration up/down round-trip · feature flag default OFF · tenant from
context only · RBAC on every route · adapter mocks default (no external
calls in CI) · `Prove It` protocol output at each step's end.

### Step 0 — Preconditions (BLOCKER until green; nothing below starts)
0.1 Commit current dirty working tree (~60 modified files) — clean slate,
    reviewable diffs.
0.2 Spec 10: RAG arbitrary-file-read fix + `/api/rag/*` behind
    `rag:read` (00078 perms exist).
0.3 Spec 09: kill hardcoded TenantID "1" (26+ sites) + unify dual event
    buses; CI grep gates ON.
0.4 Spec 11: Razorpay server-side order + verify.
0.5 Spec 15 baseline gates running in CI (tenant-grep, RAG-auth grep).
**Exit gate:** CI green incl. grep gates; zero `TenantID: "1"` outside
tests. **Rollback:** n/a (fix-forward). **Risk:** scope creep in Phase 0 —
timebox each spec item to its README bullet, park extras in its spec.

### Step 1 — S1 Alert inbox (00092)
Pipeline engine writes severity_rank/money_at_risk/ack_status at emit;
inbox API + partial; console placeholder page.
**Files:** `internal/alerts/pipeline/engine.go` (emit enrich),
`internal/alerts/repository` + sqlite repo, handler `internal/handlers/alerts_inbox.go`,
templates `_alert_inbox.html`, migration 00092, tests.
**Exit gate:** inbox shows ranked live alerts; ack/snooze work; storm-batch
groups. **Rollback:** flag `ALERT_INBOX_ENABLED=false`; migration down.

### Step 2 — S2 Money strip
**Files:** `internal/handlers/console.go` (new), `internal/service/pnl_service.go`
(aggregation method), `_money_strip.html`. No migration.
**Exit gate:** strip matches report totals for seeded month.

### Step 3 — S3 Fleet strip + map + context panel
**Files:** `console.html`, `_fleet_strip.html`, `_context_panel.html`,
`static/js/console.js`, `internal/handlers/console.go` (context endpoint),
service aggregation reading: telemetry latest position, booking/trip,
driver, pnl, kharcha, ewaybill, fastag, doc vault.
**Exit gate:** select vehicle → panel complete <300ms p95; all sections
render for seeded fleet; no page navigations.
**Risk:** N+1 queries → single composite query per service + 60s cache.

### Step 4 — S4 Inline actions
Wire existing kharcha approve/reject, settle, share-link, call; NEW
`POST /api/ewaybill/{id}/extend` behind `EWB_EXTEND_ENABLED` + mock.
**Exit gate:** every panel action executes without leaving `/console`;
audit_log row per action.

### Step 5 — S5 Kanban board
**Files:** `handlers/bookings.go` (board route), `bookings_board.html`,
reuse booking status API + SSE. No migration.
**Exit gate:** drag updates status for second user within 2s (SSE);
backwards drag rejected (edge 9).

### Step 6 — S6 Universal search
Extend existing search handler to the 5 entity types + ⌘K palette.
**Exit gate:** "mh12" returns tenant-scoped hits across types; perm-
filtered. **Risk:** slow LIKE scans → prefix indexes where missing (check
before adding; index changes = new migration? NO — CREATE INDEX inside
existing tables goes in 00092 batch only if needed, else 00095 new slot).

### Step 7 — S7 Driver Paisa tab (00093)
**Files:** `internal/handlers/driver_money.go`, `internal/service/driver_balance_service.go`
(new), migration 00093, mobile `PaisaScreen.tsx` + tab bar swap + API client.
**Exit gate:** driver sees balance matching admin settlement view ±₹1;
advance request→decision→next settlement includes it (integration test).

### Step 8 — S8 Kharcha OCR + auto-flag (00094)
**Files:** `internal/integration/ocr/` (adapter+mock+http), async verifier
subscribing expense-created events (bus from Step 0.3), `verification_state`
writes, flagged queue template rework, mobile OCR-confirm step.
**Exit gate:** rule table tests green; flagged queue shows evidence map
pin; auto_verified rate on pilot data reported.

### Step 9 — S9 Compliance radar
**Files:** `internal/service/compliance_radar_service.go`, nightly sweep
worker (leader-elected — 00079 leases), radar template + panel section,
Vahan adapter (`integration/vahan/`, mock default).
**Exit gate:** seeded docs fire at 30/7/1; EWB fires 12/4h; one-tap extend
wired from inbox rank-1 row.

### Step 10 — S10 WhatsApp channel
**Files:** `internal/alerts/channels/whatsapp.go` (+mock), prefs UI option,
`notification_log` delivery rows, templates for top-5 alerts (rank 1–3 only
by default).
**Exit gate:** mock send logged; rank-4/5 never WhatsApp by default.

### Step 11 — S11 PWA + responsive console
Spec 12 items: service worker, manifest, bottom-sheet layout, offline
shell. **Exit gate:** Lighthouse PWA pass; console usable at 360px.

### Step 12 — S12 Instrumentation + KPI review
Event on: console open, panel action, ack latency, kharcha auto-verify
rate, advance turnaround. KPI targets (2-week pilot): settlement cycle
<10min, kharcha app-submitted ≥90%, disputed settlements <5%, driver WAU
≥80%, EWB expiry caught 100%.

### Step 13 — Flag flip + cleanup
Enable flags per tenant (feature_flags @00089) → console becomes default
landing. Old dashboard kept at `/dashboard-classic` for 30 days, then
removed (templates pruned, routes retired).

### Step 14 — Scale spec handoff
Write `23-scale-tiering.md` (Postgres/Timescale plan) — out of scope here.

## 11. Open items / VERIFY at implementation

1. **WhatsApp BSP** — Gupshup vs Meta Cloud API: pricing/message unit,
   template approval lead time. DECIDE before S10 build.
2. **OCR** — local Tesseract (license ok, Hindi numeral accuracy?) vs HTTP
   API (cost/latency). DECIDE before S8 build; mock ships either way.
3. **Vahan data** — official API partnership vs aggregator; licensing
   cost. Mock until decided; radar works without it.
4. **drivers.id / trips.id key format** — confirm before any FK thought
   (default: no FK, §3 note).
5. **Index anomaly** — ownership index reserves 00068–00072 + 00085 with
   NO migration files on disk; specs 18/19/20 files also absent from
   docs/tech-specs/. Confirm intentional (reserved/unbuilt) vs index rot;
   fix index prose if rot. Do NOT reuse numbers.
6. **`bin/rag` auth broken** — teach attempt returns `401 api token
   invalid` (binary exists, token stale). Fix token before relying on RAG
   teaching; spec content is in-repo regardless.
7. **Severity constants** — idle LPH, EWB penalty, fuel price defaults:
   propose in company_config seed, owner-editable, VERIFY with pilot fleet
   numbers.
8. **Alert dedupe key** — (type, vehicle_id, day) may over-collapse for
   repeat SOS; VERIFY per-type exceptions during S1 tests.

## 12. File list

CREATE: `db/migrations/00092_alert_inbox.sql`,
`00093_driver_advances.sql`, `00094_kharcha_verification.sql`,
`internal/handlers/console.go`, `internal/handlers/alerts_inbox.go`,
`internal/handlers/driver_money.go`,
`internal/service/driver_balance_service.go`,
`internal/service/compliance_radar_service.go`,
`internal/integration/ocr/{client,mock,http}.go`,
`internal/integration/vahan/{client,mock}.go`,
`internal/alerts/channels/whatsapp.go`,
`internal/templates/console.html`, `_fleet_strip.html`,
`_context_panel.html`, `_alert_inbox.html`, `_money_strip.html`,
`bookings_board.html`, `static/js/console.js`,
`mobile/src/screens/PaisaScreen.tsx`, `mobile/src/screens/DocsScreen.tsx`,
plus `_test.go` beside every new Go file.

MODIFY: `internal/alerts/pipeline/engine.go`, alerts sqlite repo,
`internal/handlers/app.go` (routes/landing), `bookings.go`, `search.go`,
`internal/service/pnl_service.go`, `kharcha_service.go`,
`internal/integration/ewaybill/client.go` (extend op),
`internal/config/config.go`, `cmd/server/main.go` (wiring),
`mobile/src/navigation` (tab bar), `docs/tech-specs/00-migration-ownership-index.md`
(done — this spec reserved), `docs/tech-specs/README.md` (done).

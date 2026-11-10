# A6: OpenAPI↔router parity audit

> CONTRACT STATUS: `openapi.yaml` is a HAND-MAINTAINED snapshot — no generator
> exists (no Makefile/scripts/CI target emits it). Treat it as the human-curated
> contract: every new `/api/v1` route ships with a `paths:` entry in the same
> PR (rule below), and this audit re-runs by hand re-executing the extraction
> method. Do NOT auto-generate it without a spec decision (mock-shaped
> integrations would get pinned as contract).

Date: 2026-09-08. Method: static extraction of every `.(Get|Post|Put|Delete|Patch)("...")`
and `r.Route("/api/v1...", func...)` block from non-test Go, normalized `{param}`→`{}`,
diffed method-aware against `openapi.yaml` `paths:`. (Case-only diffs ignored.)

## Result
- Router: **128** `/api/v1` paths. Spec: **38** paths.
- **In spec, no route: 0** — everything documented exists. The spec never lies.
- **In router, not in spec: 90** — the B12 backlog, grouped below.

## Gap by domain (B12 work order proposal)
1. **Drivers/me cluster (15):** `drivers/me`, `/status`, `/issues`, `/license`,
   `/offers`, `/onboarding`, `/wallet`, `/verification/submit`, `/vehicle-claims`,
   `/payout-account`, `/payouts`, `/documents`, `/commands`, `/push-token`,
   `drivers/onboarding/funnel`, `drivers/{}/verify`,
   `drivers/{}/vehicle-assignments`, `drivers/{}/vehicle-claims/{}/verify`,
   `driver/push-token`, `drivers/push-token` — the entire mobile driver contract
   is undocumented. Highest B12 priority (mobile is the operational backbone).
2. **Integrations cluster (20):** all `/api/v1/integrations/*` (einvoice, GSTR,
   EWB part-A/B, cancel/extend, journal, reconcile) — mock-by-default providers;
   spec them when providers go real, not before (else the spec pins mock shapes).
3. **Telemetry (10):** `/live`, `/history`, `/playback`, `/stream`, `/geofences`,
   `/events`, `/reverse_geocode`, `/sessions/start|end`, `devices/{}/gps` —
   freshly hardened this cycle; spec-worthy now.
4. **Trip stops & views (6):** `trips/{}/stops/{reach,complete,pod}`,
   `trips/{}/summary|playback|deliver-pod`, `driver/trips/{}/stops/{}/pod`.
5. **Work orders (4):** `/work-orders`, `/{}/assign|transition`, `/{}`.
6. **Auth (4):** `otp/send|verify`, `forgot|reset-password`.
7. **Ops/misc (rest):** `routes/optimize*`, `settlements/calculate`,
   `customer/*`, `control-tower/*`, `kharcha/expense`, `outbox/batch`, `sos`,
   `hsn-sac/search`, `compliance/dashboard`, `users/me/preferences`,
   `vehicle-assignments/{}/accept`, webhooks (`billing`, `payouts`,
   `payments/razorpay*`).

## Re-audit 2026-09-11 (B12 closure)

Method: same method-aware extraction (direct `.Verb("/api/v1...")` incl.
chained `r.With(...).Verb`, `r.Route` prefix + relative verbs, nested routes,
`Mount` closure-variable resolve), non-test Go (`internal/`, `cmd/server/main.go`),
`{param}`→`{}`, trailing-slash normalized.

## Result
- Router: **229** `/api/v1` ops. Spec: **214** ops (`openapi.yaml`: 186 paths, re-audit 2026-09-13).
- **In router, not in spec: 20** — all `/api/v1/integrations/*` (ewaybill,
  fastag, gstn, accounting stubs in `internal/integration/handler.go`).
  Deliberately deferred per above (mock-by-default; spec when providers go real).
- **In spec, no v1 route: 0** — the spec never lies. (5 apparent dead entries
  are `/api/driver/*` non-v1 twins whose routes exist in `cmd/server/main.go`;
  out of v1-spec scope by design.)
- B12 closure criterion amended: router∖spec = integrations-deferred-only. ✅

## Rule going forward
New `/api/v1` routes ship with an `openapi.yaml` `paths:` entry in the same PR
(B12 closure criterion per route). This audit re-runs by re-executing the
extraction script above; target for B12-done is: router∖spec = ∅.


## Re-run 2026-11-10 (automated, head v185)

Method: `python3 scripts/openapi-parity.py` — regex extraction of
`paths:`/method keys from `openapi.yaml` vs Go route registrations
(method-call regex, `Route("prefix", func...)` prefix stack, and expansion of
`Route("prefix", setupFunc)` closures). Params normalized to `{}`; scope
is `/api/v1/*`. The script exits 1 if the spec documents a route the router
does not register, and CI now runs it in `backend-build`.

### Result
- Spec: **207 paths / 236 path+method entries**. Router: **358 method registrations**.
- **Dead spec entries: 0** (after fixing real drift found by this audit).
- **Undocumented router routes: 122** (B12 backlog), top domains:
  `integrations` 20, `tyres` 13, `video` 13, `inventory` 12, `rfq` 11,
  `cold-chain` 10, `report-builder` 10, `freight-audit` 8, `ev` 7,
  `loadboard` 4, `kharcha` 3, `sto` 3.

### Drift fixed this run
- Added `GET /api/v1/drivers/me/payouts` (masked account + rail state + payout
  history; encrypted account number never returned) and
  `POST /api/v1/drivers/me/payout-account/verify` (penny-drop start).
- Payout webhook spec: `X-Razorpay-Signature` is now documented as required
  (the handler rejects unsigned callbacks).
- Found and fixed a real spec lie: the `/api/v1/errors/{fingerprint}` path
  header was missing, so its `get:` block sat under
  `/api/v1/support/tickets/{ticket}/status` (parser saw a dead GET). Path restored.

### Known limitations of the audit
- Routes mounted dynamically outside `RegisterRoutes`-style helpers are not seen.
- Only `/api/v1/*` is in scope (legacy `/api/*` routes excluded).
- Undocumented count is a backlog metric, not a failure; keep the rule "new
  `/api/v1` route ships with a spec entry in the same PR" until B12 closes.

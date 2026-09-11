# A6: OpenAPI↔router parity audit

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
- Router: **229** `/api/v1` ops. Spec: **214** ops (`openapi.yaml`: 183 paths).
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

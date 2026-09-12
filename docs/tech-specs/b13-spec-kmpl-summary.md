# B13: KMPL SOP Report Set (ZMOTM_MR p.19 follow-up)

- **Migration Slot:** `00152` (`vehicles.standard_kmpl REAL NULL, CHECK > 0 when set`; PG port in `migrations_pg/`)
- **Owner:** Domain: `internal/handlers/zmotm_reports.go` (summary) | Norm: vehicle stack (`standard_kmpl` = vehicle attribute like `fuel_type`)
- **Routes:** `GET /api/v1/reports/kmpl-summary`, `GET /reports/kmpl-summary.csv`

## 1. Gap (why B5 was partial)

B5 shipped the per-fill Fuel & KMPL report (tank-to-tank rows per `fuel_issues`),
but the SOP report set a fleet manager opens is per-vehicle × period with a norm
to compare against. Two gaps closed here:

1. **No norm store:** no `standard_kmpl` anywhere — variance/pilferage flagging impossible.
2. **Pagination bug:** `prevOdoMap` was built per page, so every page-2+ row per
   vehicle wrongly showed blank KMPL. Fixed by seeding from the latest pre-page
   odometer per vehicle (one bounded query; max odo breaks timestamp ties).

## 2. Method (tank-to-tank, per vehicle × month)

- Period `[month-01, next-month-01)`, UTC. `?month=YYYY-MM` (default current month),
  optional `?vehicle_id=`.
- Opening odometer = latest pre-period fill with odo > 0. Without one, the first
  in-period fill with odo > 0 opens (its litres excluded — they belong to prior
  consumption).
- `distance = last_odo − opening_odo` (must be > 0), `fuel = Σ litres` after the
  opening fill, `KMPL = distance / fuel` (fuel > 0). Fills with NULL/zero odo are
  skipped for distance but counted in `fills`.
- `variance_pct = (kmpl − norm) / norm × 100`; `flag = BELOW_NORM` when norm is set
  and actual < norm. Vehicles with fills but no computable KMPL are still listed
  (NULL metrics) — visibility over silence.
- Norm editing: `PUT /api/v1/vehicles/{id}` / HTML vehicle form (`standard_kmpl`
  field); nil preserves on API, blank clears on HTML form. Non-positive rejected
  with a friendly 400 (DB CHECK is the backstop, never the user-facing error).

## 3. Acceptance

- `TestKMPLSummary_MathVarianceAndIsolation`: 500 km / 110 L = 4.5454 vs norm 5.0
  → −9.09% BELOW_NORM; no-norm / single-fill / isolation / filter / bad-month cases.
- `TestKMPLSummary_CSV`: headers + formatted row.
- `TestFuelKMPL_PaginationCarryover`: red-proven (fails with seeding neutered).
- `TestMigration00152_StandardKmplColumn`: column present, CHECK rejects 0/negative,
  down drops / up restores.
- `TestVehicleAPI_StandardKmplRoundTrip`: create/get/PUT-preserve/PUT-change/400s.

# Fleet Registry — TMS SOP Parity (Tech Spec)

- Status: approved for implementation
- Date: 2026-09-05
- Source: `TMS_SOP.pdf` (CEPT IPVS, 04-Feb-2021) — all 20 pages image-verified (`pdftoppm` render; text extraction misses SAP screenshots)
- Decisions (locked 2026-09-05): unified fleet object / full measuring model / consolidate domains / tech-spec doc
- Migration slot: `00126` (disk head `00125_tenant_company_profiles.sql`; next free `00126_*.sql` per append-only rule)

## 1. Goal / non-goals

Goal: `vehicles` registry reaches SOP fleet-object parity: own + contractual vehicles plus fuel stations in one catalog, with org data, master-spec fields, and counter-based measuring points/documents.

Non-goals (follow-ups, not this spec): trip start/close `ZMOTM_MMS` (pp.6-8), fuel issue entry (p.9), maintenance plans `IP41` / notifications `IW28` (pp.10-15), reports `ZMOTM_MR` Gate/Fuel/KMPL/Breakdown (pp.16-20). Integration notes for those specs (from screenshots): Trip Close dialog writes close reading/date/time + has Breakdown button → outstanding notification (p.8 scr, p.15 text); fuel-station OP/CL pump readings map to `PUMP` measuring points and station expiry maps to `valid_to` (p.9 scr); Gate Register needs start/close readings per trip (p.18 scr).

## 2. SOP mapping

| SOP concept | SOP ref | Target |
|---|---|---|
| Fleet object lifecycle `IE31/IE32/IE33`, Equipment ID = reg number | p.2 | `vehicles.registration_number` stays canonical ID; `vehicle_number` display alias only; Create/Update handlers = IE31/IE32 equivalent |
| Vehicle type `CV/PV/FS/OFS` | p.2 | New `vehicles.fleet_class TEXT CHECK (CV,PV,FS,OFS)`; keep existing `vehicle_type` (truck/bus/…) as body type |
| Equipment category `O/C/F/M` | p.2 | New `vehicles.ownership TEXT CHECK (O,C,F,M)` |
| General tab (p.3 scr): description, manufacturer, manuf country, model no., constr yr/mth, acquis value/date, valid from/to, status | p.3 | New `description, manufacturer, manuf_country, model, constr_year_month, acquisition_value/currency/date, valid_from/valid_to`; status `AVLB` maps to `available` |
| Organizational tab (p.3 scr, dept only): MaintPlant + Planning plant (MMS Office), Company Code, Business Area (Circle), Cost Center, Asset | p.3 | New `facility_id` (Office ID, e.g. `MM21000000757` — also keys Trip Start/Close pp.7-8) + `maint_plant, planning_plant, company_code, business_area, cost_center, asset_no` (all nullable; NULL ok for contractual `C`) |
| Vehicle Details tab (p.4 scr): fleet object no., chassis no., ValidityEndDate, vehicle category, insurance/PUC/fitness valid-to, engine serial/power/capacity/cylinders/max speed | p.4 | Expiries already exist; new `fleet_object_no, chassis_no` (moved here — screenshot location, not General), `vehicle_category, engine_power, engine_capacity, cylinder_count, max_speed`; `ValidityEndDate` = `valid_to` |
| Vehicle Technology tab (p.4 scr): weight + unit, max load, load volume + unit, primary/secondary fuel, usage indicator | p.4 | `capacity` exists (repurposed as max load where apt); new `weight, weight_unit, load_volume, volume_unit, secondary_fuel, usage_indicator` |
| Vehicle Master report exact columns (p.18 scr): Vehicle No, Type, Category, Manufacturer, Model, Purchase Date/Value, Currency, Fuel Type, Fleet Number, Chassis No, Engine SNo | p.18 | All covered after this spec. ⚠️ label swap: report "Vehicle Type" = ownership (`O`), "Vehicle Category" = fleet class (`CV`) — opposite of IE31 screen labels. New `fleet_number TEXT` (serial 35/104/…; distinct from reg number) |
| Measuring points `IK01/02/03` (p.5 scr: MeasPosition `DISTANCE`, characteristic + km unit, decimal places, `AnnualEstimate 50000`, count-backwards, counter-over-reading) | pp.4-5 | New `vehicle_measuring_points` table; `annual_estimate` feeds maintenance-plan scheduling (p.10: plan date derives from it) |
| Measuring documents `IK11` (p.6 scr: doc no. `1197`, MeasurementTime, Read by, counter + difference + TotalCtrl reading, long text) | pp.5-6 | New `vehicle_measurements` table with `measured_at, read_by, total_counter_reading, remarks` |
| Facility ID lookup `ZFID` (`MM`, p.1 scr) | p.1 | `facility_id` free-form TEXT this spec; ZFID sync = follow-up. Facility master columns observed (p.2 scr: description, facility ID, valid from, plant, profit/cost center + name, circle) define that follow-up's table — not built here. Missing-facility process (request to CEPT with facility master template) stays manual |
| Vehicle Master `ZMOTM_MR` (manufacturer, model, reg, engine, purchase) | p.17-18 | Master-spec columns above make report possible; exact p.18 column list = acceptance test |
| Structure tab (visible pp.3-4, no SOP content) | — | Explicitly excluded — SAP functional-location hierarchy, no parity target |
| Create copy-frame (p.3 initial scr: copy from Reference Equipment/Material) | p.3 | Handler nice-to-have: `?copy_from=<reg>` prefills Create form; not migration work |

## 3. Current state (verified 2026-09-05)

- Table: `db/migrations/00003_vehicles.sql:2` base + `00018 tenant_id`, `00021 version`, `00030 blocked/blocked_reason/rc_expiry/odometer`, `00042 tank_capacity_litres/fuel_sensor_fitted/maintenance_due`, `00044 maintenance_override_*`, `00046 puc_expiry`.
- New aggregate `internal/vehicle/domain/aggregate/vehicle_aggregate.go:36` lacks blocked/RC/PUC/odometer; legacy `internal/domain/vehicle/entity.go:11` has them plus `CanAssign()` compliance block (`:68`).
- Handlers `internal/handlers/vehicles.go:43` use new UCs except `Delete (:365)` uses legacy service.
- sqlc `db/query/vehicles.sql:1` selects base columns only; ignores blocked/RC/PUC/odometer/maintenance.
- `status` CHECK lacks `blocked` (entity defines `VehicleBlocked`, DB rejects it) — widens in `00126` via rebuild (see §4.3).
- Templates: `vehicle_edit.html:1` lacks RC/PUC/odometer/ownership inputs; `vehicle_view.html:65` doc strip present. `openapi.yaml` has zero `/vehicles` paths (only `trips/{id}/assign-vehicle`).

## 4. Schema — migration `00126_fleet_registry_sop_parity.sql`

Rules: ONE feature = ONE number; `-- +goose Up/Down`; tenant triggers per `00-migration-ownership-index.md:14` + `00124` pattern; fail-closed `WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''`; never edit old migrations.

### 4.1 `vehicles` new columns (all nullable/DEFAULTed — zero backfill breakage)

```sql
-- Identity / SOP classification (p.2 + p.18 report: Type=O is ownership, Category=CV is fleet class)
ALTER TABLE vehicles ADD COLUMN fleet_class TEXT NOT NULL DEFAULT 'CV'
  CHECK (fleet_class IN ('CV','PV','FS','OFS'));
ALTER TABLE vehicles ADD COLUMN ownership TEXT NOT NULL DEFAULT 'O'
  CHECK (ownership IN ('O','C','F','M'));
ALTER TABLE vehicles ADD COLUMN fleet_number TEXT; -- Vehicle Master serial (35/104/...), distinct from reg no.
-- General tab (p.3 screenshot)
ALTER TABLE vehicles ADD COLUMN description TEXT; -- e.g. 'TATA TRUCK'
ALTER TABLE vehicles ADD COLUMN manufacturer TEXT;
ALTER TABLE vehicles ADD COLUMN manuf_country TEXT; -- e.g. 'IN'
ALTER TABLE vehicles ADD COLUMN model TEXT;
ALTER TABLE vehicles ADD COLUMN constr_year_month TEXT; -- e.g. '2018'
ALTER TABLE vehicles ADD COLUMN acquisition_value REAL; -- AcquisnValue 70,000.00
ALTER TABLE vehicles ADD COLUMN acquisition_currency TEXT NOT NULL DEFAULT 'INR';
ALTER TABLE vehicles ADD COLUMN acquisition_date DATE; -- Acquisiton date 06.08.2018
ALTER TABLE vehicles ADD COLUMN purchase_vendor TEXT; -- extension, no SOP basis
ALTER TABLE vehicles ADD COLUMN valid_from DATE;
ALTER TABLE vehicles ADD COLUMN valid_to DATE; -- = ValidityEndDate (p.4)
-- Organization tab (p.3 screenshot, dept vehicles only; NULL ok for contractual)
ALTER TABLE vehicles ADD COLUMN facility_id TEXT; -- Office ID e.g. MM21000000757
ALTER TABLE vehicles ADD COLUMN maint_plant TEXT; -- e.g. 'MMS Office - Bangalore'
ALTER TABLE vehicles ADD COLUMN planning_plant TEXT;
ALTER TABLE vehicles ADD COLUMN company_code TEXT; -- e.g. DOP1 Dept of Post India
ALTER TABLE vehicles ADD COLUMN business_area TEXT; -- e.g. 1013 Karnataka Circle
ALTER TABLE vehicles ADD COLUMN cost_center TEXT; -- e.g. 2111000000 Bangalore MMS
ALTER TABLE vehicles ADD COLUMN asset_no TEXT;
-- Vehicle Details tab (p.4 screenshot)
ALTER TABLE vehicles ADD COLUMN fleet_object_no TEXT; -- EIR... (distinct from fleet_number)
ALTER TABLE vehicles ADD COLUMN chassis_no TEXT;
ALTER TABLE vehicles ADD COLUMN vehicle_category TEXT; -- e.g. '1'
ALTER TABLE vehicles ADD COLUMN engine_number TEXT; -- EngineSerialNo.
ALTER TABLE vehicles ADD COLUMN engine_power TEXT;
ALTER TABLE vehicles ADD COLUMN engine_capacity TEXT;
ALTER TABLE vehicles ADD COLUMN cylinder_count INTEGER;
ALTER TABLE vehicles ADD COLUMN max_speed REAL;
-- Vehicle Technology tab (p.4 screenshot)
ALTER TABLE vehicles ADD COLUMN weight REAL;
ALTER TABLE vehicles ADD COLUMN weight_unit TEXT NOT NULL DEFAULT 'TO' CHECK (weight_unit IN ('TO','KG')); -- UoM evidence p.20 scr (6.000 TO, 2.000 KG)
ALTER TABLE vehicles ADD COLUMN load_volume REAL;
ALTER TABLE vehicles ADD COLUMN volume_unit TEXT;
ALTER TABLE vehicles ADD COLUMN secondary_fuel TEXT;
ALTER TABLE vehicles ADD COLUMN usage_indicator TEXT; -- e.g. 'M' (p.4 scr)
```

Seed rule: existing rows default `CV/O`; `FS/OFS` rows arrive via Create only. Contractual (`C`) rows may leave `facility_id` NULL; dept (`O`) rows should set it.

### 4.2 Measuring tables (full `IK01/IK11` parity)

```sql
CREATE TABLE vehicle_measuring_points (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  vehicle_id TEXT NOT NULL,
  category TEXT NOT NULL DEFAULT 'M', -- MeasPtCategory (p.4)
  kind TEXT NOT NULL CHECK (kind IN ('ODO','FUEL_TOPUP','PUMP')),
  -- SAP characteristic mapping (p.12 scr): ODO -> meas_position 'DISTANCE', fuel top-up -> 'FUELTOPUP' (unit KM both)
  meas_position TEXT NOT NULL DEFAULT 'DISTANCE', -- MeasPosition (p.5 scr)
  unit TEXT NOT NULL DEFAULT 'KM' CHECK (unit IN ('KM','L')), -- CharacterUnit km
  decimal_places INTEGER NOT NULL DEFAULT 0,
  annual_estimate REAL, -- AnnualEstimate 50000; feeds IP41 plan scheduling (p.10)
  count_backwards INTEGER NOT NULL DEFAULT 0,
  is_counter INTEGER NOT NULL DEFAULT 1,
  description TEXT NOT NULL DEFAULT '', -- e.g. 'Tata Truck KA01KA0123'
  created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_meas_points_tenant_vehicle ON vehicle_measuring_points(tenant_id, vehicle_id);

CREATE TABLE vehicle_measurements (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  point_id TEXT NOT NULL REFERENCES vehicle_measuring_points(id),
  doc_number TEXT, -- MeasDocument no. e.g. 1197 (p.6 scr)
  counter_reading REAL NOT NULL, -- e.g. 1200
  difference_reading REAL, -- Difference
  total_counter_reading REAL, -- TotalCtrlReading e.g. 1200
  measured_at DATETIME, -- MeasurementTime 08.04.2015/14:13:22
  read_by TEXT, -- e.g. TCS795488
  remarks TEXT, -- Long text
  recorded_at DATETIME NOT NULL DEFAULT (datetime('now')),
  recorded_by TEXT
);
CREATE INDEX idx_measurements_point_time ON vehicle_measurements(point_id, recorded_at);
```

Counter rule: `counter_reading` monotonic per point (enforce in UC, not DB — SQLite cross-row CHECK impractical). `difference_reading` computed at write (= current − last) to match `IK11`.

Tenant triggers: insert/update FK triggers on both tables mirroring `db/migrations/00124_work_orders_tenant_fk.sql:9-26`. Down: drop triggers, drop `vehicle_measurements`, drop `vehicle_measuring_points`, drop new `vehicles` columns (SQLite: columns drop supported; status CHECK handled in §4.3).

### 4.3 `status` CHECK widen (`blocked`)

SQLite cannot `ALTER CHECK`. Two options — doc mandates option A:

- A (mandated): table rebuild in `00126` (`vehicles_new` with full CHECK incl. `blocked`, copy, drop, rename, reindex, retrigger). Single migration, tested `up/down`.
- B (rejected): trigger-based status validation — leaves schema lying about allowed values.

## 5. Domain consolidation (locked)

`internal/vehicle` canonical. Steps:

1. `vehicle_aggregate.go:15-53`: add `FleetClass, Ownership, FleetNumber, Description, Manufacturer, ManufCountry, Model, ConstrYearMonth, AcquisitionValue/Currency/Date, PurchaseVendor, ValidFrom/To, FacilityID + org fields (MaintPlant, PlanningPlant, CompanyCode, BusinessArea, CostCenter, AssetNo), FleetObjectNo, ChassisNo, VehicleCategory, EnginePower/Capacity, CylinderCount, MaxSpeed, Weight/WeightUnit, LoadVolume/VolumeUnit, SecondaryFuel, UsageIndicator, Blocked/BlockedReason, Odometer, RCExpiry, PUCExpiry`; add `VehicleBlocked = "blocked"`; extend `NewVehicleAggregate` + `UpdateDetails (:110)`.
2. `internal/vehicle/domain/repository.go:12` + `infrastructure/persistence/sql/converters/vehicle_converter.go:13`: map new fields both directions.
3. `internal/vehicle/application/create_vehicle.go:14`, `update_vehicle.go:14`, `get_vehicle.go:14`, `facade.go:12`: extend commands + `VehicleResponseDTO` (measuring history via separate query, not DTO bloat).
4. New `application/record_measurement.go` UC: monotonic-counter guard + outbox event `MeasurementRecorded`.
5. Legacy `internal/domain/vehicle/service.go` + `internal/service/vehicle_service.go:12` become thin delegates to facade; `handlers/vehicles.go:365 Delete` switches to UC; `CanAssign() (entity.go:68)` moves to aggregate method (keep legacy re-export for one release). SOP dispatch rule preserved: IW28 In-Process notification ⇒ vehicle unavailable for Trip Start (p.13) = our maintenance/dispatch block.
6. sqlc `db/query/vehicles.sql:1` regen to include new columns; add `db/query/vehicle_measurements.sql` (points + measurements CRUD scoped by `tenant_id`).

## 6. Handlers / templates / API

- `internal/handlers/vehicles.go`: `Create (:102)` / `Update (:308)` parse new fields; remove silent `now+1yr` default on bad date (fail `400` instead); `List (:54)` adds `fleet_class/ownership` filters; `View (:159)` adds org block + measuring history (last 20).
- Templates: `vehicle_edit.html` gains grouped fieldsets mirroring SAP tabs (General / Organization / Vehicle Details / Vehicle Technology) + `?copy_from=<reg>` prefill; `vehicle_view.html` org + measuring sections (points with annual estimate, last 20 docs); list filter chips `CV/PV/FS` + ownership.
- `openapi.yaml`: add `/vehicles` GET/POST + `/vehicles/{id}` GET/PUT/DELETE (currently absent); measuring endpoints `/vehicles/{id}/measurements` POST/GET.

## 7. Tests (ship with code — `_test.go` mandatory)

- Aggregate: new-fields round-trip, `blocked` status const, `CanAssign` RC/Fitness/Insurance/PUC expiry (extend `internal/domain/vehicle/entity_test.go` PUC gap).
- UC: create/update validation error, tenant isolation, measurement monotonicity reject.
- Repo: save/find with new cols, `SearchReadModels` tenant + fleet_class filter, closed-DB errors (follow `vehicle_repository_test.go` pattern).
- Handlers: extend `vehicles_selected_test.go` CRUD + auth + pagination for new filters.
- Migration: `goose up/down` applies and rolls back `00126` on throwaway SQLite.

## 8. Rollout order

1. Land this doc (no code).
2. `00126` migration + index-table row (`00126 | fleet registry SOP parity | this spec`).
3. Aggregate → converters → sqlc regen → UCs → legacy delegation.
4. Handlers/templates/openapi.
5. Tests + Prove-It (§9) + security gate.

## 9. Prove-It (mandatory before claiming done)

```bash
go build ./...
go vet ./...
go test ./internal/...
LINT_BASE=$(git rev-parse HEAD) ./scripts/security-check.sh
```

New code ships ZERO new gosec/errcheck findings; migration applies AND rolls back; response ends with Agent Verification Report quoting this spec section (§4/§5).

## 10. Implementation status (2026-09-05)

Built:
- `00126` + up/down proof (`db/migrations_00126_test.go`) + index row.
- Aggregate `VehicleProfile`/`ApplyProfile`, `Create/Update/Delete` + `CreateMeasuringPoint`/`RecordMeasurement`/`ListVehicleMeasurements` UCs, sqlc regen, repo + converters.
- JSON API `internal/vehicle/presentation/api/handlers/` mounted in `cmd/server/main.go` (`/api/v1/vehicles`, `/{id}`, `/{id}/points`, `/{id}/measurements`) matching `openapi.yaml`.
- HTML handlers/templates (SAP-tab form, strict 400 dates, class/ownership filters, Fleet Object + IK01/IK11 view sections).

Deferred (explicit, not forgotten):
- Legacy delegation (§5 step 5): `internal/domain/vehicle/service.go` + `internal/service/vehicle_service.go` untouched; HTML `Delete` still uses the legacy service (keeps its audit + conflict behavior). New-stack `DeleteVehicleUseCase` serves the JSON API.
- `CanAssign` stays legacy-only (`internal/domain/vehicle/entity.go:68`); no aggregate duplicate.
- RC/PUC/odometer form inputs pre-date this spec and stay out (new-stack writes never clobber them; legacy raw UPDATE preserves profile columns by construction).
- Facility master table (ZFID sync) stays a follow-up; `facility_id` is free-form TEXT.

Implementation notes:
- sqlc v1.31.1 corrupts statements after non-ASCII bytes: keep `db/query/` pure ASCII (warning comment in `vehicle_measurements.sql`).
- Measuring writes use tx-aware `execTx` (never bypass an open UoW tx — shared-cache SQLite deadlocks otherwise).
- JSON PUT merges over stored values (nil profile / empty dates preserved, never clobbered).
- 00126 runs with `-- +goose NO TRANSACTION` + `PRAGMA foreign_keys=OFF/ON` around the work and ends OFF: vehicles is a referenced parent (trips.vehicle_id et al.) under app-wide FK enforcement, so a transactional DROP fails with SQLITE_CONSTRAINT_FOREIGNKEY (caught by live boot against real data, not by the empty-chain test). Ending ON would flip one pooled handle and break FK-oblivious seeds (21 pkgs red); ending OFF restores ambient default and the server boot re-establishes posture post-migration (`ApplySQLitePragmas` in `internal/database`, re-applied in `cmd/server/main.go` after `provider.Up`). No existing test needed changes.
- Live-DB proof (2026-09-06, copy of dev `transport.db`): boot auto-applied 124→126, both vehicle rows preserved with CV/O defaults, `foreign_key_check` 61 before = 61 after (pre-existing legacy debt, zero added), all new API routes answer 401 (mounted + gated), HTML boots.

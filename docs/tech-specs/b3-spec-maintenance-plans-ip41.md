# Spec 04 §14: Maintenance Plans IP41 Parity & Annual Estimate Scheduling (B3)

- **Migration Slot:** `00143` (Allocated in ownership index)
- **Owner:** Domain: `internal/maintenance` & `internal/handlers/maintenance_plans.go` | Route: `/api/v1/maintenance/plans/*`
- **Status:** Approved Spec (Roadmap B3)

## 1. Problem Statement & Operational Rationale
In commercial fleet operations (TMS SOP pp.10-15), vehicle maintenance is governed by Single Cycle Maintenance Plans (SAP PM transaction `IP41`). Fleets define periodic maintenance intervals by distance (e.g. engine oil every 10,000 km) and/or calendar duration (e.g. general inspection every 180 days).

Instead of waiting for an odometer threshold to be breached reactively, fleets forecast service dates using the vehicle's `annual_estimate` (configured on the measuring point in `00126`, e.g. 50,000 km/year). By dividing the annual estimate by 365, the scheduler calculates daily fleet utilization (~137 km/day) and projects the precise date when the next service will fall due.

When the vehicle reaches the configured **Call Horizon** (e.g. 90% or 100% of the maintenance cycle), the plan automatically generates an open Job Card (`work_orders`, `00123`), ensuring maintenance bays, parts, and labor are scheduled in advance before catastrophic breakdown.

## 2. Schema Architecture (`00143`)

### SQLite (`db/migrations/00143_maintenance_plans_ip41.sql`)
```sql
CREATE TABLE IF NOT EXISTS maintenance_plans (
    id                    TEXT PRIMARY KEY,
    tenant_id             TEXT NOT NULL,
    plan_number           TEXT NOT NULL,
    vehicle_id            TEXT NOT NULL REFERENCES vehicles(id),
    measuring_point_id    TEXT REFERENCES vehicle_measuring_points(id),
    service_type          TEXT NOT NULL,
    description           TEXT NOT NULL DEFAULT '',
    cycle_interval_km     REAL,
    cycle_interval_days   INTEGER,
    call_horizon_percent  REAL NOT NULL DEFAULT 100.0,
    last_scheduled_km     REAL,
    last_scheduled_date   DATETIME,
    next_due_km           REAL,
    next_due_date         DATETIME,
    status                TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
    created_at            DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at            DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE(tenant_id, plan_number)
);

CREATE INDEX IF NOT EXISTS idx_maint_plans_tenant_vehicle ON maintenance_plans(tenant_id, vehicle_id);
CREATE INDEX IF NOT EXISTS idx_maint_plans_tenant_status  ON maintenance_plans(tenant_id, status);

-- Tenant FK triggers (00103 standard)
CREATE TRIGGER IF NOT EXISTS trg_maint_plans_tenant_fk_insert
BEFORE INSERT ON maintenance_plans
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for maintenance_plans.tenant_id') END;
END;

CREATE TRIGGER IF NOT EXISTS trg_maint_plans_tenant_fk_update
BEFORE UPDATE OF tenant_id ON maintenance_plans
FOR EACH ROW WHEN NEW.tenant_id IS NOT NULL AND NEW.tenant_id != ''
BEGIN
  SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM tenants WHERE id = NEW.tenant_id)
  THEN RAISE(ABORT, 'FK violation: tenants(id) missing for maintenance_plans.tenant_id') END;
END;

ALTER TABLE work_orders ADD COLUMN plan_id TEXT REFERENCES maintenance_plans(id);
ALTER TABLE work_orders ADD COLUMN due_km REAL;
```

## 3. Projection & Scheduling Algorithm

1. **Daily Rate Derivation:**
   - From linked measuring point (`vehicle_measuring_points.annual_estimate`, default 0):
     $$\text{DailyRate} = \frac{\text{AnnualEstimate}}{365.0}$$
2. **Next Due Target:**
   - Target KM:
     $$\text{NextDueKM} = \text{BaseKM} + \text{CycleIntervalKM}$$
     where $\text{BaseKM} = \text{LastScheduledKM}$ if present, else vehicle current odometer.
   - Projected Date from KM:
     $$\Delta\text{KM} = \text{NextDueKM} - \text{CurrentOdometer}$$
     $$\text{DaysToDue} = \frac{\Delta\text{KM}}{\text{DailyRate}}$$
     $$\text{ProjectedKMDate} = \text{Now} + \text{DaysToDue}$$
   - Calendar Due Date:
     $$\text{CalendarDueDate} = \text{BaseDate} + \text{CycleIntervalDays}$$
   - Unified Next Due Date:
     $$\text{NextDueDate} = \min(\text{ProjectedKMDate}, \text{CalendarDueDate})$$
3. **Call Horizon & Work Order Generation:**
   - Call Threshold KM:
     $$\text{CallKM} = \text{NextDueKM} - \left(\text{CycleIntervalKM} \times \left(1.0 - \frac{\text{CallHorizonPercent}}{100.0}\right)\right)$$
   - If $\text{CurrentOdometer} \ge \text{CallKM}$ or $\text{Now} \ge \text{CallDate}$:
     - If no active (non-terminal) work order exists for `(tenant_id, vehicle_id, plan_id)`:
       - Open new `work_orders` entry:
         - `status = 'open'`
         - `plan_id = plan.id`
         - `due_at = NextDueDate`
         - `due_km = NextDueKM`
         - `title = "IP41: " + plan.service_type + " - " + plan.plan_number`
     - Advance plan schedule state:
       - `last_scheduled_km = CurrentOdometer`
       - `last_scheduled_date = Now`
       - Recalculate `next_due_km` and `next_due_date` for the subsequent cycle.

## 4. API Endpoints
- `POST /api/v1/maintenance/plans`: Create maintenance plan (validates vehicle and measuring point)
- `GET /api/v1/maintenance/plans`: List maintenance plans for the authenticated tenant
- `GET /api/v1/maintenance/plans/{id}`: Fetch single plan with live projection details
- `POST /api/v1/maintenance/plans/{id}/evaluate`: Trigger manual evaluation and work order generation

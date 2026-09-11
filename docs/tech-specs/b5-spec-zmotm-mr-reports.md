# Spec 04 §15: ZMOTM_MR Fleet & Operational Reports Suite (B5)

- **Migration Slot:** `00144` (Allocated in ownership index)
- **Owner:** Domain: `internal/handlers/zmotm_reports.go` & `internal/handlers/reports.go` | Routes: `/reports/*`, `/api/v1/reports/*`
- **Status:** Approved Spec (Roadmap B5)

## 1. Operational Rationale & Acceptance Criteria
Fleet operations in the SAP TMS SOP (pp.16-20) mandate standard operational reporting transaction `ZMOTM_MR` across four critical operational dimensions:
1. **Vehicle Master Report (p.18 scr):** Comprehensive capital and equipment inventory export with exact column naming and semantics.
2. **Gate Register (p.18 scr):** Depot/facility in-and-out logging capturing departure/arrival timestamps, start/close odometers, and net trip distance.
3. **Fuel & KMPL Report (p.19 scr):** Reconciliation of fuel dispenses (`fuel_issues`, `00142`) against odometer distance delta to compute true operational efficiency.
4. **Breakdown / Outstanding Notifications Report (p.20 scr):** Fleet incident log tracking vehicle breakdown events, maintenance work order linkage, and resolution time.

## 2. Schema Architecture (`00144`)
To support the Gate Register with physical start/close counter readings per trip:
```sql
ALTER TABLE trips ADD COLUMN start_odometer REAL;
ALTER TABLE trips ADD COLUMN gate_facility_id TEXT;
```

## 3. Vehicle Master Exact Column Specification (p.18 Acceptance Test)
In accordance with `fleet-registry-sop-parity.md` §2 row 26:
| CSV / Export Column Header | Source Field | Mapping & Description |
|---|---|---|
| `Vehicle No` | `vehicles.registration_number` | Canonical vehicle registration plate (e.g. KA01TR1234) |
| `Type` | `vehicles.ownership` | Equipment ownership ('O' = Owned/Dept, 'C' = Contractual, 'F' = Fast, 'M' = Market) |
| `Category` | `vehicles.fleet_class` | Fleet class ('CV' = Commercial, 'PV' = Passenger, 'FS' = Fuel Station, 'OFS' = Off-site FS) |
| `Manufacturer` | `vehicles.manufacturer` | OEM manufacturer name (Tata, Ashok Leyland, BharatBenz, etc.) |
| `Model` | `vehicles.model` | Vehicle model designation (e.g. 1618, 4018) |
| `Purchase Date` | `vehicles.acquisition_date` | Date of capital asset acquisition (YYYY-MM-DD) |
| `Purchase Value` | `vehicles.acquisition_value` | Asset purchase value / capital book value |
| `Currency` | `vehicles.acquisition_currency` | ISO currency code (default: INR) |
| `Fuel Type` | `vehicles.fuel_type` | Primary propulsion fuel (diesel, cng, electric, etc.) |
| `Fleet Number` | `vehicles.fleet_number` | Internal fleet serial number (e.g. 35, 104) |
| `Chassis No` | `vehicles.chassis_no` | Manufacturer chassis / VIN number |
| `Engine SNo` | `vehicles.engine_number` | Manufacturer engine serial number |

## 4. Report Routes & Endpoints
- Vehicle Master:
  - HTML: `/reports/vehicles`
  - CSV: `/reports/vehicle-master.csv` & `/reports/vehicles.csv`
  - API: `GET /api/v1/reports/vehicle-master`
- Gate Register:
  - CSV: `/reports/gate-register.csv`
  - API: `GET /api/v1/reports/gate-register`
- Fuel & KMPL:
  - HTML: `/fuel/kmpl`
  - CSV: `/reports/fuel-kmpl.csv`
  - API: `GET /api/v1/reports/fuel-kmpl`
- Breakdown / Notifications:
  - CSV: `/reports/breakdown.csv`
  - API: `GET /api/v1/reports/breakdown`

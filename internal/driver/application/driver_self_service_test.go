package application

// Characterization + extraction tests for the driver-app self-service
// operations moved out of internal/handlers/drivers.go (handlers-end-state
// ADR, Phase 2): tenant-isolated identity resolution, the legacy unscoped
// status/issue identity rule, KYC/status transitions (deactivated_at /
// kyc_purged_at), license/expiry write semantics and the driver issue
// insert/list behavior.

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

const driverSelfServiceSchema = `
CREATE TABLE users (
    id TEXT PRIMARY KEY,
    email TEXT,
    password_hash TEXT,
    name TEXT,
    role_id INTEGER,
    status TEXT
);
CREATE TABLE drivers (
    id TEXT PRIMARY KEY,
    driver_id TEXT NOT NULL,
    first_name TEXT NOT NULL,
    last_name TEXT NOT NULL,
    phone TEXT NOT NULL,
    email TEXT,
    license_number TEXT,
    license_expiry DATE,
    bank_details TEXT,
    aadhaar TEXT,
    pan TEXT,
    status TEXT NOT NULL DEFAULT 'available',
    notes TEXT,
    score REAL,
    tier TEXT,
    deactivated_at DATETIME,
    kyc_purged_at DATETIME,
    tenant_id TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE vehicles (
    id TEXT PRIMARY KEY,
    registration_number TEXT NOT NULL,
    vehicle_number TEXT NOT NULL,
    vehicle_type TEXT NOT NULL DEFAULT 'truck',
    capacity REAL NOT NULL DEFAULT 5000,
    fuel_type TEXT NOT NULL DEFAULT 'diesel',
    insurance_expiry DATE,
    fitness_expiry DATE,
    permit_expiry DATE,
    status TEXT NOT NULL DEFAULT 'available',
    tenant_id TEXT NOT NULL
);
CREATE TABLE trips (
    id TEXT PRIMARY KEY,
    trip_number TEXT,
    driver_id TEXT,
    vehicle_id TEXT,
    status TEXT NOT NULL DEFAULT 'draft',
    departure_time DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    tenant_id TEXT NOT NULL
);
CREATE TABLE vehicle_latest_position (
    vehicle_id TEXT PRIMARY KEY,
    latitude REAL NOT NULL,
    longitude REAL NOT NULL
);
CREATE TABLE telemetry_snapshots (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    vehicle_id TEXT,
    driver_id TEXT,
    latitude REAL,
    longitude REAL,
    timestamp DATETIME NOT NULL
);
CREATE TABLE telemetry_devices (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL DEFAULT '1',
    imei TEXT NOT NULL,
    vehicle_id TEXT,
    status TEXT NOT NULL DEFAULT 'inventory',
    updated_at DATETIME
);
CREATE TABLE driver_behaviour_events (
    id TEXT PRIMARY KEY,
    driver_id TEXT NOT NULL,
    event_type TEXT NOT NULL,
    severity TEXT NOT NULL DEFAULT 'medium',
    metadata TEXT NOT NULL DEFAULT '{}',
    occurred_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE driver_preferred_vehicles (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL DEFAULT '1',
    driver_id TEXT NOT NULL,
    vehicle_id TEXT NOT NULL,
    is_primary INTEGER NOT NULL DEFAULT 1,
    assigned_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    unassigned_at DATETIME
);
CREATE TABLE driver_issues (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NOT NULL DEFAULT '1',
    driver_id TEXT NOT NULL,
    trip_id TEXT,
    category TEXT NOT NULL DEFAULT 'other',
    severity TEXT NOT NULL DEFAULT 'low',
    message TEXT NOT NULL,
    photo_url TEXT,
    status TEXT NOT NULL DEFAULT 'open',
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
`

func newDriverSelfServiceTestDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("driver_self_service_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared")
	require.NoError(t, err)
	_, err = db.Exec(driverSelfServiceSchema)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func mustExecDriverTest(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	_, err := db.Exec(query, args...)
	require.NoError(t, err)
}

func driverDeactivatedAt(t *testing.T, db *sql.DB, driverID string) sql.NullTime {
	t.Helper()
	var deactivated sql.NullTime
	require.NoError(t, db.QueryRow(`SELECT deactivated_at FROM drivers WHERE id = $1`, driverID).Scan(&deactivated))
	return deactivated
}

func driverKYCColumn(t *testing.T, db *sql.DB, driverID, column string) string {
	t.Helper()
	var value string
	require.NoError(t, db.QueryRow(`SELECT `+column+` FROM drivers WHERE id = $1`, driverID).Scan(&value))
	return value
}

func TestDriverSelfService_GetMe_TenantIsolationAndIdentityRules(t *testing.T) {
	db := newDriverSelfServiceTestDB(t)
	ctx := context.Background()
	mustExecDriverTest(t, db, `INSERT INTO users (id, email) VALUES ('u-1', 'one@example.com')`)
	mustExecDriverTest(t, db, `INSERT INTO drivers (id, driver_id, first_name, last_name, phone, email, license_number, license_expiry, bank_details, aadhaar, pan, status, tenant_id)
		VALUES ('d-1', 'DRV-1', 'Tenant', 'One', '9000000001', 'one@example.com', 'DL-1', '2030-01-01', 'HDFC ****1111', '1111-2222-3333', 'ABCDE1234F', 'available', '1')`)
	// Same email in another tenant: must resolve only inside its own tenant.
	mustExecDriverTest(t, db, `INSERT INTO drivers (id, driver_id, first_name, last_name, phone, email, license_number, license_expiry, status, tenant_id)
		VALUES ('d-2', 'DRV-2', 'Tenant', 'Two', '9000000002', 'one@example.com', 'DL-2', '2031-01-01', 'available', '2')`)
	// Driver display code is optional: driver_code falls back to the row id.
	mustExecDriverTest(t, db, `INSERT INTO drivers (id, driver_id, first_name, last_name, phone, status, tenant_id)
		VALUES ('d-3', '', 'No', 'Code', '9000000003', 'available', '1')`)

	self := NewDriverSelfService(db)

	me, err := self.GetMe(ctx, "u-1", "1")
	require.NoError(t, err)
	assert.Equal(t, "DRV-1", me.DriverCode)
	assert.Equal(t, "d-1", me.Profile.ID)
	assert.Equal(t, "Tenant", me.Profile.FirstName)
	assert.Equal(t, "9000000001", me.Profile.Phone)
	assert.Equal(t, "2030-01-01", me.Profile.LicenseExpiry)
	// KYC fields are returned to the driver for their own row (self-service
	// endpoint, no masking at this layer); the raw stored columns are kept.
	assert.Equal(t, "HDFC ****1111", me.Profile.BankDetails)
	assert.Equal(t, "1111-2222-3333", me.Profile.Aadhaar)
	assert.Equal(t, "ABCDE1234F", me.Profile.Pan)

	// Identity is tenant-scoped (Spec 13 §2.2): the same user resolves the
	// other tenant row only when acting in that tenant.
	me, err = self.GetMe(ctx, "u-1", "2")
	require.NoError(t, err)
	assert.Equal(t, "DRV-2", me.DriverCode)

	// A cross-tenant user id/email match never resolves.
	_, err = self.GetMe(ctx, "u-1", "3")
	assert.ErrorIs(t, err, ErrDriverNotFound)

	// drivers.id identity rule.
	me, err = self.GetMe(ctx, "d-1", "1")
	require.NoError(t, err)
	assert.Equal(t, "DRV-1", me.DriverCode)

	// driver_code fallback to the row id.
	me, err = self.GetMe(ctx, "d-3", "1")
	require.NoError(t, err)
	assert.Equal(t, "d-3", me.DriverCode)
}

func TestDriverSelfService_GetMe_ActiveTripVehicleAndLocation(t *testing.T) {
	db := newDriverSelfServiceTestDB(t)
	ctx := context.Background()
	mustExecDriverTest(t, db, `INSERT INTO users (id, email) VALUES ('u-1', 'one@example.com')`)
	mustExecDriverTest(t, db, `INSERT INTO drivers (id, driver_id, first_name, last_name, phone, email, status, tenant_id)
		VALUES ('d-1', 'DRV-1', 'Active', 'Trip', '9000000001', 'one@example.com', 'on_trip', '1')`)
	mustExecDriverTest(t, db, `INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, tenant_id)
		VALUES ('v-1', 'MH-12-PQ-4521', 'VEH-01', 'truck', '1')`)
	// Trip linked by the display driver_code, which the legacy query also matches.
	mustExecDriverTest(t, db, `INSERT INTO trips (id, trip_number, driver_id, vehicle_id, status, departure_time, tenant_id)
		VALUES ('t-1', 'TRP-8492', 'DRV-1', 'v-1', 'in_transit', '2026-08-19 10:30:00', '1')`)
	mustExecDriverTest(t, db, `INSERT INTO vehicle_latest_position (vehicle_id, latitude, longitude) VALUES ('v-1', 18.5204, 73.8567)`)

	self := NewDriverSelfService(db)
	me, err := self.GetMe(ctx, "u-1", "1")
	require.NoError(t, err)
	assert.Equal(t, "MH-12-PQ-4521", me.VehiclePlate)
	require.NotNil(t, me.CurrentLocation)
	assert.InDelta(t, 18.5204, me.CurrentLocation.Latitude, 0.0001)
	assert.InDelta(t, 73.8567, me.CurrentLocation.Longitude, 0.0001)

	// Without a device latest position the snapshot fallback is used.
	mustExecDriverTest(t, db, `DELETE FROM vehicle_latest_position WHERE vehicle_id = 'v-1'`)
	mustExecDriverTest(t, db, `INSERT INTO telemetry_snapshots (vehicle_id, driver_id, latitude, longitude, timestamp)
		VALUES ('v-1', 'd-1', 19.0760, 72.8777, '2026-08-19 11:00:00')`)
	me, err = self.GetMe(ctx, "u-1", "1")
	require.NoError(t, err)
	require.NotNil(t, me.CurrentLocation)
	assert.InDelta(t, 19.0760, me.CurrentLocation.Latitude, 0.0001)

	// A newer active trip in another tenant must not leak its plate.
	mustExecDriverTest(t, db, `INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, tenant_id)
		VALUES ('v-9', 'KA-99-XX-0001', 'VEH-9', 'truck', '2')`)
	mustExecDriverTest(t, db, `INSERT INTO trips (id, trip_number, driver_id, vehicle_id, status, departure_time, tenant_id)
		VALUES ('t-9', 'TRP-9', 'd-1', 'v-9', 'in_transit', '2026-09-01 10:30:00', '2')`)
	me, err = self.GetMe(ctx, "u-1", "1")
	require.NoError(t, err)
	assert.Equal(t, "MH-12-PQ-4521", me.VehiclePlate)

	// No active trip: the plate stays empty but the driver-owned snapshot
	// still supplies the last known location (legacy best-effort query).
	mustExecDriverTest(t, db, `UPDATE trips SET status = 'completed' WHERE id IN ('t-1', 't-9')`)
	me, err = self.GetMe(ctx, "u-1", "1")
	require.NoError(t, err)
	assert.Empty(t, me.VehiclePlate)
	require.NotNil(t, me.CurrentLocation)
	assert.InDelta(t, 19.0760, me.CurrentLocation.Latitude, 0.0001)
}

func TestDriverSelfService_UpdateMe_TenantScopingKYCAndExpiry(t *testing.T) {
	db := newDriverSelfServiceTestDB(t)
	ctx := context.Background()
	mustExecDriverTest(t, db, `INSERT INTO users (id, email) VALUES ('u-1', 'one@example.com'), ('u-2', 'two@example.com')`)
	mustExecDriverTest(t, db, `INSERT INTO drivers (id, driver_id, first_name, last_name, phone, email, license_number, license_expiry, bank_details, aadhaar, pan, status, deactivated_at, kyc_purged_at, tenant_id)
		VALUES ('d-1', 'DRV-1', 'Tenant', 'One', '9000000001', 'one@example.com', 'OLD-DL', '2025-01-01', 'OLD-BANK', '1111', 'OLDPAN', 'inactive', '2025-01-01 00:00:00', '2026-01-01 00:00:00', '1')`)
	mustExecDriverTest(t, db, `INSERT INTO drivers (id, driver_id, first_name, last_name, phone, email, license_number, status, tenant_id)
		VALUES ('d-2', 'DRV-2', 'Tenant', 'Two', '9000000002', 'two@example.com', 'DL-2', 'available', '2')`)

	self := NewDriverSelfService(db)

	// Cross-tenant write must not resolve.
	_, err := self.UpdateMe(ctx, UpdateMeCommand{UserID: "u-2", TenantID: "1", LicenseNumber: "HACK", Status: "inactive"})
	assert.ErrorIs(t, err, ErrDriverNotFound)
	assert.Equal(t, "DL-2", driverKYCColumn(t, db, "d-2", "license_number"))

	// Reactivation clears deactivated_at, updates KYC columns and leaves the
	// retention purge stamp alone (the worker owns kyc_purged_at).
	result, err := self.UpdateMe(ctx, UpdateMeCommand{
		UserID:        "u-1",
		TenantID:      "1",
		LicenseNumber: "NEW-DL",
		LicenseExpiry: "2032-12-31",
		BankDetails:   "HDFC ****6587",
		Aadhaar:       "9999-8888-7777",
		Pan:           "NEWPAN1234",
		Status:        "available",
	})
	require.NoError(t, err)
	assert.Equal(t, "d-1", result.DriverID)
	assert.Empty(t, result.VehiclePlate)
	assert.Equal(t, "NEW-DL", driverKYCColumn(t, db, "d-1", "license_number"))
	// DATE affinity scans back through database/sql as RFC3339 (same as the legacy handler).
	assert.Equal(t, "2032-12-31T00:00:00Z", driverKYCColumn(t, db, "d-1", "license_expiry"))
	assert.Equal(t, "HDFC ****6587", driverKYCColumn(t, db, "d-1", "bank_details"))
	assert.Equal(t, "9999-8888-7777", driverKYCColumn(t, db, "d-1", "aadhaar"))
	assert.Equal(t, "NEWPAN1234", driverKYCColumn(t, db, "d-1", "pan"))
	assert.Equal(t, "available", driverKYCColumn(t, db, "d-1", "status"))
	assert.False(t, driverDeactivatedAt(t, db, "d-1").Valid, "reactivation must clear deactivated_at")
	assert.Equal(t, "2026-01-01T00:00:00Z", driverKYCColumn(t, db, "d-1", "kyc_purged_at"))

	// Empty values keep the stored columns; an unknown status leaves the
	// stored status untouched (legacy CASE guard).
	_, err = self.UpdateMe(ctx, UpdateMeCommand{UserID: "u-1", TenantID: "1", Status: "on_trip"})
	require.NoError(t, err)
	assert.Equal(t, "NEW-DL", driverKYCColumn(t, db, "d-1", "license_number"))
	assert.Equal(t, "HDFC ****6587", driverKYCColumn(t, db, "d-1", "bank_details"))
	assert.Equal(t, "available", driverKYCColumn(t, db, "d-1", "status"))

	// Inactive stamps deactivated_at; leave clears it again.
	_, err = self.UpdateMe(ctx, UpdateMeCommand{UserID: "u-1", TenantID: "1", Status: "inactive"})
	require.NoError(t, err)
	assert.True(t, driverDeactivatedAt(t, db, "d-1").Valid, "inactive must start the KYC retention clock")
	assert.Equal(t, "2026-01-01T00:00:00Z", driverKYCColumn(t, db, "d-1", "kyc_purged_at"))
	_, err = self.UpdateMe(ctx, UpdateMeCommand{UserID: "u-1", TenantID: "1", Status: "leave"})
	require.NoError(t, err)
	assert.False(t, driverDeactivatedAt(t, db, "d-1").Valid, "leave must clear deactivated_at")

	// No expiry guard on this legacy path: the submitted date is stored
	// verbatim even when it is already in the past.
	_, err = self.UpdateMe(ctx, UpdateMeCommand{UserID: "u-1", TenantID: "1", LicenseExpiry: "2001-01-01"})
	require.NoError(t, err)
	assert.Equal(t, "2001-01-01T00:00:00Z", driverKYCColumn(t, db, "d-1", "license_expiry"))

	// Unknown actor.
	_, err = self.UpdateMe(ctx, UpdateMeCommand{UserID: "u-missing", TenantID: "1"})
	assert.ErrorIs(t, err, ErrDriverNotFound)
}

func TestDriverSelfService_UpdateMe_VehicleBinding(t *testing.T) {
	db := newDriverSelfServiceTestDB(t)
	ctx := context.Background()
	mustExecDriverTest(t, db, `INSERT INTO users (id, email) VALUES ('u-1', 'one@example.com')`)
	mustExecDriverTest(t, db, `INSERT INTO drivers (id, driver_id, first_name, last_name, phone, email, status, tenant_id)
		VALUES ('d-1', 'DRV-1', 'Vehicle', 'Binding', '9000000001', 'one@example.com', 'available', '1')`)
	// Synthetic telemetry device keyed by the acting user id (legacy link rule).
	mustExecDriverTest(t, db, `INSERT INTO telemetry_devices (id, tenant_id, imei, status) VALUES ('dev-1', '1', 'u-1', 'inventory')`)

	self := NewDriverSelfService(db)
	result, err := self.UpdateMe(ctx, UpdateMeCommand{UserID: "u-1", TenantID: "1", VehicleNumber: " mh-12-ab-1234 "})
	require.NoError(t, err)
	assert.Equal(t, "MH-12-AB-1234", result.VehiclePlate)

	var vehicleID, registration, status string
	require.NoError(t, db.QueryRow(`SELECT id, registration_number, status FROM vehicles WHERE tenant_id = '1'`).Scan(&vehicleID, &registration, &status))
	assert.Equal(t, "MH-12-AB-1234", registration)
	assert.Equal(t, "available", status)
	assert.Equal(t, "MH-12-AB-1234", driverKYCColumn(t, db, "d-1", "notes"))

	var deviceVehicleID, deviceStatus string
	require.NoError(t, db.QueryRow(`SELECT vehicle_id, status FROM telemetry_devices WHERE id = 'dev-1'`).Scan(&deviceVehicleID, &deviceStatus))
	assert.Equal(t, vehicleID, deviceVehicleID)
	assert.Equal(t, "active", deviceStatus)

	// Re-submitting the same plate reuses the existing vehicle row.
	result, err = self.UpdateMe(ctx, UpdateMeCommand{UserID: "u-1", TenantID: "1", VehicleNumber: "MH-12-AB-1234"})
	require.NoError(t, err)
	assert.Equal(t, "MH-12-AB-1234", result.VehiclePlate)
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM vehicles WHERE tenant_id = '1' AND registration_number = 'MH-12-AB-1234'`).Scan(&count))
	assert.Equal(t, 1, count)

	// vehicle_plate is the fallback field for the same normalisation.
	result, err = self.UpdateMe(ctx, UpdateMeCommand{UserID: "u-1", TenantID: "1", VehiclePlate: "MH-12-CD-9999"})
	require.NoError(t, err)
	assert.Equal(t, "MH-12-CD-9999", result.VehiclePlate)

	// The vehicle lookup is tenant-scoped: an existing tenant-2 plate is not
	// reused, a new tenant-1 vehicle row is created instead.
	mustExecDriverTest(t, db, `INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, tenant_id)
		VALUES ('v-t2', 'MH-12-EF-7777', 'VEH-T2', 'truck', '2')`)
	result, err = self.UpdateMe(ctx, UpdateMeCommand{UserID: "u-1", TenantID: "1", VehicleNumber: "MH-12-EF-7777"})
	require.NoError(t, err)
	assert.Equal(t, "MH-12-EF-7777", result.VehiclePlate)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM vehicles WHERE registration_number = 'MH-12-EF-7777'`).Scan(&count))
	assert.Equal(t, 2, count, "tenant isolation must prevent cross-tenant vehicle reuse")
}

func TestDriverSelfService_UpdateMyStatus_StatusTransitionsAndKYCClock(t *testing.T) {
	db := newDriverSelfServiceTestDB(t)
	ctx := context.Background()
	mustExecDriverTest(t, db, `INSERT INTO users (id, email) VALUES ('u-1', 'one@example.com')`)
	mustExecDriverTest(t, db, `INSERT INTO drivers (id, driver_id, first_name, last_name, phone, email, status, tenant_id)
		VALUES ('d-1', 'DRV-1', 'Status', 'Toggle', '9000000001', 'one@example.com', 'available', '1')`)

	self := NewDriverSelfService(db)
	driverID, err := self.UpdateMyStatus(ctx, "u-1", "inactive")
	require.NoError(t, err)
	assert.Equal(t, "d-1", driverID)
	assert.Equal(t, "inactive", driverKYCColumn(t, db, "d-1", "status"))
	assert.True(t, driverDeactivatedAt(t, db, "d-1").Valid, "inactive must stamp deactivated_at")

	// Stamping is idempotent (COALESCE keeps the first deactivation time).
	first := driverDeactivatedAt(t, db, "d-1")
	_, err = self.UpdateMyStatus(ctx, "u-1", "inactive")
	require.NoError(t, err)
	second := driverDeactivatedAt(t, db, "d-1")
	assert.Equal(t, first.Time.UTC().Format(time.RFC3339), second.Time.UTC().Format(time.RFC3339))

	// available and leave both clear the clock.
	_, err = self.UpdateMyStatus(ctx, "u-1", "available")
	require.NoError(t, err)
	assert.False(t, driverDeactivatedAt(t, db, "d-1").Valid, "reactivation must clear deactivated_at")
	_, err = self.UpdateMyStatus(ctx, "u-1", "leave")
	require.NoError(t, err)
	assert.False(t, driverDeactivatedAt(t, db, "d-1").Valid, "leave must clear deactivated_at")
	assert.Equal(t, "leave", driverKYCColumn(t, db, "d-1", "status"))

	// Unknown actor.
	_, err = self.UpdateMyStatus(ctx, "u-missing", "inactive")
	assert.ErrorIs(t, err, ErrDriverNotFound)
}

func TestDriverSelfService_LegacyUnscopedIdentity(t *testing.T) {
	db := newDriverSelfServiceTestDB(t)
	ctx := context.Background()
	mustExecDriverTest(t, db, `INSERT INTO users (id, email) VALUES ('u-9', 'nine@example.com')`)
	mustExecDriverTest(t, db, `INSERT INTO drivers (id, driver_id, first_name, last_name, phone, email, status, tenant_id)
		VALUES ('d-9', 'DRV-9', 'Other', 'Tenant', '9000000009', 'nine@example.com', 'available', '2')`)

	// FROZEN LEGACY BEHAVIOR: the mobile status/issue endpoints predate
	// tenant scoping and resolve the driver by id-or-user-email without a
	// tenant filter. Behavior is preserved exactly by the extraction; the
	// tenant-scoped GetMe/UpdateMe paths are pinned separately above.
	self := NewDriverSelfService(db)
	driverID, err := self.UpdateMyStatus(ctx, "u-9", "inactive")
	require.NoError(t, err)
	assert.Equal(t, "d-9", driverID)
	assert.Equal(t, "inactive", driverKYCColumn(t, db, "d-9", "status"))

	_, err = self.ReportIssue(ctx, ReportIssueCommand{DriverID: driverID, Category: "vehicle", Severity: "high", Message: "legacy scope"})
	require.NoError(t, err)
	issues, err := self.ListMyIssues(ctx, "u-9")
	require.NoError(t, err)
	require.Len(t, issues, 1)
}

func TestDriverSelfService_ReportIssueAndListMyIssues(t *testing.T) {
	db := newDriverSelfServiceTestDB(t)
	ctx := context.Background()
	mustExecDriverTest(t, db, `INSERT INTO users (id, email) VALUES ('u-1', 'one@example.com')`)
	mustExecDriverTest(t, db, `INSERT INTO drivers (id, driver_id, first_name, last_name, phone, email, status, tenant_id)
		VALUES ('d-1', 'DRV-1', 'Issue', 'Driver', '9000000001', 'one@example.com', 'available', '1')`)
	mustExecDriverTest(t, db, `INSERT INTO drivers (id, driver_id, first_name, last_name, phone, email, status, tenant_id)
		VALUES ('d-other', 'DRV-O', 'Other', 'Driver', '9000000002', 'other@example.com', 'available', '1')`)

	self := NewDriverSelfService(db)
	firstID, err := self.ReportIssue(ctx, ReportIssueCommand{DriverID: "d-1", TripID: "t-1", Category: "vehicle", Severity: "high", Message: "Tyre wear front-left"})
	require.NoError(t, err)
	assert.NotEmpty(t, firstID)
	mustExecDriverTest(t, db, `UPDATE driver_issues SET created_at = '2026-01-01 00:00:00' WHERE id = $1`, firstID)

	secondID, err := self.ReportIssue(ctx, ReportIssueCommand{DriverID: "d-1", Category: "other", Severity: "low", Message: "Second issue", PhotoURL: "/files/f-1"})
	require.NoError(t, err)
	mustExecDriverTest(t, db, `UPDATE driver_issues SET created_at = '2026-01-02 00:00:00' WHERE id = $1`, secondID)

	// Another driver's issue must not leak into the list.
	_, err = self.ReportIssue(ctx, ReportIssueCommand{DriverID: "d-other", Category: "road", Severity: "medium", Message: "Other driver"})
	require.NoError(t, err)

	issues, err := self.ListMyIssues(ctx, "u-1")
	require.NoError(t, err)
	require.Len(t, issues, 2)
	assert.Equal(t, secondID, issues[0].ID, "newest issue first")
	assert.Empty(t, issues[0].TripID)
	assert.Equal(t, "other", issues[0].Category)
	assert.Equal(t, "low", issues[0].Severity)
	assert.Equal(t, "Second issue", issues[0].Message)
	assert.Equal(t, "/files/f-1", issues[0].PhotoURL)
	assert.Equal(t, "open", issues[0].Status)
	// DATETIME scans through database/sql as RFC3339 (legacy handler scanned into a string too).
	assert.Equal(t, "2026-01-02T00:00:00Z", issues[0].CreatedAt)
	assert.Equal(t, firstID, issues[1].ID)
	assert.Equal(t, "t-1", issues[1].TripID)
	assert.Empty(t, issues[1].PhotoURL, "NULL photo_url renders as an empty string")

	// The legacy read caps the list at 20 newest rows.
	for i := 0; i < 22; i++ {
		_, err = self.ReportIssue(ctx, ReportIssueCommand{DriverID: "d-1", Category: "other", Severity: "low", Message: fmt.Sprintf("bulk %d", i)})
		require.NoError(t, err)
	}
	mustExecDriverTest(t, db, `UPDATE driver_issues SET created_at = '2026-03-01 00:00:00' WHERE driver_id = 'd-1' AND message LIKE 'bulk %'`)
	issues, err = self.ListMyIssues(ctx, "u-1")
	require.NoError(t, err)
	require.Len(t, issues, 20)

	// Unknown actor.
	_, err = self.ListMyIssues(ctx, "u-missing")
	assert.ErrorIs(t, err, ErrDriverNotFound)
}

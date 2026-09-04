package sql

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/repository"
	"transport-app/internal/shared"
	"transport-app/internal/trip/domain/aggregate"
)

const tripTestSchema = `
CREATE TABLE users (
    id TEXT PRIMARY KEY,
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    name TEXT NOT NULL
);
CREATE TABLE drivers (
    id TEXT PRIMARY KEY,
    driver_id TEXT NOT NULL,
    first_name TEXT,
    last_name TEXT,
    tenant_id TEXT NOT NULL DEFAULT '1',
    email TEXT
);
CREATE TABLE vehicles (
    id TEXT PRIMARY KEY,
    registration_number TEXT,
    vehicle_number TEXT,
    tenant_id TEXT NOT NULL DEFAULT '1'
);
CREATE TABLE routes (
    id TEXT PRIMARY KEY,
    source TEXT NOT NULL,
    destination TEXT NOT NULL,
    tenant_id TEXT NOT NULL DEFAULT '1'
);
CREATE TABLE trips (
    id TEXT PRIMARY KEY,
    trip_number TEXT NOT NULL UNIQUE,
    booking_id TEXT,
    driver_id TEXT,
    vehicle_id TEXT,
    route_id TEXT NOT NULL,
    departure_time DATETIME NOT NULL,
    arrival_time DATETIME,
    status TEXT NOT NULL,
    remarks TEXT,
    tenant_id TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    started_at DATETIME,
    reached_pickup_at DATETIME,
    in_transit_at DATETIME,
    delivered_at DATETIME,
    completed_at DATETIME,
    idempotency_key TEXT
);
CREATE TABLE outbox_events (
    id TEXT PRIMARY KEY,
    aggregate_id TEXT NOT NULL,
    aggregate_type TEXT NOT NULL,
    event_type TEXT NOT NULL,
    payload TEXT NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP NOT NULL,
    published_at DATETIME
);
`

func setupTripTestDB(t *testing.T) *sql.DB {
	t.Helper()
	safeName := strings.ReplaceAll(t.Name(), "/", "_")
	safeName = strings.ReplaceAll(safeName, " ", "_")
	dsn := "file:" + safeName + "?mode=memory&cache=shared&_pragma=journal_mode(WAL)"
	dbConn, err := sql.Open("sqlite", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = dbConn.Close() })
	_, err = dbConn.Exec(tripTestSchema)
	require.NoError(t, err)
	return dbConn
}

func seedRoute(t *testing.T, dbConn *sql.DB, id, source, destination string) {
	t.Helper()
	_, err := dbConn.Exec(`INSERT INTO routes (id, source, destination, tenant_id) VALUES (?, ?, ?, ?)`, id, source, destination, "1")
	require.NoError(t, err)
}

func seedRouteWithTenant(t *testing.T, dbConn *sql.DB, id, source, destination, tenantID string) {
	t.Helper()
	_, err := dbConn.Exec(`INSERT INTO routes (id, source, destination, tenant_id) VALUES (?, ?, ?, ?)`, id, source, destination, tenantID)
	require.NoError(t, err)
}

func seedDriver(t *testing.T, dbConn *sql.DB, id, driverCode, firstName, lastName, tenantID, email string) {
	t.Helper()
	_, err := dbConn.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, tenant_id, email) VALUES (?, ?, ?, ?, ?, ?)`, id, driverCode, firstName, lastName, tenantID, email)
	require.NoError(t, err)
}

func seedVehicle(t *testing.T, dbConn *sql.DB, id, regNo, vehicleNo, tenantID string) {
	t.Helper()
	_, err := dbConn.Exec(`INSERT INTO vehicles (id, registration_number, vehicle_number, tenant_id) VALUES (?, ?, ?, ?)`, id, regNo, vehicleNo, tenantID)
	require.NoError(t, err)
}

func seedUser(t *testing.T, dbConn *sql.DB, id, email string) {
	t.Helper()
	_, err := dbConn.Exec(`INSERT INTO users (id, email, password_hash, name) VALUES (?, ?, ?, ?)`, id, email, "hash", "Test User")
	require.NoError(t, err)
}

func newTestTripAgg(id, tenantID, tripNumber string, bookingID *string, routeID string, departure time.Time, remarks string, now time.Time) *aggregate.TripAggregate {
	return aggregate.NewTripAggregate(
		aggregate.TripID(id),
		shared.TenantID(tenantID),
		tripNumber,
		bookingID,
		routeID,
		departure,
		remarks,
		now,
	)
}

// ---------------------------------------------------------------------------
// Save / Find / Exists - improving 55%/57%/66%
// ---------------------------------------------------------------------------

func TestTripRepository_SaveAndFind(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	agg := newTestTripAgg("tr-1", "1", "TR-0001", nil, "route-1", now.Add(2*time.Hour), "Testing remarks", now)
	require.NoError(t, repo.Save(ctx, agg))
	assert.Equal(t, int64(1), agg.Version)
	found, err := repo.Find(ctx, "tr-1", "1")
	require.NoError(t, err)
	require.Equal(t, agg.TripNumber, found.TripNumber)
	require.Equal(t, agg.Remarks, found.Remarks)
	require.Equal(t, agg.Status, found.Status)
	require.Equal(t, int64(1), found.Version)
}

func TestTripRepository_Save_UpdateSuccess(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	agg := newTestTripAgg("tr-1", "1", "TR-0001", nil, "route-1", now.Add(2*time.Hour), "Initial", now)
	require.NoError(t, repo.Save(ctx, agg))
	require.Equal(t, int64(1), agg.Version)
	// mutate and save again -> Update path
	agg.Remarks = "Updated remarks"
	newDeparture := now.Add(5 * time.Hour)
	agg.DepartureTime = newDeparture
	agg.UpdatedAt = now.Add(time.Hour)
	require.NoError(t, repo.Save(ctx, agg))
	require.Equal(t, int64(2), agg.Version)
	found, err := repo.Find(ctx, "tr-1", "1")
	require.NoError(t, err)
	require.Equal(t, "Updated remarks", found.Remarks)
	require.Equal(t, newDeparture, found.DepartureTime)
	require.Equal(t, int64(2), found.Version)
	// verify outbox events were written (at least 1 per save, may be 2 total)
	var outboxCount int
	err = dbConn.QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE aggregate_id = ?`, "tr-1").Scan(&outboxCount)
	require.NoError(t, err)
	require.GreaterOrEqual(t, outboxCount, 1)
}

func TestTripRepository_Save_WithDriverAndVehicle(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	seedDriver(t, dbConn, "drv-1", "DRV-001", "John", "Doe", "1", "john@example.com")
	seedVehicle(t, dbConn, "veh-1", "MH01AB1234", "V-001", "1")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	agg := newTestTripAgg("tr-2", "1", "TR-0002", nil, "route-1", now.Add(2*time.Hour), "With driver", now)
	// assign driver and vehicle via domain methods before first save (driver required before vehicle)
	require.NoError(t, agg.AssignDriver("drv-1", now))
	require.NoError(t, agg.AssignVehicle("veh-1", now))
	require.NotNil(t, agg.DriverID)
	require.NotNil(t, agg.VehicleID)
	require.NoError(t, repo.Save(ctx, agg))
	found, err := repo.Find(ctx, "tr-2", "1")
	require.NoError(t, err)
	require.NotNil(t, found.DriverID)
	require.Equal(t, "drv-1", *found.DriverID)
	require.NotNil(t, found.VehicleID)
	require.Equal(t, "veh-1", *found.VehicleID)
	require.Equal(t, aggregate.TripAssigned, found.Status)
}

func TestTripRepository_Save_WithTimelineFields(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	agg := newTestTripAgg("tr-tl", "1", "TR-TL-01", nil, "route-1", now.Add(2*time.Hour), "timeline", now)
	require.NoError(t, agg.Schedule(now))
	require.NoError(t, agg.AssignDriver("drv-x", now))
	require.NoError(t, agg.Start(now.Add(time.Hour)))
	require.NoError(t, agg.ReachPickup(now.Add(2*time.Hour)))
	require.NoError(t, agg.StartTransit(now.Add(3*time.Hour)))
	require.NoError(t, agg.Deliver(now.Add(4*time.Hour)))
	require.NoError(t, agg.Complete(now.Add(5*time.Hour)))
	require.NoError(t, repo.Save(ctx, agg))
	found, err := repo.Find(ctx, "tr-tl", "1")
	require.NoError(t, err)
	require.NotNil(t, found.StartedAt)
	require.NotNil(t, found.ReachedPickupAt)
	require.NotNil(t, found.InTransitAt)
	require.NotNil(t, found.DeliveredAt)
	require.NotNil(t, found.CompletedAt)
	require.Equal(t, aggregate.TripCompleted, found.Status)
	// also verify via GetReadModel timeline fields
	rm, err := repo.GetReadModel(ctx, "tr-tl", "1")
	require.NoError(t, err)
	require.NotNil(t, rm.StartedAt)
	require.NotNil(t, rm.ReachedPickupAt)
	require.NotNil(t, rm.InTransitAt)
	require.NotNil(t, rm.DeliveredAt)
	require.NotNil(t, rm.CompletedAt)
}

func TestTripRepository_Save_ConcurrencyConflict(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	agg := newTestTripAgg("tr-1", "1", "TR-0001", nil, "route-1", now.Add(2*time.Hour), "Initial", now)
	require.NoError(t, repo.Save(ctx, agg))
	require.Equal(t, int64(1), agg.Version)
	// simulate stale aggregate: reset version to 0
	agg.Version = 0
	agg.Remarks = "Stale update"
	err := repo.Save(ctx, agg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "concurrency conflict")
}

func TestTripRepository_Save_ErrorClosedDB(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	agg := newTestTripAgg("tr-1", "1", "TR-0001", nil, "route-1", now.Add(2*time.Hour), "Test", now)
	_ = dbConn.Close()
	err := repo.Save(ctx, agg)
	require.Error(t, err)
}

func TestTripRepository_Save_UpdateErrorClosedDB(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	agg := newTestTripAgg("tr-1", "1", "TR-0001", nil, "route-1", now.Add(2*time.Hour), "Test", now)
	require.NoError(t, repo.Save(ctx, agg))
	_ = dbConn.Close()
	agg.Remarks = "Update after close"
	err := repo.Save(ctx, agg)
	require.Error(t, err)
}

func TestTripRepository_Save_WithBookingID(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	bookingID := "bk-1"
	agg := newTestTripAgg("tr-bk", "1", "TR-BK-01", &bookingID, "route-1", now.Add(2*time.Hour), "with booking", now)
	require.NoError(t, repo.Save(ctx, agg))
	found, err := repo.Find(ctx, "tr-bk", "1")
	require.NoError(t, err)
	require.NotNil(t, found.BookingID)
	require.Equal(t, "bk-1", *found.BookingID)
}

func TestTripRepository_Find_Success(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	agg := newTestTripAgg("tr-1", "1", "TR-0001", nil, "route-1", now.Add(2*time.Hour), "Remarks", now)
	require.NoError(t, repo.Save(ctx, agg))
	found, err := repo.Find(ctx, "tr-1", "1")
	require.NoError(t, err)
	require.Equal(t, "TR-0001", found.TripNumber)
	require.Equal(t, "route-1", found.RouteID)
}

func TestTripRepository_Find_NotFound(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	_, err := repo.Find(ctx, "non-existent", "1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "trip not found")
}

func TestTripRepository_Find_TenantIsolation(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRouteWithTenant(t, dbConn, "route-1", "A", "B", "1")
	seedRouteWithTenant(t, dbConn, "route-2", "C", "D", "2")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	agg := newTestTripAgg("tr-1", "1", "TR-0001", nil, "route-1", now.Add(2*time.Hour), "Tenant1", now)
	require.NoError(t, repo.Save(ctx, agg))
	_, err := repo.Find(ctx, "tr-1", "2")
	require.Error(t, err)
	require.Contains(t, err.Error(), "trip not found")
	found, err := repo.Find(ctx, "tr-1", "1")
	require.NoError(t, err)
	require.Equal(t, "TR-0001", found.TripNumber)
}

func TestTripRepository_Find_ErrorClosedDB(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	agg := newTestTripAgg("tr-1", "1", "TR-0001", nil, "route-1", now.Add(2*time.Hour), "Test", now)
	require.NoError(t, repo.Save(ctx, agg))
	_ = dbConn.Close()
	_, err := repo.Find(ctx, "tr-1", "1")
	require.Error(t, err)
}

func TestTripRepository_Find_NullOptionalFields(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	agg := newTestTripAgg("tr-null", "1", "TR-NULL", nil, "route-1", now.Add(2*time.Hour), "", now)
	require.NoError(t, repo.Save(ctx, agg))
	found, err := repo.Find(ctx, "tr-null", "1")
	require.NoError(t, err)
	require.Nil(t, found.BookingID)
	require.Nil(t, found.DriverID)
	require.Nil(t, found.VehicleID)
	require.Nil(t, found.ArrivalTime)
	require.Equal(t, "", found.Remarks)
}

func TestTripRepository_Exists(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	exists, err := repo.Exists(ctx, "tr-1", "1")
	require.NoError(t, err)
	require.False(t, exists)
	agg := newTestTripAgg("tr-1", "1", "TR-0001", nil, "route-1", now.Add(2*time.Hour), "Test", now)
	require.NoError(t, repo.Save(ctx, agg))
	exists, err = repo.Exists(ctx, "tr-1", "1")
	require.NoError(t, err)
	require.True(t, exists)
}

func TestTripRepository_Exists_TenantIsolation(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	agg := newTestTripAgg("tr-1", "1", "TR-0001", nil, "route-1", now.Add(2*time.Hour), "Test", now)
	require.NoError(t, repo.Save(ctx, agg))
	exists, err := repo.Exists(ctx, "tr-1", "2")
	require.NoError(t, err)
	require.False(t, exists)
	exists, err = repo.Exists(ctx, "tr-1", "1")
	require.NoError(t, err)
	require.True(t, exists)
}

func TestTripRepository_Exists_ErrorClosedDB(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	agg := newTestTripAgg("tr-1", "1", "TR-0001", nil, "route-1", now.Add(2*time.Hour), "Test", now)
	require.NoError(t, repo.Save(ctx, agg))
	_ = dbConn.Close()
	_, err := repo.Exists(ctx, "tr-1", "1")
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// FindByNumber - 0% -> cover
// ---------------------------------------------------------------------------

func TestTripRepository_FindByNumber_Success(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "Mumbai", "Delhi")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	agg := newTestTripAgg("tr-1", "1", "TR-0001", nil, "route-1", now.Add(2*time.Hour), "Notes A", now)
	require.NoError(t, repo.Save(ctx, agg))
	found, err := repo.FindByNumber(ctx, "TR-0001", "1")
	require.NoError(t, err)
	require.NotNil(t, found)
	require.Equal(t, "tr-1", string(found.ID))
	require.Equal(t, "TR-0001", found.TripNumber)
	require.Equal(t, "Notes A", found.Remarks)
	require.Equal(t, int64(1), found.Version)
}

func TestTripRepository_FindByNumber_WithBookingID(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "Mumbai", "Delhi")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	bk := "bk-123"
	agg := newTestTripAgg("tr-bk", "1", "TR-BK-01", &bk, "route-1", now.Add(2*time.Hour), "With BK", now)
	require.NoError(t, repo.Save(ctx, agg))
	found, err := repo.FindByNumber(ctx, "TR-BK-01", "1")
	require.NoError(t, err)
	require.NotNil(t, found.BookingID)
	require.Equal(t, "bk-123", *found.BookingID)
}

func TestTripRepository_FindByNumber_NullOptionalFields(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "Mumbai", "Delhi")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	agg := newTestTripAgg("tr-null", "1", "TR-NULL-1", nil, "route-1", now.Add(2*time.Hour), "", now)
	require.NoError(t, repo.Save(ctx, agg))
	found, err := repo.FindByNumber(ctx, "TR-NULL-1", "1")
	require.NoError(t, err)
	require.Nil(t, found.BookingID)
	require.Nil(t, found.DriverID)
	require.Nil(t, found.VehicleID)
	require.Nil(t, found.ArrivalTime)
	require.Equal(t, "", found.Remarks)
}

func TestTripRepository_FindByNumber_NotFound(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "Mumbai", "Delhi")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	_, err := repo.FindByNumber(ctx, "NON-EXISTENT", "1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "trip not found")
}

func TestTripRepository_FindByNumber_TenantIsolation(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "Mumbai", "Delhi")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	agg := newTestTripAgg("tr-1", "1", "TR-0001", nil, "route-1", now.Add(2*time.Hour), "Tenant1", now)
	require.NoError(t, repo.Save(ctx, agg))
	_, err := repo.FindByNumber(ctx, "TR-0001", "2")
	require.Error(t, err)
	require.Contains(t, err.Error(), "trip not found")
	found, err := repo.FindByNumber(ctx, "TR-0001", "1")
	require.NoError(t, err)
	require.Equal(t, "tr-1", string(found.ID))
}

func TestTripRepository_FindByNumber_ErrorClosedDB(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "Mumbai", "Delhi")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	agg := newTestTripAgg("tr-1", "1", "TR-0001", nil, "route-1", now.Add(2*time.Hour), "Test", now)
	require.NoError(t, repo.Save(ctx, agg))
	_ = dbConn.Close()
	_, err := repo.FindByNumber(ctx, "TR-0001", "1")
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// GetReadModel - 0% -> cover
// ---------------------------------------------------------------------------

func TestTripRepository_GetReadModel_Success(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "Mumbai", "Delhi")
	seedDriver(t, dbConn, "drv-1", "DRV-001", "John", "Doe", "1", "john@example.com")
	seedVehicle(t, dbConn, "veh-1", "MH01AB1234", "V-001", "1")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	agg := newTestTripAgg("tr-1", "1", "TR-0001", nil, "route-1", now.Add(2*time.Hour), "ReadModel Test", now)
	require.NoError(t, agg.AssignDriver("drv-1", now))
	require.NoError(t, agg.AssignVehicle("veh-1", now))
	require.NoError(t, repo.Save(ctx, agg))
	rm, err := repo.GetReadModel(ctx, "tr-1", "1")
	require.NoError(t, err)
	require.Equal(t, "tr-1", rm.ID)
	require.Equal(t, "TR-0001", rm.TripNumber)
	require.NotNil(t, rm.DriverID)
	require.Equal(t, "drv-1", *rm.DriverID)
	require.Equal(t, "DRV-001", rm.DriverDisplayID)
	require.Equal(t, "John", rm.DriverFirstName)
	require.Equal(t, "Doe", rm.DriverLastName)
	require.NotNil(t, rm.VehicleID)
	require.Equal(t, "MH01AB1234", rm.VehicleRegistrationNumber)
	require.Equal(t, "V-001", rm.VehicleNumber)
	require.Equal(t, "Mumbai", rm.RouteSource)
	require.Equal(t, "Delhi", rm.RouteDestination)
	require.Equal(t, "ReadModel Test", rm.Remarks)
}

func TestTripRepository_GetReadModel_NullFields(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "Origin", "Destination")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	agg := newTestTripAgg("tr-null", "1", "TR-NULL-1", nil, "route-1", now.Add(2*time.Hour), "", now)
	require.NoError(t, repo.Save(ctx, agg))
	rm, err := repo.GetReadModel(ctx, "tr-null", "1")
	require.NoError(t, err)
	require.Nil(t, rm.DriverID)
	require.Nil(t, rm.VehicleID)
	require.Nil(t, rm.BookingID)
	require.Nil(t, rm.ArrivalTime)
	require.Nil(t, rm.StartedAt)
	require.Nil(t, rm.ReachedPickupAt)
	require.Nil(t, rm.InTransitAt)
	require.Nil(t, rm.DeliveredAt)
	require.Nil(t, rm.CompletedAt)
	require.Equal(t, "", rm.Remarks)
	require.Equal(t, "", rm.DriverDisplayID)
	require.Equal(t, "", rm.DriverFirstName)
	require.Equal(t, "", rm.VehicleRegistrationNumber)
}

func TestTripRepository_GetReadModel_WithTimelineAndArrival(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	agg := newTestTripAgg("tr-tl", "1", "TR-TL-01", nil, "route-1", now.Add(2*time.Hour), "timeline", now)
	require.NoError(t, agg.Schedule(now))
	require.NoError(t, agg.AssignDriver("drv-1", now))
	require.NoError(t, agg.Start(now.Add(time.Hour)))
	require.NoError(t, repo.Save(ctx, agg))
	rm, err := repo.GetReadModel(ctx, "tr-tl", "1")
	require.NoError(t, err)
	require.NotNil(t, rm.StartedAt)
	require.Equal(t, "started", rm.Status)
	// now progress to delivered to test ArrivalTime populated via Deliver
	agg2, err := repo.Find(ctx, "tr-tl", "1")
	require.NoError(t, err)
	require.NoError(t, agg2.ReachPickup(now.Add(2*time.Hour)))
	require.NoError(t, agg2.StartTransit(now.Add(3*time.Hour)))
	require.NoError(t, agg2.Deliver(now.Add(4*time.Hour)))
	require.NoError(t, repo.Save(ctx, agg2))
	rm2, err := repo.GetReadModel(ctx, "tr-tl", "1")
	require.NoError(t, err)
	require.NotNil(t, rm2.DeliveredAt)
	require.NotNil(t, rm2.ArrivalTime)
}

func TestTripRepository_GetReadModel_TenantIsolation(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	agg := newTestTripAgg("tr-1", "1", "TR-0001", nil, "route-1", now.Add(2*time.Hour), "Tenant1", now)
	require.NoError(t, repo.Save(ctx, agg))
	_, err := repo.GetReadModel(ctx, "tr-1", "2")
	require.Error(t, err)
	require.Contains(t, err.Error(), "trip not found")
	_, err = repo.GetReadModel(ctx, "tr-1", "1")
	require.NoError(t, err)
}

func TestTripRepository_GetReadModel_NotFound(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	_, err := repo.GetReadModel(ctx, "nonexistent", "1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "trip not found")
}

func TestTripRepository_GetReadModel_ErrorClosedDB(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	agg := newTestTripAgg("tr-1", "1", "TR-0001", nil, "route-1", now.Add(2*time.Hour), "Test", now)
	require.NoError(t, repo.Save(ctx, agg))
	_ = dbConn.Close()
	_, err := repo.GetReadModel(ctx, "tr-1", "1")
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// CheckDriverConflict - 0% -> cover
// ---------------------------------------------------------------------------

func TestTripRepository_CheckDriverConflict_NoConflictInitially(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	seedDriver(t, dbConn, "drv-1", "DRV-001", "John", "Doe", "1", "john@example.com")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	conflicts, err := repo.CheckDriverConflict(ctx, "drv-1", "1", "", time.Now(), nil)
	require.NoError(t, err)
	require.Len(t, conflicts, 0)
}

func TestTripRepository_CheckDriverConflict_WithConflict(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	seedDriver(t, dbConn, "drv-1", "DRV-001", "John", "Doe", "1", "john@example.com")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	// Create trip with driver assigned and status scheduled (conflict-eligible)
	agg := newTestTripAgg("tr-1", "1", "TR-0001", nil, "route-1", now.Add(2*time.Hour), "Test", now)
	require.NoError(t, agg.Schedule(now))
	require.NoError(t, agg.AssignDriver("drv-1", now))
	require.NoError(t, repo.Save(ctx, agg))
	// Directly update status to scheduled to ensure conflict query matches (Assign sets assigned which is also conflict)
	// Already assigned -> should be conflict
	conflicts, err := repo.CheckDriverConflict(ctx, "drv-1", "1", "", now, nil)
	require.NoError(t, err)
	require.Len(t, conflicts, 1)
	require.Equal(t, "tr-1", conflicts[0].ID)
	require.Equal(t, "TR-0001", conflicts[0].TripNumber)
	require.NotNil(t, conflicts[0].DepartureTime)
}

func TestTripRepository_CheckDriverConflict_ExcludeTripID(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	seedDriver(t, dbConn, "drv-1", "DRV-001", "John", "Doe", "1", "john@example.com")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	agg1 := newTestTripAgg("tr-1", "1", "TR-0001", nil, "route-1", now.Add(2*time.Hour), "Test", now)
	require.NoError(t, agg1.AssignDriver("drv-1", now))
	require.NoError(t, repo.Save(ctx, agg1))
	agg2 := newTestTripAgg("tr-2", "1", "TR-0002", nil, "route-1", now.Add(3*time.Hour), "Test2", now)
	require.NoError(t, agg2.AssignDriver("drv-1", now))
	require.NoError(t, repo.Save(ctx, agg2))
	// Without exclude -> 2 conflicts
	conflicts, err := repo.CheckDriverConflict(ctx, "drv-1", "1", "", now, nil)
	require.NoError(t, err)
	require.Len(t, conflicts, 2)
	// Exclude tr-1 -> only tr-2
	conflicts, err = repo.CheckDriverConflict(ctx, "drv-1", "1", "tr-1", now, nil)
	require.NoError(t, err)
	require.Len(t, conflicts, 1)
	require.Equal(t, "tr-2", conflicts[0].ID)
}

func TestTripRepository_CheckDriverConflict_TenantIsolation(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	seedDriver(t, dbConn, "drv-1", "DRV-001", "John", "Doe", "1", "john@example.com")
	seedDriver(t, dbConn, "drv-2", "DRV-001", "John", "Doe", "2", "john@example.com")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	agg := newTestTripAgg("tr-1", "1", "TR-0001", nil, "route-1", now.Add(2*time.Hour), "Test", now)
	require.NoError(t, agg.AssignDriver("drv-1", now))
	require.NoError(t, repo.Save(ctx, agg))
	conflicts, err := repo.CheckDriverConflict(ctx, "drv-1", "2", "", now, nil)
	require.NoError(t, err)
	require.Len(t, conflicts, 0)
	conflicts, err = repo.CheckDriverConflict(ctx, "drv-1", "1", "", now, nil)
	require.NoError(t, err)
	require.Len(t, conflicts, 1)
}

func TestTripRepository_CheckDriverConflict_StatusFiltering(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	seedDriver(t, dbConn, "drv-1", "DRV-001", "John", "Doe", "1", "john@example.com")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	// Draft trip should NOT be considered conflict
	aggDraft := newTestTripAgg("tr-draft", "1", "TR-DRAFT", nil, "route-1", now.Add(2*time.Hour), "Draft", now)
	// Stay draft but assign driver (still draft? Assign moves to assigned, so we insert directly via SQL to keep draft)
	_, err := dbConn.Exec(`INSERT INTO trips (id, trip_number, driver_id, route_id, departure_time, status, tenant_id, version) VALUES (?, ?, ?, ?, ?, ?, ?, 1)`, "tr-draft", "TR-DRAFT", "drv-1", "route-1", now.Add(2*time.Hour), "draft", "1")
	require.NoError(t, err)
	// Completed trip should NOT be conflict
	_, err = dbConn.Exec(`INSERT INTO trips (id, trip_number, driver_id, route_id, departure_time, status, tenant_id, version) VALUES (?, ?, ?, ?, ?, ?, ?, 1)`, "tr-completed", "TR-COMP", "drv-1", "route-1", now.Add(3*time.Hour), "completed", "1")
	require.NoError(t, err)
	// Cancelled trip should NOT be conflict
	_, err = dbConn.Exec(`INSERT INTO trips (id, trip_number, driver_id, route_id, departure_time, status, tenant_id, version) VALUES (?, ?, ?, ?, ?, ?, ?, 1)`, "tr-cancelled", "TR-CANC", "drv-1", "route-1", now.Add(4*time.Hour), "cancelled", "1")
	require.NoError(t, err)
	// Scheduled trip SHOULD be conflict
	aggSched := newTestTripAgg("tr-sched", "1", "TR-SCHED", nil, "route-1", now.Add(5*time.Hour), "Sched", now)
	require.NoError(t, aggSched.Schedule(now))
	require.NoError(t, aggSched.AssignDriver("drv-1", now))
	require.NoError(t, repo.Save(ctx, aggSched))
	conflicts, err := repo.CheckDriverConflict(ctx, "drv-1", "1", "", now, nil)
	require.NoError(t, err)
	require.Len(t, conflicts, 1)
	require.Equal(t, "tr-sched", conflicts[0].ID)
	// Also test with draft agg via Save would have been assigned, so not testing that path
	_ = aggDraft
}

func TestTripRepository_CheckDriverConflict_EmptyDriverID(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	conflicts, err := repo.CheckDriverConflict(ctx, "", "1", "", time.Now(), nil)
	require.NoError(t, err)
	require.Len(t, conflicts, 0)
}

func TestTripRepository_CheckDriverConflict_WithArrivalTime(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	seedDriver(t, dbConn, "drv-1", "DRV-001", "John", "Doe", "1", "john@example.com")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	arrival := now.Add(5 * time.Hour)
	_, err := dbConn.Exec(`INSERT INTO trips (id, trip_number, driver_id, route_id, departure_time, arrival_time, status, tenant_id, version) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1)`, "tr-arr", "TR-ARR", "drv-1", "route-1", now.Add(2*time.Hour), arrival, "scheduled", "1")
	require.NoError(t, err)
	conflicts, err := repo.CheckDriverConflict(ctx, "drv-1", "1", "", now, nil)
	require.NoError(t, err)
	require.Len(t, conflicts, 1)
	require.NotNil(t, conflicts[0].ArrivalTime)
	require.WithinDuration(t, arrival, *conflicts[0].ArrivalTime, time.Second)
	// Insert another without arrival
	_, err = dbConn.Exec(`INSERT INTO trips (id, trip_number, driver_id, route_id, departure_time, status, tenant_id, version) VALUES (?, ?, ?, ?, ?, ?, ?, 1)`, "tr-no-arr", "TR-NOARR", "drv-1", "route-1", now.Add(6*time.Hour), "assigned", "1")
	require.NoError(t, err)
	conflicts, err = repo.CheckDriverConflict(ctx, "drv-1", "1", "", now, nil)
	require.NoError(t, err)
	require.Len(t, conflicts, 2)
	var foundNoArr bool
	for _, c := range conflicts {
		if c.ID == "tr-no-arr" {
			require.Nil(t, c.ArrivalTime)
			foundNoArr = true
		}
	}
	require.True(t, foundNoArr)
}

func TestTripRepository_CheckDriverConflict_ErrorClosedDB(t *testing.T) {
	dbConn := setupTripTestDB(t)
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	_ = dbConn.Close()
	_, err := repo.CheckDriverConflict(ctx, "drv-1", "1", "", time.Now(), nil)
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// CheckVehicleConflict - 0% -> cover
// ---------------------------------------------------------------------------

func TestTripRepository_CheckVehicleConflict_NoConflictInitially(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	seedVehicle(t, dbConn, "veh-1", "MH01AB1234", "V-001", "1")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	conflicts, err := repo.CheckVehicleConflict(ctx, "veh-1", "1", "", time.Now(), nil)
	require.NoError(t, err)
	require.Len(t, conflicts, 0)
}

func TestTripRepository_CheckVehicleConflict_WithConflict(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	seedVehicle(t, dbConn, "veh-1", "MH01AB1234", "V-001", "1")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	_, err := dbConn.Exec(`INSERT INTO trips (id, trip_number, vehicle_id, route_id, departure_time, status, tenant_id, version) VALUES (?, ?, ?, ?, ?, ?, ?, 1)`, "tr-1", "TR-0001", "veh-1", "route-1", now.Add(2*time.Hour), "scheduled", "1")
	require.NoError(t, err)
	conflicts, err := repo.CheckVehicleConflict(ctx, "veh-1", "1", "", now, nil)
	require.NoError(t, err)
	require.Len(t, conflicts, 1)
	require.Equal(t, "tr-1", conflicts[0].ID)
}

func TestTripRepository_CheckVehicleConflict_ExcludeTripID(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	seedVehicle(t, dbConn, "veh-1", "MH01AB1234", "V-001", "1")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	_, err := dbConn.Exec(`INSERT INTO trips (id, trip_number, vehicle_id, route_id, departure_time, status, tenant_id, version) VALUES (?, ?, ?, ?, ?, ?, ?, 1)`, "tr-1", "TR-0001", "veh-1", "route-1", now.Add(2*time.Hour), "scheduled", "1")
	require.NoError(t, err)
	_, err = dbConn.Exec(`INSERT INTO trips (id, trip_number, vehicle_id, route_id, departure_time, status, tenant_id, version) VALUES (?, ?, ?, ?, ?, ?, ?, 1)`, "tr-2", "TR-0002", "veh-1", "route-1", now.Add(3*time.Hour), "assigned", "1")
	require.NoError(t, err)
	conflicts, err := repo.CheckVehicleConflict(ctx, "veh-1", "1", "", now, nil)
	require.NoError(t, err)
	require.Len(t, conflicts, 2)
	conflicts, err = repo.CheckVehicleConflict(ctx, "veh-1", "1", "tr-1", now, nil)
	require.NoError(t, err)
	require.Len(t, conflicts, 1)
	require.Equal(t, "tr-2", conflicts[0].ID)
}

func TestTripRepository_CheckVehicleConflict_TenantIsolation(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	seedVehicle(t, dbConn, "veh-1", "MH01AB1234", "V-001", "1")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	_, err := dbConn.Exec(`INSERT INTO trips (id, trip_number, vehicle_id, route_id, departure_time, status, tenant_id, version) VALUES (?, ?, ?, ?, ?, ?, ?, 1)`, "tr-1", "TR-0001", "veh-1", "route-1", now.Add(2*time.Hour), "scheduled", "1")
	require.NoError(t, err)
	conflicts, err := repo.CheckVehicleConflict(ctx, "veh-1", "2", "", now, nil)
	require.NoError(t, err)
	require.Len(t, conflicts, 0)
	conflicts, err = repo.CheckVehicleConflict(ctx, "veh-1", "1", "", now, nil)
	require.NoError(t, err)
	require.Len(t, conflicts, 1)
}

func TestTripRepository_CheckVehicleConflict_StatusFiltering(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	seedVehicle(t, dbConn, "veh-1", "MH01AB1234", "V-001", "1")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	_, err := dbConn.Exec(`INSERT INTO trips (id, trip_number, vehicle_id, route_id, departure_time, status, tenant_id, version) VALUES (?, ?, ?, ?, ?, ?, ?, 1)`, "tr-draft", "TR-DRAFT", "veh-1", "route-1", now.Add(2*time.Hour), "draft", "1")
	require.NoError(t, err)
	_, err = dbConn.Exec(`INSERT INTO trips (id, trip_number, vehicle_id, route_id, departure_time, status, tenant_id, version) VALUES (?, ?, ?, ?, ?, ?, ?, 1)`, "tr-comp", "TR-COMP", "veh-1", "route-1", now.Add(3*time.Hour), "completed", "1")
	require.NoError(t, err)
	_, err = dbConn.Exec(`INSERT INTO trips (id, trip_number, vehicle_id, route_id, departure_time, status, tenant_id, version) VALUES (?, ?, ?, ?, ?, ?, ?, 1)`, "tr-sched", "TR-SCHED", "veh-1", "route-1", now.Add(4*time.Hour), "scheduled", "1")
	require.NoError(t, err)
	conflicts, err := repo.CheckVehicleConflict(ctx, "veh-1", "1", "", now, nil)
	require.NoError(t, err)
	require.Len(t, conflicts, 1)
	require.Equal(t, "tr-sched", conflicts[0].ID)
}

func TestTripRepository_CheckVehicleConflict_EmptyVehicleID(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	conflicts, err := repo.CheckVehicleConflict(ctx, "", "1", "", time.Now(), nil)
	require.NoError(t, err)
	require.Len(t, conflicts, 0)
}

func TestTripRepository_CheckVehicleConflict_WithArrivalTime(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	seedVehicle(t, dbConn, "veh-1", "MH01AB1234", "V-001", "1")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	arrival := now.Add(5 * time.Hour)
	_, err := dbConn.Exec(`INSERT INTO trips (id, trip_number, vehicle_id, route_id, departure_time, arrival_time, status, tenant_id, version) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1)`, "tr-arr", "TR-ARR", "veh-1", "route-1", now.Add(2*time.Hour), arrival, "scheduled", "1")
	require.NoError(t, err)
	conflicts, err := repo.CheckVehicleConflict(ctx, "veh-1", "1", "", now, nil)
	require.NoError(t, err)
	require.Len(t, conflicts, 1)
	require.NotNil(t, conflicts[0].ArrivalTime)
	_, err = dbConn.Exec(`INSERT INTO trips (id, trip_number, vehicle_id, route_id, departure_time, status, tenant_id, version) VALUES (?, ?, ?, ?, ?, ?, ?, 1)`, "tr-no-arr", "TR-NOARR", "veh-1", "route-1", now.Add(6*time.Hour), "assigned", "1")
	require.NoError(t, err)
	conflicts, err = repo.CheckVehicleConflict(ctx, "veh-1", "1", "", now, nil)
	require.NoError(t, err)
	require.Len(t, conflicts, 2)
}

func TestTripRepository_CheckVehicleConflict_ErrorClosedDB(t *testing.T) {
	dbConn := setupTripTestDB(t)
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	_ = dbConn.Close()
	_, err := repo.CheckVehicleConflict(ctx, "veh-1", "1", "", time.Now(), nil)
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// SearchReadModels - improve 69.8% -> higher
// ---------------------------------------------------------------------------

func TestTripRepository_SearchReadModels_Pagination(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "Mumbai", "Delhi")
	seedRoute(t, dbConn, "route-2", "Pune", "Goa")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	agg1 := newTestTripAgg("tr-1", "1", "TR-0001", nil, "route-1", now.Add(72*time.Hour), "First", now)
	agg2 := newTestTripAgg("tr-2", "1", "TR-0002", nil, "route-1", now.Add(48*time.Hour), "Second", now)
	agg3 := newTestTripAgg("tr-3", "1", "TR-0003", nil, "route-2", now.Add(24*time.Hour), "Third", now)
	aggOther := newTestTripAgg("tr-4", "2", "TR-OTHER-1", nil, "route-1", now.Add(96*time.Hour), "OtherTenant", now)
	require.NoError(t, repo.Save(ctx, agg1))
	require.NoError(t, repo.Save(ctx, agg2))
	require.NoError(t, repo.Save(ctx, agg3))
	require.NoError(t, repo.Save(ctx, aggOther))
	models, total, err := repo.SearchReadModels(ctx, "1", "", "", 2, 0)
	require.NoError(t, err)
	require.Equal(t, int64(3), total)
	require.Len(t, models, 2)
	require.Equal(t, "TR-0001", models[0].TripNumber)
	require.Equal(t, "TR-0002", models[1].TripNumber)
	models, total, err = repo.SearchReadModels(ctx, "1", "", "", 2, 2)
	require.NoError(t, err)
	require.Equal(t, int64(3), total)
	require.Len(t, models, 1)
	require.Equal(t, "TR-0003", models[0].TripNumber)
	models, total, err = repo.SearchReadModels(ctx, "1", "", "", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(3), total)
	require.Len(t, models, 3)
	models, total, err = repo.SearchReadModels(ctx, "2", "", "", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, models, 1)
	require.Equal(t, "TR-OTHER-1", models[0].TripNumber)
	models, total, err = repo.SearchReadModels(ctx, "1", "", "", 10, 10)
	require.NoError(t, err)
	require.Equal(t, int64(3), total)
	require.Len(t, models, 0)
}

func TestTripRepository_SearchReadModels_FilterByQuery(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedDriver(t, dbConn, "drv-1", "DRV-001", "Alice", "Wonder", "1", "alice@example.com")
	seedDriver(t, dbConn, "drv-2", "DRV-002", "Bob", "Builder", "1", "bob@example.com")
	seedVehicle(t, dbConn, "veh-1", "MH01AB1234", "V-001", "1")
	seedRoute(t, dbConn, "route-1", "Mumbai", "Delhi")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	agg1 := newTestTripAgg("tr-1", "1", "TR-1001", nil, "route-1", now.Add(24*time.Hour), "", now)
	require.NoError(t, agg1.AssignDriver("drv-1", now))
	require.NoError(t, repo.Save(ctx, agg1))
	agg2 := newTestTripAgg("tr-2", "1", "TR-1002", nil, "route-1", now.Add(48*time.Hour), "", now)
	require.NoError(t, agg2.AssignDriver("drv-2", now))
	require.NoError(t, repo.Save(ctx, agg2))
	agg3 := newTestTripAgg("tr-3", "1", "TR-2001", nil, "route-1", now.Add(72*time.Hour), "", now)
	require.NoError(t, repo.Save(ctx, agg3))
	// Need to set vehicles for query test on registration number: update tr-1 to have vehicle
	_, err := dbConn.Exec(`UPDATE trips SET vehicle_id = ? WHERE id = ?`, "veh-1", "tr-1")
	require.NoError(t, err)
	models, total, err := repo.SearchReadModels(ctx, "1", "TR-1001", "", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, models, 1)
	require.Equal(t, "TR-1001", models[0].TripNumber)
	models, total, err = repo.SearchReadModels(ctx, "1", "TR-100", "", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	require.Len(t, models, 2)
	models, total, err = repo.SearchReadModels(ctx, "1", "Alice", "", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, models, 1)
	require.Equal(t, "Alice", models[0].DriverFirstName)
	models, total, err = repo.SearchReadModels(ctx, "1", "MH01AB1234", "", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, models, 1)
	require.Equal(t, "TR-1001", models[0].TripNumber)
	models, total, err = repo.SearchReadModels(ctx, "1", "Mumbai", "", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(3), total)
	require.Len(t, models, 3)
	models, total, err = repo.SearchReadModels(ctx, "1", "NONEXISTENTXYZ", "", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(0), total)
	require.Len(t, models, 0)
}

func TestTripRepository_SearchReadModels_FilterByStatus(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "Mumbai", "Delhi")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	agg1 := newTestTripAgg("tr-1", "1", "TR-0001", nil, "route-1", now.Add(24*time.Hour), "", now)
	agg2 := newTestTripAgg("tr-2", "1", "TR-0002", nil, "route-1", now.Add(48*time.Hour), "", now)
	require.NoError(t, agg2.Schedule(now))
	agg3 := newTestTripAgg("tr-3", "1", "TR-0003", nil, "route-1", now.Add(72*time.Hour), "", now)
	require.NoError(t, repo.Save(ctx, agg1))
	require.NoError(t, repo.Save(ctx, agg2))
	require.NoError(t, repo.Save(ctx, agg3))
	models, total, err := repo.SearchReadModels(ctx, "1", "", "draft", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	require.Len(t, models, 2)
	for _, m := range models {
		require.Equal(t, "draft", m.Status)
	}
	models, total, err = repo.SearchReadModels(ctx, "1", "", "scheduled", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, models, 1)
	require.Equal(t, "TR-0002", models[0].TripNumber)
	models, total, err = repo.SearchReadModels(ctx, "1", "", "", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(3), total)
	require.Len(t, models, 3)
	models, total, err = repo.SearchReadModels(ctx, "1", "TR-000", "draft", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	require.Len(t, models, 2)
	models, total, err = repo.SearchReadModels(ctx, "1", "", "cancelled", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(0), total)
	require.Len(t, models, 0)
}

func TestTripRepository_SearchReadModels_NullFields(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "Origin", "Destination")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	_, err := dbConn.Exec(`
		INSERT INTO trips (id, trip_number, booking_id, driver_id, vehicle_id, route_id, departure_time, status, tenant_id, version)
		VALUES ('trip-unassigned', 'TR-999', NULL, NULL, NULL, 'route-1', CURRENT_TIMESTAMP, 'scheduled', '1', 1)
	`)
	require.NoError(t, err)
	models, total, err := repo.SearchReadModels(ctx, "1", "", "", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, models, 1)
	require.Nil(t, models[0].VehicleID)
	require.Nil(t, models[0].DriverID)
	require.Nil(t, models[0].BookingID)
	require.Equal(t, "", models[0].DriverDisplayID)
	require.Equal(t, "", models[0].VehicleRegistrationNumber)
}

func TestTripRepository_SearchReadModels_ErrorClosedDB(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "Mumbai", "Delhi")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	_ = dbConn.Close()
	_, _, err := repo.SearchReadModels(ctx, "1", "", "", 10, 0)
	require.Error(t, err)
}

// Keep original second test name for compatibility but now using shared helper
func TestTripRepository_SearchReadModels_NullFields_Original(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "Origin", "Destination")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	_, err := dbConn.Exec(`
		INSERT INTO trips (id, trip_number, booking_id, driver_id, vehicle_id, route_id, departure_time, status, tenant_id, version)
		VALUES ('trip-unassigned-2', 'TR-998', NULL, NULL, NULL, 'route-1', CURRENT_TIMESTAMP, 'scheduled', '1', 1)
	`)
	require.NoError(t, err)
	models, total, err := repo.SearchReadModels(ctx, "1", "", "", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, models, 1)
	require.Nil(t, models[0].VehicleID)
	require.Nil(t, models[0].DriverID)
	require.Nil(t, models[0].BookingID)
}

// ---------------------------------------------------------------------------
// SearchReadModelsByDriver - 0% -> cover
// ---------------------------------------------------------------------------

func TestTripRepository_SearchReadModelsByDriver_EmptyDriverIDs(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	_, _, err := repo.SearchReadModelsByDriver(ctx, "1", []string{}, "", "", 10, 0)
	require.Error(t, err)
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func TestTripRepository_SearchReadModelsByDriver_UnresolvedDriverIDs(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	_, _, err := repo.SearchReadModelsByDriver(ctx, "1", []string{"nonexistent-id"}, "", "", 10, 0)
	require.Error(t, err)
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func TestTripRepository_SearchReadModelsByDriver_SuccessByID(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "Mumbai", "Delhi")
	seedDriver(t, dbConn, "drv-1", "DRV-001", "John", "Doe", "1", "john@example.com")
	seedDriver(t, dbConn, "drv-2", "DRV-002", "Bob", "Builder", "1", "bob@example.com")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	agg1 := newTestTripAgg("tr-1", "1", "TR-0001", nil, "route-1", now.Add(24*time.Hour), "", now)
	require.NoError(t, agg1.AssignDriver("drv-1", now))
	require.NoError(t, repo.Save(ctx, agg1))
	agg2 := newTestTripAgg("tr-2", "1", "TR-0002", nil, "route-1", now.Add(48*time.Hour), "", now)
	require.NoError(t, agg2.AssignDriver("drv-2", now))
	require.NoError(t, repo.Save(ctx, agg2))
	agg3 := newTestTripAgg("tr-3", "1", "TR-0003", nil, "route-1", now.Add(72*time.Hour), "", now)
	require.NoError(t, repo.Save(ctx, agg3))
	models, total, err := repo.SearchReadModelsByDriver(ctx, "1", []string{"drv-1"}, "", "", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, models, 1)
	require.Equal(t, "TR-0001", models[0].TripNumber)
	// multiple driver IDs
	models, total, err = repo.SearchReadModelsByDriver(ctx, "1", []string{"drv-1", "drv-2"}, "", "", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	require.Len(t, models, 2)
}

func TestTripRepository_SearchReadModelsByDriver_SuccessByDriverCode(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	seedDriver(t, dbConn, "drv-1", "DRV-001", "John", "Doe", "1", "john@example.com")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	agg := newTestTripAgg("tr-1", "1", "TR-0001", nil, "route-1", now.Add(24*time.Hour), "", now)
	require.NoError(t, agg.AssignDriver("drv-1", now))
	require.NoError(t, repo.Save(ctx, agg))
	// Pass driver_code DRV-001 instead of internal id drv-1
	models, total, err := repo.SearchReadModelsByDriver(ctx, "1", []string{"DRV-001"}, "", "", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, models, 1)
	require.Equal(t, "TR-0001", models[0].TripNumber)
}

func TestTripRepository_SearchReadModelsByDriver_SuccessByUserEmail(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	seedDriver(t, dbConn, "drv-1", "DRV-001", "John", "Doe", "1", "john@example.com")
	seedUser(t, dbConn, "user-1", "john@example.com")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	agg := newTestTripAgg("tr-1", "1", "TR-0001", nil, "route-1", now.Add(24*time.Hour), "", now)
	require.NoError(t, agg.AssignDriver("drv-1", now))
	require.NoError(t, repo.Save(ctx, agg))
	// Pass user ID user-1, which resolves via email join
	models, total, err := repo.SearchReadModelsByDriver(ctx, "1", []string{"user-1"}, "", "", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, models, 1)
	require.Equal(t, "TR-0001", models[0].TripNumber)
}

func TestTripRepository_SearchReadModelsByDriver_EmptyStringFiltered(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	seedDriver(t, dbConn, "drv-1", "DRV-001", "John", "Doe", "1", "john@example.com")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	agg := newTestTripAgg("tr-1", "1", "TR-0001", nil, "route-1", now.Add(24*time.Hour), "", now)
	require.NoError(t, agg.AssignDriver("drv-1", now))
	require.NoError(t, repo.Save(ctx, agg))
	// Include empty string which should be skipped, still resolves drv-1
	models, total, err := repo.SearchReadModelsByDriver(ctx, "1", []string{"", "drv-1", ""}, "", "", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, models, 1)
	// Only empty strings -> should result in ErrNoRows because no resolved IDs
	_, _, err = repo.SearchReadModelsByDriver(ctx, "1", []string{"", ""}, "", "", 10, 0)
	require.Error(t, err)
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func TestTripRepository_SearchReadModelsByDriver_TenantIsolation(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	seedRoute(t, dbConn, "route-2", "C", "D")
	seedDriver(t, dbConn, "drv-1", "DRV-001", "John", "Doe", "1", "john@example.com")
	seedDriver(t, dbConn, "drv-1-t2", "DRV-001", "John", "Doe", "2", "john@example.com")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	agg1 := newTestTripAgg("tr-1", "1", "TR-0001", nil, "route-1", now.Add(24*time.Hour), "", now)
	require.NoError(t, agg1.AssignDriver("drv-1", now))
	require.NoError(t, repo.Save(ctx, agg1))
	agg2 := newTestTripAgg("tr-2", "2", "TR-0002", nil, "route-2", now.Add(24*time.Hour), "", now)
	require.NoError(t, agg2.AssignDriver("drv-1-t2", now))
	require.NoError(t, repo.Save(ctx, agg2))
	// Query tenant 1 should only see tr-1
	models, total, err := repo.SearchReadModelsByDriver(ctx, "1", []string{"drv-1"}, "", "", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Equal(t, "TR-0001", models[0].TripNumber)
	// Query tenant 2 with its driver
	models, total, err = repo.SearchReadModelsByDriver(ctx, "2", []string{"drv-1-t2"}, "", "", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Equal(t, "TR-0002", models[0].TripNumber)
	// Cross tenant: drv-1 is tenant 1, query tenant 2 -> no resolution -> ErrNoRows
	_, _, err = repo.SearchReadModelsByDriver(ctx, "2", []string{"drv-1"}, "", "", 10, 0)
	require.Error(t, err)
	require.ErrorIs(t, err, sql.ErrNoRows)
}

func TestTripRepository_SearchReadModelsByDriver_FilterQueryAndStatus(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "Mumbai", "Delhi")
	seedRoute(t, dbConn, "route-2", "Pune", "Goa")
	seedDriver(t, dbConn, "drv-1", "DRV-001", "John", "Doe", "1", "john@example.com")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	agg1 := newTestTripAgg("tr-1", "1", "TR-1001", nil, "route-1", now.Add(24*time.Hour), "", now)
	require.NoError(t, agg1.AssignDriver("drv-1", now))
	require.NoError(t, repo.Save(ctx, agg1))
	agg2 := newTestTripAgg("tr-2", "1", "TR-1002", nil, "route-2", now.Add(48*time.Hour), "", now)
	require.NoError(t, agg2.AssignDriver("drv-1", now))
	require.NoError(t, agg2.Start(now))
	require.NoError(t, repo.Save(ctx, agg2))
	agg3 := newTestTripAgg("tr-3", "1", "TR-2001", nil, "route-1", now.Add(72*time.Hour), "", now)
	require.NoError(t, agg3.AssignDriver("drv-1", now))
	require.NoError(t, repo.Save(ctx, agg3))
	// Filter by query TR-100
	models, total, err := repo.SearchReadModelsByDriver(ctx, "1", []string{"drv-1"}, "TR-100", "", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	require.Len(t, models, 2)
	// Filter by status draft (agg1 and agg3 are assigned after driver assign? Actually AssignDriver sets assigned, agg2 scheduled. Let's check statuses)
	// agg1 after AssignDriver -> assigned, agg3 assigned, agg2 scheduled
	models, total, err = repo.SearchReadModelsByDriver(ctx, "1", []string{"drv-1"}, "", "assigned", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	for _, m := range models {
		require.Equal(t, "assigned", m.Status)
	}
	// started filter should match agg2
	models, total, err = repo.SearchReadModelsByDriver(ctx, "1", []string{"drv-1"}, "", "started", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Equal(t, "started", models[0].Status)
	// Combined query+status
	models, total, err = repo.SearchReadModelsByDriver(ctx, "1", []string{"drv-1"}, "TR-100", "assigned", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Equal(t, "TR-1001", models[0].TripNumber)
}

func TestTripRepository_SearchReadModelsByDriver_Pagination(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	seedDriver(t, dbConn, "drv-1", "DRV-001", "John", "Doe", "1", "john@example.com")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	for i := 1; i <= 5; i++ {
		id := "tr-" + string(rune('0'+i))
		num := "TR-000" + string(rune('0'+i))
		agg := newTestTripAgg(id, "1", num, nil, "route-1", now.Add(time.Duration(i)*24*time.Hour), "", now)
		require.NoError(t, agg.AssignDriver("drv-1", now))
		require.NoError(t, repo.Save(ctx, agg))
	}
	models, total, err := repo.SearchReadModelsByDriver(ctx, "1", []string{"drv-1"}, "", "", 2, 0)
	require.NoError(t, err)
	require.Equal(t, int64(5), total)
	require.Len(t, models, 2)
	models, total, err = repo.SearchReadModelsByDriver(ctx, "1", []string{"drv-1"}, "", "", 2, 2)
	require.NoError(t, err)
	require.Equal(t, int64(5), total)
	require.Len(t, models, 2)
	models, total, err = repo.SearchReadModelsByDriver(ctx, "1", []string{"drv-1"}, "", "", 10, 10)
	require.NoError(t, err)
	require.Equal(t, int64(5), total)
	require.Len(t, models, 0)
}

func TestTripRepository_SearchReadModelsByDriver_ErrorClosedDB(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	seedDriver(t, dbConn, "drv-1", "DRV-001", "John", "Doe", "1", "john@example.com")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	agg := newTestTripAgg("tr-1", "1", "TR-0001", nil, "route-1", now.Add(24*time.Hour), "", now)
	require.NoError(t, agg.AssignDriver("drv-1", now))
	require.NoError(t, repo.Save(ctx, agg))
	_ = dbConn.Close()
	_, _, err := repo.SearchReadModelsByDriver(ctx, "1", []string{"drv-1"}, "", "", 10, 0)
	require.Error(t, err)
}

func TestTripRepository_Q_WithTransaction(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	agg := newTestTripAgg("tr-tx", "1", "TR-TX-01", nil, "route-1", now.Add(2*time.Hour), "tx test", now)
	// Use transaction context to hit Q's Tx branch
	tx, err := dbConn.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	txCtx := repository.WithTxInContext(ctx, tx)
	// Q should return a Queries bound to tx
	qTx := repo.(*tripRepository).Q(txCtx)
	require.NotNil(t, qTx)
	qNoTx := repo.(*tripRepository).Q(ctx)
	require.NotNil(t, qNoTx)
	// Save inside transaction should use Q with tx
	require.NoError(t, repo.Save(txCtx, agg))
	require.Equal(t, int64(1), agg.Version)
	// Find inside transaction should also use tx
	found, err := repo.Find(txCtx, "tr-tx", "1")
	require.NoError(t, err)
	require.Equal(t, "TR-TX-01", found.TripNumber)
	require.NoError(t, tx.Commit())
	// After commit, find via normal context should succeed
	found2, err := repo.Find(ctx, "tr-tx", "1")
	require.NoError(t, err)
	require.Equal(t, "TR-TX-01", found2.TripNumber)
}

func TestTripRepository_Save_DuplicateTripNumber(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	agg1 := newTestTripAgg("tr-1", "1", "TR-0001", nil, "route-1", now.Add(2*time.Hour), "First", now)
	require.NoError(t, repo.Save(ctx, agg1))
	// Second trip with same trip number should fail on unique constraint
	agg2 := newTestTripAgg("tr-2", "1", "TR-0001", nil, "route-1", now.Add(3*time.Hour), "Duplicate", now)
	err := repo.Save(ctx, agg2)
	require.Error(t, err)
}

func TestTripRepository_SearchReadModels_AllNulls(t *testing.T) {
	dbConn := setupTripTestDB(t)
	seedRoute(t, dbConn, "route-1", "A", "B")
	repo := NewTripRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	// Insert via SQL with all nullable fields NULL
	_, err := dbConn.Exec(`INSERT INTO trips (id, trip_number, route_id, departure_time, status, tenant_id, version) VALUES (?, ?, ?, ?, ?, ?, 1)`, "tr-all-null", "TR-ALLNULL", "route-1", now, "draft", "1")
	require.NoError(t, err)
	models, total, err := repo.SearchReadModels(ctx, "1", "", "", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, models, 1)
	require.Nil(t, models[0].BookingID)
	require.Nil(t, models[0].DriverID)
	require.Nil(t, models[0].VehicleID)
	require.Nil(t, models[0].ArrivalTime)
	require.Equal(t, "", models[0].Remarks)
	require.Nil(t, models[0].StartedAt)
	require.Nil(t, models[0].ReachedPickupAt)
	require.Nil(t, models[0].InTransitAt)
	require.Nil(t, models[0].DeliveredAt)
	require.Nil(t, models[0].CompletedAt)
}

package sql

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"transport-app/internal/booking/domain/aggregate"
	"transport-app/internal/shared"
)

func TestBookingRepository_SaveAndFind(t *testing.T) {
	dbConn, err := sql.Open("sqlite", ":memory:")
	assert.NoError(t, err)
	defer func() { _ = dbConn.Close() }()

	// Set up simple sqlite schema for testing
	_, err = dbConn.Exec(`
		CREATE TABLE customers (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			company TEXT,
			phone TEXT,
			email TEXT,
			address TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE routes (
			id TEXT PRIMARY KEY,
			source TEXT NOT NULL,
			destination TEXT NOT NULL,
			distance REAL,
			duration REAL,
			standard_fare REAL,
			notes TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE bookings (
			id TEXT PRIMARY KEY,
			booking_number TEXT NOT NULL UNIQUE,
			customer_id TEXT NOT NULL,
			pickup_date DATETIME NOT NULL,
			route_id TEXT NOT NULL,
			vehicle_type TEXT NOT NULL,
			passengers INTEGER NOT NULL DEFAULT 1,
			cargo_weight REAL,
			price REAL NOT NULL,
			notes TEXT,
			status TEXT NOT NULL,
			tenant_id TEXT NOT NULL,
			version INTEGER NOT NULL DEFAULT 1,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			idempotency_key TEXT,
			FOREIGN KEY (customer_id) REFERENCES customers(id),
			FOREIGN KEY (route_id) REFERENCES routes(id)
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
	`)
	assert.NoError(t, err)

	_, err = dbConn.Exec(`INSERT INTO customers (id, name, company) VALUES ('cust-1', 'Alice', 'ACME')`)
	assert.NoError(t, err)
	_, err = dbConn.Exec(`INSERT INTO routes (id, source, destination) VALUES ('route-1', 'A', 'B')`)
	assert.NoError(t, err)

	repo := NewBookingRepository(dbConn)
	ctx := context.Background()

	price := shared.FloatToMoney(150.0, "USD")
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)

	agg := aggregate.NewBookingAggregate(
		"bk-1",
		"1",
		"BK-0001",
		"cust-1",
		"route-1",
		now.Add(24*time.Hour),
		"Van",
		4,
		nil,
		price,
		"Testing",
		now,
	)

	err = repo.Save(ctx, agg)
	assert.NoError(t, err)

	found, err := repo.Find(ctx, "bk-1", "1")
	assert.NoError(t, err)
	assert.Equal(t, agg.BookingNumber, found.BookingNumber)
	assert.Equal(t, agg.Notes, found.Notes)
	assert.Equal(t, agg.Status, found.Status)

	// Test read model
	readModel, err := repo.GetReadModel(ctx, "bk-1", "1")
	assert.NoError(t, err)
	assert.Equal(t, "Alice", readModel.CustomerName)
	assert.Equal(t, "A", readModel.RouteSource)
	assert.Equal(t, "B", readModel.RouteDestination)
}

// ---------------------------------------------------------------------------
// helpers for extended tests
// ---------------------------------------------------------------------------

const bookingTestSchema = `
		CREATE TABLE customers (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			company TEXT,
			phone TEXT,
			email TEXT,
			address TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE routes (
			id TEXT PRIMARY KEY,
			source TEXT NOT NULL,
			destination TEXT NOT NULL,
			distance REAL,
			duration REAL,
			standard_fare REAL,
			notes TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE bookings (
			id TEXT PRIMARY KEY,
			booking_number TEXT NOT NULL UNIQUE,
			customer_id TEXT NOT NULL,
			pickup_date DATETIME NOT NULL,
			route_id TEXT NOT NULL,
			vehicle_type TEXT NOT NULL,
			passengers INTEGER NOT NULL DEFAULT 1,
			cargo_weight REAL,
			price REAL NOT NULL,
			notes TEXT,
			status TEXT NOT NULL,
			tenant_id TEXT NOT NULL,
			version INTEGER NOT NULL DEFAULT 1,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			idempotency_key TEXT,
			FOREIGN KEY (customer_id) REFERENCES customers(id),
			FOREIGN KEY (route_id) REFERENCES routes(id)
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

func setupBookingTestDB(t *testing.T) *sql.DB {
	t.Helper()
	// Use unique in-memory identifier per test when possible to avoid cross-test
	// contamination with cache=shared. Fall back to exact string required by spec.
	dsn := "file:" + t.Name() + "?mode=memory&cache=shared&_pragma=journal_mode(WAL)"
	// If test name contains slash, sqlite URI needs escaping; replace slashes.
	// Simple sanitization: rely on modernc.org/sqlite handling; if fails, fallback.
	dbConn, err := sql.Open("sqlite", dsn)
	if err != nil {
		// fallback to spec literal
		dbConn, err = sql.Open("sqlite", ":memory:?cache=shared&_pragma=journal_mode(WAL)")
	}
	require.NoError(t, err)
	t.Cleanup(func() { _ = dbConn.Close() })

	_, err = dbConn.Exec(bookingTestSchema)
	require.NoError(t, err)
	return dbConn
}

func seedCustomer(t *testing.T, dbConn *sql.DB, id, name, company string) {
	t.Helper()
	// company can be empty -> NULL handling: pass nil if empty to test null path via direct insert
	if company == "" {
		_, err := dbConn.Exec(`INSERT INTO customers (id, name) VALUES (?, ?)`, id, name)
		require.NoError(t, err)
	} else {
		_, err := dbConn.Exec(`INSERT INTO customers (id, name, company) VALUES (?, ?, ?)`, id, name, company)
		require.NoError(t, err)
	}
}

func seedRoute(t *testing.T, dbConn *sql.DB, id, source, destination string) {
	t.Helper()
	_, err := dbConn.Exec(`INSERT INTO routes (id, source, destination) VALUES (?, ?, ?)`, id, source, destination)
	require.NoError(t, err)
}

func newTestBookingAgg(id, tenantID, bookingNumber, customerID, routeID string, pickupDate time.Time, vehicleType string, passengers int64, cargoWeight *float64, price shared.Money, notes string, now time.Time) *aggregate.BookingAggregate {
	return aggregate.NewBookingAggregate(
		aggregate.BookingID(id),
		shared.TenantID(tenantID),
		bookingNumber,
		customerID,
		routeID,
		pickupDate,
		vehicleType,
		passengers,
		cargoWeight,
		price,
		notes,
		now,
	)
}

// ---------------------------------------------------------------------------
// FindByNumber
// ---------------------------------------------------------------------------

func TestBookingRepository_FindByNumber_Success(t *testing.T) {
	dbConn := setupBookingTestDB(t)
	seedCustomer(t, dbConn, "cust-1", "Alice", "ACME")
	seedRoute(t, dbConn, "route-1", "Mumbai", "Delhi")

	repo := NewBookingRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	price := shared.FloatToMoney(150.0, "INR")

	agg := newTestBookingAgg("bk-1", "1", "BK-0001", "cust-1", "route-1", now.Add(24*time.Hour), "Van", 2, nil, price, "Notes A", now)
	require.NoError(t, repo.Save(ctx, agg))

	found, err := repo.FindByNumber(ctx, "BK-0001", "1")
	require.NoError(t, err)
	require.NotNil(t, found)
	require.Equal(t, "bk-1", string(found.ID))
	require.Equal(t, "BK-0001", found.BookingNumber)
	require.Equal(t, "cust-1", found.CustomerID)
	require.Equal(t, "Notes A", found.Notes)
	require.Equal(t, aggregate.BookingPending, found.Status)
	require.Equal(t, int64(1), found.Version)
}

func TestBookingRepository_FindByNumber_WithCargoWeightAndNotes(t *testing.T) {
	dbConn := setupBookingTestDB(t)
	seedCustomer(t, dbConn, "cust-1", "Alice", "ACME")
	seedRoute(t, dbConn, "route-1", "Mumbai", "Delhi")

	repo := NewBookingRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	price := shared.FloatToMoney(200.0, "INR")
	cargo := 12.5

	agg := newTestBookingAgg("bk-cargo", "1", "BK-CARGO-1", "cust-1", "route-1", now.Add(24*time.Hour), "Truck", 1, &cargo, price, "Fragile", now)
	require.NoError(t, repo.Save(ctx, agg))

	found, err := repo.FindByNumber(ctx, "BK-CARGO-1", "1")
	require.NoError(t, err)
	require.NotNil(t, found.CargoWeight)
	require.InDelta(t, 12.5, *found.CargoWeight, 0.001)
	require.Equal(t, "Fragile", found.Notes)
}

func TestBookingRepository_FindByNumber_NullOptionalFields(t *testing.T) {
	dbConn := setupBookingTestDB(t)
	seedCustomer(t, dbConn, "cust-2", "Bob", "") // company NULL
	seedRoute(t, dbConn, "route-1", "Mumbai", "Delhi")

	repo := NewBookingRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	price := shared.FloatToMoney(100.0, "INR")

	agg := newTestBookingAgg("bk-null", "1", "BK-NULL-1", "cust-2", "route-1", now.Add(24*time.Hour), "Van", 1, nil, price, "", now)
	require.NoError(t, repo.Save(ctx, agg))

	found, err := repo.FindByNumber(ctx, "BK-NULL-1", "1")
	require.NoError(t, err)
	require.Nil(t, found.CargoWeight)
	require.Equal(t, "", found.Notes)
}

func TestBookingRepository_FindByNumber_NotFound(t *testing.T) {
	dbConn := setupBookingTestDB(t)
	seedCustomer(t, dbConn, "cust-1", "Alice", "ACME")
	seedRoute(t, dbConn, "route-1", "Mumbai", "Delhi")

	repo := NewBookingRepository(dbConn)
	ctx := context.Background()

	_, err := repo.FindByNumber(ctx, "NON-EXISTENT", "1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "booking not found")
}

func TestBookingRepository_FindByNumber_TenantIsolation(t *testing.T) {
	dbConn := setupBookingTestDB(t)
	seedCustomer(t, dbConn, "cust-1", "Alice", "ACME")
	seedRoute(t, dbConn, "route-1", "Mumbai", "Delhi")

	repo := NewBookingRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	price := shared.FloatToMoney(150.0, "INR")

	agg := newTestBookingAgg("bk-1", "1", "BK-0001", "cust-1", "route-1", now.Add(24*time.Hour), "Van", 1, nil, price, "", now)
	require.NoError(t, repo.Save(ctx, agg))

	// Same booking number but different tenant should not be found
	_, err := repo.FindByNumber(ctx, "BK-0001", "2")
	require.Error(t, err)
	require.Contains(t, err.Error(), "booking not found")

	// Correct tenant still succeeds
	found, err := repo.FindByNumber(ctx, "BK-0001", "1")
	require.NoError(t, err)
	require.Equal(t, "bk-1", string(found.ID))
}

func TestBookingRepository_FindByNumber_ErrorClosedDB(t *testing.T) {
	dbConn := setupBookingTestDB(t)
	seedCustomer(t, dbConn, "cust-1", "Alice", "ACME")
	seedRoute(t, dbConn, "route-1", "Mumbai", "Delhi")

	repo := NewBookingRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	price := shared.FloatToMoney(150.0, "INR")

	agg := newTestBookingAgg("bk-1", "1", "BK-0001", "cust-1", "route-1", now.Add(24*time.Hour), "Van", 1, nil, price, "", now)
	require.NoError(t, repo.Save(ctx, agg))

	// Close DB to force error path
	_ = dbConn.Close()

	_, err := repo.FindByNumber(ctx, "BK-0001", "1")
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// SearchReadModels
// ---------------------------------------------------------------------------

func TestBookingRepository_SearchReadModels_Pagination(t *testing.T) {
	dbConn := setupBookingTestDB(t)
	seedCustomer(t, dbConn, "cust-1", "Alice", "ACME")
	seedCustomer(t, dbConn, "cust-2", "Bob", "BetaCorp")
	seedRoute(t, dbConn, "route-1", "Mumbai", "Delhi")
	seedRoute(t, dbConn, "route-2", "Pune", "Goa")

	repo := NewBookingRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	price := shared.FloatToMoney(100.0, "INR")

	// Create 3 bookings for tenant 1 with distinct pickup_dates for deterministic ordering (DESC)
	agg1 := newTestBookingAgg("bk-1", "1", "BK-0001", "cust-1", "route-1", now.Add(72*time.Hour), "Van", 2, nil, price, "First", now)
	agg2 := newTestBookingAgg("bk-2", "1", "BK-0002", "cust-2", "route-1", now.Add(48*time.Hour), "Truck", 1, nil, price, "Second", now)
	agg3 := newTestBookingAgg("bk-3", "1", "BK-0003", "cust-1", "route-2", now.Add(24*time.Hour), "Van", 3, nil, price, "Third", now)
	// Booking for other tenant should not be counted
	aggOther := newTestBookingAgg("bk-4", "2", "BK-OTHER-1", "cust-1", "route-1", now.Add(96*time.Hour), "Van", 1, nil, price, "OtherTenant", now)

	require.NoError(t, repo.Save(ctx, agg1))
	require.NoError(t, repo.Save(ctx, agg2))
	require.NoError(t, repo.Save(ctx, agg3))
	require.NoError(t, repo.Save(ctx, aggOther))

	// Page 1: limit 2 offset 0 -> 2 items, total 3, ordered by pickup_date DESC => bk-1 (72h) first
	models, total, err := repo.SearchReadModels(ctx, "1", "", "", 2, 0)
	require.NoError(t, err)
	require.Equal(t, int64(3), total)
	require.Len(t, models, 2)
	require.Equal(t, "BK-0001", models[0].BookingNumber) // latest pickup
	require.Equal(t, "BK-0002", models[1].BookingNumber)

	// Page 2: limit 2 offset 2 -> 1 item remaining
	models, total, err = repo.SearchReadModels(ctx, "1", "", "", 2, 2)
	require.NoError(t, err)
	require.Equal(t, int64(3), total)
	require.Len(t, models, 1)
	require.Equal(t, "BK-0003", models[0].BookingNumber)

	// Full: limit 10 offset 0 -> all 3
	models, total, err = repo.SearchReadModels(ctx, "1", "", "", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(3), total)
	require.Len(t, models, 3)

	// Tenant isolation: tenant 2 sees only its own
	models, total, err = repo.SearchReadModels(ctx, "2", "", "", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, models, 1)
	require.Equal(t, "BK-OTHER-1", models[0].BookingNumber)

	// Offset beyond total -> empty slice but count still correct
	models, total, err = repo.SearchReadModels(ctx, "1", "", "", 10, 10)
	require.NoError(t, err)
	require.Equal(t, int64(3), total)
	require.Len(t, models, 0)
}

func TestBookingRepository_SearchReadModels_FilterByQuery(t *testing.T) {
	dbConn := setupBookingTestDB(t)
	seedCustomer(t, dbConn, "cust-1", "Alice", "ACME")
	seedCustomer(t, dbConn, "cust-2", "Bob", "BetaCorp")
	seedCustomer(t, dbConn, "cust-3", "Charlie", "GammaLtd")
	seedRoute(t, dbConn, "route-1", "Mumbai", "Delhi")

	repo := NewBookingRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	price := shared.FloatToMoney(100.0, "INR")

	agg1 := newTestBookingAgg("bk-1", "1", "BK-1001", "cust-1", "route-1", now.Add(24*time.Hour), "Van", 1, nil, price, "", now)
	agg2 := newTestBookingAgg("bk-2", "1", "BK-1002", "cust-2", "route-1", now.Add(48*time.Hour), "Van", 1, nil, price, "", now)
	agg3 := newTestBookingAgg("bk-3", "1", "BK-2001", "cust-3", "route-1", now.Add(72*time.Hour), "Van", 1, nil, price, "", now)

	require.NoError(t, repo.Save(ctx, agg1))
	require.NoError(t, repo.Save(ctx, agg2))
	require.NoError(t, repo.Save(ctx, agg3))

	// Search by exact booking number fragment
	models, total, err := repo.SearchReadModels(ctx, "1", "BK-1001", "", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, models, 1)
	require.Equal(t, "BK-1001", models[0].BookingNumber)

	// Search by partial BK-100 matches two
	models, total, err = repo.SearchReadModels(ctx, "1", "BK-100", "", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	require.Len(t, models, 2)

	// Search by customer name "Alice" -> 1
	models, total, err = repo.SearchReadModels(ctx, "1", "Alice", "", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, models, 1)
	require.Equal(t, "Alice", models[0].CustomerName)

	// Search by company "BetaCorp" -> 1
	models, total, err = repo.SearchReadModels(ctx, "1", "BetaCorp", "", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, models, 1)
	require.Equal(t, "Bob", models[0].CustomerName)
	require.Equal(t, "BetaCorp", models[0].CustomerCompany)

	// Search with partial company "ACME" -> 1 (Alice)
	models, total, err = repo.SearchReadModels(ctx, "1", "ACME", "", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, models, 1)

	// Non-matching query -> empty
	models, total, err = repo.SearchReadModels(ctx, "1", "NONEXISTENTXYZ", "", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(0), total)
	require.Len(t, models, 0)
}

func TestBookingRepository_SearchReadModels_FilterByStatus(t *testing.T) {
	dbConn := setupBookingTestDB(t)
	seedCustomer(t, dbConn, "cust-1", "Alice", "ACME")
	seedRoute(t, dbConn, "route-1", "Mumbai", "Delhi")

	repo := NewBookingRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	price := shared.FloatToMoney(100.0, "INR")

	// agg1 stays pending
	agg1 := newTestBookingAgg("bk-1", "1", "BK-0001", "cust-1", "route-1", now.Add(24*time.Hour), "Van", 1, nil, price, "", now)
	// agg2 will be confirmed before save
	agg2 := newTestBookingAgg("bk-2", "1", "BK-0002", "cust-1", "route-1", now.Add(48*time.Hour), "Van", 1, nil, price, "", now)
	require.NoError(t, agg2.Confirm(now))
	// agg3 pending
	agg3 := newTestBookingAgg("bk-3", "1", "BK-0003", "cust-1", "route-1", now.Add(72*time.Hour), "Van", 1, nil, price, "", now)

	require.NoError(t, repo.Save(ctx, agg1))
	require.NoError(t, repo.Save(ctx, agg2))
	require.NoError(t, repo.Save(ctx, agg3))

	// Filter pending
	models, total, err := repo.SearchReadModels(ctx, "1", "", "pending", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	require.Len(t, models, 2)
	for _, m := range models {
		require.Equal(t, "pending", m.Status)
	}

	// Filter confirmed
	models, total, err = repo.SearchReadModels(ctx, "1", "", "confirmed", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, models, 1)
	require.Equal(t, "confirmed", models[0].Status)
	require.Equal(t, "BK-0002", models[0].BookingNumber)

	// No filter status "" -> all 3
	models, total, err = repo.SearchReadModels(ctx, "1", "", "", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(3), total)
	require.Len(t, models, 3)

	// Combined query + status: search "BK-000" + pending -> 2
	models, total, err = repo.SearchReadModels(ctx, "1", "BK-000", "pending", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	require.Len(t, models, 2)

	// Non-existent status -> 0
	models, total, err = repo.SearchReadModels(ctx, "1", "", "cancelled", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(0), total)
	require.Len(t, models, 0)
}

func TestBookingRepository_SearchReadModels_NullFields(t *testing.T) {
	dbConn := setupBookingTestDB(t)
	seedCustomer(t, dbConn, "cust-1", "Alice", "") // company NULL
	seedRoute(t, dbConn, "route-1", "Mumbai", "Delhi")

	repo := NewBookingRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	price := shared.FloatToMoney(100.0, "INR")

	// cargoWeight nil, notes empty -> both NULL in DB
	agg := newTestBookingAgg("bk-null", "1", "BK-NULL-1", "cust-1", "route-1", now.Add(24*time.Hour), "Van", 1, nil, price, "", now)
	require.NoError(t, repo.Save(ctx, agg))

	models, total, err := repo.SearchReadModels(ctx, "1", "", "", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, models, 1)
	require.Nil(t, models[0].CargoWeight)
	require.Equal(t, "", models[0].Notes)
	require.Equal(t, "", models[0].CustomerCompany) // NULL company mapped to ""

	// Now with cargo weight and notes populated
	cargo := 25.5
	agg2 := newTestBookingAgg("bk-full", "1", "BK-FULL-1", "cust-1", "route-1", now.Add(48*time.Hour), "Van", 1, &cargo, price, "Handle with care", now)
	require.NoError(t, repo.Save(ctx, agg2))

	models, total, err = repo.SearchReadModels(ctx, "1", "BK-FULL-1", "", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.NotNil(t, models[0].CargoWeight)
	require.InDelta(t, 25.5, *models[0].CargoWeight, 0.001)
	require.Equal(t, "Handle with care", models[0].Notes)
}

func TestBookingRepository_SearchReadModels_ErrorClosedDB(t *testing.T) {
	dbConn := setupBookingTestDB(t)
	seedCustomer(t, dbConn, "cust-1", "Alice", "ACME")
	seedRoute(t, dbConn, "route-1", "Mumbai", "Delhi")

	repo := NewBookingRepository(dbConn)
	ctx := context.Background()

	_ = dbConn.Close()

	_, _, err := repo.SearchReadModels(ctx, "1", "", "", 10, 0)
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// Delete
// ---------------------------------------------------------------------------

func TestBookingRepository_Delete_Success(t *testing.T) {
	dbConn := setupBookingTestDB(t)
	seedCustomer(t, dbConn, "cust-1", "Alice", "ACME")
	seedRoute(t, dbConn, "route-1", "Mumbai", "Delhi")

	repo := NewBookingRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	price := shared.FloatToMoney(150.0, "INR")

	agg := newTestBookingAgg("bk-1", "1", "BK-0001", "cust-1", "route-1", now.Add(24*time.Hour), "Van", 1, nil, price, "", now)
	require.NoError(t, repo.Save(ctx, agg))

	// Verify exists before delete
	found, err := repo.Find(ctx, "bk-1", "1")
	require.NoError(t, err)
	require.Equal(t, "BK-0001", found.BookingNumber)

	// Delete
	require.NoError(t, repo.Delete(ctx, "bk-1", "1"))

	// Find should now fail
	_, err = repo.Find(ctx, "bk-1", "1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "booking not found")

	// FindByNumber should also fail
	_, err = repo.FindByNumber(ctx, "BK-0001", "1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "booking not found")

	// Search count should be 0
	models, total, err := repo.SearchReadModels(ctx, "1", "", "", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(0), total)
	require.Len(t, models, 0)

	// GetReadModel should fail
	_, err = repo.GetReadModel(ctx, "bk-1", "1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "booking not found")
}

func TestBookingRepository_Delete_NonExistent(t *testing.T) {
	dbConn := setupBookingTestDB(t)
	seedCustomer(t, dbConn, "cust-1", "Alice", "ACME")
	seedRoute(t, dbConn, "route-1", "Mumbai", "Delhi")

	repo := NewBookingRepository(dbConn)
	ctx := context.Background()

	// Delete non-existent should not error (sql Exec returns nil even if 0 rows)
	err := repo.Delete(ctx, "non-existent-id", "1")
	require.NoError(t, err)
}

func TestBookingRepository_Delete_TenantIsolation(t *testing.T) {
	dbConn := setupBookingTestDB(t)
	seedCustomer(t, dbConn, "cust-1", "Alice", "ACME")
	seedRoute(t, dbConn, "route-1", "Mumbai", "Delhi")

	repo := NewBookingRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	price := shared.FloatToMoney(150.0, "INR")

	agg := newTestBookingAgg("bk-1", "1", "BK-0001", "cust-1", "route-1", now.Add(24*time.Hour), "Van", 1, nil, price, "", now)
	require.NoError(t, repo.Save(ctx, agg))

	// Attempt delete with wrong tenant -> should NOT delete
	require.NoError(t, repo.Delete(ctx, "bk-1", "2"))

	// Still findable with correct tenant
	found, err := repo.Find(ctx, "bk-1", "1")
	require.NoError(t, err)
	require.Equal(t, "BK-0001", found.BookingNumber)

	// Now delete with correct tenant -> succeeds
	require.NoError(t, repo.Delete(ctx, "bk-1", "1"))
	_, err = repo.Find(ctx, "bk-1", "1")
	require.Error(t, err)
}

func TestBookingRepository_Delete_ErrorClosedDB(t *testing.T) {
	dbConn := setupBookingTestDB(t)
	seedCustomer(t, dbConn, "cust-1", "Alice", "ACME")
	seedRoute(t, dbConn, "route-1", "Mumbai", "Delhi")

	repo := NewBookingRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	price := shared.FloatToMoney(150.0, "INR")

	agg := newTestBookingAgg("bk-1", "1", "BK-0001", "cust-1", "route-1", now.Add(24*time.Hour), "Van", 1, nil, price, "", now)
	require.NoError(t, repo.Save(ctx, agg))

	_ = dbConn.Close()

	err := repo.Delete(ctx, "bk-1", "1")
	require.Error(t, err)
}

func TestBookingRepository_Delete_ThenFindByNumberNotFound(t *testing.T) {
	dbConn := setupBookingTestDB(t)
	seedCustomer(t, dbConn, "cust-1", "Alice", "ACME")
	seedRoute(t, dbConn, "route-1", "Mumbai", "Delhi")

	repo := NewBookingRepository(dbConn)
	ctx := context.Background()
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	price := shared.FloatToMoney(150.0, "INR")

	agg1 := newTestBookingAgg("bk-1", "1", "BK-0001", "cust-1", "route-1", now.Add(24*time.Hour), "Van", 1, nil, price, "", now)
	agg2 := newTestBookingAgg("bk-2", "1", "BK-0002", "cust-1", "route-1", now.Add(48*time.Hour), "Van", 1, nil, price, "", now)
	require.NoError(t, repo.Save(ctx, agg1))
	require.NoError(t, repo.Save(ctx, agg2))

	require.NoError(t, repo.Delete(ctx, "bk-1", "1"))

	// bk-1 gone, bk-2 still there
	_, err := repo.FindByNumber(ctx, "BK-0001", "1")
	require.Error(t, err)

	found, err := repo.FindByNumber(ctx, "BK-0002", "1")
	require.NoError(t, err)
	require.Equal(t, "bk-2", string(found.ID))

	models, total, err := repo.SearchReadModels(ctx, "1", "", "", 10, 0)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, models, 1)
	require.Equal(t, "BK-0002", models[0].BookingNumber)
}

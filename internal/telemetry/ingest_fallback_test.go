package telemetry

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"transport-app/internal/telemetry/providers"

	"github.com/stretchr/testify/require"

	"transport-app/internal/events"
)

func seedFallbackDriver(t *testing.T, db *sql.DB, id, driverID, tenant string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, status, tenant_id)
		VALUES (?, ?, 'F', 'L', '000', 'available', ?)`, id, driverID, tenant)
	require.NoError(t, err)
}

func seedFallbackTrip(t *testing.T, db *sql.DB, id, num, driverID, vehicleID, tenant, status, departure string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO trips (id, trip_number, driver_id, vehicle_id, route_id, departure_time, status, tenant_id)
		VALUES (?, ?, ?, ?, 'r1', ?, ?, ?)`, id, num, driverID, vehicleID, departure, status, tenant)
	require.NoError(t, err)
}

// Unbound device resolves via the driver's active trip (probe by driver_id).
func TestFallbackVehicleID_ResolvesViaActiveTrip(t *testing.T) {
	db := newTestIngestorDB(t)
	ing := newTestIngestor(t, db, events.NewInMemoryBus())
	ctx := context.Background()

	seedFallbackDriver(t, db, "d-fb1", "user-fb1", "1")
	insertTestVehicle(t, db, "v-fb1")
	seedFallbackTrip(t, db, "t-fb1", "TRIP-FB1", "user-fb1", "v-fb1", "1", "started", "2026-09-01 10:00:00")

	if got := ing.fallbackVehicleID(ctx, "1", "user-fb1"); got != "v-fb1" {
		t.Fatalf("fallback = %q, want v-fb1", got)
	}
}

// Two active trips: latest departure wins, deterministically.
func TestFallbackVehicleID_LatestDepartureWins(t *testing.T) {
	db := newTestIngestorDB(t)
	ing := newTestIngestor(t, db, events.NewInMemoryBus())
	ctx := context.Background()

	seedFallbackDriver(t, db, "d-fb2", "user-fb2", "1")
	insertTestVehicle(t, db, "v-fb1")
	_, err := db.Exec(`INSERT INTO vehicles (id, registration_number, vehicle_number, vehicle_type, capacity, insurance_expiry, fitness_expiry, permit_expiry)
		VALUES ('v-fb2','REG-FB2','MH-02','truck',15,date('now','+1 year'),date('now','+1 year'),date('now','+1 year'))`)
	require.NoError(t, err)
	seedFallbackTrip(t, db, "t-fb-old", "TRIP-FB-OLD", "user-fb2", "v-fb1", "1", "started", "2026-09-01 08:00:00")
	seedFallbackTrip(t, db, "t-fb-new", "TRIP-FB-NEW", "user-fb2", "v-fb2", "1", "in_transit", "2026-09-01 12:00:00")

	for i := 0; i < 3; i++ {
		if got := ing.fallbackVehicleID(ctx, "1", "user-fb2"); got != "v-fb2" {
			t.Fatalf("attempt %d: fallback = %q, want v-fb2", i, got)
		}
	}
}

// The drivers.notes plate convention is gone: a note is not a vehicle.
func TestFallbackVehicleID_IgnoresNotesConvention(t *testing.T) {
	db := newTestIngestorDB(t)
	ing := newTestIngestor(t, db, events.NewInMemoryBus())
	ctx := context.Background()

	_, err := db.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, status, notes, tenant_id)
		VALUES ('d-fb3','user-fb3','F','L','000','available','MH-01-XXXX','1')`)
	require.NoError(t, err)
	insertTestVehicle(t, db, "v-fb1")

	if got := ing.fallbackVehicleID(ctx, "1", "user-fb3"); got != "" {
		t.Fatalf("fallback = %q, want empty (notes must not resolve)", got)
	}
}

// Tenant-scoped: a driver in another tenant never resolves here.
func TestFallbackVehicleID_TenantScoped(t *testing.T) {
	db := newTestIngestorDB(t)
	ing := newTestIngestor(t, db, events.NewInMemoryBus())
	ctx := context.Background()

	seedFallbackDriver(t, db, "d-fb4", "user-fb4", "2")
	insertTestVehicle(t, db, "v-fb1")
	seedFallbackTrip(t, db, "t-fb4", "TRIP-FB4", "user-fb4", "v-fb1", "2", "started", "2026-09-01 10:00:00")

	if got := ing.fallbackVehicleID(ctx, "1", "user-fb4"); got != "" {
		t.Fatalf("fallback = %q, want empty (cross-tenant)", got)
	}
}

func ownFrame(imei, msgID string, lat, lng float64, sats *int) providers.RawFrame {
	return providers.RawFrame{
		IMEI:          imei,
		DeviceTime:    time.Now().UTC().Add(-time.Second),
		Latitude:      lat,
		Longitude:     lng,
		Speed:         10,
		Provider:      "own",
		ProviderMsgID: msgID,
		Satellites:    sats,
	}
}

func latestLat(t *testing.T, db *sql.DB, vehicleID string) (float64, bool) {
	t.Helper()
	var lat float64
	err := db.QueryRow(`SELECT latitude FROM vehicle_latest_position WHERE vehicle_id = ?`, vehicleID).Scan(&lat)
	if err == sql.ErrNoRows {
		return 0, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return lat, true
}

func satPtr(n int) *int { return &n }

// H4 trust policy: mobile frames without Valid are judged by fix quality.
func TestFixTrusted_MobileFrames(t *testing.T) {
	db := newTestIngestorDB(t)
	ing := newTestIngestor(t, db, events.NewInMemoryBus())
	ctx := context.Background()
	insertTestVehicle(t, db, "v-trust")
	insertTestDevice(t, db, "IMEI-TRUST", DeviceStatusActive, strPtr("v-trust"))

	// Good mobile fix moves the live map.
	res, err := ing.IngestRawFrame(ctx, ownFrame("IMEI-TRUST", "t-good", 19.07, 72.87, satPtr(8)))
	requireNoErr(t, err)
	requireTrue(t, res.Accepted)
	lat, ok := latestLat(t, db, "v-trust")
	requireTrue(t, ok)
	if lat != 19.07 {
		t.Fatalf("latest lat = %v, want 19.07", lat)
	}

	// Null-island fix: history keeps it, live map must not move.
	res, err = ing.IngestRawFrame(ctx, ownFrame("IMEI-TRUST", "t-zero", 0, 0, satPtr(8)))
	requireNoErr(t, err)
	requireTrue(t, res.Accepted)
	lat, _ = latestLat(t, db, "v-trust")
	if lat != 19.07 {
		t.Fatalf("(0,0) frame overwrote live map: lat = %v", lat)
	}

	// Sub-3 satellites: same treatment.
	res, err = ing.IngestRawFrame(ctx, ownFrame("IMEI-TRUST", "t-sats", 19.08, 72.88, satPtr(1)))
	requireNoErr(t, err)
	requireTrue(t, res.Accepted)
	lat, _ = latestLat(t, db, "v-trust")
	if lat != 19.07 {
		t.Fatalf("1-sat frame overwrote live map: lat = %v", lat)
	}
}

func requireNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func requireTrue(t *testing.T, v bool) {
	t.Helper()
	if !v {
		t.Fatal("want true")
	}
}

// L9: unbound-device frames store NULL vehicle_id, never the ” sentinel.
func TestInsertPosition_UnboundStoresNull(t *testing.T) {
	db := newTestIngestorDB(t)
	ing := newTestIngestor(t, db, events.NewInMemoryBus())
	ctx := context.Background()
	insertTestDevice(t, db, "IMEI-NULL", DeviceStatusActive, nil)

	res, err := ing.IngestRawFrame(ctx, ownFrame("IMEI-NULL", "t-null", 19.07, 72.87, satPtr(8)))
	requireNoErr(t, err)
	requireTrue(t, res.Accepted)

	var n string
	err = db.QueryRow(`SELECT vehicle_id FROM telemetry_positions WHERE imei = 'IMEI-NULL'`).Scan(&n)
	if err == nil {
		t.Fatalf("vehicle_id = %q, want NULL", n)
	}
	if err != sql.ErrNoRows {
		// Scan into string errors on NULL with "converting NULL to string" — either way, not ''.
		t.Logf("scan err (NULL expected): %v", err)
	}
	var empties int
	requireNoErr(t, db.QueryRow(`SELECT COUNT(*) FROM telemetry_positions WHERE vehicle_id = ''`).Scan(&empties))
	if empties != 0 {
		t.Fatalf("%d ''-sentinel rows, want 0", empties)
	}
}

package telemetry

import (
	"context"
	"database/sql"
	"strings"
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

// A frame that carries no trip_id (every real source: mobile sync, MQTT
// device topic, TCP hardware) must still land attributed. Empty
// telemetry_snapshots.trip_id starves the dwell engine — pickup/drop zones
// and the ReachPickup/StartTransit gates need a trip — and makes
// /api/v1/telemetry/live?trip_id= return zero rows.
func TestIngestRawFrame_PopulatesTripIDFromActiveTrip(t *testing.T) {
	db := newTestIngestorDB(t)
	ing := newTestIngestor(t, db, events.NewInMemoryBus())
	ctx := context.Background()

	insertTestVehicle(t, db, "v-attrib")
	insertTestDevice(t, db, "IMEI-ATTRIB", DeviceStatusActive, strPtr("v-attrib"))
	seedFallbackDriver(t, db, "d-attrib", "user-attrib", "1")
	seedFallbackTrip(t, db, "t-attrib", "TRIP-ATTRIB", "d-attrib", "v-attrib", "1", "started", "2026-09-01 10:00:00")

	res, err := ing.IngestRawFrame(ctx, ownFrame("IMEI-ATTRIB", "attrib-msg", 19.07, 72.87, satPtr(8)))
	requireNoErr(t, err)
	requireTrue(t, res.Accepted)

	var got string
	requireNoErr(t, db.QueryRow(`SELECT trip_id FROM telemetry_snapshots WHERE vehicle_id = 'v-attrib'`).Scan(&got))
	if got != "t-attrib" {
		t.Fatalf("telemetry_snapshots.trip_id = %q, want t-attrib", got)
	}

	var positionTrip string
	requireNoErr(t, db.QueryRow(`SELECT trip_id FROM telemetry_positions WHERE imei = 'IMEI-ATTRIB'`).Scan(&positionTrip))
	if positionTrip != "t-attrib" {
		t.Fatalf("telemetry_positions.trip_id = %q, want t-attrib", positionTrip)
	}
}

// A trip the caller already supplied is never overwritten by the resolver —
// the frame is the authority on which trip it belongs to.
func TestIngestRawFrame_KeepsCallerSuppliedTripID(t *testing.T) {
	db := newTestIngestorDB(t)
	ing := newTestIngestor(t, db, events.NewInMemoryBus())
	ctx := context.Background()

	insertTestVehicle(t, db, "v-caller")
	insertTestDevice(t, db, "IMEI-CALLER", DeviceStatusActive, strPtr("v-caller"))
	seedFallbackDriver(t, db, "d-caller", "user-caller", "1")
	seedFallbackTrip(t, db, "t-caller-active", "TRIP-CALLER-ACTIVE", "d-caller", "v-caller", "1", "started", "2026-09-01 10:00:00")

	frame := ownFrame("IMEI-CALLER", "caller-msg", 19.07, 72.87, satPtr(8))
	frame.TripID = "t-caller-supplied"
	res, err := ing.IngestRawFrame(ctx, frame)
	requireNoErr(t, err)
	requireTrue(t, res.Accepted)

	var got string
	requireNoErr(t, db.QueryRow(`SELECT trip_id FROM telemetry_snapshots WHERE vehicle_id = 'v-caller'`).Scan(&got))
	if got != "t-caller-supplied" {
		t.Fatalf("telemetry_snapshots.trip_id = %q, want t-caller-supplied", got)
	}
}

// No trip on the road: attribution stays empty rather than latching onto a
// cancelled or completed trip.
func TestIngestRawFrame_NoActiveTripStaysEmpty(t *testing.T) {
	db := newTestIngestorDB(t)
	ing := newTestIngestor(t, db, events.NewInMemoryBus())
	ctx := context.Background()

	insertTestVehicle(t, db, "v-none")
	insertTestDevice(t, db, "IMEI-NONE", DeviceStatusActive, strPtr("v-none"))
	seedFallbackDriver(t, db, "d-none", "user-none", "1")
	seedFallbackTrip(t, db, "t-done", "TRIP-DONE", "d-none", "v-none", "1", "completed", "2026-09-01 10:00:00")

	res, err := ing.IngestRawFrame(ctx, ownFrame("IMEI-NONE", "none-msg", 19.07, 72.87, satPtr(8)))
	requireNoErr(t, err)
	requireTrue(t, res.Accepted)

	// COALESCE: unattributed frames store SQL NULL (FK columns never take the
	// '' sentinel), and readers already filter IS NOT NULL AND != ''.
	var got string
	requireNoErr(t, db.QueryRow(`SELECT COALESCE(trip_id, '') FROM telemetry_snapshots WHERE vehicle_id = 'v-none'`).Scan(&got))
	if got != "" {
		t.Fatalf("telemetry_snapshots.trip_id = %q, want empty (completed trip is not active)", got)
	}
}

// Ratchet: the accept-offer flow stores trips.driver_id as the AUTH USER ID
// (dispatch_service.go writes session.UserID), and production has an empty
// drivers table, so the phone publishes its user id as the device identity.
// Neither fallback probe joined that shape, so the frame resolved no vehicle at
// all: no snapshot row, no live marker, and the dwell engine still starved.
func TestFallbackVehicleID_ResolvesViaAuthUserIDTrip(t *testing.T) {
	db := newTestIngestorDB(t)
	ing := newTestIngestor(t, db, events.NewInMemoryBus())
	ctx := context.Background()

	_, err := db.Exec(`INSERT INTO users (id, email, password_hash, name, tenant_id)
		VALUES ('usr-fb5', 'drv@fb5.test', 'x', 'Driver', '1')`)
	require.NoError(t, err)
	insertTestVehicle(t, db, "v-fb5")
	seedFallbackTrip(t, db, "t-fb5", "TRIP-FB5", "usr-fb5", "v-fb5", "1", "started", "2026-09-01 10:00:00")

	if got := ing.fallbackVehicleID(ctx, "1", "usr-fb5"); got != "v-fb5" {
		t.Fatalf("fallback = %q, want v-fb5 (trip assigned by auth user id)", got)
	}
}

// Ratchet: two drivers share a vehicle and both trips are active. The frame's
// driver is known (the MQTT driver topic resolves it to drivers.id), so the
// trip to attribute to is that driver's own — not the latest departure on the
// vehicle, which is the other driver's. trips.driver_id FKs to drivers(id), so
// the trips are keyed by row id here.
func TestMQTTIngest_AttributesFrameToItsOwnDriversTrip(t *testing.T) {
	db := newTestIngestorDBFK(t)
	insertTestVehicle(t, db, "vh-share")
	// FKs are ON here, so the trips' route must exist (seedFallbackTrip pins r1).
	_, err := db.Exec(`INSERT INTO routes (id, source, destination, distance, estimated_hours, standard_fare, tenant_id)
		VALUES ('r1', 'Pune', 'Mumbai', 150, 3, 1000, '1')`)
	require.NoError(t, err)
	// phones are UNIQUE — seed inline rather than through the shared helper.
	for _, d := range []struct{ id, code, phone string }{
		{"drv-sh-A", "DRV-SH-A", "000000000001"},
		{"drv-sh-B", "DRV-SH-B", "000000000002"},
	} {
		_, err = db.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, status, tenant_id)
			VALUES (?, ?, 'F', 'L', ?, 'available', '1')`, d.id, d.code, d.phone)
		require.NoError(t, err)
	}
	// A departs earlier; B's trip is the latest departure on the shared vehicle.
	seedFallbackTrip(t, db, "t-sh-A", "TRIP-SH-A", "drv-sh-A", "vh-share", "1", "started", "2026-09-01 08:00:00")
	seedFallbackTrip(t, db, "t-sh-B", "TRIP-SH-B", "drv-sh-B", "vh-share", "1", "assigned", "2026-09-01 12:00:00")

	h := NewMQTTIngestHandler(newTestIngestor(t, db, nil), nil)
	h.HandleMessage(context.Background(), "avandab/telemetry/drivers/DRV-SH-A/gps",
		[]byte(`{"driver_id":"DRV-SH-A","latitude":19.07,"longitude":72.83,"timestamp":"2026-09-27T05:00:00Z"}`))

	var got string
	require.NoError(t, db.QueryRow(
		`SELECT COALESCE(trip_id, '') FROM telemetry_snapshots WHERE vehicle_id = 'vh-share'`).Scan(&got))
	if got != "t-sh-A" {
		t.Fatalf("telemetry_snapshots.trip_id = %q, want t-sh-A (the frame's own driver's trip)", got)
	}
}

// Ratchet: trips.driver_id carries three identity shapes in this codebase —
// the drivers row id, the human driver code, and (accept-offer) the auth user
// id. Attribution must resolve all of them to the frame's driver, and must
// refuse a trip that provably belongs to somebody else.
func TestActiveTripID_ResolvesEveryDriverIdentityShape(t *testing.T) {
	db := newTestIngestorDB(t)
	ing := newTestIngestor(t, db, events.NewInMemoryBus())
	ctx := context.Background()
	insertTestVehicle(t, db, "v-id")
	// d-row/DRV-ID: the same human, reachable as row id, as code, and (via the
	// email bridge) as the auth user id their trip may carry.
	_, err := db.Exec(`INSERT INTO users (id, email, password_hash, name, tenant_id)
		VALUES ('usr-id', 'a@id.test', 'x', 'Driver', '1')`)
	require.NoError(t, err)
	for _, d := range []struct{ id, code, phone, email string }{
		{"d-row", "DRV-ID", "000000000010", "a@id.test"},
		{"d-other", "DRV-OTHER", "000000000011", "b@id.test"},
	} {
		_, err = db.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, email, status, tenant_id)
			VALUES (?, ?, 'F', 'L', ?, ?, 'available', '1')`, d.id, d.code, d.phone, d.email)
		require.NoError(t, err)
	}

	// Trip keyed by the human driver code.
	seedFallbackTrip(t, db, "t-code", "TRIP-CODE", "DRV-ID", "v-id", "1", "started", "2026-09-01 08:00:00")
	if got := ing.activeTripID(ctx, "1", "v-id", "d-row"); got != "t-code" {
		t.Fatalf("code-keyed trip: got %q, want t-code", got)
	}

	// Same trip keyed by the auth user id (what executeAcceptOffer writes).
	_, err = db.Exec(`UPDATE trips SET driver_id = 'usr-id' WHERE id = 't-code'`)
	require.NoError(t, err)
	if got := ing.activeTripID(ctx, "1", "v-id", "d-row"); got != "t-code" {
		t.Fatalf("user-id-keyed trip: got %q, want t-code", got)
	}

	// Another driver's trip is never ours, whatever shape it is stored in.
	seedFallbackTrip(t, db, "t-foreign", "TRIP-FOREIGN", "DRV-OTHER", "v-id", "1", "assigned", "2026-09-01 12:00:00")
	if got := ing.activeTripID(ctx, "1", "v-id", "d-row"); got == "t-foreign" {
		t.Fatal("attributed the frame to another driver's trip")
	}

	// No identity on the frame (hardware, user-identity phones): the vehicle is
	// all we have, exactly as before — no regression for that shape.
	if got := ing.activeTripID(ctx, "1", "v-id", ""); got == "" {
		t.Fatal("driver-less frame lost its vehicle-level attribution")
	}
}

// The identity arms must not cost a table scan: this runs per accepted frame,
// inside the ingest transaction. Asserts the plan of the exact SQL that runs.
func TestFallbackVehicleID_QueryPlanStaysIndexed(t *testing.T) {
	db := newTestIngestorDB(t)
	rows, err := db.Query("EXPLAIN QUERY PLAN "+fallbackVehicleSQL, "some-identity", "1")
	require.NoError(t, err)
	defer rows.Close()

	var scanned []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		require.NoError(t, rows.Scan(&id, &parent, &notUsed, &detail))
		if strings.Contains(detail, "USING INDEX") || strings.Contains(detail, "USING COVERING INDEX") ||
			strings.Contains(detail, "BLOOM FILTER") || strings.Contains(detail, "TEMP B-TREE") ||
			strings.HasPrefix(detail, "LIST SUBQUERY") || strings.HasPrefix(detail, "MULTI-INDEX OR") ||
			strings.HasPrefix(detail, "INDEX ") {
			continue
		}
		scanned = append(scanned, detail)
	}
	require.NoError(t, rows.Err())
	if len(scanned) > 0 {
		t.Fatalf("fallback vehicle lookup stops being indexed: %v", scanned)
	}
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

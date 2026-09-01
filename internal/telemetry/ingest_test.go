package telemetry

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"transport-app/internal/events"
	"transport-app/internal/shared/id"
	"transport-app/internal/shared/uow"
	"transport-app/internal/telemetry/providers"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestIngestorDB creates an in-memory SQLite DB with all migrations applied
// and returns it with a fresh goose dialect set.
func newTestIngestorDB(t *testing.T) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("test_telem_%s_%d", strings.ReplaceAll(t.Name(), "/", "_"), time.Now().UnixNano())
	db, err := sql.Open("sqlite", "file:"+name+"?mode=memory&cache=shared&_pragma=journal_mode(WAL)")
	require.NoError(t, err)
	_ = goose.SetDialect("sqlite")
	require.NoError(t, goose.Up(db, "../../db/migrations"))
	// 00103 enforces tenant_id FKs — seed common test tenants used across telemetry tests.
	_, _ = db.Exec(`INSERT OR IGNORE INTO tenants (id, name, slug) VALUES ('1','Default','default'), ('2','Tenant 2','tenant-2'), ('7','Tenant 7','tenant-7'), ('9','Tenant 9','tenant-9'), ('other-tenant','Other Tenant','other-tenant'), ('tenant-1','Test Tenant 1','tenant-1'), ('tenant-a','Tenant A','tenant-a'), ('tenant-b','Tenant B','tenant-b')`)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// insertTestDevice inserts a device row and returns its IMEI.
func insertTestDevice(t *testing.T, db *sql.DB, imei, status string, vehicleID *string) {
	t.Helper()
	var vID, custID interface{} = vehicleID, nil
	if vehicleID != nil {
		vID = *vehicleID
	}
	_, err := db.Exec(`INSERT INTO telemetry_devices
		(id, tenant_id, imei, serial_number, device_type, status, vehicle_id, customer_id, device_secret_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"dev1", "1", imei, "SN-001", "hardware", status, vID, custID, "hash")
	require.NoError(t, err)
}

// insertTestVehicle inserts a vehicle row for FK compliance.
func insertTestVehicle(t *testing.T, db *sql.DB, vid string) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO vehicles
		(id, registration_number, vehicle_number, vehicle_type, capacity, insurance_expiry, fitness_expiry, permit_expiry)
		VALUES (?, ?, ?, ?, ?, date('now','+1 year'), date('now','+1 year'), date('now','+1 year'))`,
		vid, "REG-001", "MH-01-XXXX", "truck", 15)
	require.NoError(t, err)
}

// testAudit is a no-op AuditLogger for tests that don't assert on audit logs.
type testAudit struct {
	mu      sync.Mutex
	actions []auditCall
}
type auditCall struct {
	action, table, record string
}

func (a *testAudit) LogAction(ctx context.Context, action, tableName, recordID string, oldValues, newValues map[string]interface{}) error {
	a.mu.Lock()
	a.actions = append(a.actions, auditCall{action, tableName, recordID})
	a.mu.Unlock()
	return nil
}

func newTestIngestor(t *testing.T, db *sql.DB, bus events.EventBus) *Ingestor {
	if bus == nil {
		bus = events.NewInMemoryBus()
	}
	return NewIngestor(
		db,
		uow.NewSQLUnitOfWork(db),
		bus,
		id.NewUUIDGenerator(),
		&testAudit{},
		IngestConfig{
			OdometerMaxRegressionKM: 1.0,
			FuelClampDeltaPct:       5.0,
		},
	)
}

// Test 1: valid active device → all tables written in one tx
func TestIngestor_ValidFramePipeline(t *testing.T) {
	db := newTestIngestorDB(t)
	insertTestVehicle(t, db, "v-1")
	insertTestDevice(t, db, "IMEI001", DeviceStatusActive, strPtr("v-1"))

	bus := events.NewInMemoryBus()
	ing := newTestIngestor(t, db, bus)

	var snapshotReceived sync.Map
	bus.Subscribe(BusEventTelemetrySnapshot, func(ctx context.Context, e events.Event) error {
		snapshotReceived.Store("event", e)
		return nil
	})

	frame := providers.RawFrame{
		IMEI:          "IMEI001",
		DeviceTime:    time.Now().UTC().Add(-2 * time.Second),
		Latitude:      19.07,
		Longitude:     72.83,
		Speed:         45.0,
		Provider:      "own",
		ProviderMsgID: "msg-001",
	}
	res, err := ing.IngestRawFrame(context.Background(), frame)
	require.NoError(t, err)
	assert.True(t, res.Accepted)
	assert.False(t, res.Deduped)

	// Verify raw_events
	var rawID string
	err = db.QueryRow(`SELECT id FROM telemetry_raw_events WHERE imei = ? AND provider_msg_id = ?`, "IMEI001", "msg-001").Scan(&rawID)
	require.NoError(t, err)

	// Verify positions
	var lat float64
	err = db.QueryRow(`SELECT latitude FROM telemetry_positions WHERE imei = ? AND raw_event_id = ?`, "IMEI001", rawID).Scan(&lat)
	require.NoError(t, err)
	assert.Equal(t, 19.07, lat)

	// Verify latest position
	var latestLat float64
	err = db.QueryRow(`SELECT latitude FROM vehicle_latest_position WHERE vehicle_id = ?`, "v-1").Scan(&latestLat)
	require.NoError(t, err)
	assert.Equal(t, 19.07, latestLat)

	// Verify snapshot
	var snapCount int
	err = db.QueryRow(`SELECT count(*) FROM telemetry_snapshots WHERE vehicle_id = ?`, "v-1").Scan(&snapCount)
	require.NoError(t, err)
	assert.Equal(t, 1, snapCount)

	// Verify outbox
	var obCount int
	err = db.QueryRow(`SELECT count(*) FROM outbox_events WHERE aggregate_type = ? AND event_type = ?`, "Vehicle", EventTypePosition).Scan(&obCount)
	require.NoError(t, err)
	assert.Equal(t, 1, obCount)

	// Verify dual-write bus publish fired
	_, ok := snapshotReceived.Load("event")
	assert.True(t, ok, "dual-write fast-path should have published telemetry.snapshot")
}

// Test 2: replay same provider_msg_id → Deduped=true, no duplicate position
func TestIngestor_DedupReplay(t *testing.T) {
	db := newTestIngestorDB(t)
	insertTestVehicle(t, db, "v-2")
	insertTestDevice(t, db, "IMEI002", DeviceStatusActive, strPtr("v-2"))

	bus := events.NewInMemoryBus()
	ing := newTestIngestor(t, db, bus)

	frame := providers.RawFrame{
		IMEI:          "IMEI002",
		DeviceTime:    time.Now().UTC(),
		Latitude:      19.0,
		Longitude:     72.0,
		Speed:         30.0,
		Provider:      "own",
		ProviderMsgID: "dup-msg",
	}

	// First ingest
	res1, err := ing.IngestRawFrame(context.Background(), frame)
	require.NoError(t, err)
	assert.True(t, res1.Accepted)
	assert.False(t, res1.Deduped)

	// Replay with same provider_msg_id
	res2, err := ing.IngestRawFrame(context.Background(), frame)
	require.NoError(t, err)
	assert.True(t, res2.Accepted)
	assert.True(t, res2.Deduped, "replay should be deduped")

	// Only one position row
	var count int
	err = db.QueryRow(`SELECT count(*) FROM telemetry_positions WHERE imei = ?`, "IMEI002").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

// Test 3: unknown IMEI → quarantine, zero position writes
func TestIngestor_UnknownIMEIQuarantine(t *testing.T) {
	db := newTestIngestorDB(t)
	bus := events.NewInMemoryBus()
	ing := newTestIngestor(t, db, bus)

	frame := providers.RawFrame{
		IMEI:       "UNKNOWN",
		DeviceTime: time.Now().UTC(),
		Latitude:   19.0,
		Longitude:  72.0,
		Provider:   "own",
	}
	res, err := ing.IngestRawFrame(context.Background(), frame)
	require.NoError(t, err)
	assert.True(t, res.Quarantined || res.Accepted == false,
		"unknown device should be quarantined")

	var qCount int
	err = db.QueryRow(`SELECT count(*) FROM device_quarantine WHERE imei = ? AND reason = ?`, "UNKNOWN", QuarantineReasonUnknownDevice).Scan(&qCount)
	require.NoError(t, err)
	assert.Equal(t, 1, qCount)

	var posCount int
	err = db.QueryRow(`SELECT count(*) FROM telemetry_positions WHERE imei = ?`, "UNKNOWN").Scan(&posCount)
	require.NoError(t, err)
	assert.Equal(t, 0, posCount)
}

// Test 4: retired device → quarantine
func TestIngestor_RetiredDeviceQuarantine(t *testing.T) {
	db := newTestIngestorDB(t)
	insertTestDevice(t, db, "IMEI003", DeviceStatusRetired, nil)
	bus := events.NewInMemoryBus()
	ing := newTestIngestor(t, db, bus)

	frame := providers.RawFrame{
		IMEI:       "IMEI003",
		DeviceTime: time.Now().UTC(),
		Latitude:   19.0,
		Longitude:  72.0,
		Provider:   "own",
	}
	res, err := ing.IngestRawFrame(context.Background(), frame)
	require.NoError(t, err)
	assert.False(t, res.Accepted, "retired device should be quarantined")

	var qCount int
	err = db.QueryRow(`SELECT count(*) FROM device_quarantine WHERE imei = ? AND reason = ?`, "IMEI003", QuarantineReasonRetiredDevice).Scan(&qCount)
	require.NoError(t, err)
	assert.Equal(t, 1, qCount)
}

// Test 5: quarantined device → quarantine
func TestIngestor_QuarantinedDevice(t *testing.T) {
	db := newTestIngestorDB(t)
	insertTestDevice(t, db, "IMEI004", DeviceStatusQuarantined, nil)
	bus := events.NewInMemoryBus()
	ing := newTestIngestor(t, db, bus)

	frame := providers.RawFrame{
		IMEI:       "IMEI004",
		DeviceTime: time.Now().UTC(),
		Latitude:   19.0,
		Longitude:  72.0,
		Provider:   "own",
	}
	res, err := ing.IngestRawFrame(context.Background(), frame)
	require.NoError(t, err)
	assert.False(t, res.Accepted)

	var qCount int
	err = db.QueryRow(`SELECT count(*) FROM device_quarantine WHERE imei = ? AND reason = ?`, "IMEI004", QuarantineReasonQuarantinedDevice).Scan(&qCount)
	require.NoError(t, err)
	assert.Equal(t, 1, qCount)
}

// Test 6: odometer rollback guard fires → position uses last known odometer,
// audit log written
func TestIngestor_OdometerRollbackGuard(t *testing.T) {
	db := newTestIngestorDB(t)
	insertTestVehicle(t, db, "v-3")
	insertTestDevice(t, db, "IMEI005", DeviceStatusActive, strPtr("v-3"))

	audit := &testAudit{}
	ing := NewIngestor(db, uow.NewSQLUnitOfWork(db), events.NewInMemoryBus(),
		id.NewUUIDGenerator(), audit,
		IngestConfig{OdometerMaxRegressionKM: 1.0, FuelClampDeltaPct: 5.0})

	now := time.Now().UTC()
	frame1 := providers.RawFrame{
		IMEI:          "IMEI005",
		DeviceTime:    now,
		Latitude:      19.0,
		Longitude:     72.0,
		Odometer:      floatPtr(1000.0),
		Provider:      "own",
		ProviderMsgID: "msg-odd-1",
	}
	_, err := ing.IngestRawFrame(context.Background(), frame1)
	require.NoError(t, err)

	// Frame 2: odometer regresses 50km (> 1.0 threshold)
	frame2 := providers.RawFrame{
		IMEI:          "IMEI005",
		DeviceTime:    now.Add(5 * time.Second),
		Latitude:      19.1,
		Longitude:     72.1,
		Odometer:      floatPtr(950.0), // regression of 50
		Provider:      "own",
		ProviderMsgID: "msg-odd-2",
	}
	res, err := ing.IngestRawFrame(context.Background(), frame2)
	require.NoError(t, err)
	assert.True(t, res.Accepted)

	// The position's odometer should be clamped to last known (1000.0)
	var storedOdom float64
	err = db.QueryRow(`SELECT odometer FROM telemetry_positions WHERE imei = ? ORDER BY device_time DESC LIMIT 1`, "IMEI005").Scan(&storedOdom)
	require.NoError(t, err)
	assert.Equal(t, 1000.0, storedOdom, "odometer should keep last known value on rollback")

	// Audit log written
	assert.True(t, len(audit.actions) > 0, "audit log should be written for odometer rollback")
}

// Test 7: fuel clamp fires when |Δfuel| > threshold
func TestIngestor_FuelClamp(t *testing.T) {
	db := newTestIngestorDB(t)
	insertTestVehicle(t, db, "v-4")
	insertTestDevice(t, db, "IMEI006", DeviceStatusActive, strPtr("v-4"))

	audit := &testAudit{}
	ing := NewIngestor(db, uow.NewSQLUnitOfWork(db), events.NewInMemoryBus(),
		id.NewUUIDGenerator(), audit,
		IngestConfig{OdometerMaxRegressionKM: 1.0, FuelClampDeltaPct: 5.0})

	now := time.Now().UTC()
	frame1 := providers.RawFrame{
		IMEI:          "IMEI006",
		DeviceTime:    now,
		Latitude:      19.0,
		Longitude:     72.0,
		FuelLevel:     floatPtr(50.0),
		Provider:      "own",
		ProviderMsgID: "msg-fuel-1",
	}
	_, err := ing.IngestRawFrame(context.Background(), frame1)
	require.NoError(t, err)

	// Frame 2: fuel jumps 20 pct (> 5 threshold)
	frame2 := providers.RawFrame{
		IMEI:          "IMEI006",
		DeviceTime:    now.Add(5 * time.Second),
		Latitude:      19.1,
		Longitude:     72.1,
		FuelLevel:     floatPtr(70.0),
		Provider:      "own",
		ProviderMsgID: "msg-fuel-2",
	}
	res, err := ing.IngestRawFrame(context.Background(), frame2)
	require.NoError(t, err)
	assert.True(t, res.Accepted)

	// Clamped to 50 + 5 = 55
	var storedFuel float64
	err = db.QueryRow(`SELECT fuel_level FROM telemetry_positions WHERE imei = ? ORDER BY device_time DESC LIMIT 1`, "IMEI006").Scan(&storedFuel)
	require.NoError(t, err)
	assert.Equal(t, 55.0, storedFuel, "fuel should be clamped to 55")

	assert.True(t, len(audit.actions) > 0, "audit log should be written for fuel clamp")
}

// Test 8: NULL provider_msg_id → always inserted (no dedup)
func TestIngestor_NullProviderMsgIDNoDedup(t *testing.T) {
	db := newTestIngestorDB(t)
	insertTestVehicle(t, db, "v-5")
	insertTestDevice(t, db, "IMEI007", DeviceStatusActive, strPtr("v-5"))

	bus := events.NewInMemoryBus()
	ing := newTestIngestor(t, db, bus)

	frame := providers.RawFrame{
		IMEI:       "IMEI007",
		DeviceTime: time.Now().UTC(),
		Latitude:   19.0,
		Longitude:  72.0,
		Provider:   "own",
		// no ProviderMsgID
	}
	res, err := ing.IngestRawFrame(context.Background(), frame)
	require.NoError(t, err)
	assert.True(t, res.Accepted)

	// Re-ingest same frame (no msg id) → should insert again, not dedup
	res2, err := ing.IngestRawFrame(context.Background(), frame)
	require.NoError(t, err)
	assert.True(t, res2.Accepted)
	assert.False(t, res2.Deduped, "NULL provider_msg_id should never dedup")

	var count int
	err = db.QueryRow(`SELECT count(*) FROM telemetry_positions WHERE imei = ?`, "IMEI007").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 2, count)
}

// Test 9: vehicle_latest_position only updated when device_time is newer
func TestIngestor_LatestPositionOnlyNewer(t *testing.T) {
	db := newTestIngestorDB(t)
	insertTestVehicle(t, db, "v-6")
	insertTestDevice(t, db, "IMEI008", DeviceStatusActive, strPtr("v-6"))

	bus := events.NewInMemoryBus()
	ing := newTestIngestor(t, db, bus)

	now := time.Now().UTC()
	// Insert newer frame first
	frameNew := providers.RawFrame{
		IMEI:          "IMEI008",
		DeviceTime:    now,
		Latitude:      20.0,
		Longitude:     73.0,
		Provider:      "own",
		ProviderMsgID: "new-1",
	}
	_, err := ing.IngestRawFrame(context.Background(), frameNew)
	require.NoError(t, err)

	// Insert older frame — latest position should NOT be overwritten
	frameOld := providers.RawFrame{
		IMEI:          "IMEI008",
		DeviceTime:    now.Add(-10 * time.Second),
		Latitude:      18.0,
		Longitude:     71.0,
		Provider:      "own",
		ProviderMsgID: "old-1",
	}
	_, err = ing.IngestRawFrame(context.Background(), frameOld)
	require.NoError(t, err)

	var latestLat float64
	err = db.QueryRow(`SELECT latitude FROM vehicle_latest_position WHERE vehicle_id = ?`, "v-6").Scan(&latestLat)
	require.NoError(t, err)
	assert.Equal(t, 20.0, latestLat, "latest position should keep the newer frame's value")
}

func strPtr(s string) *string     { return &s }
func floatPtr(v float64) *float64 { return &v }

func TestIngestor_SOSEmittedInSameTransaction(t *testing.T) {
	db := newTestIngestorDB(t)
	insertTestVehicle(t, db, "v-1")
	insertTestDevice(t, db, "IMEI-SOS", DeviceStatusActive, strPtr("v-1"))

	bus := events.NewInMemoryBus()
	ing := newTestIngestor(t, db, bus)

	var sosOnBus sync.Map
	bus.Subscribe(EventTypeSOS, func(ctx context.Context, e events.Event) error {
		sosOnBus.Store("event", e)
		return nil
	})

	now := time.Now().UTC()
	result, err := ing.IngestRawFrame(context.Background(), providers.RawFrame{
		IMEI:          "IMEI-SOS",
		Latitude:      12.9716,
		Longitude:     77.5946,
		Speed:         42,
		DriverID:      "d-1",
		TripID:        "",
		SOS:           true,
		ProviderMsgID: "mqtt:sos-1",
		DeviceTime:    now,
	})
	require.NoError(t, err)
	require.True(t, result.Accepted)
	require.False(t, result.Deduped)

	var eventType string
	var payload string
	err = db.QueryRow(`SELECT event_type, payload FROM outbox_events WHERE aggregate_id = 'v-1' AND event_type = 'SOSEvent'`).
		Scan(&eventType, &payload)
	require.NoError(t, err, "SOSEvent must be persisted in the outbox")
	assert.Contains(t, payload, `"vehicle_id":"v-1"`)
	assert.Contains(t, payload, `"device_imei":"IMEI-SOS"`)

	var rawPayload string
	err = db.QueryRow(`SELECT payload FROM telemetry_raw_events WHERE imei = 'IMEI-SOS'`).
		Scan(&rawPayload)
	require.NoError(t, err)
	assert.Contains(t, rawPayload, `"sos":true`, "stored raw frame must carry the SOS flag")

	if _, ok := sosOnBus.Load("event"); !ok {
		t.Fatal("SOSEvent must be fast-path published to the in-memory bus post-commit")
	}
}

func TestIngestor_SOSReplayDeduped(t *testing.T) {
	db := newTestIngestorDB(t)
	insertTestVehicle(t, db, "v-1")
	insertTestDevice(t, db, "IMEI-SOS2", DeviceStatusActive, strPtr("v-1"))
	ing := newTestIngestor(t, db, nil)

	frame := providers.RawFrame{
		IMEI:          "IMEI-SOS2",
		Latitude:      1,
		Longitude:     2,
		SOS:           true,
		ProviderMsgID: "mqtt:sos-dup",
		DeviceTime:    time.Now().UTC(),
	}
	res1, err := ing.IngestRawFrame(context.Background(), frame)
	require.NoError(t, err)
	require.True(t, res1.Accepted)

	res2, err := ing.IngestRawFrame(context.Background(), frame)
	require.NoError(t, err)
	require.True(t, res2.Deduped)

	var n int
	err = db.QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE event_type = 'SOSEvent'`).Scan(&n)
	require.NoError(t, err)
	assert.Equal(t, 1, n, "replayed SOS frame must not emit a second SOSEvent")
}

func TestIngestor_NoSOSFlagNoSOSEvent(t *testing.T) {
	db := newTestIngestorDB(t)
	insertTestVehicle(t, db, "v-1")
	insertTestDevice(t, db, "IMEI-OK", DeviceStatusActive, strPtr("v-1"))
	ing := newTestIngestor(t, db, nil)

	res, err := ing.IngestRawFrame(context.Background(), providers.RawFrame{
		IMEI:       "IMEI-OK",
		Latitude:   3,
		Longitude:  4,
		DeviceTime: time.Now().UTC(),
	})
	require.NoError(t, err)
	require.True(t, res.Accepted)

	var n int
	err = db.QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE event_type = 'SOSEvent'`).Scan(&n)
	require.NoError(t, err)
	assert.Zero(t, n)
}

// TestIngestor_InvalidFixStoredButNotLatest (migration 00117): an invalid GNSS
// fix is kept in telemetry_positions for audit but must never overwrite
// vehicle_latest_position — the parking-drift corruption guard.
func TestIngestor_InvalidFixStoredButNotLatest(t *testing.T) {
	db := newTestIngestorDB(t)
	insertTestVehicle(t, db, "v-inv")
	insertTestDevice(t, db, "IMEI-INV", DeviceStatusActive, strPtr("v-inv"))
	ing := newTestIngestor(t, db, nil)
	ctx := context.Background()

	base := time.Now().UTC()
	validFrame := providers.RawFrame{
		IMEI: "IMEI-INV", DeviceTime: base, Latitude: 19.07, Longitude: 72.83,
		Provider: "own", ProviderMsgID: "inv-good",
	}
	res, err := ing.IngestRawFrame(ctx, validFrame)
	require.NoError(t, err)
	require.True(t, res.Accepted)

	invalid := false
	invalidFrame := providers.RawFrame{
		IMEI: "IMEI-INV", DeviceTime: base.Add(time.Minute), Latitude: 19.50, Longitude: 73.20,
		Provider: "own", ProviderMsgID: "inv-bad", Valid: &invalid,
	}
	res, err = ing.IngestRawFrame(ctx, invalidFrame)
	require.NoError(t, err)
	require.True(t, res.Accepted) // stored, not quarantined

	// Both frames in history; the invalid one flagged.
	var nValid, nInvalid int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM telemetry_positions WHERE imei='IMEI-INV' AND valid=1`).Scan(&nValid))
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM telemetry_positions WHERE imei='IMEI-INV' AND valid=0`).Scan(&nInvalid))
	assert.Equal(t, 1, nValid)
	assert.Equal(t, 1, nInvalid)

	// Latest row still holds the VALID frame's coordinates.
	var lat, lng float64
	require.NoError(t, db.QueryRow(`SELECT latitude, longitude FROM vehicle_latest_position WHERE vehicle_id='v-inv'`).Scan(&lat, &lng))
	assert.InDelta(t, 19.07, lat, 0.001)
	assert.InDelta(t, 72.83, lng, 0.001)

	// A subsequent VALID frame takes over again.
	recoveredFrame := providers.RawFrame{
		IMEI: "IMEI-INV", DeviceTime: base.Add(2 * time.Minute), Latitude: 19.10, Longitude: 72.90,
		Provider: "own", ProviderMsgID: "inv-good-2",
	}
	res, err = ing.IngestRawFrame(ctx, recoveredFrame)
	require.NoError(t, err)
	require.True(t, res.Accepted)
	require.NoError(t, db.QueryRow(`SELECT latitude FROM vehicle_latest_position WHERE vehicle_id='v-inv'`).Scan(&lat))
	assert.InDelta(t, 19.10, lat, 0.001)
}

// TestIngestor_ProviderParityFieldsPersisted (migration 00117): satellites,
// battery_level, external_voltage, gsm_signal, motion, fix_time round-trip.
func TestIngestor_ProviderParityFieldsPersisted(t *testing.T) {
	db := newTestIngestorDB(t)
	insertTestVehicle(t, db, "v-par")
	insertTestDevice(t, db, "IMEI-PAR", DeviceStatusActive, strPtr("v-par"))
	ing := newTestIngestor(t, db, nil)

	sats := 9
	batt := 78.5
	volt := 13.8
	gsm := 3
	moving := true
	fix := time.Now().UTC().Add(-time.Second)
	frame := providers.RawFrame{
		IMEI: "IMEI-PAR", DeviceTime: time.Now().UTC(),
		Latitude: 18.94, Longitude: 72.93,
		Provider: "own", ProviderMsgID: "par-1",
		Satellites: &sats, BatteryLevel: &batt, ExternalVoltage: &volt,
		GSMSignal: &gsm, Motion: &moving, FixTime: &fix,
	}
	res, err := ing.IngestRawFrame(context.Background(), frame)
	require.NoError(t, err)
	require.True(t, res.Accepted)

	var gotSats int
	var gotBatt, gotVolt float64
	var gotGsm int
	var gotMotion bool
	var gotFix time.Time
	err = db.QueryRow(`SELECT p.satellites, p.battery_level, p.external_voltage, p.gsm_signal, p.motion, p.fix_time
		FROM telemetry_positions p JOIN telemetry_raw_events e ON e.id = p.raw_event_id
		WHERE e.provider_msg_id = 'par-1'`,
	).Scan(&gotSats, &gotBatt, &gotVolt, &gotGsm, &gotMotion, &gotFix)
	require.NoError(t, err)
	assert.Equal(t, 9, gotSats)
	assert.InDelta(t, 78.5, gotBatt, 0.01)
	assert.InDelta(t, 13.8, gotVolt, 0.01)
	assert.Equal(t, 3, gotGsm)
	assert.True(t, gotMotion)
	assert.WithinDuration(t, fix, gotFix, time.Second)
}

// TestIngestor_ParkedDedupSkipsRedundantRows (migration 00117): consecutive
// motion=0 frames within the window and radius collapse to ONE position row;
// raw events + last_seen still record every frame; a moving frame writes.
func TestIngestor_ParkedDedupSkipsRedundantRows(t *testing.T) {
	db := newTestIngestorDB(t)
	insertTestVehicle(t, db, "v-park")
	insertTestDevice(t, db, "IMEI-PARK", DeviceStatusActive, strPtr("v-park"))
	ing := newTestIngestor(t, db, nil)
	ctx := context.Background()

	parked := false
	base := time.Now().UTC()

	first := providers.RawFrame{IMEI: "IMEI-PARK", DeviceTime: base, Latitude: 19.07, Longitude: 72.83,
		Provider: "own", ProviderMsgID: "pk-1", Motion: &parked}
	res, err := ing.IngestRawFrame(ctx, first)
	require.NoError(t, err)
	require.True(t, res.Accepted)

	// Same spot, 2 minutes later, still parked → deduped.
	sameSpot := providers.RawFrame{IMEI: "IMEI-PARK", DeviceTime: base.Add(2 * time.Minute), Latitude: 19.070001, Longitude: 72.830001,
		Provider: "own", ProviderMsgID: "pk-2", Motion: &parked}
	res, err = ing.IngestRawFrame(ctx, sameSpot)
	require.NoError(t, err)
	require.True(t, res.Accepted) // acked to the mobile queue, but no row

	var nPositions int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM telemetry_positions WHERE imei='IMEI-PARK'`).Scan(&nPositions))
	assert.Equal(t, 1, nPositions, "parked duplicate must not create a position row")
	// Raw event still stored for audit.
	var nRaw int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM telemetry_raw_events WHERE provider_msg_id='pk-2'`).Scan(&nRaw))
	assert.Equal(t, 1, nRaw)

	// Vehicle moves → new row.
	moving := true
	move := providers.RawFrame{IMEI: "IMEI-PARK", DeviceTime: base.Add(3 * time.Minute), Latitude: 19.50, Longitude: 73.20,
		Provider: "own", ProviderMsgID: "pk-3", Motion: &moving}
	res, err = ing.IngestRawFrame(ctx, move)
	require.NoError(t, err)
	require.True(t, res.Accepted)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM telemetry_positions WHERE imei='IMEI-PARK'`).Scan(&nPositions))
	assert.Equal(t, 2, nPositions)
}

// TestIngestor_DeviceHealthAlerts (migration 00117): low-battery and
// voltage-cut transitions emit exactly one AlertEvent each — recovery
// re-arms, repeated unhealthy frames stay silent.
func TestIngestor_DeviceHealthAlerts(t *testing.T) {
	db := newTestIngestorDB(t)
	insertTestVehicle(t, db, "v-health")
	insertTestDevice(t, db, "IMEI-HEALTH", DeviceStatusActive, strPtr("v-health"))
	ing := newTestIngestor(t, db, nil)
	ctx := context.Background()

	base := time.Now().UTC()
	batt := func(p float64) *float64 { return &p }

	// Frame 1: healthy battery (80%).
	f1 := providers.RawFrame{IMEI: "IMEI-HEALTH", DeviceTime: base, Latitude: 19.07, Longitude: 72.83,
		Provider: "own", ProviderMsgID: "h-1", BatteryLevel: batt(80)}
	res, err := ing.IngestRawFrame(ctx, f1)
	require.NoError(t, err)
	require.True(t, res.Accepted)
	var nAlerts int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE event_type = 'AlertEvent'`).Scan(&nAlerts))
	assert.Zero(t, nAlerts, "healthy frame must not alert")

	// Frame 2: crosses to 15% → exactly one low_battery alert.
	f2 := providers.RawFrame{IMEI: "IMEI-HEALTH", DeviceTime: base.Add(time.Minute), Latitude: 19.07, Longitude: 72.83,
		Provider: "own", ProviderMsgID: "h-2", BatteryLevel: batt(15)}
	res, err = ing.IngestRawFrame(ctx, f2)
	require.NoError(t, err)
	require.True(t, res.Accepted)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE event_type = 'AlertEvent'`).Scan(&nAlerts))
	assert.Equal(t, 1, nAlerts)

	// Frame 3: still 15% → no new alert (hysteresis).
	f3 := providers.RawFrame{IMEI: "IMEI-HEALTH", DeviceTime: base.Add(2 * time.Minute), Latitude: 19.07, Longitude: 72.83,
		Provider: "own", ProviderMsgID: "h-3", BatteryLevel: batt(15)}
	res, err = ing.IngestRawFrame(ctx, f3)
	require.NoError(t, err)
	require.True(t, res.Accepted)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE event_type = 'AlertEvent'`).Scan(&nAlerts))
	assert.Equal(t, 1, nAlerts)

	// Frame 4: recovered to 60% → no alert, re-armed.
	f4 := providers.RawFrame{IMEI: "IMEI-HEALTH", DeviceTime: base.Add(3 * time.Minute), Latitude: 19.07, Longitude: 72.83,
		Provider: "own", ProviderMsgID: "h-4", BatteryLevel: batt(60)}
	res, err = ing.IngestRawFrame(ctx, f4)
	require.NoError(t, err)
	require.True(t, res.Accepted)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE event_type = 'AlertEvent'`).Scan(&nAlerts))
	assert.Equal(t, 1, nAlerts)

	// Frame 5: crosses again → second alert.
	f5 := providers.RawFrame{IMEI: "IMEI-HEALTH", DeviceTime: base.Add(4 * time.Minute), Latitude: 19.07, Longitude: 72.83,
		Provider: "own", ProviderMsgID: "h-5", BatteryLevel: batt(10)}
	res, err = ing.IngestRawFrame(ctx, f5)
	require.NoError(t, err)
	require.True(t, res.Accepted)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE event_type = 'AlertEvent'`).Scan(&nAlerts))
	assert.Equal(t, 2, nAlerts)

	// The stored alert is a low_battery event with the right vehicle.
	var payload string
	require.NoError(t, db.QueryRow(`SELECT payload FROM outbox_events WHERE event_type = 'AlertEvent' ORDER BY id DESC LIMIT 1`).Scan(&payload))
	assert.Contains(t, payload, `"alert_type":"low_battery"`)
	assert.Contains(t, payload, `"vehicle_id":"v-health"`)
}

// TestIngestor_AlertKindsAndPriority (migration 00117): voltage drop on a
// hardwired device → tamper; on an app device → power_cut; GSM collapse →
// poor_signal; a frame crossing TWO thresholds emits exactly one alert and
// power/tamper wins over battery.
func TestIngestor_AlertKindsAndPriority(t *testing.T) {
	db := newTestIngestorDB(t)
	insertTestVehicleReg(t, db, "v-prio", "REG-PRIO-HW")
	insertTestVehicleReg(t, db, "v-prio-app", "REG-PRIO-APP")

	// Hardwired device (device_type 'hardware') → tamper on voltage drop.
	insertTestDevice(t, db, "IMEI-PRIO-HW", DeviceStatusActive, strPtr("v-prio"))
	// App device (device_type 'mobile_app') → power_cut on voltage drop.
	_, err := db.Exec(`INSERT INTO telemetry_devices
		(id, tenant_id, imei, serial_number, device_type, status, vehicle_id, customer_id, device_secret_hash)
		VALUES ('dev-app', '1', 'IMEI-PRIO-APP', 'SN', 'mobile_app', ?, ?, NULL, 'hash')`,
		DeviceStatusActive, "v-prio-app")
	require.NoError(t, err)

	ing := newTestIngestor(t, db, nil)
	ctx := context.Background()
	base := time.Now().UTC()
	volt := func(v float64) *float64 { return &v }
	batt := func(p float64) *float64 { return &p }

	// Healthy voltage frames for both devices.
	ing.IngestRawFrame(ctx, providers.RawFrame{IMEI: "IMEI-PRIO-HW", DeviceTime: base, Latitude: 19.07, Longitude: 72.83,
		Provider: "own", ProviderMsgID: "pr-hw-1", ExternalVoltage: volt(13.8)})
	ing.IngestRawFrame(ctx, providers.RawFrame{IMEI: "IMEI-PRIO-APP", DeviceTime: base, Latitude: 19.07, Longitude: 72.83,
		Provider: "own", ProviderMsgID: "pr-app-1", ExternalVoltage: volt(13.8)})

	// HW device: voltage cut → tamper (critical).
	ing.IngestRawFrame(ctx, providers.RawFrame{IMEI: "IMEI-PRIO-HW", DeviceTime: base.Add(time.Minute), Latitude: 19.07, Longitude: 72.83,
		Provider: "own", ProviderMsgID: "pr-hw-2", ExternalVoltage: volt(0.4)})
	var payload string
	require.NoError(t, db.QueryRow(`SELECT payload FROM outbox_events WHERE event_type='AlertEvent' AND payload LIKE '%IMEI-PRIO-HW%' ORDER BY id DESC LIMIT 1`).Scan(&payload))
	assert.Contains(t, payload, `"alert_type":"tamper"`)
	assert.Contains(t, payload, `"severity":"critical"`)

	// APP device: voltage cut → power_cut (not tamper).
	ing.IngestRawFrame(ctx, providers.RawFrame{IMEI: "IMEI-PRIO-APP", DeviceTime: base.Add(time.Minute), Latitude: 19.07, Longitude: 72.83,
		Provider: "own", ProviderMsgID: "pr-app-2", ExternalVoltage: volt(0.4)})
	require.NoError(t, db.QueryRow(`SELECT payload FROM outbox_events WHERE event_type='AlertEvent' AND payload LIKE '%IMEI-PRIO-APP%' ORDER BY id DESC LIMIT 1`).Scan(&payload))
	assert.Contains(t, payload, `"alert_type":"power_cut"`)

	// GSM collapse → poor_signal (previous row healthy, next drops).
	ing.IngestRawFrame(ctx, providers.RawFrame{IMEI: "IMEI-PRIO-HW", DeviceTime: base.Add(2 * time.Minute), Latitude: 19.07, Longitude: 72.83,
		Provider: "own", ProviderMsgID: "pr-hw-3", GSMSignal: intPtr(4)})
	ing.IngestRawFrame(ctx, providers.RawFrame{IMEI: "IMEI-PRIO-HW", DeviceTime: base.Add(3 * time.Minute), Latitude: 19.07, Longitude: 72.83,
		Provider: "own", ProviderMsgID: "pr-hw-4", GSMSignal: intPtr(1)})
	var poorCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE event_type='AlertEvent' AND payload LIKE '%"alert_type":"poor_signal"%'`).Scan(&poorCount))
	assert.Equal(t, 1, poorCount)

	// Priority: a frame crossing voltage AND battery thresholds at once →
	// exactly one alert, and it is the tamper/power_cut one.
	ing.IngestRawFrame(ctx, providers.RawFrame{IMEI: "IMEI-PRIO-HW", DeviceTime: base.Add(5 * time.Minute), Latitude: 19.07, Longitude: 72.83,
		Provider: "own", ProviderMsgID: "pr-hw-6", ExternalVoltage: volt(13.8), BatteryLevel: batt(80)})
	ing.IngestRawFrame(ctx, providers.RawFrame{IMEI: "IMEI-PRIO-HW", DeviceTime: base.Add(6 * time.Minute), Latitude: 19.07, Longitude: 72.83,
		Provider: "own", ProviderMsgID: "pr-hw-7", ExternalVoltage: volt(0.2), BatteryLevel: batt(10)})
	var prioCount int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE event_type='AlertEvent' AND payload LIKE '%IMEI-PRIO-HW%' AND payload LIKE '%"alert_type":"low_battery"%'`).Scan(&prioCount))
	assert.Zero(t, prioCount, "power/tamper must win the single alert slot")
	var hwAlerts int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM outbox_events WHERE event_type='AlertEvent' AND payload LIKE '%IMEI-PRIO-HW%'`).Scan(&hwAlerts))
	assert.Equal(t, 3, hwAlerts, "tamper(1) + poor_signal(1) + tamper(1); no low_battery")
}

// intPtr is a test helper for RawFrame.GSMSignal/Satellites.
func intPtr(i int) *int { return &i }

// TestIngestor_ParkedDedupEdges (migration 00117): window expiry and distance
// escape both break dedup — a real movement is never swallowed.
func TestIngestor_ParkedDedupEdges(t *testing.T) {
	db := newTestIngestorDB(t)
	insertTestVehicle(t, db, "v-park2")
	insertTestDevice(t, db, "IMEI-PARK2", DeviceStatusActive, strPtr("v-park2"))
	ing := newTestIngestor(t, db, nil)
	ctx := context.Background()

	parked := false
	base := time.Now().UTC()

	first := providers.RawFrame{IMEI: "IMEI-PARK2", DeviceTime: base, Latitude: 19.07, Longitude: 72.83,
		Provider: "own", ProviderMsgID: "pe-1", Motion: &parked}
	_, err := ing.IngestRawFrame(ctx, first)
	require.NoError(t, err)

	countRows := func() int {
		t.Helper()
		var n int
		require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM telemetry_positions WHERE imei='IMEI-PARK2'`).Scan(&n))
		return n
	}

	// Same spot 11 minutes later → window expired → NEW row (last_seen must
	// keep advancing via rows, otherwise staleness lies about the vehicle).
	expired := providers.RawFrame{IMEI: "IMEI-PARK2", DeviceTime: base.Add(11 * time.Minute), Latitude: 19.07, Longitude: 72.83,
		Provider: "own", ProviderMsgID: "pe-2", Motion: &parked}
	_, err = ing.IngestRawFrame(ctx, expired)
	require.NoError(t, err)
	assert.Equal(t, 2, countRows(), "window expiry must produce a fresh row")

	// Parked again, then 200 m away 1 minute later → distance escape → NEW row.
	far := providers.RawFrame{IMEI: "IMEI-PARK2", DeviceTime: base.Add(12 * time.Minute), Latitude: 19.0718, Longitude: 72.83,
		Provider: "own", ProviderMsgID: "pe-3", Motion: &parked}
	_, err = ing.IngestRawFrame(ctx, far)
	require.NoError(t, err)
	assert.Equal(t, 3, countRows(), "~200 m movement must never be deduped")
}

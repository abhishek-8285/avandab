package telemetry

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/events"
	"transport-app/internal/shared/id"
	"transport-app/internal/shared/uow"
)

// Complements the TestMQTTIngestHandler_* cases in http_ingest_test.go
// (happy path, parity fields, spoof mismatch, SOS, topic extraction) with
// the defensive edges: malformed input and unknown devices.

// newMQTTEdgeSetup builds a handler with its own inspectable audit log.
func newMQTTEdgeSetup(t *testing.T, imei string) (*MQTTIngestHandler, *testAudit, *sql.DB) {
	t.Helper()
	db := newTestIngestorDB(t)
	insertTestVehicle(t, db, "v-mqtt-edge")
	vid := "v-mqtt-edge"
	insertTestDevice(t, db, imei, DeviceStatusActive, &vid)
	audit := &testAudit{}
	ing := NewIngestor(db, uow.NewSQLUnitOfWork(db), events.NewInMemoryBus(),
		id.NewUUIDGenerator(), audit, IngestConfig{OdometerMaxRegressionKM: 1.0, FuelClampDeltaPct: 5.0})
	return NewMQTTIngestHandler(ing, slog.Default()), audit, db
}

func mqttEdgePayload(t *testing.T, imei string, seq int64) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"imei":        imei,
		"seq":         seq,
		"device_time": time.Now().UTC().Add(-2 * time.Second).Format(time.RFC3339),
		"latitude":    19.07,
		"longitude":   72.83,
	})
	require.NoError(t, err)
	return b
}

func countRows(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRowContext(context.Background(), query, args...).Scan(&n))
	return n
}

func TestMQTTEdge_InvalidTopicIgnored(t *testing.T) {
	h, audit, db := newMQTTEdgeSetup(t, "IMEI-MQTT-EDGE-1")

	for _, topic := range []string{"", "garbage", "avandab/telemetry/devices/gps", "other/a/b/c/d"} {
		h.HandleMessage(context.Background(), topic, mqttEdgePayload(t, "IMEI-MQTT-EDGE-1", 9))
	}

	assert.Equal(t, 0, countRows(t, db, `SELECT COUNT(*) FROM telemetry_raw_events`))
	assert.Equal(t, 0, countRows(t, db, `SELECT COUNT(*) FROM device_quarantine`))
	assert.Empty(t, audit.actions)
}

func TestMQTTEdge_InvalidJSONIgnored(t *testing.T) {
	h, _, db := newMQTTEdgeSetup(t, "IMEI-MQTT-EDGE-2")

	h.HandleMessage(context.Background(), "avandab/telemetry/devices/IMEI-MQTT-EDGE-2/gps", []byte("{not json"))

	assert.Equal(t, 0, countRows(t, db, `SELECT COUNT(*) FROM telemetry_raw_events`))
	assert.Equal(t, 0, countRows(t, db, `SELECT COUNT(*) FROM device_quarantine`))
}

func TestMQTTEdge_UnknownIMEIQuarantined(t *testing.T) {
	h, _, db := newMQTTEdgeSetup(t, "IMEI-MQTT-EDGE-3")

	h.HandleMessage(context.Background(), "avandab/telemetry/devices/IMEI-GHOST/gps",
		mqttEdgePayload(t, "IMEI-GHOST", 10))

	assert.Equal(t, 1, countRows(t, db,
		`SELECT COUNT(*) FROM device_quarantine WHERE imei = ? AND status = 'open'`, "IMEI-GHOST"))
	assert.Equal(t, 0, countRows(t, db, `SELECT COUNT(*) FROM telemetry_positions`))
}

// Audit claim 1: the mobile app publishes live fixes to
// avandab/telemetry/drivers/{driverId}/gps and the backend discarded them all.
// The driver_id is the device identity (Decision D3, same fallback as
// HandleTelemetrySync), the synthetic device is auto-provisioned in the
// driver's own tenant, and the fix must land in telemetry_positions.
func TestMQTTIngest_DriverTopicIngestsFix(t *testing.T) {
	db := newTestIngestorDBFK(t)
	insertTestVehicle(t, db, "vh-mqtt-drv")
	_, err := db.Exec(`INSERT INTO drivers
		(id, driver_id, first_name, last_name, phone, license_number, license_expiry, status, tenant_id)
		VALUES ('drv-row-1', 'DRV-MQTT-1', 'Test', 'Driver', '+9190000000001', 'DL-1', '2030-01-01', 'available', '1')`)
	require.NoError(t, err)

	ing := newTestIngestor(t, db, nil)
	h := NewMQTTIngestHandler(ing, nil)

	h.HandleMessage(context.Background(), "avandab/telemetry/drivers/DRV-MQTT-1/gps",
		[]byte(`{"driver_id":"DRV-MQTT-1","latitude":19.07,"longitude":72.83,"timestamp":"2026-09-27T05:00:00Z"}`))

	require.Equal(t, 1, countRows(t, db,
		`SELECT COUNT(*) FROM telemetry_positions WHERE imei = ?`, "DRV-MQTT-1"),
		"phone-published fix must reach telemetry_positions")
	assert.Equal(t, 1, countRows(t, db,
		`SELECT COUNT(*) FROM telemetry_devices WHERE imei = ? AND tenant_id = '1' AND status = 'active'`,
		"DRV-MQTT-1"), "synthetic mobile device must be auto-provisioned in the driver's tenant")
	assert.Equal(t, 1, countRows(t, db, `SELECT COUNT(*) FROM telemetry_raw_events WHERE imei = ?`, "DRV-MQTT-1"))

	var storedDriver string
	require.NoError(t, db.QueryRowContext(context.Background(),
		`SELECT COALESCE(driver_id, '') FROM telemetry_positions WHERE imei = ?`,
		"DRV-MQTT-1").Scan(&storedDriver))
	assert.Equal(t, "drv-row-1", storedDriver,
		"position must store drivers.id (the FK target), not the published driver code")
}

// The mobile app sends no sequence number: a constant provider_msg_id would
// collide with itself in UNIQUE(imei, provider_msg_id) and drop every frame
// after the first. A second publish must also land.
func TestMQTTIngest_DriverTopicSecondFrameNotDeduped(t *testing.T) {
	db := newTestIngestorDB(t)
	_, err := db.Exec(`INSERT INTO drivers
		(id, driver_id, first_name, last_name, phone, license_number, license_expiry, status, tenant_id)
		VALUES ('drv-row-2', 'DRV-MQTT-2', 'Test', 'Driver', '+9190000000002', 'DL-2', '2030-01-01', 'available', '1')`)
	require.NoError(t, err)

	ing := newTestIngestor(t, db, nil)
	h := NewMQTTIngestHandler(ing, nil)
	topic := "avandab/telemetry/drivers/DRV-MQTT-2/gps"

	h.HandleMessage(context.Background(), topic,
		[]byte(`{"driver_id":"DRV-MQTT-2","latitude":19.07,"longitude":72.83,"timestamp":"2026-09-27T05:00:00Z"}`))
	h.HandleMessage(context.Background(), topic,
		[]byte(`{"driver_id":"DRV-MQTT-2","latitude":19.08,"longitude":72.84,"timestamp":"2026-09-27T05:00:05Z"}`))

	assert.Equal(t, 2, countRows(t, db, `SELECT COUNT(*) FROM telemetry_raw_events WHERE imei = ?`, "DRV-MQTT-2"),
		"both frames must be recorded; a constant provider_msg_id would dedup the second away")
}

// Payload driver_id disagreeing with the topic is a spoof: dropped, nothing
// written under the topic's identity.
func TestMQTTIngest_DriverTopicSpoofGuard(t *testing.T) {
	db := newTestIngestorDB(t)
	ing := newTestIngestor(t, db, nil)
	h := NewMQTTIngestHandler(ing, nil)

	h.HandleMessage(context.Background(), "avandab/telemetry/drivers/DRV-MQTT-3/gps",
		[]byte(`{"driver_id":"DRV-VICTIM","latitude":19.07,"longitude":72.83,"timestamp":"2026-09-27T05:00:00Z"}`))

	assert.Equal(t, 0, countRows(t, db, `SELECT COUNT(*) FROM telemetry_positions`))
	assert.Equal(t, 0, countRows(t, db, `SELECT COUNT(*) FROM telemetry_raw_events`))
}

// A driver_id that resolves to no drivers row gets no tenant: the frame must
// NOT be provisioned anywhere and is quarantined as unknown, exactly like
// hardware with an unregistered IMEI.
func TestMQTTIngest_DriverTopicUnknownDriverQuarantined(t *testing.T) {
	db := newTestIngestorDB(t)
	ing := newTestIngestor(t, db, nil)
	h := NewMQTTIngestHandler(ing, nil)

	h.HandleMessage(context.Background(), "avandab/telemetry/drivers/NO-SUCH-DRIVER/gps",
		[]byte(`{"driver_id":"NO-SUCH-DRIVER","latitude":19.07,"longitude":72.83,"timestamp":"2026-09-27T05:00:00Z"}`))

	assert.Equal(t, 1, countRows(t, db,
		`SELECT COUNT(*) FROM device_quarantine WHERE imei = ? AND status = 'open'`, "NO-SUCH-DRIVER"))
	assert.Equal(t, 0, countRows(t, db, `SELECT COUNT(*) FROM telemetry_positions`))
}

func TestMQTTIngest_DriverTopicResolvesFromUsersTable(t *testing.T) {
	db := newTestIngestorDBFK(t)
	insertTestVehicle(t, db, "vh-mqtt-user")
	// Production reality: the drivers table is empty, so the mobile app
	// publishes its auth user id (authStore: driverId ?? id).
	_, err := db.Exec(`INSERT INTO users (id, email, password_hash, name, tenant_id)
		VALUES ('user-mqtt-1', 'driver@avandab.test', 'x', 'Driver', '7')`)
	require.NoError(t, err)

	ing := newTestIngestor(t, db, nil)
	h := NewMQTTIngestHandler(ing, nil)

	h.HandleMessage(context.Background(), "avandab/telemetry/drivers/user-mqtt-1/gps",
		[]byte(`{"driver_id":"user-mqtt-1","latitude":19.07,"longitude":72.83,"timestamp":"2026-09-27T05:00:00Z"}`))

	require.Equal(t, 1, countRows(t, db,
		`SELECT COUNT(*) FROM telemetry_positions WHERE imei = ?`, "user-mqtt-1"),
		"frame keyed by auth user id must reach telemetry_positions")
	assert.Equal(t, 1, countRows(t, db,
		`SELECT COUNT(*) FROM telemetry_devices WHERE imei = ? AND tenant_id = '7' AND status = 'active'`,
		"user-mqtt-1"), "synthetic device must be provisioned in the user's tenant, not the default one")

	var storedDriver string
	require.NoError(t, db.QueryRowContext(context.Background(),
		`SELECT COALESCE(driver_id, '') FROM telemetry_positions WHERE imei = ?`,
		"user-mqtt-1").Scan(&storedDriver))
	assert.Empty(t, storedDriver,
		"no drivers row exists, so driver_id must be NULL — writing the users.id breaks the FK")
}

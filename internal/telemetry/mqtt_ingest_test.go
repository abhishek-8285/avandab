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

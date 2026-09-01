package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/shared/id"
	"transport-app/internal/shared/uow"
	"transport-app/internal/telemetry/providers"
)

// newHTTPTestRouter sets up a full HTTP test environment: migrated DB, a
// registered active device with a known device-secret hash, routes mounted.
func newHTTPTestRouter(t *testing.T, deviceStatus string, pepper string) (chi.Router, string) {
	t.Helper()
	db := newTestIngestorDB(t)
	ing := newTestIngestor(t, db, nil)
	handler := NewHTTPIngestHandler(ing, NewDeviceStore(db), pepper)
	vID := "vh-http-1"
	insertTestVehicle(t, db, vID)
	imei := "IMEI-HTTP-1"
	token := "valid-token-123"
	secretHash := hmacSHA256(pepper, token)
	var vIDPtr = vID
	var nullStr *string
	// insert device with status + secret hash
	_, err := db.Exec(`INSERT INTO telemetry_devices
		(id, tenant_id, imei, serial_number, device_type, status, vehicle_id, customer_id, device_secret_hash)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"dev-http-1", "1", imei, "SN", "hardware", deviceStatus, &vIDPtr, nullStr, secretHash)
	require.NoError(t, err)

	r := chi.NewRouter()
	handler.RegisterRoutes(r)
	return r, imei
}

func TestHTTPDeviceGPS_ValidToken(t *testing.T) {
	pepper := "test-pepper"
	r, imei := newHTTPTestRouter(t, DeviceStatusActive, pepper)

	frame := providers.RawFrame{
		IMEI:      imei,
		Latitude:  19.07,
		Longitude: 72.83,
		Speed:     45.0,
		Provider:  "own",
	}
	body, _ := json.Marshal(frame)

	req := httptest.NewRequest("POST", "/api/v1/telemetry/devices/"+imei+"/gps", bytes.NewReader(body))
	req.Header.Set("X-Device-Token", "valid-token-123")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&resp)
	assert.True(t, resp["accepted"] == true)
}

func TestHTTPDeviceGPS_BadToken(t *testing.T) {
	r, imei := newHTTPTestRouter(t, DeviceStatusActive, "test-pepper")

	body, _ := json.Marshal(providers.RawFrame{Latitude: 19.0, Longitude: 72.0})
	req := httptest.NewRequest("POST", "/api/v1/telemetry/devices/"+imei+"/gps", bytes.NewReader(body))
	req.Header.Set("X-Device-Token", "wrong-token")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHTTPDeviceGPS_MissingToken(t *testing.T) {
	r, imei := newHTTPTestRouter(t, DeviceStatusActive, "test-pepper")

	req := httptest.NewRequest("POST", "/api/v1/telemetry/devices/"+imei+"/gps", bytes.NewReader([]byte("{}")))
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHTTPDeviceGPS_UnknownIMEI(t *testing.T) {
	r, _ := newHTTPTestRouter(t, DeviceStatusActive, "test-pepper")

	req := httptest.NewRequest("POST", "/api/v1/telemetry/devices/UNKNOWN-IMEI/gps", bytes.NewReader([]byte("{}")))
	req.Header.Set("X-Device-Token", "valid-token-123")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHTTPDeviceGPS_RetiredDevice(t *testing.T) {
	r, imei := newHTTPTestRouter(t, DeviceStatusRetired, "test-pepper")

	req := httptest.NewRequest("POST", "/api/v1/telemetry/devices/"+imei+"/gps", bytes.NewReader([]byte("{}")))
	req.Header.Set("X-Device-Token", "valid-token-123")
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusAccepted, w.Code)
	var resp map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&resp)
	assert.True(t, resp["quarantined"] == true)
}

func TestHTTPDeviceGPS_DedupReplay(t *testing.T) {
	r, imei := newHTTPTestRouter(t, DeviceStatusActive, "test-pepper")

	frame := providers.RawFrame{
		IMEI:          imei,
		Latitude:      19.07,
		Longitude:     72.83,
		Provider:      "own",
		ProviderMsgID: "http-dedup-1",
	}
	body, _ := json.Marshal(frame)

	// First ingest
	req := httptest.NewRequest("POST", "/api/v1/telemetry/devices/"+imei+"/gps", bytes.NewReader(body))
	req.Header.Set("X-Device-Token", "valid-token-123")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Replay same body
	req2 := httptest.NewRequest("POST", "/api/v1/telemetry/devices/"+imei+"/gps", bytes.NewReader(body))
	req2.Header.Set("X-Device-Token", "valid-token-123")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusOK, w2.Code)

	var resp map[string]interface{}
	_ = json.NewDecoder(w2.Body).Decode(&resp)
	assert.True(t, resp["deduped"] == true, "replay should be deduped")
}

// TestMQTTIngestHandler_ExtractIMEI verifies topic parsing.
func TestMQTTIngestHandler_ExtractIMEI(t *testing.T) {
	assert.Equal(t, "IMEI123", extractIMEIFromTopic("avandab/telemetry/devices/IMEI123/gps"))
	assert.Equal(t, "", extractIMEIFromTopic("avandab/telemetry/drivers/drv1/gps"))
	assert.Equal(t, "", extractIMEIFromTopic("garbage/topic"))
}

// TestMQTTIngestHandler_ValidMessage processes a valid MQTT frame through the
// pipeline via the handler callback.
func TestMQTTIngestHandler_ValidMessage(t *testing.T) {
	db := newTestIngestorDB(t)
	insertTestVehicle(t, db, "vh-mqtt-1")
	insertTestDevice(t, db, "IMEI-MQTT-1", DeviceStatusActive, strPtr("vh-mqtt-1"))

	ing := newTestIngestor(t, db, nil)
	h := NewMQTTIngestHandler(ing, nil)

	payload := `{
		"imei": "IMEI-MQTT-1",
		"seq": 42,
		"device_time": "2026-08-13T00:00:00Z",
		"latitude": 19.07,
		"longitude": 72.83,
		"speed": 50.0,
		"sos": false
	}`

	h.HandleMessage(context.Background(), "avandab/telemetry/devices/IMEI-MQTT-1/gps", []byte(payload))

	var count int
	err := db.QueryRow(`SELECT count(*) FROM telemetry_positions WHERE imei = ?`, "IMEI-MQTT-1").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

// TestMQTTIngestHandler_ParityFieldsPersisted (migration 00117): parity
// signals published on the MQTT topic must survive into telemetry_positions —
// the hardwired-tracker door for tamper/low-battery/poor-signal guards.
func TestMQTTIngestHandler_ParityFieldsPersisted(t *testing.T) {
	db := newTestIngestorDB(t)
	insertTestVehicle(t, db, "vh-mqtt-par")
	insertTestDevice(t, db, "IMEI-MQTT-PAR", DeviceStatusActive, strPtr("vh-mqtt-par"))

	ing := newTestIngestor(t, db, nil)
	h := NewMQTTIngestHandler(ing, nil)

	payload := `{
		"imei": "IMEI-MQTT-PAR",
		"seq": 43,
		"device_time": "2026-08-31T13:30:00Z",
		"latitude": 19.07,
		"longitude": 72.83,
		"speed": 12.5,
		"satellites": 10,
		"battery_level": 91.5,
		"external_voltage": 13.9,
		"gsm_signal": 4,
		"motion": true,
		"valid": true
	}`
	h.HandleMessage(context.Background(), "avandab/telemetry/devices/IMEI-MQTT-PAR/gps", []byte(payload))

	var sats int
	var batt, volt float64
	var gsm int
	var motion bool
	err := db.QueryRow(`SELECT satellites, battery_level, external_voltage, gsm_signal, motion
		FROM telemetry_positions WHERE imei = 'IMEI-MQTT-PAR'`).
		Scan(&sats, &batt, &volt, &gsm, &motion)
	require.NoError(t, err)
	assert.Equal(t, 10, sats)
	assert.InDelta(t, 91.5, batt, 0.01)
	assert.InDelta(t, 13.9, volt, 0.01)
	assert.Equal(t, 4, gsm)
	assert.True(t, motion)
}

// TestMQTTIngestHandler_IMEIMismatch verifies the spoof guard drops frames
// when payload IMEI does not match topic IMEI, and records an audit entry.
func TestMQTTIngestHandler_IMEIMismatch(t *testing.T) {
	db := newTestIngestorDB(t)
	audit := &testAudit{}
	ing := NewIngestor(db, uow.NewSQLUnitOfWork(db), nil, id.NewUUIDGenerator(), audit,
		IngestConfig{OdometerMaxRegressionKM: 1.0, FuelClampDeltaPct: 5.0})
	h := NewMQTTIngestHandler(ing, nil)

	payload := `{"imei":"IMEI-FORGED","latitude":19.0,"longitude":72.0}`
	h.HandleMessage(context.Background(), "avandab/telemetry/devices/IMEI-REAL/gps", []byte(payload))

	var posCount int
	err := db.QueryRow(`SELECT count(*) FROM telemetry_raw_events`).Scan(&posCount)
	require.NoError(t, err)
	assert.Equal(t, 0, posCount, "no raw events written on spoof")

	require.Len(t, audit.actions, 1, "spoof attempt should be logged")
	assert.Equal(t, "mqtt_spoof_guard", audit.actions[0].action)
	assert.Equal(t, "IMEI-REAL", audit.actions[0].record)
}

// TestMQTTIngestHandler_SOSLogging verifies SOS flag is handled.
func TestMQTTIngestHandler_SOSLogging(t *testing.T) {
	db := newTestIngestorDB(t)
	insertTestVehicle(t, db, "vh-mqtt-sos")
	insertTestDevice(t, db, "IMEI-SOS", DeviceStatusActive, strPtr("vh-mqtt-sOS"))

	ing := newTestIngestor(t, db, nil)
	h := NewMQTTIngestHandler(ing, nil)

	payload := `{"imei":"IMEI-SOS","seq":1,"device_time":"2026-08-13T00:00:00Z","latitude":19.0,"longitude":72.0,"sos":true}`
	h.HandleMessage(context.Background(), "avandab/telemetry/devices/IMEI-SOS/gps", []byte(payload))

	var count int
	err := db.QueryRow(`SELECT count(*) FROM telemetry_positions WHERE imei = ?`, "IMEI-SOS").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "SOS frame should still be ingested as a position")
}

// Ensure parseDeviceTime is exercised.
func TestParseDeviceTime(t *testing.T) {
	ts, err := parseDeviceTime("2026-08-13T00:00:00Z")
	require.NoError(t, err)
	assert.Equal(t, "2026-08-13", ts.UTC().Format("2006-01-02"))
}

// Ensure the device secret hash matches what HTTPIngestHandler validates.
func TestHMACSHA256_ValidateToken(t *testing.T) {
	pepper := "my-pepper"
	token := "device-secret-xyz"
	stored := hmacSHA256(pepper, token)
	assert.NotEqual(t, "", stored)
	assert.Equal(t, stored, hmacSHA256(pepper, token))
	assert.NotEqual(t, stored, hmacSHA256(pepper, "wrong-token"))
}

package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"transport-app/internal/middleware"
)

// mqttCredRouter mounts the broker credential routes exactly as main.go will,
// as the driver who has a driver row, inside tenant 1.
func mqttCredRouter(t *testing.T, userID, tenant, driverCode string) (*chi.Mux, *sql.DB) {
	t.Helper()
	db := newShareTestDB(t)
	app := newShareTestApp(t, db, allowAuthSvc{})

	email := userID + "@t.example"
	_, err := db.Exec(`INSERT INTO users (id, email, password_hash, name, role_id, status, tenant_id)
		VALUES (?, ?, 'hash', 'Driver', 5, 'active', ?)`, userID, email, tenant)
	require.NoError(t, err)
	if driverCode != "" {
		_, err = db.Exec(`INSERT INTO drivers (id, driver_id, first_name, last_name, phone, email,
				license_number, license_expiry, status, tenant_id)
			VALUES (?, ?, 'First', 'Last', '+919000000000', ?, 'DL-1', '2030-01-01', 'available', ?)`,
			"row-"+userID, driverCode, email, tenant)
		require.NoError(t, err)
	}

	r := chi.NewRouter()
	r.With(
		withUserAndTenant(userID, tenant, nil),
		middleware.RequirePermission(app.AuthSrv, "driver", "write-self"),
	).Get("/api/v1/telemetry/mqtt-credentials", app.MQTTCredentials.Get)
	r.With(
		withUserAndTenant(userID, tenant, nil),
		middleware.RequirePermission(app.AuthSrv, "driver", "write-self"),
	).Post("/api/v1/telemetry/mqtt-credentials/rotate", app.MQTTCredentials.Rotate)
	return r, db
}

func getCred(t *testing.T, r *chi.Mux, path string) (int, mqttCredential) {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	var out mqttCredential
	if w.Code == http.StatusOK {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	}
	return w.Code, out
}

func storedHash(t *testing.T, db *sql.DB, username string) string {
	t.Helper()
	var h string
	require.NoError(t, db.QueryRow(
		`SELECT password_hash FROM driver_mqtt_credentials WHERE username = ?`, username).Scan(&h))
	return h
}

// The first call must hand the device a usable secret exactly once: the broker
// is anonymous-off, so a driver with no credential cannot publish at all, and a
// stored plaintext would be a silent breach of that secret.
func TestMQTTCredentials_FirstCallProvisionsSecretThenStopsRepeatingIt(t *testing.T) {
	r, db := mqttCredRouter(t, "u-mqtt-1", "1", "DRV-MQTT-1")

	code, first := getCred(t, r, "/api/v1/telemetry/mqtt-credentials")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "DRV-MQTT-1", first.Username, "username must be the identity published in the topic")
	assert.NotEmpty(t, first.Password, "first call must carry the plaintext secret")
	assert.True(t, first.Provisioned)

	var rows int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM driver_mqtt_credentials`).Scan(&rows))
	assert.Equal(t, 1, rows)

	// The stored secret is the mosquitto hash, never the plaintext.
	hash := storedHash(t, db, "DRV-MQTT-1")
	assert.NotContains(t, hash, first.Password, "password_hash must not contain the plaintext secret")
	assert.Contains(t, hash, "$7$1000$", "must be a mosquitto PBKDF2-SHA512 password-file entry")

	// Second call: username only. Replaying the secret would put it in every
	// proxy log between the phone and the app.
	code, second := getCred(t, r, "/api/v1/telemetry/mqtt-credentials")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "DRV-MQTT-1", second.Username)
	assert.Empty(t, second.Password, "the plaintext secret is issued once")
	assert.False(t, second.Provisioned)
}

// A reinstalled phone has no secret; rotation re-issues one and replaces the
// hash the broker's password file is built from.
func TestMQTTCredentials_RotateIssuesNewSecretAndReplacesHash(t *testing.T) {
	r, db := mqttCredRouter(t, "u-mqtt-2", "1", "DRV-MQTT-2")

	_, first := getCred(t, r, "/api/v1/telemetry/mqtt-credentials")
	firstHash := storedHash(t, db, "DRV-MQTT-2")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("POST", "/api/v1/telemetry/mqtt-credentials/rotate", nil))
	require.Equal(t, http.StatusOK, w.Code)
	var rotated mqttCredential
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &rotated))
	assert.Equal(t, "DRV-MQTT-2", rotated.Username)
	assert.NotEmpty(t, rotated.Password)
	assert.NotEqual(t, first.Password, rotated.Password, "rotation must issue a new secret")

	secondHash := storedHash(t, db, "DRV-MQTT-2")
	assert.NotEqual(t, firstHash, secondHash, "rotation must replace the broker password hash")

	var rotatedAt string
	require.NoError(t, db.QueryRow(
		`SELECT COALESCE(rotated_at, '') FROM driver_mqtt_credentials WHERE username = 'DRV-MQTT-2'`).Scan(&rotatedAt))
	assert.NotEmpty(t, rotatedAt, "rotation must be recorded for the ops reload")
}

// Production has an empty drivers table, so the mobile publishes its auth user
// id as the topic segment. The broker username must be that same value or the
// ACL `pattern ... /%u/gps` would deny the driver's own topic.
func TestMQTTCredentials_FallsBackToAuthUserIDWithoutDriverRow(t *testing.T) {
	r, db := mqttCredRouter(t, "u-mqtt-3", "1", "")

	code, cred := getCred(t, r, "/api/v1/telemetry/mqtt-credentials")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "u-mqtt-3", cred.Username, "no driver row → username is the auth user id")
	assert.NotEmpty(t, cred.Password)
	assert.NotEmpty(t, storedHash(t, db, "u-mqtt-3"))
}

// A driver identity carrying an MQTT wildcard would turn
// `pattern write avandab/telemetry/drivers/%u/gps` into a wildcard that unlocks
// other drivers' topics (mosquitto#1610, docs/13 §4.2). Refuse instead.
func TestMQTTCredentials_RejectsWildcardDriverIdentity(t *testing.T) {
	r, db := mqttCredRouter(t, "u-mqtt-4", "1", "DRV+4")

	code, _ := getCred(t, r, "/api/v1/telemetry/mqtt-credentials")
	assert.Equal(t, http.StatusUnprocessableEntity, code)

	var rows int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM driver_mqtt_credentials`).Scan(&rows))
	assert.Equal(t, 0, rows, "a wildcard identity must never be provisioned")
}

// Another org must not be able to read or overwrite this driver's credential.
func TestMQTTCredentials_TenantScopedToTheCallersOrg(t *testing.T) {
	r, db := mqttCredRouter(t, "u-mqtt-5", "1", "DRV-MQTT-5")

	_, cred := getCred(t, r, "/api/v1/telemetry/mqtt-credentials")
	require.NotEmpty(t, cred.Password)

	var tenantID string
	require.NoError(t, db.QueryRow(
		`SELECT tenant_id FROM driver_mqtt_credentials WHERE username = 'DRV-MQTT-5'`).Scan(&tenantID))
	assert.Equal(t, "1", tenantID, "credential must be owned by the caller's tenant")
}

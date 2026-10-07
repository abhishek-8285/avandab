package handlers

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"

	"transport-app/internal/mqttservice"
	"transport-app/internal/shared"
)

// MQTT credential provisioning for the hardened broker (docs/13 §5.2).
//
// The broker runs `allow_anonymous false` + `acl_file` with
//
//	pattern write avandab/telemetry/drivers/%u/gps
//
// so a driver's credential only unlocks their own GPS topic — verified on
// mosquitto 2.1.2: a cross-driver publish is dropped by the ACL while the
// client still sees PUBACK RC:0. The plaintext password exists only in this
// response; the table keeps the mosquitto password-file hash, so a lost phone
// is fixed by rotation, not by reading the database.
type MQTTCredentialsHandlers struct {
	*App
	db *sql.DB
}

// NewMQTTCredentialsHandlers wires the broker credential endpoints.
func NewMQTTCredentialsHandlers(app *App, db *sql.DB) *MQTTCredentialsHandlers {
	return &MQTTCredentialsHandlers{App: app, db: db}
}

// mqttCredential is the API shape. Password is only populated on the response
// that mints or rotates the secret.
type mqttCredential struct {
	Username string `json:"username"`
	Password string `json:"password,omitempty"`
	// Provisioned is true when this response carried a usable secret. False
	// means the device already holds one and must rotate to get another.
	Provisioned bool `json:"provisioned"`
}

// requireTenant resolves the acting org, failing closed: these routes sit
// behind auth middleware, so a missing tenant is a bad session, not a 500.
func (h *MQTTCredentialsHandlers) requireTenant(w http.ResponseWriter, r *http.Request) (string, bool) {
	tid, err := shared.TenantRequired(r.Context())
	if err != nil {
		http.Error(w, `{"error":"tenant required"}`, http.StatusUnauthorized)
		return "", false
	}
	return string(tid), true
}

// brokerUsername resolves the identity the mobile publishes in its MQTT topic
// and the broker ACL binds with %u. It mirrors the client: the driver code
// once a driver row exists, otherwise the auth user id (the production case —
// the drivers table is empty, and `driverId ?? id` in authStore).
func (h *MQTTCredentialsHandlers) brokerUsername(ctx context.Context, tenantID, userID string) (string, error) {
	var username sql.NullString
	err := h.db.QueryRowContext(ctx, `
		SELECT d.driver_id FROM users u
		LEFT JOIN drivers d
		  ON d.tenant_id = u.tenant_id
		 AND d.email IS NOT NULL AND d.email = u.email
		WHERE u.id = $1 AND u.tenant_id = $2`, userID, tenantID).Scan(&username)
	if errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if err != nil {
		return "", err
	}
	if username.Valid && username.String != "" {
		return username.String, nil
	}
	return userID, nil
}

// Get provisions the broker credential on first call and afterwards reports the
// username without a password: the plaintext is returned exactly once.
func (h *MQTTCredentialsHandlers) Get(w http.ResponseWriter, r *http.Request) {
	h.issue(w, r, false)
}

// Rotate re-issues the secret for a device that lost it (reinstall, wiped
// secure storage) and invalidates the old one on the next broker reload.
func (h *MQTTCredentialsHandlers) Rotate(w http.ResponseWriter, r *http.Request) {
	h.issue(w, r, true)
}

func (h *MQTTCredentialsHandlers) issue(w http.ResponseWriter, r *http.Request, rotate bool) {
	user, ok := h.getUserFromContext(r)
	if !ok || user == nil {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	ctx := r.Context()

	username, err := h.brokerUsername(ctx, tenantID, user.UserID)
	if err != nil {
		slog.ErrorContext(ctx, "broker username lookup failed",
			slog.String("user_id", user.UserID), slog.Any("error", err))
		http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
		return
	}
	// `+`/`#` in a username would turn `pattern ... /%u/gps` into a wildcard
	// that unlocks other drivers' topics (mosquitto#1610, docs/13 §4.2).
	if err := mqttservice.ValidateBrokerUsername(username); err != nil {
		slog.WarnContext(ctx, "driver identity cannot be a broker username",
			slog.String("username", username), slog.Any("error", err))
		http.Error(w, `{"error":"driver identity is not a valid broker username"}`, http.StatusUnprocessableEntity)
		return
	}

	var existing string
	err = h.db.QueryRowContext(ctx,
		`SELECT password_hash FROM driver_mqtt_credentials WHERE username = $1`, username).Scan(&existing)
	if err == nil && !rotate {
		writeJSON(w, http.StatusOK, mqttCredential{Username: username, Provisioned: false})
		return
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		slog.ErrorContext(ctx, "broker credential lookup failed",
			slog.String("username", username), slog.Any("error", err))
		http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
		return
	}

	secret, hash, err := mqttservice.NewBrokerSecret()
	if err != nil {
		slog.ErrorContext(ctx, "broker secret generation failed", slog.Any("error", err))
		http.Error(w, `{"error":"could not issue credential"}`, http.StatusInternalServerError)
		return
	}

	// One upsert for both paths. A rotate against a driver with no stored row
	// (row deleted, provisioning write lost, phone reinstalled against a fresh
	// database) must still record the secret: the broker password file is built
	// from this table, so an UPDATE-only rotate would hand the phone a plaintext
	// that no table and no password file knows.
	_, err = h.db.ExecContext(ctx, `
		INSERT INTO driver_mqtt_credentials (driver_key, tenant_id, username, password_hash)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (username) DO UPDATE SET password_hash = excluded.password_hash,
		                                        rotated_at = datetime('now')`,
		username, tenantID, username, hash)
	if err != nil {
		slog.ErrorContext(ctx, "broker credential write failed",
			slog.String("username", username), slog.Any("error", err))
		http.Error(w, `{"error":"could not issue credential"}`, http.StatusInternalServerError)
		return
	}

	// The plaintext exists only here; nothing downstream stores it.
	writeJSON(w, http.StatusOK, mqttCredential{Username: username, Password: secret, Provisioned: true})
}
